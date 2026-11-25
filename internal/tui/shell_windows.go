//go:build windows

package tui

import (
	"os/exec"
	"strings"
	"syscall"
)

// shellCommand runs command through cmd /C. CmdLine bypasses Go's argument
// escaping, which cmd.exe does not understand.
func shellCommand(command string) *exec.Cmd {
	c := exec.Command("cmd")
	c.SysProcAttr = &syscall.SysProcAttr{CmdLine: `cmd /S /C "` + command + `"`}
	return c
}

// shellQuote quotes s as one cmd.exe word. A quote is doubled so cmd keeps its
// quoting state; %VAR% is still expanded by cmd, which no quoting can stop.
func shellQuote(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
