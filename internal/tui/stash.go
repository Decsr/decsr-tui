package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Decsr/decsr-tui/internal/git"
)

func (m *Model) stashItems(int) []item {
	var out []item
	for _, s := range m.stashes.stashes {
		t := "{" + itoa(s.Index) + "} " + s.Subject
		out = append(out, item{key: s.Subject, plain: t, styled: t})
	}
	return out
}

func (m *Model) selStash() (git.Stash, bool) {
	for _, s := range m.stashes.stashes {
		if s.Subject == m.stashes.list.key {
			return s, true
		}
	}
	return git.Stash{}, false
}

type stashPanel struct {
	stashes []git.Stash
	list    list
}

func (m *Model) stashKey(k string) (tea.Cmd, bool) {
	p := &m.stashes
	if p.list.navKey(k, m.items(pStash, 40), 10) {
		return nil, true
	}
	s, ok := m.selStash()
	if !ok {
		return nil, k == " " || k == "g" || k == "d"
	}
	switch k {
	case " ":
		return m.do(func() error { return m.repo.StashApply(s.Index) }), true
	case "g":
		return m.do(func() error { return m.repo.StashPop(s.Index) }), true
	case "d":
		m.confirm("Drop stash", "Drop "+s.Ref+"?\n"+s.Subject, func() tea.Cmd {
			return m.do(func() error { return m.repo.StashDrop(s.Index) })
		})
		return nil, true
	}
	return nil, false
}
