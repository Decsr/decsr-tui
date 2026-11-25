// Package pricing turns token counts into an estimated USD cost from the
// prices the user configured under "pricing:". There are no built-in prices:
// they go stale, so a cost is shown only when a price is known.
package pricing

import (
	"path"

	"github.com/Decsr/decsr-tui/internal/config"
)

// IsLocal reports whether the runner runs on the user's machine; its cost is
// always zero.
func IsLocal(runner string) bool {
	switch runner {
	case "ollama", "lmstudio", "mlx":
		return true
	}
	return false
}

// Lookup returns the configured price of model: the exact key first, then the
// matching glob with the most characters (ties go to the smaller key).
func Lookup(cfg config.Config, model string) (config.ModelPrice, bool) {
	if p, ok := cfg.Pricing[model]; ok {
		return p, true
	}
	best, found := "", false
	for key := range cfg.Pricing {
		if ok, err := path.Match(key, model); err != nil || !ok {
			continue
		}
		if !found || len(key) > len(best) || (len(key) == len(best) && key < best) {
			best, found = key, true
		}
	}
	if !found {
		return config.ModelPrice{}, false
	}
	return cfg.Pricing[best], true
}

// Cost is the estimated USD cost of in fresh input tokens, cached input
// tokens read and out output tokens of model on runner. Cached tokens are
// billed at the cache price when one is configured, else at the input price.
// known is false when no price is configured for the model.
func Cost(cfg config.Config, runner, model string, in, cached, out int64) (usd float64, known bool) {
	if IsLocal(runner) {
		return 0, true
	}
	p, ok := Lookup(cfg, model)
	if !ok {
		return 0, false
	}
	cp := p.CacheReadPerMTok
	if cp == 0 {
		cp = p.InputPerMTok
	}
	return (float64(in)*p.InputPerMTok + float64(cached)*cp + float64(out)*p.OutputPerMTok) / 1e6, true
}
