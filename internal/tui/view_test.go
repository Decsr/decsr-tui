package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Decsr/decsr-tui/internal/ui"
)

// useTheme sets the theme and a colour profile for one test and restores both.
func useTheme(t *testing.T, mode string) {
	t.Helper()
	trueColor(t)
	prev := ui.IsDark()
	ui.Apply(mode, nil)
	t.Cleanup(func() {
		if prev {
			ui.Apply(ui.ThemeDark, nil)
		} else {
			ui.Apply(ui.ThemeLight, nil)
		}
	})
}

var viewSizes = [][2]int{{40, 10}, {80, 24}, {120, 40}, {200, 60}}

// checkFits fails when the view has more lines than the screen or a line wider.
func checkFits(t *testing.T, m *Model, name string) {
	t.Helper()
	for _, sz := range viewSizes {
		pump(m, tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
		ls := strings.Split(m.View(), "\n")
		if len(ls) > sz[1] {
			t.Errorf("%s %v: %d lines", name, sz, len(ls))
		}
		for i, l := range ls {
			if w := lipgloss.Width(l); w > sz[0] {
				t.Errorf("%s %v line %d width %d: %q", name, sz, i, w, ansi.Strip(l))
			}
		}
	}
}

func TestViewFitsBothThemes(t *testing.T) {
	for _, theme := range []string{ui.ThemeDark, ui.ThemeLight} {
		t.Run(theme, func(t *testing.T) {
			useTheme(t, theme)
			m := newTestModel(t, fakeAgent())
			writeFile(t, m.repo.Dir, "a.txt", strings.Replace(lines(10), "line2", "CHANGED\twith tab", 1))
			writeFile(t, m.repo.Dir, "dir/n.go", "package x\n")
			commitFile(t, m, "b.txt", "b\n", "two")
			press(m, "R")
			openAgent(t, m)
			checkFits(t, m, "agent live")
			press(m, "f", "k")
			checkFits(t, m, "agent detail")
			press(m, "c")
			checkFits(t, m, "agent changes")
			press(m, "c", "esc")
			checkFits(t, m, "agent sessions")
			press(m, "1")
			checkFits(t, m, "files")
			press(m, "?")
			checkFits(t, m, "help popup")
			press(m, "esc", "x")
			checkFits(t, m, "menu popup")
			press(m, "esc", "c")
			checkFits(t, m, "commit popup")
			press(m, "esc", "d")
			checkFits(t, m, "discard menu")
			press(m, "esc", "enter")
			checkFits(t, m, "staging view")
			press(m, "a", "j", "v", "j")
			checkFits(t, m, "staging line mode")
			press(m, "esc", "2")
			checkFits(t, m, "branches")
			press(m, "3")
			checkFits(t, m, "commits")
			press(m, "i")
			checkFits(t, m, "rebase todo")
		})
	}
}

func TestHeaderShowsRunnerChip(t *testing.T) {
	useTheme(t, ui.ThemeDark)
	m := newTestModel(t, fakeAgent())
	pump(m, agentTickMsg{})
	head := ansi.Strip(strings.Split(m.View(), "\n")[0])
	for _, want := range []string{"decsr", "main", "claude", runnerGlyph("claude", iconsUnicode)} {
		if !strings.Contains(head, want) {
			t.Fatalf("header %q lacks %q", head, want)
		}
	}
	if strings.Contains(head, "no agent session") {
		t.Fatalf("header %q ignores the live session", head)
	}
}

// TestStagingCursorStagesTheRightLine moves the staging cursor with j and k
// and checks that the line under it, and no other, is staged.
func TestStagingCursorStagesTheRightLine(t *testing.T) {
	m := newTestModel(t, nil)
	text := strings.Replace(lines(10), "line3\n", "LINE3\n", 1)
	text = strings.Replace(text, "line6\n", "LINE6\n", 1)
	writeFile(t, m.repo.Dir, "a.txt", text)
	press(m, "R", "1", "enter", "a")
	s := m.main.stg
	if s == nil || s.dv == nil || !s.lineMode {
		t.Fatalf("staging line mode not open: %+v", s)
	}
	at := func() string { return s.rowText(s.dv.cur) }
	for at() != "+LINE6" {
		press(m, "j")
	}
	// the selection follows the cursor: only the cursor line is marked
	if !s.dv.selOn || len(s.dv.selLines) != 1 {
		t.Fatalf("selection %v %v", s.dv.selOn, s.dv.selLines)
	}
	press(m, "k") // -line6
	if at() != "-line6" {
		t.Fatalf("k should move up one row, at %q", at())
	}
	press(m, "j", " ")
	cached := gitIn(t, m.repo.Dir, "diff", "--cached")
	if !strings.Contains(cached, "+LINE6") || strings.Contains(cached, "+LINE3") || strings.Contains(cached, "-line6") {
		t.Fatalf("expected only +LINE6 staged:\n%s", cached)
	}
	// a range (v, then move) stages every line it covers
	for at() != "-line3" {
		press(m, "k")
	}
	press(m, "v", "j")
	if len(s.dv.selLines) != 2 {
		t.Fatalf("range should mark two lines: %v", s.dv.selLines)
	}
	press(m, " ")
	cached = gitIn(t, m.repo.Dir, "diff", "--cached")
	if !strings.Contains(cached, "+LINE3") || !strings.Contains(cached, "-line3") {
		t.Fatalf("range not staged:\n%s", cached)
	}
}

func TestStagingRowsDrawTheSelection(t *testing.T) {
	useTheme(t, ui.ThemeDark)
	m := newTestModel(t, nil)
	writeFile(t, m.repo.Dir, "a.txt", strings.Replace(lines(10), "line3\n", "LINE3\n", 1))
	press(m, "R", "1", "enter", "a", "j", "j")
	v := m.main.stg.dv
	if v.cur != 2 || !v.selOn {
		t.Fatalf("cursor %d selOn %v", v.cur, v.selOn)
	}
	out := ansi.Strip(strings.Join(v.render(80, 20), "\n"))
	if !strings.Contains(out, "▌") || !strings.Contains(out, "-line3") && !strings.Contains(out, "line3") {
		t.Fatalf("cursor not drawn:\n%s", out)
	}
}
