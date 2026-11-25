package claude

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

type record struct {
	Type      string `json:"type"`
	UUID      string `json:"uuid"`
	Timestamp string `json:"timestamp"`
	Cwd       string `json:"cwd"`
	GitBranch string `json:"gitBranch"`
	IsMeta    bool   `json:"isMeta"`
	AITitle   string `json:"aiTitle"`
	Message   struct {
		ID      string          `json:"id"`
		Model   string          `json:"model"`
		Content json.RawMessage `json:"content"`
		Usage   *struct {
			Input      int64 `json:"input_tokens"`
			Output     int64 `json:"output_tokens"`
			CacheRead  int64 `json:"cache_read_input_tokens"`
			CacheWrite int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

type block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

// stream incrementally parses one JSONL transcript file.
type stream struct {
	path   string
	offset int64
	cwd    string // fallback cwd for relative paths

	acts    []agent.Activity
	byTool  map[string]int // tool_use id -> index in acts
	seenMsg map[string]bool
	usage   agent.Usage

	title, firstPrompt, sessCwd, branch, model string
	started                                    time.Time
	mtime                                      time.Time
}

func newStream(path, cwd string) *stream {
	return &stream{path: path, cwd: cwd, byTool: map[string]int{}, seenMsg: map[string]bool{}}
}

// poll reads new complete lines; reports whether anything changed.
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
	if st.Size() < s.offset { // truncated: start over
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
		return changed, nil // only a partial line so far
	}
	s.offset += int64(end + 1)
	for _, line := range strings.Split(text[:end], "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			s.handle(line)
		}
	}
	return true, nil
}

func (s *stream) handle(line string) {
	var r record
	if json.Unmarshal([]byte(line), &r) != nil {
		return
	}
	at, _ := time.Parse(time.RFC3339Nano, r.Timestamp)
	if r.Cwd != "" && s.sessCwd == "" {
		s.sessCwd = r.Cwd
	}
	if r.GitBranch != "" {
		s.branch = r.GitBranch
	}
	if !at.IsZero() && (s.started.IsZero() || at.Before(s.started)) {
		s.started = at
	}
	switch r.Type {
	case "ai-title":
		if r.AITitle != "" {
			s.title = r.AITitle
		}
	case "user":
		if !r.IsMeta {
			s.handleUser(r, at)
		}
	case "assistant":
		if !r.IsMeta {
			s.handleAssistant(r, at)
		}
	}
}

func parseBlocks(raw json.RawMessage) (text string, blocks []block, isString bool) {
	if len(raw) == 0 {
		return "", nil, false
	}
	if raw[0] == '"' {
		_ = json.Unmarshal(raw, &text)
		return text, nil, true
	}
	_ = json.Unmarshal(raw, &blocks)
	return "", blocks, false
}

func (s *stream) handleUser(r record, at time.Time) {
	text, blocks, isStr := parseBlocks(r.Message.Content)
	if isStr {
		s.addPrompt(r.UUID, 0, text, at)
		return
	}
	for i, b := range blocks {
		switch b.Type {
		case "tool_result":
			s.finishTool(b, at)
		case "text":
			s.addPrompt(r.UUID, i, b.Text, at)
		}
	}
}

var (
	dropTags   = regexp.MustCompile(`(?s)<(system-reminder|local-command-caveat|command-message)>.*?</(system-reminder|local-command-caveat|command-message)>`)
	keepTags   = regexp.MustCompile(`(?s)<(command-name|local-command-stdout)>(.*?)</(command-name|local-command-stdout)>`)
	argsTag    = regexp.MustCompile(`(?s)<command-args>(.*?)</command-args>`)
	danglerTag = regexp.MustCompile(`(?s)<(system-reminder|local-command-caveat)>.*$`)
)

// cleanPrompt strips the harness tags Claude Code wraps around user prompts.
func cleanPrompt(text string) string {
	text = dropTags.ReplaceAllString(text, "")
	text = danglerTag.ReplaceAllString(text, "")
	text = keepTags.ReplaceAllString(text, "$2")
	text = argsTag.ReplaceAllString(text, " $1")
	return strings.TrimSpace(text)
}

func (s *stream) addPrompt(uuid string, i int, text string, at time.Time) {
	text = cleanPrompt(text)
	if text == "" {
		return
	}
	if s.firstPrompt == "" {
		s.firstPrompt = text
	}
	s.acts = append(s.acts, agent.Activity{
		ID: uuid + "#" + strconv.Itoa(i), Kind: agent.KindUser, State: agent.StateDone,
		Title: firstLine(text), Detail: capStr(text), At: at,
	})
}

func (s *stream) handleAssistant(r record, at time.Time) {
	if r.Message.Model != "" {
		s.model = r.Message.Model
	}
	if u := r.Message.Usage; u != nil {
		s.usage.Context = u.Input + u.CacheRead + u.CacheWrite
	}
	if u := r.Message.Usage; u != nil && (r.Message.ID == "" || !s.seenMsg[r.Message.ID]) {
		s.usage.Input += u.Input
		s.usage.Output += u.Output
		s.usage.CacheRead += u.CacheRead
		s.usage.CacheWrite += u.CacheWrite
		if r.Message.ID != "" {
			s.seenMsg[r.Message.ID] = true
		}
	}
	_, blocks, _ := parseBlocks(r.Message.Content)
	for i, b := range blocks {
		id := r.UUID + "#" + strconv.Itoa(i)
		switch b.Type {
		case "text":
			if strings.TrimSpace(b.Text) != "" {
				s.acts = append(s.acts, agent.Activity{ID: id, Kind: agent.KindMessage, State: agent.StateDone,
					Title: firstLine(b.Text), Detail: capStr(b.Text), At: at})
			}
		case "thinking":
			if strings.TrimSpace(b.Thinking) != "" {
				s.acts = append(s.acts, agent.Activity{ID: id, Kind: agent.KindThinking, State: agent.StateDone,
					Title: firstLine(b.Thinking), Detail: capStr(b.Thinking), At: at})
			}
		case "tool_use":
			if _, dup := s.byTool[b.ID]; dup {
				continue
			}
			if b.ID != "" {
				s.byTool[b.ID] = len(s.acts)
			}
			s.acts = append(s.acts, s.toolActivity(b, at))
		}
	}
}

func (s *stream) finishTool(b block, at time.Time) {
	i, ok := s.byTool[b.ToolUseID]
	if !ok {
		return
	}
	a := &s.acts[i]
	a.State = agent.StateDone
	if b.IsError {
		a.State = agent.StateFailed
	}
	if !at.IsZero() && !a.At.IsZero() {
		a.Duration = at.Sub(a.At)
	}
	if res := resultText(b.Content); res != "" {
		a.Detail = capStr(a.Detail + "\n\n" + res)
	}
}

func resultText(raw json.RawMessage) string {
	text, blocks, isStr := parseBlocks(raw)
	if isStr {
		return text
	}
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func (s *stream) toolActivity(b block, at time.Time) agent.Activity {
	var in map[string]any
	_ = json.Unmarshal(b.Input, &in)
	str := func(k string) string { v, _ := in[k].(string); return v }
	a := agent.Activity{ID: b.ID, Tool: b.Name, State: agent.StateRunning, At: at, Kind: classify(b.Name)}
	cwd := s.sessCwd
	if cwd == "" {
		cwd = s.cwd
	}
	switch a.Kind {
	case agent.KindCommand:
		cmd := str("command")
		a.Title = firstLine(str("description"))
		if a.Title == "" {
			a.Title = firstLine(cmd)
		}
		a.Detail = cmd
	case agent.KindRead, agent.KindEdit, agent.KindWrite:
		p := str("file_path")
		if p == "" {
			p = str("notebook_path")
		}
		a.File = p
		a.Title = b.Name + " " + relPath(cwd, p)
		a.Detail = string(b.Input)
		switch b.Name {
		case "Edit":
			a.OldText, a.NewText = str("old_string"), str("new_string")
		case "MultiEdit":
			edits, _ := in["edits"].([]any)
			for _, e := range edits {
				m, _ := e.(map[string]any)
				o, _ := m["old_string"].(string)
				n, _ := m["new_string"].(string)
				a.OldText += o
				a.NewText += n
			}
		case "NotebookEdit":
			a.NewText = str("new_source")
		case "Write":
			a.NewText = str("content")
		}
		if a.Kind != agent.KindRead {
			a.Removed, a.Added = countLines(a.OldText), countLines(a.NewText)
		}
	case agent.KindSearch:
		q := str("pattern")
		for _, k := range []string{"query", "url", "path"} {
			if q == "" {
				q = str(k)
			}
		}
		a.Title = strings.TrimSpace(b.Name + " " + q)
		a.Detail = string(b.Input)
	case agent.KindSubagent:
		t := str("subagent_type")
		if t == "" {
			t = b.Name
		}
		a.Title = t + ": " + firstLine(str("description"))
		a.Detail = string(b.Input)
	default:
		a.Title = b.Name
		a.Detail = string(b.Input)
	}
	a.Detail = capStr(a.Detail)
	return a
}

func classify(name string) agent.Kind {
	switch name {
	case "Bash", "PowerShell", "BashOutput":
		return agent.KindCommand
	case "Read", "NotebookRead":
		return agent.KindRead
	case "Edit", "MultiEdit", "NotebookEdit":
		return agent.KindEdit
	case "Write":
		return agent.KindWrite
	case "Grep", "Glob", "LS", "WebSearch", "WebFetch", "ToolSearch":
		return agent.KindSearch
	case "Agent", "Task":
		return agent.KindSubagent
	}
	return agent.KindTool
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

func countLines(s string) int {
	if s == "" {
		return 0
	}
	n := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		n++
	}
	return n
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
