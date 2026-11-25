package agent

import (
	"path/filepath"
	"runtime"
	"strings"
)

// FoldCase reports whether file names compare case-insensitively on this OS
// (Windows and macOS; Linux is case-sensitive).
func FoldCase() bool { return runtime.GOOS == "windows" || runtime.GOOS == "darwin" }

// EqualName compares two names, ignoring case only where the OS does.
func EqualName(a, b string) bool {
	if FoldCase() {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// SameDir compares two directories: cleaned, and case-insensitive on Windows
// and macOS only.
// Both sides go through filepath.Clean so "/" and "\" agree on Windows.
func SameDir(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return EqualName(filepath.Clean(a), filepath.Clean(b))
}
