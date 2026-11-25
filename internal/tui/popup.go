package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Decsr/decsr-tui/internal/ui"
)

type popupKind int

const (
	pkConfirm popupKind = iota
	pkInput
	pkCommit
	pkMenu
	pkError
	pkHelp
)

type menuItem struct {
	key   string
	label string
	fn    func() tea.Cmd
}

type popup struct {
	kind  popupKind
	title string
	body  string

	in textinput.Model
	ta textarea.Model

	items  []menuItem
	cur    int
	scroll int // first visible row of the help list

	yes    func() tea.Cmd
	submit func(text string) tea.Cmd
}

func newError(msg string) *popup {
	return &popup{kind: pkError, title: "Error", body: msg}
}

func newInput(title, value string, submit func(string) tea.Cmd) *popup {
	in := textinput.New()
	in.Prompt = ""
	in.Cursor.SetMode(cursor.CursorStatic)
	in.SetValue(value)
	in.CursorEnd()
	in.Focus()
	return &popup{kind: pkInput, title: title, in: in, submit: submit}
}

// newCommitInput is the multi-line message box; the first line is the summary.
func newCommitInput(title, value string, submit func(string) tea.Cmd) *popup {
	ta := textarea.New()
	ta.Prompt = ""
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.Cursor.SetMode(cursor.CursorStatic)
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("ctrl+j", "alt+enter"))
	ta.SetWidth(56)
	ta.SetHeight(6)
	ta.SetValue(value)
	ta.Focus()
	return &popup{kind: pkCommit, title: title, ta: ta, submit: submit}
}

func newMenu(title string, items []menuItem) *popup {
	return &popup{kind: pkMenu, title: title, items: items}
}

// popupKey handles a key while a popup is open.
func (m *Model) popupKey(msg tea.KeyMsg) tea.Cmd {
	p := m.popup
	k := keyStr(msg)
	switch p.kind {
	case pkConfirm:
		switch k {
		case "y", "enter":
			m.popup = nil
			if p.yes != nil {
				return p.yes()
			}
		case "n", "esc", "q":
			m.popup = nil
		}
	case pkInput:
		switch k {
		case "enter":
			m.popup = nil
			return p.submit(strings.TrimSpace(p.in.Value()))
		case "esc":
			m.popup = nil
		default:
			p.in, _ = p.in.Update(msg)
		}
	case pkCommit:
		switch k {
		case "enter":
			m.popup = nil
			return p.submit(strings.TrimSpace(p.ta.Value()))
		case "esc":
			m.popup = nil
		default:
			p.ta, _ = p.ta.Update(msg)
		}
	case pkMenu:
		switch k {
		case "j", "down":
			p.cur = min(p.cur+1, len(p.items)-1)
		case "k", "up":
			p.cur = max(p.cur-1, 0)
		case "enter":
			if p.cur < len(p.items) {
				it := p.items[p.cur]
				m.popup = nil
				return it.fn()
			}
		case "esc", "q":
			m.popup = nil
		default:
			for _, it := range p.items {
				if it.key != "" && it.key == k {
					m.popup = nil
					return it.fn()
				}
			}
		}
	case pkHelp: // j k and the page keys scroll; any other key closes
		switch k {
		case "j", "down":
			p.scroll++
		case "k", "up":
			p.scroll = max(p.scroll-1, 0)
		case "pgdown", "ctrl+d", " ":
			p.scroll += 8
		case "pgup", "ctrl+u":
			p.scroll = max(p.scroll-8, 0)
		default:
			m.popup = nil
		}
	default: // error closes on any key
		m.popup = nil
	}
	return nil
}

// / color is the border colour of the popup: Warn asks, Fail reports, the rest
// takes a group colour of the decsr palette.
func (p *popup) color() lipgloss.TerminalColor {
	switch p.kind {
	case pkConfirm:
		return ui.Warn
	case pkError:
		return ui.Fail
	case pkMenu:
		return ui.GroupRun
	case pkHelp:
		return ui.GroupSession
	}
	return ui.Accent
}

// popupBox draws the popup for a screen of w x h: a compact ui.Overlay, or
// the fixed ui.Panel frame for the long help list. The caller places it with
// ui.PlaceOverlay.
func (m *Model) popupBox(w, h int) string {
	p := m.popup
	if p.kind == pkHelp {
		lines := strings.Split(p.body, "\n")
		return ui.Panel(ui.PanelOpts{
			Title: p.title, Color: p.color(), Body: lines,
			Keys: []ui.Hint{{Key: "any key", Label: "close"}}, ScreenW: w, ScreenH: h,
		})
	}
	pw := min(max(w-4, 10), 60)
	if p.kind == pkError {
		pw = min(max(w-4, 10), 76)
	}
	inner := pw - 4
	var body []string
	var hints []ui.Hint
	switch p.kind {
	case pkConfirm:
		body = append(body, hardWrapText(p.body, inner)...)
		hints = []ui.Hint{{Key: "y", Label: "yes"}, {Key: "n", Label: "no"}}
	case pkInput:
		if p.body != "" {
			body = append(hardWrapText(p.body, inner), "")
		}
		body = append(body, p.in.View())
		hints = []ui.Hint{{Key: "enter", Label: "ok"}, {Key: "esc", Label: "cancel"}}
	case pkCommit:
		p.ta.SetWidth(max(inner-1, 4))
		body = strings.Split(p.ta.View(), "\n")
		hints = []ui.Hint{{Key: "enter", Label: "commit"}, {Key: "ctrl+j", Label: "newline"}, {Key: "esc", Label: "cancel"}}
	case pkMenu:
		for i, it := range p.items {
			label := sText.Render(it.label)
			if it.key != "" {
				label = lipgloss.NewStyle().Foreground(ui.Accent).Bold(true).Render(it.key) + " " + label
			}
			if i == p.cur {
				body = append(body, lipgloss.NewStyle().Background(ui.Sel).Foreground(ui.Accent).Render("▌")+withBg(label, ui.Sel, inner-1))
			} else {
				body = append(body, " "+label)
			}
		}
		hints = []ui.Hint{{Key: "enter", Label: "select"}, {Key: "esc", Label: "close"}}
	case pkError:
		body = hardWrapText(p.body, inner)
		hints = []ui.Hint{{Key: "any key", Label: "close"}}
	}
	maxBody := max(h-6, 1)
	if len(body) > maxBody {
		if p.kind == pkMenu { // keep the selected row on screen
			from := min(max(p.cur-maxBody+1, 0), len(body)-maxBody)
			body = body[from : from+maxBody]
		} else {
			body = body[:maxBody]
		}
	}
	return ui.Overlay(p.title, body, hints, pw, p.color())
}

// hardWrapText wraps text to w cells in the Text colour, keeping blank lines.
func hardWrapText(text string, w int) []string {
	var out []string
	for _, l := range hardWrap(sanitizeText(text), w) {
		out = append(out, sText.Render(l))
	}
	return out
}
