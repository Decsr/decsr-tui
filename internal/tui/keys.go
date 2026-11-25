package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Decsr/decsr-tui/internal/ui"
)

type binding struct{ key, desc string }

// keyStr is the canonical name of a key press (space is " ").
func keyStr(msg tea.KeyMsg) string {
	s := msg.String()
	if s == "space" {
		return " "
	}
	return s
}

// keyMsgFromString builds the key press that keyStr maps back to s.
func keyMsgFromString(s string) tea.KeyMsg {
	named := map[string]tea.KeyType{
		"enter": tea.KeyEnter, "esc": tea.KeyEsc, "tab": tea.KeyTab, "shift+tab": tea.KeyShiftTab,
		"ctrl+g": tea.KeyCtrlG, "ctrl+d": tea.KeyCtrlD, "ctrl+u": tea.KeyCtrlU, "ctrl+j": tea.KeyCtrlJ, "ctrl+k": tea.KeyCtrlK,
		"up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight,
		"pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown,
	}
	if t, ok := named[s]; ok {
		return tea.KeyMsg{Type: t}
	}
	if s == " " {
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func showKey(k string) string {
	if k == " " {
		return "space"
	}
	return k
}

var globalBindings = []binding{
	{"tab", "next panel"}, {"1", "jump to panel 1-5"}, {"f", "fetch (not in Commits/Agent)"}, {"p", "pull"}, {"P", "push"},
	{"z", "undo (reflog)"}, {"Z", "redo"},
	{"R", "refresh"}, {"/", "filter"}, {"x", "action menu"}, {"?", "help"}, {"@", "toggle command log"},
	{"m", "merge/rebase menu"}, {"esc", "back / cancel network op"}, {"q", "quit"},
}

// bindings lists the keys of the focused panel in its current mode.
func (m *Model) bindings() []binding {
	if m.todo != nil {
		return []binding{{"p", "pick"}, {"r", "reword"}, {"s", "squash"}, {"f", "fixup"}, {"d", "drop"}, {"e", "edit"},
			{"ctrl+j", "move down"}, {"ctrl+k", "move up"}, {"enter", "start"}, {"esc", "cancel"}}
	}
	if m.main.stg != nil {
		return []binding{{" ", "stage/unstage"}, {"v", "range"}, {"a", "hunk/line mode"}, {"d", "discard"},
			{"tab", "staged/unstaged"}, {"j", "down"}, {"k", "up"}, {"esc", "back"}}
	}
	if m.mainFocus {
		return []binding{{"j", "scroll down"}, {"k", "scroll up"}, {"G", "bottom"}, {"esc", "back"}}
	}
	switch m.focus {
	case pFiles:
		return []binding{{" ", "stage/unstage"}, {"a", "stage/unstage all"}, {"d", "discard"}, {"enter", "staging view"},
			{"c", "commit"}, {"A", "amend"}, {"s", "stash"}, {"S", "stash options"}, {"e", "edit"}, {"`", "tree/flat"}}
	case pBranches:
		tabs := binding{"[", "prev tab"}
		switch m.branches.tab {
		case tabRemotes:
			return []binding{{" ", "checkout"}, {"n", "new branch"}, {"d", "delete on remote"}, {"M", "merge into current"},
				{"r", "rebase onto"}, tabs, {"]", "next tab"}, {"enter", "log"}}
		case tabTags:
			return []binding{{"n", "new tag"}, {"d", "delete"}, {"P", "push tag"}, {" ", "checkout"}, tabs, {"]", "next tab"}, {"enter", "scroll"}}
		case tabWorktrees:
			return []binding{{"n", "add"}, {"d", "remove"}, {" ", "switch to"}, {"p", "prune"}, tabs, {"]", "next tab"}}
		}
		return []binding{{" ", "checkout"}, {"n", "new branch"}, {"d", "delete"}, {"R", "rename"}, {"M", "merge into current"},
			{"r", "rebase onto"}, {"u", "set upstream"}, tabs, {"]", "next tab"}, {"enter", "log"}}
	case pCommits:
		if m.commits.sub != nil {
			return []binding{{"enter", "scroll diff"}, {"esc", "back"}}
		}
		return []binding{{"enter", "files"}, {"g", "reset"}, {"A", "amend with staged"}, {"s", "squash"}, {"f", "fixup"},
			{"d", "drop"}, {"r", "reword"}, {"ctrl+j", "move down"}, {"ctrl+k", "move up"}, {"e", "edit"}, {"F", "fixup commit"},
			{"C", "copy"}, {"V", "paste"}, {"t", "revert"}, {"i", "interactive rebase"}, {"ctrl+g", "graph view"}, {"esc", "clear copied"}}
	case pStash:
		return []binding{{" ", "apply"}, {"g", "pop"}, {"d", "drop"}}
	case pAgent:
		if m.agent.open {
			return []binding{{" ", "expand subagent"}, {"enter", "expand/scroll"}, {"f", "follow"}, {"c", "changes"},
				{"G", "newest"}, {"esc", "back"}}
		}
		return []binding{{"enter", "open session"}}
	}
	return nil
}

// hints is the hint bar: the keys of the focused panel in its current mode,
// then help and quit. When they do not fit, the ones in the middle go first.
func (m *Model) hints(w int) string {
	var hs []ui.Hint
	for _, b := range m.bindings() {
		hs = append(hs, ui.Hint{Key: showKey(b.key), Label: b.desc})
	}
	if !m.modal() && !m.mainFocus {
		hs = append(hs, ui.Hint{Key: "?", Label: "help"}, ui.Hint{Key: "q", Label: "quit"})
	}
	return ui.HintBar(fitHints(w, hs), w)
}

// fitHints keeps the first hint and the last (quit) and drops the ones in
// between, from the back, until the bar fits width.
func fitHints(width int, hints []ui.Hint) []ui.Hint {
	size := func(hs []ui.Hint) int {
		total := 2 + 3*max(len(hs)-1, 0)
		for _, h := range hs {
			total += lenW(h.Key) + 1 + lenW(h.Label)
		}
		return total
	}
	for len(hints) > 2 && size(hints) > width {
		hints = append(hints[:len(hints)-2], hints[len(hints)-1])
	}
	return hints
}

func (m *Model) helpPopup() {
	var b strings.Builder
	row := func(k, d string) {
		fmt.Fprintf(&b, "%s%s %s\n", lipgloss.NewStyle().Foreground(ui.Accent).Bold(true).Render(fmt.Sprintf("%-8s", showKey(k))), "", sText.Render(d))
	}
	b.WriteString(sBold.Render(panelNames[m.focus]) + "\n")
	for _, bd := range m.bindings() {
		row(bd.key, bd.desc)
	}
	b.WriteString("\n" + sBold.Render("Global") + "\n")
	for _, bd := range globalBindings {
		row(bd.key, bd.desc)
	}
	if cs := m.customHere(); len(cs) > 0 {
		b.WriteString("\n" + sBold.Render("Custom") + "\n")
		for _, cc := range cs {
			row(cc.Key, cc.Description)
		}
	}
	m.popup = &popup{kind: pkHelp, title: "Keybindings", body: strings.TrimRight(b.String(), "\n")}
}
func (m *Model) actionMenu() {
	var items []menuItem
	for _, b := range m.bindings() {
		b := b
		items = append(items, menuItem{label: fmt.Sprintf("%-7s %s", showKey(b.key), b.desc), fn: func() tea.Cmd {
			return m.key(keyMsgFromString(b.key))
		}})
	}
	for _, cc := range m.customHere() {
		cc := cc
		items = append(items, menuItem{label: fmt.Sprintf("%-7s Custom: %s", showKey(cc.Key), cc.Description), fn: func() tea.Cmd {
			return m.runCustom(cc)
		}})
	}
	m.popup = newMenu(panelNames[m.focus]+" actions", items)
}

// key routes a key press.
func (m *Model) key(msg tea.KeyMsg) tea.Cmd {
	if m.popup != nil {
		return m.popupKey(msg)
	}
	k := keyStr(msg)
	switch k {
	case "ctrl+c", "q":
		if m.net != nil {
			m.net.cancel()
		}
		m.quit = true
		return nil
	case "ctrl+d", "ctrl+u", "pgdown", "pgup":
		m.scrollMain(k)
		return nil
	}
	if m.main.stg != nil {
		return m.stagingKey(k)
	}
	if m.todo != nil {
		return m.todoKey(k)
	}
	if m.mainFocus {
		return m.scrollKey(k)
	}
	if cc, ok := m.customFor(k); ok {
		return m.runCustom(cc)
	}
	switch k {
	case "1", "2", "3", "4", "5":
		m.focus = panelID(k[0] - '1')
		return nil
	case "tab", "l", "right":
		m.focus = (m.focus + 1) % numPanels
		return nil
	case "shift+tab", "h", "left":
		m.focus = (m.focus + numPanels - 1) % numPanels
		return nil
	case "?":
		m.helpPopup()
		return nil
	case "x":
		m.actionMenu()
		return nil
	case "/":
		return m.filterPrompt()
	case "@":
		m.showLog = !m.showLog
		return nil
	}
	if cmd, ok := m.panelKey(k); ok {
		return cmd
	}
	return m.globalKey(k)
}

func (m *Model) panelKey(k string) (tea.Cmd, bool) {
	switch m.focus {
	case pFiles:
		return m.filesKey(k)
	case pBranches:
		return m.branchesKey(k)
	case pCommits:
		return m.commitsKey(k)
	case pStash:
		return m.stashKey(k)
	case pAgent:
		return m.agentKey(k)
	}
	return nil, false
}

func (m *Model) globalKey(k string) tea.Cmd {
	switch k {
	case "esc":
		if m.net != nil {
			m.net.cancel()
			return nil
		}
		if l := m.listOf(m.focus); l.filter != "" {
			l.filter = ""
		}
	case "z":
		return m.undoRedo(false)
	case "Z":
		return m.undoRedo(true)
	case "f":
		return m.startNet("fetch", func(ctx context.Context) error { return m.repo.Fetch(ctx) })
	case "p":
		return m.startNet("pull", func(ctx context.Context) error { return m.repo.Pull(ctx, false) })
	case "P":
		return m.push()
	case "R":
		return tea.Batch(m.loadCmd(true), m.sessionsCmd())
	case "m":
		if m.state != 0 {
			m.stateMenu()
		}
	}
	return nil
}

// undoRedo confirms and runs the reflog undo (or redo).
func (m *Model) undoRedo(redo bool) tea.Cmd {
	name := "Undo"
	target, ok, err := m.repo.UndoTarget()
	if redo {
		name = "Redo"
		target, ok, err = m.repo.RedoTarget()
	}
	if err != nil {
		m.errorPopup(err)
		return nil
	}
	if !ok {
		m.errorPopup(fmt.Errorf("nothing to %s", strings.ToLower(name)))
		return nil
	}
	what := "checkout " + target.Branch
	if target.Kind == "reset" {
		what = "reset to " + shortHash(target.Hash)
		if cs, err := m.repo.Log(target.Hash, 1); err == nil && len(cs) > 0 {
			what += " '" + cs[0].Subject + "'"
		}
	}
	m.confirm(name, fmt.Sprintf("%s: %s\n(reverts %s)", name, what, target.Desc), func() tea.Cmd {
		return m.do(func() error {
			if redo {
				return m.repo.Redo()
			}
			return m.repo.Undo()
		})
	})
	return nil
}

func (m *Model) push() tea.Cmd {
	if m.head.Upstream == "" && !m.head.Detached {
		m.confirm("No upstream", fmt.Sprintf("Branch %q has no upstream.\nPush and set upstream to origin/%s?", m.head.Branch, m.head.Branch), func() tea.Cmd {
			return m.startNet("push", func(ctx context.Context) error { return m.repo.Push(ctx, false, true) })
		})
		return nil
	}
	return m.startNet("push", func(ctx context.Context) error { return m.repo.Push(ctx, false, false) })
}

func (m *Model) stateMenu() {
	name := strings.ToUpper(m.state.String())
	m.popup = newMenu(name, []menuItem{
		{"c", "Continue", func() tea.Cmd { return m.do(m.repo.Continue) }},
		{"a", "Abort", func() tea.Cmd { return m.do(m.repo.Abort) }},
	})
}

func (m *Model) filterPrompt() tea.Cmd {
	l := m.listOf(m.focus)
	m.popup = newInput("Filter "+panelNames[m.focus], l.filter, func(s string) tea.Cmd {
		l.filter = s
		return nil
	})
	return nil
}
