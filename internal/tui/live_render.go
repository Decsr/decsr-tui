package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Decsr/decsr-tui/internal/agent"
	"github.com/Decsr/decsr-tui/internal/git"
	"github.com/Decsr/decsr-tui/internal/ui"
)

// ── Running line ─────────────────────────────────────────────────────────────

// tokCompact is a token count as "512", "2.1k", "182k" or "1.2M".
func tokCompact(n int) string {
	switch {
	case n >= 999_500:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1e6), ".0") + "M"
	case n >= 10_000:
		return fmt.Sprintf("%dk", (n+500)/1000)
	case n >= 1000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1000), ".0") + "k"
	}
	return fmt.Sprintf("%d", n)
}

// liveRun is what the running line shows about one agent session.
type liveRun struct {
	State   agent.State
	Runner  string
	Model   string
	Effort  string
	Started time.Time
	Ended   time.Time // zero while running
	Usage   agent.Usage
}

// runningLine is "⠋ claude · opus-5-5 · effort medium · 1m 12s · ↑ 182k
// (164k cached) ↓ 2.1k". The spinner shows while the session runs; a finished
// one shows ✓ or ✗ and its final duration.
func runningLine(r liveRun, spin string, now time.Time) string {
	dim := lipgloss.NewStyle().Foreground(ui.Muted)
	text := lipgloss.NewStyle().Foreground(ui.Text)
	var glyph string
	end := r.Ended
	switch r.State {
	case agent.StateRunning:
		if spin == "" {
			spin = "▖"
		}
		glyph = lipgloss.NewStyle().Foreground(ui.Accent).Render(spin)
		end = now
	case agent.StateDone:
		glyph = lipgloss.NewStyle().Foreground(ui.OK).Render("✓")
	case agent.StateFailed:
		glyph = lipgloss.NewStyle().Foreground(ui.Fail).Render("✗")
	default:
		glyph = dim.Render("·")
	}
	parts := []string{}
	if r.Runner != "" {
		parts = append(parts, text.Render(r.Runner))
	}
	model := r.Model
	if i := strings.LastIndex(model, "/"); i >= 0 {
		model = model[i+1:]
	}
	if model == "" {
		model = "(default)"
	}
	parts = append(parts, text.Render(model))
	if r.Effort != "" {
		parts = append(parts, dim.Render("effort "+r.Effort))
	}
	if !r.Started.IsZero() && !end.IsZero() && !end.Before(r.Started) {
		parts = append(parts, text.Render(feedDuration(end.Sub(r.Started))))
	}
	if u := r.Usage; u.Input > 0 || u.Output > 0 {
		tok := "↑ " + tokCompact(int(u.Input))
		if u.CacheRead > 0 {
			tok += " (" + tokCompact(int(u.CacheRead)) + " cached)"
		}
		tok += " ↓ " + tokCompact(int(u.Output))
		parts = append(parts, dim.Render(tok))
	}
	return glyph + " " + strings.Join(parts, dim.Render(" · "))
}

// ── Changed files of a running session ───────────────────────────────────────

type liveFile struct {
	path           string
	added, removed int
}

// liveFiles is the files the session edited so far, summed from its
// activities, the newest first.
func liveFiles(acts []agent.Activity) []liveFile {
	idx := map[string]int{}
	last := map[string]int{}
	var out []liveFile
	for i, a := range acts {
		if (a.Kind != agent.KindEdit && a.Kind != agent.KindWrite) || a.State == agent.StateFailed {
			continue
		}
		p := a.File
		if p == "" {
			p = a.Title
		}
		p = oneLine(sanitizeText(p))
		if p == "" || p == "patch" {
			continue
		}
		j, ok := idx[p]
		if !ok {
			j = len(out)
			idx[p] = j
			out = append(out, liveFile{path: p})
		}
		out[j].added += a.Added
		out[j].removed += a.Removed
		last[p] = i
	}
	sort.SliceStable(out, func(a, b int) bool { return last[out[a].path] > last[out[b].path] })
	return out
}

// liveItem is one entry of the live timeline: a line of text or a step.
type liveItem struct {
	text string
	act  *agent.Activity
}

// liveTimeline is the session's activities in order: a message is a text item,
// everything else a step.
func liveTimeline(acts []agent.Activity) []liveItem {
	var out []liveItem
	for i := range acts {
		a := &acts[i]
		if a.Kind != agent.KindMessage {
			out = append(out, liveItem{act: a})
			continue
		}
		t := a.Detail
		if strings.TrimSpace(t) == "" {
			t = a.Title
		}
		if t = strings.TrimSpace(sanitizeText(t)); t != "" {
			out = append(out, liveItem{text: t})
		}
	}
	return out
}

// liveSteps is the newest rows of the timeline of acts, the newest at the
// bottom: text wrapped to w cells and one line per step. A thinking step shows
// as "thinking…" only. The step in progress, when it is last, shows spin.
func liveSteps(acts []agent.Activity, w, rows int, spin string, now time.Time) []string {
	dim := lipgloss.NewStyle().Foreground(ui.Muted)
	text := lipgloss.NewStyle().Foreground(ui.Text)
	items := liveTimeline(acts)
	if len(items) > rows {
		items = items[len(items)-rows:]
	}
	var out []string
	for _, it := range items {
		switch {
		case it.act == nil:
			for _, l := range strings.Split(ansi.Wrap(it.text, max(w, 1), ""), "\n") {
				out = append(out, text.Render(l))
			}
		case it.act.Kind == agent.KindThinking:
			out = append(out, dim.Render(actGlyph(it.act.Kind)+" thinking…"))
		default:
			out = append(out, activityLine(*it.act, now, w))
		}
	}
	if len(out) > rows {
		out = out[len(out)-rows:]
	}
	if len(items) == 0 {
		out = append(out, dim.Render("waiting for the first output…"))
	} else if a := items[len(items)-1].act; a != nil && a.State == agent.StateRunning && spin != "" && len(out) > 0 {
		last := len(out) - 1
		out[last] = strings.Replace(out[last], actGlyph(a.Kind), lipgloss.NewStyle().Foreground(ui.Accent).Render(spin), 1)
	}
	for len(out) < rows {
		out = append(out, "")
	}
	return out[:max(rows, 0)]
}

// liveFileRows is the changed files as "path +a −r" rows of w cells.
func liveFileRows(files []liveFile, w, rows int) []string {
	dim := lipgloss.NewStyle().Foreground(ui.Muted)
	out := make([]string, 0, rows)
	if len(files) == 0 {
		out = append(out, dim.Render("no file changes yet"))
	}
	for i, f := range files {
		if i == rows {
			break
		}
		if i == rows-1 && len(files) > rows {
			out = append(out, dim.Render(fmt.Sprintf("+%d more", len(files)-i)))
			break
		}
		counts := ""
		if f.added+f.removed > 0 {
			counts = diffCounts(f.added, f.removed)
		}
		cw := ansi.StringWidth(diffCountsPlain(f.added, f.removed))
		if counts == "" {
			cw = 0
		}
		name := tailPath(f.path, max(w-cw-1, 4))
		gap := max(w-ansi.StringWidth(name)-cw, 1)
		out = append(out, lipgloss.NewStyle().Foreground(ui.Text).Render(name)+strings.Repeat(" ", gap)+counts)
	}
	for len(out) < rows {
		out = append(out, "")
	}
	for i := range out {
		out[i] = ansi.Truncate(out[i], w, "")
	}
	return out
}

// ── Changes pane ─────────────────────────────────────────────────────────────

// mergeOutDiff is the view for a new diff f: the cursor and the scroll of old
// stay when it shows the same file. A failed reload keeps the old view.
func mergeOutDiff(old *diffView, f git.FileDiff, err error) *diffView {
	if err != nil {
		if old != nil && len(old.rows) > 0 {
			return old
		}
		return &diffView{err: oneLine(err.Error())}
	}
	v := newDiffView(f)
	if old != nil && old.file.Path == f.Path {
		v.hx, v.cur, v.top = old.hx, old.cur, old.top
	}
	v.cur = max(min(v.cur, len(v.rows)-1), 0)
	return v
}

// outChangesPane is the changes pane: a label row, the file list, a rule with
// the file path and the unified diff of the selected file (files[sel], shown
// by v); rows lines of w cells.
func outChangesPane(files []git.FileDiff, sel int, v *diffView, running bool, w, rows int, focused bool) []string {
	dim := lipgloss.NewStyle().Foreground(ui.Muted)
	label := "Changes"
	if len(files) > 0 {
		var a, r int
		for _, f := range files {
			fa, fr := fileCounts(f)
			a, r = a+fa, r+fr
		}
		label += fmt.Sprintf(" · %d %s %s", len(files), pluralize(len(files), "file", "files"), diffCountsPlain(a, r))
	}
	out := []string{ui.PaneLabel(label, focused, w)}
	msg := func(s string) []string { return append(out, "  "+dim.Render(truncateOneLine(s, max(w-4, 1)))) }
	switch {
	case v != nil && v.err != "":
		return msg("! " + v.err)
	case len(files) == 0 && running:
		return msg("No file changes yet.")
	case len(files) == 0:
		return msg("No changes in this session.")
	case v == nil:
		return msg("loading…")
	}
	body := rows - 1
	fl := min(len(files), max(body/3, 1))
	out = append(out, diffFileList(files, sel, w, fl)...)
	if rest := body - fl - 1; rest >= 1 {
		rule := fmt.Sprintf(" %s ", truncateOneLine(v.file.Path, max(w-8, 4)))
		out = append(out, lipgloss.NewStyle().Foreground(ui.Border).Render("──")+dim.Render(rule)+
			lipgloss.NewStyle().Foreground(ui.Border).Render(strings.Repeat("─", max(w-2-ansi.StringWidth(rule), 0))))
		out = append(out, v.render(w, rest)...)
	}
	return out
}
