package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func newRepo(t *testing.T) *Repo {
	t.Helper()
	dir := t.TempDir()
	gitCmd(t, dir, "init", "-q", "-b", "main")
	gitCmd(t, dir, "config", "user.name", "Test")
	gitCmd(t, dir, "config", "user.email", "t@example.com")
	gitCmd(t, dir, "config", "core.autocrlf", "false")
	gitCmd(t, dir, "config", "commit.gpgsign", "false")
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func write(t *testing.T, r *Repo, name, content string) {
	t.Helper()
	p := filepath.Join(r.Dir, name)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commitAll(t *testing.T, r *Repo, msg string) {
	t.Helper()
	if err := r.StageAll(); err != nil {
		t.Fatal(err)
	}
	if err := r.Commit(msg, false); err != nil {
		t.Fatal(err)
	}
}

func numbered(n int) []string {
	var s []string
	for i := 1; i <= n; i++ {
		s = append(s, fmt.Sprintf("line%d", i))
	}
	return s
}

func join(lines []string) string { return strings.Join(lines, "\n") + "\n" }

func TestOpenNotRepo(t *testing.T) {
	if _, err := Open(t.TempDir()); err == nil {
		t.Fatal("expected error")
	}
}

func TestStatus(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.txt", "a\n")
	write(t, r, "dir/with space.txt", "x\n")
	write(t, r, "old.txt", "one\ntwo\nthree\nfour\n")
	commitAll(t, r, "init")

	write(t, r, "a.txt", "changed\n")
	write(t, r, "new é.txt", "u\n")
	write(t, r, "dir/with space.txt", "y\n")
	r.Stage("dir/with space.txt")
	gitCmd(t, r.Dir, "mv", "old.txt", "renamed.txt")

	h, files, err := r.Status()
	if err != nil {
		t.Fatal(err)
	}
	if h.Branch != "main" || h.Detached {
		t.Fatalf("head %+v", h)
	}
	by := map[string]File{}
	for _, f := range files {
		by[f.Path] = f
	}
	if f := by["a.txt"]; f.Unstaged != 'M' || f.Staged != ' ' {
		t.Errorf("a.txt %+v", f)
	}
	if f := by["dir/with space.txt"]; f.Staged != 'M' || f.Unstaged != ' ' {
		t.Errorf("space %+v", f)
	}
	if f := by["new é.txt"]; !f.Untracked {
		t.Errorf("untracked %+v", f)
	}
	if f := by["renamed.txt"]; f.Staged != 'R' || f.OrigPath != "old.txt" {
		t.Errorf("rename %+v", f)
	}
}

func TestStageUnstageNoCommits(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.txt", "a\n")
	if err := r.Stage("a.txt"); err != nil {
		t.Fatal(err)
	}
	_, files, _ := r.Status()
	if files[0].Staged != 'A' {
		t.Fatalf("%+v", files)
	}
	if err := r.Unstage("a.txt"); err != nil {
		t.Fatal(err)
	}
	_, files, _ = r.Status()
	if !files[0].Untracked {
		t.Fatalf("%+v", files)
	}
	if err := r.StageAll(); err != nil {
		t.Fatal(err)
	}
	if err := r.UnstageAll(); err != nil {
		t.Fatal(err)
	}
	_, files, _ = r.Status()
	if !files[0].Untracked {
		t.Fatalf("%+v", files)
	}
}

func TestStageUnstageDiscard(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.txt", "a\n")
	commitAll(t, r, "init")
	write(t, r, "a.txt", "b\n")
	write(t, r, "u.txt", "u\n")

	if err := r.Stage("a.txt"); err != nil {
		t.Fatal(err)
	}
	if d, _ := r.DiffFile("a.txt", true); !strings.Contains(d, "+b") {
		t.Errorf("staged diff %q", d)
	}
	if err := r.Unstage("a.txt"); err != nil {
		t.Fatal(err)
	}
	if d, _ := r.DiffFile("a.txt", false); !strings.Contains(d, "+b") {
		t.Errorf("unstaged diff %q", d)
	}
	d, err := r.DiffFile("u.txt", false)
	if err != nil || !strings.Contains(d, "+++ b/u.txt") || !strings.Contains(d, "+u\n") {
		t.Errorf("untracked diff %q %v", d, err)
	}
	if fd := ParseDiff(d); len(fd) != 1 || len(fd[0].Hunks) != 1 {
		t.Errorf("parse untracked diff: %+v", fd)
	}

	r.Stage("a.txt")
	if err := r.Discard("a.txt"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(r.Dir, "a.txt")); string(b) != "a\n" {
		t.Errorf("a.txt = %q", b)
	}
	if err := r.Discard("u.txt"); err != nil {
		t.Fatal(err)
	}
	_, files, _ := r.Status()
	if len(files) != 0 {
		t.Errorf("files %+v", files)
	}
}

// twoHunk makes a 40-line file committed, then modifies line 2 and line 30
// (replacing line 30 and inserting a line after it).
func twoHunk(t *testing.T) *Repo {
	r := newRepo(t)
	base := numbered(40)
	write(t, r, "f.txt", join(base))
	commitAll(t, r, "init")
	mod := append([]string(nil), base...)
	mod[1] = "CHANGED2"
	mod[29] = "CHANGED30\nINSERTED"
	write(t, r, "f.txt", join(mod))
	return r
}

func fileDiff(t *testing.T, r *Repo, staged bool) FileDiff {
	t.Helper()
	d, err := r.DiffFile("f.txt", staged)
	if err != nil {
		t.Fatal(err)
	}
	fds := ParseDiff(d)
	if len(fds) != 1 {
		t.Fatalf("diff files: %d\n%s", len(fds), d)
	}
	return fds[0]
}

func lineIdx(t *testing.T, h Hunk, kind byte, text string) int {
	t.Helper()
	for i, l := range h.Lines {
		if l.Kind == kind && l.Text == text {
			return i
		}
	}
	t.Fatalf("line %c%s not found in %+v", kind, text, h.Lines)
	return -1
}

func TestParseDiff(t *testing.T) {
	r := twoHunk(t)
	fd := fileDiff(t, r, false)
	if len(fd.Hunks) != 2 || fd.Path != "f.txt" {
		t.Fatalf("%+v", fd)
	}
	h := fd.Hunks[0]
	if h.OldStart != 1 || h.OldLines != 5 || h.NewStart != 1 || h.NewLines != 5 {
		t.Errorf("hunk0 %+v", h)
	}
}

func TestHunkStaging(t *testing.T) {
	r := twoHunk(t)
	fd := fileDiff(t, r, false)
	patch := BuildPatch(fd, 1, nil, false)
	if err := r.ApplyPatch(patch, true, false); err != nil {
		t.Fatalf("%v\n%s", err, patch)
	}
	cached := gitCmd(t, r.Dir, "diff", "--cached")
	if !strings.Contains(cached, "+CHANGED30") || strings.Contains(cached, "CHANGED2\n") {
		t.Errorf("cached diff:\n%s", cached)
	}
	// worktree still has hunk 0 unstaged
	if d := gitCmd(t, r.Dir, "diff"); !strings.Contains(d, "+CHANGED2") || strings.Contains(d, "CHANGED30") {
		t.Errorf("worktree diff:\n%s", d)
	}
}

func TestLineStagingAndUnstaging(t *testing.T) {
	r := twoHunk(t)
	fd := fileDiff(t, r, false)
	h := fd.Hunks[1]
	ins := lineIdx(t, h, '+', "INSERTED")
	patch := BuildPatch(fd, 1, map[int]bool{ins: true}, false)
	if err := r.ApplyPatch(patch, true, false); err != nil {
		t.Fatalf("%v\n%s", err, patch)
	}
	cached := gitCmd(t, r.Dir, "diff", "--cached")
	if !strings.Contains(cached, "+INSERTED") || strings.Contains(cached, "CHANGED30") || strings.Contains(cached, "-line30") {
		t.Errorf("cached diff:\n%s", cached)
	}

	// stage the rest of hunk 1 too, then unstage only INSERTED.
	r.Stage("f.txt")
	sfd := fileDiff(t, r, true)
	sh := sfd.Hunks[1]
	ins = lineIdx(t, sh, '+', "INSERTED")
	patch = BuildPatch(sfd, 1, map[int]bool{ins: true}, true)
	if err := r.ApplyPatch(patch, true, true); err != nil {
		t.Fatalf("%v\n%s", err, patch)
	}
	cached = gitCmd(t, r.Dir, "diff", "--cached")
	if strings.Contains(cached, "INSERTED") || !strings.Contains(cached, "+CHANGED30") || !strings.Contains(cached, "+CHANGED2") {
		t.Errorf("after unstage line:\n%s", cached)
	}
}

func TestDiscardHunk(t *testing.T) {
	r := twoHunk(t)
	fd := fileDiff(t, r, false)
	patch := BuildPatch(fd, 0, nil, true)
	if err := r.ApplyPatch(patch, false, true); err != nil {
		t.Fatalf("%v\n%s", err, patch)
	}
	b, _ := os.ReadFile(filepath.Join(r.Dir, "f.txt"))
	s := string(b)
	if strings.Contains(s, "CHANGED2\n") || !strings.Contains(s, "line2\n") || !strings.Contains(s, "CHANGED30") {
		t.Errorf("file:\n%s", s)
	}
}

func TestPatchCRLFAndNoNewline(t *testing.T) {
	r := newRepo(t)
	write(t, r, "c.txt", "a\r\nb\r\nc\r\nd")
	commitAll(t, r, "init")
	write(t, r, "c.txt", "a\r\nB\r\nc\r\nD")
	d, _ := r.DiffFile("c.txt", false)
	fd := ParseDiff(d)[0]
	patch := BuildPatch(fd, 0, nil, false)
	if !strings.Contains(patch, "\r\n+B\r") && !strings.Contains(patch, "+B\r\n") {
		t.Errorf("CR lost: %q", patch)
	}
	if err := r.ApplyPatch(patch, true, false); err != nil {
		t.Fatalf("%v\n%q", err, patch)
	}
	if c := gitCmd(t, r.Dir, "diff", "--cached"); !strings.Contains(c, "No newline at end of file") {
		t.Errorf("cached:\n%s", c)
	}
	// only the "B" line: the D/no-newline part must be skipped.
	r.UnstageAll()
	idx := -1
	for i, l := range fd.Hunks[0].Lines {
		if l.Kind == '+' && strings.HasPrefix(l.Text, "B") {
			idx = i
		}
	}
	patch = BuildPatch(fd, 0, map[int]bool{idx: true}, false)
	if err := r.ApplyPatch(patch, true, false); err != nil {
		t.Fatalf("%v\n%q", err, patch)
	}
	if c := gitCmd(t, r.Dir, "diff", "--cached"); strings.Contains(c, "+D") {
		t.Errorf("cached:\n%s", c)
	}
}

func TestCommitAmendLog(t *testing.T) {
	r := newRepo(t)
	var logged []string
	r.LogFunc = func(c string) { logged = append(logged, c) }
	if cs, err := r.Log0(); err != nil || len(cs) != 0 {
		t.Fatalf("empty log: %v %v", cs, err)
	}
	write(t, r, "a.txt", "a\n")
	write(t, r, "d/b c.txt", "b\n")
	commitAll(t, r, "first\n\nbody line")
	if m := r.LastCommitMessage(); m != "first\n\nbody line" {
		t.Errorf("msg %q", m)
	}
	write(t, r, "a.txt", "a2\n")
	r.StageAll()
	if err := r.Commit("second", true); err != nil {
		t.Fatal(err)
	}
	cs, err := r.Log("", 10)
	if err != nil || len(cs) != 1 {
		t.Fatalf("log %v %v", cs, err)
	}
	c := cs[0]
	if c.Subject != "second" || c.Author != "Test" || c.Email != "t@example.com" || c.Date.IsZero() || len(c.Hash) != 40 || c.Short == "" {
		t.Errorf("%+v", c)
	}
	if !strings.Contains(c.Refs, "main") {
		t.Errorf("refs %q", c.Refs)
	}
	files, err := r.CommitFiles(c.Hash)
	if err != nil || len(files) != 2 || files[1].Path != "d/b c.txt" || files[0].Status != 'A' {
		t.Errorf("files %+v %v", files, err)
	}
	if d, err := r.DiffCommit(c.Hash, "a.txt"); err != nil || !strings.Contains(d, "+a2") || strings.Contains(d, "b c") {
		t.Errorf("diffcommit %q %v", d, err)
	}
	g, err := r.GraphLog(10)
	if err != nil || len(g) != 1 || !strings.Contains(g[0], "second") {
		t.Errorf("graph %v %v", g, err)
	}
	if len(logged) == 0 || !strings.HasPrefix(logged[0], "git ") {
		t.Errorf("log callback %v", logged)
	}
	re, err := r.Reflog(5)
	if err != nil || len(re) != 2 {
		t.Errorf("reflog %+v %v", re, err)
	}
}

// Log0 is a tiny helper so the empty-repo case reads clearly.
func (r *Repo) Log0() ([]Commit, error) { return r.Log("", 5) }

func TestBranches(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.txt", "a\n")
	commitAll(t, r, "init")
	if err := r.CreateBranch("feat", "", false); err != nil {
		t.Fatal(err)
	}
	if err := r.CreateBranch("feat2", "main", true); err != nil {
		t.Fatal(err)
	}
	if err := r.Checkout("main"); err != nil {
		t.Fatal(err)
	}
	if err := r.RenameBranch("feat", "feature"); err != nil {
		t.Fatal(err)
	}
	if err := r.DeleteBranch("feat2", false); err != nil {
		t.Fatal(err)
	}
	bs, err := r.Branches()
	if err != nil || len(bs) != 2 {
		t.Fatalf("%+v %v", bs, err)
	}
	names := map[string]Branch{}
	for _, b := range bs {
		names[b.Name] = b
	}
	if !names["main"].Head || names["feature"].Head || names["feature"].Date.IsZero() {
		t.Errorf("%+v", bs)
	}
}

func TestStash(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.txt", "a\n")
	commitAll(t, r, "init")
	write(t, r, "a.txt", "one\n")
	if err := r.StashPush("first", false); err != nil {
		t.Fatal(err)
	}
	write(t, r, "a.txt", "two\n")
	write(t, r, "u.txt", "u\n")
	if err := r.StashPush("second", true); err != nil {
		t.Fatal(err)
	}
	ss, err := r.Stashes()
	if err != nil || len(ss) != 2 {
		t.Fatalf("%+v %v", ss, err)
	}
	if ss[0].Ref != "stash@{0}" || !strings.Contains(ss[0].Subject, "second") || !strings.Contains(ss[1].Subject, "first") {
		t.Errorf("%+v", ss)
	}
	if d, err := r.StashDiff(1); err != nil || !strings.Contains(d, "+one") {
		t.Errorf("diff %q %v", d, err)
	}
	if err := r.StashPop(1); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(r.Dir, "a.txt")); string(b) != "one\n" {
		t.Errorf("a.txt %q", b)
	}
	if err := r.StashDrop(0); err != nil {
		t.Fatal(err)
	}
	if ss, _ := r.Stashes(); len(ss) != 0 {
		t.Errorf("%+v", ss)
	}
}

func TestReset(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.txt", "a\n")
	commitAll(t, r, "one")
	write(t, r, "a.txt", "b\n")
	commitAll(t, r, "two")
	if err := r.Reset("HEAD~1", "soft"); err != nil {
		t.Fatal(err)
	}
	if _, files, _ := r.Status(); len(files) != 1 || files[0].Staged != 'M' {
		t.Errorf("soft: %+v", files)
	}
	if err := r.Reset("HEAD", "hard"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(r.Dir, "a.txt")); string(b) != "a\n" {
		t.Errorf("hard: %q", b)
	}
	if err := r.Reset("HEAD", "bogus"); err == nil {
		t.Error("expected error")
	}
}

func TestMergeConflictStateAbort(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.txt", "base\n")
	commitAll(t, r, "base")
	r.CreateBranch("other", "", true)
	write(t, r, "a.txt", "other\n")
	commitAll(t, r, "other")
	r.Checkout("main")
	write(t, r, "a.txt", "main\n")
	commitAll(t, r, "main")

	if r.State() != Normal {
		t.Fatal("expected normal")
	}
	if err := r.Merge("other"); err == nil {
		t.Fatal("expected conflict")
	}
	if r.State() != Merging {
		t.Fatalf("state %v", r.State())
	}
	_, files, _ := r.Status()
	if len(files) != 1 || !files[0].Conflicted {
		t.Errorf("%+v", files)
	}
	if err := r.Abort(); err != nil {
		t.Fatal(err)
	}
	if r.State() != Normal {
		t.Fatalf("state after abort %v", r.State())
	}
	if err := r.Abort(); err == nil {
		t.Error("abort with nothing to abort should fail")
	}

	// merge again, resolve, continue
	r.Merge("other")
	write(t, r, "a.txt", "resolved\n")
	r.Stage("a.txt")
	if err := r.Continue(); err != nil {
		t.Fatal(err)
	}
	if r.State() != Normal {
		t.Fatalf("state after continue %v", r.State())
	}
}

func TestRemotePushPullFetch(t *testing.T) {
	remote := t.TempDir()
	gitCmd(t, remote, "init", "-q", "--bare", "-b", "main")

	a := newRepo(t)
	gitCmd(t, a.Dir, "remote", "add", "origin", remote)
	write(t, a, "a.txt", "a\n")
	commitAll(t, a, "init")
	ctx := context.Background()
	if err := a.Push(ctx, false, true); err != nil {
		t.Fatal(err)
	}
	h, _, _ := a.Status()
	if h.Upstream != "origin/main" {
		t.Fatalf("head %+v", h)
	}
	rs, err := a.Remotes()
	if err != nil || len(rs) != 1 || rs[0].Name != "origin" {
		t.Errorf("%+v %v", rs, err)
	}

	// second clone pushes a commit, first pulls it.
	bdir := filepath.Join(t.TempDir(), "b")
	gitCmd(t, ".", "clone", "-q", remote, bdir)
	b, err := Open(bdir)
	if err != nil {
		t.Fatal(err)
	}
	gitCmd(t, b.Dir, "config", "user.name", "B")
	gitCmd(t, b.Dir, "config", "user.email", "b@example.com")
	write(t, b, "b.txt", "b\n")
	commitAll(t, b, "from b")
	if err := b.Push(ctx, false, false); err != nil {
		t.Fatal(err)
	}

	if err := a.Fetch(ctx); err != nil {
		t.Fatal(err)
	}
	h, _, _ = a.Status()
	if h.Behind != 1 {
		t.Errorf("behind %+v", h)
	}
	if err := a.Pull(ctx, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(a.Dir, "b.txt")); err != nil {
		t.Error("pull did not bring b.txt")
	}

	// diverge, rebase pull, force push
	write(t, a, "a.txt", "a2\n")
	commitAll(t, a, "a2")
	write(t, b, "b.txt", "b2\n")
	commitAll(t, b, "b2")
	b.Push(ctx, false, false)
	if err := a.Pull(ctx, false); err == nil {
		t.Error("ff-only pull should fail when diverged")
	}
	if err := a.Pull(ctx, true); err != nil {
		t.Fatal(err)
	}
	if err := a.Push(ctx, false, false); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, a.Dir, "commit", "-q", "--amend", "-m", "a2 amended")
	if err := a.Push(ctx, false, false); err == nil {
		t.Error("non-ff push should fail")
	}
	if err := a.Push(ctx, true, false); err != nil {
		t.Fatal(err)
	}
	b.Fetch(ctx)
	bs, _ := b.Branches()
	var haveRemote bool
	for _, br := range bs {
		if br.Remote && br.Name == "origin/main" {
			haveRemote = true
		}
	}
	if !haveRemote {
		t.Errorf("branches %+v", bs)
	}
}

func TestFetchCancelled(t *testing.T) {
	r := newRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := r.Fetch(ctx); err == nil {
		t.Error("expected error")
	}
}

func TestDiffHead(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.txt", "one\n")
	commitAll(t, r, "init")
	write(t, r, "a.txt", "one\ntwo\n")
	if err := r.Stage("a.txt"); err != nil {
		t.Fatal(err)
	}
	write(t, r, "a.txt", "one\ntwo\nthree\n")
	write(t, r, "new.txt", "n\n")
	out, err := r.DiffHead("a.txt")
	if err != nil || !strings.Contains(out, "+two") || !strings.Contains(out, "+three") {
		t.Fatalf("tracked: %q %v", out, err)
	}
	out, err = r.DiffHead("new.txt")
	if err != nil || !strings.Contains(out, "+n") {
		t.Fatalf("untracked: %q %v", out, err)
	}
}
