package claude

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Decsr/decsr-tui/internal/agent"
)

const repo = "/work/demo"

// setup copies the fixtures into a temp projects root.
func setup(t *testing.T) (root string, src agent.Source) {
	t.Helper()
	root = t.TempDir()
	base := filepath.Join("testdata", "projects", "-work-demo")
	filepath.Walk(base, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel("testdata/projects", p)
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

func TestEncodeDir(t *testing.T) {
	if got := EncodeDir(`E:\CODER\Decsr-Code`); got != "E--CODER-Decsr-Code" {
		t.Fatalf("got %q", got)
	}
}

func TestSessions(t *testing.T) {
	root, src := setup(t)
	now := time.Now()
	os.Chtimes(filepath.Join(root, "-work-demo", "s1.jsonl"), now, now.Add(-time.Hour))
	os.Chtimes(filepath.Join(root, "-work-demo", "s2.jsonl"), now, now.Add(-2*time.Hour))
	ss, err := src.Sessions(repo)
	if err != nil || len(ss) != 2 {
		t.Fatalf("sessions=%v err=%v", ss, err)
	}
	if ss[0].ID != "s1" || ss[1].ID != "s2" {
		t.Fatalf("order: %s %s", ss[0].ID, ss[1].ID)
	}
	if ss[0].Title != "Fix x bug" || ss[0].Branch != "main" || ss[0].Cwd != repo || ss[0].Model != "claude-opus-4" || ss[0].Live {
		t.Fatalf("s1: %+v", ss[0])
	}
	if ss[1].Title != "Older session without a title" {
		t.Fatalf("s2 title: %q", ss[1].Title)
	}
	if none, err := src.Sessions("/nope"); err != nil || len(none) != 0 {
		t.Fatalf("missing project: %v %v", none, err)
	}
}

func TestTrackerSnapshot(t *testing.T) {
	_, src := setup(t)
	ss := sessionByID(t, src, "s1")
	tr := src.Open(ss)
	if changed, err := tr.Poll(); err != nil || !changed {
		t.Fatalf("poll: %v %v", changed, err)
	}
	s := tr.Snapshot()
	if s.Session.Title != "Fix x bug" {
		t.Errorf("title %q", s.Session.Title)
	}
	if u := byID(s.Activities, "u1#0"); u.Kind != agent.KindUser || u.Title != "Fix the bug in x.go" {
		t.Errorf("user: %+v", u)
	}
	for _, a := range s.Activities {
		if strings.Contains(a.Detail, "meta noise") {
			t.Errorf("meta record not skipped")
		}
	}
	if th := byID(s.Activities, "a1#0"); th.Kind != agent.KindThinking || th.Title != "Let me look" {
		t.Errorf("thinking: %+v", th)
	}
	if m := byID(s.Activities, "a1#1"); m.Kind != agent.KindMessage {
		t.Errorf("message: %+v", m)
	}
	t1 := byID(s.Activities, "toolu_t1")
	if t1.Kind != agent.KindCommand || t1.State != agent.StateDone || t1.Title != "go test ./..." ||
		t1.Duration != 2*time.Second || !strings.Contains(t1.Detail, "all passed") {
		t.Errorf("t1: %+v", t1)
	}
	t2 := byID(s.Activities, "toolu_t2")
	if t2.State != agent.StateFailed || t2.Title != "Run failing thing" {
		t.Errorf("t2: %+v", t2)
	}
	t3 := byID(s.Activities, "toolu_t3")
	if t3.Kind != agent.KindEdit || t3.Removed != 2 || t3.Added != 3 || t3.OldText != "a\nb" ||
		!strings.Contains(t3.Title, "x.go") || t3.State != agent.StateDone || !strings.Contains(t3.Detail, "edited") {
		t.Errorf("t3: %+v", t3)
	}
	if t5 := byID(s.Activities, "toolu_t5"); t5.State != agent.StateRunning {
		t.Errorf("t5 should be running: %+v", t5)
	}
	// usage dedupe: m1 counted once (10/5/100/20) + 4 messages of 1/1.
	if s.Usage != (agent.Usage{Input: 14, Output: 9, CacheRead: 100, CacheWrite: 20, Context: 1}) {
		t.Errorf("usage %+v", s.Usage)
	}
	// subagent linked via meta.json
	t4 := byID(s.Activities, "toolu_t4")
	if t4.Kind != agent.KindSubagent || t4.Title != "Explore: Map the code" || t4.SubagentID != "ab12" {
		t.Errorf("t4: %+v", t4)
	}
	if len(s.Subagents) != 1 {
		t.Fatalf("subagents: %d", len(s.Subagents))
	}
	sa := s.Subagents[0]
	if sa.ID != "ab12" || sa.Type != "Explore" || sa.ToolUseID != "toolu_t4" || !sa.Finished || sa.Live ||
		len(sa.Activities) != 2 || sa.Usage.Input != 7 {
		t.Errorf("sub: %+v", sa)
	}
	if g := sa.Activities[1]; g.Kind != agent.KindSearch || g.Title != "Grep func main" || g.State != agent.StateDone {
		t.Errorf("sub grep: %+v", g)
	}
}

func TestIncrementalPoll(t *testing.T) {
	root, src := setup(t)
	ss := sessionByID(t, src, "s1")
	tr := src.Open(ss)
	tr.Poll()
	if changed, _ := tr.Poll(); changed {
		// mtime unchanged and nothing appended: must be quiet
		t.Errorf("idle poll reported change")
	}
	path := filepath.Join(root, "-work-demo", "s1.jsonl")
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	defer f.Close()

	line := `{"type":"user","uuid":"u9","timestamp":"2026-01-01T10:01:00Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_t5","content":"slept"}]}}` + "\n"
	f.WriteString(line[:40]) // partial last line
	if _, err := tr.Poll(); err != nil {
		t.Fatal(err)
	}
	if byID(tr.Snapshot().Activities, "toolu_t5").State != agent.StateRunning {
		t.Fatal("partial line must not be applied")
	}
	f.WriteString(line[40:])
	if changed, _ := tr.Poll(); !changed {
		t.Fatal("completed line not seen")
	}
	if byID(tr.Snapshot().Activities, "toolu_t5").State != agent.StateDone {
		t.Fatal("t5 should be done")
	}
	f.WriteString(`{"type":"assistant","uuid":"a9","timestamp":"2026-01-01T10:01:01Z","message":{"id":"m9","content":[{"type":"text","text":"All done"}]}}` + "\n")
	tr.Poll()
	n := len(tr.Snapshot().Activities)
	if last := tr.Snapshot().Activities[n-1]; last.Title != "All done" {
		t.Errorf("last: %+v", last)
	}

	// truncation re-reads from scratch
	os.WriteFile(path, []byte(`{"type":"user","uuid":"z","message":{"content":"fresh"}}`+"\n"), 0o644)
	tr.Poll()
	if a := tr.Snapshot().Activities; len(a) != 1 || a[0].Title != "fresh" {
		t.Errorf("after truncate: %+v", a)
	}
}

func TestLateSubagent(t *testing.T) {
	root, src := setup(t)
	dir := filepath.Join(root, "-work-demo", "s1", "subagents")
	meta := filepath.Join(dir, "agent-ab12.meta.json")
	data, _ := os.ReadFile(meta)
	if err := os.Remove(meta); err != nil {
		t.Fatal(err)
	}
	ss := sessionByID(t, src, "s1")
	tr := src.Open(ss)
	tr.Poll()
	if n := len(tr.Snapshot().Subagents); n != 0 {
		t.Fatalf("subagents before meta: %d", n)
	}
	if err := os.WriteFile(meta, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if changed, _ := tr.Poll(); !changed || len(tr.Snapshot().Subagents) != 1 {
		t.Fatal("late subagent not picked up")
	}
}

func TestRelPath(t *testing.T) {
	cwd := t.TempDir()
	if got := relPath(cwd, filepath.Join(cwd, "a", "b.go")); got != filepath.Join("a", "b.go") {
		t.Errorf("got %q", got)
	}
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

func TestSubagentRunningUntilFinished(t *testing.T) {
	root, src := setup(t)
	dir := filepath.Join(root, "-work-demo")
	// make the Agent tool return at once, like a background subagent
	f, _ := os.OpenFile(filepath.Join(dir, "s1.jsonl"), os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(`{"type":"user","uuid":"u8","timestamp":"2026-01-01T10:00:11Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_t4","content":"started"}]}}` + "\n")
	f.Close()
	meta := filepath.Join(dir, "s1", "subagents", "agent-ab12.meta.json")
	data, _ := os.ReadFile(meta)
	os.WriteFile(meta, []byte(strings.Replace(string(data), `"finished":true`, `"finished":false`, 1)), 0o644)

	tr := src.Open(sessionByID(t, src, "s1"))
	tr.Poll()
	if a := byID(tr.Snapshot().Activities, "toolu_t4"); a.State != agent.StateRunning {
		t.Fatalf("unfinished subagent should be running: %+v", a)
	}
	os.WriteFile(meta, data, 0o644)
	tr.Poll()
	if a := byID(tr.Snapshot().Activities, "toolu_t4"); a.State != agent.StateDone {
		t.Fatalf("finished subagent should be done: %+v", a)
	}
}

func TestCleanPrompt(t *testing.T) {
	cases := map[string]string{
		"<system-reminder>x\ny</system-reminder>hello":                                                             "hello",
		"<local-command-caveat>Caveat: no</local-command-caveat>":                                                  "",
		"<command-name>/model</command-name><command-message>model</command-message><command-args></command-args>": "/model",
		"<local-command-stdout>Set model to X</local-command-stdout>":                                              "Set model to X",
		"plain": "plain",
	}
	for in, want := range cases {
		if got := cleanPrompt(in); got != want {
			t.Errorf("cleanPrompt(%q)=%q want %q", in, got, want)
		}
	}
}

func TestEncodeDirLinux(t *testing.T) {
	if got := EncodeDir("/home/u/my repo"); got != "-home-u-my-repo" {
		t.Fatalf("got %q", got)
	}
}
