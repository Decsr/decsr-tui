package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Decsr/decsr-tui/internal/agent"
	"github.com/Decsr/decsr-tui/internal/ui"
)

const (
	thinkingPeek   = 60 // first characters of a thought on its line
	actDetailLines = 3  // wrapped detail lines of an expanded item in a card
)

// ── Formatting ───────────────────────────────────────────────────────────────

// actGlyph is the kind glyph of an activity item.
func actGlyph(k agent.Kind) string {
	switch k {
	case agent.KindCommand:
		return "▸"
	case agent.KindEdit, agent.KindWrite:
		return "✎"
	case agent.KindRead:
		return "◦"
	case agent.KindSearch:
		return "⌕"
	case agent.KindTool:
		return "⚙"
	case agent.KindSubagent:
		return "◈"
	case agent.KindUser:
		return "❯"
	case agent.KindError:
		return "!"
	case agent.KindMessage:
		return "›"
	}
	return "·"
}

// actDuration is "3.2s" under ten seconds, else a compact duration.
func actDuration(d time.Duration) string {
	if d < 10*time.Second {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return feedDuration(d)
}

// midPath shortens a path to w cells from the left at a separator when it can:
// "…/internal/x.go".
func midPath(p string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(p) <= w {
		return p
	}
	if w <= 2 {
		return tailPath(p, w)
	}
	tail := ansi.TruncateLeft(p, ansi.StringWidth(p)-(w-2), "")
	if i := strings.IndexByte(tail, '/'); i >= 0 && i+1 < len(tail) {
		return "…/" + tail[i+1:]
	}
	return tailPath(p, w)
}

// clipEnd cuts s to w cells with "…" at the end.
func clipEnd(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return ansi.Truncate(s, w, "…")
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return oneLine(sanitizeText(s))
}

// actText is the text of an item without glyph and meta, cut to w cells.
// compact drops the verbs ("Edited") and quotes.
func actText(a agent.Activity, w int, compact bool) string {
	title := oneLine(sanitizeText(a.Title))
	switch a.Kind {
	case agent.KindEdit, agent.KindWrite:
		verb := "Edited "
		if a.Kind == agent.KindWrite {
			verb = "Added "
		}
		if compact {
			verb = ""
		}
		return verb + midPath(title, w-len(verb))
	case agent.KindRead:
		verb := "Read "
		if compact {
			verb = ""
		}
		return verb + midPath(title, w-len(verb))
	case agent.KindSearch:
		if compact {
			return clipEnd(title, w)
		}
		return clipEnd(`Searched "`+title+`"`, w)
	case agent.KindSubagent:
		if compact {
			return clipEnd(title, w)
		}
		return clipEnd("Subagent · "+title, w)
	case agent.KindUser:
		if title == "" {
			title = firstLine(a.Detail)
		}
		return clipEnd(title, w)
	case agent.KindThinking:
		peek := firstLine(a.Detail)
		if peek == "" {
			peek = title
		}
		if compact || peek == "" {
			return clipEnd("thinking…", w)
		}
		if utf8.RuneCountInString(peek) > thinkingPeek {
			peek = string([]rune(peek)[:thinkingPeek-1]) + "…"
		}
		return clipEnd(`thinking… "`+peek+`"`, w)
	case agent.KindMessage:
		t := title
		if t == "" {
			t = firstLine(a.Detail)
		}
		return clipEnd(t, w)
	}
	return clipEnd(title, w)
}

// actMeta is the right-aligned meta of an item and its colours: plain text
// and the rendered form.
func actMeta(a agent.Activity, now time.Time) (plain, styled string) {
	dim := lipgloss.NewStyle().Foreground(ui.Muted)
	ok := lipgloss.NewStyle().Foreground(ui.OK)
	fail := lipgloss.NewStyle().Foreground(ui.Fail)
	switch a.Kind {
	case agent.KindEdit, agent.KindWrite:
		var pp, ps []string
		if a.Added > 0 {
			pp = append(pp, fmt.Sprintf("+%d", a.Added))
			ps = append(ps, ok.Render(fmt.Sprintf("+%d", a.Added)))
		}
		if a.Removed > 0 {
			pp = append(pp, fmt.Sprintf("−%d", a.Removed))
			ps = append(ps, fail.Render(fmt.Sprintf("−%d", a.Removed)))
		}
		if len(pp) > 0 {
			return strings.Join(pp, " "), strings.Join(ps, " ")
		}
		if a.State == agent.StateFailed {
			return "✗", fail.Render("✗")
		}
		return "", ""
	case agent.KindCommand:
		switch a.State {
		case agent.StateRunning:
			if a.At.IsZero() {
				return "", ""
			}
			s := feedDuration(now.Sub(a.At))
			return s, dim.Render(s)
		case agent.StateFailed:
			s := "✗"
			if a.Duration > 0 {
				s += " · " + actDuration(a.Duration)
			}
			return s, fail.Render(s)
		}
		s := "✓"
		if a.Duration > 0 {
			s += " " + actDuration(a.Duration)
		}
		return s, dim.Render(s)
	}
	if a.State == agent.StateFailed {
		return "✗", fail.Render("✗")
	}
	return "", ""
}

// actColor is the colour of the glyph and text of an item.
func actColor(a agent.Activity) lipgloss.TerminalColor {
	switch {
	case a.Kind == agent.KindError || a.State == agent.StateFailed:
		return ui.Fail
	case a.Kind == agent.KindThinking || a.Kind == agent.KindMessage || a.Kind == agent.KindUser:
		return ui.Muted
	case a.State == agent.StateRunning:
		return ui.Accent
	}
	return ui.Text
}

// activityLine renders one item as glyph, text and right-aligned dim meta in
// at most w cells (exactly w when there is meta).
func activityLine(a agent.Activity, now time.Time, w int) string {
	if w < 3 {
		return clipEnd(actGlyph(a.Kind), w)
	}
	plain, styled := actMeta(a, now)
	mw := ansi.StringWidth(plain)
	if mw > 0 && w-2-mw-2 < 8 { // no room for the text: drop the meta
		styled, mw = "", 0
	}
	room := w - 2
	if mw > 0 {
		room -= mw + 1
	}
	text := actText(a, room, false)
	col := actColor(a)
	st := lipgloss.NewStyle().Foreground(col)
	if a.Kind == agent.KindThinking || a.Kind == agent.KindMessage || a.Kind == agent.KindUser {
		st = lipgloss.NewStyle().Foreground(ui.Muted)
	}
	left := st.Render(actGlyph(a.Kind)) + " " + st.Render(text)
	if mw == 0 {
		return left
	}
	gap := max(w-ansi.StringWidth(left)-mw, 1)
	return left + strings.Repeat(" ", gap) + styled
}

// activityDetailLines is the wrapped detail of an item, indented by four.
func activityDetailLines(detail string, w, limit int) []string {
	detail = strings.TrimSpace(sanitizeText(detail))
	if detail == "" || w < 8 {
		return nil
	}
	dim := lipgloss.NewStyle().Foreground(ui.Muted)
	var out []string
	for _, l := range hardWrap(detail, w-4) {
		if limit > 0 && len(out) == limit {
			out[len(out)-1] = dim.Render("    " + clipEnd(strings.TrimSpace(l)+" …", w-4))
			break
		}
		out = append(out, dim.Render("    "+l))
	}
	return out
}

// activityMessageLines is the detail of a finished message item as Markdown,
// indented by four and cut to limit lines; it falls back to activityDetailLines
// when Markdown is off or the text is still running.
func activityMessageLines(detail string, w, limit int, running bool) []string {
	detail = strings.TrimSpace(sanitizeText(detail))
	if running || detail == "" {
		return activityDetailLines(detail, w, limit)
	}
	md, ok := renderMarkdown(detail, w-4)
	if !ok {
		return activityDetailLines(detail, w, limit)
	}
	if limit > 0 && len(md) > limit {
		md = md[:limit]
		md[limit-1] = ansi.Truncate(md[limit-1], max(w-6, 1), "") + lipgloss.NewStyle().Foreground(ui.Muted).Render(" …")
	}
	out := make([]string, len(md))
	for i, l := range md {
		out[i] = "    " + l
	}
	return out
}
