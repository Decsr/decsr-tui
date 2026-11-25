package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Decsr/decsr-tui/internal/git"
)

const (
	tabLocal = iota
	tabRemotes
	tabTags
	tabWorktrees
	numTabs
)

var tabNames = [numTabs]string{"Local", "Remotes", "Tags", "Worktrees"}

type branchesPanel struct {
	all  []git.Branch
	tab  int
	list list
}

// setTab switches the tab and restarts the cursor (rows differ per tab).
func (p *branchesPanel) setTab(t int) {
	if t < 0 || t >= numTabs || t == p.tab {
		return
	}
	p.tab = t
	p.list.cur, p.list.off, p.list.key = 0, 0, ""
}

func (m *Model) branchItems(w int) []item {
	switch m.branches.tab {
	case tabTags:
		return m.tagItems()
	case tabWorktrees:
		return m.worktreeItems()
	}
	var out []item
	for _, b := range m.branches.all {
		if b.Remote != (m.branches.tab == tabRemotes) || strings.HasSuffix(b.Name, "/HEAD") {
			continue
		}
		mark, name := "  ", b.Name
		styled := name
		if b.Head {
			mark = "* "
			styled = sGreen.Render(name)
		}
		extra := ""
		if b.Ahead > 0 {
			extra += fmt.Sprintf(" ↑%d", b.Ahead)
		}
		if b.Behind > 0 {
			extra += fmt.Sprintf(" ↓%d", b.Behind)
		}
		out = append(out, item{key: b.Name, plain: mark + name + extra, styled: mark + styled + sYellow.Render(extra)})
	}
	return out
}

func (m *Model) selBranch() (git.Branch, bool) {
	for _, b := range m.branches.all {
		if b.Name == m.branches.list.key && b.Remote == (m.branches.tab == tabRemotes) {
			return b, true
		}
	}
	return git.Branch{}, false
}

func (m *Model) branchesKey(k string) (tea.Cmd, bool) {
	p := &m.branches
	if p.list.navKey(k, m.items(pBranches, 40), 10) {
		return nil, true
	}
	switch k {
	case "[":
		p.setTab(p.tab - 1)
		return nil, true
	case "]":
		p.setTab(p.tab + 1)
		return nil, true
	}
	switch p.tab {
	case tabTags:
		return m.tagsKey(k)
	case tabWorktrees:
		return m.worktreesKey(k)
	}
	b, ok := m.selBranch()
	if !ok {
		return nil, k == "enter" || k == " " || k == "n" || k == "d" || k == "R" || k == "M" || k == "r" || k == "u"
	}
	checkoutName := b.Name
	if b.Remote {
		checkoutName = b.Short
	}
	switch k {
	case " ":
		return m.do(func() error { return m.repo.Checkout(checkoutName) }), true
	case "n":
		m.popup = newInput("New branch from "+b.Name, "", func(name string) tea.Cmd {
			if name == "" {
				return nil
			}
			return m.do(func() error { return m.repo.CreateBranch(name, b.Name, true) })
		})
		return nil, true
	case "d":
		if b.Remote {
			remote, short := b.Name, b.Short
			if i := strings.Index(remote, "/"); i > 0 {
				remote = remote[:i]
			}
			m.confirm("Delete remote branch", "Delete branch "+short+" on "+remote+"?\nThis cannot be undone.", func() tea.Cmd {
				return m.startNet("delete remote branch", func(ctx context.Context) error {
					return m.repo.DeleteRemoteBranch(ctx, remote, short)
				})
			})
			return nil, true
		}
		m.confirm("Delete branch", "Delete branch "+b.Name+"?", func() tea.Cmd {
			return func() tea.Msg {
				err := m.repo.DeleteBranch(b.Name, false)
				if err != nil && strings.Contains(err.Error(), "not fully merged") {
					return forceDeleteMsg{b.Name}
				}
				return actionMsg{err}
			}
		})
		return nil, true
	case "R":
		m.popup = newInput("Rename "+b.Name, b.Name, func(name string) tea.Cmd {
			if name == "" || name == b.Name {
				return nil
			}
			return m.do(func() error { return m.repo.RenameBranch(b.Name, name) })
		})
		return nil, true
	case "M":
		m.confirm("Merge", fmt.Sprintf("Merge %s into %s?", b.Name, m.head.Branch), func() tea.Cmd {
			return m.do(func() error { return m.repo.Merge(b.Name) })
		})
		return nil, true
	case "r":
		m.confirm("Rebase", fmt.Sprintf("Rebase %s onto %s?", m.head.Branch, b.Name), func() tea.Cmd {
			return m.do(func() error { return m.repo.RebaseOnto(b.Name) })
		})
		return nil, true
	case "u":
		if b.Remote {
			return nil, true
		}
		def := b.Upstream
		if def == "" {
			def = "origin/" + b.Name
		}
		m.popup = newInput("Upstream of "+b.Name, def, func(up string) tea.Cmd {
			if up == "" {
				return nil
			}
			return m.do(func() error { return m.repo.SetUpstream(b.Name, up) })
		})
		return nil, true
	case "enter":
		m.mainFocus = true
		return nil, true
	}
	return nil, false
}

// forceDeleteMsg asks whether to force-delete an unmerged branch.
type forceDeleteMsg struct{ name string }
