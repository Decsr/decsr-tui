package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Decsr/decsr-tui/internal/agent"
	"github.com/Decsr/decsr-tui/internal/ui"
)

// Home is decsr's start screen: the starfield logo on the painted background
// and titled boxes for the sessions, the repo and the menu.

// The two lists Home can focus.
const (
	homeColSessions = iota
	homeColMenu
)

type homeState struct {
	col  int // focused list
	cur  int // selected session
	mcur int // selected menu row
}

type homeMenuItem struct {
	icon, label, desc, key string
	do                     func(m *Model) tea.Cmd
}

var homeMenu = []homeMenuItem{
	{"◉", "Watch", "follow a session, node by node", "w", func(m *Model) tea.Cmd { return m.goWatch() }},
	{"◆", "Git", "status, branches, commits, stash", "g", func(m *Model) tea.Cmd { m.screen = scGit; return nil }},
	{"?", "Help", "the keys of every screen", "?", func(m *Model) tea.Cmd { m.helpPopup(); return nil }},
}

// nodeCount is the number of nodes of a session that is not the watched one,
// counted once per change of the session.
type nodeCount struct {
	updated time.Time
	n       int
}

type countsMsg struct{ counts map[string]nodeCount }

// countsCmd counts the nodes of the newest sessions that are not watched, off
// the UI thread. A live session is counted once; the others again whenever
// they changed. It reads the same transcripts the watcher reads.
func (m *Model) countsCmd(ss []agent.Session) tea.Cmd {
	if m.counting {
		return nil
	}
	var todo []agent.Session
	for i, s := range ss {
		if i >= 12 {
			break
		}
		if s.ID == m.agent.cur {
			continue
		}
		c, ok := m.counts[s.ID]
		if !ok || (!s.Live && !c.updated.Equal(s.Updated)) {
			todo = append(todo, s)
		}
	}
	if len(todo) == 0 {
		return nil
	}
	m.counting = true
	src := m.src
	return func() tea.Msg {
		out := map[string]nodeCount{}
		for _, s := range todo {
			tr := src.Open(s)
			_, _ = tr.Poll()
			out[s.ID] = nodeCount{s.Updated, 1 + len(tr.Snapshot().Subagents)}
		}
		return countsMsg{out}
	}
}

func (m *Model) applyCounts(c map[string]nodeCount) {
	m.counting = false
	if m.counts == nil {
		m.counts = map[string]nodeCount{}
	}
	for id, v := range c {
		m.counts[id] = v
	}
}

// nodesOf is the number of nodes of a session, 0 when not counted yet.
func (m *Model) nodesOf(s agent.Session) int {
	if s.ID == m.agent.cur {
		return len(m.wnodes())
	}
	return m.counts[s.ID].n
}

// goWatch opens the Watch screen on the watched session.
func (m *Model) goWatch() tea.Cmd {
	m.screen = scWatch
	m.refreshWatch()
	return m.agentPollCmd()
}

// homeKey handles a key on Home.
func (m *Model) homeKey(k string) tea.Cmd {
	h := &m.home
	rows := len(m.agent.sessions)
	if h.col == homeColMenu {
		rows = len(homeMenu)
	}
	cur := &h.cur
	if h.col == homeColMenu {
		cur = &h.mcur
	}
	switch k {
	case "up", "k":
		*cur--
	case "down", "j":
		*cur++
	case "home":
		*cur = 0
	case "end":
		*cur = rows - 1
	case "tab", "shift+tab", "left", "right", "h", "l":
		h.col = 1 - h.col
	case "enter", " ":
		return m.homeActivate()
	case "w":
		return m.goWatch()
	case "g":
		m.screen = scGit
	case "R":
		return tea.Batch(m.loadCmd(true), m.sessionsCmd())
	case "?":
		m.helpPopup()
	}
	h.cur = max(min(h.cur, len(m.agent.sessions)-1), 0)
	h.mcur = max(min(h.mcur, len(homeMenu)-1), 0)
	return nil
}

func (m *Model) homeActivate() tea.Cmd {
	h := &m.home
	if h.col == homeColMenu {
		return homeMenu[max(min(h.mcur, len(homeMenu)-1), 0)].do(m)
	}
	if h.cur < 0 || h.cur >= len(m.agent.sessions) {
		return nil
	}
	return m.openWatch(m.agent.sessions[h.cur])
}

// ---- drawing ----

// Starfield around the logo (46 cells wide, the logo in the middle 24): ✦ is
// bright, G a gold star, the rest dim.
var (
	logoStarsTop    = "  ·       ✦        +      ⋆       G           "
	logoStarsBottom = "    ⋆       ·          ✦              G       "
	logoStarsLeft   = [3]string{"   ·       ", "  ⋆        ", "     ·     "}
	logoStarsRight  = [3]string{"  ·        ", "    G      ", "   ✦       "}
)

func renderStars(s string) string {
	dm := lipgloss.NewStyle().Foreground(ui.Muted)
	bright := lipgloss.NewStyle().Foreground(ui.Text)
	gold := lipgloss.NewStyle().Foreground(ui.Warn)
	var sb strings.Builder
	for _, r := range s {
		switch r {
		case ' ':
			sb.WriteRune(r)
		case '✦':
			sb.WriteString(bright.Render(string(r)))
		case 'G':
			sb.WriteString(gold.Render("✦"))
		default:
			sb.WriteString(dm.Render(string(r)))
		}
	}
	return sb.String()
}

// renderLogo is the logo block with its stars and the tagline, centred in w
// cells; a short or narrow screen gets the one-line mark.
func renderLogo(w, h int) []string {
	if w >= 62 && h >= 24 {
		lines := []string{renderStars(logoStarsTop)}
		for i, bl := range ui.Logo(true) {
			lines = append(lines, renderStars(logoStarsLeft[i])+bl+renderStars(logoStarsRight[i]))
		}
		lines = append(lines, renderStars(logoStarsBottom), sDim.Render("       "+ui.Tagline))
		for i := range lines {
			lines[i] = centerLine(lines[i], w)
		}
		return append(lines, "")
	}
	if h >= 12 {
		line := ui.Wordmark("DECSR") + sDim.Render("  "+ui.Tagline)
		return []string{centerLine(truncateOneLine(line, w), w), ""}
	}
	return nil
}

// sessionRows are the rows of the Sessions box at w cells, windowed to h rows
// around the selection.
func (m *Model) sessionRows(w, h int, focused bool) []string {
	ss := m.agent.sessions
	if len(ss) == 0 {
		msg := "no " + m.src.Name() + " sessions for this repo yet"
		if m.src.Name() == "all" {
			msg = "no agent sessions for this repo yet"
		}
		return []string{sDim.Render(msg), "", sDim.Render("Start an agent in this repo; R looks again.")}
	}
	cur := max(min(m.home.cur, len(ss)-1), 0)
	top := max(min(cur-h+1, len(ss)-h), 0)
	if cur < top {
		top = cur
	}
	var out []string
	for i := top; i < min(top+h, len(ss)); i++ {
		s := ss[i]
		runner := s.Runner
		if runner == "" {
			runner = m.src.Name()
		}
		title := oneLine(sanitizeText(s.Title))
		if title == "" {
			title = s.ID
		}
		dot := sDim.Render("○")
		if s.Live {
			dot = lipgloss.NewStyle().Foreground(ui.OK).Render("●")
		}
		meta := ago(s.Updated)
		if n := m.nodesOf(s); n > 0 {
			meta = plural(n, "node", "nodes") + " · " + meta
		}
		left := dot + " " + runnerBadge(runner, iconsUnicode) + " " + sText.Render(title)
		row := splitRow(left, sDim.Render(meta), max(w-1, 1))
		if i == cur {
			cursor := lipgloss.NewStyle().Background(ui.Sel).Foreground(ui.Muted).Render("▌")
			if focused {
				cursor = lipgloss.NewStyle().Background(ui.Sel).Foreground(ui.Accent).Render("▌")
			}
			out = append(out, cursor+withBg(row, ui.Sel, w-1))
		} else {
			out = append(out, " "+row)
		}
	}
	return out
}

// repoRows are the rows of the Repo box.
func (m *Model) repoRows() []string {
	branch := m.head.Branch
	if m.head.Detached {
		branch = "(detached)"
	}
	if branch == "" {
		branch = "-"
	}
	line := chip(branch, ui.GroupProject)
	if m.head.Ahead > 0 {
		line += sYellow.Render(fmt.Sprintf(" ↑%d", m.head.Ahead))
	}
	if m.head.Behind > 0 {
		line += sYellow.Render(fmt.Sprintf(" ↓%d", m.head.Behind))
	}
	staged, unstaged, untracked := 0, 0, 0
	for _, f := range m.files.files {
		switch {
		case f.Untracked:
			untracked++
		default:
			if f.Staged != ' ' {
				staged++
			}
			if f.Unstaged != ' ' {
				unstaged++
			}
		}
	}
	state := sGreen.Render("✓ clean")
	if m.state != 0 {
		state = chip(strings.ToUpper(m.state.String()), ui.Fail)
	}
	files := sGreen.Render(fmt.Sprintf("● %d staged", staged)) + "  " + sRed.Render(fmt.Sprintf("◐ %d unstaged", unstaged)) +
		"  " + sDim.Render(fmt.Sprintf("? %d untracked", untracked))
	return []string{
		sDim.Render(fmt.Sprintf("%-8s", "BRANCH")) + line,
		sDim.Render(fmt.Sprintf("%-8s", "STATE")) + state,
		sDim.Render(fmt.Sprintf("%-8s", "FILES")) + files,
	}
}

// menuRows are the rows of the Menu box.
func (m *Model) menuRows(w int, focused bool) []string {
	var out []string
	for i, it := range homeMenu {
		row := lipgloss.NewStyle().Foreground(ui.GroupRun).Render(it.icon) + " " + sBold.Render(fmt.Sprintf("%-6s", it.label)) + sDim.Render(it.desc)
		if i == m.home.mcur {
			cursor := lipgloss.NewStyle().Background(ui.Sel).Foreground(ui.Muted).Render("▌")
			if focused {
				cursor = lipgloss.NewStyle().Background(ui.Sel).Foreground(ui.Accent).Render("▌")
			}
			out = append(out, cursor+withBg(row, ui.Sel, w-1))
		} else {
			out = append(out, " "+row)
		}
	}
	return out
}

// homeBody draws Home in exactly h rows of w cells.
func (m *Model) homeBody(w, h int) []string {
	cw := max(w-2, 1) // content width: one cell of margin on each side
	lines := renderLogo(cw, h)
	rest := h - len(lines)
	sessFocus, menuFocus := m.home.col == homeColSessions, m.home.col == homeColMenu
	sess := func(bw, bh int) []string {
		title := "◆ SESSIONS"
		if n := len(m.agent.sessions); n > 0 {
			title += fmt.Sprintf(" · %d", n)
		}
		return pixelBox(title, ui.GroupSession, sessFocus, m.sessionRows(bw-4, bh-2, sessFocus), bw, bh)
	}
	repo := func(bw, bh int) []string {
		return pixelBox("◆ REPO", ui.GroupProject, false, m.repoRows(), bw, bh)
	}
	menu := func(bw, bh int) []string {
		return pixelBox("◆ MENU", ui.GroupRun, menuFocus, m.menuRows(bw-4, menuFocus), bw, bh)
	}
	const repoH, menuH, minSess = 5, 5, 5
	var body []string
	switch {
	case cw >= 86 && rest >= menuH:
		lw := cw * 55 / 100
		rw := cw - lw - 1
		left := sess(lw, rest)
		var right []string
		if rest >= repoH+menuH {
			right = append(repo(rw, repoH), menu(rw, menuH)...)
		} else {
			right = menu(rw, min(rest, menuH))
		}
		for i := 0; i < rest; i++ {
			body = append(body, fit(pick(left, i), lw)+" "+fit(pick(right, i), rw))
		}
	case rest >= repoH+menuH+minSess:
		body = append(append(sess(cw, rest-repoH-menuH), repo(cw, repoH)...), menu(cw, menuH)...)
	case rest >= menuH+minSess:
		body = append(sess(cw, rest-menuH), menu(cw, menuH)...)
	case menuFocus:
		body = menu(cw, rest)
	default:
		body = sess(cw, rest)
	}
	for _, l := range body {
		lines = append(lines, " "+l)
	}
	return exact(lines, w, h)
}
