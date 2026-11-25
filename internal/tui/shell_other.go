//go:build !windows

package tui

import (
	"os/exec"
	"strings"
)

func shellCommand(command string) *exec.Cmd { return exec.Command("sh", "-c", command) }

// shellQuote quotes s as one POSIX shell word.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
