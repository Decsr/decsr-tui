package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Decsr/decsr-tui/internal/ui"
)

// pixelSpinner is a quadrant-block spinner (the braille spinner.Dot frames are
// missing from Monaspace Krypton).
var pixelSpinner = spinner.Spinner{
	Frames: []string{"▘", "▝", "▗", "▖"},
	FPS:    time.Second / 8,
}

// paintBackground fills the whole width x height screen with the dark base
// colour of the theme (the dark PanelInk), as decsr's Home does. Inner styled
// segments end with an SGR reset that would drop the terminal back to its
// default background, so the background is re-applied after every reset.
// Returns view unchanged when the size is unknown or the profile has no colour.
func paintBackground(view string, width, height int) string {
	if width <= 0 || height <= 0 {
		return view
	}
	// Render a marker to obtain the profile-aware background sequence.
	seq, _, ok := strings.Cut(lipgloss.NewStyle().Background(ui.Dark(ui.PanelInk)).Render("x"), "x")
	if !ok || seq == "" {
		return view
	}
	const reset = "\x1b[0m"
	lines := strings.Split(view, "\n")
	var b strings.Builder
	for i := 0; i < height || i < len(lines); i++ {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(seq)
		for _, r := range []string{"\x1b[0m", "\x1b[m", "\x1b[49m"} {
			line = strings.ReplaceAll(line, r, r+seq)
		}
		b.WriteString(line)
		if pad := width - ansi.StringWidth(line); pad > 0 {
			b.WriteString(strings.Repeat(" ", pad))
		}
		b.WriteString(reset)
	}
	return b.String()
}

// keycap renders a key on a slightly raised (Sel) background: a pill.
func keycap(k string) string {
	return lipgloss.NewStyle().Background(ui.Sel).Foreground(ui.Text).Bold(true).Render(" " + k + " ")
}

// chip renders a small coloured chip: ink on an accent block.
func chip(label string, color lipgloss.TerminalColor) string {
	return lipgloss.NewStyle().Background(color).Foreground(ui.PanelInk).Bold(true).Render(" " + label + " ")
}

// panelTop builds the top border line of a pixel box with the title embedded:
// ┌─ ◆ TITLE ────────┐ (w is the full box width including corners).
func panelTop(w int, title string, titleColor, borderColor lipgloss.TerminalColor) string {
	bs := lipgloss.NewStyle().Foreground(borderColor)
	if w < 2 {
		return ""
	}
	if title == "" || w < 8 {
		return bs.Render("┌" + strings.Repeat("─", w-2) + "┐")
	}
	t := ansi.Truncate(title, w-6, "…")
	label := " " + t + " "
	fill := max(w-3-ansi.StringWidth(label), 0)
	return bs.Render("┌─") +
		lipgloss.NewStyle().Foreground(titleColor).Bold(true).Render(label) +
		bs.Render(strings.Repeat("─", fill)+"┐")
}

// pixelBox draws body inside a square-cornered box of exactly w x h cells with
// the title in its top edge, as decsr's Home does. A focused box's border takes
// the colour of its title.
func pixelBox(title string, titleColor lipgloss.TerminalColor, focused bool, body []string, w, h int) []string {
	if w < 6 || h < 2 {
		return nil
	}
	var bc lipgloss.TerminalColor = ui.Border
	if focused {
		bc = titleColor
	}
	bs := lipgloss.NewStyle().Foreground(bc)
	out := []string{panelTop(w, title, titleColor, bc)}
	for i := 0; i < h-2; i++ {
		l := ""
		if i < len(body) {
			l = body[i]
		}
		out = append(out, bs.Render("│")+" "+fit(l, w-4)+" "+bs.Render("│"))
	}
	return append(out, bs.Render("└"+strings.Repeat("─", w-2)+"┘"))
}
