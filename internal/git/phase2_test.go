package git

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// subjects returns commit subjects, oldest first.
func subjects(t *testing.T, r *Repo) []string {
	t.Helper()
	cs, err := r.Log("", 0)
	if err != nil {
		t.Fatal(err)
	}
	var s []string
	for i := len(cs) - 1; i >= 0; i-- {
		s = append(s, cs[i].Subject)
	}
	return s
}

// hashOf returns the hash of the commit with the given subject.
func hashOf(t *testing.T, r *Repo, subject string) string {
	t.Helper()
	cs, _ := r.Log("", 0)
	for _, c := range cs {
		if c.Subject == subject {
			return c.Hash
		}
	}
	t.Fatalf("no commit %q", subject)
	return ""
}

// fourCommits makes one, two, three, four, each adding its own file.
func fourCommits(t *testing.T) *Repo {
	r := newRepo(t)
	for _, n := range []string{"one", "two", "three", "four"} {
		write(t, r, n+".txt", n+"\n")
		commitAll(t, r, n)
	}
	return r
}

func wantSubjects(t *testing.T, r *Repo, want ...string) {
	t.Helper()
	if got := subjects(t, r); !reflect.DeepEqual(got, want) {
		t.Fatalf("history = %v, want %v", got, want)
	}
	if r.State() != Normal {
		t.Fatalf("state = %v", r.State())
	}
}

func TestRebaseActions(t *testing.T) {
	t.Run("reword", func(t *testing.T) {
		r := fourCommits(t)
		if err := r.Reword(hashOf(t, r, "two"), "renamed\n\nbody"); err != nil {
			t.Fatal(err)
		}
		wantSubjects(t, r, "one", "renamed", "three", "four")
	})
	t.Run("reword root", func(t *testing.T) {
		r := fourCommits(t)
		if err := r.Reword(hashOf(t, r, "one"), "first"); err != nil {
			t.Fatal(err)
		}
		wantSubjects(t, r, "first", "two", "three", "four")
	})
	t.Run("squash", func(t *testing.T) {
		r := fourCommits(t)
		if err := r.SquashInto(hashOf(t, r, "three")); err != nil {
			t.Fatal(err)
		}
		wantSubjects(t, r, "one", "two", "four")
		if msg := gitCmd(t, r.Dir, "log", "-1", "--format=%B", "HEAD~1"); !strings.Contains(msg, "three") {
			t.Fatalf("squash lost the message: %q", msg)
		}
	})
	t.Run("squash root child", func(t *testing.T) {
		r := fourCommits(t)
		if err := r.SquashInto(hashOf(t, r, "two")); err != nil {
			t.Fatal(err)
		}
		wantSubjects(t, r, "one", "three", "four")
	})
	t.Run("squash root fails", func(t *testing.T) {
		r := fourCommits(t)
		if err := r.SquashInto(hashOf(t, r, "one")); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("fixup", func(t *testing.T) {
		r := fourCommits(t)
		if err := r.Fixup(hashOf(t, r, "three")); err != nil {
			t.Fatal(err)
		}
		wantSubjects(t, r, "one", "two", "four")
		if msg := gitCmd(t, r.Dir, "log", "-1", "--format=%B", "HEAD~1"); strings.Contains(msg, "three") {
			t.Fatalf("fixup kept the message: %q", msg)
		}
		if _, err := os.Stat(filepath.Join(r.Dir, "three.txt")); err != nil {
			t.Fatal("fixup lost the file")
		}
	})
	t.Run("drop", func(t *testing.T) {
		r := fourCommits(t)
		if err := r.Drop(hashOf(t, r, "two")); err != nil {
			t.Fatal(err)
		}
		wantSubjects(t, r, "one", "three", "four")
		if _, err := os.Stat(filepath.Join(r.Dir, "two.txt")); err == nil {
			t.Fatal("two.txt still exists")
		}
	})
	t.Run("move", func(t *testing.T) {
		r := fourCommits(t)
		if err := r.MoveUp(hashOf(t, r, "two")); err != nil {
			t.Fatal(err)
		}
		wantSubjects(t, r, "one", "three", "two", "four")
		if err := r.MoveDown(hashOf(t, r, "two")); err != nil {
			t.Fatal(err)
		}
		wantSubjects(t, r, "one", "two", "three", "four")
		if err := r.MoveUp(hashOf(t, r, "four")); err == nil {
			t.Fatal("moving HEAD up should fail")
		}
		if err := r.MoveDown(hashOf(t, r, "one")); err == nil {
			t.Fatal("moving root down should fail")
		}
	})
	t.Run("edit then continue", func(t *testing.T) {
		r := fourCommits(t)
		if err := r.EditCommit(hashOf(t, r, "two")); err != nil {
			t.Fatal(err)
		}
		if r.State() != Rebasing {
			t.Fatalf("state = %v", r.State())
		}
		write(t, r, "extra.txt", "x\n")
		if err := r.Stage("extra.txt"); err != nil {
			t.Fatal(err)
		}
		if err := r.Commit("", true); err != nil {
			t.Fatal(err)
		}
		if err := r.Continue(); err != nil {
			t.Fatal(err)
		}
		wantSubjects(t, r, "one", "two", "three", "four")
		if _, err := os.Stat(filepath.Join(r.Dir, "extra.txt")); err != nil {
			t.Fatal("amend lost")
		}
	})
	t.Run("copyCmd quoting", func(t *testing.T) {
		if got := copyCmd(`C:\a b\it's`); got != `cp 'C:/a b/it'\''s'` {
			t.Fatal(got)
		}
	})
}

func TestRebaseInteractiveTodo(t *testing.T) {
	r := fourCommits(t)
	todo, err := r.RebaseTodo("--root")
	if err != nil || len(todo) != 4 || todo[0].Subject != "one" || todo[0].Action != "pick" {
		t.Fatalf("todo = %v, %v", todo, err)
	}
	todo[1], todo[3] = todo[3], todo[1] // reorder: one four three two
	todo[2].Action = "drop"
	if err := r.RebaseInteractive("--root", todo); err != nil {
		t.Fatal(err)
	}
	wantSubjects(t, r, "one", "four", "two")
}

func TestRebaseConflictAbort(t *testing.T) {
	r := newRepo(t)
	for _, v := range []string{"a", "b", "c"} {
		write(t, r, "x.txt", v+"\n")
		commitAll(t, r, "set "+v)
	}
	before := hashOf(t, r, "set c")
	if err := r.Drop(hashOf(t, r, "set b")); err == nil {
		t.Fatal("expected conflict error")
	}
	if r.State() != Rebasing {
		t.Fatalf("state = %v", r.State())
	}
	if err := r.Abort(); err != nil {
		t.Fatal(err)
	}
	if r.State() != Normal || hashOf(t, r, "set c") != before {
		t.Fatal("abort did not restore")
	}
}

func TestFixupInto(t *testing.T) {
	r := fourCommits(t)
	write(t, r, "two.txt", "two fixed\n")
	if err := r.Stage("two.txt"); err != nil {
		t.Fatal(err)
	}
	if err := r.FixupInto(hashOf(t, r, "two")); err != nil {
		t.Fatal(err)
	}
	wantSubjects(t, r, "one", "two", "three", "four")
	if got := gitCmd(t, r.Dir, "show", hashOf(t, r, "two")+":two.txt"); got != "two fixed\n" {
		t.Fatalf("two.txt = %q", got)
	}
	if err := r.FixupInto(hashOf(t, r, "two")); err == nil {
		t.Fatal("expected nothing-staged error")
	}
}

func TestCherryPickRevert(t *testing.T) {
	r := newRepo(t)
	write(t, r, "base.txt", "b\n")
	commitAll(t, r, "base")
	if err := r.CreateBranch("feat", "", true); err != nil {
		t.Fatal(err)
	}
	write(t, r, "f1.txt", "1\n")
	commitAll(t, r, "f1")
	write(t, r, "f2.txt", "2\n")
	commitAll(t, r, "f2")
	f1, f2 := hashOf(t, r, "f1"), hashOf(t, r, "f2")
	if err := r.Checkout("main"); err != nil {
		t.Fatal(err)
	}
	if err := r.CherryPick(f1, f2); err != nil {
		t.Fatal(err)
	}
	wantSubjects(t, r, "base", "f1", "f2")
	if err := r.Revert(hashOf(t, r, "f2")); err != nil {
		t.Fatal(err)
	}
	if got := subjects(t, r); got[len(got)-1] != `Revert "f2"` {
		t.Fatalf("history = %v", got)
	}
	if _, err := os.Stat(filepath.Join(r.Dir, "f2.txt")); err == nil {
		t.Fatal("f2.txt survived revert")
	}
	if err := r.CherryPick(); err == nil {
		t.Fatal("expected error for empty pick")
	}
}

func TestRevertMerge(t *testing.T) {
	r := newRepo(t)
	write(t, r, "base.txt", "b\n")
	commitAll(t, r, "base")
	gitCmd(t, r.Dir, "checkout", "-q", "-b", "feat")
	write(t, r, "feat.txt", "f\n")
	commitAll(t, r, "feat work")
	gitCmd(t, r.Dir, "checkout", "-q", "main")
	write(t, r, "main.txt", "m\n")
	commitAll(t, r, "main work")
	gitCmd(t, r.Dir, "merge", "--no-ff", "-m", "merge feat", "feat")
	if err := r.Revert("HEAD"); err == nil {
		t.Fatal("plain revert of a merge should fail")
	}
	if r.State() == Reverting {
		r.Abort()
	}
	if err := r.RevertMerge("HEAD", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(r.Dir, "feat.txt")); err == nil {
		t.Fatal("feat.txt survived merge revert")
	}
	if _, err := os.Stat(filepath.Join(r.Dir, "main.txt")); err != nil {
		t.Fatal("main.txt lost")
	}
}

func bareRemote(t *testing.T, r *Repo, name string) string {
	t.Helper()
	dir := t.TempDir()
	gitCmd(t, dir, "init", "-q", "--bare", "-b", "main")
	gitCmd(t, r.Dir, "remote", "add", name, dir)
	return dir
}

func TestTags(t *testing.T) {
	r := fourCommits(t)
	remote := bareRemote(t, r, "origin")
	if err := r.CreateTag("light", hashOf(t, r, "two"), ""); err != nil {
		t.Fatal(err)
	}
	if err := r.CreateTag("v1", "", "release one\n\ndetails"); err != nil {
		t.Fatal(err)
	}
	tags, err := r.Tags()
	if err != nil || len(tags) != 2 {
		t.Fatalf("tags = %v, %v", tags, err)
	}
	by := map[string]Tag{}
	for _, tg := range tags {
		by[tg.Name] = tg
	}
	if l := by["light"]; l.Annotated || l.Hash != hashOf(t, r, "two") || l.Subject != "two" || l.Date.IsZero() {
		t.Fatalf("light = %+v", l)
	}
	if v := by["v1"]; !v.Annotated || v.Hash != hashOf(t, r, "four") || v.Subject != "release one" {
		t.Fatalf("v1 = %+v", v)
	}
	ctx := context.Background()
	if err := r.PushTag(ctx, "origin", "v1"); err != nil {
		t.Fatal(err)
	}
	if out := gitCmd(t, remote, "tag", "-l"); strings.TrimSpace(out) != "v1" {
		t.Fatalf("remote tags = %q", out)
	}
	if err := r.DeleteRemoteTag(ctx, "origin", "v1"); err != nil {
		t.Fatal(err)
	}
	if out := gitCmd(t, remote, "tag", "-l"); strings.TrimSpace(out) != "" {
		t.Fatalf("remote tags = %q", out)
	}
	if err := r.DeleteTag("light"); err != nil {
		t.Fatal(err)
	}
	if err := r.DeleteTag("v1"); err != nil {
		t.Fatal(err)
	}
	if tags, _ := r.Tags(); len(tags) != 0 {
		t.Fatalf("tags left: %v", tags)
	}
}

func TestDeleteRemoteBranchAndPushRemote(t *testing.T) {
	r := fourCommits(t)
	remote := bareRemote(t, r, "up") // the only remote, not named origin
	ctx := context.Background()
	if err := r.CreateBranch("topic", "", true); err != nil {
		t.Fatal(err)
	}
	if err := r.Push(ctx, false, true); err != nil {
		t.Fatalf("push with only remote up: %v", err)
	}
	if up := gitCmd(t, r.Dir, "rev-parse", "--abbrev-ref", "@{u}"); strings.TrimSpace(up) != "up/topic" {
		t.Fatalf("upstream = %q", up)
	}
	if out := gitCmd(t, remote, "branch", "--list", "topic"); !strings.Contains(out, "topic") {
		t.Fatalf("remote branches = %q", out)
	}
	if err := r.DeleteRemoteBranch(ctx, "up", "topic"); err != nil {
		t.Fatal(err)
	}
	if out := gitCmd(t, remote, "branch", "--list", "topic"); strings.TrimSpace(out) != "" {
		t.Fatalf("remote branches = %q", out)
	}
}

func TestWorktrees(t *testing.T) {
	r := fourCommits(t)
	path := filepath.Join(t.TempDir(), "wt")
	if err := r.AddWorktree(path, "wtbranch", true); err != nil {
		t.Fatal(err)
	}
	ws, err := r.Worktrees()
	if err != nil || len(ws) != 2 {
		t.Fatalf("worktrees = %+v, %v", ws, err)
	}
	if !ws[0].Main || ws[1].Main || ws[1].Branch != "wtbranch" || filepath.Base(ws[1].Path) != "wt" || ws[1].Head == "" || ws[0].Branch != "main" {
		t.Fatalf("worktrees = %+v", ws)
	}
	if err := r.RemoveWorktree(path, false); err != nil {
		t.Fatal(err)
	}
	if ws, _ := r.Worktrees(); len(ws) != 1 {
		t.Fatalf("worktrees = %+v", ws)
	}
	// existing branch, then delete the directory behind git's back and prune
	if err := r.AddWorktree(path, "wtbranch", false); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	if ws, _ := r.Worktrees(); len(ws) != 2 || !ws[1].Prunable {
		t.Fatalf("worktrees = %+v", ws)
	}
	if err := r.PruneWorktrees(); err != nil {
		t.Fatal(err)
	}
	if ws, _ := r.Worktrees(); len(ws) != 1 {
		t.Fatalf("worktrees = %+v", ws)
	}
}

func TestUndoRedoCommits(t *testing.T) {
	r := fourCommits(t)
	for want := 3; want >= 1; want-- {
		if err := r.Undo(); err != nil {
			t.Fatal(err)
		}
		if got := len(subjects(t, r)); got != want {
			t.Fatalf("after undo: %d commits, want %d", got, want)
		}
	}
	if _, ok, _ := r.UndoTarget(); ok {
		t.Fatal("initial commit should not be undoable")
	}
	if err := r.Undo(); err == nil {
		t.Fatal("expected nothing to undo")
	}
	if u, ok, _ := r.RedoTarget(); !ok || u.Kind != "reset" || u.Hash == "" {
		t.Fatalf("redo target = %+v %v", u, ok)
	}
	for want := 2; want <= 4; want++ {
		if err := r.Redo(); err != nil {
			t.Fatal(err)
		}
		if got := len(subjects(t, r)); got != want {
			t.Fatalf("after redo: %d commits, want %d", got, want)
		}
	}
	if err := r.Redo(); err == nil {
		t.Fatal("expected nothing to redo")
	}
	// a new action makes the saved state stale; undo works from the new top
	write(t, r, "five.txt", "5\n")
	commitAll(t, r, "five")
	if err := r.Undo(); err != nil {
		t.Fatal(err)
	}
	wantSubjects(t, r, "one", "two", "three", "four")
	if err := r.Redo(); err != nil {
		t.Fatal(err)
	}
	wantSubjects(t, r, "one", "two", "three", "four", "five")
}

func TestUndoRedoCheckoutAndAmend(t *testing.T) {
	r := fourCommits(t)
	if err := r.Commit("four amended", true); err != nil {
		t.Fatal(err)
	}
	if err := r.CreateBranch("other", "", true); err != nil {
		t.Fatal(err)
	}
	head := func() string { h, _, _ := r.Status(); return h.Branch }
	u, ok, err := r.UndoTarget()
	if err != nil || !ok || u.Kind != "checkout" || u.Branch != "main" {
		t.Fatalf("target = %+v %v %v", u, ok, err)
	}
	if err := r.Undo(); err != nil || head() != "main" {
		t.Fatalf("undo checkout: %v on %q", err, head())
	}
	// next undo is the amend, skipping our own checkout entry
	if err := r.Undo(); err != nil {
		t.Fatal(err)
	}
	wantSubjects(t, r, "one", "two", "three", "four")
	if err := r.Redo(); err != nil {
		t.Fatal(err)
	}
	wantSubjects(t, r, "one", "two", "three", "four amended")
	if err := r.Redo(); err != nil || head() != "other" {
		t.Fatalf("redo checkout: %v on %q", err, head())
	}
}

func TestUndoRefusesDirtyTree(t *testing.T) {
	r := fourCommits(t)
	write(t, r, "four.txt", "local edit\n")
	err := r.Undo()
	if err == nil || !strings.Contains(err.Error(), "uncommitted") {
		t.Fatalf("err = %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(r.Dir, "four.txt")); string(b) != "local edit\n" {
		t.Fatal("edit lost")
	}
	if len(subjects(t, r)) != 4 {
		t.Fatal("history changed")
	}
}

func TestUndoRebase(t *testing.T) {
	r := fourCommits(t)
	old := hashOf(t, r, "four")
	if err := r.Drop(hashOf(t, r, "two")); err != nil {
		t.Fatal(err)
	}
	if err := r.Undo(); err != nil {
		t.Fatal(err)
	}
	if hashOf(t, r, "four") != old {
		t.Fatal("undo of rebase did not restore HEAD")
	}
}

func TestDiscardUnstaged(t *testing.T) {
	r := newRepo(t)
	write(t, r, "f.txt", "base\n")
	commitAll(t, r, "init")
	write(t, r, "f.txt", "staged\n")
	if err := r.Stage("f.txt"); err != nil {
		t.Fatal(err)
	}
	write(t, r, "f.txt", "worktree\n")
	if err := r.DiscardUnstaged("f.txt"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(r.Dir, "f.txt")); string(b) != "staged\n" {
		t.Fatalf("worktree = %q", b)
	}
	if out := gitCmd(t, r.Dir, "diff", "--cached", "--name-only"); strings.TrimSpace(out) != "f.txt" {
		t.Fatalf("staged = %q", out)
	}
}

func TestCommitBranches(t *testing.T) {
	r := fourCommits(t)
	gitCmd(t, r.Dir, "branch", "early", hashOf(t, r, "two"))
	bs, err := r.CommitBranches(hashOf(t, r, "one"))
	if err != nil || len(bs) != 2 {
		t.Fatalf("branches = %v, %v", bs, err)
	}
	if bs, _ := r.CommitBranches(hashOf(t, r, "four")); len(bs) != 1 || bs[0] != "main" {
		t.Fatalf("branches = %v", bs)
	}
}

func TestParseDiffQuotedPaths(t *testing.T) {
	text := `diff --git "a/sp\tace.txt" "b/sp\tace.txt"
index 1..2 100644
--- "a/sp\tace.txt"
+++ "b/sp\tace.txt"
@@ -1 +1 @@
-a
+b
diff --git "a/caf\303\251 \"q\".txt" "b/caf\303\251 \"q\".txt"
index 1..2 100644
--- "a/caf\303\251 \"q\".txt"
+++ "b/caf\303\251 \"q\".txt"
@@ -1 +1 @@
-a
+b
diff --git "a/old\tname" "b/new\tname"
similarity index 100%
rename from "old\tname"
rename to "new\tname"
`
	fds := ParseDiff(text)
	if len(fds) != 3 {
		t.Fatalf("files = %d", len(fds))
	}
	for i, want := range []string{"sp\tace.txt", `café "q".txt`, "new\tname"} {
		if fds[i].Path != want {
			t.Errorf("file %d path = %q, want %q", i, fds[i].Path, want)
		}
	}
	if fds[2].OrigPath != "old\tname" {
		t.Errorf("orig = %q", fds[2].OrigPath)
	}
	// header only (no +++ line) still resolves
	hdr := ParseDiff("diff --git \"a/x\\ty\" \"b/x\\ty\"\nBinary files a and b differ\n")
	if len(hdr) != 1 || hdr[0].Path != "x\ty" {
		t.Fatalf("hdr = %+v", hdr)
	}
}
