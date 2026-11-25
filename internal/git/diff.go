package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Line is one line of a hunk. Kind is ' ', '+', '-' or backslash (the
// "No newline at end of file" marker). Text excludes the kind character
// and keeps any trailing CR.
type Line struct {
	Kind byte
	Text string
}

// Hunk is one @@ block of a unified diff.
type Hunk struct {
	Header                                 string
	OldStart, OldLines, NewStart, NewLines int
	Lines                                  []Line
}

// FileDiff is one file of a unified diff. Header holds the lines before
// the first hunk (diff --git, index, ---, +++ ...).
type FileDiff struct {
	Path, OrigPath string
	Header         []string
	Hunks          []Hunk
	Binary         bool
}

// DiffFile returns the diff of one file: staged (index vs HEAD) or
// unstaged (worktree vs index). Untracked files get a synthesized
// "new file" diff.
func (r *Repo) DiffFile(path string, staged bool) (string, error) {
	if staged {
		return r.read("diff", "--cached", "--no-color", "--", path)
	}
	if out, err := r.read("ls-files", "--others", "--exclude-standard", "-z", "--", path); err == nil && out != "" {
		return r.newFileDiff(path)
	}
	return r.read("diff", "--no-color", "--", path)
}

func (r *Repo) newFileDiff(path string) (string, error) {
	data, err := os.ReadFile(filepath.Join(r.Dir, filepath.FromSlash(path)))
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "diff --git a/%s b/%s\nnew file mode 100644\n", path, path)
	if strings.IndexByte(string(data), 0) >= 0 {
		fmt.Fprintf(&b, "Binary files /dev/null and b/%s differ\n", path)
		return b.String(), nil
	}
	if len(data) == 0 {
		return b.String(), nil
	}
	text := string(data)
	noEOL := !strings.HasSuffix(text, "\n")
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	fmt.Fprintf(&b, "--- /dev/null\n+++ b/%s\n@@ -0,0 +1,%d @@\n", path, len(lines))
	for _, l := range lines {
		b.WriteString("+" + l + "\n")
	}
	if noEOL {
		b.WriteString("\\ No newline at end of file\n")
	}
	return b.String(), nil
}

// DiffCommit returns the diff a commit introduced, optionally for one path.
func (r *Repo) DiffCommit(hash, path string) (string, error) {
	args := []string{"show", "--format=", "--no-color", hash}
	if path != "" {
		args = append(args, "--", path)
	}
	return r.read(args...)
}

// DiffRange returns the diff between two refs.
func (r *Repo) DiffRange(from, to string) (string, error) {
	return r.read("diff", "--no-color", from, to)
}

// ParseDiff splits unified diff text into files and hunks.
func ParseDiff(text string) []FileDiff {
	lines := strings.Split(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	var out []FileDiff
	var fd *FileDiff
	var h *Hunk
	flush := func() {
		if h != nil && fd != nil {
			fd.Hunks = append(fd.Hunks, *h)
		}
		h = nil
	}
	for _, l := range lines {
		switch {
		case strings.HasPrefix(l, "diff --git "):
			flush()
			if fd != nil {
				out = append(out, *fd)
			}
			fd = &FileDiff{Header: []string{l}}
			fd.Path = headerPath(strings.TrimPrefix(l, "diff --git "))
		case fd == nil:
			// text before any file header
		case strings.HasPrefix(l, "@@ "):
			flush()
			nh := parseHunkHeader(l)
			h = &nh
		case h != nil:
			if l == "" {
				l = " "
			}
			h.Lines = append(h.Lines, Line{Kind: l[0], Text: l[1:]})
		default:
			fd.Header = append(fd.Header, l)
			switch {
			case strings.HasPrefix(l, "Binary files") || strings.HasPrefix(l, "GIT binary patch"):
				fd.Binary = true
			case strings.HasPrefix(l, "rename from "):
				fd.OrigPath = unquoteC(strings.TrimPrefix(l, "rename from "))
			case strings.HasPrefix(l, "rename to "):
				fd.Path = unquoteC(strings.TrimPrefix(l, "rename to "))
			case strings.HasPrefix(l, "+++ b/"):
				fd.Path = strings.TrimSuffix(strings.TrimPrefix(l, "+++ b/"), "\t")
			case strings.HasPrefix(l, `+++ "b/`):
				fd.Path = strings.TrimPrefix(unquoteC(strings.TrimPrefix(l, "+++ ")), "b/")
			}
		}
	}
	flush()
	if fd != nil {
		out = append(out, *fd)
	}
	return out
}

// headerPath extracts the path from "a/P b/P" (equal paths only).
func headerPath(rest string) string {
	if strings.HasPrefix(rest, `"`) || strings.Contains(rest, ` "b/`) {
		// quoted form: "a/P" "b/P" (or one side unquoted)
		var second string
		if strings.HasPrefix(rest, `"`) {
			second = strings.TrimLeft(rest[quotedEnd(rest):], " ")
		} else {
			_, second, _ = strings.Cut(rest, ` "b/`)
			second = `"b/` + second
		}
		return strings.TrimPrefix(unquoteC(second), "b/")
	}
	if n := (len(rest) - 5) / 2; n > 0 && len(rest) == 2*n+5 && rest[2:2+n] == rest[len(rest)-n:] {
		return rest[2 : 2+n]
	}
	if _, after, ok := strings.Cut(rest, " b/"); ok {
		return after
	}
	return rest
}

func parseHunkHeader(l string) Hunk {
	h := Hunk{Header: l, OldLines: 1, NewLines: 1}
	body := strings.TrimPrefix(l, "@@ ")
	if i := strings.Index(body, " @@"); i >= 0 {
		body = body[:i]
	}
	f := strings.Fields(body)
	if len(f) == 2 {
		h.OldStart, h.OldLines = parseRange(f[0][1:], 1)
		h.NewStart, h.NewLines = parseRange(f[1][1:], 1)
	}
	return h
}

func parseRange(s string, def int) (start, n int) {
	a, b, ok := strings.Cut(s, ",")
	start, _ = strconv.Atoi(a)
	n = def
	if ok {
		n, _ = strconv.Atoi(b)
	}
	return
}

// BuildPatch builds a patch for one hunk of fd. selected holds line indexes
// within the hunk to include (nil = the whole hunk). For staging pass
// reverse=false on an unstaged diff; for unstaging or discarding pass
// reverse=true on a staged or unstaged diff and apply the result with
// reverse. It returns "" when the selection contains no change.
func BuildPatch(fd FileDiff, hunkIdx int, selected map[int]bool, reverse bool) string {
	if hunkIdx < 0 || hunkIdx >= len(fd.Hunks) {
		return ""
	}
	hk := fd.Hunks[hunkIdx]
	var body []string
	var oldN, newN, changes int
	partial := false
	prevKept := true
	for i, ln := range hk.Lines {
		sel := selected == nil || selected[i]
		kind := ln.Kind
		switch kind {
		case '+':
			if !sel {
				partial = true
				if reverse {
					kind = ' '
				} else {
					prevKept = false
					continue
				}
			}
		case '-':
			if !sel {
				partial = true
				if !reverse {
					kind = ' '
				} else {
					prevKept = false
					continue
				}
			}
		case '\\':
			if !prevKept {
				continue
			}
		}
		prevKept = true
		if kind != ' ' && kind != '\\' {
			changes++
		}
		if kind == ' ' || kind == '-' {
			oldN++
		}
		if kind == ' ' || kind == '+' {
			newN++
		}
		body = append(body, string(kind)+ln.Text)
	}
	if changes == 0 {
		return ""
	}
	start := hk.OldStart
	if reverse {
		start = hk.NewStart
	}
	header := fd.Header
	isNew := hasPrefixLine(header, "new file mode")
	isDel := hasPrefixLine(header, "deleted file mode")
	if partial && ((reverse && isNew) || (!reverse && isDel)) {
		header = make([]string, 0, len(fd.Header))
		for _, l := range fd.Header {
			switch {
			case strings.HasPrefix(l, "new file mode"), strings.HasPrefix(l, "deleted file mode"):
				continue
			case l == "--- /dev/null":
				l = "--- a/" + fd.Path
			case l == "+++ /dev/null":
				l = "+++ b/" + fd.Path
			}
			header = append(header, l)
		}
		if start == 0 {
			start = 1
		}
	}
	var b strings.Builder
	for _, l := range header {
		b.WriteString(l + "\n")
	}
	fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@\n", start, oldN, start, newN)
	for _, l := range body {
		b.WriteString(l + "\n")
	}
	return b.String()
}

func hasPrefixLine(lines []string, prefix string) bool {
	for _, l := range lines {
		if strings.HasPrefix(l, prefix) {
			return true
		}
	}
	return false
}

// ApplyPatch applies a patch to the index (cached) or worktree.
func (r *Repo) ApplyPatch(patch string, cached, reverse bool) error {
	args := []string{"apply", "--recount", "--whitespace=nowarn"}
	if cached {
		args = append(args, "--cached")
	}
	if reverse {
		args = append(args, "--reverse")
	}
	args = append(args, "-")
	_, err := r.exec(context.Background(), patch, true, args...)
	return err
}

// DiffHead returns the diff of one path against HEAD (staged and unstaged
// together). Untracked files get a synthesized "new file" diff.
func (r *Repo) DiffHead(path string) (string, error) {
	if out, err := r.read("ls-files", "--others", "--exclude-standard", "-z", "--", path); err == nil && out != "" {
		return r.newFileDiff(path)
	}
	if !r.hasCommits() {
		return r.read("diff", "--cached", "--no-color", "--", path)
	}
	return r.read("diff", "HEAD", "--no-color", "--", path)
}

// quotedEnd returns the index just past the closing quote of the C-quoted
// string at the start of s (len(s) when unterminated).
func quotedEnd(s string) int {
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '"':
			return i + 1
		}
	}
	return len(s)
}

// unquoteC decodes a git C-quoted path ("sp\tace", octal escapes for bytes).
// Strings without surrounding quotes are returned unchanged.
func unquoteC(s string) string {
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return s
	}
	s = s[1 : len(s)-1]
	var b []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' || i+1 >= len(s) {
			b = append(b, c)
			continue
		}
		i++
		switch c = s[i]; c {
		case 'a':
			b = append(b, 7)
		case 'b':
			b = append(b, 8)
		case 'f':
			b = append(b, 12)
		case 'n':
			b = append(b, '\n')
		case 'r':
			b = append(b, '\r')
		case 't':
			b = append(b, '\t')
		case 'v':
			b = append(b, 11)
		case '0', '1', '2', '3':
			if i+2 < len(s) && s[i+1] >= '0' && s[i+1] <= '7' && s[i+2] >= '0' && s[i+2] <= '7' {
				b = append(b, (c-'0')<<6|(s[i+1]-'0')<<3|(s[i+2]-'0'))
				i += 2
			} else {
				b = append(b, c)
			}
		default:
			b = append(b, c)
		}
	}
	return string(b)
}
