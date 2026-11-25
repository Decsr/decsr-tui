package tui

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Decsr/decsr-tui/internal/git"
)

type commitsPanel struct {
	commits []git.Commit
	graph   bool
	list    list
	sub     *commitSub // the files of one commit
}

type commitSub struct {
	hash  string
	files []git.ChangedFile
	list  list
}

func (m *Model) commitItems(int) []item {
	if sub := m.commits.sub; sub != nil {
		var out []item
		for _, f := range sub.files {
			s := string(f.Status)
			out = append(out, item{key: f.Path, plain: s + " " + f.Path, styled: sYellow.Render(s) + " " + f.Path})
		}
		return out
	}
	var out []item
	for _, c := range m.commits.commits {
		mark := ""
		if len(m.clip) > 0 {
			mark = "  "
			if m.inClip(c.Hash) >= 0 {
				mark = "+ "
			}
		}
		out = append(out, item{key: c.Hash, plain: mark + c.Short + " " + c.Subject, styled: sGreen.Render(mark) + sYellow.Render(c.Short) + " " + c.Subject})
	}
	return out
}

func (m *Model) inClip(hash string) int {
	for i, c := range m.clip {
		if c.Hash == hash {
			return i
		}
	}
	return -1
}

// clipAdd copies c. The clipboard is kept oldest first (the order to
// cherry-pick in) against the log shown now; commits copied from another
// log keep their copy order.
func (m *Model) clipAdd(c git.Commit) {
	pos := map[string]int{}
	for i, x := range m.commits.commits {
		pos[x.Hash] = i
	}
	at := len(m.clip)
	for i, x := range m.clip {
		px, okx := pos[x.Hash]
		if okx && px < pos[c.Hash] { // x is newer than c
			at = i
			break
		}
	}
	m.clip = append(m.clip, git.Commit{})
	copy(m.clip[at+1:], m.clip[at:])
	m.clip[at] = c
}

func (m *Model) clipHashes() []string {
	var out []string
	for _, c := range m.clip {
		out = append(out, c.Hash)
	}
	return out
}

// commitMessage is the full message of a commit.
func (m *Model) commitMessage(c git.Commit) string {
	if c.Hash == m.headHash {
		return m.repo.LastCommitMessage()
	}
	out, err := exec.Command("git", "-C", m.repo.Dir, "log", "-1", "--format=%B", c.Hash).Output()
	if err != nil {
		return c.Subject
	}
	return strings.TrimRight(string(out), "\r\n\t ")
}

func (m *Model) hasStaged() bool {
	for _, f := range m.files.files {
		if f.Staged != ' ' && !f.Untracked && !f.Conflicted {
			return true
		}
	}
	return false
}

// moveCommit moves the selected commit one row (+1 down towards older, -1 up).
func (m *Model) moveCommit(c git.Commit, delta int) tea.Cmd {
	l := &m.commits.list
	if l.cur+delta < 0 || l.cur+delta >= len(m.commits.commits) {
		return nil
	}
	l.cur += delta // the hash changes, so the cursor follows by position
	l.key = ""
	if delta > 0 {
		return m.do(func() error { return m.repo.MoveDown(c.Hash) })
	}
	return m.do(func() error { return m.repo.MoveUp(c.Hash) })
}

func (m *Model) revertCommit(c git.Commit) {
	if len(c.Parents) < 2 {
		m.confirm("Revert", "Revert "+c.Short+"?\n"+c.Subject, func() tea.Cmd {
			return m.do(func() error { return m.repo.Revert(c.Hash) })
		})
		return
	}
	var items []menuItem
	for i, p := range c.Parents {
		n, p := i+1, p
		if len(p) > 7 {
			p = p[:7]
		}
		items = append(items, menuItem{fmt.Sprint(n), fmt.Sprintf("Parent %d (%s)", n, p), func() tea.Cmd {
			return m.do(func() error { return m.repo.RevertMerge(c.Hash, n) })
		}})
	}
	m.popup = newMenu("Revert merge "+c.Short+": keep which parent?", items)
}

func (m *Model) rewordCommit(c git.Commit) {
	m.popup = newCommitInput("Reword "+c.Short, m.commitMessage(c), func(msg string) tea.Cmd {
		if msg == "" {
			return nil
		}
		if c.Hash == m.headHash && !m.hasStaged() {
			return m.do(func() error { return m.repo.Commit(msg, true) })
		}
		return m.do(func() error { return m.repo.Reword(c.Hash, msg) })
	})
}

func (m *Model) selCommit() (git.Commit, bool) {
	for _, c := range m.commits.commits {
		if c.Hash == m.commits.list.key {
			return c, true
		}
	}
	return git.Commit{}, false
}

func (m *Model) selCommitFile() (git.ChangedFile, bool) {
	if sub := m.commits.sub; sub != nil {
		for _, f := range sub.files {
			if f.Path == sub.list.key {
				return f, true
			}
		}
	}
	return git.ChangedFile{}, false
}

func (m *Model) commitsKey(k string) (tea.Cmd, bool) {
	p := &m.commits
	if p.sub != nil {
		if p.sub.list.navKey(k, m.items(pCommits, 40), 10) {
			return nil, true
		}
		switch k {
		case "esc":
			p.sub = nil
			return nil, true
		case "enter":
			m.mainFocus = true
			return nil, true
		}
		return nil, false
	}
	if p.list.navKey(k, m.items(pCommits, 40), 10) {
		return nil, true
	}
	c, ok := m.selCommit()
	if !ok && (len(k) == 1 && strings.Contains("sfdrejiFCtV", k) || k == "ctrl+j" || k == "ctrl+k") {
		return nil, true
	}
	switch k {
	case "esc":
		if len(m.clip) > 0 {
			m.clip = nil
			return nil, true
		}
	case "s":
		m.confirm("Squash", "Squash "+c.Short+" into its parent?\nBoth messages are kept.", func() tea.Cmd {
			return m.do(func() error { return m.repo.SquashInto(c.Hash) })
		})
		return nil, true
	case "f":
		m.confirm("Fixup", "Fixup "+c.Short+" into its parent?\nThis message is discarded.", func() tea.Cmd {
			return m.do(func() error { return m.repo.Fixup(c.Hash) })
		})
		return nil, true
	case "d":
		m.confirm("Drop commit", "Drop "+c.Short+"?\n"+c.Subject, func() tea.Cmd {
			return m.do(func() error { return m.repo.Drop(c.Hash) })
		})
		return nil, true
	case "r":
		m.rewordCommit(c)
		return nil, true
	case "ctrl+j":
		return m.moveCommit(c, 1), true
	case "ctrl+k":
		return m.moveCommit(c, -1), true
	case "e":
		return m.do(func() error { return m.repo.EditCommit(c.Hash) }), true
	case "F":
		return m.do(func() error { return m.repo.FixupInto(c.Hash) }), true
	case "C":
		if i := m.inClip(c.Hash); i >= 0 {
			m.clip = append(m.clip[:i], m.clip[i+1:]...)
		} else {
			m.clipAdd(c)
		}
		return nil, true
	case "V":
		if len(m.clip) == 0 {
			return nil, true
		}
		hashes := m.clipHashes()
		m.confirm("Cherry-pick", "Cherry-pick "+plural(len(hashes), "commit", "commits")+" onto "+m.head.Branch+"?", func() tea.Cmd {
			m.clip = nil
			return m.do(func() error { return m.repo.CherryPick(hashes...) })
		})
		return nil, true
	case "t":
		m.revertCommit(c)
		return nil, true
	case "i":
		return m.openTodo(c), true
	case "ctrl+g":
		p.graph = !p.graph
		return nil, true
	case "enter":
		if !ok {
			return nil, true
		}
		p.sub = &commitSub{hash: c.Hash}
		repo, hash := m.repo, c.Hash
		return func() tea.Msg {
			fs, err := repo.CommitFiles(hash)
			return commitFilesMsg{hash, fs, err}
		}, true
	case "g":
		if !ok {
			return nil, true
		}
		reset := func(mode string) tea.Cmd {
			return m.do(func() error { return m.repo.Reset(c.Hash, mode) })
		}
		m.popup = newMenu("Reset to "+c.Short, []menuItem{
			{"s", "Soft (keep index and files)", func() tea.Cmd { return reset("soft") }},
			{"m", "Mixed (keep files)", func() tea.Cmd { return reset("mixed") }},
			{"h", "Hard (discard everything)", func() tea.Cmd {
				m.confirm("Hard reset", "Hard reset to "+c.Short+"?\nUncommitted changes are lost.", func() tea.Cmd { return reset("hard") })
				return nil
			}},
		})
		return nil, true
	case "A":
		m.confirm("Amend", "Amend HEAD with the staged changes?", func() tea.Cmd {
			return m.do(func() error { return m.repo.Commit("", true) })
		})
		return nil, true
	}
	return nil, false
}

// ago formats a time as a short relative age.
func ago(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return itoa(int(d.Minutes())) + "m ago"
	case d < 48*time.Hour:
		return itoa(int(d.Hours())) + "h ago"
	}
	return itoa(int(d.Hours()/24)) + "d ago"
}
