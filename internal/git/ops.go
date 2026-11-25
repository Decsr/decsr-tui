package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ---- staging ----

// Stage adds paths to the index.
func (r *Repo) Stage(paths ...string) error {
	return r.write(append([]string{"add", "--"}, paths...)...)
}

// Unstage removes paths from the index (works before the first commit too).
func (r *Repo) Unstage(paths ...string) error {
	if r.hasCommits() {
		return r.write(append([]string{"restore", "--staged", "--"}, paths...)...)
	}
	return r.write(append([]string{"rm", "--cached", "-r", "-q", "--ignore-unmatch", "--"}, paths...)...)
}

// StageAll stages everything.
func (r *Repo) StageAll() error { return r.write("add", "-A") }

// UnstageAll clears the index.
func (r *Repo) UnstageAll() error { return r.Unstage(".") }

// Discard throws away all changes to path: tracked files return to HEAD,
// untracked files are deleted.
func (r *Repo) Discard(path string) error {
	if _, err := r.read("cat-file", "-e", "HEAD:"+path); err == nil {
		return r.write("restore", "--source=HEAD", "--staged", "--worktree", "--", path)
	}
	if out, _ := r.read("ls-files", "--cached", "-z", "--", path); out != "" {
		return r.write("rm", "-f", "-q", "--", path) // staged new file
	}
	return os.Remove(filepath.Join(r.Dir, filepath.FromSlash(path)))
}

// ---- commit ----

// Commit commits the index with msg. With amend and an empty msg the
// previous message is kept.
func (r *Repo) Commit(msg string, amend bool) error {
	args := []string{"commit"}
	stdin := ""
	if amend {
		args = append(args, "--amend")
	}
	if amend && msg == "" {
		args = append(args, "--no-edit")
	} else {
		args = append(args, "-F", "-")
		stdin = msg
	}
	_, err := r.exec(context.Background(), stdin, true, args...)
	return err
}

// LastCommitMessage returns the full message of HEAD ("" if none).
func (r *Repo) LastCommitMessage() string {
	out, err := r.read("log", "-1", "--format=%B")
	if err != nil {
		return ""
	}
	return strings.TrimRight(out, "\r\n\t ")
}

// ---- log ----

type Commit struct {
	Hash, Short, Author, Email string
	Date                       time.Time
	Subject, Refs              string
	Parents                    []string
}

// Log returns up to limit commits reachable from ref (HEAD when empty;
// limit <= 0 means all).
func (r *Repo) Log(ref string, limit int) ([]Commit, error) {
	if ref == "" {
		if !r.hasCommits() {
			return nil, nil
		}
		ref = "HEAD"
	}
	args := []string{"log", "--no-color", "--format=%H%x00%h%x00%an%x00%ae%x00%aI%x00%s%x00%D%x00%P%x1e"}
	if limit > 0 {
		args = append(args, "-n", strconv.Itoa(limit))
	}
	out, err := r.read(append(args, ref, "--")...)
	if err != nil {
		return nil, err
	}
	var commits []Commit
	for _, rec := range strings.Split(out, "\x1e") {
		rec = strings.TrimLeft(rec, "\r\n")
		f := strings.Split(rec, "\x00")
		if len(f) < 8 {
			continue
		}
		d, _ := time.Parse(time.RFC3339, f[4])
		commits = append(commits, Commit{
			Hash: f[0], Short: f[1], Author: f[2], Email: f[3], Date: d,
			Subject: f[5], Refs: f[6], Parents: strings.Fields(f[7]),
		})
	}
	return commits, nil
}

// GraphLog returns the lines of the all-branches commit graph.
func (r *Repo) GraphLog(limit int) ([]string, error) {
	if !r.hasCommits() {
		return nil, nil
	}
	args := []string{"log", "--graph", "--oneline", "--decorate", "--all", "--color=never"}
	if limit > 0 {
		args = append(args, "-n", strconv.Itoa(limit))
	}
	out, err := r.read(args...)
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimRight(out, "\r\n"), "\n"), nil
}

// ChangedFile is one file touched by a commit.
type ChangedFile struct {
	Status         byte
	Path, OrigPath string
}

// CommitFiles lists the files a commit changed.
func (r *Repo) CommitFiles(hash string) ([]ChangedFile, error) {
	out, err := r.read("show", "--name-status", "-z", "-M", "--format=", hash)
	if err != nil {
		return nil, err
	}
	var files []ChangedFile
	toks := strings.Split(out, "\x00")
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if t == "" {
			continue
		}
		cf := ChangedFile{Status: t[0]}
		if (t[0] == 'R' || t[0] == 'C') && i+2 < len(toks) {
			cf.OrigPath, cf.Path = toks[i+1], toks[i+2]
			i += 2
		} else if i+1 < len(toks) {
			cf.Path = toks[i+1]
			i++
		}
		files = append(files, cf)
	}
	return files, nil
}

// ---- branches ----

// Branch is a local or remote-tracking branch. Name is the short ref
// ("main", "origin/main"); Short drops the remote name for remote branches.
type Branch struct {
	Name, Upstream, Short string
	Head                  bool
	Ahead, Behind         int
	Remote                bool
	Date                  time.Time
}

// Branches lists local and remote branches, newest commit first.
func (r *Repo) Branches() ([]Branch, error) {
	const f = "%(HEAD)%00%(refname)%00%(refname:short)%00%(upstream:short)%00%(upstream:track,nobracket)%00%(committerdate:iso-strict)"
	out, err := r.read("for-each-ref", "--sort=-committerdate", "--format="+f, "refs/heads", "refs/remotes")
	if err != nil {
		return nil, err
	}
	var bs []Branch
	for _, line := range strings.Split(out, "\n") {
		p := strings.Split(strings.TrimRight(line, "\r"), "\x00")
		if len(p) < 6 || strings.HasSuffix(p[1], "/HEAD") {
			continue
		}
		b := Branch{Name: p[2], Short: p[2], Upstream: p[3], Head: p[0] == "*", Remote: strings.HasPrefix(p[1], "refs/remotes/")}
		if b.Remote {
			_, b.Short, _ = strings.Cut(b.Name, "/")
		}
		for _, part := range strings.Split(p[4], ",") {
			part = strings.TrimSpace(part)
			if v, ok := strings.CutPrefix(part, "ahead "); ok {
				b.Ahead, _ = strconv.Atoi(v)
			} else if v, ok := strings.CutPrefix(part, "behind "); ok {
				b.Behind, _ = strconv.Atoi(v)
			}
		}
		b.Date, _ = time.Parse(time.RFC3339, p[5])
		bs = append(bs, b)
	}
	return bs, nil
}

func (r *Repo) Checkout(ref string) error { return r.write("checkout", ref) }

// CreateBranch creates name at from (HEAD when empty), optionally switching to it.
func (r *Repo) CreateBranch(name, from string, checkout bool) error {
	args := []string{"branch", name}
	if checkout {
		args = []string{"checkout", "-b", name}
	}
	if from != "" {
		args = append(args, from)
	}
	return r.write(args...)
}

func (r *Repo) DeleteBranch(name string, force bool) error {
	flag := "-d"
	if force {
		flag = "-D"
	}
	return r.write("branch", flag, name)
}

func (r *Repo) RenameBranch(old, new string) error { return r.write("branch", "-m", old, new) }
func (r *Repo) Merge(ref string) error             { return r.write("merge", "--no-edit", ref) }
func (r *Repo) RebaseOnto(ref string) error        { return r.write("rebase", ref) }

func (r *Repo) SetUpstream(branch, upstream string) error {
	return r.write("branch", "--set-upstream-to="+upstream, branch)
}

// ---- stash ----

type Stash struct {
	Index        int
	Ref, Subject string
}

func (r *Repo) Stashes() ([]Stash, error) {
	out, err := r.read("stash", "list", "--format=%gd%x00%gs")
	if err != nil {
		return nil, err
	}
	var ss []Stash
	for _, line := range strings.Split(out, "\n") {
		ref, subj, ok := strings.Cut(strings.TrimRight(line, "\r"), "\x00")
		if !ok {
			continue
		}
		ss = append(ss, Stash{Index: len(ss), Ref: ref, Subject: subj})
	}
	return ss, nil
}

func (r *Repo) StashPush(msg string, includeUntracked bool) error {
	args := []string{"stash", "push"}
	if includeUntracked {
		args = append(args, "-u")
	}
	if msg != "" {
		args = append(args, "-m", msg)
	}
	return r.write(args...)
}

func stashRef(i int) string { return fmt.Sprintf("stash@{%d}", i) }

func (r *Repo) StashApply(i int) error { return r.write("stash", "apply", stashRef(i)) }
func (r *Repo) StashPop(i int) error   { return r.write("stash", "pop", stashRef(i)) }
func (r *Repo) StashDrop(i int) error  { return r.write("stash", "drop", stashRef(i)) }
func (r *Repo) StashDiff(i int) (string, error) {
	return r.read("stash", "show", "-p", "--no-color", stashRef(i))
}

// ---- remotes ----

type Remote struct{ Name, URL string }

func (r *Repo) Remotes() ([]Remote, error) {
	out, err := r.read("remote", "-v")
	if err != nil {
		return nil, err
	}
	var rs []Remote
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		rest, ok := strings.CutSuffix(line, " (fetch)")
		if !ok {
			continue
		}
		name, url, _ := strings.Cut(rest, "\t")
		rs = append(rs, Remote{Name: name, URL: url})
	}
	return rs, nil
}

func (r *Repo) Fetch(ctx context.Context) error {
	_, err := r.exec(ctx, "", true, "fetch", "--all", "--prune")
	return err
}

// Pull fast-forwards (or rebases when rebase is set).
func (r *Repo) Pull(ctx context.Context, rebase bool) error {
	mode := "--ff-only"
	if rebase {
		mode = "--rebase"
	}
	_, err := r.exec(ctx, "", true, "pull", mode)
	return err
}

// Push pushes the current branch. force uses --force-with-lease;
// setUpstream adds "-u origin <branch>" when the branch has no upstream.
func (r *Repo) Push(ctx context.Context, force, setUpstream bool) error {
	args := []string{"push"}
	if force {
		args = append(args, "--force-with-lease")
	}
	if setUpstream {
		if _, err := r.read("rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"); err != nil {
			branch, err := r.read("symbolic-ref", "--short", "HEAD")
			if err != nil {
				return err
			}
			branch = strings.TrimSpace(branch)
			remote, err := r.pushRemote(branch)
			if err != nil {
				return err
			}
			args = append(args, "-u", remote, branch)
		}
	}
	_, err := r.exec(ctx, "", true, args...)
	return err
}

// pushRemote picks the remote for branch: its configured remote, else the
// only remote, else "origin" if it exists.
func (r *Repo) pushRemote(branch string) (string, error) {
	if out, err := r.read("config", "--get", "branch."+branch+".remote"); err == nil && strings.TrimSpace(out) != "" {
		return strings.TrimSpace(out), nil
	}
	out, err := r.read("remote")
	if err != nil {
		return "", err
	}
	names := strings.Fields(out)
	switch {
	case len(names) == 1:
		return names[0], nil
	case len(names) == 0:
		return "", fmt.Errorf("git push: no remote configured")
	}
	for _, n := range names {
		if n == "origin" {
			return n, nil
		}
	}
	return "", fmt.Errorf("git push: several remotes and none configured for %s", branch)
}

// DeleteRemoteBranch deletes branch on remote.
func (r *Repo) DeleteRemoteBranch(ctx context.Context, remote, branch string) error {
	_, err := r.exec(ctx, "", true, "push", remote, "--delete", branch)
	return err
}

// DiscardUnstaged throws away worktree changes to path and keeps what is
// staged. Untracked files are deleted.
func (r *Repo) DiscardUnstaged(path string) error {
	if out, _ := r.read("ls-files", "--cached", "-z", "--", path); out == "" {
		return os.Remove(filepath.Join(r.Dir, filepath.FromSlash(path)))
	}
	return r.write("restore", "--worktree", "--", path)
}

// CommitBranches lists the branches (local and remote) that contain hash.
func (r *Repo) CommitBranches(hash string) ([]string, error) {
	out, err := r.read("branch", "-a", "--contains", hash, "--format=%(refname:short)")
	if err != nil {
		return nil, err
	}
	var bs []string
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimRight(l, "\r")
		if l == "" || strings.HasSuffix(l, "/HEAD") || strings.HasPrefix(l, "(") {
			continue
		}
		bs = append(bs, l)
	}
	return bs, nil
}

// ---- repo state ----

type RepoState int

const (
	Normal RepoState = iota
	Merging
	Rebasing
	CherryPicking
	Reverting
)

func (s RepoState) String() string {
	return [...]string{"normal", "merging", "rebasing", "cherry-picking", "reverting"}[s]
}

// State detects an in-progress operation from files in the git dir.
func (r *Repo) State() RepoState {
	names := []string{"rebase-merge", "rebase-apply", "MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD"}
	args := []string{"rev-parse"}
	for _, n := range names {
		args = append(args, "--git-path", n)
	}
	out, err := r.read(args...)
	if err != nil {
		return Normal
	}
	paths := strings.Split(strings.TrimRight(out, "\r\n"), "\n")
	states := []RepoState{Rebasing, Rebasing, Merging, CherryPicking, Reverting}
	for i, p := range paths {
		if i >= len(states) {
			break
		}
		p = strings.TrimRight(p, "\r")
		if !filepath.IsAbs(p) {
			p = filepath.Join(r.Dir, p)
		}
		if _, err := os.Stat(p); err == nil {
			return states[i]
		}
	}
	return Normal
}

// Abort cancels the in-progress operation.
func (r *Repo) Abort() error { return r.stateCmd("--abort") }

// Continue resumes the in-progress operation after conflicts are resolved.
func (r *Repo) Continue() error { return r.stateCmd("--continue") }

func (r *Repo) stateCmd(flag string) error {
	switch r.State() {
	case Merging:
		if flag == "--continue" {
			return r.write("commit", "--no-edit")
		}
		return r.write("merge", flag)
	case Rebasing:
		return r.write("rebase", flag)
	case CherryPicking:
		return r.write("cherry-pick", flag)
	case Reverting:
		return r.write("revert", flag)
	}
	return fmt.Errorf("no merge, rebase, cherry-pick or revert in progress")
}

// ---- reflog / reset ----

type ReflogEntry struct{ Hash, Selector, Subject string }

func (r *Repo) Reflog(limit int) ([]ReflogEntry, error) {
	if !r.hasCommits() {
		return nil, nil
	}
	args := []string{"reflog", "--format=%H%x00%gd%x00%gs"}
	if limit > 0 {
		args = append(args, "-n", strconv.Itoa(limit))
	}
	out, err := r.read(args...)
	if err != nil {
		return nil, err
	}
	var es []ReflogEntry
	for _, line := range strings.Split(out, "\n") {
		p := strings.Split(strings.TrimRight(line, "\r"), "\x00")
		if len(p) == 3 {
			es = append(es, ReflogEntry{p[0], p[1], p[2]})
		}
	}
	return es, nil
}

// Reset moves HEAD to ref; mode is soft, mixed or hard.
func (r *Repo) Reset(ref, mode string) error {
	switch mode {
	case "soft", "mixed", "hard":
		return r.write("reset", "--"+mode, ref)
	}
	return fmt.Errorf("invalid reset mode %q", mode)
}
