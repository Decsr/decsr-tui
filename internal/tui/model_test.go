package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Decsr/decsr-tui/internal/agent"
	"github.com/Decsr/decsr-tui/internal/config"
)

// ---- fakes and helpers ----

type fakeSource struct {
	sessions []agent.Session
	snap     agent.Snapshot
}

func (f *fakeSource) Name() string                             { return "claude" }
func (f *fakeSource) Sessions(string) ([]agent.Session, error) { return f.sessions, nil }
func (f *fakeSource) Open(agent.Session) agent.Tracker         { return &fakeTracker{f} }
func (f *fakeSource) add(a agent.Activity)                     { f.snap.Activities = append(f.snap.Activities, a) }
func (t *fakeTracker) Poll() (bool, error)                     { return true, nil }
func (t *fakeTracker) Snapshot() agent.Snapshot                { return t.f.snap }

type fakeTracker struct{ f *fakeSource }

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func lines(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "line%d\n", i)
	}
	return b.String()
}

// pump feeds msg to the model and runs every resulting command to the end.
func pump(m *Model, msg tea.Msg) {
	_, cmd := m.Update(msg)
	drain(m, cmd)
}

func drain(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-done:
	case <-time.After(5 * time.Second):
		return
	}
	switch msg := msg.(type) {
	case nil, spinner.TickMsg, statusTickMsg, agentTickMsg, spinTickMsg, tea.QuitMsg:
	case tea.BatchMsg:
		for _, c := range msg {
			drain(m, c)
		}
	default:
		pump(m, msg)
	}
}

func press(m *Model, keys ...string) {
	for _, k := range keys {
		pump(m, keyMsgFromString(k))
	}
}

func typeText(m *Model, s string) {
	pump(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
}

func newTestModel(t *testing.T, src *fakeSource) *Model {
	t.Helper()
	return newTestModelOpts(t, src, Options{})
}

func newTestModelOpts(t *testing.T, src *fakeSource, opts Options) *Model {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "config", "user.name", "Test")
	gitIn(t, dir, "config", "user.email", "t@example.com")
	gitIn(t, dir, "config", "core.autocrlf", "false")
	gitIn(t, dir, "config", "commit.gpgsign", "false")
	writeFile(t, dir, "a.txt", lines(10))
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", "init")
	if src == nil {
		src = &fakeSource{}
	}
	m, err := New(dir, src, opts)
	if err != nil {
		t.Fatal(err)
	}
	pump(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	drain(m, m.startup())
	return m
}

func fileByPath(m *Model, p string) (f struct{ staged, unstaged byte }, ok bool) {
	for _, x := range m.files.files {
		if x.Path == p {
			return struct{ staged, unstaged byte }{x.Staged, x.Unstaged}, true
		}
	}
	return f, false
}

// ---- git panels ----

func TestSpaceStagesFile(t *testing.T) {
	m := newTestModel(t, nil)
	writeFile(t, m.repo.Dir, "a.txt", strings.Replace(lines(10), "line2", "CHANGED", 1))
	press(m, "R")
	if f, ok := fileByPath(m, "a.txt"); !ok || f.unstaged != 'M' {
		t.Fatalf("a.txt not listed as modified: %+v", m.files.files)
	}
	press(m, "1", " ")
	if f, _ := fileByPath(m, "a.txt"); f.staged != 'M' || f.unstaged != ' ' {
		t.Fatalf("not staged: %+v", f)
	}
	press(m, " ")
	if f, _ := fileByPath(m, "a.txt"); f.staged != ' ' {
		t.Fatalf("not unstaged: %+v", f)
	}
}

func TestStagingViewStagesOneLine(t *testing.T) {
	m := newTestModel(t, nil)
	text := strings.Replace(lines(10), "line3\n", "LINE3\n", 1)
	text = strings.Replace(text, "line6\n", "LINE6\n", 1)
	writeFile(t, m.repo.Dir, "a.txt", text)
	press(m, "R", "1", "enter")
	s := m.main.stg
	if s == nil || s.dv == nil || len(s.dv.rows) == 0 {
		t.Fatalf("staging view not open: %+v", s)
	}
	press(m, "a") // line mode
	for s.rowText(s.dv.cur) != "+LINE3" {
		press(m, "j")
	}
	press(m, " ")
	cached := gitIn(t, m.repo.Dir, "diff", "--cached")
	if !strings.Contains(cached, "+LINE3") || strings.Contains(cached, "+LINE6") || strings.Contains(cached, "-line3") {
		t.Fatalf("expected only the +LINE3 line staged:\n%s", cached)
	}
	if m.main.stg == nil {
		t.Fatal("staging view should stay open with LINE6 left")
	}
	press(m, "esc")
	if m.main.stg != nil || m.mainFocus {
		t.Fatal("esc should leave the staging view")
	}
}

func TestCommit(t *testing.T) {
	m := newTestModel(t, nil)
	writeFile(t, m.repo.Dir, "b.txt", "hello\n")
	press(m, "R", "1")
	for m.files.list.key != "b.txt" {
		press(m, "j")
	}
	press(m, " ", "c")
	if m.popup == nil || m.popup.kind != pkCommit {
		t.Fatal("commit popup not open")
	}
	typeText(m, "add b")
	press(m, "enter")
	if m.popup != nil {
		t.Fatal("popup should close")
	}
	if got := strings.TrimSpace(gitIn(t, m.repo.Dir, "log", "-1", "--format=%s")); got != "add b" {
		t.Fatalf("subject %q", got)
	}
	if len(m.files.files) != 0 {
		t.Fatalf("files left: %+v", m.files.files)
	}
}

func TestBranchCreateAndCheckout(t *testing.T) {
	m := newTestModel(t, nil)
	press(m, "2", "n")
	typeText(m, "feature")
	press(m, "enter")
	if m.head.Branch != "feature" {
		t.Fatalf("branch %q", m.head.Branch)
	}
	m.branches.list.key = "main"
	m.syncLists()
	press(m, " ")
	if m.head.Branch != "main" {
		t.Fatalf("branch %q", m.head.Branch)
	}
}

func TestStashPushAndPop(t *testing.T) {
	m := newTestModel(t, nil)
	writeFile(t, m.repo.Dir, "a.txt", "changed\n")
	press(m, "R", "1", "s")
	typeText(m, "wip")
	press(m, "enter")
	if len(m.stashes.stashes) != 1 || len(m.files.files) != 0 {
		t.Fatalf("stashes=%d files=%d", len(m.stashes.stashes), len(m.files.files))
	}
	press(m, "4", "g")
	if len(m.stashes.stashes) != 0 || len(m.files.files) != 1 {
		t.Fatalf("after pop: stashes=%d files=%d", len(m.stashes.stashes), len(m.files.files))
	}
}

func TestStatusPollKeepsSelection(t *testing.T) {
	m := newTestModel(t, nil)
	writeFile(t, m.repo.Dir, "a.txt", "x\n")
	writeFile(t, m.repo.Dir, "b.txt", "y\n")
	writeFile(t, m.repo.Dir, "c.txt", "z\n")
	press(m, "R", "1", "j", "j")
	if m.files.list.key != "c.txt" {
		t.Fatalf("key %q", m.files.list.key)
	}
	writeFile(t, m.repo.Dir, "0.txt", "new\n") // sorts before the selection
	pump(m, statusTickMsg{})
	if m.files.list.key != "c.txt" || m.files.list.cur != 3 {
		t.Fatalf("selection moved: key=%q cur=%d", m.files.list.key, m.files.list.cur)
	}
}

func TestFilterAndTreeToggle(t *testing.T) {
	m := newTestModel(t, nil)
	writeFile(t, m.repo.Dir, "d/x.go", "x\n")
	writeFile(t, m.repo.Dir, "d/y.go", "y\n")
	press(m, "R", "1")
	if n := len(m.items(pFiles, 40)); n != 3 { // d/, x.go, y.go
		t.Fatalf("tree rows %d", n)
	}
	press(m, "enter") // collapse d/
	if n := len(m.items(pFiles, 40)); n != 1 {
		t.Fatalf("collapsed rows %d", n)
	}
	press(m, "`")
	press(m, "/")
	typeText(m, "y.go")
	press(m, "enter")
	if its := m.items(pFiles, 40); len(its) != 1 || its[0].key != "d/y.go" {
		t.Fatalf("filtered: %+v", its)
	}
}

// ---- agent panel ----

func fakeAgent() *fakeSource {
	now := time.Now()
	s := agent.Session{Runner: "claude", ID: "sess1", Title: "Fix things", Live: true, Updated: now}
	f := &fakeSource{sessions: []agent.Session{s}}
	f.snap = agent.Snapshot{Session: s}
	f.add(agent.Activity{ID: "u1", Kind: agent.KindUser, State: agent.StateDone, Title: "do it", Detail: "do it"})
	f.add(agent.Activity{ID: "t1", Kind: agent.KindSubagent, State: agent.StateRunning, Title: "Explore: map", SubagentID: "sa1", Detail: "{}"})
	f.snap.Subagents = []agent.Subagent{{ID: "sa1", Type: "Explore", Activities: []agent.Activity{
		{ID: "c1", Kind: agent.KindSearch, State: agent.StateDone, Title: "Grep main"},
		{ID: "c2", Kind: agent.KindRead, State: agent.StateRunning, Title: "Read x.go"},
	}}}
	return f
}

func openAgent(t *testing.T, m *Model) {
	t.Helper()
	pump(m, agentTickMsg{}) // polls the auto-watched newest session
	press(m, "5", "enter")
	if !m.agent.open {
		t.Fatal("session not opened")
	}
}

func TestFollowTracksNewActivity(t *testing.T) {
	src := fakeAgent()
	m := newTestModel(t, src)
	openAgent(t, m)
	if m.agent.tree.key != "t1" || !m.agent.follow {
		t.Fatalf("follow should select the newest row: key=%q follow=%v", m.agent.tree.key, m.agent.follow)
	}
	src.add(agent.Activity{ID: "m1", Kind: agent.KindMessage, State: agent.StateDone, Title: "All done", Detail: "All done here"})
	pump(m, agentTickMsg{})
	if m.agent.tree.key != "m1" {
		t.Fatalf("selection did not follow: %q", m.agent.tree.key)
	}
	if m.main.kind != mkLive || !strings.Contains(ansi.Strip(m.View()), "All done here") {
		t.Fatalf("the live view does not show the newest text: kind %d", m.main.kind)
	}
	press(m, "k") // moving up switches follow off
	src.add(agent.Activity{ID: "m2", Kind: agent.KindMessage, State: agent.StateDone, Title: "more"})
	pump(m, agentTickMsg{})
	if m.agent.follow || m.agent.tree.key == "m2" {
		t.Fatal("follow should be off after moving up")
	}
	press(m, "f")
	if !m.agent.follow || m.agent.tree.key != "m2" {
		t.Fatalf("f should re-enable follow: key=%q", m.agent.tree.key)
	}
}

func TestSubagentExpands(t *testing.T) {
	m := newTestModel(t, fakeAgent())
	openAgent(t, m)
	if n := len(m.items(pAgent, 60)); n != 2 {
		t.Fatalf("rows before expand: %d", n)
	}
	press(m, "G", "f") // select the subagent row, follow off
	m.agent.tree.key = "t1"
	press(m, " ")
	its := m.items(pAgent, 60)
	if len(its) != 4 || its[2].key != "sa1/c1" || its[3].key != "sa1/c2" {
		t.Fatalf("rows after expand: %+v", its)
	}
	press(m, " ")
	if n := len(m.items(pAgent, 60)); n != 2 {
		t.Fatalf("rows after collapse: %d", n)
	}
}

func TestAgentEmptyAndHeader(t *testing.T) {
	m := newTestModel(t, nil)
	if !strings.Contains(m.View(), "no claude sessions for this repo yet") {
		// the agent panel is collapsed when unfocused: focus it
		press(m, "5")
		if !strings.Contains(m.View(), "no claude sessions for this repo yet") {
			t.Fatal("missing empty text")
		}
	}
	if h := strings.Split(m.View(), "\n")[0]; !strings.Contains(h, "no agent session") {
		t.Fatalf("header without a session: %q", h)
	}
	m2 := newTestModel(t, fakeAgent())
	openAgent(t, m2)
	h := ansi.Strip(strings.Split(m2.View(), "\n")[0])
	if !strings.Contains(h, "claude") || !strings.Contains(h, runnerGlyph("claude", iconsUnicode)) || !strings.Contains(h, "Fix things") {
		t.Fatalf("header lacks the live runner chip: %q", h)
	}
}

func TestEditShowsDiff(t *testing.T) {
	src := fakeAgent()
	src.add(agent.Activity{ID: "e1", Kind: agent.KindEdit, State: agent.StateDone, Title: "Edit x.go",
		File: "x.go", OldText: "a\nb", NewText: "a\nc", Added: 2, Removed: 2})
	m := newTestModel(t, src)
	openAgent(t, m)
	press(m, "f") // follow off: the pane shows the selected edit, not the live view
	if len(m.main.secs) != 1 || !m.main.secs[0].diff || !strings.Contains(m.main.secs[0].text, "-b\n+c") {
		t.Fatalf("edit diff not shown: %+v", m.main.secs)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "x.go") || !strings.Contains(v, "+ c") {
		t.Fatalf("edit diff not drawn:\n%s", v)
	}
}

// ---- rendering ----

func TestViewFitsAllSizes(t *testing.T) {
	m := newTestModel(t, fakeAgent())
	writeFile(t, m.repo.Dir, "a.txt", strings.Replace(lines(10), "line2", "CHANGED\twith tab", 1))
	writeFile(t, m.repo.Dir, "dir/n.go", "package x\n")
	press(m, "R")
	openAgent(t, m)
	modes := []struct {
		name string
		do   func()
	}{
		{"agent", func() {}},
		{"files", func() { press(m, "1") }},
		{"help", func() { press(m, "?") }},
		{"menu", func() { press(m, "esc", "x") }},
		{"commit", func() { press(m, "esc", "c") }},
		{"staging", func() { press(m, "esc", "enter") }},
	}
	for _, md := range modes {
		md.do()
		for _, sz := range [][2]int{{40, 10}, {80, 24}, {200, 60}} {
			pump(m, tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
			out := m.View()
			ls := strings.Split(out, "\n")
			if len(ls) > sz[1] {
				t.Errorf("%s %v: %d lines", md.name, sz, len(ls))
			}
			for i, l := range ls {
				if w := lipgloss.Width(l); w > sz[0] {
					t.Errorf("%s %v line %d width %d: %q", md.name, sz, i, w, l)
				}
			}
		}
	}
}

// ---- phase 2 ----

func commitFile(t *testing.T, m *Model, name, content, msg string) {
	t.Helper()
	writeFile(t, m.repo.Dir, name, content)
	gitIn(t, m.repo.Dir, "add", "-A")
	gitIn(t, m.repo.Dir, "commit", "-q", "-m", msg)
	press(m, "R")
}

func selectCommit(t *testing.T, m *Model, subject string) {
	t.Helper()
	for _, c := range m.commits.commits {
		if c.Subject == subject {
			m.commits.list.key = c.Hash
			m.syncLists()
			return
		}
	}
	t.Fatalf("no commit %q", subject)
}

// subjects lists the log subjects, newest first.
func subjects(t *testing.T, dir string) []string {
	t.Helper()
	return strings.Split(strings.TrimSpace(gitIn(t, dir, "log", "--format=%s")), "\n")
}

func wantSubjects(t *testing.T, dir string, want ...string) {
	t.Helper()
	if got := subjects(t, dir); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("log = %v, want %v", got, want)
	}
}

func TestRewordViaPopup(t *testing.T) {
	m := newTestModel(t, nil)
	commitFile(t, m, "b.txt", "b\n", "two")
	commitFile(t, m, "c.txt", "c\n", "three")
	press(m, "3")
	selectCommit(t, m, "two")
	press(m, "r")
	if m.popup == nil || m.popup.kind != pkCommit || m.popup.ta.Value() != "two" {
		t.Fatalf("reword popup not prefilled: %+v", m.popup)
	}
	m.popup.ta.SetValue("two, reworded")
	press(m, "enter")
	wantSubjects(t, m.repo.Dir, "three", "two, reworded", "init")
	// HEAD takes the amend path
	selectCommit(t, m, "three")
	press(m, "r")
	m.popup.ta.SetValue("three!")
	press(m, "enter")
	wantSubjects(t, m.repo.Dir, "three!", "two, reworded", "init")
}

func TestSquashAndFixup(t *testing.T) {
	m := newTestModel(t, nil)
	commitFile(t, m, "b.txt", "b\n", "two")
	commitFile(t, m, "c.txt", "c\n", "three")
	commitFile(t, m, "d.txt", "d\n", "four")
	press(m, "3")
	selectCommit(t, m, "three")
	press(m, "s")
	if m.popup == nil || m.popup.kind != pkConfirm {
		t.Fatal("squash should ask first")
	}
	press(m, "y")
	wantSubjects(t, m.repo.Dir, "four", "two", "init")
	if body := gitIn(t, m.repo.Dir, "log", "-1", "--skip=1", "--format=%B"); !strings.Contains(body, "three") {
		t.Fatalf("squash lost the message: %q", body)
	}
	selectCommit(t, m, "four")
	press(m, "f", "y")
	wantSubjects(t, m.repo.Dir, "two", "init")
}

func TestDropMoveAndRevert(t *testing.T) {
	m := newTestModel(t, nil)
	commitFile(t, m, "b.txt", "b\n", "two")
	commitFile(t, m, "c.txt", "c\n", "three")
	press(m, "3")
	selectCommit(t, m, "three")
	press(m, "ctrl+j")
	wantSubjects(t, m.repo.Dir, "two", "three", "init")
	if m.commits.commits[m.commits.list.cur].Subject != "three" {
		t.Fatalf("cursor did not follow the moved commit: %d", m.commits.list.cur)
	}
	press(m, "t", "y")
	if got := subjects(t, m.repo.Dir)[0]; !strings.HasPrefix(got, "Revert") {
		t.Fatalf("no revert commit: %q", got)
	}
	selectCommit(t, m, "two")
	press(m, "d", "y")
	for _, s := range subjects(t, m.repo.Dir) {
		if s == "two" {
			t.Fatal("two not dropped")
		}
	}
}

func TestCherryPickClipboard(t *testing.T) {
	m := newTestModel(t, nil)
	gitIn(t, m.repo.Dir, "branch", "other")
	commitFile(t, m, "b.txt", "b\n", "two")
	commitFile(t, m, "c.txt", "c\n", "three")
	press(m, "3")
	selectCommit(t, m, "three")
	press(m, "C")
	selectCommit(t, m, "two")
	press(m, "C")
	if its := m.commitItems(40); len(m.clip) != 2 || !strings.HasPrefix(its[0].plain, "+ ") || !strings.HasPrefix(its[2].plain, "  ") {
		t.Fatalf("clipboard not marked: %d %q", len(m.clip), its[0].plain)
	}
	press(m, "esc")
	if len(m.clip) != 0 {
		t.Fatal("esc should clear the clipboard")
	}
	selectCommit(t, m, "three")
	press(m, "C")
	selectCommit(t, m, "two")
	press(m, "C")
	gitIn(t, m.repo.Dir, "checkout", "-q", "other")
	press(m, "R")
	if len(m.clip) != 2 {
		t.Fatal("clipboard should survive a branch switch")
	}
	press(m, "V", "y")
	wantSubjects(t, m.repo.Dir, "three", "two", "init")
	if len(m.clip) != 0 {
		t.Fatal("clipboard should be empty after pasting")
	}
}

func TestRebaseTodoEditor(t *testing.T) {
	m := newTestModel(t, nil)
	commitFile(t, m, "b.txt", "b\n", "A")
	commitFile(t, m, "c.txt", "c\n", "B")
	commitFile(t, m, "d.txt", "d\n", "C")
	press(m, "3")
	selectCommit(t, m, "A")
	press(m, "i")
	if m.todo == nil || len(m.todo.rows) != 3 || m.todo.rows[0].Subject != "C" {
		t.Fatalf("todo editor not open: %+v", m.todo)
	}
	press(m, "d")      // drop C
	press(m, "j", "j") // on A
	press(m, "ctrl+k") // A above B
	if r := m.todo.rows; r[0].Subject != "C" || r[1].Subject != "A" || r[2].Subject != "B" {
		t.Fatalf("rows after move: %+v", r)
	}
	press(m, "enter")
	if m.todo != nil {
		t.Fatal("todo should close on start")
	}
	wantSubjects(t, m.repo.Dir, "A", "B", "init")
}

func TestRebaseTodoReword(t *testing.T) {
	m := newTestModel(t, nil)
	commitFile(t, m, "b.txt", "b\n", "A")
	commitFile(t, m, "c.txt", "c\n", "B")
	press(m, "3")
	selectCommit(t, m, "A")
	press(m, "i", "j", "r", "enter") // reword A, the lower row
	if m.popup == nil || m.popup.kind != pkCommit {
		t.Fatal("reword should ask for the message")
	}
	m.popup.ta.SetValue("A2")
	press(m, "enter")
	wantSubjects(t, m.repo.Dir, "B", "A2", "init")
	if m.state != 0 {
		t.Fatalf("rebase left running: %v", m.state)
	}
}

func TestEditAndRebasingState(t *testing.T) {
	m := newTestModel(t, nil)
	commitFile(t, m, "b.txt", "b\n", "A")
	commitFile(t, m, "c.txt", "c\n", "B")
	press(m, "3")
	selectCommit(t, m, "A")
	press(m, "e")
	if m.state == 0 {
		t.Fatal("repo should be rebasing after e")
	}
	press(m, "m")
	if m.popup == nil || m.popup.kind != pkMenu {
		t.Fatal("m should open the continue/abort menu")
	}
	press(m, "a")
	if m.state != 0 {
		t.Fatal("abort should end the rebase")
	}
}

func TestTagCreateAndDelete(t *testing.T) {
	m := newTestModel(t, nil)
	press(m, "2", "]", "]")
	if m.branches.tab != tabTags {
		t.Fatalf("tab %d", m.branches.tab)
	}
	press(m, "n")
	typeText(m, "v1")
	press(m, "enter")
	if m.popup == nil || m.popup.kind != pkInput {
		t.Fatal("the message prompt should follow")
	}
	press(m, "enter") // empty message: lightweight
	if len(m.tags) != 1 || m.tags[0].Name != "v1" || m.tags[0].Annotated {
		t.Fatalf("tags %+v", m.tags)
	}
	press(m, "n")
	typeText(m, "v2")
	press(m, "enter")
	typeText(m, "release")
	press(m, "enter")
	if len(m.tags) != 2 {
		t.Fatalf("tags %+v", m.tags)
	}
	m.branches.list.key = "v1"
	m.syncLists()
	press(m, "d")
	if m.popup == nil || m.popup.kind != pkMenu || len(m.popup.items) != 2 {
		t.Fatal("delete should offer local or remote too")
	}
	press(m, "l")
	if len(m.tags) != 1 || m.tags[0].Name != "v2" || !m.tags[0].Annotated {
		t.Fatalf("after delete: %+v", m.tags)
	}
	if len(m.main.secs) == 0 || !strings.Contains(m.main.secs[0].text, "init") {
		t.Fatalf("main pane should show the tagged commit: %+v", m.main.secs)
	}
	press(m, " ", "y")
	if !m.head.Detached {
		t.Fatal("checkout of a tag should detach HEAD")
	}
}

func TestWorktreeAddAndSwitch(t *testing.T) {
	m := newTestModel(t, nil)
	main := m.repo.Dir
	wt := filepath.Join(t.TempDir(), "wt")
	press(m, "2", "]", "]", "]")
	if m.branches.tab != tabWorktrees {
		t.Fatalf("tab %d", m.branches.tab)
	}
	press(m, "n")
	typeText(m, wt)
	press(m, "enter")
	typeText(m, "wtbranch")
	press(m, "enter", "c")
	if len(m.worktrees) != 2 {
		t.Fatalf("worktrees %+v", m.worktrees)
	}
	for _, w := range m.worktrees {
		if !w.Main {
			m.branches.list.key = w.Path
		}
	}
	m.syncLists()
	press(m, " ")
	if filepath.Base(m.repo.Dir) != "wt" || m.repo.Dir == main {
		t.Fatalf("repo dir %q did not switch", m.repo.Dir)
	}
	if m.head.Branch != "wtbranch" {
		t.Fatalf("branch %q", m.head.Branch)
	}
}

func TestUndoAfterCommitAndRefusal(t *testing.T) {
	m := newTestModel(t, nil)
	commitFile(t, m, "b.txt", "b\n", "two")
	press(m, "z")
	if m.popup == nil || m.popup.kind != pkConfirm || !strings.Contains(m.popup.body, "Undo: reset to") || !strings.Contains(m.popup.body, "'init'") {
		t.Fatalf("undo confirm: %+v", m.popup)
	}
	press(m, "y")
	wantSubjects(t, m.repo.Dir, "init")
	writeFile(t, m.repo.Dir, "a.txt", "dirty\n")
	press(m, "R", "Z")
	if m.popup == nil || m.popup.kind != pkConfirm || !strings.HasPrefix(m.popup.body, "Redo") {
		t.Fatalf("redo confirm: %+v", m.popup)
	}
	press(m, "y")
	if m.popup == nil || m.popup.kind != pkError {
		t.Fatalf("a dirty tree should be refused in the error popup: %+v", m.popup)
	}
}

func TestDiscardUnstagedMenu(t *testing.T) {
	m := newTestModel(t, nil)
	writeFile(t, m.repo.Dir, "a.txt", "staged\n")
	gitIn(t, m.repo.Dir, "add", "a.txt")
	writeFile(t, m.repo.Dir, "a.txt", "staged\nmore\n")
	press(m, "R", "1", "d")
	if m.popup == nil || m.popup.kind != pkMenu || len(m.popup.items) != 2 {
		t.Fatal("discard menu missing")
	}
	press(m, "u")
	if m.popup == nil || m.popup.kind != pkConfirm {
		t.Fatal("discard should confirm")
	}
	press(m, "y")
	if d := gitIn(t, m.repo.Dir, "diff"); d != "" {
		t.Fatalf("unstaged changes remain: %s", d)
	}
	if d := gitIn(t, m.repo.Dir, "diff", "--cached"); !strings.Contains(d, "+staged") {
		t.Fatalf("staged changes lost: %s", d)
	}
}

func TestDeleteRemoteBranchConfirm(t *testing.T) {
	m := newTestModel(t, nil)
	remote := t.TempDir()
	gitIn(t, remote, "init", "-q", "--bare")
	gitIn(t, m.repo.Dir, "remote", "add", "origin", remote)
	gitIn(t, m.repo.Dir, "push", "-q", "origin", "main:feat")
	gitIn(t, m.repo.Dir, "fetch", "-q", "origin")
	press(m, "R", "2", "]")
	m.branches.list.key = "origin/feat"
	m.syncLists()
	press(m, "d")
	if m.popup == nil || m.popup.kind != pkConfirm {
		t.Fatalf("remote delete should confirm: %+v", m.popup)
	}
	press(m, "y")
	if out := gitIn(t, remote, "branch"); strings.Contains(out, "feat") {
		t.Fatalf("remote branch still there: %s", out)
	}
}

func TestCustomCommand(t *testing.T) {
	cfg := config.Default()
	cfg.CustomCommands = []config.Custom{
		{Key: "X", Context: "files", Description: "branch name", Command: "git rev-parse --abbrev-ref {{quote .CheckedOutBranch}}", Output: "log"},
		{Key: "Y", Context: "global", Description: "echo", Command: `echo {{index .Form "msg"}}`, Output: "log", Confirm: true,
			Prompts: []config.Prompt{{Name: "msg", Title: "Message"}}},
	}
	m := newTestModelOpts(t, nil, Options{Config: cfg})
	m.focus = pFiles
	cmd := m.key(keyMsgFromString("X"))
	if cmd == nil {
		t.Fatal("X started nothing")
	}
	msg, ok := cmd().(customDoneMsg)
	if !ok || msg.err != nil {
		t.Fatalf("command failed: %+v", msg)
	}
	pump(m, msg)
	if got := strings.Join(m.log.last(5), "\n"); !strings.Contains(got, "$ git rev-parse --abbrev-ref") || !strings.Contains(got, "main") {
		t.Fatalf("log %q", got)
	}
	// prompts, then a confirm showing the rendered command
	if cmd := m.key(keyMsgFromString("Y")); cmd != nil || m.popup == nil || m.popup.kind != pkInput {
		t.Fatalf("prompt expected: %+v", m.popup)
	}
	m.popup.in.SetValue("hello")
	if cmd := m.key(keyMsgFromString("enter")); cmd != nil || m.popup == nil || m.popup.kind != pkConfirm || !strings.Contains(m.popup.body, "echo hello") {
		t.Fatalf("confirm expected: %+v", m.popup)
	}
	msg = m.key(keyMsgFromString("y"))().(customDoneMsg)
	pump(m, msg)
	if !strings.Contains(strings.Join(m.log.last(3), "\n"), "hello") {
		t.Fatalf("log %v", m.log.last(3))
	}
	// custom keys show up in help and in the x menu, and only in their context
	press(m, "?")
	if !strings.Contains(m.popup.body, "Custom") || !strings.Contains(m.popup.body, "branch name") {
		t.Fatalf("help lacks custom commands: %s", m.popup.body)
	}
	press(m, "esc", "x")
	found := false
	for _, it := range m.popup.items {
		found = found || strings.Contains(it.label, "Custom: branch name")
	}
	if !found {
		t.Fatal("x menu lacks the custom command")
	}
	press(m, "esc", "3")
	if _, ok := m.customFor("X"); ok {
		t.Fatal("files command should not apply in Commits")
	}
	if _, ok := m.customFor("Y"); !ok {
		t.Fatal("global command should apply everywhere")
	}
}

func TestSpinnerOnlyWhileRunning(t *testing.T) {
	src := fakeAgent()
	m := newTestModel(t, src)
	openAgent(t, m)
	if !m.spinning {
		t.Fatal("spinner should run while an activity is running")
	}
	f := m.frame
	pump(m, spinTickMsg{})
	if m.frame != f+1 {
		t.Fatal("tick should advance the frame")
	}
	src.snap.Activities[1].State = agent.StateDone
	src.snap.Subagents[0].Activities[1].State = agent.StateDone
	pump(m, agentTickMsg{})
	pump(m, spinTickMsg{})
	if m.spinning {
		t.Fatal("spinner should stop when nothing runs")
	}
}

func TestHeaderAndStatusFit(t *testing.T) {
	m := newTestModel(t, fakeAgent())
	openAgent(t, m)
	for _, w := range []int{12, 20, 30, 40, 60, 120} {
		pump(m, tea.WindowSizeMsg{Width: w, Height: 30})
		for name, l := range map[string]string{"header": m.headerLine(), "status": m.statusLine(), "hints": m.hints(w)} {
			if lipgloss.Width(l) > w {
				t.Errorf("%s at %d: width %d %q", name, w, lipgloss.Width(l), l)
			}
		}
	}
}

func TestViewFitsNewPanels(t *testing.T) {
	m := newTestModel(t, fakeAgent())
	commitFile(t, m, "b.txt", "b\n", "two")
	commitFile(t, m, "c.txt", "c\n", "three")
	check := func(name string) {
		t.Helper()
		for _, sz := range [][2]int{{40, 10}, {80, 24}, {200, 60}} {
			pump(m, tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
			ls := strings.Split(m.View(), "\n")
			if len(ls) > sz[1] {
				t.Errorf("%s %v: %d lines", name, sz, len(ls))
			}
			for i, l := range ls {
				if w := lipgloss.Width(l); w > sz[0] {
					t.Errorf("%s %v line %d width %d: %q", name, sz, i, w, l)
				}
			}
		}
	}
	for i, tab := range tabNames {
		press(m, "esc", "2")
		m.branches.setTab(i)
		check("tab " + tab)
		press(m, "n")
		check("tab " + tab + " prompt")
		press(m, "esc")
	}
	press(m, "3")
	check("commits")
	press(m, "C")
	check("clipboard")
	press(m, "r")
	check("reword popup")
	press(m, "esc", "t")
	check("revert confirm")
	press(m, "esc", "z")
	check("undo confirm")
	press(m, "esc", "i")
	check("todo editor")
	press(m, "d")
	check("todo editor drop")
	press(m, "esc", "?")
	check("commits help")
	press(m, "esc", "x")
	check("commits menu")
}
