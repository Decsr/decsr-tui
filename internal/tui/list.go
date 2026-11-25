package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Decsr/decsr-tui/internal/ui"
)

// item is one row of a panel list. plain is what a filter matches; styled is
// the row as drawn (the selected row keeps its colours on the Sel background).
type item struct {
	key, plain, styled string
}

// list is the cursor state of one panel list. The selection is kept by key,
// so a refresh that reorders rows does not move it.
type list struct {
	cur, off int
	key      string
	filter   string
}

func filterItems(its []item, filter string) []item {
	if filter == "" {
		return its
	}
	f := strings.ToLower(filter)
	var out []item
	for _, it := range its {
		if strings.Contains(strings.ToLower(it.plain), f) {
			out = append(out, it)
		}
	}
	return out
}

// sync moves the cursor to the remembered key, or clamps it.
func (l *list) sync(its []item) {
	if l.key != "" {
		for i, it := range its {
			if it.key == l.key {
				l.cur = i
				return
			}
		}
	}
	l.cur = max(0, min(l.cur, len(its)-1))
	if len(its) > 0 {
		l.key = its[l.cur].key
	} else {
		l.key = ""
	}
}

func (l *list) moveTo(its []item, i int) {
	if len(its) == 0 {
		return
	}
	l.cur = max(0, min(i, len(its)-1))
	l.key = its[l.cur].key
}

// selected returns the key of the selected row ("" when empty).
func (l *list) selected(its []item) (item, bool) {
	if l.cur >= 0 && l.cur < len(its) {
		return its[l.cur], true
	}
	return item{}, false
}

// navKey handles the movement keys shared by every list.
func (l *list) navKey(k string, its []item, page int) bool {
	switch k {
	case "j", "down":
		l.moveTo(its, l.cur+1)
	case "k", "up":
		l.moveTo(its, l.cur-1)
	case "home":
		l.moveTo(its, 0)
	case "G", "end":
		l.moveTo(its, len(its)-1)
	case "J":
		l.moveTo(its, l.cur+max(page, 1))
	case "K":
		l.moveTo(its, l.cur-max(page, 1))
	default:
		return false
	}
	return true
}

// / window keeps the cursor inside the h visible rows and returns the first one.
func (l *list) window(n, h int) int {
	l.off = max(0, min(l.off, n-h))
	if l.cur < l.off {
		l.off = l.cur
	}
	if h > 0 && l.cur >= l.off+h {
		l.off = l.cur - h + 1
	}
	return l.off
}

// rows draws every row at w cells: a cursor column, then the row. The
// selected row is filled with the Sel background and has the Accent "▌"
// cursor of a decsr list (Muted when the panel does not have focus).
func (l *list) rows(its []item, w int, focused bool) []string {
	out := make([]string, len(its))
	for i, it := range its {
		if i != l.cur {
			out[i] = " " + fit(it.styled, w-1)
			continue
		}
		c := ui.Muted
		var cc lipgloss.TerminalColor = c
		if focused {
			cc = ui.Accent
		}
		out[i] = lipgloss.NewStyle().Background(ui.Sel).Foreground(cc).Render("▌") + withBg(it.styled, ui.Sel, w-1)
	}
	return out
}
