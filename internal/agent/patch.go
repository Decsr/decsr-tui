package agent

import "strings"

// FilePatch is one file of an apply_patch envelope.
type FilePatch struct {
	Path             string
	OldText, NewText string // removed and added lines, newline-joined
	Added, Removed   int
}

// ParsePatch reads the "*** Begin Patch" format used by Codex and opencode's
// apply_patch tool and returns one FilePatch per file (none if unrecognised).
func ParsePatch(text string) []FilePatch {
	var files []FilePatch
	var old, add []string
	flush := func() {
		if n := len(files); n > 0 {
			files[n-1].OldText, files[n-1].NewText = strings.Join(old, "\n"), strings.Join(add, "\n")
			files[n-1].Removed, files[n-1].Added = len(old), len(add)
		}
		old, add = nil, nil
	}
	for _, ln := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		switch {
		case strings.HasPrefix(ln, "*** Add File: "), strings.HasPrefix(ln, "*** Update File: "), strings.HasPrefix(ln, "*** Delete File: "):
			flush()
			files = append(files, FilePatch{Path: strings.TrimSpace(ln[strings.Index(ln, ": ")+2:])})
		case strings.HasPrefix(ln, "*** Move to: "):
			if n := len(files); n > 0 {
				files[n-1].Path = strings.TrimSpace(strings.TrimPrefix(ln, "*** Move to: "))
			}
		case strings.HasPrefix(ln, "***"), strings.HasPrefix(ln, "@@"):
		case len(files) > 0 && strings.HasPrefix(ln, "+"):
			add = append(add, ln[1:])
		case len(files) > 0 && strings.HasPrefix(ln, "-"):
			old = append(old, ln[1:])
		}
	}
	flush()
	return files
}
