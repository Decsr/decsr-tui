package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Decsr/decsr-tui/internal/git"
	"github.com/Decsr/decsr-tui/internal/ui"
)

// todoEditor is the interactive rebase todo shown in the main pane. rows are
// listed like the commit log, newest first.
type todoEditor struct {
	base string
	rows []git.TodoItem
	cur  int
	off  int
}

var todoActions = map[string]string{"p": "pick", "r": "reword", "s": "squash", "f": "fixup", "d": "drop", "e": "edit"}

// openTodo opens the editor for the commits from c up to HEAD.
func (m *Model) openTodo(c git.Commit) tea.Cmd {
	base := "--root"
	if len(c.Parents) > 0 {
		base = c.Parents[0]
	}
	todo, err := m.repo.RebaseTodo(base)
	if err != nil {
		m.errorPopup(err)
		return nil
	}
	if len(todo) == 0 {
		m.errorPopup(fmt.Errorf("nothing to rebase"))
		return nil
	}
	rows := make([]git.TodoItem, len(todo))
	for i, it := range todo {
		rows[len(todo)-1-i] = it
	}
	m.todo = &todoEditor{base: base, rows: rows}
	m.mainFocus = false
	return nil
}

func (m *Model) todoKey(k string) tea.Cmd {
	t := m.todo
	switch k {
	case "j", "down":
		t.cur = min(t.cur+1, len(t.rows)-1)
	case "k", "up":
		t.cur = max(t.cur-1, 0)
	case "ctrl+j":
		if t.cur+1 < len(t.rows) {
			t.rows[t.cur], t.rows[t.cur+1] = t.rows[t.cur+1], t.rows[t.cur]
			t.cur++
		}
	case "ctrl+k":
		if t.cur > 0 {
			t.rows[t.cur], t.rows[t.cur-1] = t.rows[t.cur-1], t.rows[t.cur]
			t.cur--
		}
	case "enter", "m":
		return m.todoStart()
	case "esc":
		m.todo = nil
	default:
		if a, ok := todoActions[k]; ok {
			t.rows[t.cur].Action = a
		}
	}
	return nil
}

// todoStart runs the rebase; reword items ask for their message first.
func (m *Model) todoStart() tea.Cmd {
	t := m.todo
	todo := make([]git.TodoItem, len(t.rows))
	for i, it := range t.rows {
		todo[len(t.rows)-1-i] = it
	}
	if a := todo[0].Action; a == "squash" || a == "fixup" {
		m.errorPopup(fmt.Errorf("the oldest commit cannot be %sed: there is nothing before it to fold into", a))
		return nil
	}
	var rewords []git.TodoItem
	for _, it := range todo {
		if it.Action == "reword" {
			rewords = append(rewords, it)
		}
	}
	msgs := map[string]string{}
	var ask func(i int) tea.Cmd
	ask = func(i int) tea.Cmd {
		if i == len(rewords) {
			m.todo = nil
			return m.do(func() error { return m.rebaseWithRewords(t.base, todo, msgs) })
		}
		it := rewords[i]
		m.popup = newCommitInput("Reword "+shortHash(it.Hash), it.Subject, func(msg string) tea.Cmd {
			if msg == "" {
				msg = it.Subject
			}
			msgs[it.Hash] = msg
			return ask(i + 1)
		})
		return nil
	}
	return ask(0)
}

func shortHash(h string) string {
	if len(h) > 7 {
		return h[:7]
	}
	return h
}

// rebaseWithRewords runs the todo. RebaseInteractive has no per-commit
// messages, so each reword becomes an edit stop where the message is amended.
func (m *Model) rebaseWithRewords(base string, todo []git.TodoItem, msgs map[string]string) error {
	var queue []string
	edit := append([]git.TodoItem(nil), todo...)
	for i := range edit {
		if edit[i].Action == "reword" {
			edit[i].Action = "edit"
			queue = append(queue, edit[i].Hash)
		}
	}
	err := m.repo.RebaseInteractive(base, edit)
	for _, h := range queue {
		if m.repo.State() != git.Rebasing {
			return err
		}
		if _, fs, _ := m.repo.Status(); anyConflict(fs) {
			return err
		}
		if e := m.repo.Commit(msgs[h], true); e != nil {
			return e
		}
		err = m.repo.Continue()
	}
	return err
}

func anyConflict(fs []git.File) bool {
	for _, f := range fs {
		if f.Conflicted {
			return true
		}
	}
	return false
}

var actionStyle = map[string]lipgloss.Style{
	"pick": sDim, "reword": sCyan, "edit": sYellow,
	"squash": sMag, "fixup": sMag, "drop": sRed,
}

// todoLines renders the editor at w x h: a dim key line, then one row per
// commit with its action in the action's colour.
func (m *Model) todoLines(w, h int) []string {
	t := m.todo
	if h < 1 {
		return nil
	}
	body := h - 1
	t.off = max(0, min(t.off, len(t.rows)-body))
	if t.cur < t.off {
		t.off = t.cur
	}
	if body > 0 && t.cur >= t.off+body {
		t.off = t.cur - body + 1
	}
	out := []string{ansi.Truncate(sDim.Render("p pick · r reword · s squash · f fixup · d drop · e edit · ctrl+j/k move · enter start · esc cancel"), w, "…")}
	for i := t.off; i < len(t.rows) && i < t.off+body; i++ {
		it := t.rows[i]
		row := actionStyle[it.Action].Render(fmt.Sprintf("%-6s", it.Action)) + " " + sYellow.Render(shortHash(it.Hash)) + " " + sText.Render(strings.ReplaceAll(it.Subject, "\n", " "))
		if i == t.cur {
			out = append(out, lipgloss.NewStyle().Background(ui.Sel).Foreground(ui.Accent).Render("▌")+withBg(row, ui.Sel, w-1))
			continue
		}
		out = append(out, " "+fit(row, w-1))
	}
	return out
}
