package codex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Decsr/decsr-tui/internal/agent"
)

const repo = "/work/demo"

func setup(t *testing.T) (root string, src agent.Source) {
	t.Helper()
	root = t.TempDir()
	filepath.Walk("testdata/sessions", func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel("testdata/sessions", p)
		dst := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(dst), 0o755)
		data, _ := os.ReadFile(p)
		return os.WriteFile(dst, data, 0o644)
	})
	return root, NewWithRoot(root)
}

func byID(acts []agent.Activity, id string) agent.Activity {
	for _, a := range acts {
		if a.ID == id {
			return a
		}
	}
	return agent.Activity{}
}

func sessionByID(t *testing.T, src agent.Source, id string) agent.Session {
	t.Helper()
	ss, _ := src.Sessions(repo)
	for _, s := range ss {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("no session %s", id)
	return agent.Session{}
}

func TestSessionsFilteredByCwd(t *testing.T) {
	root, src := setup(t)
	now := time.Now()
	dir := filepath.Join(root, "2026", "01", "01")
	os.Chtimes(filepath.Join(dir, "rollout-2026-01-01T10-00-00-c1c1.jsonl"), now, now.Add(-time.Hour))
	os.Chtimes(filepath.Join(dir, "rollout-2026-01-01T08-00-00-c3c3.jsonl"), now, now.Add(-3*time.Hour))
	ss, err := src.Sessions(repo)
	if err != nil || len(ss) != 2 {
		t.Fatalf("sessions=%v err=%v", ss, err)
	}
	if ss[0].ID != "c1c1" || ss[1].ID != "c3c3" { // c3c3 matches case-insensitively with a trailing slash
		t.Fatalf("order: %s %s", ss[0].ID, ss[1].ID)
	}
	s := ss[0]
	if s.Runner != "codex" || s.Title != "Fix the bug in x.go" || s.Branch != "main" || s.Model != "gpt-5-codex" || s.Cwd != repo || s.Live {
		t.Fatalf("c1: %+v", s)
	}
	if none, err := src.Sessions("/nope"); err != nil || len(none) != 0 {
		t.Fatalf("no match: %v %v", none, err)
	}
	if _, err := NewWithRoot(filepath.Join(root, "missing")).Sessions(repo); err != nil {
		t.Fatalf("missing root: %v", err)
	}
}

func TestTrackerSnapshot(t *testing.T) {
	_, src := setup(t)
	tr := src.Open(sessionByID(t, src, "c1c1"))
	if changed, err := tr.Poll(); err != nil || !changed {
		t.Fatalf("poll: %v %v", changed, err)
	}
	s := tr.Snapshot()
	var kinds []agent.Kind
	for _, a := range s.Activities {
		kinds = append(kinds, a.Kind)
		if strings.Contains(a.Detail, "environment_context") || strings.Contains(a.Detail, "future") {
			t.Errorf("noise leaked: %+v", a)
		}
	}
	if u := s.Activities[0]; u.Kind != agent.KindUser || u.Title != "Fix the bug in x.go" {
		t.Errorf("user: %+v", u)
	}
	if th := s.Activities[1]; th.Kind != agent.KindThinking || th.Title != "Looking at the code" {
		t.Errorf("thinking: %+v", th)
	}
	c1 := byID(s.Activities, "call_1")
	if c1.Kind != agent.KindCommand || c1.State != agent.StateDone || c1.Title != "go test ./..." ||
		c1.Duration != 2*time.Second || !strings.Contains(c1.Detail, "all passed") || strings.Contains(c1.Detail, "\n\nok") {
		t.Errorf("call_1: %+v", c1)
	}
	if c2 := byID(s.Activities, "call_2"); c2.State != agent.StateFailed || !strings.Contains(c2.Detail, "boom") {
		t.Errorf("call_2: %+v", c2)
	}
	px := byID(s.Activities, "call_3#0")
	if px.Kind != agent.KindEdit || px.State != agent.StateDone || px.Removed != 2 || px.Added != 3 ||
		px.OldText != "a\nb" || px.NewText != "A\nB\nC" || px.File != "/work/demo/x.go" || !strings.Contains(px.Title, "x.go") {
		t.Errorf("patch x: %+v", px)
	}
	py := byID(s.Activities, "call_3#1")
	if py.Kind != agent.KindEdit || py.Added != 2 || py.Removed != 0 || py.File != "y.go" || py.State != agent.StateDone {
		t.Errorf("patch y: %+v", py)
	}
	if c4 := byID(s.Activities, "call_4"); c4.State != agent.StateRunning {
		t.Errorf("call_4 should be running: %+v", c4)
	}
	if last := s.Activities[len(s.Activities)-1]; last.Kind != agent.KindMessage || last.Title != "All done" {
		t.Errorf("last: %+v", last)
	}
	if s.Usage != (agent.Usage{Input: 600, Output: 50, CacheRead: 400}) {
		t.Errorf("usage %+v", s.Usage)
	}
	if s.Session.Cwd != repo || s.Session.Model != "gpt-5-codex" {
		t.Errorf("session: %+v", s.Session)
	}
	_ = kinds
}

func TestIncrementalPoll(t *testing.T) {
	root, src := setup(t)
	tr := src.Open(sessionByID(t, src, "c1c1"))
	tr.Poll()
	if changed, _ := tr.Poll(); changed {
		t.Errorf("idle poll reported change")
	}
	path := filepath.Join(root, "2026", "01", "01", "rollout-2026-01-01T10-00-00-c1c1.jsonl")
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	defer f.Close()
	line := `{"timestamp":"2026-01-01T10:01:00.000Z","type":"response_item","payload":{"type":"function_call_output","call_id":"call_4","output":"Exit code: 0\nOutput:\nslept"}}` + "\n"
	f.WriteString(line[:50])
	if _, err := tr.Poll(); err != nil {
		t.Fatal(err)
	}
	if byID(tr.Snapshot().Activities, "call_4").State != agent.StateRunning {
		t.Fatal("partial line must not be applied")
	}
	f.WriteString(line[50:])
	if changed, _ := tr.Poll(); !changed {
		t.Fatal("completed line not seen")
	}
	if byID(tr.Snapshot().Activities, "call_4").State != agent.StateDone {
		t.Fatal("call_4 should be done")
	}
	os.WriteFile(path, []byte(`{"timestamp":"2026-01-01T10:00:00Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"fresh"}]}}`+"\n"), 0o644)
	tr.Poll()
	if a := tr.Snapshot().Activities; len(a) != 1 || a[0].Title != "fresh" {
		t.Errorf("after truncate: %+v", a)
	}
}
