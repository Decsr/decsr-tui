package git

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
)

// TodoItem is one line of an interactive rebase todo. Action is one of
// pick, reword, edit, squash, fixup, drop.
type TodoItem struct{ Action, Hash, Subject string }

// RebaseTodo lists the commits from base (exclusive; "--root" for all) to
// HEAD, oldest first, as pick items. Merge commits are left out, as a plain
// interactive rebase drops them.
func (r *Repo) RebaseTodo(base string) ([]TodoItem, error) {
	rng := "HEAD"
	if base != "--root" {
		rng = base + "..HEAD"
	}
	out, err := r.read("log", "--reverse", "--topo-order", "--no-merges", "--format=%H%x00%s", rng, "--")
	if err != nil {
		return nil, err
	}
	var todo []TodoItem
	for _, line := range strings.Split(out, "\n") {
		h, s, ok := strings.Cut(strings.TrimRight(line, "\r"), "\x00")
		if ok {
			todo = append(todo, TodoItem{"pick", h, s})
		}
	}
	return todo, nil
}

// RebaseInteractive runs `git rebase -i base` with todo as the sequence, no
// editor involved. On conflict (or an edit stop) it returns the git error
// and the repo stays in the REBASING state.
//
// The todo is written to a temp file and GIT_SEQUENCE_EDITOR is set to
// `cp '<tmp>'`: git runs editor commands through its bundled sh, which
// understands forward-slash Windows paths.
func (r *Repo) RebaseInteractive(base string, todo []TodoItem) error {
	return r.rebase(base, todo, "")
}

// copyCmd builds a shell editor command that overwrites git's file with src.
func copyCmd(src string) string {
	if runtime.GOOS == "windows" {
		src = strings.ReplaceAll(src, `\`, "/") // git's sh reads backslashes as escapes
	}
	return "cp '" + strings.ReplaceAll(src, "'", `'\''`) + "'"
}

func tempFile(content string) (string, error) {
	f, err := os.CreateTemp("", "decsr-tui-*")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// rebase runs the interactive rebase. A non-empty msg is used as the
// message of every reword item (GIT_EDITOR copies it over the message file).
func (r *Repo) rebase(base string, todo []TodoItem, msg string) error {
	var b strings.Builder
	for _, it := range todo {
		fmt.Fprintf(&b, "%s %s %s\n", it.Action, it.Hash, it.Subject)
	}
	todoFile, err := tempFile(b.String())
	if err != nil {
		return err
	}
	defer os.Remove(todoFile)
	env := []string{"GIT_SEQUENCE_EDITOR=" + copyCmd(todoFile), "GIT_EDITOR=true"}
	if msg != "" {
		msgFile, err := tempFile(msg)
		if err != nil {
			return err
		}
		defer os.Remove(msgFile)
		env[1] = "GIT_EDITOR=" + copyCmd(msgFile)
	}
	args := []string{"rebase", "-i"}
	if base == "--root" {
		args = append(args, "--root")
	} else {
		args = append(args, base)
	}
	_, err = r.execEnv(context.Background(), "", true, env, args...)
	return err
}

// commitHash resolves ref to a full commit hash.
func (r *Repo) commitHash(ref string) (string, error) {
	out, err := r.read("rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// parentOf returns the first parent of hash, or "" for a root commit.
func (r *Repo) parentOf(hash string) string {
	out, err := r.read("rev-parse", "--verify", "-q", hash+"^")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// rewrite rebases from the parent of hash (from the grandparent when back
// is set, so the todo also holds the parent) and lets edit change the todo
// item i that belongs to hash.
func (r *Repo) rewrite(hash string, back bool, msg string, edit func(todo []TodoItem, i int) error) error {
	full, err := r.commitHash(hash)
	if err != nil {
		return err
	}
	base := r.parentOf(full)
	if back {
		if base == "" {
			return fmt.Errorf("rebase: commit %.7s has no parent", full)
		}
		base = r.parentOf(base)
	}
	if base == "" {
		base = "--root"
	}
	todo, err := r.RebaseTodo(base)
	if err != nil {
		return err
	}
	i := -1
	for n, it := range todo {
		if it.Hash == full {
			i = n
		}
	}
	if i < 0 {
		return fmt.Errorf("rebase: commit %.7s is not on the current branch history", full)
	}
	if err := edit(todo, i); err != nil {
		return err
	}
	return r.rebase(base, todo, msg)
}

func setAction(action string, needParent bool) func([]TodoItem, int) error {
	return func(todo []TodoItem, i int) error {
		if needParent && i == 0 {
			return fmt.Errorf("rebase: no previous commit to %s into", action)
		}
		todo[i].Action = action
		return nil
	}
}

// SquashInto squashes the commit into its parent, keeping both messages.
func (r *Repo) SquashInto(hash string) error {
	return r.rewrite(hash, true, "", setAction("squash", true))
}

// Fixup squashes the commit into its parent and discards its message.
func (r *Repo) Fixup(hash string) error {
	return r.rewrite(hash, true, "", setAction("fixup", true))
}

// Drop removes the commit from history.
func (r *Repo) Drop(hash string) error { return r.rewrite(hash, false, "", setAction("drop", false)) }

// Reword changes the commit message.
func (r *Repo) Reword(hash, msg string) error {
	if strings.TrimSpace(msg) == "" {
		return fmt.Errorf("rebase: empty commit message")
	}
	return r.rewrite(hash, false, msg, setAction("reword", false))
}

// EditCommit stops the rebase at the commit so it can be amended; the repo
// is then in the REBASING state until Continue or Abort.
func (r *Repo) EditCommit(hash string) error {
	return r.rewrite(hash, false, "", setAction("edit", false))
}

// MoveUp moves the commit one place up the log (towards HEAD), swapping it
// with its child.
func (r *Repo) MoveUp(hash string) error {
	return r.rewrite(hash, false, "", func(todo []TodoItem, i int) error {
		if i+1 >= len(todo) {
			return fmt.Errorf("rebase: commit is already the newest")
		}
		todo[i], todo[i+1] = todo[i+1], todo[i]
		return nil
	})
}

// MoveDown moves the commit one place down the log (away from HEAD),
// swapping it with its parent.
func (r *Repo) MoveDown(hash string) error {
	return r.rewrite(hash, true, "", func(todo []TodoItem, i int) error {
		if i == 0 {
			return fmt.Errorf("rebase: no previous commit to swap with")
		}
		todo[i-1], todo[i] = todo[i], todo[i-1]
		return nil
	})
}

// FixupInto commits the staged changes as a fixup of target and folds it
// into target with an autosquash rebase.
func (r *Repo) FixupInto(target string) error {
	full, err := r.commitHash(target)
	if err != nil {
		return err
	}
	if _, err := r.read("diff", "--cached", "--quiet"); err == nil {
		return fmt.Errorf("fixup: nothing staged")
	}
	if err := r.write("commit", "--fixup="+full); err != nil {
		return err
	}
	args := []string{"rebase", "-i", "--autosquash"}
	if base := r.parentOf(full); base == "" {
		args = append(args, "--root")
	} else {
		args = append(args, base)
	}
	_, err = r.execEnv(context.Background(), "", true, []string{"GIT_SEQUENCE_EDITOR=true"}, args...)
	return err
}

// ---- cherry-pick / revert ----

// CherryPick applies the commits in the order given.
func (r *Repo) CherryPick(hashes ...string) error {
	if len(hashes) == 0 {
		return fmt.Errorf("cherry-pick: no commits given")
	}
	return r.write(append([]string{"cherry-pick"}, hashes...)...)
}

// Revert creates a commit undoing hash.
func (r *Repo) Revert(hash string) error { return r.write("revert", "--no-edit", hash) }

// RevertMerge reverts a merge commit relative to its parent number (1-based).
func (r *Repo) RevertMerge(hash string, parent int) error {
	return r.write("revert", "--no-edit", "-m", fmt.Sprint(parent), hash)
}
