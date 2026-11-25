package tui

import (
	"strings"

	"github.com/Decsr/decsr-tui/internal/ui"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// StatusBarModel renders a single-line status bar at the bottom.
type StatusBarModel struct {
	branch       string
	state        string
	stageInfo    string // scrolling info text next to the state
	hints        []ui.Hint
	width        int
	scrollOffset int // marquee scroll position for stageInfo
}

func NewStatusBar(width int) StatusBarModel {
	return StatusBarModel{width: width, state: "idle"}
}

func (m StatusBarModel) WithBranch(b string) StatusBarModel { m.branch = b; return m }
func (m StatusBarModel) WithState(s string) StatusBarModel  { m.state = s; return m }
func (m StatusBarModel) WithStageInfo(s string) StatusBarModel {
	m.stageInfo = s
	m.scrollOffset = 0
	return m
}
func (m StatusBarModel) WithWidth(w int) StatusBarModel { m.width = w; return m }

// WithHints sets the key hints shown on the right.
func (m StatusBarModel) WithHints(h []ui.Hint) StatusBarModel { m.hints = h; return m }

// AdvanceScroll moves the marquee scroll position forward by one.
func (m StatusBarModel) AdvanceScroll() StatusBarModel {
	m.scrollOffset++
	return m
}

// Text styles of the status bar: no background, Muted for the frame of the
// line and Text for the live values.
var (
	statusBarText = lipgloss.NewStyle().Foreground(ui.Muted).Padding(0, 1)
	statusBarKey  = lipgloss.NewStyle().Foreground(ui.Text).Bold(true).Padding(0, 1)
)

func (m StatusBarModel) View() string {
	return strings.Join(m.viewLines(), "\n")
}

// Height returns the number of terminal rows View() will occupy at the current
// width (1 normally, more when the shortcuts must wrap).
func (m StatusBarModel) Height() int {
	return len(m.viewLines())
}

// viewLines renders the status bar, wrapping the shortcut hints onto additional
// lines when the left status segment plus the hints do not fit on one line —
// instead of letting the hints overflow past the right edge.
func (m StatusBarModel) viewLines() []string {
	descStyle := lipgloss.NewStyle().Foreground(ui.Muted)

	var hints []string
	for _, h := range m.hints {
		keyStyle := lipgloss.NewStyle().Foreground(ui.Accent).Bold(true)
		hints = append(hints, keyStyle.Render(" "+h.Key+" ")+descStyle.Render(h.Label))
	}

	rightSide := strings.Join(hints, " ")
	rightWidth := lipglossLen(rightSide)

	// ── Left side: fixed prefix + scrollable stageInfo + fixed suffix ──
	prefix := ""
	if m.branch != "" {
		prefix = lipgloss.NewStyle().Foreground(ui.Accent).Bold(true).Render(m.branch) + statusBarText.Render(" ")
	}
	prefix += statusBarText.Render("● " + m.state)

	suffix := ""

	prefixWidth := lipglossLen(prefix)
	suffixWidth := lipglossLen(suffix)
	separatorWidth := 2 // "  " between prefix and stageInfo

	availableForStage := m.width - rightWidth - prefixWidth - suffixWidth - separatorWidth - 2
	stageRendered := ""
	if m.stageInfo != "" && availableForStage > 4 {
		stageRendered = "  " + m.renderScrollingStage(availableForStage)
	}

	left := prefix + stageRendered + suffix
	leftWidth := lipglossLen(left)

	// Single line when everything fits.
	if m.width <= 0 || leftWidth+2+rightWidth <= m.width {
		gap := m.width - leftWidth - rightWidth
		if gap < 0 {
			gap = 0
		}
		return []string{left + strings.Repeat(" ", gap) + rightSide}
	}

	// Otherwise: left on its own line, then wrap the hints (+ version) so they
	// stay within the terminal width instead of overflowing the right edge.
	lines := []string{left}
	tokens := hints
	lines = append(lines, wrapStatusTokens(tokens, m.width)...)
	return lines
}

// wrapStatusTokens greedily packs pre-rendered tokens (joined by two spaces)
// into lines no wider than maxWidth.
func wrapStatusTokens(tokens []string, maxWidth int) []string {
	var lines []string
	cur := ""
	curWidth := 0
	for _, tok := range tokens {
		tw := lipglossLen(tok)
		if cur == "" {
			cur = tok
			curWidth = tw
			continue
		}
		if curWidth+2+tw > maxWidth {
			lines = append(lines, cur)
			cur = tok
			curWidth = tw
			continue
		}
		cur += "  " + tok
		curWidth += 2 + tw
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

// renderScrollingStage returns the stageInfo text, applying marquee scrolling
// if it exceeds the available width.
func (m StatusBarModel) renderScrollingStage(availableWidth int) string {
	label := "▸ " + m.stageInfo
	runes := []rune(label)

	if len(runes) <= availableWidth {
		return statusBarKey.Render(label)
	}

	separator := []rune("   ···   ")
	cycle := append(runes, separator...)
	cycleLen := len(cycle)

	visible := make([]rune, availableWidth)
	for i := 0; i < availableWidth; i++ {
		visible[i] = cycle[(m.scrollOffset+i)%cycleLen]
	}

	return statusBarKey.Render(string(visible))
}

// lipglossLen returns the visible cell width of a rendered string (ANSI-aware,
// wide-glyph aware).
func lipglossLen(s string) int { return ansi.StringWidth(s) }
