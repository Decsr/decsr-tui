package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// sanitizeText removes terminal escape sequences and control characters from
// untrusted text. Newlines are kept and tabs become spaces.
func sanitizeText(s string) string {
	if strings.ContainsRune(s, '\x1b') {
		s = ansi.Strip(s)
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\t", "    ")
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n':
			return r
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f):
			return -1
		}
		return r
	}, s)
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// feedDuration is a compact duration: "37s", "2m", "1m 10s", "1h 5m".
func feedDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	h, m, s := int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60
	switch {
	case h > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	case m > 0 && s == 0:
		return fmt.Sprintf("%dm", m)
	case m > 0:
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func wrapText(s string, w int) []string {
	if w < 8 {
		w = 8
	}
	var lines []string
	var cur string
	for _, word := range strings.Fields(s) {
		if cur == "" {
			cur = word
			continue
		}
		if ansi.StringWidth(cur)+1+ansi.StringWidth(word) > w {
			lines = append(lines, cur)
			cur = word
		} else {
			cur += " " + word
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

// hardWrap wraps s into lines of at most w cells; a word longer than w is
// split.
func hardWrap(s string, w int) []string {
	if w < 1 {
		return nil
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		wrapped := wrapText(para, w)
		if len(wrapped) == 0 {
			out = append(out, "")
			continue
		}
		for _, l := range wrapped {
			if ansi.StringWidth(l) <= w {
				out = append(out, l)
				continue
			}
			out = append(out, strings.Split(ansi.Hardwrap(l, w, true), "\n")...)
		}
	}
	return out
}

// tailPath shortens a path from the left to w cells: "…ts/version.go".
func tailPath(p string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(p) <= w {
		return p
	}
	if w == 1 {
		return "…"
	}
	return "…" + ansi.TruncateLeft(p, ansi.StringWidth(p)-(w-1), "")
}

func pluralize(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func truncateOneLine(s string, w int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if w <= 0 {
		return ""
	}
	return ansi.Truncate(s, w, "…")
}
