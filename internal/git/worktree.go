package git

import (
	"path/filepath"
	"strings"
)

// Worktree is one entry of `git worktree list`. Branch is the short name
// ("" when detached or bare); Main marks the main working tree.
type Worktree struct {
	Path, Branch, Head                     string
	Main, Bare, Detached, Locked, Prunable bool
}

func (r *Repo) Worktrees() ([]Worktree, error) {
	out, err := r.read("worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var ws []Worktree
	for _, line := range strings.Split(out, "\n") {
		key, val, _ := strings.Cut(strings.TrimRight(line, "\r"), " ")
		if key == "worktree" {
			ws = append(ws, Worktree{Path: filepath.FromSlash(val), Main: len(ws) == 0})
			continue
		}
		if len(ws) == 0 {
			continue
		}
		cur := &ws[len(ws)-1]
		switch key {
		case "HEAD":
			cur.Head = val
		case "branch":
			cur.Branch = strings.TrimPrefix(val, "refs/heads/")
		case "bare":
			cur.Bare = true
		case "detached":
			cur.Detached = true
		case "locked":
			cur.Locked = true
		case "prunable":
			cur.Prunable = true
		}
	}
	return ws, nil
}

// AddWorktree adds a worktree at path. With newBranch, branch is created;
// otherwise it is checked out (an empty branch lets git pick).
func (r *Repo) AddWorktree(path, branch string, newBranch bool) error {
	args := []string{"worktree", "add"}
	switch {
	case newBranch:
		args = append(args, "-b", branch, path)
	case branch != "":
		args = append(args, path, branch)
	default:
		args = append(args, path)
	}
	return r.write(args...)
}

func (r *Repo) RemoveWorktree(path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	return r.write(append(args, path)...)
}

func (r *Repo) PruneWorktrees() error { return r.write("worktree", "prune") }
