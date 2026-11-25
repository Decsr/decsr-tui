package tui

import (
	"fmt"
	"strings"
	"text/template"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Decsr/decsr-tui/internal/config"
)

// customDoneMsg is the end of a custom command that ran in the background.
type customDoneMsg struct {
	cc  config.Custom
	cmd string
	out string
	err error
}

// tmplData is what a custom command template can read.
type tmplData struct {
	SelectedFile, SelectedCommit, SelectedBranch, SelectedTag, SelectedWorktree string
	CheckedOutBranch, RepoDir                                                   string
	Form                                                                        map[string]string
}

// context is the custom-command context of the focused panel.
func (m *Model) context() string {
	switch m.focus {
	case pFiles:
		return "files"
	case pBranches:
		switch m.branches.tab {
		case tabTags:
			return "tags"
		case tabWorktrees:
			return "worktrees"
		}
		return "branches"
	case pCommits:
		return "commits"
	case pStash:
		return "stash"
	case pAgent:
		return "agent"
	}
	return "global"
}

// customHere lists the custom commands active in the focused panel.
func (m *Model) customHere() []config.Custom {
	ctx := m.context()
	var out []config.Custom
	for _, cc := range m.cfg.CustomCommands {
		if cc.Context == ctx || cc.Context == "global" {
			out = append(out, cc)
		}
	}
	return out
}

// customFor finds the command bound to key k; the panel's own context wins over global.
func (m *Model) customFor(k string) (config.Custom, bool) {
	var global *config.Custom
	for i, cc := range m.cfg.CustomCommands {
		if cc.Key != k {
			continue
		}
		if cc.Context == m.context() {
			return cc, true
		}
		if cc.Context == "global" && global == nil {
			global = &m.cfg.CustomCommands[i]
		}
	}
	if global != nil {
		return *global, true
	}
	return config.Custom{}, false
}

func (m *Model) tmplData() tmplData {
	d := tmplData{CheckedOutBranch: m.head.Branch, RepoDir: m.repo.Dir, Form: map[string]string{}}
	if _, path, _, ok := m.selFile(); ok {
		d.SelectedFile = path
	}
	if c, ok := m.selCommit(); ok {
		d.SelectedCommit = c.Hash
	}
	switch m.branches.tab {
	case tabLocal, tabRemotes:
		if b, ok := m.selBranch(); ok {
			d.SelectedBranch = b.Name
		}
	case tabTags:
		if t, ok := m.selTag(); ok {
			d.SelectedTag = t.Name
		}
	case tabWorktrees:
		if w, ok := m.selWorktree(); ok {
			d.SelectedWorktree = w.Path
		}
	}
	return d
}

func render(cc config.Custom, d tmplData) (string, error) {
	t, err := template.New("cmd").Funcs(template.FuncMap{"quote": shellQuote}).Option("missingkey=error").Parse(cc.Command)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	if err := t.Execute(&sb, d); err != nil {
		return "", err
	}
	return sb.String(), nil
}

// runCustom asks the prompts, renders the command, confirms and runs it.
func (m *Model) runCustom(cc config.Custom) tea.Cmd {
	d := m.tmplData()
	if _, err := render(cc, d); err != nil && len(cc.Prompts) == 0 {
		m.errorPopup(fmt.Errorf("custom command %q: %w", cc.Description, err))
		return nil
	}
	var ask func(i int) tea.Cmd
	ask = func(i int) tea.Cmd {
		if i < len(cc.Prompts) {
			p := cc.Prompts[i]
			title := p.Title
			if title == "" {
				title = p.Name
			}
			m.popup = newInput(title, "", func(v string) tea.Cmd {
				d.Form[p.Name] = v
				return ask(i + 1)
			})
			return nil
		}
		cmd, err := render(cc, d)
		if err != nil {
			m.errorPopup(fmt.Errorf("custom command %q: %w", cc.Description, err))
			return nil
		}
		if cc.Confirm {
			m.confirm(cc.Description, "Run:\n"+cmd, func() tea.Cmd { return m.execCustom(cc, cmd) })
			return nil
		}
		return m.execCustom(cc, cmd)
	}
	return ask(0)
}

func (m *Model) execCustom(cc config.Custom, command string) tea.Cmd {
	c := shellCommand(command)
	c.Dir = m.repo.Dir
	m.log.add("$ " + command)
	if cc.Output == "terminal" {
		return tea.ExecProcess(c, func(err error) tea.Msg { return editorDoneMsg{err} })
	}
	return func() tea.Msg {
		out, err := c.CombinedOutput()
		return customDoneMsg{cc, command, strings.TrimRight(string(out), "\r\n "), err}
	}
}

func (m *Model) customDone(msg customDoneMsg) tea.Cmd {
	lines := strings.Split(msg.out, "\n")
	if len(lines) > 50 {
		lines = lines[len(lines)-50:]
	}
	if msg.out != "" {
		for _, l := range lines {
			m.log.add(strings.TrimRight(l, "\r"))
		}
	}
	if msg.err != nil {
		m.log.add("error: " + msg.err.Error())
	}
	switch {
	case msg.cc.Output == "popup":
		body := msg.out
		if msg.err != nil {
			body += "\n\n" + msg.err.Error()
		}
		if strings.TrimSpace(body) == "" {
			body = "(no output)"
		}
		m.popup = &popup{kind: pkError, title: msg.cc.Description, body: body}
	}
	return m.loadCmd(true)
}
