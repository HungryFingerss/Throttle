package prices

import (
	"math"
	"testing"

	"github.com/jagannivas/throttle/internal/core"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-12 }

func TestFallbackLoads(t *testing.T) {
	tbl := Fallback()
	if tbl.Len() == 0 {
		t.Fatal("fallback table is empty")
	}
}

func TestCostExactClaude(t *testing.T) {
	tbl := Fallback()
	tk := core.Tokens{Input: 1000, Output: 500, CacheRead: 2000, CacheCreation: 4000}
	got, est := tbl.Cost(tk, "claude-sonnet-4-6")
	if est {
		t.Fatal("claude-sonnet-4-6 should be an exact price, not an estimate")
	}
	// 1000*3e-6 + 500*15e-6 + 2000*0.3e-6 + 4000*3.75e-6
	want := 0.003 + 0.0075 + 0.0006 + 0.015
	if !approx(got, want) {
		t.Fatalf("cost = %.10f, want %.10f", got, want)
	}
}

func TestProviderPrefixStripped(t *testing.T) {
	tbl := Fallback()
	tk := core.Tokens{Output: 1000}
	a, _ := tbl.Cost(tk, "claude-sonnet-4-6")
	b, est := tbl.Cost(tk, "anthropic/claude-sonnet-4-6")
	if est {
		t.Fatal("provider-prefixed model should still match")
	}
	if !approx(a, b) {
		t.Fatalf("prefixed price %.10f != bare price %.10f", b, a)
	}
}

func TestFuzzyVersionFallback(t *testing.T) {
	tbl := Fallback()
	tk := core.Tokens{Output: 1000}
	// claude-sonnet-4-9 isn't listed; should fuzzy-match the sonnet-4 family.
	got, est := tbl.Cost(tk, "claude-sonnet-4-9")
	if est {
		t.Fatal("expected a fuzzy family match, got estimate=true")
	}
	want, _ := tbl.Cost(tk, "claude-sonnet-4-6")
	if !approx(got, want) {
		t.Fatalf("fuzzy sonnet price %.10f != sonnet-4-6 %.10f", got, want)
	}
}

func TestUnknownModelIsEstimate(t *testing.T) {
	tbl := Fallback()
	tk := core.Tokens{Input: 100}
	got, est := tbl.Cost(tk, "totally-unknown-zzz")
	if !est {
		t.Fatal("unknown model should be flagged estimate")
	}
	if got != 0 {
		t.Fatalf("unknown model cost should be 0, got %v", got)
	}
}

func TestOverlayWins(t *testing.T) {
	tbl := Fallback()
	override := []byte(`{"claude-sonnet-4-6":{"input_cost_per_token":0.001,"output_cost_per_token":0.002}}`)
	if err := tbl.Overlay(override); err != nil {
		t.Fatalf("overlay: %v", err)
	}
	got, _ := tbl.Cost(core.Tokens{Input: 1000}, "claude-sonnet-4-6")
	if !approx(got, 1.0) { // 1000 * 0.001
		t.Fatalf("overlaid price not applied: got %v", got)
	}
}
