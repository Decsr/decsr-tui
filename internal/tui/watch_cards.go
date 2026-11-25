package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Decsr/decsr-tui/internal/agent"
	"github.com/Decsr/decsr-tui/internal/ui"
)

// The node cards are decsr's feed cards (workspace_feed.go: renderNodeCard,
// cardHeader, cardBody) for watched nodes.

const (
	cardFileRows   = 4 // changed files a card lists before "+N more"
	cardErrorLines = 3
)

// cardOpts is everything a node card shows.
type cardOpts struct {
	n        wnode
	width    int // total width, borders included
	selected bool
	expanded bool
	now      time.Time
	spin     string // current spinner frame
	icons    string
}

// feedClock is the local wall-clock time of t as HH:MM; "" for a zero time.
func feedClock(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format("15:04")
}

// cardHeader is the first row of a card: status glyph, label and bold title on
// the left; runner glyph, model and start time dim on the right.
func cardHeader(o cardOpts, iw int) string {
	n := o.n
	dm := lipgloss.NewStyle().Foreground(ui.Muted)
	glyph, _, col := nodeStatus(n)
	if n.running() && o.spin != "" {
		glyph = o.spin
	}
	left := lipgloss.NewStyle().Foreground(col).Bold(true).Render(glyph) + " " + dm.Render(truncateOneLine(n.label, max(iw/3, 4))) + " "
	clock := feedClock(n.started)
	var cands []string
	badge := runnerBadge(n.runner, o.icons)
	if m := shortModel(n.model); m != "" {
		cands = append(cands, badge+dm.Render(strings.TrimRight(" "+m+" · "+clock, " ·")))
	}
	cands = append(cands, badge+dm.Render(" "+clock))
	cands = append(cands, dm.Render(clock))
	right := cands[len(cands)-1]
	for _, c := range cands {
		if iw-ansi.StringWidth(left)-ansi.StringWidth(c)-1 >= 10 {
			right = c
			break
		}
	}
	room := max(iw-ansi.StringWidth(left)-ansi.StringWidth(right)-1, 1)
	title := lipgloss.NewStyle().Foreground(ui.Text).Bold(true).Render(truncateOneLine(n.title, room))
	line := left + title
	gap := max(iw-ansi.StringWidth(line)-ansi.StringWidth(right), 1)
	return line + strings.Repeat(" ", gap) + right
}

// stepsLabel is "14 steps" or "1 step", with the failed ones: "14 steps · ✗ 2".
func stepsLabel(n wnode) string {
	total, failed := activityCounts(n)
	if total == 0 {
		return ""
	}
	s := plural(total, "step", "steps")
	if failed > 0 {
		s += fmt.Sprintf(" · ✗ %d", failed)
	}
	return s
}

// cardFiles lists the changed files of a node, at most cardFileRows and a
// "+N more files" line.
func cardFiles(n wnode, w int) []string {
	files := liveFiles(n.acts)
	var rows []string
	dm := lipgloss.NewStyle().Foreground(ui.Muted)
	for i, f := range files {
		if i == cardFileRows {
			more := len(files) - cardFileRows
			rows = append(rows, dm.Render(fmt.Sprintf("+%d more %s", more, pluralize(more, "file", "files"))))
			break
		}
		counts, cw := "", 0
		if f.added+f.removed > 0 {
			counts, cw = diffCounts(f.added, f.removed), ansi.StringWidth(diffCountsPlain(f.added, f.removed))
		}
		verb := "Edited"
		room := w - len(verb) - 1
		if cw > 0 {
			room -= cw + 2
		}
		left := dm.Render(verb) + " " + lipgloss.NewStyle().Foreground(ui.Text).Render(tailPath(f.path, room))
		if cw == 0 {
			rows = append(rows, left)
			continue
		}
		gap := max(w-ansi.StringWidth(left)-cw, 2)
		rows = append(rows, left+strings.Repeat(" ", gap)+counts)
	}
	return rows
}

// activityTimelineRows is the whole saved timeline of a node for an expanded
// card: a divider and every step; the detail of thinking steps is shown wrapped
// under them.
func activityTimelineRows(n wnode, now time.Time, iw int) []string {
	dm := lipgloss.NewStyle().Foreground(ui.Muted)
	rows := []string{dm.Render(strings.Repeat("─", min(iw, 24)))}
	for _, a := range n.acts {
		if a.Kind == agent.KindUser {
			continue
		}
		rows = append(rows, activityLine(a, now, iw))
		if a.Kind == agent.KindThinking {
			rows = append(rows, activityDetailLines(a.Detail, iw, actDetailLines)...)
		}
	}
	return rows
}

// cardBody is the rows under the header: what the node is doing (or did) with
// its duration, steps, context, tokens and stall text, the current action and
// the files it changed.
func cardBody(o cardOpts, iw int) []string {
	n := o.n
	dm := lipgloss.NewStyle().Foreground(ui.Muted)
	txt := lipgloss.NewStyle().Foreground(ui.Text)
	fail := lipgloss.NewStyle().Foreground(ui.Fail)
	warn := lipgloss.NewStyle().Foreground(ui.Warn)
	dur := nodeDuration(n, o.now)
	took := func(word string) string {
		if dur < 0 {
			return strings.TrimSpace(word)
		}
		return word + " " + feedDuration(dur)
	}
	arrow := ""
	if len(n.acts) > 0 {
		arrow = " ›"
		if o.expanded {
			arrow = " ⌄"
		}
	}
	meta := cardMeta(n)
	if s := stepsLabel(n); s != "" {
		meta = " · " + s + meta
	}
	var rows []string
	switch n.state {
	case "running":
		sp := o.spin
		if sp == "" {
			sp = "▖"
		}
		work := txt.Render(took("Working for"))
		if s := stallText(n, o.now); s != "" {
			work += dm.Render(" · ") + warn.Render("◌ "+s)
		}
		rows = append(rows, lipgloss.NewStyle().Foreground(ui.Accent).Render(sp)+" "+work+dm.Render(meta+arrow))
	case "done":
		rows = append(rows, lipgloss.NewStyle().Foreground(ui.OK).Render("✓")+" "+dm.Render(took("Worked for")+meta+arrow))
	case "failed":
		rows = append(rows, fail.Render("! "+took("Failed after"))+dm.Render(meta+arrow))
	default:
		line := "○ idle"
		if len(n.acts) == 0 {
			line += " · no activity yet"
		}
		rows = append(rows, dm.Render(line+meta+arrow))
	}
	if o.expanded && len(n.acts) > 0 {
		rows = append(rows, activityTimelineRows(n, o.now, iw)...)
	} else if cur, ok := currentAction(n); ok {
		rows = append(rows, activityLine(cur, o.now, iw))
		if n.state == "failed" && cur.State != agent.StateFailed {
			for i := len(n.acts) - 1; i >= 0; i-- {
				if n.acts[i].State == agent.StateFailed || n.acts[i].Kind == agent.KindError {
					errLines := hardWrap(sanitizeText(n.acts[i].Detail), iw)
					for j, l := range errLines {
						if j == cardErrorLines {
							break
						}
						rows = append(rows, fail.Render(truncateOneLine(l, iw)))
					}
					break
				}
			}
		}
	}
	rows = append(rows, cardFiles(n, iw)...)
	return rows
}

// renderNodeCard draws a node card in a rounded border of exactly o.width
// cells: ui.Accent when selected, ui.Fail when failed.
func renderNodeCard(o cardOpts) []string {
	iw := max(o.width-4, 4)
	rows := append([]string{cardHeader(o, iw)}, cardBody(o, iw)...)
	bc := ui.Border
	switch {
	case o.selected:
		bc = ui.Accent
	case o.n.state == "failed":
		bc = ui.Fail
	}
	b := lipgloss.NewStyle().Foreground(bc)
	out := make([]string, 0, len(rows)+2)
	out = append(out, b.Render("╭"+strings.Repeat("─", iw+2)+"╮"))
	for _, r := range rows {
		out = append(out, b.Render("│")+" "+padRight(ansi.Truncate(r, iw, "…"), iw)+" "+b.Render("│"))
	}
	return append(out, b.Render("╰"+strings.Repeat("─", iw+2)+"╯"))
}
