package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Decsr/decsr-tui/internal/git"
)

func (m *Model) tagItems() []item {
	var out []item
	for _, t := range m.tags {
		short := t.Hash
		if len(short) > 7 {
			short = short[:7]
		}
		plain := t.Name + " " + short + " " + t.Subject
		out = append(out, item{key: t.Name, plain: plain, styled: sYellow.Render(t.Name) + " " + sDim.Render(short) + " " + t.Subject})
	}
	return out
}

func (m *Model) selTag() (git.Tag, bool) {
	for _, t := range m.tags {
		if t.Name == m.branches.list.key {
			return t, true
		}
	}
	return git.Tag{}, false
}

// remoteName is the remote tag and branch deletes go to.
func (m *Model) remoteName() string {
	if rs, err := m.repo.Remotes(); err == nil && len(rs) > 0 {
		for _, r := range rs {
			if r.Name == "origin" {
				return r.Name
			}
		}
		return rs[0].Name
	}
	return "origin"
}

func (m *Model) tagsKey(k string) (tea.Cmd, bool) {
	if k == "n" {
		ref := ""
		if c, ok := m.selCommit(); ok {
			ref = c.Hash
		}
		m.popup = newInput("New tag name", "", func(name string) tea.Cmd {
			if name == "" {
				return nil
			}
			m.popup = newInput("Tag message (empty: lightweight tag)", "", func(msg string) tea.Cmd {
				return m.do(func() error { return m.repo.CreateTag(name, ref, msg) })
			})
			return nil
		})
		return nil, true
	}
	t, ok := m.selTag()
	if !ok {
		return nil, k == "d" || k == "P" || k == " " || k == "enter"
	}
	switch k {
	case "d":
		m.popup = newMenu("Delete tag "+t.Name, []menuItem{
			{"l", "Delete local tag only", func() tea.Cmd {
				return m.do(func() error { return m.repo.DeleteTag(t.Name) })
			}},
			{"r", "Delete local and remote tag", func() tea.Cmd {
				return m.startNet("delete tag", func(ctx context.Context) error {
					if err := m.repo.DeleteTag(t.Name); err != nil {
						return err
					}
					return m.repo.DeleteRemoteTag(ctx, m.remoteName(), t.Name)
				})
			}},
		})
		return nil, true
	case "P":
		return m.startNet("push tag", func(ctx context.Context) error {
			return m.repo.PushTag(ctx, m.remoteName(), t.Name)
		}), true
	case " ":
		m.confirm("Checkout tag", "Check out tag "+t.Name+"?\nHEAD will be detached.", func() tea.Cmd {
			return m.do(func() error { return m.repo.Checkout(t.Name) })
		})
		return nil, true
	case "enter":
		m.mainFocus = true
		return nil, true
	}
	return nil, false
}
