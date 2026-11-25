package opencode

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Decsr/decsr-tui/internal/agent"
)

const repo = "/work/demo"

// copyTestdata copies testdata/storage into <root>/storage.
func copyTestdata(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	filepath.Walk("testdata/storage", func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel("testdata", p)
		dst := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(dst), 0o755)
		data, _ := os.ReadFile(p)
		return os.WriteFile(dst, data, 0o644)
	})
	return root
}

// sqliteFixture builds opencode.db from the same JSON fixtures, shaped like the upstream tables.
func sqliteFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(root, "opencode.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, q := range []string{
		`CREATE TABLE project (id TEXT PRIMARY KEY, worktree TEXT NOT NULL)`,
		`CREATE TABLE session (id TEXT PRIMARY KEY, project_id TEXT NOT NULL, parent_id TEXT, directory TEXT, title TEXT, time_created INTEGER, time_updated INTEGER)`,
		`CREATE TABLE message (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, time_created INTEGER, time_updated INTEGER, data TEXT NOT NULL)`,
		`CREATE TABLE part (id TEXT PRIMARY KEY, message_id TEXT NOT NULL, session_id TEXT NOT NULL, time_created INTEGER, time_updated INTEGER, data TEXT NOT NULL)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	read := func(pattern string) (out []map[string]any, raw [][]byte) {
		files, _ := filepath.Glob(filepath.Join("testdata", "storage", pattern))
		for _, f := range files {
			data, _ := os.ReadFile(f)
			var m map[string]any
			json.Unmarshal(data, &m)
			out, raw = append(out, m), append(raw, data)
		}
		return
	}
	num := func(m map[string]any, k string) int64 {
		tm, _ := m["time"].(map[string]any)
		f, _ := tm[k].(float64)
		return int64(f)
	}
	ps, _ := read("project/*.json")
	for _, p := range ps {
		db.Exec(`INSERT INTO project VALUES (?,?)`, p["id"], p["worktree"])
	}
	ss, _ := read("session/*/*.json")
	for _, s := range ss {
		db.Exec(`INSERT INTO session VALUES (?,?,?,?,?,?,?)`, s["id"], s["projectID"], s["parentID"], s["directory"], s["title"], num(s, "created"), num(s, "updated"))
	}
	ms, mraw := read("message/*/*.json")
	for i, m := range ms {
		db.Exec(`INSERT INTO message VALUES (?,?,?,?,?)`, m["id"], m["sessionID"], num(m, "created"), num(m, "created"), string(mraw[i]))
	}
	pp, praw := read("part/*/*.json")
	for i, p := range pp {
		db.Exec(`INSERT INTO part VALUES (?,?,?,?,?,?)`, p["id"], p["messageID"], p["sessionID"], 1000, 1000+int64(i), string(praw[i]))
	}
	return root
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
	for name, root := range map[string]string{"json": copyTestdata(t), "sqlite": sqliteFixture(t)} {
		src := NewWithRoot(root)
		ss, err := src.Sessions(demoQuery())
		if err != nil || len(ss) != 2 {
			t.Fatalf("%s: sessions=%v err=%v", name, ss, err)
		}
		// child (parentID) and the other project's session are excluded; ses_old matches via the project worktree
		if ss[0].ID != "ses_main" || ss[1].ID != "ses_old" {
			t.Fatalf("%s: order %s %s", name, ss[0].ID, ss[1].ID)
		}
		if s := ss[0]; s.Runner != "opencode" || s.Title != "Fix x bug" || s.Cwd != repo || s.Live {
			t.Fatalf("%s: %+v", name, s)
		}
		if none, err := src.Sessions("/nope"); err != nil || len(none) != 0 {
			t.Fatalf("%s: no match: %v %v", name, none, err)
		}
	}
	if ss, err := NewWithRoot(t.TempDir()).Sessions(repo); err != nil || len(ss) != 0 {
		t.Fatalf("empty root: %v %v", ss, err)
	}
}

func checkSnapshot(t *testing.T, name string, s agent.Snapshot) {
	t.Helper()
	if u := s.Activities[0]; u.Kind != agent.KindUser || u.Title != "Fix the bug in x.go" {
		t.Errorf("%s user: %+v", name, u)
	}
	if th := byID(s.Activities, "prt_011"); th.Kind != agent.KindThinking || th.Title != "Let me look" {
		t.Errorf("%s thinking: %+v", name, th)
	}
	c1 := byID(s.Activities, "call_1")
	if c1.Kind != agent.KindCommand || c1.State != agent.StateDone || c1.Title != "Run tests" ||
		c1.Duration != 2*time.Second || !strings.Contains(c1.Detail, "go test ./...") || !strings.Contains(c1.Detail, "all passed") {
		t.Errorf("%s call_1: %+v", name, c1)
	}
	if c2 := byID(s.Activities, "call_2"); c2.State != agent.StateFailed || !strings.Contains(c2.Detail, "exit status 1") {
		t.Errorf("%s call_2: %+v", name, c2)
	}
	if c6 := byID(s.Activities, "call_6"); c6.State != agent.StateFailed {
		t.Errorf("%s call_6 (exit 2): %+v", name, c6)
	}
	if c7 := byID(s.Activities, "call_7"); c7.State != agent.StateRunning {
		t.Errorf("%s call_7 should be running: %+v", name, c7)
	}
	c3 := byID(s.Activities, "call_3")
	if c3.Kind != agent.KindEdit || c3.Removed != 2 || c3.Added != 3 || c3.OldText != "a\nb" || !strings.Contains(c3.Title, "x.go") || c3.State != agent.StateDone {
		t.Errorf("%s edit: %+v", name, c3)
	}
	p0, p1 := byID(s.Activities, "call_5#0"), byID(s.Activities, "call_5#1")
	if p0.Kind != agent.KindEdit || p0.Removed != 1 || p0.Added != 2 || p0.File != "/work/demo/a.go" ||
		p1.Added != 1 || p1.File != "b.go" || p1.State != agent.StateDone {
		t.Errorf("%s patch: %+v %+v", name, p0, p1)
	}
	if last := s.Activities[len(s.Activities)-1]; last.Kind != agent.KindMessage || last.Title != "All done" {
		t.Errorf("%s last: %+v", name, last)
	}
	if s.Usage != (agent.Usage{Input: 100, Output: 25, CacheRead: 300, CacheWrite: 40, Context: 440}) {
		t.Errorf("%s usage %+v", name, s.Usage)
	}
	if s.Session.Model != "claude-sonnet-4" {
		t.Errorf("%s model %q", name, s.Session.Model)
	}
	t4 := byID(s.Activities, "call_4")
	if t4.Kind != agent.KindSubagent || t4.Title != "explore: Map the code" || t4.SubagentID != "ses_child" || t4.State != agent.StateDone {
		t.Errorf("%s task: %+v", name, t4)
	}
	if len(s.Subagents) != 1 {
		t.Fatalf("%s subagents: %d", name, len(s.Subagents))
	}
	sa := s.Subagents[0]
	if sa.ID != "ses_child" || sa.ToolUseID != "call_4" || sa.Type != "explore" || sa.Description != "Map the code" ||
		!sa.Finished || sa.Live || sa.Model != "claude-haiku" || sa.Usage.Input != 7 || len(sa.Activities) != 1 {
		t.Errorf("%s sub: %+v", name, sa)
	}
	if g := sa.Activities[0]; g.Kind != agent.KindSearch || g.Title != "grep func main" || g.State != agent.StateDone {
		t.Errorf("%s sub grep: %+v", name, g)
	}
}

func TestTrackerSnapshot(t *testing.T) {
	for name, root := range map[string]string{"json": copyTestdata(t), "sqlite": sqliteFixture(t)} {
		src := NewWithRoot(root)
		tr := src.Open(sessionByID(t, src, "ses_main"))
		if changed, err := tr.Poll(); err != nil || !changed {
			t.Fatalf("%s poll: %v %v", name, changed, err)
		}
		checkSnapshot(t, name, tr.Snapshot())
	}
}

func TestIncrementalPollJSON(t *testing.T) {
	root := copyTestdata(t)
	src := NewWithRoot(root)
	tr := src.Open(sessionByID(t, src, "ses_main"))
	tr.Poll()
	if changed, _ := tr.Poll(); changed {
		t.Errorf("idle poll reported change")
	}
	part := filepath.Join(root, "storage", "part", "msg_a1", "prt_018.json")
	os.WriteFile(part, []byte(`{"id":"prt_018","messageID":"msg_a1","sessionID":"ses_main","type":"tool","callID":"call_7","tool":"bash","state":{"status":"completed","input":{"command":"sleep 60","description":"Wait"},"output":"slept","metadata":{"exit":0},"time":{"start":1767261640000,"end":1767261645000}}}`), 0o644)
	if changed, _ := tr.Poll(); !changed {
		t.Fatal("rewritten part not seen")
	}
	if a := byID(tr.Snapshot().Activities, "call_7"); a.State != agent.StateDone || a.Duration != 5*time.Second {
		t.Fatalf("call_7: %+v", a)
	}
	os.WriteFile(filepath.Join(root, "storage", "part", "msg_a1", "prt_099.json"), []byte(`{"id":"prt_099","messageID":"msg_a1","sessionID":"ses_main","type":"text","text":"Later"`), 0o644)
	if changed, _ := tr.Poll(); changed { // half-written file is ignored
		t.Fatal("partial file must not count")
	}
	os.WriteFile(filepath.Join(root, "storage", "part", "msg_a1", "prt_099.json"), []byte(`{"id":"prt_099","messageID":"msg_a1","sessionID":"ses_main","type":"text","text":"Later"}`), 0o644)
	if changed, _ := tr.Poll(); !changed {
		t.Fatal("completed file not seen")
	}
	acts := tr.Snapshot().Activities
	if acts[len(acts)-1].Title != "Later" {
		t.Fatalf("last: %+v", acts[len(acts)-1])
	}
}

func TestIncrementalPollSQLite(t *testing.T) {
	root := sqliteFixture(t)
	src := NewWithRoot(root)
	tr := src.Open(sessionByID(t, src, "ses_main"))
	tr.Poll()
	if changed, _ := tr.Poll(); changed {
		t.Errorf("idle poll reported change")
	}
	db, _ := sql.Open("sqlite", filepath.Join(root, "opencode.db"))
	defer db.Close()
	data := `{"id":"prt_018","messageID":"msg_a1","sessionID":"ses_main","type":"tool","callID":"call_7","tool":"bash","state":{"status":"completed","input":{"command":"sleep 60"},"output":"slept","metadata":{"exit":0},"time":{"start":1,"end":2}}}`
	if _, err := db.Exec(`UPDATE part SET data=?, time_updated=time_updated+5000 WHERE id='prt_018'`, data); err != nil {
		t.Fatal(err)
	}
	if changed, _ := tr.Poll(); !changed {
		t.Fatal("updated row not seen")
	}
	if a := byID(tr.Snapshot().Activities, "call_7"); a.State != agent.StateDone {
		t.Fatalf("call_7: %+v", a)
	}
	// the driver must refuse writes through the read-only connection
	ro, _ := (&sqlBackend{path: filepath.Join(root, "opencode.db")}).open()
	defer ro.Close()
	if _, err := ro.Exec(`DELETE FROM part`); err == nil {
		t.Fatal("read-only connection accepted a write")
	}
}

// TestSessionModelFromAssistantMessage covers real opencode data, where the
// session row names no model: the listing takes it from the newest assistant
// message, whether that has a modelID, a {providerID, modelID} object or a
// "providerID/modelID" string.
func TestSessionModelFromAssistantMessage(t *testing.T) {
	// the fixture's assistant message has a plain modelID
	for name, root := range map[string]string{"json": copyTestdata(t), "sqlite": sqliteFixture(t)} {
		if ss, _ := NewWithRoot(root).Sessions(repo); len(ss) == 0 || ss[0].Model != "claude-sonnet-4" {
			t.Errorf("%s: model of the listed session: %+v", name, ss)
		}
	}
	msgs := map[string]string{
		"object": `{"id":"m1","role":"assistant","time":{"created":5},"model":{"providerID":"deepseek","modelID":"v4-flash"}}`,
		"string": `{"id":"m1","role":"assistant","time":{"created":5},"model":"deepseek/v4-flash"}`,
	}
	const wantModel = "deepseek/v4-flash"
	for name, msg := range msgs {
		root := t.TempDir()
		dir := filepath.Join(root, "storage")
		for p, body := range map[string]string{
			"session/p1/ses_x.json": `{"id":"ses_x","projectID":"p1","directory":"/work/demo","title":"t","time":{"created":1,"updated":9}}`,
			"message/ses_x/m0.json": `{"id":"m0","role":"user","time":{"created":1},"model":{"providerID":"old","modelID":"user-model"}}`,
			"message/ses_x/m1.json": msg,
			"project/p1.json":       `{"id":"p1","worktree":"/work/demo"}`,
			"part/m1/prt_1.json":    `{"id":"prt_1","type":"text","text":"hi"}`,
		} {
			full := filepath.Join(dir, filepath.FromSlash(p))
			os.MkdirAll(filepath.Dir(full), 0o755)
			if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		src := NewWithRoot(root)
		ss, err := src.Sessions(repo)
		if err != nil || len(ss) != 1 || ss[0].Model != wantModel {
			t.Fatalf("%s: listed %+v %v", name, ss, err)
		}
		tr := src.Open(ss[0])
		if _, err := tr.Poll(); err != nil {
			t.Fatal(err)
		}
		if got := tr.Snapshot().Session.Model; got != wantModel {
			t.Errorf("%s: snapshot model %q", name, got)
		}
	}
}

// demoQuery spells the demo repo the way this OS may: with a different case and
// a trailing separator where file names fold case.
func demoQuery() string {
	switch runtime.GOOS {
	case "windows":
		return `\work\DEMO\`
	case "darwin":
		return "/work/DEMO/"
	}
	return "/work/demo/"
}
