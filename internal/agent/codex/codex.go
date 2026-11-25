// Package codex reads OpenAI Codex CLI rollout files (~/.codex/sessions) read-only.
//
// Format based on openai/codex (main, checked 2026-11), codex-rs/protocol:
//   - protocol.rs: RolloutLine {timestamp, type, payload} with type one of
//     session_meta | response_item | turn_context | event_msg | compacted;
//     SessionMeta/SessionMetaLine (cwd, id, timestamp, git.branch),
//     TurnContextItem.model, EventMsg token_count/exec_command_end/patch_apply_end.
//   - models.rs: ResponseItem (message, reasoning, function_call,
//     function_call_output, local_shell_call, custom_tool_call(_output),
//     web_search_call), FunctionCallOutputPayload (serialized as a string).
//
// Only the rollout files are read. auth.json and config.toml are never opened.
package codex

import (
	"bufio"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Decsr/decsr-tui/internal/agent"
)

const liveWindow = 15 * time.Second

type source struct{ root string }

// New reads from $CODEX_HOME/sessions or ~/.codex/sessions.
func New() agent.Source {
	dir := os.Getenv("CODEX_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".codex")
	}
	return NewWithRoot(filepath.Join(dir, "sessions"))
}

// NewWithRoot reads from an explicit sessions directory (tests).
func NewWithRoot(sessionsRoot string) agent.Source { return &source{root: sessionsRoot} }

func (s *source) Name() string { return "codex" }

func (s *source) Sessions(repoDir string) ([]agent.Session, error) {
	if _, err := os.Stat(s.root); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []agent.Session
	filepath.WalkDir(s.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		name := d.Name()
		if !strings.HasPrefix(name, "rollout-") || !strings.HasSuffix(name, ".jsonl") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		ss := scanSession(p, info.ModTime())
		if agent.SameDir(ss.Cwd, repoDir) {
			out = append(out, ss)
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out, nil
}

// scanSession reads the head of a rollout for metadata, without building activities.
func scanSession(path string, mtime time.Time) agent.Session {
	ss := agent.Session{
		Runner: "codex", ID: strings.TrimSuffix(filepath.Base(path), ".jsonl"),
		Path: path, Updated: mtime, Live: time.Since(mtime) < liveWindow,
	}
	f, err := os.Open(path)
	if err != nil {
		return ss
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)
	var prompt string
	for n := 0; n < 400; n++ {
		line, err := r.ReadString('\n')
		if strings.HasSuffix(line, "\n") {
			var rl rolloutLine
			if json.Unmarshal([]byte(line), &rl) == nil {
				switch rl.Type {
				case "session_meta":
					var m metaPayload
					if json.Unmarshal(rl.Payload, &m) == nil {
						ss.Cwd, ss.Branch = m.Cwd, m.Git.Branch
						if m.ID != "" {
							ss.ID = m.ID
						}
						if t, e := time.Parse(time.RFC3339Nano, m.Timestamp); e == nil {
							ss.Started = t
						}
					}
				case "turn_context":
					var m struct{ Model string }
					if json.Unmarshal(rl.Payload, &m) == nil && ss.Model == "" {
						ss.Model = m.Model
					}
				case "response_item":
					if prompt == "" {
						if it := parseItem(rl.Payload); it.Type == "message" && it.Role == "user" {
							prompt = userText(it)
						}
					}
				}
			}
		}
		if err != nil || (ss.Cwd != "" && ss.Model != "" && prompt != "") {
			break
		}
	}
	ss.Title = truncate(firstLine(prompt), 80)
	return ss
}

func (s *source) Open(sess agent.Session) agent.Tracker {
	return &tracker{sess: sess, st: newStream(sess.Path, sess.Cwd)}
}

type tracker struct {
	sess agent.Session
	st   *stream
	live bool
}

func (t *tracker) Poll() (bool, error) {
	changed, err := t.st.poll()
	if err != nil {
		return changed, err
	}
	if live := time.Since(t.st.mtime) < liveWindow; live != t.live {
		t.live, changed = live, true
	}
	return changed, nil
}

func (t *tracker) Snapshot() agent.Snapshot {
	st := t.st
	ss := t.sess
	ss.Runner = "codex"
	if st.sessCwd != "" {
		ss.Cwd = st.sessCwd
	}
	if st.branch != "" {
		ss.Branch = st.branch
	}
	if st.model != "" {
		ss.Model = st.model
	}
	if !st.started.IsZero() {
		ss.Started = st.started
	}
	if ss.Title == "" && st.firstPrompt != "" {
		ss.Title = truncate(firstLine(st.firstPrompt), 80)
	}
	if !st.mtime.IsZero() {
		ss.Updated, ss.Live = st.mtime, t.live
	}
	return agent.Snapshot{Session: ss, Activities: append([]agent.Activity(nil), st.acts...), Usage: st.usage}
}
