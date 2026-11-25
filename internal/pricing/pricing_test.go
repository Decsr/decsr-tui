package pricing

import (
	"math"
	"testing"

	"github.com/Decsr/decsr-tui/internal/config"
)

func cfgWith(p map[string]config.ModelPrice) config.Config { return config.Config{Pricing: p} }

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestCostExactBeatsGlob(t *testing.T) {
	c := cfgWith(map[string]config.ModelPrice{
		"deepseek-*":        {InputPerMTok: 1, OutputPerMTok: 2},
		"deepseek-v4-flash": {InputPerMTok: 0.1, OutputPerMTok: 0.2},
	})
	usd, ok := Cost(c, "opencode", "deepseek-v4-flash", 1_000_000, 0, 1_000_000)
	if !ok || !near(usd, 0.3) {
		t.Fatalf("got %v %v", usd, ok)
	}
}

func TestCostLongestGlob(t *testing.T) {
	c := cfgWith(map[string]config.ModelPrice{
		"*":             {InputPerMTok: 9},
		"openai/*":      {InputPerMTok: 5},
		"openai/gpt-5*": {InputPerMTok: 3},
		"*/gpt-5*":      {InputPerMTok: 4},
	})
	usd, ok := Cost(c, "opencode", "openai/gpt-5.1", 1_000_000, 0, 0)
	if !ok || !near(usd, 3) {
		t.Fatalf("got %v %v", usd, ok)
	}
}

func TestCostLocalIsFree(t *testing.T) {
	c := cfgWith(map[string]config.ModelPrice{"*": {InputPerMTok: 9, OutputPerMTok: 9}})
	for _, r := range []string{"ollama", "lmstudio", "mlx"} {
		if usd, ok := Cost(c, r, "x", 1e6, 0, 1e6); !ok || usd != 0 {
			t.Errorf("%s: %v %v", r, usd, ok)
		}
	}
}

func TestCostUnknownWithoutPrice(t *testing.T) {
	if _, ok := Cost(config.Config{}, "claude", "claude-opus-4", 10, 10, 10); ok {
		t.Fatal("no table: the cost must be unknown")
	}
	c := cfgWith(map[string]config.ModelPrice{"gpt-*": {InputPerMTok: 1}})
	if _, ok := Cost(c, "claude", "claude-opus-4", 10, 10, 10); ok {
		t.Fatal("no matching key: the cost must be unknown")
	}
}

func TestCostCachedUsesCachePrice(t *testing.T) {
	c := cfgWith(map[string]config.ModelPrice{"m": {InputPerMTok: 10, OutputPerMTok: 0, CacheReadPerMTok: 1}})
	if usd, _ := Cost(c, "claude", "m", 0, 1_000_000, 0); !near(usd, 1) {
		t.Fatalf("cache price: %v", usd)
	}
	c = cfgWith(map[string]config.ModelPrice{"m": {InputPerMTok: 10}})
	if usd, _ := Cost(c, "claude", "m", 0, 1_000_000, 0); !near(usd, 10) {
		t.Fatalf("cache at input price: %v", usd)
	}
}
