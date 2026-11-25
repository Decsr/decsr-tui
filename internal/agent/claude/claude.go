// Package claude reads Claude Code transcripts (~/.claude/projects) read-only.
package claude

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Decsr/decsr-tui/internal/agent"
)

const liveWindow = 15 * time.Second

var nonProjectChar = regexp.MustCompile(`[^A-Za-z0-9-]`)

// EncodeDir maps a session cwd to its Claude project directory name.
func EncodeDir(cwd string) string { return nonProjectChar.ReplaceAllString(cwd, "-") }

type source struct{ root string }

// New reads from $CLAUDE_CONFIG_DIR/projects or ~/.claude/projects.
func New() agent.Source {
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".claude")
	}
	return NewWithRoot(filepath.Join(dir, "projects"))
}

// NewWithRoot reads from an explicit projects directory (tests).
func NewWithRoot(projectsRoot string) agent.Source { return &source{root: projectsRoot} }

func (s *source) Name() string { return "claude" }

func (s *source) projectDir(repoDir string) string {
	name := EncodeDir(repoDir)
	exact := filepath.Join(s.root, name)
	if st, err := os.Stat(exact); err == nil && st.IsDir() {
		return exact
	}
	if entries, err := os.ReadDir(s.root); err == nil {
		for _, e := range entries {
			if e.IsDir() && agent.EqualName(e.Name(), name) {
				return filepath.Join(s.root, e.Name())
			}
		}
	}
	return exact
}

func (s *source) Sessions(repoDir string) ([]agent.Session, error) {
	dir := s.projectDir(repoDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []agent.Session
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, scanSession(filepath.Join(dir, e.Name()), info.ModTime()))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out, nil
}

// scanSession reads the metadata of a transcript without building activities.
func scanSession(path string, mtime time.Time) agent.Session {
	ss := agent.Session{
		Runner: "claude", ID: strings.TrimSuffix(filepath.Base(path), ".jsonl"),
		Path: path, Updated: mtime, Live: time.Since(mtime) < liveWindow,
	}
	f, err := os.Open(path)
	if err != nil {
		return ss
	}
	defer f.Close()
	var prompt string
	r := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := r.ReadString('\n')
		if strings.HasSuffix(line, "\n") { // ignore a partial last line
			complete := ss.Cwd != "" && ss.Branch != "" && ss.Model != "" && prompt != "" && !ss.Started.IsZero()
			if !complete || strings.Contains(line, `"ai-title"`) {
				var rec record
				if json.Unmarshal([]byte(line), &rec) == nil {
					if rec.Cwd != "" && ss.Cwd == "" {
						ss.Cwd = rec.Cwd
					}
					if rec.GitBranch != "" && ss.Branch == "" {
						ss.Branch = rec.GitBranch
					}
					if t, e := time.Parse(time.RFC3339Nano, rec.Timestamp); e == nil && ss.Started.IsZero() {
						ss.Started = t
					}
					switch rec.Type {
					case "ai-title":
						if rec.AITitle != "" {
							ss.Title = rec.AITitle
						}
					case "assistant":
						if rec.Message.Model != "" && ss.Model == "" {
							ss.Model = rec.Message.Model
						}
					case "user":
						if prompt == "" && !rec.IsMeta {
							prompt = promptText(rec)
						}
					}
				}
			}
		}
		if err != nil {
			break
		}
	}
	if ss.Title == "" {
		ss.Title = truncate(firstLine(prompt), 80)
	}
	return ss
}

func promptText(r record) string {
	text, blocks, isStr := parseBlocks(r.Message.Content)
	if isStr {
		return cleanPrompt(text)
	}
	for _, b := range blocks {
		if b.Type == "text" && cleanPrompt(b.Text) != "" {
			return cleanPrompt(b.Text)
		}
	}
	return ""
}

func (s *source) Open(sess agent.Session) agent.Tracker {
	t := &tracker{sess: sess, main: newStream(sess.Path, sess.Cwd), subs: map[string]*sub{}}
	t.subDir = filepath.Join(filepath.Dir(sess.Path), strings.TrimSuffix(filepath.Base(sess.Path), ".jsonl"), "subagents")
	return t
}

type sub struct {
	agent.Subagent
	st    *stream
	mtime time.Time
}

type tracker struct {
	sess   agent.Session
	main   *stream
	subDir string
	subs   map[string]*sub
	order  []string
	live   bool
}

type metaFile struct {
	AgentType   string `json:"agentType"`
	Description string `json:"description"`
	ToolUseID   string `json:"toolUseId"`
	Model       string `json:"model"`
	Finished    bool   `json:"finished"`
}

func (t *tracker) Poll() (bool, error) {
	changed, err := t.main.poll()
	if err != nil {
		return changed, err
	}
	if live := time.Since(t.main.mtime) < liveWindow; live != t.live {
		t.live, changed = live, true
	}
	if t.pollSubs() {
		changed = true
	}
	return changed, nil
}

func (t *tracker) pollSubs() bool {
	changed := false
	metas, _ := filepath.Glob(filepath.Join(t.subDir, "agent-*.meta.json"))
	sort.Strings(metas)
	for _, mp := range metas {
		id := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(mp), "agent-"), ".meta.json")
		sb := t.subs[id]
		var m metaFile
		data, err := os.ReadFile(mp)
		metaOK := err == nil && json.Unmarshal(data, &m) == nil
		if !metaOK && sb == nil {
			continue // meta may still be mid-write
		}
		if sb == nil {
			jp := filepath.Join(t.subDir, "agent-"+id+".jsonl")
			sb = &sub{st: newStream(jp, t.sess.Cwd)}
			sb.ID, sb.Path = id, jp
			t.subs[id] = sb
			t.order = append(t.order, id)
			changed = true
		}
		if metaOK && (sb.Type != m.AgentType || sb.Description != m.Description || sb.ToolUseID != m.ToolUseID ||
			sb.Model != m.Model || sb.Finished != m.Finished) {
			sb.Type, sb.Description, sb.ToolUseID, sb.Model, sb.Finished =
				m.AgentType, m.Description, m.ToolUseID, m.Model, m.Finished
			changed = true
		}
		if c, _ := sb.st.poll(); c {
			changed = true
		}
		if live := !sb.Finished && time.Since(sb.st.mtime) < liveWindow; live != sb.Live {
			sb.Live, changed = live, true
		}
	}
	return changed
}

func (t *tracker) Snapshot() agent.Snapshot {
	st := t.main
	ss := t.sess
	ss.Runner = "claude"
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
	if st.title != "" {
		ss.Title = st.title
	} else if st.firstPrompt != "" && ss.Title == "" {
		ss.Title = truncate(firstLine(st.firstPrompt), 80)
	}
	if !st.mtime.IsZero() {
		ss.Updated, ss.Live = st.mtime, t.live
	}

	acts := append([]agent.Activity(nil), st.acts...)
	snap := agent.Snapshot{Session: ss, Activities: acts, Usage: st.usage}
	byTool := map[string]string{}
	for _, id := range t.order {
		sb := t.subs[id]
		cp := sb.Subagent
		cp.Activities = append([]agent.Activity(nil), sb.st.acts...)
		cp.Usage = sb.st.usage
		if cp.ToolUseID != "" {
			byTool[cp.ToolUseID] = id
		}
		snap.Subagents = append(snap.Subagents, cp)
	}
	for i := range acts {
		if acts[i].Kind == agent.KindSubagent {
			id := byTool[acts[i].ID]
			acts[i].SubagentID = id
			// The Agent tool returns at once for background subagents, so its
			// tool_result alone does not mean the work is done.
			if id != "" && !t.subs[id].Finished && acts[i].State == agent.StateDone {
				acts[i].State = agent.StateRunning
			}
		}
	}
	return snap
}
