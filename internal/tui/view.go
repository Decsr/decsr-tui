package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Decsr/decsr-tui/internal/ui"
)

type rect struct{ x, y, w, h int }

type panelRect struct {
	id panelID
	rect
}

type layoutInfo struct {
	left   int // width of the left column (0 when only the main pane shows)
	panels []panelRect
	log    rect
	main   rect // w == 0 when hidden
}

const (
	minSplitW   = 60 // below this width the screen is one column: the focused pane only
	panelMinH   = 3  // a collapsed panel: one row between its borders
	panelTabsH  = 6  // a panel this tall shows its tab bar
	panelFilter = 5  // a panel this tall shows its filter line
	logRows     = 5
)

// groupColor is the colour of a panel when it has focus: the command-group
// colours of the decsr palette.
var groupColor = [numPanels]lipgloss.TerminalColor{
	pFiles:    ui.GroupProject,
	pBranches: ui.GroupRun,
	pCommits:  ui.GroupSession,
	pStash:    ui.GroupUsage,
	pAgent:    ui.GroupAgents,
}

// bodyH is the height between the notice line and the status and hint lines.
func (m *Model) bodyH() int { return max(m.h-4, 1) }

// layout computes the screen rectangles from the size and focus. Below
// minSplitW the screen is one column (decsr's narrow collapse): the focused
// panel, or the main pane while it has focus.
func (m *Model) layout() layoutInfo {
	var lay layoutInfo
	avail := m.bodyH()
	lw, mw := m.w, 0
	if m.w >= minSplitW {
		lw = min(max(m.w*35/100, 30), 56)
		mw = m.w - lw
	}
	if mw == 0 && (m.modal() || m.mainFocus) {
		lay.main = rect{0, 0, m.w, avail}
		return lay
	}
	lay.left = lw
	if mw > 0 {
		lay.main = rect{lw, 0, mw, avail}
	}
	lh := 0
	if m.showLog && avail >= 30 {
		lh = logRows
	}
	rem := avail - lh
	if rem < panelMinH*(int(numPanels)-1)+panelTabsH { // too small for every panel: only the focused one
		lay.panels = []panelRect{{m.focus, rect{0, 0, lw, max(avail, panelMinH)}}}
		return lay
	}
	var heights [numPanels]int
	for i := range heights {
		heights[i] = panelMinH
	}
	heights[m.focus] += rem - panelMinH*int(numPanels)
	y := 0
	for i := panelID(0); i < numPanels; i++ {
		lay.panels = append(lay.panels, panelRect{i, rect{0, y, lw, heights[i]}})
		y += heights[i]
	}
	if lh > 0 {
		lay.log = rect{0, y, lw, lh}
	}
	return lay
}

func (m *Model) mainRect() rect { return m.layout().main }

// modal reports whether the main pane is taken over by the staging view or the rebase todo.
func (m *Model) modal() bool { return m.main.stg != nil || m.todo != nil }

// panelTabs is the tab bar of a panel of height h: the Branches tabs, or
// Activity and Changes of an open session. A short panel has none.
func (m *Model) panelTabs(id panelID, h int) (tabs []string, active int) {
	if h < panelTabsH {
		return nil, 0
	}
	switch id {
	case pBranches:
		return tabNames[:], m.branches.tab
	case pAgent:
		if m.agent.open {
			if m.agent.changes {
				active = 1
			}
			return []string{"Activity", "Changes"}, active
		}
	}
	return nil, 0
}

// panelTitle is the title of a panel; the active tab is named in it while the
// panel is too short to show its tab bar.
func (m *Model) panelTitle(id panelID, tabsShown bool) string {
	t := fmt.Sprintf("%d %s", int(id)+1, panelNames[id])
	switch id {
	case pFiles:
		if n := len(m.files.files); n > 0 {
			t += fmt.Sprintf(" · %d", n)
		}
	case pBranches:
		if !tabsShown {
			t += " · " + tabNames[m.branches.tab]
		}
	case pCommits:
		if m.commits.sub != nil {
			t += " › files"
		} else if m.commits.graph {
			t += " · graph"
		}
		if n := len(m.clip); n > 0 {
			t += fmt.Sprintf(" · %d copied", n)
		}
	case pAgent:
		if m.agent.open {
			if m.agent.follow && !m.agent.changes {
				t += " · follow"
			}
			if !tabsShown && m.agent.changes {
				t += " · changes"
			}
			t += " · " + m.agent.snap.Session.Runner + " " + oneLine(sanitizeText(m.agent.snap.Session.Title))
		}
	}
	return t
}

func (m *Model) emptyText(id panelID) string {
	switch id {
	case pFiles:
		return "working tree clean"
	case pBranches:
		switch m.branches.tab {
		case tabTags:
			return "no tags"
		case tabWorktrees:
			return "no worktrees"
		}
		return "no branches"
	case pCommits:
		return "no commits"
	case pStash:
		return "no stashes"
	}
	if m.agent.open {
		return "no activity yet"
	}
	if m.src.Name() == "all" {
		return "no agent sessions for this repo yet"
	}
	return "no " + m.src.Name() + " sessions for this repo yet"
}

// panelOpts is the ui.Panel look of a list panel: its group colour when it
// has focus, the dim border colour otherwise.
func (m *Model) panelOpts(pr panelRect) (ui.PanelOpts, bool) {
	focused := pr.id == m.focus && !m.mainFocus
	o := ui.PanelOpts{Color: ui.Border, TitleColor: ui.Muted}
	if focused {
		o.Color, o.TitleColor = groupColor[pr.id], groupColor[pr.id]
	}
	o.Tabs, o.Active = m.panelTabs(pr.id, pr.h)
	o.Title = m.panelTitle(pr.id, len(o.Tabs) > 0)
	if f := m.listOf(pr.id).filter; f != "" {
		if pr.h >= panelFilter {
			o.Filter = f
		} else {
			o.Title += " /" + f
		}
	}
	return o, focused
}

func (m *Model) panelBox(pr panelRect) string {
	o, focused := m.panelOpts(pr)
	inner := pr.w - 4
	rows := ui.PanelBodyRows(o, pr.h)
	its := m.items(pr.id, inner-1)
	if len(its) == 0 {
		o.Body = []string{"  " + sDim.Render(m.emptyText(pr.id))}
	} else {
		l := m.listOf(pr.id)
		o.Scroll = l.window(len(its), rows)
		o.Body = l.rows(its, inner, focused)
	}
	return ui.PanelSized(o, pr.w, pr.h)
}

func (m *Model) logBox(r rect) string {
	o := ui.PanelOpts{Title: "Command log", Color: ui.Border, TitleColor: ui.Muted}
	for _, l := range m.log.last(ui.PanelBodyRows(o, r.h)) {
		o.Body = append(o.Body, sDim.Render(ansiTrunc(l, r.w-4)))
	}
	return ui.PanelSized(o, r.w, r.h)
}

func (m *Model) mainBox(r rect) string {
	o := ui.PanelOpts{Title: m.main.title, Color: ui.Border, TitleColor: ui.Muted}
	if m.mainFocus {
		o.Color, o.TitleColor = groupColor[m.focus], groupColor[m.focus]
	}
	switch {
	case m.main.stg != nil:
		s := m.main.stg
		mode := "hunk"
		if s.lineMode {
			mode = "line"
		}
		o.Title = "Staging " + s.path + " · " + mode + " mode"
		o.Tabs = []string{"Unstaged", "Staged"}
		if s.staged {
			o.Active = 1
		}
	case m.todo != nil:
		o.Title = "Interactive rebase " + shortHash(m.todo.base)
	}
	if o.Title == "" {
		o.Title = "decsr"
	}
	ih := ui.PanelBodyRows(o, r.h)
	lines, scrolls := m.mainLines(max(r.w-4, 1), ih)
	if scrolls {
		m.main.scroll = max(0, min(m.main.scroll, len(lines)-ih))
		o.Scroll = m.main.scroll
	}
	o.Body = lines
	return ui.PanelSized(o, r.w, r.h)
}

// View renders the whole screen: header, notice line, the body of the screen
// (Home, Git or Watch), status line and footer, then the popup over it. Home
// is drawn on the painted dark background; Git and Watch keep the terminal's.
func (m *Model) View() string {
	if m.w < 1 || m.h < 2 {
		return ""
	}
	if m.screen == scHome {
		return ui.WithDark(func() string { return paintBackground(m.compose(), m.w, m.h) })
	}
	return m.compose()
}

func (m *Model) compose() string {
	var body []string
	switch m.screen {
	case scHome:
		body = m.homeBody(m.w, m.bodyH())
	case scWatch:
		body = m.watchBody(m.w, m.bodyH())
	default:
		body = m.gitBody()
	}
	rows := append([]string{m.headerLine(), m.noticeLine()}, body...)
	rows = append(rows, m.statusLine(), m.hints(m.w))
	v := strings.Join(rows, "\n")
	if m.screen == scWatch && m.watch.overlay != nil && m.popup == nil {
		v = ui.PlaceOverlay(v, m.overlayBox(), m.w, m.h)
	}
	if m.popup != nil {
		v = ui.PlaceOverlay(v, m.popupBox(m.w, m.h), m.w, m.h)
	}
	return fitView(v, m.w, m.h)
}

// gitBody is the Git screen: the five panels and the command log on the left,
// the main pane on the right, in exactly bodyH rows.
func (m *Model) gitBody() []string {
	lay := m.layout()
	var left []string
	for _, pr := range lay.panels {
		left = append(left, strings.Split(m.panelBox(pr), "\n")...)
	}
	if lay.log.h > 0 {
		left = append(left, strings.Split(m.logBox(lay.log), "\n")...)
	}
	var right []string
	if lay.main.w > 0 {
		right = strings.Split(m.mainBox(lay.main), "\n")
	}
	var rows []string
	for i := 0; i < m.bodyH(); i++ {
		var l, r string
		if lay.left > 0 {
			l = fit(pick(left, i), lay.left)
		}
		if lay.main.w > 0 {
			r = fit(pick(right, i), lay.main.w)
		}
		rows = append(rows, l+r)
	}
	return rows
}

// fitView guarantees at most h lines of at most w cells.
func fitView(s string, w, h int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	for i, l := range lines {
		if ansi.StringWidth(l) > w {
			lines[i] = ansi.Truncate(l, w, "")
		}
	}
	return strings.Join(lines, "\n")
}

func pick(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return ""
}

// ---- mouse ----

func (m *Model) mouse(msg tea.MouseMsg) tea.Cmd {
	if m.popup != nil || m.screen != scGit {
		return nil
	}
	lay := m.layout()
	y := msg.Y - 2 // the header and the notice line
	in := func(r rect) bool {
		return r.w > 0 && msg.X >= r.x && msg.X < r.x+r.w && y >= r.y && y < r.y+r.h
	}
	wheel := 0
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		wheel = -3
	case tea.MouseButtonWheelDown:
		wheel = 3
	}
	if in(lay.main) {
		if wheel != 0 && !m.modal() {
			m.main.scroll += wheel
			m.clampScroll()
		}
		return nil
	}
	for _, pr := range lay.panels {
		if !in(pr.rect) {
			continue
		}
		l := m.listOf(pr.id)
		if wheel != 0 {
			l.moveTo(m.items(pr.id, pr.w-5), l.cur+wheel)
			return nil
		}
		if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
			return nil
		}
		m.focus, m.mainFocus = pr.id, false
		o, _ := m.panelOpts(pr)
		row := y - pr.y - 1
		if len(o.Tabs) > 0 {
			row--
		}
		if o.Filter != "" {
			row--
		}
		if row >= 0 && row < ui.PanelBodyRows(o, pr.h) {
			its := m.items(pr.id, pr.w-5)
			if i := l.off + row; i < len(its) {
				l.moveTo(its, i)
			}
		}
		return nil
	}
	return nil
}
