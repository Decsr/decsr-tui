package codex

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Decsr/decsr-tui/internal/agent"
)

const detailCap = 64 << 10

type rolloutLine struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type metaPayload struct {
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	Cwd       string `json:"cwd"`
	Git       struct {
		Branch string `json:"branch"`
	} `json:"git"`
}

// item is a ResponseItem or an EventMsg payload; fields of both share one struct.
type item struct {
	Type      string          `json:"type"`
	Role      string          `json:"role"`
	Content   json.RawMessage `json:"content"`
	Summary   json.RawMessage `json:"summary"`
	Name      string          `json:"name"`
	Arguments string          `json:"arguments"`
	Input     string          `json:"input"`
	CallID    string          `json:"call_id"`
	ID        string          `json:"id"`
	Output    json.RawMessage `json:"output"`
	Action    json.RawMessage `json:"action"`

	// event_msg
	ExitCode         *int   `json:"exit_code"`
	AggregatedOutput string `json:"aggregated_output"`
	Success          *bool  `json:"success"`
	Stdout           string `json:"stdout"`
}

func parseItem(raw json.RawMessage) item {
	var it item
	_ = json.Unmarshal(raw, &it)
	return it
}

// stream incrementally parses one rollout file.
type stream struct {
	path   string
	offset int64
	cwd    string

	acts   []agent.Activity
	byCall map[string][]int // call_id -> indexes in acts
	result map[string]int   // call_id -> detail length before result text (-1: from the output item)
	usage  agent.Usage

	firstPrompt, sessCwd, branch, model string
	started, mtime                      time.Time
}

func newStream(path, cwd string) *stream {
	return &stream{path: path, cwd: cwd, byCall: map[string][]int{}, result: map[string]int{}}
}

func (s *stream) poll() (bool, error) {
	f, err := os.Open(s.path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return false, err
	}
	changed := !st.ModTime().Equal(s.mtime)
	if st.Size() < s.offset {
		*s = *newStream(s.path, s.cwd)
		changed = true
	}
	s.mtime = st.ModTime()
	if st.Size() == s.offset {
		return changed, nil
	}
	if _, err := f.Seek(s.offset, io.SeekStart); err != nil {
		return changed, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return changed, err
	}
	text := string(data)
	end := strings.LastIndexByte(text, '\n')
	if end < 0 {
		return changed, nil
	}
	s.offset += int64(end + 1)
	for _, line := range strings.Split(text[:end], "\n") {
		if line = strings.TrimSpace(line); line != "" {
			s.handle(line)
		}
	}
	return true, nil
}

func (s *stream) handle(line string) {
	var rl rolloutLine
	if json.Unmarshal([]byte(line), &rl) != nil {
		return
	}
	at, _ := time.Parse(time.RFC3339Nano, rl.Timestamp)
	if !at.IsZero() && (s.started.IsZero() || at.Before(s.started)) {
		s.started = at
	}
	switch rl.Type {
	case "session_meta":
		var m metaPayload
		if json.Unmarshal(rl.Payload, &m) == nil {
			if m.Cwd != "" && s.sessCwd == "" {
				s.sessCwd = m.Cwd
			}
			if m.Git.Branch != "" {
				s.branch = m.Git.Branch
			}
			if t, e := time.Parse(time.RFC3339Nano, m.Timestamp); e == nil && (s.started.IsZero() || t.Before(s.started)) {
				s.started = t
			}
		}
	case "turn_context":
		var m struct{ Model string }
		if json.Unmarshal(rl.Payload, &m) == nil && m.Model != "" {
			s.model = m.Model
		}
	case "response_item":
		s.handleItem(parseItem(rl.Payload), at)
	case "event_msg":
		s.handleEvent(parseItem(rl.Payload), rl.Payload, at)
	}
}

// contentText joins the text parts of a ContentItem list (or a plain string).
func contentText(raw json.RawMessage) string {
	var parts []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		var str string
		if json.Unmarshal(raw, &str) == nil {
			return str
		}
		return ""
	}
	var out []string
	for _, p := range parts {
		if p.Text != "" {
			out = append(out, p.Text)
		}
	}
	return strings.Join(out, "\n")
}

// userText returns the human prompt of a user message item, or "" for the
// harness context Codex injects as user messages (<environment_context> etc.).
func userText(it item) string {
	t := strings.TrimSpace(contentText(it.Content))
	if strings.HasPrefix(t, "<") || strings.HasPrefix(t, "# AGENTS.md instructions") {
		return ""
	}
	return t
}

func (s *stream) add(a agent.Activity, callID string) {
	if callID != "" {
		s.byCall[callID] = append(s.byCall[callID], len(s.acts))
	}
	s.acts = append(s.acts, a)
}

func (s *stream) handleItem(it item, at time.Time) {
	id := "i" + strconv.Itoa(len(s.acts))
	switch it.Type {
	case "message":
		switch it.Role {
		case "user":
			if t := userText(it); t != "" {
				if s.firstPrompt == "" {
					s.firstPrompt = t
				}
				s.add(agent.Activity{ID: id, Kind: agent.KindUser, State: agent.StateDone,
					Title: firstLine(t), Detail: capStr(t), At: at}, "")
			}
		case "assistant":
			if text := strings.TrimSpace(contentText(it.Content)); text != "" {
				s.add(agent.Activity{ID: id, Kind: agent.KindMessage, State: agent.StateDone,
					Title: firstLine(text), Detail: capStr(text), At: at}, "")
			}
		}
	case "reasoning":
		text := strings.TrimSpace(contentText(it.Summary))
		if text == "" {
			text = strings.TrimSpace(contentText(it.Content))
		}
		if text != "" {
			s.add(agent.Activity{ID: id, Kind: agent.KindThinking, State: agent.StateDone,
				Title: firstLine(text), Detail: capStr(text), At: at}, "")
		}
	case "function_call":
		s.call(it.CallID, it.Name, it.Arguments, at)
	case "local_shell_call":
		var act struct {
			Command []string `json:"command"`
		}
		_ = json.Unmarshal(it.Action, &act)
		cid := it.CallID
		if cid == "" {
			cid = it.ID
		}
		s.command(cid, "local_shell", act.Command, "", at)
	case "custom_tool_call":
		if it.Name == "apply_patch" {
			s.patch(it.CallID, it.Name, it.Input, at)
		} else {
			s.add(agent.Activity{ID: it.CallID, Kind: agent.KindTool, State: agent.StateRunning, Tool: it.Name,
				Title: it.Name, Detail: capStr(it.Input), At: at}, it.CallID)
		}
	case "web_search_call":
		var act struct{ Query string }
		_ = json.Unmarshal(it.Action, &act)
		s.add(agent.Activity{ID: id, Kind: agent.KindSearch, State: agent.StateDone, Tool: "web_search",
			Title: strings.TrimSpace("web_search " + act.Query), Detail: string(it.Action), At: at}, "")
	case "function_call_output", "custom_tool_call_output":
		text, failed := outputText(it.Output)
		s.finish(it.CallID, failed, text, at, false)
	}
}

func (s *stream) call(callID, name, args string, at time.Time) {
	var in map[string]any
	_ = json.Unmarshal([]byte(args), &in)
	str := func(k string) string { v, _ := in[k].(string); return v }
	switch name {
	case "shell", "container.exec", "shell_command", "exec_command", "unified_exec", "local_shell":
		var cmd []string
		switch v := in["command"].(type) {
		case []any:
			for _, x := range v {
				cmd = append(cmd, toStr(x))
			}
		case string:
			cmd = []string{v}
		}
		if len(cmd) == 0 && str("cmd") != "" {
			cmd = []string{str("cmd")}
		}
		s.command(callID, name, cmd, str("justification"), at)
	case "apply_patch":
		s.patch(callID, name, firstNonEmpty(str("input"), str("patch")), at)
	default:
		a := agent.Activity{ID: callID, State: agent.StateRunning, Tool: name, At: at, Detail: capStr(args), Kind: agent.KindTool, Title: name}
		switch name {
		case "read_file", "view_image":
			a.Kind = agent.KindRead
			a.File = firstNonEmpty(str("file_path"), str("path"))
			a.Title = name + " " + relPath(s.curCwd(), a.File)
		case "grep_files", "list_dir", "web_search", "search", "file_search":
			a.Kind = agent.KindSearch
			a.Title = strings.TrimSpace(name + " " + firstNonEmpty(str("pattern"), str("query"), str("dir_path"), str("path")))
		case "spawn_agent", "task":
			a.Kind = agent.KindSubagent
			a.Title = name + ": " + firstLine(firstNonEmpty(str("message"), str("description"), str("prompt")))
		}
		s.add(a, callID)
	}
}

func (s *stream) curCwd() string {
	if s.sessCwd != "" {
		return s.sessCwd
	}
	return s.cwd
}

var shellWrap = regexp.MustCompile(`(?i)^(bash|sh|zsh|cmd|powershell|pwsh)(\.exe)?$`)

// command adds a shell call. A command that runs apply_patch is shown as an edit.
func (s *stream) command(callID, tool string, cmd []string, why string, at time.Time) {
	text := strings.Join(cmd, " ")
	if len(cmd) >= 3 && shellWrap.MatchString(filepath.Base(cmd[0])) {
		text = cmd[len(cmd)-1] // bash -lc "<script>"
	}
	if i := strings.Index(text, "*** Begin Patch"); i >= 0 && strings.Contains(text[:i], "apply_patch") {
		s.patch(callID, "apply_patch", text[i:], at)
		return
	}
	title := firstLine(text)
	if why != "" {
		title = firstLine(why)
	}
	s.add(agent.Activity{ID: callID, Kind: agent.KindCommand, State: agent.StateRunning, Tool: tool,
		Title: title, Detail: capStr(text), At: at}, callID)
}

// patch parses the apply_patch envelope and emits one edit Activity per file.
func (s *stream) patch(callID, tool, text string, at time.Time) {
	files := agent.ParsePatch(text)
	if len(files) == 0 {
		s.add(agent.Activity{ID: callID, Kind: agent.KindEdit, State: agent.StateRunning, Tool: tool,
			Title: tool, Detail: capStr(text), At: at}, callID)
		return
	}
	for i, fp := range files {
		id := callID
		if len(files) > 1 {
			id = callID + "#" + strconv.Itoa(i)
		}
		s.add(agent.Activity{ID: id, Kind: agent.KindEdit, State: agent.StateRunning, Tool: tool,
			Title: tool + " " + relPath(s.curCwd(), fp.Path), Detail: capStr(text), File: fp.Path,
			OldText: fp.OldText, NewText: fp.NewText, Added: fp.Added, Removed: fp.Removed, At: at}, callID)
	}
}

func (s *stream) handleEvent(ev item, raw json.RawMessage, at time.Time) {
	switch ev.Type {
	case "token_count":
		var e struct {
			Info *struct {
				Total struct {
					Input  int64 `json:"input_tokens"`
					Cached int64 `json:"cached_input_tokens"`
					Output int64 `json:"output_tokens"`
					Write  int64 `json:"cache_write_input_tokens"`
				} `json:"total_token_usage"`
				Last struct {
					Input int64 `json:"input_tokens"` // includes the cached part
					Write int64 `json:"cache_write_input_tokens"`
				} `json:"last_token_usage"`
			} `json:"info"`
		}
		if json.Unmarshal(raw, &e) == nil && e.Info != nil {
			t := e.Info.Total // cumulative for the session; input includes cached
			s.usage = agent.Usage{Input: t.Input - t.Cached, Output: t.Output, CacheRead: t.Cached, CacheWrite: t.Write,
				Context: e.Info.Last.Input + e.Info.Last.Write}
		}
	case "exec_command_end":
		s.finish(ev.CallID, ev.ExitCode != nil && *ev.ExitCode != 0, ev.AggregatedOutput, at, true)
	case "patch_apply_end":
		s.finish(ev.CallID, ev.Success != nil && !*ev.Success, ev.Stdout, at, true)
	}
}

// finish completes the activities of a call; a failure is never downgraded.
// Result text from an event is replaced by the output item's text when that arrives.
func (s *stream) finish(callID string, failed bool, text string, at time.Time, fromEvent bool) {
	idx, ok := s.byCall[callID]
	if !ok {
		return
	}
	base, had := s.result[callID]
	for _, i := range idx {
		a := &s.acts[i]
		if failed || a.State == agent.StateFailed {
			a.State = agent.StateFailed
		} else {
			a.State = agent.StateDone
		}
		if a.Duration == 0 && !at.IsZero() && !a.At.IsZero() {
			a.Duration = at.Sub(a.At)
		}
		if text == "" || (had && (base < 0 || fromEvent)) {
			continue
		}
		if had {
			a.Detail = a.Detail[:base] // drop the event text
		}
		n := len(a.Detail)
		a.Detail = capStr(a.Detail + "\n\n" + text)
		if !had {
			base = n
		}
	}
	if text != "" && (!had || (base >= 0 && !fromEvent)) {
		if fromEvent {
			s.result[callID] = base
		} else {
			s.result[callID] = -1
		}
	}
}

var exitLine = regexp.MustCompile(`(?m)^Exit code: (-?\d+)`)

// outputText unwraps a function_call_output payload: a plain string, a JSON
// string {"output","metadata":{"exit_code"}}, a content-item list, or {content,success}.
func outputText(raw json.RawMessage) (text string, failed bool) {
	if len(raw) == 0 {
		return "", false
	}
	var obj struct {
		Output   string `json:"output"`
		Content  string `json:"content"`
		Success  *bool  `json:"success"`
		Metadata struct {
			ExitCode *int `json:"exit_code"`
		} `json:"metadata"`
	}
	str := ""
	switch raw[0] {
	case '"':
		_ = json.Unmarshal(raw, &str)
		var inner = obj
		if strings.HasPrefix(strings.TrimSpace(str), "{") && json.Unmarshal([]byte(str), &inner) == nil &&
			(inner.Output != "" || inner.Metadata.ExitCode != nil) {
			obj, str = inner, ""
		}
	case '{':
		_ = json.Unmarshal(raw, &obj)
	case '[':
		str = contentText(raw)
	}
	text = firstNonEmpty(str, obj.Output, obj.Content)
	if obj.Success != nil && !*obj.Success {
		failed = true
	}
	if obj.Metadata.ExitCode != nil && *obj.Metadata.ExitCode != 0 {
		failed = true
	}
	if m := exitLine.FindStringSubmatch(text); m != nil && m[1] != "0" {
		failed = true
	}
	return text, failed
}

func toStr(v any) string { s, _ := v.(string); return s }

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

func relPath(cwd, p string) string {
	if p == "" || cwd == "" || !filepath.IsAbs(p) {
		return p
	}
	rel, err := filepath.Rel(cwd, p)
	if err != nil || strings.HasPrefix(rel, "..") {
		return p
	}
	return rel
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s
}

func capStr(s string) string {
	if len(s) > detailCap {
		return s[:detailCap]
	}
	return s
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n-1]) + "..."
	}
	return s
}
