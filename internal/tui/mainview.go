package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Decsr/decsr-tui/internal/agent"
	"github.com/Decsr/decsr-tui/internal/git"
	"github.com/Decsr/decsr-tui/internal/linediff"
	"github.com/Decsr/decsr-tui/internal/ui"
)

// section is one block of the main pane: a label line plus text or a diff.
type section struct {
	label   string
	text    string
	path    string // for syntax highlighting
	diff    bool
	md      bool             // text is a finished agent message, drawn as Markdown
	running bool             // the message is still being written
	quiet   bool             // detail text in the dim agent style
	steps   []agent.Activity // activities drawn as live steps
	files   []git.FileDiff   // the diffs of the Changes tab
}

// mainKind says how the main pane draws what it shows.
type mainKind int

const (
	mkSections mainKind = iota // sections, scrolled by the pane
	mkLive                     // the live view of the watched session
	mkSession                  // the selected session of the Agent list
	mkChanges                  // the Changes tab: file list and diff
)

type mainView struct {
	ident, ver string // what the loaded sections show
	pendIdent  string // what an in-flight load will show
	pendVer    string
	seq        int
	title      string
	kind       mainKind
	secs       []section
	scroll     int

	lines    []string // rendered sections, cached per width and theme
	lineW    int
	lineDark bool

	stg *staging
}

func (v *mainView) apply(ident, ver string, secs []section) {
	if ident != v.ident {
		v.scroll = 0
	}
	v.ident, v.ver = ident, ver
	v.setSections(secs)
}

func (v *mainView) setSections(secs []section) {
	if !reflect.DeepEqual(v.secs, secs) {
		v.secs, v.lines = secs, nil
	}
}

// render returns all lines of the sections at width w. Sections with live
// steps are drawn again every time (the spinner moves); the rest is cached.
func (v *mainView) render(w int, spin string) []string {
	dark := ui.IsDark()
	if v.lines != nil && v.lineW == w && v.lineDark == dark {
		return v.lines
	}
	var out []string
	live := false
	for i, s := range v.secs {
		if i > 0 {
			out = append(out, "")
		}
		if s.label != "" {
			out = append(out, fit(sBold.Render(s.label), w))
		}
		switch {
		case s.diff:
			out = append(out, diffLines(s.text, s.path, w, s.label != "")...)
		case s.md:
			out = append(out, activityMessageLines(s.text, w, 0, s.running)...)
		case s.quiet:
			out = append(out, activityDetailLines(s.text, w, 0)...)
		case len(s.steps) > 0:
			live = true
			out = append(out, trimBlank(liveSteps(s.steps, w, stepRows(s.steps, w), spin, time.Now()))...)
		default:
			out = append(out, textLines(s.text, w)...)
		}
	}
	for i := range out {
		out[i] = ansi.Truncate(out[i], w, "")
	}
	if !live {
		v.lines, v.lineW, v.lineDark = out, w, dark
	}
	return out
}

// stepRows is an upper bound of the rows liveSteps needs for acts at w cells.
func stepRows(acts []agent.Activity, w int) int {
	n := 0
	for _, it := range liveTimeline(acts) {
		n += 1 + strings.Count(it.text, "\n") + len(it.text)/max(w, 1)
	}
	return n + 1
}

func trimBlank(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(ansi.Strip(lines[len(lines)-1])) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// diffLines draws unified diff text with decsr's diff view: syntax colours
// from chroma, a line-number gutter and tinted rows. path names the file of
// a diff that has no "diff --git" header (an agent edit). A file gets a rule
// with its path and counts unless quiet is set and it is the only one.
func diffLines(text, path string, w int, quiet bool) []string {
	if !strings.HasPrefix(text, "diff --git ") && path != "" {
		slash := filepath.ToSlash(path)
		text = "diff --git a/" + slash + " b/" + slash + "\n" + text
	}
	fds := git.ParseDiff(text)
	if len(fds) == 0 {
		return textLines(text, w)
	}
	var out []string
	for i, fd := range fds {
		if path != "" && len(fds) == 1 {
			fd.Path = filepath.ToSlash(path)
		}
		if !(quiet && len(fds) == 1) {
			if i > 0 {
				out = append(out, "")
			}
			out = append(out, fileRule(fd, w))
		}
		out = append(out, newDiffView(fd).renderAll(w)...)
	}
	return out
}

// fileRule is "── path ─────── +a −r" in w cells.
func fileRule(fd git.FileDiff, w int) string {
	a, r := fileCounts(fd)
	counts := ""
	if !fd.Binary {
		counts = " " + diffCounts(a, r) + " "
	}
	name := " " + tailPath(fd.Path, max(w-lenW(counts)-6, 4)) + " "
	rule := lipgloss.NewStyle().Foreground(ui.Border)
	fill := max(w-2-lenW(name)-lenW(counts), 0)
	return rule.Render("──") + sText.Render(name) + rule.Render(strings.Repeat("─", fill)) + counts
}

func (m *Model) scrollMain(k string) {
	h := max(m.mainRect().h-2, 1)
	switch k {
	case "ctrl+d":
		m.main.scroll += h / 2
	case "ctrl+u":
		m.main.scroll -= h / 2
	case "pgdown":
		m.main.scroll += h
	case "pgup":
		m.main.scroll -= h
	}
	m.clampScroll()
}

func (m *Model) clampScroll() {
	if m.main.kind != mkSections {
		return
	}
	r := m.mainRect()
	n := len(m.main.render(max(r.w-4, 1), m.spinFrame()))
	m.main.scroll = max(0, min(m.main.scroll, n-max(r.h-2, 1)))
}

// scrollKey handles keys while the main pane has focus.
func (m *Model) scrollKey(k string) tea.Cmd {
	a := &m.agent
	if m.main.kind == mkChanges && a.chView != nil {
		switch k {
		case "j", "down":
			a.chView.cur++
		case "k", "up":
			a.chView.cur--
		case "G", "end":
			a.chView.cur = 1 << 30
		case "home":
			a.chView.cur = 0
		case "h", "left":
			a.chView.hx = max(a.chView.hx-8, 0)
		case "l", "right":
			a.chView.hx += 8
		case "[", "]":
			m.stepChanges(map[string]int{"[": -1, "]": 1}[k])
		case "esc", "enter":
			m.mainFocus = false
		}
		return nil
	}
	switch k {
	case "j", "down":
		m.main.scroll++
	case "k", "up":
		m.main.scroll--
	case "G", "end":
		m.main.scroll = 1 << 30
	case "home":
		m.main.scroll = 0
	case "esc", "enter":
		m.mainFocus = false
	}
	m.clampScroll()
	return nil
}

// want describes what the main pane should show for the current selection.
type want struct {
	ident, ver string
	title      string
	kind       mainKind
	secs       []section // set when the content needs no git call
	load       func() []section
}

// syncMain makes the main pane follow the selection; it returns a load
// command when the content needs git.
func (m *Model) syncMain() tea.Cmd {
	if m.modal() {
		return nil
	}
	wt := m.want()
	m.main.title = wt.title
	m.main.kind = wt.kind
	if wt.ident == "" {
		m.main.apply("", "", nil)
		return nil
	}
	if wt.load == nil { // no git call: sections, or a view drawn from the model
		m.main.apply(wt.ident, wt.ver, wt.secs)
		return nil
	}
	if wt.ident == m.main.ident && wt.ver == m.main.ver {
		return nil
	}
	if wt.ident == m.main.pendIdent && wt.ver == m.main.pendVer {
		return nil
	}
	m.main.pendIdent, m.main.pendVer = wt.ident, wt.ver
	m.main.seq++
	seq, load, ident, ver := m.main.seq, wt.load, wt.ident, wt.ver
	return func() tea.Msg { return mainMsg{seq, ident, ver, load()} }
}

func (m *Model) want() want {
	switch m.focus {
	case pFiles:
		return m.wantFile()
	case pBranches:
		switch m.branches.tab {
		case tabTags:
			return m.wantTag()
		case tabWorktrees:
			return m.wantWorktree()
		}
		if b, ok := m.selBranch(); ok {
			name := b.Name
			return want{ident: "branch:" + name, ver: fmt.Sprint(m.gen), title: "Log " + name, load: func() []section {
				cs, err := m.repo.Log(name, 200)
				if err != nil {
					return []section{{text: err.Error()}}
				}
				var sb strings.Builder
				for _, c := range cs {
					fmt.Fprintf(&sb, "%s %s (%s, %s)\n", c.Short, c.Subject, c.Author, ago(c.Date))
				}
				return []section{{text: sb.String()}}
			}}
		}
	case pCommits:
		return m.wantCommit()
	case pStash:
		if s, ok := m.selStash(); ok {
			idx := s.Index
			return want{ident: "stash:" + s.Subject, ver: fmt.Sprint(m.gen), title: "Stash " + s.Ref, load: func() []section {
				d, err := m.repo.StashDiff(idx)
				if err != nil {
					return []section{{text: err.Error()}}
				}
				return []section{{diff: true, text: d}}
			}}
		}
	case pAgent:
		return m.wantAgent()
	}
	return want{}
}

// wantFile shows the unstaged and the staged diff of the selected file.
func (m *Model) wantFile() want {
	f, path, isDir, ok := m.selFile()
	if !ok {
		return want{}
	}
	if isDir {
		return want{ident: "dir:" + path, title: "Directory", secs: []section{{text: path}}}
	}
	ver := fmt.Sprintf("%d:%c%c:%s", m.gen, f.Staged, f.Unstaged, statSig(filepath.Join(m.repo.Dir, filepath.FromSlash(path))))
	title := "Diff " + path
	if f.Conflicted {
		title = "Conflict " + path
	}
	return want{ident: "file:" + path, ver: ver, title: title, load: func() []section {
		var secs []section
		if f.Conflicted {
			d, _ := m.repo.DiffFile(path, false)
			return []section{{label: "Conflicted (markers shown)", diff: true, path: path, text: d}}
		}
		if d, _ := m.repo.DiffFile(path, false); strings.TrimSpace(d) != "" {
			secs = append(secs, section{label: "Unstaged changes", diff: true, path: path, text: d})
		}
		if d, _ := m.repo.DiffFile(path, true); strings.TrimSpace(d) != "" {
			secs = append(secs, section{label: "Staged changes", diff: true, path: path, text: d})
		}
		if secs == nil {
			secs = []section{{text: "no changes"}}
		}
		return secs
	}}
}

func statSig(p string) string {
	st, err := os.Stat(p)
	if err != nil {
		return "-"
	}
	return fmt.Sprintf("%d:%d", st.Size(), st.ModTime().UnixNano())
}

func (m *Model) wantCommit() want {
	if m.commits.graph && m.commits.sub == nil {
		return want{ident: "graph", ver: fmt.Sprint(m.gen), title: "Graph", load: func() []section {
			ls, err := m.repo.GraphLog(300)
			if err != nil {
				return []section{{text: err.Error()}}
			}
			return []section{{text: strings.Join(ls, "\n")}}
		}}
	}
	if sub := m.commits.sub; sub != nil {
		hash := sub.hash
		path := ""
		if f, ok := m.selCommitFile(); ok {
			path = f.Path
		}
		return want{ident: "cfile:" + hash + ":" + path, ver: fmt.Sprint(m.gen), title: "Diff " + path, load: func() []section {
			d, err := m.repo.DiffCommit(hash, path)
			if err != nil {
				return []section{{text: err.Error()}}
			}
			return []section{{diff: true, path: path, text: d}}
		}}
	}
	c, ok := m.selCommit()
	if !ok {
		return want{}
	}
	return want{ident: "commit:" + c.Hash, ver: fmt.Sprint(m.gen), title: "Commit " + c.Short, load: func() []section { return m.commitSections(c) }}
}

// commitSections is the header (with the branches containing the commit) and the diff of c.
func (m *Model) commitSections(c git.Commit) []section {
	d, err := m.repo.DiffCommit(c.Hash, "")
	if err != nil {
		return []section{{text: err.Error()}}
	}
	head := fmt.Sprintf("%s\n%s <%s> %s", c.Subject, c.Author, c.Email, ago(c.Date))
	if bs, err := m.repo.CommitBranches(c.Hash); err == nil && len(bs) > 0 {
		head += "\nBranches: " + strings.Join(bs, ", ")
	}
	return []section{{text: head}, {diff: true, text: d}}
}

// wantTag shows the commit a tag points at.
func (m *Model) wantTag() want {
	t, ok := m.selTag()
	if !ok {
		return want{}
	}
	return want{ident: "tag:" + t.Name, ver: fmt.Sprint(m.gen), title: "Tag " + t.Name, load: func() []section {
		cs, err := m.repo.Log(t.Hash, 1)
		if err != nil || len(cs) == 0 {
			return []section{{text: "cannot read the tagged commit"}}
		}
		return m.commitSections(cs[0])
	}}
}

func (m *Model) wantWorktree() want {
	w, ok := m.selWorktree()
	if !ok {
		return want{}
	}
	txt := fmt.Sprintf("%s\n\nbranch  %s\nhead    %s", w.Path, w.Branch, w.Head)
	return want{ident: "worktree:" + w.Path, title: "Worktree", secs: []section{{text: txt}}}
}

// / wantAgent shows the selected session, the live view of the watched session,
// the selected activity, or the changes of the session.
func (m *Model) wantAgent() want {
	a := &m.agent
	if !a.open {
		s, ok := a.selSession()
		if !ok {
			return want{}
		}
		return want{ident: "session:" + s.ID, title: "Session", kind: mkSession}
	}
	if a.changes {
		return m.wantChanges()
	}
	if a.follow {
		return want{ident: "live:" + a.cur, title: "Live " + a.snap.Session.Runner, kind: mkLive}
	}
	act, ok := a.selActivity()
	if !ok {
		return want{}
	}
	return want{ident: "act:" + act.ID, title: string(act.Kind), secs: activitySections(act, a.snap)}
}

func activitySections(act agent.Activity, snap agent.Snapshot) []section {
	head := oneLine(sanitizeText(act.Title))
	if act.Duration > 0 {
		head += "  (" + actDuration(act.Duration) + ")"
	}
	switch act.Kind {
	case agent.KindEdit, agent.KindWrite:
		d := linediff.Unified(act.OldText, act.NewText, 3)
		if d == "" {
			d = "(no textual change)"
		}
		return []section{{label: fmt.Sprintf("%s  +%d -%d", act.Title, act.Added, act.Removed), diff: true, path: act.File, text: d}}
	case agent.KindSubagent:
		for _, s := range snap.Subagents {
			if s.ID == act.SubagentID {
				sum := fmt.Sprintf("%s subagent, %s, %s\n%s", s.Type, subState(s), plural(len(s.Activities), "activity", "activities"), s.Description)
				return []section{{label: head, text: sum}, {label: "Steps", steps: s.Activities}}
			}
		}
	case agent.KindMessage:
		return []section{{label: head, text: act.Detail, md: true, running: act.State == agent.StateRunning}}
	}
	return []section{{label: head, text: act.Detail, quiet: true}}
}

func subState(s agent.Subagent) string {
	if s.Finished {
		return "finished"
	}
	return "running"
}

// wantChanges shows `git diff HEAD` of every file the session touched.
func (m *Model) wantChanges() want {
	paths := m.agent.touchedFiles(m.repo.Dir)
	var sig []string
	for _, p := range paths {
		sig = append(sig, p+"="+statSig(filepath.Join(m.repo.Dir, filepath.FromSlash(p))))
	}
	ver := fmt.Sprint(m.gen) + "|" + strings.Join(sig, ",")
	return want{ident: "changes", ver: ver, title: "Changes", kind: mkChanges, load: func() []section {
		var files []git.FileDiff
		for _, p := range paths {
			d, _ := m.repo.DiffHead(p)
			if strings.TrimSpace(d) == "" {
				continue
			}
			for _, fd := range git.ParseDiff(d) {
				files = append(files, fd)
			}
		}
		return []section{{files: files}}
	}}
}

// changeFiles are the diffs shown in the Changes tab.
func (m *Model) changeFiles() []git.FileDiff {
	if m.main.ident != "changes" || len(m.main.secs) == 0 {
		return nil
	}
	return m.main.secs[0].files
}

// syncChanges keeps the selected file and its diff view in step with the files.
func (m *Model) syncChanges() {
	a, files := &m.agent, m.changeFiles()
	if len(files) == 0 {
		a.chSel, a.chView = 0, nil
		return
	}
	a.chSel = max(min(a.chSel, len(files)-1), 0)
	a.chView = mergeOutDiff(a.chView, files[a.chSel], nil)
}

// stepChanges selects the next (+1) or previous (-1) file of the Changes tab.
func (m *Model) stepChanges(d int) {
	a, files := &m.agent, m.changeFiles()
	if len(files) == 0 {
		return
	}
	sel := max(min(a.chSel+d, len(files)-1), 0)
	if sel != a.chSel {
		a.chSel, a.chView = sel, nil
	}
	m.syncChanges()
}

// ---- drawing ----

// mainLines draws the main pane for an inner area of w x h cells. The
// returned scroll is the first visible row of a scrolling kind (the caller
// windows it), -1 when the pane drew exactly what fits.
func (m *Model) mainLines(w, h int) (lines []string, scrolls bool) {
	if m.main.stg != nil {
		return m.stagingLines(w, h), false
	}
	if m.todo != nil {
		return m.todoLines(w, h), false
	}
	switch m.main.kind {
	case mkLive:
		a := &m.agent
		return m.liveLines(a.snap, a.snap.Session, w, h), false
	case mkSession:
		if s, ok := m.agent.selSession(); ok {
			if s.ID == m.agent.cur && m.agent.loaded {
				return m.liveLines(m.agent.snap, s, w, h), false
			}
			return m.sessionMeta(s, w), false
		}
		return nil, false
	case mkChanges:
		if m.main.ident != "changes" {
			return []string{"  " + sDim.Render("loading…")}, false
		}
		a := &m.agent
		return outChangesPane(m.changeFiles(), a.chSel, a.chView, a.snap.Session.Live, w, h, m.mainFocus), false
	}
	if len(m.main.secs) == 0 {
		return m.emptyMain(w, h), false
	}
	return m.main.render(w, m.spinFrame()), true
}

// emptyMain is the pane when nothing is selected: the decsr logo and tagline.
func (m *Model) emptyMain(w, h int) []string {
	var lines []string
	if w >= ui.LogoWidth+2 && h >= 5 {
		for _, l := range ui.Logo(false) {
			lines = append(lines, centerLine(l, w))
		}
		lines = append(lines, "", centerLine(sDim.Render(ui.Tagline), w))
	} else {
		lines = []string{sDim.Render(ansi.Truncate(ui.Tagline, w, ""))}
	}
	return append(make([]string, max((h-len(lines))/2, 0)), lines...)
}

func centerLine(s string, w int) string {
	if pad := (w - lenW(s)) / 2; pad > 0 {
		return strings.Repeat(" ", pad) + s
	}
	return s
}

// liveRunOf is what the running line shows about a session and its snapshot.
func liveRunOf(s agent.Session, snap agent.Snapshot) liveRun {
	st := agent.StateDone
	if s.Live {
		st = agent.StateRunning
	}
	r := liveRun{State: st, Runner: s.Runner, Model: s.Model, Started: s.Started, Usage: snap.Usage}
	if !s.Live {
		r.Ended = s.Updated
	}
	return r
}

// liveLines is the live view of a session: the running line, the newest
// steps and text, and the files it changed.
func (m *Model) liveLines(snap agent.Snapshot, s agent.Session, w, h int) []string {
	now := time.Now()
	spin := m.spinFrame()
	out := []string{ansi.Truncate(runningLine(liveRunOf(s, snap), spin, now), w, "…"), ""}
	files := liveFiles(snap.Activities)
	for _, sub := range snap.Subagents {
		files = append(files, liveFiles(sub.Activities)...)
	}
	fileRows := 0
	if len(files) > 0 && h >= 12 {
		fileRows = min(len(files), 4) + 2
	}
	steps := max(h-len(out)-fileRows, 1)
	out = append(out, liveSteps(snap.Activities, w, steps, spin, now)...)
	if fileRows > 0 {
		out = append(out[:min(len(out), h-fileRows)], ui.PaneLabel("Changed files", false, w))
		out = append(out, liveFileRows(files, w, fileRows-1)...)
	}
	return out
}

// sessionMeta describes a session that is not the one being watched.
func (m *Model) sessionMeta(s agent.Session, w int) []string {
	runner := s.Runner
	if runner == "" {
		runner = m.src.Name()
	}
	r := liveRunOf(s, agent.Snapshot{})
	r.Runner = runner
	out := []string{ansi.Truncate(runningLine(r, m.spinFrame(), time.Now()), w, "…"), "", sBold.Render(ansi.Truncate(oneLine(sanitizeText(s.Title)), w, "…")), ""}
	kv := func(k, v string) {
		if v != "" {
			out = append(out, ansi.Truncate(sDim.Render(fmt.Sprintf("%-8s", k))+sText.Render(v), w, "…"))
		}
	}
	kv("id", s.ID)
	kv("branch", s.Branch)
	kv("model", s.Model)
	kv("started", s.Started.Format("2006-01-02 15:04"))
	kv("updated", ago(s.Updated))
	kv("cwd", s.Cwd)
	return append(out, "", sDim.Render("enter opens the activity tree"))
}
