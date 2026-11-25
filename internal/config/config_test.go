package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDefaultsWhenMissingOrEmpty(t *testing.T) {
	for _, p := range []string{"", filepath.Join(t.TempDir(), "nope.yml"), write(t, "")} {
		c, err := Load(p)
		if err != nil {
			t.Fatalf("%q: %v", p, err)
		}
		if c.Refresh.GitSeconds != 2 || c.Refresh.AgentMillis != 500 || len(c.Agents) != 3 || len(c.CustomCommands) != 0 {
			t.Fatalf("%q: not the defaults: %+v", p, c)
		}
	}
}

func TestParseFull(t *testing.T) {
	c, err := Load(write(t, `
refresh: { gitSeconds: 5 }
agents: [claude, opencode]
customCommands:
  - key: "X"
    context: files
    description: "Run tests"
    command: "go test ./..."
    prompts:
      - { name: "msg", title: "Message" }
  - key: "Y"
    context: global
    command: "echo hi"
    output: popup
    confirm: true
`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Refresh.GitSeconds != 5 || c.Refresh.AgentMillis != 500 {
		t.Fatalf("partial refresh should keep the other default: %+v", c.Refresh)
	}
	if strings.Join(c.Agents, ",") != "claude,opencode" {
		t.Fatalf("agents %v", c.Agents)
	}
	x, y := c.CustomCommands[0], c.CustomCommands[1]
	if x.Output != "log" || len(x.Prompts) != 1 || x.Prompts[0].Name != "msg" {
		t.Fatalf("x %+v", x)
	}
	if y.Output != "popup" || !y.Confirm || y.Description != "echo hi" {
		t.Fatalf("y %+v", y)
	}
}

func TestBadFiles(t *testing.T) {
	cases := map[string]string{
		"bad yaml":        "refresh: [",
		"unknown field":   "colour: red\n",
		"unknown runner":  "agents: [gemini]\n",
		"bad context":     "customCommands:\n  - {key: X, context: nowhere, command: ls}\n",
		"missing command": "customCommands:\n  - {key: X, context: files}\n",
		"bad output":      "customCommands:\n  - {key: X, context: files, command: ls, output: tv}\n",
		"zero refresh":    "refresh: {gitSeconds: 0}\n",
	}
	for name, body := range cases {
		if _, err := Load(write(t, body)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestDefaultPathEnv(t *testing.T) {
	t.Setenv(Env, "x.yml")
	if DefaultPath() != "x.yml" {
		t.Fatal("env should override the path")
	}
}

func TestTheme(t *testing.T) {
	if c, _ := Load(""); c.Theme != "auto" {
		t.Fatalf("default theme %q", c.Theme)
	}
	c, err := Load(write(t, "theme: light\n"))
	if err != nil || c.Theme != "light" {
		t.Fatalf("theme: %q, %v", c.Theme, err)
	}
}
