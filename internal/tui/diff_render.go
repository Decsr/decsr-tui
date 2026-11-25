package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Decsr/decsr-tui/internal/git"
	"github.com/Decsr/decsr-tui/internal/ui"
)

// diffRow is one row of the diff of a file: a hunk header (line -1) or a line.
type diffRow struct{ hunk, line int }

// lineNo is the old and new line number of one diff line; 0 means none.
type lineNo struct{ old, new int }

// diffView renders one file of a unified diff: hunk headers, a two-column
// line-number gutter and syntax-coloured code on tinted rows. It holds only
// render state; the caller moves cur/top/hx.
type diffView struct {
	file git.FileDiff
	rows []diffRow
	nos  [][]lineNo // per hunk, per line
	cur  int        // cursor row
	top  int        // first visible row
	hx   int        // horizontal scroll in cells
	hl   *fileHL
	err  string // load error, shown instead of the diff

	static   bool         // no cursor: the view only shows the diff
	selOn    bool         // rows of selHunk are marked as the selection
	selHunk  int          // the selected hunk
	selLines map[int]bool // its selected lines; nil selects the whole hunk
}

// newDiffView prepares f for rendering.
func newDiffView(f git.FileDiff) *diffView {
	return &diffView{file: f, rows: diffRows(f), nos: diffLineNos(f)}
}

// diffRows lists the rows of f: each hunk header followed by its lines.
func diffRows(f git.FileDiff) []diffRow {
	var rows []diffRow
	for h, hk := range f.Hunks {
		rows = append(rows, diffRow{hunk: h, line: -1})
		for l := range hk.Lines {
			rows = append(rows, diffRow{hunk: h, line: l})
		}
	}
	return rows
}

// diffLineNos derives the old and new line number of every line from the hunk
// start positions.
func diffLineNos(f git.FileDiff) [][]lineNo {
	out := make([][]lineNo, len(f.Hunks))
	for h, hk := range f.Hunks {
		o, n := hk.OldStart, hk.NewStart
		out[h] = make([]lineNo, len(hk.Lines))
		for i, l := range hk.Lines {
			switch l.Kind {
			case '+':
				out[h][i] = lineNo{new: n}
				n++
			case '-':
				out[h][i] = lineNo{old: o}
				o++
			case '\\':
			default:
				out[h][i] = lineNo{old: o, new: n}
				o++
				n++
			}
		}
	}
	return out
}

// fileCounts is the number of added and removed lines of f.
func fileCounts(f git.FileDiff) (added, removed int) {
	for _, hk := range f.Hunks {
		for _, l := range hk.Lines {
			switch l.Kind {
			case '+':
				added++
			case '-':
				removed++
			}
		}
	}
	return added, removed
}

func truncPlain(s string, w int) string { return ansi.Truncate(s, w, "…") }

func padRight(s string, w int) string {
	if d := w - ansi.StringWidth(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

// sanitizeCode makes one line of code safe to print: tabs become four spaces
// and control characters are dropped.
func sanitizeCode(s string) string {
	if !strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\t':
			b.WriteString("    ")
		case r < 0x20 || r == 0x7f:
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func diffCounts(added, removed int) string {
	ok := lipgloss.NewStyle().Foreground(ui.OK)
	bad := lipgloss.NewStyle().Foreground(ui.Fail)
	return ok.Render(fmt.Sprintf("+%d", added)) + " " + bad.Render(fmt.Sprintf("−%d", removed))
}

func digits(n int) int { return len(fmt.Sprint(n)) }

// render draws the visible rows at w cells and at most h rows, moving top so
// the cursor row stays inside the window.
func (v *diffView) render(w, h int) []string {
	f := v.file
	if len(v.rows) == 0 {
		msg := "No textual changes."
		switch {
		case f.Binary:
			msg = "Binary file."
		case f.OrigPath != "":
			msg = "Renamed from " + f.OrigPath + "."
		}
		return []string{"  " + lipgloss.NewStyle().Foreground(ui.Muted).Render(ansi.Truncate(msg, max(w-2, 0), "…"))}
	}
	maxNo := 0
	for _, hk := range v.nos {
		for _, n := range hk {
			maxNo = max(maxNo, max(n.old, n.new))
		}
	}
	nw := max(digits(maxNo), 3)
	v.cur = max(min(v.cur, len(v.rows)-1), 0)
	v.top = max(min(v.top, v.cur), 0)
	if v.cur-v.top >= h {
		v.top = v.cur - h + 1
	}
	var out []string
	for i := v.top; i < len(v.rows) && len(out) < h; i++ {
		out = append(out, v.renderRow(v.rows[i], i == v.cur, w, nw))
	}
	return out
}

// renderRow renders one row to exactly w cells. Added and removed rows are
// tinted over the whole width in one background so no stripes show.
func (v *diffView) renderRow(r diffRow, cursor bool, w, nw int) string {
	f := v.file
	gutter := 2*nw + 3
	cursor = cursor && !v.static
	picked := cursor || v.inSel(r)
	mark := " "
	switch {
	case cursor:
		mark = lipgloss.NewStyle().Foreground(ui.Accent).Render("▌")
	case picked:
		mark = lipgloss.NewStyle().Foreground(ui.Accent).Render("▏")
	}
	avail := max(w-gutter-1, 1)
	if r.line < 0 {
		hdr := f.Hunks[r.hunk].Header
		st := lipgloss.NewStyle().Foreground(ui.Accent).Faint(true)
		if picked {
			st = st.Background(ui.Sel)
		}
		return mark + st.Render(padRight(truncPlain(" "+strings.Repeat(" ", gutter-1)+sanitizeCode(hdr), w-1), w-1))
	}
	l := f.Hunks[r.hunk].Lines[r.line]
	no := v.nos[r.hunk][r.line]
	bg := lipgloss.NewStyle()
	fg := ui.Text
	sign := string(l.Kind)
	switch l.Kind {
	case '+':
		bg, fg = bg.Background(ui.DiffAdd), ui.OK
	case '-':
		bg, fg = bg.Background(ui.DiffDel), ui.Fail
	case '\\':
		fg, sign = ui.Muted, " "
	}
	if cursor {
		bg = lipgloss.NewStyle().Background(ui.Sel)
	}
	num := func(n int) string {
		if n <= 0 {
			return strings.Repeat(" ", nw)
		}
		return fmt.Sprintf("%*d", nw, n)
	}
	nbg := bg
	if picked && !cursor { // a selected line keeps its tint; its line numbers take the selection colour
		nbg = lipgloss.NewStyle().Background(ui.Sel)
	}
	nums := nbg.Foreground(ui.Muted).Render(num(no.old) + " " + num(no.new) + " ")
	if l.Kind != '\\' {
		if hl := v.segments(); hl != nil && r.hunk < len(hl) && hl[r.hunk][r.line] != nil {
			return mark + nums + renderCodeSegs(sign+" ", hl[r.hunk][r.line], v.hx, avail, bg, fg)
		}
	}
	text := ansi.TruncateLeft(sanitizeCode(l.Text), v.hx, "")
	body := sign + " " + text
	body = padRight(ansi.Truncate(body, avail, "…"), avail)
	return mark + nums + bg.Foreground(fg).Render(body)
}

func diffCountsPlain(added, removed int) string { return fmt.Sprintf("+%d −%d", added, removed) }

// diffFileList is the left column: one row per file, the file at index cur on
// the raised background.
func diffFileList(files []git.FileDiff, cur, w, h int) []string {
	n := len(files)
	start := 0
	if n > h {
		start = max(min(cur-h/2, n-h), 0)
	}
	var out []string
	for i := start; i < n && len(out) < h; i++ {
		f := files[i]
		added, removed := fileCounts(f)
		counts := diffCountsPlain(added, removed)
		if f.Binary {
			counts = "bin"
		}
		room := max(w-ansi.StringWidth(counts)-3, 4)
		name := tailPath(f.Path, room)
		gap := max(w-2-ansi.StringWidth(name)-ansi.StringWidth(counts)-1, 1)
		sel := i == cur
		bg := lipgloss.NewStyle()
		if sel {
			bg = bg.Background(ui.Sel)
		}
		fg := ui.Text
		if !sel {
			fg = ui.Muted
		}
		lead := "  "
		if sel {
			lead = "▌ "
		}
		row := bg.Foreground(ui.Accent).Render(lead) + bg.Foreground(fg).Render(name+strings.Repeat(" ", gap))
		if f.Binary {
			row += bg.Foreground(ui.Muted).Render(counts + " ")
		} else {
			row += bg.Foreground(ui.OK).Render(fmt.Sprintf("+%d", added)) + bg.Render(" ") + bg.Foreground(ui.Fail).Render(fmt.Sprintf("−%d", removed)) + bg.Render(" ")
		}
		out = append(out, row)
	}
	return out
}

// inSel reports whether row r is part of the selection: the whole hunk (its
// header too) when selLines is nil, else the selected lines of the hunk.
func (v *diffView) inSel(r diffRow) bool {
	if !v.selOn || r.hunk != v.selHunk {
		return false
	}
	return v.selLines == nil || (r.line >= 0 && v.selLines[r.line])
}

// renderAll draws every row at w cells without a cursor, for panes that scroll
// the whole diff themselves.
func (v *diffView) renderAll(w int) []string {
	v.static, v.top = true, 0
	return v.render(w, max(len(v.rows), 1))
}
