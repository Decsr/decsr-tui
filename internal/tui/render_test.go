package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/Decsr/decsr-tui/internal/agent"
	"github.com/Decsr/decsr-tui/internal/git"
	"github.com/Decsr/decsr-tui/internal/ui"
)

func trueColor(t *testing.T) {
	t.Helper()
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}

var allKinds = []agent.Kind{
	agent.KindUser, agent.KindMessage, agent.KindThinking, agent.KindCommand, agent.KindRead,
	agent.KindEdit, agent.KindWrite, agent.KindSearch, agent.KindTool, agent.KindSubagent, agent.KindError,
}

func TestActivityLinePerKind(t *testing.T) {
	now := time.Now()
	for _, k := range allKinds {
		for _, st := range []agent.State{agent.StateRunning, agent.StateDone, agent.StateFailed} {
			a := agent.Activity{
				Kind: k, State: st, Tool: "X", Title: "internal/pkg/some/long/path/file.go", Detail: "a detail\nsecond line",
				File: "file.go", Added: 3, Removed: 1, At: now.Add(-5 * time.Second), Duration: 1200 * time.Millisecond,
			}
			for _, w := range []int{0, 2, 10, 40, 100} {
				line := activityLine(a, now, w)
				if got := lipgloss.Width(line); got > w {
					t.Errorf("kind %s state %s w=%d: line is %d cells", k, st, w, got)
				}
			}
			if w := 60; k != agent.KindThinking && actGlyph(k) == "·" {
				t.Errorf("kind %s has no glyph (w=%d)", k, w)
			}
			_ = activityDetailLines(a.Detail, 40, 3)
			_ = activityMessageLines(a.Detail, 80, 3, st == agent.StateRunning)
		}
	}
}

func TestActivityMetaCounts(t *testing.T) {
	plain, _ := actMeta(agent.Activity{Kind: agent.KindEdit, Added: 4, Removed: 2}, time.Now())
	if plain != "+4 −2" {
		t.Errorf("edit meta = %q", plain)
	}
	plain, _ = actMeta(agent.Activity{Kind: agent.KindCommand, State: agent.StateFailed, Duration: 3 * time.Second}, time.Now())
	if !strings.HasPrefix(plain, "✗") {
		t.Errorf("failed command meta = %q", plain)
	}
}

func twoHunkDiff() git.FileDiff {
	return git.FileDiff{
		Path: "main.go",
		Hunks: []git.Hunk{
			{
				Header: "@@ -1,3 +1,3 @@ package main", OldStart: 1, OldLines: 3, NewStart: 1, NewLines: 3,
				Lines: []git.Line{
					{Kind: ' ', Text: "package main"},
					{Kind: '-', Text: `var x = "old"`},
					{Kind: '+', Text: `var x = "new" // a very long trailing comment that must be cut to the width of the pane`},
					{Kind: ' ', Text: "\tfunc tab() {}"},
				},
			},
			{
				Header: "@@ -20,2 +20,3 @@ func main()", OldStart: 20, OldLines: 2, NewStart: 20, NewLines: 3,
				Lines: []git.Line{
					{Kind: ' ', Text: "a"},
					{Kind: '+', Text: "b"},
					{Kind: '\\', Text: "No newline at end of file"},
				},
			},
		},
	}
}

func TestDiffViewRender(t *testing.T) {
	trueColor(t)
	f := twoHunkDiff()
	if a, r := fileCounts(f); a != 2 || r != 1 {
		t.Fatalf("fileCounts = %d, %d", a, r)
	}
	nos := diffLineNos(f)
	if nos[1][0] != (lineNo{old: 20, new: 20}) || nos[1][1] != (lineNo{new: 21}) {
		t.Errorf("line numbers wrong: %+v", nos[1])
	}
	for _, w := range []int{12, 30, 60, 100} {
		v := newDiffView(f)
		for cur := 0; cur < len(v.rows); cur++ {
			v.cur = cur
			for hx := 0; hx <= 4; hx += 4 {
				v.hx = hx
				lines := v.render(w, 20)
				if len(lines) != len(v.rows) {
					t.Fatalf("w=%d: %d lines for %d rows", w, len(lines), len(v.rows))
				}
				for i, l := range lines {
					if got := lipgloss.Width(l); got > w {
						t.Errorf("w=%d cur=%d line %d is %d cells: %q", w, cur, i, got, ansi.Strip(l))
					}
				}
			}
		}
	}
	v := newDiffView(f)
	out := ansi.Strip(strings.Join(v.render(80, 20), "\n"))
	for _, want := range []string{"@@ -1,3 +1,3 @@", "@@ -20,2 +20,3 @@", `var x = "new"`, "No newline"} {
		if !strings.Contains(out, want) {
			t.Errorf("render lacks %q:\n%s", want, out)
		}
	}
}

func TestDiffViewSmallWindowScrolls(t *testing.T) {
	v := newDiffView(twoHunkDiff())
	v.cur = len(v.rows) - 1
	lines := v.render(60, 3)
	if len(lines) != 3 || v.top != v.cur-2 {
		t.Errorf("got %d lines, top %d, cur %d", len(lines), v.top, v.cur)
	}
}

func TestDiffViewBinaryAndEmpty(t *testing.T) {
	out := newDiffView(git.FileDiff{Path: "a.png", Binary: true}).render(30, 5)
	if len(out) != 1 || !strings.Contains(ansi.Strip(out[0]), "Binary") {
		t.Errorf("binary: %q", out)
	}
}

func TestHighlightFileDiffColoursGo(t *testing.T) {
	segs := highlightFileDiff(twoHunkDiff(), chromaStyle())
	if segs == nil || len(segs) != 2 || len(segs[0]) != 4 {
		t.Fatalf("segs = %v", segs)
	}
	if segs[1][2] != nil {
		t.Errorf("the backslash line must be plain")
	}
	if highlightFileDiff(git.FileDiff{Path: "x.unknownext", Hunks: twoHunkDiff().Hunks}, chromaStyle()) != nil {
		t.Errorf("no lexer must give no segments")
	}
}

func TestLiveRenderers(t *testing.T) {
	now := time.Now()
	acts := []agent.Activity{
		{ID: "1", Kind: agent.KindMessage, Detail: "starting the work on the thing"},
		{ID: "2", Kind: agent.KindThinking, Detail: "hmm"},
		{ID: "3", Kind: agent.KindEdit, State: agent.StateDone, File: "a/b.go", Title: "a/b.go", Added: 3, Removed: 1},
		{ID: "4", Kind: agent.KindEdit, State: agent.StateDone, File: "a/b.go", Added: 2},
		{ID: "5", Kind: agent.KindCommand, State: agent.StateRunning, Title: "go test ./...", At: now},
	}
	files := liveFiles(acts)
	if len(files) != 1 || files[0].added != 5 || files[0].removed != 1 {
		t.Fatalf("liveFiles = %+v", files)
	}
	for _, w := range []int{20, 50} {
		for _, l := range liveSteps(acts, w, 6, "⠋", now) {
			if lipgloss.Width(l) > w {
				t.Errorf("step wider than %d: %q", w, ansi.Strip(l))
			}
		}
		for _, l := range liveFileRows(files, w, 3) {
			if lipgloss.Width(l) > w {
				t.Errorf("file row wider than %d: %q", w, ansi.Strip(l))
			}
		}
	}
	if got := len(liveSteps(acts, 40, 6, "", now)); got != 6 {
		t.Errorf("liveSteps rows = %d", got)
	}
	line := ansi.Strip(runningLine(liveRun{State: agent.StateDone, Runner: "claude", Model: "anthropic/opus", Started: now.Add(-72 * time.Second), Ended: now,
		Usage: agent.Usage{Input: 182000, Output: 2100, CacheRead: 164000}}, "", now))
	for _, want := range []string{"✓", "claude", "opus", "1m 12s", "↑ 182k", "164k cached", "↓ 2.1k"} {
		if !strings.Contains(line, want) {
			t.Errorf("running line %q lacks %q", line, want)
		}
	}
}

func TestOutChangesPane(t *testing.T) {
	f := twoHunkDiff()
	files := []git.FileDiff{f, {Path: "b.go", Hunks: f.Hunks[:1]}}
	v := newDiffView(files[0])
	for _, w := range []int{30, 80} {
		for _, l := range outChangesPane(files, 0, v, true, w, 14, true) {
			if lipgloss.Width(l) > w {
				t.Errorf("changes line wider than %d: %q", w, ansi.Strip(l))
			}
		}
	}
	if out := outChangesPane(nil, 0, nil, true, 40, 5, false); !strings.Contains(ansi.Strip(strings.Join(out, "\n")), "No file changes yet") {
		t.Errorf("empty running pane: %q", out)
	}
	old := newDiffView(files[0])
	old.cur, old.hx = 3, 2
	if m := mergeOutDiff(old, files[0], nil); m.cur != 3 || m.hx != 2 {
		t.Errorf("merge lost the cursor: %+v", m)
	}
	if m := mergeOutDiff(old, files[1], nil); m.cur != 0 {
		t.Errorf("merge kept the cursor on another file")
	}
}

func TestStatusBarFits(t *testing.T) {
	sb := NewStatusBar(60).WithBranch("main").WithState("watching").WithStageInfo("a long long long long long stage text that scrolls").
		WithHints([]ui.Hint{{Key: "q", Label: "quit"}, {Key: "?", Label: "help"}})
	for _, l := range strings.Split(sb.View(), "\n") {
		if lipgloss.Width(l) > 60 {
			t.Errorf("status line wider than 60: %q", ansi.Strip(l))
		}
	}
	if sb.AdvanceScroll().View() == "" {
		t.Error("empty status bar")
	}
}
