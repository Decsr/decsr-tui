package agent

import "testing"

func TestContextMax(t *testing.T) {
	for model, want := range map[string]int64{
		"claude-opus-4-5":                   200_000,
		"claude-sonnet-4[1m]":               1_000_000,
		"opencode/deepseek-v4.1-flash-free": 128_000,
		"openai/gpt-5.1-codex":              400_000,
		"gpt-4.1":                           1_047_576,
		"gemini-2.5-pro":                    1_000_000,
		"":                                  0,
		"some-new-model":                    0,
	} {
		if got := ContextMax(model); got != want {
			t.Errorf("ContextMax(%q) = %d, want %d", model, got, want)
		}
	}
}
