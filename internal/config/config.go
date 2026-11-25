// Package config loads the optional decsr-tui config file.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/Decsr/decsr-tui/internal/ui"
)

// Env names the environment variable that overrides the default path.
const Env = "DECSR_TUI_CONFIG"

// Custom is one user-defined command bound to a key.
type Custom struct {
	Key         string   `yaml:"key"`
	Context     string   `yaml:"context"`
	Description string   `yaml:"description"`
	Command     string   `yaml:"command"`
	Confirm     bool     `yaml:"confirm"`
	Prompts     []Prompt `yaml:"prompts"`
	Output      string   `yaml:"output"` // log (default) | popup | terminal
}

// Prompt is one question asked before a custom command runs.
type Prompt struct {
	Name  string `yaml:"name"`
	Title string `yaml:"title"`
}

// Config is the whole file.
type Config struct {
	Refresh struct {
		GitSeconds  int `yaml:"gitSeconds"`
		AgentMillis int `yaml:"agentMillis"`
	} `yaml:"refresh"`
	Theme          string   `yaml:"theme"` // auto (default) | dark | light
	Agents         []string `yaml:"agents"`
	CustomCommands []Custom `yaml:"customCommands"`
}

// Contexts are the values a custom command's context may take.
var Contexts = []string{"files", "branches", "commits", "stash", "tags", "worktrees", "agent", "global"}

// Runners are the agent watchers that can be enabled.
var Runners = []string{"claude", "codex", "opencode"}

// Default is the config used when no file exists.
func Default() Config {
	var c Config
	c.Refresh.GitSeconds = 2
	c.Refresh.AgentMillis = 500
	c.Theme = ui.ThemeAuto
	c.Agents = append([]string(nil), Runners...)
	return c
}

// DefaultPath is $DECSR_TUI_CONFIG, else <user config dir>/decsr-tui/config.yml.
func DefaultPath() string {
	if p := os.Getenv(Env); p != "" {
		return p
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "decsr-tui", "config.yml")
}

// Load reads path; a missing file (or an empty path) gives the defaults.
func Load(path string) (Config, error) {
	c := Default()
	if path == "" {
		return c, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil && !errors.Is(err, io.EOF) {
		return Default(), fmt.Errorf("%s: %w", path, err)
	}
	if err := c.validate(); err != nil {
		return Default(), fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

func in(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func (c *Config) validate() error {
	if !ui.ValidTheme(c.Theme) {
		return fmt.Errorf("theme %q is not auto, dark or light", c.Theme)
	}
	if c.Refresh.GitSeconds <= 0 || c.Refresh.AgentMillis <= 0 {
		return errors.New("refresh.gitSeconds and refresh.agentMillis must be positive")
	}
	for _, a := range c.Agents {
		if !in(Runners, a) {
			return fmt.Errorf("agents: unknown runner %q (want claude, codex or opencode)", a)
		}
	}
	for i := range c.CustomCommands {
		cc := &c.CustomCommands[i]
		at := fmt.Sprintf("customCommands[%d]", i)
		if cc.Key == "" || cc.Command == "" {
			return fmt.Errorf("%s: key and command are required", at)
		}
		if !in(Contexts, cc.Context) {
			return fmt.Errorf("%s: context %q is not one of %v", at, cc.Context, Contexts)
		}
		if cc.Output == "" {
			cc.Output = "log"
		}
		if cc.Output != "log" && cc.Output != "popup" && cc.Output != "terminal" {
			return fmt.Errorf("%s: output %q is not log, popup or terminal", at, cc.Output)
		}
		if cc.Description == "" {
			cc.Description = cc.Command
		}
		for _, p := range cc.Prompts {
			if p.Name == "" {
				return fmt.Errorf("%s: a prompt needs a name", at)
			}
		}
	}
	return nil
}
