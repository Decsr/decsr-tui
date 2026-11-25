package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Decsr/decsr-tui/internal/agent"
	"github.com/Decsr/decsr-tui/internal/config"
	"github.com/Decsr/decsr-tui/internal/ui"
)

func plain(m *Model) string { return ansi.Strip(m.View()) }

func homeModel(t *testing.T, src *fakeSource) *Model {
	t.Helper()
	m := newTestModel(t, src)
	m.screen = scHome
	pump(m, agentTickMsg{})
	return m
}

func TestStallText(t *testing.T) {
	now := time.Now()
	n := wnode{state: "running", last: now.Add(-90 * time.Second)}
	if got := stallText(n, now); got != "no activity for 1m" {
		t.Fatalf("stall = %q", got)
	}
	n.last = now.Add(-59 * time.Second)
	if got := stallText(n, now); got != "" {
		t.Fatalf("not stalled yet: %q", got)
	}
	n.last, n.state = now.Add(-10*time.Minute), "done"
	if got := stallText(n, now); got != "" {
		t.Fatalf("a finished node never stalls: %q", got)
	}
}

func TestCtxLabelAndMeter(t *testing.T) {
	m := newTestModel(t, fakeAgent())
	n := wnode{id: "x", label: "claude", runner: "claude", model: "claude-opus-4", state: "running",
		usage: agent.Usage{Input: 10, Context: 18000}}
	if got := ctxLabel(n); got != "ctx 18k / 200k" {
		t.Fatalf("ctx = %q", got)
	}
	if row := ansi.Strip(m.ctxRow(n, 60)); !strings.Contains(row, "ctx 18k / 200k") || !strings.Contains(row, "█") {
		t.Fatalf("meter row: %q", row)
	}
	n.model = "some-unknown-model"
	if got := ctxLabel(n); got != "ctx 18k" {
		t.Fatalf("unknown model: %q", got)
	}
	if row := ansi.Strip(m.ctxRow(n, 60)); strings.ContainsAny(row, "█░") {
		t.Fatalf("the meter must be hidden when the window is unknown: %q", row)
	}
	n.usage.Context = 250000
	n.model = "claude-opus-4"
	if got := ctxLabel(n); !strings.Contains(got, "/ 1M") {
		t.Fatalf("past 200k means the 1M window: %q", got)
	}
}

func TestHomeNavigationOpensWatch(t *testing.T) {
	src := fakeAgent()
	older := agent.Session{Runner: "claude", ID: "sess0", Title: "Older work", Updated: time.Now().Add(-time.Hour)}
	src.sessions = append(src.sessions, older)
	m := homeModel(t, src)
	if m.screen != scHome {
		t.Fatal("not on Home")
	}
	v := plain(m)
	for _, want := range []string{"SESSIONS", "REPO", "MENU", "Fix things", "Older work", "main", "git + live agent watcher"} {
		if !strings.Contains(v, want) {
			t.Errorf("Home lacks %q", want)
		}
	}
	press(m, "down", "enter")
	if m.screen != scWatch || m.agent.cur != "sess0" {
		t.Fatalf("enter should open Watch for the selected session: screen %d cur %q", m.screen, m.agent.cur)
	}
	press(m, "H")
	if m.screen != scHome {
		t.Fatal("H should go Home")
	}
	press(m, "tab", "down", "enter") // the menu: Git
	if m.screen != scGit {
		t.Fatalf("menu Git: screen %d", m.screen)
	}
	press(m, "H", "W")
	if m.screen != scWatch {
		t.Fatalf("W: screen %d", m.screen)
	}
	press(m, "G")
	if m.screen != scGit {
		t.Fatalf("G: screen %d", m.screen)
	}
}

func TestSessionFlagOpensWatchDirectly(t *testing.T) {
	m := newTestModelOpts(t, fakeAgent(), Options{Session: "sess"})
	if m.screen != scWatch {
		t.Fatalf("--session should start on Watch, got %d", m.screen)
	}
	pump(m, agentTickMsg{})
	if m.agent.cur != "sess1" || !strings.Contains(plain(m), "Explore") {
		t.Fatalf("session not shown: cur %q", m.agent.cur)
	}
	if m2 := newTestModelOpts(t, fakeAgent(), Options{}); m2.screen != scGit {
		t.Fatal("test helper should start on Git")
	}
	m3, _ := New(m.repo.Dir, fakeAgent(), Options{})
	if m3.screen != scHome {
		t.Fatal("default start screen is Home")
	}
}

func watchModel(t *testing.T, src *fakeSource) *Model {
	t.Helper()
	m := newTestModel(t, src)
	pump(m, agentTickMsg{})
	press(m, "W")
	pump(m, agentTickMsg{})
	return m
}

func TestWatchGraphAndFeedStayInSync(t *testing.T) {
	useTheme(t, ui.ThemeDark)
	m := watchModel(t, fakeAgent())
	if m.watch.sel != "sa1" || !m.watch.follow {
		t.Fatalf("follow should select the running subagent: %q", m.watch.sel)
	}
	press(m, "up")
	if m.watch.sel != rootID || m.watch.follow {
		t.Fatalf("up: sel %q follow %v", m.watch.sel, m.watch.follow)
	}
	nodes := m.wnodes()
	check := func(sel string) {
		t.Helper()
		graph := strings.Join(m.watchGraph(nodes, 44, 12), "\n")
		feed := m.watchFeed(nodes, 60, 20)
		other := "root"
		if sel == "root" {
			other = "sa1"
		}
		if m.watch.sel != sel {
			t.Fatalf("sel %q want %q", m.watch.sel, sel)
		}
		// the selected card carries the "▌" cursor; the canvas draws the same node selected
		marked := 0
		for _, l := range feed {
			if strings.Contains(ansi.Strip(l), "▌") {
				marked++
			}
		}
		if marked == 0 {
			t.Fatalf("no selected card for %s", sel)
		}
		o := canvasOpts{width: 42, height: 12, selected: other, icons: iconsUnicode}
		o2 := o
		o2.selected = sel
		if renderCanvas(nodes, o) == renderCanvas(nodes, o2) {
			t.Fatal("the graph does not mark the selection")
		}
		if !strings.Contains(ansi.Strip(graph), "Graph") {
			t.Fatal("no graph pane")
		}
	}
	check("root")
	press(m, "down")
	check("sa1")
	press(m, "left")
	check("root")
	press(m, "right")
	check("sa1")
}

func TestWatchLivePaneToggle(t *testing.T) {
	useTheme(t, ui.ThemeDark)
	m := watchModel(t, fakeAgent())
	v := plain(m)
	if !strings.Contains(v, "Live · ") || !strings.Contains(v, "v hide") {
		t.Fatalf("live pane missing:\n%s", v)
	}
	press(m, "v")
	if strings.Contains(plain(m), "Live · ") {
		t.Fatal("v should hide the live pane")
	}
	press(m, "v")
	if !strings.Contains(plain(m), "Live · ") {
		t.Fatal("v should show the live pane again")
	}
}

func TestFollowSelectsNewSubagent(t *testing.T) {
	src := fakeAgent()
	m := watchModel(t, src)
	src.add(agent.Activity{ID: "t2", Kind: agent.KindSubagent, State: agent.StateRunning, Title: "Plan: x", SubagentID: "sa2"})
	src.snap.Subagents = append(src.snap.Subagents, agent.Subagent{ID: "sa2", Type: "Plan", Description: "plan it"})
	pump(m, agentTickMsg{})
	if m.watch.sel != "sa2" {
		t.Fatalf("follow should select the new node, got %q", m.watch.sel)
	}
	press(m, "up")
	src.snap.Subagents = append(src.snap.Subagents, agent.Subagent{ID: "sa3", Type: "Review"})
	pump(m, agentTickMsg{})
	if m.watch.sel == "sa3" {
		t.Fatal("follow is off: the selection must stay")
	}
}

func TestWatchOverlayAndSteps(t *testing.T) {
	src := fakeAgent()
	src.add(agent.Activity{ID: "e1", Kind: agent.KindEdit, State: agent.StateDone, Title: "Edit x.go", File: "x.go",
		OldText: "a\nb", NewText: "a\nc", Added: 1, Removed: 1})
	m := watchModel(t, src)
	press(m, "home") // the root
	press(m, "enter")
	if m.watch.overlay == nil || !strings.Contains(plain(m), "x.go") {
		t.Fatal("enter should open the detail of the latest step")
	}
	press(m, "esc")
	if m.watch.overlay != nil {
		t.Fatal("esc closes the overlay")
	}
	press(m, "[")
	if m.watch.actSel[rootID] == "" {
		t.Fatal("[ should select an older step")
	}
}

func TestNoticeAppearsAndExpires(t *testing.T) {
	src := fakeAgent()
	m := newTestModel(t, src)
	now := time.Now()
	m.clock = func() time.Time { return now }
	m.say(ui.ResultOK, "push done")
	if !strings.Contains(strings.Split(plain(m), "\n")[1], "push done") {
		t.Fatalf("notice not under the header:\n%s", plain(m))
	}
	now = now.Add(5 * time.Second)
	if !strings.Contains(plain(m), "push done") {
		t.Fatal("a fading notice is still there")
	}
	now = now.Add(5 * time.Second)
	if strings.Contains(plain(m), "push done") {
		t.Fatal("the notice should be gone")
	}
	pump(m, agentTickMsg{})
	src.snap.Subagents = append(src.snap.Subagents, agent.Subagent{ID: "sa9", Type: "Review"})
	pump(m, agentTickMsg{})
	if !strings.Contains(plain(m), "subagent Review started") {
		t.Fatal("a new subagent should be announced")
	}
	m.errorPopup(errTest("fetch failed: no route"))
	if !strings.Contains(m.noticeLine(), "fetch failed") {
		t.Fatal("an error shows in the notice line too")
	}
}

type errTest string

func (e errTest) Error() string { return string(e) }

func TestWatchEmptyAndLoadingStates(t *testing.T) {
	m := newTestModel(t, nil)
	press(m, "W")
	if !strings.Contains(plain(m), "no agent session to watch") {
		t.Fatalf("empty state:\n%s", plain(m))
	}
}

func TestScreensFitBothThemes(t *testing.T) {
	for _, theme := range []string{ui.ThemeDark, ui.ThemeLight} {
		t.Run(theme, func(t *testing.T) {
			useTheme(t, theme)
			m := newTestModel(t, fakeAgent())
			writeFile(t, m.repo.Dir, "dir/n.go", "package x\n")
			src := m.src.(*fakeSource)
			src.add(agent.Activity{ID: "e1", Kind: agent.KindEdit, State: agent.StateDone, Title: "Edit x.go", File: "x.go",
				OldText: "a\nb", NewText: "a\nc", Added: 1, Removed: 1, At: time.Now()})
			src.snap.Usage = agent.Usage{Input: 1200, Output: 300, CacheRead: 20000, Context: 21000}
			src.snap.Session.Model = "claude-opus-4"
			m.cfg.Pricing = map[string]config.ModelPrice{"claude-*": {InputPerMTok: 3, OutputPerMTok: 15}}
			press(m, "R")
			pump(m, agentTickMsg{})
			for _, scr := range []string{"H", "G", "W"} {
				press(m, scr)
				checkFits(t, m, "screen "+scr)
				press(m, "?")
				checkFits(t, m, "help "+scr)
				press(m, "esc")
				if scr == "W" {
					press(m, "v")
					checkFits(t, m, "watch no live")
					press(m, "v", "[", "enter")
					checkFits(t, m, "watch overlay")
					press(m, "esc", "down", "up")
					m.say(ui.ResultError, "fetch failed: something long happened here "+strings.Repeat("x", 80))
					checkFits(t, m, "watch notice")
				}
			}
			press(m, "H", "x")
			for _, sz := range viewSizes {
				pump(m, tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
				for _, l := range strings.Split(m.View(), "\n") {
					if lipgloss.Width(l) > sz[0] {
						t.Fatalf("home %v: line too wide", sz)
					}
				}
			}
		})
	}
}
