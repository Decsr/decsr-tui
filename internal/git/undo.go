package git

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Undo describes a reflog action that can be undone, or a redo step.
// Kind "reset" means `reset --hard Hash`; kind "checkout" means checking out
// Branch. Desc is the reflog subject of the action.
type Undo struct {
	Hash, Desc string
	Kind       string // reset | checkout
	Branch     string
}

// undoState is kept in .git/decsr-tui-undo. Remaining counts the oldest
// reflog entries not yet undone (counted from the bottom, so new entries on
// top do not shift it). Len is the reflog length right after our last
// operation; if the reflog has a different length something else happened
// and the state is discarded.
type undoState struct {
	Len, Remaining int
	Redo           []undoStep
}

type undoStep struct {
	Undo
	Remaining int // Remaining before the undo this step reverses
}

func (r *Repo) undoFile() (string, error) {
	out, err := r.read("rev-parse", "--git-path", "decsr-tui-undo")
	if err != nil {
		return "", err
	}
	p := strings.TrimSpace(out)
	if !filepath.IsAbs(p) {
		p = filepath.Join(r.Dir, p)
	}
	return p, nil
}

// loadUndo returns the saved state, or a fresh one when it is stale.
func (r *Repo) loadUndo(reflogLen int) undoState {
	fresh := undoState{Len: reflogLen, Remaining: reflogLen}
	p, err := r.undoFile()
	if err != nil {
		return fresh
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return fresh
	}
	var st undoState
	if json.Unmarshal(data, &st) != nil || st.Len != reflogLen || st.Remaining > reflogLen {
		return fresh
	}
	return st
}

func (r *Repo) saveUndo(st undoState) error {
	p, err := r.undoFile()
	if err != nil {
		return err
	}
	data, _ := json.Marshal(st)
	return os.WriteFile(p, data, 0o644)
}

// classify decides whether reflog entry i (newest first) is an undoable
// action. It returns the undo, and the index just past the entries it spans.
func classify(es []ReflogEntry, i int) (u Undo, end int, ok bool) {
	s := es[i].Subject
	switch {
	case strings.HasPrefix(s, "checkout: moving from "):
		from, _, _ := strings.Cut(strings.TrimPrefix(s, "checkout: moving from "), " to ")
		return Undo{Kind: "checkout", Branch: from, Desc: s}, i + 1, true
	case strings.Contains(s, "(finish)"):
		j := i + 1
		for j < len(es) && !strings.Contains(es[j].Subject, "(start)") {
			j++
		}
		if j+1 >= len(es) {
			return Undo{}, 0, false
		}
		return Undo{Kind: "reset", Hash: es[j+1].Hash, Desc: s}, j + 1, true
	}
	for _, p := range []string{"commit", "reset:", "merge ", "cherry-pick", "revert", "pull"} {
		if strings.HasPrefix(s, p) {
			if i+1 >= len(es) {
				return Undo{}, 0, false
			}
			return Undo{Kind: "reset", Hash: es[i+1].Hash, Desc: s}, i + 1, true
		}
	}
	return Undo{}, 0, false
}

type undoPlan struct {
	st  undoState
	u   Undo
	end int // reflog index just past the undone entries
	len int
	ok  bool
}

func (r *Repo) planUndo() (undoPlan, error) {
	es, err := r.Reflog(0)
	if err != nil {
		return undoPlan{}, err
	}
	p := undoPlan{st: r.loadUndo(len(es)), len: len(es)}
	for i := len(es) - p.st.Remaining; i < len(es); i++ {
		if u, end, ok := classify(es, i); ok {
			p.u, p.end, p.ok = u, end, true
			break
		}
	}
	return p, nil
}

// UndoTarget finds the next action Undo would revert.
func (r *Repo) UndoTarget() (Undo, bool, error) {
	p, err := r.planUndo()
	return p.u, p.ok, err
}

// RedoTarget returns the step Redo would perform.
func (r *Repo) RedoTarget() (Undo, bool, error) {
	es, err := r.Reflog(0)
	if err != nil {
		return Undo{}, false, err
	}
	st := r.loadUndo(len(es))
	if len(st.Redo) == 0 {
		return Undo{}, false, nil
	}
	return st.Redo[len(st.Redo)-1].Undo, true, nil
}

// ready refuses to move HEAD while an operation is running or the tracked
// files have uncommitted changes.
func (r *Repo) ready() error {
	if s := r.State(); s != Normal {
		return fmt.Errorf("cannot undo or redo while %s", s)
	}
	out, err := r.read("status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) != "" {
		return fmt.Errorf("working tree has uncommitted changes; commit or stash them first")
	}
	return nil
}

func (r *Repo) apply(u Undo) error {
	if u.Kind == "checkout" {
		return r.Checkout(u.Branch)
	}
	return r.Reset(u.Hash, "hard")
}

func (r *Repo) afterUndo(st undoState, remaining int) error {
	es, err := r.Reflog(0)
	if err != nil {
		return err
	}
	st.Len, st.Remaining = len(es), remaining
	return r.saveUndo(st)
}

// Undo reverts the most recent undoable action (commit, amend, reset,
// rebase, merge, cherry-pick, pull, checkout). Repeated calls walk further
// back. It refuses when the working tree is dirty.
func (r *Repo) Undo() error {
	if err := r.ready(); err != nil {
		return err
	}
	p, err := r.planUndo()
	if err != nil {
		return err
	}
	if !p.ok {
		return fmt.Errorf("nothing to undo")
	}
	step := undoStep{Undo: Undo{Kind: p.u.Kind, Desc: p.u.Desc}, Remaining: p.st.Remaining}
	if p.u.Kind == "reset" {
		head, err := r.commitHash("HEAD")
		if err != nil {
			return err
		}
		step.Hash = head
	} else {
		// the checkout entry is the last one the plan spans: "moving from A to B"
		es, err := r.Reflog(0)
		if err != nil {
			return err
		}
		_, step.Branch, _ = strings.Cut(strings.TrimPrefix(es[p.end-1].Subject, "checkout: moving from "), " to ")
	}
	if err := r.apply(p.u); err != nil {
		return err
	}
	p.st.Redo = append(p.st.Redo, step)
	return r.afterUndo(p.st, p.len-p.end)
}

// Redo re-applies the last undone action.
func (r *Repo) Redo() error {
	if err := r.ready(); err != nil {
		return err
	}
	es, err := r.Reflog(0)
	if err != nil {
		return err
	}
	st := r.loadUndo(len(es))
	if len(st.Redo) == 0 {
		return fmt.Errorf("nothing to redo")
	}
	step := st.Redo[len(st.Redo)-1]
	if err := r.apply(step.Undo); err != nil {
		return err
	}
	st.Redo = st.Redo[:len(st.Redo)-1]
	return r.afterUndo(st, step.Remaining)
}
