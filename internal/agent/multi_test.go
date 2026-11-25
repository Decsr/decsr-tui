package agent_test

import (
	"errors"
	"testing"
	"time"

	"github.com/Decsr/decsr-tui/internal/agent"
)

type fakeSource struct {
	name string
	ss   []agent.Session
	err  error
}

func (f fakeSource) Name() string { return f.name }
func (f fakeSource) Sessions(string) ([]agent.Session, error) {
	return f.ss, f.err
}
func (f fakeSource) Open(s agent.Session) agent.Tracker { return fakeTracker{f.name, s} }

type fakeTracker struct {
	from string
	s    agent.Session
}

func (fakeTracker) Poll() (bool, error) { return false, nil }
func (f fakeTracker) Snapshot() agent.Snapshot {
	return agent.Snapshot{Session: agent.Session{Title: f.from}}
}

func TestMulti(t *testing.T) {
	now := time.Now()
	a := fakeSource{name: "claude", ss: []agent.Session{{Runner: "claude", ID: "a1", Updated: now.Add(-time.Hour)}}}
	b := fakeSource{name: "codex", ss: []agent.Session{{Runner: "codex", ID: "b1", Updated: now}, {Runner: "codex", ID: "b2", Updated: now.Add(-2 * time.Hour)}}}
	bad := fakeSource{name: "opencode", err: errors.New("boom")}
	m := agent.Multi(a, bad, b)
	if m.Name() != "all" {
		t.Fatalf("name %q", m.Name())
	}
	ss, err := m.Sessions("/x")
	if err != nil || len(ss) != 3 || ss[0].ID != "b1" || ss[1].ID != "a1" || ss[2].ID != "b2" {
		t.Fatalf("merge: %v %v", ss, err)
	}
	for _, s := range ss {
		if got := m.Open(s).Snapshot().Session.Title; got != s.Runner {
			t.Errorf("session %s routed to %q", s.ID, got)
		}
	}
	if tr := m.Open(agent.Session{Runner: "nobody"}); tr == nil {
		t.Fatal("unknown runner must still give a tracker")
	}
}

func TestSameDirCase(t *testing.T) {
	want := agent.FoldCase()
	if got := agent.SameDir("/Work/Demo", "/work/demo"); got != want {
		t.Fatalf("SameDir case-insensitive = %v, want %v on this OS", got, want)
	}
	if !agent.SameDir("/work/demo/", "/work/demo") {
		t.Fatal("trailing slash should match")
	}
}
