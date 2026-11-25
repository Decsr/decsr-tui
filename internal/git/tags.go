package git

import (
	"context"
	"strings"
	"time"
)

// Tag is a lightweight or annotated tag. Hash is the tagged commit; Subject
// is the tag message's first line (annotated) or the commit subject.
type Tag struct {
	Name, Hash, Subject string
	Annotated           bool
	Date                time.Time
}

// Tags lists tags, newest first.
func (r *Repo) Tags() ([]Tag, error) {
	const f = "%(refname:short)%00%(objecttype)%00%(*objectname)%00%(objectname)%00%(creatordate:iso-strict)%00%(contents:subject)"
	out, err := r.read("for-each-ref", "--sort=-creatordate", "--format="+f, "refs/tags")
	if err != nil {
		return nil, err
	}
	var ts []Tag
	for _, line := range strings.Split(out, "\n") {
		p := strings.Split(strings.TrimRight(line, "\r"), "\x00")
		if len(p) < 6 {
			continue
		}
		t := Tag{Name: p[0], Hash: p[3], Subject: p[5], Annotated: p[1] == "tag"}
		if t.Annotated {
			t.Hash = p[2]
		}
		t.Date, _ = time.Parse(time.RFC3339, p[4])
		ts = append(ts, t)
	}
	return ts, nil
}

// CreateTag tags ref (HEAD when empty); a non-empty message makes it annotated.
func (r *Repo) CreateTag(name, ref, message string) error {
	args := []string{"tag"}
	stdin := ""
	if message != "" {
		args = append(args, "-a", "-F", "-")
		stdin = message
	}
	args = append(args, name)
	if ref != "" {
		args = append(args, ref)
	}
	_, err := r.exec(context.Background(), stdin, true, args...)
	return err
}

func (r *Repo) DeleteTag(name string) error { return r.write("tag", "-d", name) }

func (r *Repo) PushTag(ctx context.Context, remote, name string) error {
	_, err := r.exec(ctx, "", true, "push", remote, "refs/tags/"+name)
	return err
}

func (r *Repo) DeleteRemoteTag(ctx context.Context, remote, name string) error {
	_, err := r.exec(ctx, "", true, "push", remote, "--delete", "refs/tags/"+name)
	return err
}
