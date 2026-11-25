// decsr-tui is a git TUI that also watches coding-agent sessions.
package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Decsr/decsr-tui/internal/agent"
	"github.com/Decsr/decsr-tui/internal/agent/claude"
	"github.com/Decsr/decsr-tui/internal/agent/codex"
	"github.com/Decsr/decsr-tui/internal/agent/opencode"
	"github.com/Decsr/decsr-tui/internal/config"
	"github.com/Decsr/decsr-tui/internal/tui"
	"github.com/Decsr/decsr-tui/internal/ui"
)

// version is set at release time with -ldflags "-X main.version=...".
var version = "0.4.0"

// sources builds the agent source from the runners the config enables.
func sources(names []string) agent.Source {
	ctor := map[string]func() agent.Source{"claude": claude.New, "codex": codex.New, "opencode": opencode.New}
	var ss []agent.Source
	for _, n := range names {
		if f, ok := ctor[n]; ok {
			ss = append(ss, f())
		}
	}
	if len(ss) == 1 {
		return ss[0]
	}
	return agent.Multi(ss...)
}

func main() {
	session := flag.String("session", "", "open the agent session whose id starts with this")
	cfgPath := flag.String("config", "", "config file (default: $"+config.Env+" or the user config dir)")
	theme := flag.String("theme", "", "colour theme: auto, dark or light (default: the config's theme, else auto)")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: decsr-tui [--session <id-prefix>] [--config <file>] [--theme auto|dark|light] [--version] [repo-dir]")
		flag.PrintDefaults()
	}
	flag.Parse()
	if *showVersion {
		fmt.Println("decsr-tui", version)
		return
	}
	dir := "."
	if flag.NArg() > 0 {
		dir = flag.Arg(0)
	}
	path := *cfgPath
	if path == "" {
		path = config.DefaultPath()
	}
	cfg, err := config.Load(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "decsr-tui: config:", err)
		os.Exit(1)
	}
	mode := cfg.Theme
	if *theme != "" {
		mode = *theme
	}
	if !ui.ValidTheme(mode) {
		fmt.Fprintf(os.Stderr, "decsr-tui: theme %q is not auto, dark or light\n", mode)
		os.Exit(1)
	}
	ui.Apply(mode, nil) // before the program starts: the terminal query must not race the input loop
	m, err := tui.New(dir, sources(cfg.Agents), tui.Options{Session: *session, Config: cfg})
	if err != nil {
		fmt.Fprintln(os.Stderr, "decsr-tui:", err)
		os.Exit(1)
	}
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "decsr-tui:", err)
		os.Exit(1)
	}
}
