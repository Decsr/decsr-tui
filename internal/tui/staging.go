package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Decsr/decsr-tui/internal/git"
)

// staging is the hunk/line staging view shown in the main pane. The diff, its
// cursor and its selection are a diffView.
type staging struct {
	path       string
	staged     bool // showing the staged diff instead of the unstaged one
	loaded     bool
	fd         git.FileDiff
	dv         *diffView
	lineMode   bool
	rangeStart int // row index where `v` started a range, -1 for none
}

func (s *staging) build() {
	dv := newDiffView(s.fd)
	if s.dv != nil {
		dv.cur, dv.top = s.dv.cur, s.dv.top
	}
	dv.cur = max(0, min(dv.cur, len(dv.rows)-1))
	s.dv = dv
	s.rangeStart = -1
	s.sync()
}

// rowText is the diff text of row i: the hunk header, or the kind and text of a line.
func (s *staging) rowText(i int) string {
	r := s.dv.rows[i]
	if r.line < 0 {
		return s.fd.Hunks[r.hunk].Header
	}
	l := s.fd.Hunks[r.hunk].Lines[r.line]
	return string(l.Kind) + l.Text
}

// selection returns the hunk and the selected line indexes (nil = whole hunk).
func (s *staging) selection() (int, map[int]bool) {
	if s.dv == nil || len(s.dv.rows) == 0 {
		return -1, nil
	}
	rows := s.dv.rows
	r := rows[s.dv.cur]
	if !s.lineMode || r.line < 0 {
		return r.hunk, nil
	}
	sel := map[int]bool{r.line: true}
	if s.rangeStart >= 0 && s.rangeStart < len(rows) {
		a, b := s.rangeStart, s.dv.cur
		if a > b {
			a, b = b, a
		}
		for i := a; i <= b; i++ {
			if rows[i].hunk == r.hunk && rows[i].line >= 0 {
				sel[rows[i].line] = true
			}
		}
	}
	return r.hunk, sel
}

// sync marks the selection in the diff view.
func (s *staging) sync() {
	hunk, sel := s.selection()
	s.dv.selOn, s.dv.selHunk, s.dv.selLines = hunk >= 0, hunk, sel
}

func (m *Model) openStaging() tea.Cmd {
	_, path, isDir, ok := m.selFile()
	if !ok || isDir {
		return nil
	}
	m.main.stg = &staging{path: path, rangeStart: -1}
	m.mainFocus = true
	return m.stgLoad(path, false, false)
}

func (m *Model) stgLoad(path string, staged, fromAction bool) tea.Cmd {
	repo := m.repo
	return func() tea.Msg {
		text, err := repo.DiffFile(path, staged)
		return stgMsg{path: path, staged: staged, text: text, fromAction: fromAction, err: err}
	}
}

func (m *Model) applyStg(msg stgMsg) tea.Cmd {
	s := m.main.stg
	if s == nil || s.path != msg.path || s.staged != msg.staged {
		return nil
	}
	var cmd tea.Cmd
	if msg.fromAction {
		cmd = m.loadCmd(true)
		if msg.actErr != nil {
			m.errorPopup(msg.actErr)
		}
	}
	if msg.err != nil {
		m.errorPopup(msg.err)
		m.closeStaging()
		return cmd
	}
	s.fd = git.FileDiff{}
	if fds := git.ParseDiff(msg.text); len(fds) > 0 {
		s.fd = fds[0]
	}
	s.loaded = true
	s.build()
	if len(s.dv.rows) == 0 && msg.fromAction {
		m.closeStaging() // nothing left to stage here
	}
	return cmd
}

func (m *Model) closeStaging() {
	m.main.stg = nil
	m.mainFocus = false
}

func (m *Model) stagingKey(k string) tea.Cmd {
	s := m.main.stg
	if s.dv == nil { // still loading
		if k == "esc" {
			m.closeStaging()
		}
		return nil
	}
	switch k {
	case "esc":
		m.closeStaging()
	case "j", "down":
		s.dv.cur = min(s.dv.cur+1, len(s.dv.rows)-1)
	case "k", "up":
		s.dv.cur = max(s.dv.cur-1, 0)
	case "tab":
		s.staged, s.loaded, s.dv, s.rangeStart = !s.staged, false, nil, -1
		return m.stgLoad(s.path, s.staged, false)
	case "a":
		s.lineMode = !s.lineMode
		s.rangeStart = -1
	case "v":
		if s.lineMode {
			s.rangeStart = s.dv.cur
		}
	case " ":
		return m.stgApply(false)
	case "d":
		if s.staged {
			return nil
		}
		hunk, sel := s.selection()
		if hunk < 0 {
			return nil
		}
		what := "hunk"
		if sel != nil {
			what = "selected lines"
		}
		m.confirm("Discard", "Discard the "+what+"? This cannot be undone.", func() tea.Cmd { return m.stgApply(true) })
	}
	if s.dv != nil {
		s.sync()
	}
	return nil
}

// stgApply stages (or unstages) the selection, or discards it.
func (m *Model) stgApply(discard bool) tea.Cmd {
	s := m.main.stg
	hunk, sel := s.selection()
	if hunk < 0 {
		return nil
	}
	reverse := s.staged || discard
	patch := git.BuildPatch(s.fd, hunk, sel, reverse)
	if patch == "" {
		return nil
	}
	repo, path, staged := m.repo, s.path, s.staged
	s.rangeStart = -1
	return func() tea.Msg {
		actErr := repo.ApplyPatch(patch, !discard, reverse)
		text, err := repo.DiffFile(path, staged)
		return stgMsg{path: path, staged: staged, text: text, fromAction: true, err: err, actErr: actErr}
	}
}

// stagingLines draws the staging view into w x h cells.
func (m *Model) stagingLines(w, h int) []string {
	s := m.main.stg
	which := "unstaged"
	if s.staged {
		which = "staged"
	}
	switch {
	case !s.loaded || s.dv == nil:
		return []string{"  " + sDim.Render("loading…")}
	case len(s.dv.rows) == 0:
		return []string{"  " + sDim.Render("nothing "+which)}
	}
	return s.dv.render(w, h)
}
