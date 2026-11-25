package opencode

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Decsr/decsr-tui/internal/agent"
)

const detailCap = 64 << 10

type tracker struct {
	be    backend
	sess  agent.Session
	main  *sessData
	subs  map[string]*sessData
	order []string
	raws  map[string]rawSession

	dirty bool
	built *built
	live  bool
}

func (t *tracker) Poll() (bool, error) {
	changed, err := t.be.load(t.main)
	if err != nil {
		return changed, err
	}
	if kids, err := t.be.children(t.sess.ID); err == nil {
		sort.Slice(kids, func(i, j int) bool { return kids[i].created.Before(kids[j].created) })
		if t.raws == nil {
			t.raws = map[string]rawSession{}
		}
		for _, k := range kids {
			sd, ok := t.subs[k.id]
			if !ok {
				sd = newSessData(k.id)
				t.subs[k.id] = sd
				t.order = append(t.order, k.id)
				changed = true
			}
			if old := t.raws[k.id]; old != k {
				t.raws[k.id] = k
				changed = true
			}
			if c, _ := t.be.load(sd); c {
				changed = true
			}
		}
	}
	if live := time.Since(t.main.last) < liveWindow; live != t.live {
		t.live, changed = live, true
	}
	if changed || t.built == nil {
		t.dirty = true
	}
	return changed, nil
}

// built is the activity model derived from the raw rows.
type built struct {
	acts     []agent.Activity
	usage    agent.Usage
	model    string
	cwd      string
	prompt   string
	taskLink []taskLink
}

type taskLink struct {
	callID, childID, typ, desc string
	state                      agent.State
}

func (t *tracker) Snapshot() agent.Snapshot {
	if t.dirty || t.built == nil {
		b := build(t.main, t.sess.Cwd)
		t.built, t.dirty = &b, false
	}
	b := t.built
	ss := t.sess
	ss.Runner = "opencode"
	if b.model != "" {
		ss.Model = b.model
	}
	if ss.Cwd == "" {
		ss.Cwd = b.cwd
	}
	if (ss.Title == "" || strings.HasPrefix(ss.Title, "New session")) && b.prompt != "" {
		ss.Title = truncate(firstLine(b.prompt), 80)
	}
	if !t.main.last.IsZero() && t.main.last.After(ss.Updated) {
		ss.Updated = t.main.last
	}
	ss.Live = t.live

	acts := append([]agent.Activity(nil), b.acts...)
	snap := agent.Snapshot{Session: ss, Activities: acts, Usage: b.usage}
	links := map[string]taskLink{}
	for _, l := range b.taskLink {
		if l.childID != "" {
			links[l.childID] = l
		}
	}
	for _, id := range t.order {
		sd, raw := t.subs[id], t.raws[id]
		cb := build(sd, ss.Cwd)
		l, linked := links[id]
		sub := agent.Subagent{
			ID: id, ToolUseID: l.callID, Type: l.typ, Description: l.desc, Model: cb.model, Path: raw.path,
			Activities: cb.acts, Usage: cb.usage,
		}
		if sub.Description == "" {
			sub.Description = raw.title
		}
		last := sd.last
		if raw.updated.After(last) {
			last = raw.updated
		}
		if linked {
			sub.Finished = l.state != agent.StateRunning
		} else {
			sub.Finished = time.Since(last) >= liveWindow
		}
		sub.Live = !sub.Finished && time.Since(last) < liveWindow
		snap.Subagents = append(snap.Subagents, sub)
	}
	return snap
}

// ---- raw JSON shapes ----

// modelRef is the model of a message: an object {providerID, modelID} or the
// string "providerID/modelID".
type modelRef struct{ ProviderID, ModelID string }

func (r *modelRef) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		if i := strings.Index(s, "/"); i >= 0 {
			r.ProviderID, r.ModelID = s[:i], s[i+1:]
		} else {
			r.ModelID = s
		}
		return nil
	}
	var o struct{ ProviderID, ModelID string }
	if json.Unmarshal(b, &o) == nil {
		r.ProviderID, r.ModelID = o.ProviderID, o.ModelID
	}
	return nil // a shape we do not know is no model, never a failed message
}

type message struct {
	Role    string   `json:"role"`
	ModelID string   `json:"modelID"`
	Model   modelRef `json:"model"`
	Path    struct {
		Cwd string `json:"cwd"`
	} `json:"path"`
	Tokens *struct {
		Input     int64 `json:"input"`
		Output    int64 `json:"output"`
		Reasoning int64 `json:"reasoning"`
		Cache     struct {
			Read  int64 `json:"read"`
			Write int64 `json:"write"`
		} `json:"cache"`
	} `json:"tokens"`
}

type part struct {
	Type      string `json:"type"`
	Text      string `json:"text"`
	Synthetic bool   `json:"synthetic"`
	Ignored   bool   `json:"ignored"`
	Tool      string `json:"tool"`
	CallID    string `json:"callID"`
	Time      struct {
		Start int64 `json:"start"`
	} `json:"time"`
	State struct {
		Status   string          `json:"status"`
		Input    map[string]any  `json:"input"`
		Output   json.RawMessage `json:"output"`
		Error    string          `json:"error"`
		Title    string          `json:"title"`
		Metadata map[string]any  `json:"metadata"`
		Time     struct {
			Start int64 `json:"start"`
			End   int64 `json:"end"`
		} `json:"time"`
	} `json:"state"`
}

func build(d *sessData, cwdHint string) built {
	var b built
	byMsg := map[string][]*partRec{}
	for _, p := range d.parts {
		byMsg[p.msgID] = append(byMsg[p.msgID], p)
	}
	for _, mr := range d.sortedMsgs() {
		var m message
		if json.Unmarshal(mr.data, &m) != nil {
			continue
		}
		if m.Role == "assistant" {
			if mn := m.modelName(); mn != "" {
				b.model = mn
			}
			if m.Path.Cwd != "" && b.cwd == "" {
				b.cwd = m.Path.Cwd
			}
			if tk := m.Tokens; tk != nil {
				b.usage.Input += tk.Input
				b.usage.Output += tk.Output + tk.Reasoning
				b.usage.CacheRead += tk.Cache.Read
				b.usage.CacheWrite += tk.Cache.Write
				if c := tk.Input + tk.Cache.Read + tk.Cache.Write; c > 0 {
					b.usage.Context = c // messages are oldest first: the last one wins
				}
			}
		} else if m.Role == "user" && m.Model.ModelID != "" && b.model == "" {
			b.model = m.Model.ModelID
		}
		parts := byMsg[mr.id]
		sort.Slice(parts, func(i, j int) bool { return parts[i].id < parts[j].id })
		cwd := firstNonEmpty(cwdHint, b.cwd)
		for _, pr := range parts {
			var p part
			if json.Unmarshal(pr.data, &p) != nil {
				continue
			}
			at := ms(firstPositive(p.Time.Start, p.State.Time.Start, mr.created))
			switch p.Type {
			case "text":
				if strings.TrimSpace(p.Text) == "" || p.Synthetic || p.Ignored {
					continue
				}
				k := agent.KindMessage
				if m.Role == "user" {
					k = agent.KindUser
					if b.prompt == "" {
						b.prompt = p.Text
					}
				}
				b.acts = append(b.acts, agent.Activity{ID: pr.id, Kind: k, State: agent.StateDone,
					Title: firstLine(p.Text), Detail: capStr(p.Text), At: at})
			case "reasoning":
				if strings.TrimSpace(p.Text) != "" {
					b.acts = append(b.acts, agent.Activity{ID: pr.id, Kind: agent.KindThinking, State: agent.StateDone,
						Title: firstLine(p.Text), Detail: capStr(p.Text), At: at})
				}
			case "tool":
				b.addTool(pr.id, p, cwd, at)
			}
		}
	}
	return b
}

func (b *built) addTool(partID string, p part, cwd string, at time.Time) {
	in := p.State.Input
	str := func(k string) string { v, _ := in[k].(string); return v }
	id := firstNonEmpty(p.CallID, partID)
	a := agent.Activity{ID: id, Tool: p.Tool, State: agent.StateRunning, At: at, Kind: agent.KindTool}
	switch p.State.Status {
	case "completed":
		a.State = agent.StateDone
	case "error":
		a.State = agent.StateFailed
	}
	if st, en := p.State.Time.Start, p.State.Time.End; st > 0 && en >= st {
		a.Duration = time.Duration(en-st) * time.Millisecond
	}
	out := outputString(p.State.Output)
	if p.State.Error != "" {
		out = p.State.Error
	}
	inJSON, _ := json.Marshal(in)
	a.Detail = string(inJSON)
	file := firstNonEmpty(str("filePath"), str("path"))

	switch strings.ToLower(p.Tool) {
	case "bash", "shell":
		a.Kind = agent.KindCommand
		a.Title = firstNonEmpty(firstLine(str("description")), firstLine(str("command")))
		a.Detail = str("command")
		if code, ok := numeric(p.State.Metadata["exit"]); ok && code != 0 && a.State == agent.StateDone {
			a.State = agent.StateFailed
		}
	case "read":
		a.Kind, a.File = agent.KindRead, file
		a.Title = p.Tool + " " + relPath(cwd, file)
	case "edit", "multiedit":
		a.Kind, a.File = agent.KindEdit, file
		a.OldText, a.NewText = str("oldString"), str("newString")
		if edits, ok := in["edits"].([]any); ok {
			for _, e := range edits {
				m, _ := e.(map[string]any)
				o, _ := m["oldString"].(string)
				n, _ := m["newString"].(string)
				a.OldText += o
				a.NewText += n
			}
		}
		a.Removed, a.Added = countLines(a.OldText), countLines(a.NewText)
		a.Title = p.Tool + " " + relPath(cwd, file)
	case "write":
		a.Kind, a.File, a.NewText = agent.KindWrite, file, str("content")
		a.Added = countLines(a.NewText)
		a.Title = p.Tool + " " + relPath(cwd, file)
	case "apply_patch", "patch":
		patchText := firstNonEmpty(str("patchText"), str("patch"))
		a.Kind = agent.KindEdit
		files := agent.ParsePatch(patchText)
		if len(files) == 0 {
			a.Title, a.Detail = p.Tool, capStr(patchText+"\n\n"+out)
			b.acts = append(b.acts, a)
			return
		}
		for i, fp := range files {
			c := a
			if len(files) > 1 {
				c.ID = id + "#" + strconv.Itoa(i)
			}
			c.File, c.OldText, c.NewText, c.Added, c.Removed = fp.Path, fp.OldText, fp.NewText, fp.Added, fp.Removed
			c.Title = p.Tool + " " + relPath(cwd, fp.Path)
			c.Detail = capStr(patchText + "\n\n" + out)
			b.acts = append(b.acts, c)
		}
		return
	case "grep", "glob", "list", "ls", "webfetch", "websearch", "codesearch":
		a.Kind = agent.KindSearch
		a.Title = strings.TrimSpace(p.Tool + " " + firstNonEmpty(str("pattern"), str("query"), str("url"), str("path")))
	case "task", "subagent":
		a.Kind = agent.KindSubagent
		typ := firstNonEmpty(str("subagent_type"), p.Tool)
		a.Title = typ + ": " + firstLine(str("description"))
		var child string
		for _, k := range []string{"sessionId", "sessionID"} {
			if v, _ := p.State.Metadata[k].(string); v != "" {
				child = v
			}
		}
		a.SubagentID = child
		b.taskLink = append(b.taskLink, taskLink{callID: id, childID: child, typ: typ, desc: str("description"), state: a.State})
	default:
		a.Title = firstNonEmpty(p.State.Title, p.Tool)
	}
	a.Detail = capStr(strings.TrimSpace(a.Detail + "\n\n" + out))
	b.acts = append(b.acts, a)
}

func outputString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(raw)
}

func numeric(v any) (int, bool) {
	f, ok := v.(float64)
	return int(f), ok
}

func firstPositive(vs ...int64) int64 {
	for _, v := range vs {
		if v > 0 {
			return v
		}
	}
	return 0
}

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

// modelName is the model a message names: its modelID, else the model object
// as "providerID/modelID", else the model string.
func (m message) modelName() string {
	switch {
	case m.ModelID != "":
		return m.ModelID
	case m.Model.ModelID != "" && m.Model.ProviderID != "":
		return m.Model.ProviderID + "/" + m.Model.ModelID
	}
	return m.Model.ModelID
}

// assistantModel is the model of an assistant message's data, "" for any other message.
func assistantModel(data []byte) string {
	var m message
	if json.Unmarshal(data, &m) != nil || m.Role != "assistant" {
		return ""
	}
	return m.modelName()
}
