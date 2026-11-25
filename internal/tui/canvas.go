package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Decsr/decsr-tui/internal/ui"
)

// The node graph is decsr's canvas (workspace_canvas.go) for watched nodes: a
// column per depth, the root on the left, its subagents next to it, nested
// ones further right, a rounded box per node and elbow connectors between a
// node and its children.

// Canvas geometry.
const (
	canvasBoxMin = 18 // narrowest node box
	canvasBoxMax = 44 // widest node box
	canvasGutter = 6
	canvasVertX  = 2 // gutter column that carries the vertical connector run
	canvasBoxH   = 4
)

// canvasFit picks the box width and the number of whole columns shown for a
// graph of cols columns drawn in avail cells. When every column fits it is the
// widest box (canvasBoxMin to canvasBoxMax) that lets them all fit. Otherwise
// it is the most whole columns that fit at the minimum width, with the boxes
// widened to use the room; scroll indicators reserve canvasMarkW cells per side
// that can hide columns.
func canvasFit(cols, avail int) (boxW, shown int) {
	if cols < 1 {
		cols = 1
	}
	width := func(k, reserve int) int {
		return min((avail-reserve-(k-1)*canvasGutter)/k, canvasBoxMax)
	}
	if w := width(cols, 0); w >= canvasBoxMin {
		return w, cols
	}
	for k := cols - 1; k >= 1; k-- {
		reserve := canvasMarkW(cols)
		if cols-k >= 2 {
			reserve *= 2
		}
		if w := width(k, reserve); w >= canvasBoxMin {
			return w, k
		}
	}
	return min(max(canvasBoxMin, 1), max(avail, 1)), 1
}

// canvasMarkW is the cells one scroll indicator ("+N ›") and its gap take.
func canvasMarkW(cols int) int { return len(fmt.Sprint(cols)) + 4 }

// canvasOpts configures renderCanvas.
type canvasOpts struct {
	width, height int
	selected      string // node id
	icons         string // unicode | ascii | nerd
	spinner       string // current spinner frame for running nodes
}

type canvasRect struct{ x, y, w, h int }

type canvasLayout struct {
	boxW   int
	layers [][]wnode
	layer  map[string]int
	rect   map[string]canvasRect
	totalW int
	totalH int
}

func computeCanvasLayout(nodes []wnode, boxW int) canvasLayout {
	l := canvasLayout{boxW: boxW, layer: map[string]int{}, rect: map[string]canvasRect{}}
	for _, n := range nodes {
		for len(l.layers) <= n.depth {
			l.layers = append(l.layers, nil)
		}
		l.layers[n.depth] = append(l.layers[n.depth], n)
		l.layer[n.id] = n.depth
	}
	for i, col := range l.layers {
		y := 0
		for _, n := range col {
			l.rect[n.id] = canvasRect{x: i * (boxW + canvasGutter), y: y, w: boxW, h: canvasBoxH}
			y += canvasBoxH + 1
		}
		if y-1 > l.totalH {
			l.totalH = y - 1
		}
	}
	if len(l.layers) > 0 {
		l.totalW = len(l.layers)*(boxW+canvasGutter) - canvasGutter
	}
	return l
}

// nodeStatus is the glyph, word and colour of a node's state. Colour is never
// the only signal: every state has its own glyph and word.
func nodeStatus(n wnode) (glyph, word string, color lipgloss.TerminalColor) {
	if n.state == "idle" {
		g, _, _ := ui.Status("todo")
		return g, "idle", ui.Muted
	}
	return ui.Status(n.state)
}

// cell is one character of the virtual grid.
type cell struct {
	r    rune
	fg   lipgloss.TerminalColor
	bg   lipgloss.TerminalColor // only the selected box has a background
	bold bool
	wide bool // r is double-width; the next cell is its continuation
	cont bool // second half of a double-width rune; draws nothing
}

type canvasGrid struct {
	w, h  int
	cells [][]cell
}

func newCanvasGrid(w, h int) *canvasGrid {
	g := &canvasGrid{w: w, h: h, cells: make([][]cell, h)}
	for y := range g.cells {
		row := make([]cell, w)
		for x := range row {
			row[x] = cell{r: ' '}
		}
		g.cells[y] = row
	}
	return g
}

func (g *canvasGrid) set(x, y int, c cell) {
	if x >= 0 && y >= 0 && x < g.w && y < g.h {
		g.cells[y][x] = c
	}
}

// runeCells is the display width of r in terminal cells (0, 1 or 2).
func runeCells(r rune) int { return ansi.StringWidth(string(r)) }

// textOn writes s at x,y by display width on a background (bg nil = none),
// clipped to maxW cells. Double-width runes take two cells; zero-width runes
// are dropped.
func (g *canvasGrid) textOn(x, y, maxW int, s string, fg, bg lipgloss.TerminalColor, bold bool) {
	i := 0
	for _, r := range s {
		rw := runeCells(r)
		if rw == 0 {
			continue
		}
		if i+rw > maxW {
			return
		}
		if rw == 2 {
			g.set(x+i, y, cell{r: r, fg: fg, bg: bg, bold: bold, wide: true})
			g.set(x+i+1, y, cell{fg: fg, bg: bg, bold: bold, cont: true})
		} else {
			g.set(x+i, y, cell{r: r, fg: fg, bg: bg, bold: bold})
		}
		i += rw
	}
}

const (
	dirN = 1 << iota
	dirE
	dirS
	dirW
)

var junctionGlyph = map[int]rune{
	dirN | dirS:               '│',
	dirE | dirW:               '─',
	dirS | dirE:               '╭',
	dirS | dirW:               '╮',
	dirN | dirE:               '╰',
	dirN | dirW:               '╯',
	dirN | dirS | dirE:        '├',
	dirN | dirS | dirW:        '┤',
	dirE | dirW | dirS:        '┬',
	dirE | dirW | dirN:        '┴',
	dirN | dirE | dirS | dirW: '┼',
	dirN:                      '│',
	dirS:                      '│',
	dirE:                      '─',
	dirW:                      '─',
}

func nodeBorderColor(n wnode, selected bool) lipgloss.TerminalColor {
	switch {
	case selected:
		return ui.Accent
	case n.state == "failed":
		return ui.Fail
	}
	return ui.Border
}

// drawBox draws one node: a rounded border, the label (dim) and bold title on
// the first row, the runner glyph and model on the second with the status
// glyph and word right-aligned. The selected box is filled with ui.Sel.
func (c *canvasGrid) drawBox(n wnode, r canvasRect, o canvasOpts) {
	sel := n.id == o.selected
	bc := nodeBorderColor(n, sel)
	var bg lipgloss.TerminalColor
	if sel {
		bg = ui.Sel
	}
	inner := r.w - 2
	tw := inner - 2 // 1 cell of padding on each side

	for y := 0; y < r.h; y++ {
		for x := 0; x < r.w; x++ {
			ch := ' '
			switch {
			case y == 0 && x == 0:
				ch = '╭'
			case y == 0 && x == r.w-1:
				ch = '╮'
			case y == r.h-1 && x == 0:
				ch = '╰'
			case y == r.h-1 && x == r.w-1:
				ch = '╯'
			case y == 0 || y == r.h-1:
				ch = '─'
			case x == 0 || x == r.w-1:
				ch = '│'
			}
			cl := cell{r: ch, bg: bg}
			if ch != ' ' {
				cl.fg = bc
			}
			c.set(r.x+x, r.y+y, cl)
		}
	}

	// Row 1: label (dim) and title (bold).
	label := truncateOneLine(n.label, max(tw/2, 4))
	lw := ansi.StringWidth(label)
	c.textOn(r.x+2, r.y+1, tw, label, ui.Muted, bg, false)
	if tw-lw-2 >= 3 {
		c.textOn(r.x+2+lw+2, r.y+1, tw-lw-2, truncateOneLine(n.title, tw-lw-2), ui.Text, bg, true)
	}

	// Row 2, right: status glyph and word.
	glyph, word, col := nodeStatus(n)
	if n.running() && o.spinner != "" {
		glyph = o.spinner
	}
	status := glyph + " " + word
	sw := min(ansi.StringWidth(status), tw)
	c.textOn(r.x+r.w-2-sw, r.y+2, sw, truncateOneLine(status, sw), col, bg, false)

	// Row 2, left: runner glyph and model.
	x, room := r.x+2, tw-sw-2
	gl := runnerGlyph(n.runner, o.icons)
	c.textOn(x, r.y+2, room, gl, runnerColor(n.runner), bg, false)
	gw := ansi.StringWidth(gl) + 1
	x, room = x+gw, room-gw
	if m := shortModel(n.model); m != "" && room > 3 {
		c.textOn(x, r.y+2, room, truncateOneLine(m, room), ui.Muted, bg, false)
	}
}

// shortModel is a model id without its "vendor/" prefix.
func shortModel(model string) string {
	if i := strings.LastIndex(model, "/"); i >= 0 {
		model = model[i+1:]
	}
	return model
}

func renderCanvas(nodes []wnode, o canvasOpts) string {
	if o.width <= 0 || o.height <= 0 {
		return ""
	}
	out := newCanvasGrid(o.width, o.height)
	if len(nodes) == 0 {
		msg := "no nodes yet"
		out.textOn(max((o.width-ansi.StringWidth(msg))/2, 0), o.height/2, o.width, msg, ui.Muted, nil, false)
		return out.render()
	}

	nCols := 1
	for _, n := range nodes {
		nCols = max(nCols, n.depth+1)
	}
	boxW, shown := canvasFit(nCols, o.width)
	l := computeCanvasLayout(nodes, boxW)
	grid := newCanvasGrid(l.totalW, l.totalH)
	for _, n := range nodes {
		if r, ok := l.rect[n.id]; ok {
			grid.drawBox(n, r, o)
		}
	}

	// Connectors: collect direction bits per gutter cell, then merge to glyphs.
	type conn struct {
		mask  int
		arrow bool
		fg    lipgloss.TerminalColor
	}
	conns := map[[2]int]*conn{}
	touch := func(x, y, dir int, fg lipgloss.TerminalColor) {
		k := [2]int{x, y}
		c := conns[k]
		if c == nil {
			c = &conn{fg: fg}
			conns[k] = c
		}
		c.mask |= dir
		if fg == ui.Accent {
			c.fg = fg
		}
	}
	for _, t := range nodes {
		if t.parent == "" {
			continue
		}
		tr, ok := l.rect[t.id]
		sr, sok := l.rect[t.parent]
		if !ok || !sok {
			continue
		}
		var fg lipgloss.TerminalColor = ui.Border
		if t.parent == o.selected || t.id == o.selected {
			fg = ui.Accent
		}
		gx := sr.x + sr.w
		ys, yt := sr.y+1, tr.y+1
		// Path points: (0,ys) -> (vx,ys) -> (vx,yt) -> (end,yt).
		var path [][2]int
		for x := 0; x <= canvasVertX; x++ {
			path = append(path, [2]int{gx + x, ys})
		}
		for y := ys; y != yt; {
			if yt > ys {
				y++
			} else {
				y--
			}
			path = append(path, [2]int{gx + canvasVertX, y})
		}
		for x := canvasVertX + 1; x < canvasGutter; x++ {
			path = append(path, [2]int{gx + x, yt})
		}
		for i := range path {
			if i > 0 {
				a, b := path[i-1], path[i]
				switch {
				case b[0] > a[0]:
					touch(a[0], a[1], dirE, fg)
					touch(b[0], b[1], dirW, fg)
				case b[1] > a[1]:
					touch(a[0], a[1], dirS, fg)
					touch(b[0], b[1], dirN, fg)
				case b[1] < a[1]:
					touch(a[0], a[1], dirN, fg)
					touch(b[0], b[1], dirS, fg)
				}
			}
		}
		// First cell touches the source box on its west side.
		touch(path[0][0], path[0][1], dirW, fg)
		last := path[len(path)-1]
		conns[[2]int{last[0], last[1]}].arrow = true
	}
	for k, c := range conns {
		r := junctionGlyph[c.mask]
		if c.arrow {
			r = '▶'
		}
		if r == 0 {
			r = '─'
		}
		grid.set(k[0], k[1], cell{r: r, fg: c.fg})
	}

	// Window: whole columns only, scrolled minimally to keep the selected
	// column visible.
	first := 0
	if _, ok := l.rect[o.selected]; ok {
		first = max(l.layer[o.selected]-shown+1, 0)
	}
	hiddenR := nCols - first - shown
	markW := canvasMarkW(nCols)
	ox := first * (boxW + canvasGutter)
	winW := shown*boxW + (shown-1)*canvasGutter
	x0 := 0
	if first > 0 && markW+winW <= o.width {
		x0 = markW
	}
	room := x0 > 0 || first == 0 && winW+markW <= o.width // margins fit the indicators
	oy := 0
	if sr, ok := l.rect[o.selected]; ok {
		if sr.y+sr.h > oy+o.height {
			oy = sr.y + sr.h - o.height
		}
		if sr.y < oy {
			oy = sr.y
		}
	}
	for y := 0; y < o.height; y++ {
		for x := 0; x < winW && x0+x < o.width; x++ {
			gx, gy := x+ox, y+oy
			if gx < grid.w && gy < grid.h {
				out.cells[y][x0+x] = grid.cells[gy][gx]
			}
		}
	}

	// Scroll indicators sit in the reserved side margins; the vertical marks
	// never cover the selected box.
	note := func(x, y int, text string) {
		for i, r := range []rune(text) {
			if x+i >= 0 && x+i < o.width && y >= 0 && y < o.height {
				out.cells[y][x+i] = cell{r: r, fg: ui.Muted}
			}
		}
	}
	if first > 0 && room {
		note(0, o.height/2, fmt.Sprintf("‹ +%d", first))
	}
	if hiddenR > 0 && room {
		t := fmt.Sprintf("+%d ›", hiddenR)
		note(o.width-len([]rune(t)), o.height/2, t)
	}
	mark := func(x, y int, r rune) {
		if x >= 0 && y >= 0 && x < o.width && y < o.height && out.cells[y][x].bg == nil {
			out.cells[y][x] = cell{r: r, fg: ui.Muted}
		}
	}
	if oy > 0 {
		mark(o.width/2, 0, '▴')
	}
	if oy+o.height < grid.h {
		mark(o.width/2, o.height-1, '▾')
	}
	return out.render()
}

// render converts the grid to styled lines, grouping runs of equal style.
func (g *canvasGrid) render() string {
	lines := make([]string, g.h)
	for y, row := range g.cells {
		var sb strings.Builder
		for x := 0; x < len(row); {
			e := x
			var run strings.Builder
			for e < len(row) && row[e].fg == row[x].fg && row[e].bg == row[x].bg && row[e].bold == row[x].bold {
				c := row[e]
				switch {
				case c.cont:
					// Drawn by its wide lead; an orphaned half becomes a space.
					if e == 0 || !row[e-1].wide {
						run.WriteRune(' ')
					}
				case c.wide:
					if e+1 < len(row) && row[e+1].cont {
						run.WriteRune(c.r)
					} else {
						run.WriteRune(' ')
					}
				default:
					run.WriteRune(c.r)
				}
				e++
			}
			if row[x].fg == nil && row[x].bg == nil && !row[x].bold {
				sb.WriteString(run.String())
			} else {
				sb.WriteString(lipgloss.NewStyle().Foreground(row[x].fg).Background(row[x].bg).Bold(row[x].bold).Render(run.String()))
			}
			x = e
		}
		lines[y] = sb.String()
	}
	return strings.Join(lines, "\n")
}
