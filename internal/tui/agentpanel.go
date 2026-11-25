package tui

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Decsr/decsr-tui/internal/agent"
	"github.com/Decsr/decsr-tui/internal/ui"
)

type agentPanel struct {
	sessions []agent.Session
	cur      string // id of the session being watched
	pinned   bool   // the user chose cur, so do not jump to a newer one
	open     bool   // showing cur as an activity tree
	tracker  agent.Tracker
	polling  bool
	loaded   bool
	snap     agent.Snapshot
	expanded map[string]bool // subagent id -> expanded
	follow   bool
	changes  bool
	toEnd    bool // select the newest row on the next sync
	sessList list
	tree     list
	chSel    int       // the file shown in the Changes tab
	chView   *diffView // its diff, with the cursor and scroll of the pane
}

func (a *agentPanel) openSession(src agent.Source, s agent.Session) {
	a.cur, a.tracker = s.ID, src.Open(s)
	a.snap, a.loaded, a.polling = agent.Snapshot{Session: s}, false, false
	a.changes = false
	a.tree = list{}
	a.chSel, a.chView = 0, nil
}

func (a *agentPanel) selSession() (agent.Session, bool) {
	for _, s := range a.sessions {
		if s.ID == a.sessList.key {
			return s, true
		}
	}
	return agent.Session{}, false
}

type arow struct {
	act   agent.Activity
	depth int
	key   string
}

// rows flattens the main thread, with expanded subagents inline.
func (a *agentPanel) rows() []arow {
	var out []arow
	subs := map[string]agent.Subagent{}
	for _, s := range a.snap.Subagents {
		subs[s.ID] = s
	}
	for _, act := range a.snap.Activities {
		out = append(out, arow{act, 0, act.ID})
		if act.Kind == agent.KindSubagent && act.SubagentID != "" && a.expanded[act.SubagentID] {
			for _, c := range subs[act.SubagentID].Activities {
				out = append(out, arow{c, 1, act.SubagentID + "/" + c.ID})
			}
		}
	}
	return out
}

func (a *agentPanel) selActivity() (agent.Activity, bool) {
	for _, r := range a.rows() {
		if r.key == a.tree.key {
			return r.act, true
		}
	}
	return agent.Activity{}, false
}

// touchedFiles lists repo-relative paths the session edited or wrote.
func (a *agentPanel) touchedFiles(repoDir string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(acts []agent.Activity) {
		for _, x := range acts {
			if (x.Kind != agent.KindEdit && x.Kind != agent.KindWrite) || x.File == "" {
				continue
			}
			p := x.File
			if filepath.IsAbs(p) {
				rel, err := filepath.Rel(repoDir, p)
				if err != nil || strings.HasPrefix(rel, "..") {
					continue
				}
				p = rel
			}
			p = filepath.ToSlash(p)
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	add(a.snap.Activities)
	for _, s := range a.snap.Subagents {
		add(s.Activities)
	}
	return out
}

// agentItems lists the rows of the Agent panel: the sessions (runner glyph in
// the runner's colour, title, age and a live dot) or, once one is opened, its
// activity tree, one activityLine per row with the subagents nested under
// their ◈ row.
func (m *Model) agentItems(w int) []item {
	a := &m.agent
	var out []item
	if !a.open {
		for _, s := range a.sessions {
			title := s.Title
			if title == "" {
				title = s.ID
			}
			runner := s.Runner
			if runner == "" {
				runner = m.src.Name()
			}
			dot := sDim.Render("○")
			if s.Live {
				dot = sGreen.Render("●")
			}
			left := runnerBadge(runner, iconsUnicode) + " " + sText.Render(oneLine(sanitizeText(title)))
			right := sDim.Render(ago(s.Updated)) + " " + dot
			out = append(out, item{key: s.ID, plain: runner + " " + title, styled: splitRow(left, right, max(w, 1))})
		}
		return out
	}
	now := time.Now()
	spin := m.spinFrame()
	for _, r := range a.rows() {
		act := r.act
		chev := "  "
		if act.Kind == agent.KindSubagent && act.SubagentID != "" {
			chev = "▸ "
			if a.expanded[act.SubagentID] {
				chev = "▾ "
			}
		}
		prefix := strings.Repeat("  ", r.depth) + chev
		line := activityLine(act, now, max(w-lenW(prefix), 3))
		if act.State == agent.StateRunning {
			line = strings.Replace(line, actGlyph(act.Kind), lipgloss.NewStyle().Foreground(ui.Accent).Render(spin), 1)
		}
		out = append(out, item{key: r.key, plain: act.Title, styled: sDim.Render(prefix) + line})
	}
	return out
}

// spinFrame is the current frame of the decsr pixel spinner.
func (m *Model) spinFrame() string { return pixelSpinner.Frames[m.frame%len(pixelSpinner.Frames)] }

func (m *Model) agentKey(k string) (tea.Cmd, bool) {
	a := &m.agent
	if !a.open {
		if a.sessList.navKey(k, m.items(pAgent, 40), 10) {
			return nil, true
		}
		if k == "enter" {
			s, ok := a.selSession()
			if !ok {
				return nil, true
			}
			if s.ID != a.cur {
				a.openSession(m.src, s)
			}
			a.pinned, a.open = true, true
			a.follow = s.Live
			a.toEnd = a.follow
			return m.agentPollCmd(), true
		}
		return nil, false
	}
	switch k {
	case "k", "up":
		a.follow = false
	case "G", "end":
		a.follow = true
		a.toEnd = true
		return nil, true
	}
	if a.tree.navKey(k, m.items(pAgent, 40), 10) {
		return nil, true
	}
	switch k {
	case " ", "enter":
		act, ok := a.selActivity()
		if ok && act.Kind == agent.KindSubagent && act.SubagentID != "" {
			a.expanded[act.SubagentID] = !a.expanded[act.SubagentID]
		} else if k == "enter" {
			m.mainFocus = true
		}
		return nil, true
	case "f":
		a.follow = !a.follow
		a.toEnd = a.follow
		return nil, true
	case "c":
		a.changes = !a.changes
		return nil, true
	case "[", "]":
		if a.changes {
			m.stepChanges(map[string]int{"[": -1, "]": 1}[k])
			return nil, true
		}
	case "esc":
		if a.changes {
			a.changes = false
		} else {
			a.open = false
		}
		return nil, true
	}
	return nil, false
}
