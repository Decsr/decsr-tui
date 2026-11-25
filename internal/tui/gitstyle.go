package tui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Decsr/decsr-tui/internal/ui"
)

// Colour roles of the git panels. Every one is a ui token, so the theme
// switch (ui.Apply) restyles them all.
var (
	sGreen  = lipgloss.NewStyle().Foreground(ui.OK)           // staged, added, current
	sRed    = lipgloss.NewStyle().Foreground(ui.Fail)         // unstaged, removed, conflicts
	sYellow = lipgloss.NewStyle().Foreground(ui.Warn)         // hashes, tags, counters
	sBlue   = lipgloss.NewStyle().Foreground(ui.Accent)       // folders, focus
	sCyan   = lipgloss.NewStyle().Foreground(ui.GroupSession) // reword, hunk headers
	sMag    = lipgloss.NewStyle().Foreground(ui.GroupRun)     // squash, fixup
	sDim    = lipgloss.NewStyle().Foreground(ui.Muted)        // metadata
	sText   = lipgloss.NewStyle().Foreground(ui.Text)
	sBold   = lipgloss.NewStyle().Foreground(ui.Text).Bold(true)
)

func itoa(n int) string { return strconv.Itoa(n) }

func lenW(s string) int { return ansi.StringWidth(s) }

func ansiTrunc(s string, n int) string {
	s = strings.ReplaceAll(expandTabs(s), "\n", " ")
	if lenW(s) <= n {
		return s
	}
	return ansi.Truncate(s, n, "…")
}

func expandTabs(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	return strings.ReplaceAll(s, "\t", "    ")
}

// fit pads or truncates s (which may hold ANSI codes) to exactly w cells.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return padRight(ansi.Truncate(s, w, ""), w)
}

// withBg draws s on the background c across exactly w cells. A styled segment
// ends with an SGR reset that would drop the background, so it is applied
// again after every reset (the way paintBackground does for the whole screen).
func withBg(s string, c lipgloss.TerminalColor, w int) string {
	s = fit(s, w)
	seq, _, ok := strings.Cut(lipgloss.NewStyle().Background(c).Render("x"), "x")
	if !ok || seq == "" {
		return s
	}
	for _, r := range []string{"\x1b[0m", "\x1b[m", "\x1b[49m"} {
		s = strings.ReplaceAll(s, r, r+seq)
	}
	return seq + s + "\x1b[0m"
}

// splitRow puts right at the right edge of a w-cell row; the left side is cut first.
func splitRow(left, right string, w int) string {
	rw := lenW(right)
	if rw >= w {
		return ansi.Truncate(right, w, "")
	}
	room := w - rw
	if rw > 0 {
		room--
	}
	left = ansi.Truncate(left, max(room, 0), "…")
	return left + strings.Repeat(" ", max(w-lenW(left)-rw, 0)) + right
}

// textLines wraps plain text to w cells, one Text-coloured line per row; a
// line that carries its own colours is left as it is.
func textLines(text string, w int) []string {
	var out []string
	for _, l := range strings.Split(strings.TrimRight(expandTabs(text), "\n"), "\n") {
		if l == "" {
			out = append(out, "")
			continue
		}
		for _, p := range strings.Split(ansi.Wrap(l, max(w, 1), ""), "\n") {
			if !strings.ContainsRune(p, '\x1b') {
				p = sText.Render(p)
			}
			out = append(out, p)
		}
	}
	return out
}
