package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Decsr/decsr-tui/internal/ui"
)

// The chrome is what every screen shares: the header (wordmark, screen tabs,
// branch and repo state chips, the agent strip), the notice line under it, the
// status line and the footer of keycap pills.

type screen int

const (
	scHome screen = iota
	scGit
	scWatch
)

var screenInfo = [...]struct{ key, name string }{scHome: {"H", "Home"}, scGit: {"G", "Git"}, scWatch: {"W", "Watch"}}

// screenKey handles the keys that switch screen: H, G and W. G is also "go
// to the bottom" of a list, so on the Git screen it stays with the list.
func (m *Model) screenKey(k string) (tea.Cmd, bool) {
	switch k {
	case "H":
		m.screen = scHome
		return nil, true
	case "W":
		return m.goWatch(), true
	case "G":
		if m.screen != scGit {
			m.screen = scGit
			return nil, true
		}
	}
	return nil, false
}

// ---- notice line ----

// notice is a one-line event under the header ("subagent Explore started").
type notice struct {
	kind, text string // a ui.Result* kind
	at         time.Time
}

const (
	noticeFade = 4 * time.Second // after this the notice turns dim
	noticeLife = 7 * time.Second // after this it is gone
)

func (m *Model) now() time.Time {
	if m.clock != nil {
		return m.clock()
	}
	return time.Now()
}

// say shows a notice for a few seconds.
func (m *Model) say(kind, text string) {
	if text = oneLine(sanitizeText(text)); text != "" {
		m.note = notice{kind, text, m.now()}
	}
}

// noticeLine is the row under the header: the current notice, a glyph first so
// it never depends on colour, dim while it fades; blank when there is none.
func (m *Model) noticeLine() string {
	n := m.note
	age := m.now().Sub(n.at)
	if n.text == "" || age >= noticeLife || age < 0 {
		return ""
	}
	text := truncateOneLine(n.text, max(m.w-6, 4))
	if age >= noticeFade {
		g, _ := ui.ResultGlyph(n.kind)
		return "  " + sDim.Render(g+" "+text)
	}
	return "  " + ui.ResultLine(n.kind, text)
}

// ---- header ----

// tabs are the screen tabs: "H Home  G Git  W Watch", the active one a filled
// pill (in brackets when there are no colours); compact is just the keys.
func (m *Model) tabs(compact bool) string {
	var parts []string
	for i, s := range screenInfo {
		label := s.key + " " + s.name
		if compact {
			label = s.key
		}
		if screen(i) == m.screen {
			if ui.NoColor() {
				parts = append(parts, "["+label+"]")
			} else {
				parts = append(parts, chip(label, ui.Accent))
			}
			continue
		}
		if compact {
			parts = append(parts, sDim.Render(label))
		} else {
			parts = append(parts, sBold.Render(s.key)+sDim.Render(" "+s.name))
		}
	}
	return strings.Join(parts, " ")
}

// headerLine is the top line: the decsr mark, the screen tabs, the branch with
// ahead and behind, the repo state and a running network command on the left;
// the agent strip on the right.
func (m *Model) headerLine() string {
	branch := m.head.Branch
	if m.head.Detached {
		branch = "(detached)"
	}
	left := ui.Wordmark("decsr")
	if m.w >= 100 {
		left += sDim.Render(" · ") + sBold.Render(repoName(m.repo))
	}
	left += "  " + m.tabs(m.w < 80)
	if branch != "" {
		left += "  " + chip(branch, ui.GroupProject)
	}
	if m.head.Ahead > 0 {
		left += sYellow.Render(fmt.Sprintf(" ↑%d", m.head.Ahead))
	}
	if m.head.Behind > 0 {
		left += sYellow.Render(fmt.Sprintf(" ↓%d", m.head.Behind))
	}
	if m.state != 0 {
		left += "  " + chip(strings.ToUpper(m.state.String()), ui.Fail)
	}
	if m.net != nil {
		left += "  " + sYellow.Render(m.spin.View()+m.net.name+" · esc cancels")
	}
	return ui.Header(left, m.agentStrip(max(m.w/2, 12)), m.w)
}

// stripGlyph is the status glyph of a node chip: the spinner while it runs.
func (m *Model) stripGlyph(n wnode) string {
	if n.running() {
		return m.spinFrame()
	}
	g, _, _ := nodeStatus(n)
	return g
}

// agentStrip is decsr's agent strip: one chip per node with its glyph, short
// label, tokens and calls ("[▖ Explore 3.1k ×4]"). Below 110 columns the chips
// drop the brackets and the calls. Cut to w cells.
func (m *Model) agentStrip(w int) string {
	nodes := m.wnodes()
	if len(nodes) == 0 {
		return sDim.Render("no agent session")
	}
	wide := m.w >= 110
	var parts []string
	for _, n := range nodes {
		tokens := "—"
		if t := promptTokens(n.usage) + int(n.usage.Output); t > 0 {
			tokens = tokCompact(t)
		}
		label := truncateOneLine(n.label, 10)
		var col lipgloss.TerminalColor = ui.RoleColor(n.label)
		if n.id == rootID {
			label = runnerGlyph(n.runner, iconsUnicode) + " " + truncateOneLine(n.runner, 10)
			col = runnerColor(n.runner)
		}
		calls, _ := activityCounts(n)
		text := m.stripGlyph(n) + " " + label + " " + tokens
		if wide {
			text = fmt.Sprintf("[%s ×%d]", text, calls)
		}
		st := lipgloss.NewStyle().Foreground(col)
		if n.running() {
			st = st.Bold(true)
		}
		parts = append(parts, st.Render(text))
	}
	sep := " "
	if wide {
		sep = "  "
	}
	return ansi.Truncate(strings.Join(parts, sep), w, "…")
}

// ---- status line ----

// statusLine is decsr's status bar: on Home and Git the repo or network state
// and the last line of the command log; on Watch the tokens and the cost.
func (m *Model) statusLine() string {
	if m.screen == scWatch {
		return m.watchStatusLine()
	}
	state := "idle"
	switch {
	case m.net != nil:
		state = m.net.name + "…"
	case m.state != 0:
		state = strings.ToLower(m.state.String())
	}
	info := ""
	if last := m.log.last(1); len(last) > 0 {
		info = oneLine(last[0])
	}
	return ansi.Truncate(NewStatusBar(m.w).WithState(state).WithStageInfo(info).View(), m.w, "")
}

// ---- footer ----

// hintWidth is the cells a hint takes as a pill: the keycap, a space, the label.
func hintWidth(h ui.Hint) int { return lenW(h.Key) + 2 + 1 + lenW(h.Label) }

// pillBar renders hints as keycap pills with dim labels, two spaces apart,
// in at most w cells.
func pillBar(hs []ui.Hint, w int) string {
	var parts []string
	for _, h := range hs {
		parts = append(parts, keycap(h.Key)+sDim.Render(" "+h.Label))
	}
	return ansi.Truncate(" "+strings.Join(parts, "  "), w, "")
}

// fitHints keeps the first hint and the last two (help and quit) and drops
// the ones in between, from the back, until the pills fit width.
func fitHints(width int, hints []ui.Hint) []ui.Hint {
	size := func(hs []ui.Hint) int {
		total := 1 + 2*max(len(hs)-1, 0)
		for _, h := range hs {
			total += hintWidth(h)
		}
		return total
	}
	for len(hints) > 3 && size(hints) > width {
		hints = append(hints[:len(hints)-3], hints[len(hints)-2:]...)
	}
	return hints
}

// hints is the footer: the keys of the screen (of the focused panel on Git, in
// its current mode) as keycap pills, then help and quit.
func (m *Model) hints(w int) string {
	var hs []ui.Hint
	switch {
	case m.screen == scHome:
		hs = []ui.Hint{{Key: "↑↓", Label: "move"}, {Key: "←→", Label: "list"}, {Key: "enter", Label: "open"},
			{Key: "W", Label: "watch"}, {Key: "G", Label: "git"}, {Key: "?", Label: "help"}, {Key: "q", Label: "quit"}}
	case m.screen == scWatch && m.watch.overlay != nil:
		hs = []ui.Hint{{Key: "j/k", Label: "scroll"}, {Key: "pgdn", Label: "page"}, {Key: "esc", Label: "close"}}
	case m.screen == scWatch:
		hs = []ui.Hint{{Key: "↑↓", Label: "node"}, {Key: "←→", Label: "parent/child"}, {Key: "[ ]", Label: "step"},
			{Key: "enter", Label: "detail"}, {Key: "space", Label: "expand"}, {Key: "f", Label: "follow"}, {Key: "v", Label: "live"},
			{Key: "n p", Label: "file"}, {Key: "H", Label: "home"}, {Key: "G", Label: "git"}, {Key: "?", Label: "help"}, {Key: "q", Label: "quit"}}
	default:
		for _, b := range m.bindings() {
			hs = append(hs, ui.Hint{Key: showKey(b.key), Label: b.desc})
		}
		if !m.modal() && !m.mainFocus {
			hs = append(hs, ui.Hint{Key: "H", Label: "home"}, ui.Hint{Key: "W", Label: "watch"},
				ui.Hint{Key: "?", Label: "help"}, ui.Hint{Key: "q", Label: "quit"})
		}
	}
	return pillBar(fitHints(w, hs), w)
}

// ---- help ----

// helpSection is one titled group of keys in the help popup.
type helpSection struct {
	title string
	color lipgloss.TerminalColor
	keys  []binding
}

var screenBindings = []binding{{"H", "Home"}, {"G", "Git (from Home and Watch)"}, {"W", "Watch the session"}}

func (m *Model) helpSections() []helpSection {
	switch m.screen {
	case scHome:
		return []helpSection{
			{"Home", ui.GroupSession, []binding{{"↑↓ j k", "move in the list"}, {"← → tab", "switch between sessions and menu"},
				{"home end", "first / last row"}, {"enter", "open the session in Watch / run the menu row"}, {"w g", "Watch, Git"}}},
			{"Screens", ui.GroupRun, screenBindings},
			{"General", ui.GroupUsage, []binding{{"R", "refresh"}, {"?", "help"}, {"q", "quit"}}},
		}
	case scWatch:
		return []helpSection{
			{"Nodes", ui.GroupAgents, []binding{{"↑↓ j k", "previous / next node (graph and feed move together)"}, {"←", "parent node"},
				{"→", "first child node"}, {"home end", "root / last node"}, {"f", "follow the newest node and step"}, {"space", "expand the card"}}},
			{"Steps", ui.GroupSession, []binding{{"[ ]", "older / newer step of the node"}, {"enter", "full detail of the step"}}},
			{"Live pane", ui.GroupProject, []binding{{"v", "hide / show the live pane"}, {"n p", "next / previous changed file"},
				{"pgup pgdn", "scroll the diff"}}},
			{"Screens", ui.GroupRun, screenBindings},
			{"General", ui.GroupUsage, []binding{{"R", "refresh"}, {"?", "help"}, {"q", "quit"}}},
		}
	}
	secs := []helpSection{{panelNames[m.focus], groupColor[m.focus].(lipgloss.TerminalColor), m.bindings()}}
	secs = append(secs, helpSection{"Global", ui.GroupUsage, globalBindings}, helpSection{"Screens", ui.GroupRun, screenBindings})
	if cs := m.customHere(); len(cs) > 0 {
		var ks []binding
		for _, cc := range cs {
			ks = append(ks, binding{cc.Key, cc.Description})
		}
		secs = append(secs, helpSection{"Custom", ui.GroupSession, ks})
	}
	return secs
}

// helpPopup opens the keybindings of the screen, grouped.
func (m *Model) helpPopup() {
	var b strings.Builder
	for i, s := range m.helpSections() {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(lipgloss.NewStyle().Foreground(s.color).Bold(true).Render(s.title) + "\n")
		for _, bd := range s.keys {
			fmt.Fprintf(&b, "%s %s\n", lipgloss.NewStyle().Foreground(ui.Accent).Bold(true).Render(fmt.Sprintf("%-10s", showKey(bd.key))), sText.Render(bd.desc))
		}
	}
	title := map[screen]string{scHome: "Keybindings · Home", scGit: "Keybindings · Git", scWatch: "Keybindings · Watch"}[m.screen]
	m.popup = &popup{kind: pkHelp, title: title, body: strings.TrimRight(b.String(), "\n")}
}
