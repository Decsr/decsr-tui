package git

import (
	"strconv"
	"strings"
)

// File is one entry of git status. Staged and Unstaged hold the porcelain
// status letters, ' ' meaning no change ('?' for both when untracked).
type File struct {
	Path, OrigPath        string
	Staged, Unstaged      byte
	Untracked, Conflicted bool
}

// Head describes the current branch.
type Head struct {
	Branch, Upstream string
	Ahead, Behind    int
	Detached         bool
}

// Status returns branch info and changed files.
func (r *Repo) Status() (Head, []File, error) {
	var h Head
	out, err := r.read("status", "--porcelain=v2", "-z", "--branch", "--untracked-files=all")
	if err != nil {
		return h, nil, err
	}
	var files []File
	recs := strings.Split(out, "\x00")
	for i := 0; i < len(recs); i++ {
		rec := recs[i]
		if rec == "" {
			continue
		}
		switch {
		case strings.HasPrefix(rec, "# branch.head "):
			h.Branch = strings.TrimPrefix(rec, "# branch.head ")
			if h.Branch == "(detached)" {
				h.Branch, h.Detached = "", true
			}
		case strings.HasPrefix(rec, "# branch.upstream "):
			h.Upstream = strings.TrimPrefix(rec, "# branch.upstream ")
		case strings.HasPrefix(rec, "# branch.ab "):
			f := strings.Fields(rec)
			if len(f) >= 4 {
				h.Ahead, _ = strconv.Atoi(strings.TrimPrefix(f[2], "+"))
				h.Behind, _ = strconv.Atoi(strings.TrimPrefix(f[3], "-"))
			}
		case strings.HasPrefix(rec, "1 "):
			p := strings.SplitN(rec, " ", 9)
			if len(p) == 9 {
				files = append(files, newFile(p[1], p[8], ""))
			}
		case strings.HasPrefix(rec, "2 "):
			p := strings.SplitN(rec, " ", 10)
			if len(p) == 10 {
				orig := ""
				if i+1 < len(recs) {
					i++
					orig = recs[i]
				}
				files = append(files, newFile(p[1], p[9], orig))
			}
		case strings.HasPrefix(rec, "u "):
			p := strings.SplitN(rec, " ", 11)
			if len(p) == 11 {
				f := newFile(p[1], p[10], "")
				f.Conflicted = true
				files = append(files, f)
			}
		case strings.HasPrefix(rec, "? "):
			files = append(files, File{Path: rec[2:], Staged: '?', Unstaged: '?', Untracked: true})
		}
	}
	return h, files, nil
}

func newFile(xy, path, orig string) File {
	dot := func(b byte) byte {
		if b == '.' {
			return ' '
		}
		return b
	}
	return File{Path: path, OrigPath: orig, Staged: dot(xy[0]), Unstaged: dot(xy[1])}
}
