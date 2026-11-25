package tui

import (
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Decsr/decsr-tui/internal/git"
)

type filesPanel struct {
	files     []git.File
	tree      bool
	collapsed map[string]bool
	list      list
}

type fnode struct {
	name, path string
	file       *git.File
	kids       []*fnode
}

func buildTree(files []git.File) *fnode {
	root := &fnode{}
	for i := range files {
		f := &files[i]
		cur := root
		parts := strings.Split(f.Path, "/")
		for j, p := range parts {
			path := strings.Join(parts[:j+1], "/")
			var next *fnode
			for _, k := range cur.kids {
				if k.name == p && (k.file == nil) == (j < len(parts)-1) {
					next = k
					break
				}
			}
			if next == nil {
				next = &fnode{name: p, path: path}
				if j == len(parts)-1 {
					next.file = f
				}
				cur.kids = append(cur.kids, next)
			}
			cur = next
		}
	}
	var sortKids func(n *fnode)
	sortKids = func(n *fnode) {
		sort.SliceStable(n.kids, func(a, b int) bool {
			ka, kb := n.kids[a], n.kids[b]
			if (ka.file == nil) != (kb.file == nil) {
				return ka.file == nil
			}
			return ka.name < kb.name
		})
		for _, k := range n.kids {
			sortKids(k)
		}
	}
	sortKids(root)
	return root
}

func statusLetters(f *git.File) (string, string) {
	if f.Untracked {
		return "?", "?"
	}
	return string(f.Staged), string(f.Unstaged)
}

func fileRow(f *git.File, indent, name string) item {
	x, y := statusLetters(f)
	plain := indent + x + y + " " + name
	xs, ys := x, y
	switch {
	case f.Conflicted:
		xs, ys = sRed.Bold(true).Render(x), sRed.Bold(true).Render(y)
		name = sRed.Render(name)
	case f.Untracked:
		xs, ys = sRed.Render(x), sRed.Render(y)
	default:
		if x != " " {
			xs = sGreen.Render(x)
		}
		if y != " " {
			ys = sRed.Render(y)
		}
	}
	return item{key: f.Path, plain: plain, styled: indent + xs + ys + " " + name}
}

func (m *Model) filesItems(int) []item {
	p := &m.files
	var out []item
	if !p.tree || p.list.filter != "" {
		fs := append([]git.File(nil), p.files...)
		sort.Slice(fs, func(a, b int) bool { return fs[a].Path < fs[b].Path })
		for i := range fs {
			out = append(out, fileRow(&fs[i], "", fs[i].Path))
		}
		return out
	}
	var walk func(n *fnode, depth int)
	walk = func(n *fnode, depth int) {
		for _, k := range n.kids {
			ind := strings.Repeat("  ", depth)
			if k.file != nil {
				out = append(out, fileRow(k.file, ind, k.name))
				continue
			}
			mark := "▾ "
			if p.collapsed[k.path] {
				mark = "▸ "
			}
			out = append(out, item{key: "/" + k.path, plain: ind + mark + k.name + "/", styled: ind + sBlue.Render(mark+k.name+"/")})
			if !p.collapsed[k.path] {
				walk(k, depth+1)
			}
		}
	}
	walk(buildTree(p.files), 0)
	return out
}

// selFile resolves the selected row: a file, or a directory (isDir).
func (m *Model) selFile() (f git.File, path string, isDir, ok bool) {
	key := m.files.list.key
	if key == "" {
		return f, "", false, false
	}
	if strings.HasPrefix(key, "/") {
		return f, key[1:], true, true
	}
	for _, x := range m.files.files {
		if x.Path == key {
			return x, key, false, true
		}
	}
	return f, "", false, false
}

// filesUnder lists the files at or below path.
func (m *Model) filesUnder(path string, isDir bool) []git.File {
	var out []git.File
	for _, f := range m.files.files {
		if f.Path == path || (isDir && strings.HasPrefix(f.Path, path+"/")) {
			out = append(out, f)
		}
	}
	return out
}

func (m *Model) filesKey(k string) (tea.Cmd, bool) {
	p := &m.files
	if p.list.navKey(k, m.items(pFiles, 40), 10) {
		return nil, true
	}
	f, path, isDir, ok := m.selFile()
	switch k {
	case " ":
		if !ok {
			return nil, true
		}
		fs := m.filesUnder(path, isDir)
		stage := false
		for _, x := range fs {
			if x.Unstaged != ' ' {
				stage = true
			}
		}
		return m.do(func() error {
			if stage {
				return m.repo.Stage(path)
			}
			return m.repo.Unstage(path)
		}), true
	case "a":
		stage := false
		for _, x := range p.files {
			if x.Unstaged != ' ' {
				stage = true
			}
		}
		return m.do(func() error {
			if stage {
				return m.repo.StageAll()
			}
			return m.repo.UnstageAll()
		}), true
	case "d":
		if !ok {
			return nil, true
		}
		fs := m.filesUnder(path, isDir)
		discard := func(title, what string, f func(string) error) tea.Cmd {
			m.confirm(title, "Discard "+what+" to "+path+"?\nThis cannot be undone.", func() tea.Cmd {
				return m.do(func() error {
					for _, x := range fs {
						if err := f(x.Path); err != nil {
							return err
						}
					}
					return nil
				})
			})
			return nil
		}
		m.popup = newMenu("Discard "+path, []menuItem{
			{"u", "Discard unstaged changes", func() tea.Cmd {
				return discard("Discard unstaged", "unstaged changes", m.repo.DiscardUnstaged)
			}},
			{"d", "Discard all changes", func() tea.Cmd {
				return discard("Discard all", "all changes", m.repo.Discard)
			}},
		})
		return nil, true
	case "enter":
		if !ok {
			return nil, true
		}
		if isDir {
			p.collapsed[path] = !p.collapsed[path]
			return nil, true
		}
		if f.Conflicted {
			m.mainFocus = true
			return nil, true
		}
		return m.openStaging(), true
	case "c":
		m.popup = newCommitInput("Commit message", "", func(msg string) tea.Cmd {
			if msg == "" {
				return nil
			}
			return m.do(func() error { return m.repo.Commit(msg, false) })
		})
		return nil, true
	case "A":
		m.confirm("Amend", "Amend the last commit?", func() tea.Cmd {
			m.popup = newCommitInput("Amend commit message", m.repo.LastCommitMessage(), func(msg string) tea.Cmd {
				return m.do(func() error { return m.repo.Commit(msg, true) })
			})
			return nil
		})
		return nil, true
	case "s":
		m.stashPrompt(true)
		return nil, true
	case "S":
		m.popup = newMenu("Stash", []menuItem{
			{"a", "Stash all changes (tracked files)", func() tea.Cmd { m.stashPrompt(false); return nil }},
			{"u", "Stash all changes and untracked files", func() tea.Cmd { m.stashPrompt(true); return nil }},
		})
		return nil, true
	case "e":
		if ok && !isDir {
			return m.editorCmd(path), true
		}
		return nil, true
	case "`":
		p.tree = !p.tree
		return nil, true
	}
	return nil, false
}

func (m *Model) stashPrompt(untracked bool) {
	m.popup = newInput("Stash message", "", func(msg string) tea.Cmd {
		return m.do(func() error { return m.repo.StashPush(msg, untracked) })
	})
}
