package agent

import "strings"

// ContextMax is the context window, in tokens, of a model family; 0 when the
// model is not known. A "[1m]" or "-1m" mark in the name means the 1M window.
// The table is matched on the lower-cased model id with its "vendor/" removed,
// longer keys first, so the answer is a stable estimate and not a promise.
func ContextMax(model string) int64 {
	m := strings.ToLower(strings.TrimSpace(model))
	if i := strings.LastIndex(m, "/"); i >= 0 {
		m = m[i+1:]
	}
	if m == "" {
		return 0
	}
	if strings.Contains(m, "[1m]") || strings.HasSuffix(m, "-1m") {
		return 1_000_000
	}
	for _, e := range contextTable {
		if strings.Contains(m, e.key) {
			return e.max
		}
	}
	return 0
}

// contextTable is ordered: the first key that the model contains wins.
var contextTable = []struct {
	key string
	max int64
}{
	{"gpt-4.1", 1_047_576},
	{"gpt-4o", 128_000},
	{"gpt-5", 400_000},
	{"codex", 400_000},
	{"o3", 200_000},
	{"o4", 200_000},
	{"claude", 200_000},
	{"opus", 200_000},
	{"sonnet", 200_000},
	{"haiku", 200_000},
	{"gemini", 1_000_000},
	{"deepseek", 128_000},
	{"kimi", 262_144},
	{"qwen3-coder", 262_144},
	{"grok", 256_000},
	{"glm", 128_000},
}
