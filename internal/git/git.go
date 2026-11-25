// Package git is a thin layer over the git binary, used by the TUI.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Repo is a git working tree. LogFunc, when set, receives every mutating
// command line (for the TUI's command log).
type Repo struct {
	Dir     string
	LogFunc func(cmd string)
}

// Open resolves the top-level of the repository containing dir.
func Open(dir string) (*Repo, error) {
	r := &Repo{Dir: dir}
	out, err := r.read("rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("not a git repository: %s: %w", dir, err)
	}
	r.Dir = filepath.Clean(strings.TrimSpace(out))
	return r, nil
}

func (r *Repo) exec(ctx context.Context, stdin string, mutating bool, args ...string) (string, error) {
	return r.execEnv(ctx, stdin, mutating, nil, args...)
}

// execEnv is exec with extra environment entries (KEY=VALUE) that override the defaults.
func (r *Repo) execEnv(ctx context.Context, stdin string, mutating bool, extra []string, args ...string) (string, error) {
	full := append([]string{"-C", r.Dir, "-c", "core.quotepath=false"}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	env := append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C", "GIT_EDITOR=true", "GIT_MERGE_AUTOEDIT=no")
	if !mutating {
		env = append(env, "GIT_OPTIONAL_LOCKS=0")
	}
	cmd.Env = append(env, extra...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if mutating && r.LogFunc != nil {
		r.LogFunc("git " + strings.Join(args, " "))
	}
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if ctx.Err() != nil {
			return stdout.String(), ctx.Err()
		}
		var ee *exec.ExitError
		if msg == "" && errors.As(err, &ee) {
			msg = ee.Error()
		} else if msg == "" {
			msg = err.Error()
		}
		return stdout.String(), fmt.Errorf("git %s: %s", args[0], msg)
	}
	return stdout.String(), nil
}

// read runs a read-only command.
func (r *Repo) read(args ...string) (string, error) {
	return r.exec(context.Background(), "", false, args...)
}

// write runs a mutating command.
func (r *Repo) write(args ...string) error {
	_, err := r.exec(context.Background(), "", true, args...)
	return err
}

func (r *Repo) hasCommits() bool {
	_, err := r.read("rev-parse", "--verify", "-q", "HEAD")
	return err == nil
}
