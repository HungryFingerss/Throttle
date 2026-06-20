// Package prices loads a per-model price table (LiteLLM-shaped) and computes
// the dollar cost of normalized token usage. It ships an embedded fallback
// table so the daemon prices correctly offline; the live table fetched from
// LiteLLM is overlaid on top (see fetch.go).
package prices

import (
	_ "embed"
	"encoding/json"
	"strings"
	"sync"

	"github.com/jagannivas/throttle/internal/core"
)

//go:embed fallback_prices.json
var fallbackJSON []byte

// Price is the per-token cost (USD) for each billable bucket.
type Price struct {
	Input         float64
	Output        float64
	CacheRead     float64
	CacheCreation float64
}

// litellmEntry is the subset of a LiteLLM model record we care about.
type litellmEntry struct {
	InputCostPerToken           float64 `json:"input_cost_per_token"`
	OutputCostPerToken          float64 `json:"output_cost_per_token"`
	CacheReadInputTokenCost     float64 `json:"cache_read_input_token_cost"`
	CacheCreationInputTokenCost float64 `json:"cache_creation_input_token_cost"`
}

// Table is a thread-safe model→price lookup with fuzzy fallback matching.
type Table struct {
	mu sync.RWMutex
	m  map[string]Price
}

// parseLiteLLM parses a LiteLLM-shaped JSON document defensively: it skips the
// "sample_spec" meta key, non-object values, and entries with no input price.
func parseLiteLLM(b []byte) (map[string]Price, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	out := make(map[string]Price, len(raw))
	for name, rm := range raw {
		if name == "sample_spec" || strings.HasPrefix(name, "_") {
			continue
		}
		var e litellmEntry
		if err := json.Unmarshal(rm, &e); err != nil {
			continue // not a model object — skip, don't fail the whole table
		}
		if e.InputCostPerToken == 0 && e.OutputCostPerToken == 0 {
			continue // no pricing info
		}
		out[normalizeKey(name)] = Price{
			Input:         e.InputCostPerToken,
			Output:        e.OutputCostPerToken,
			CacheRead:     e.CacheReadInputTokenCost,
			CacheCreation: e.CacheCreationInputTokenCost,
		}
	}
	return out, nil
}

// Fallback returns the embedded offline price table.
func Fallback() *Table {
	m, err := parseLiteLLM(fallbackJSON)
	if err != nil {
		// The embedded file is a build artifact; if it's broken that's a bug,
		// but never panic in the daemon — return an empty table (everything
		// will price as estimate 0, never crash).
		m = map[string]Price{}
	}
	return &Table{m: m}
}

// Overlay merges a fetched LiteLLM table on top of the current one (fetched
// prices win). Unknown-shaped input is ignored.
func (t *Table) Overlay(b []byte) error {
	m, err := parseLiteLLM(b)
	if err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for k, v := range m {
		t.m[k] = v
	}
	return nil
}

// normalizeKey lowercases and strips a provider prefix ("anthropic/foo" → "foo").
func normalizeKey(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	if i := strings.LastIndex(model, "/"); i >= 0 {
		model = model[i+1:]
	}
	return model
}

// Lookup returns the price for a model, applying fuzzy fallback:
//  1. exact (normalized) match
//  2. longest-prefix match against known keys (handles version drift like
//     claude-sonnet-4-6 → claude-sonnet-4-5 when only the latter is listed)
//
// The bool is false when nothing matched.
func (t *Table) Lookup(model string) (Price, bool) {
	key := normalizeKey(model)
	t.mu.RLock()
	defer t.mu.RUnlock()

	if p, ok := t.m[key]; ok {
		return p, true
	}

	// Longest shared-prefix match: pick the listed model that shares the most
	// leading characters with the requested one (same family/version line).
	var best string
	var bestLen int
	for k := range t.m {
		n := commonPrefixLen(key, k)
		// require a meaningful family match, not just "g" or "c"
		if n >= 6 && n > bestLen {
			bestLen, best = n, k
		}
	}
	if best != "" {
		return t.m[best], true
	}
	return Price{}, false
}

func commonPrefixLen(a, b string) int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return n
}

// Cost computes the USD cost of a normalized token count for a model. The bool
// "estimated" is true when no price (even fuzzy) was found — the caller should
// flag the cost as approximate. With no match, cost is 0 (never negative, never
// a crash).
func (t *Table) Cost(tk core.Tokens, model string) (usd float64, estimated bool) {
	p, ok := t.Lookup(model)
	if !ok {
		return 0, true
	}
	usd = float64(tk.Input)*p.Input +
		float64(tk.Output)*p.Output +
		float64(tk.CacheRead)*p.CacheRead +
		float64(tk.CacheCreation)*p.CacheCreation
	return usd, false
}

// Len reports how many models are priced (for diagnostics).
func (t *Table) Len() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.m)
}
