package tui

import (
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Decsr/decsr-tui/internal/agent"
	"github.com/Decsr/decsr-tui/internal/git"
)

// forceWorktreeMsg asks whether to force-remove a worktree git refused to remove.
type forceWorktreeMsg struct{ path string }

func (m *Model) worktreeItems() []item {
	var out []item
	for _, w := range m.worktrees {
		mark := "  "
		if sameDir(w.Path, m.repo.Dir) {
			mark = "* "
		}
		where := w.Branch
		switch {
		case w.Bare:
			where = "(bare)"
		case w.Detached:
			where = "(detached)"
		}
		plain := mark + filepath.Base(w.Path) + " " + where
		out = append(out, item{key: w.Path, plain: plain, styled: mark + sGreen.Render(filepath.Base(w.Path)) + " " + sDim.Render(where)})
	}
	return out
}

func sameDir(a, b string) bool {
	return agent.EqualName(filepath.Clean(a), filepath.Clean(b))
}

func (m *Model) selWorktree() (git.Worktree, bool) {
	for _, w := range m.worktrees {
		if w.Path == m.branches.list.key {
			return w, true
		}
	}
	return git.Worktree{}, false
}

func (m *Model) worktreesKey(k string) (tea.Cmd, bool) {
	switch k {
	case "n":
		m.popup = newInput("Worktree path", "", func(path string) tea.Cmd {
			if path == "" {
				return nil
			}
			if !filepath.IsAbs(path) {
				path = filepath.Join(m.repo.Dir, path)
			}
			m.popup = newInput("Branch (empty: let git pick)", "", func(branch string) tea.Cmd {
				if branch == "" {
					return m.do(func() error { return m.repo.AddWorktree(path, "", false) })
				}
				m.popup = newMenu("Branch "+branch, []menuItem{
					{"c", "Create it", func() tea.Cmd {
						return m.do(func() error { return m.repo.AddWorktree(path, branch, true) })
					}},
					{"e", "Use the existing branch", func() tea.Cmd {
						return m.do(func() error { return m.repo.AddWorktree(path, branch, false) })
					}},
				})
				return nil
			})
			return nil
		})
		return nil, true
	case "p":
		return m.do(m.repo.PruneWorktrees), true
	}
	w, ok := m.selWorktree()
	if !ok {
		return nil, k == "d" || k == " "
	}
	switch k {
	case "d":
		m.confirm("Remove worktree", "Remove worktree "+w.Path+"?", func() tea.Cmd {
			return func() tea.Msg {
				err := m.repo.RemoveWorktree(w.Path, false)
				if err != nil && strings.Contains(err.Error(), "--force") {
					return forceWorktreeMsg{w.Path}
				}
				return actionMsg{err}
			}
		})
		return nil, true
	case " ":
		if sameDir(w.Path, m.repo.Dir) {
			return nil, true
		}
		return m.switchRepo(w.Path), true
	}
	return nil, false
}
