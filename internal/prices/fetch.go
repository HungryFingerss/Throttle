package prices

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// LiteLLMURL is the maintained, per-model price table. Pulled on install and
// refreshed weekly; cached locally. Never hand-maintain prices.
const LiteLLMURL = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"

// Fetch downloads a LiteLLM-shaped price table. The caller validates by
// Overlay-ing the result onto a Table.
func Fetch(ctx context.Context, url string) ([]byte, error) {
	if url == "" {
		url = LiteLLMURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("price fetch: status %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 32<<20)) // 32 MiB ceiling
}

// LoadCached returns a Table = embedded fallback overlaid with the on-disk
// cache (if present and parseable). It never fails: a missing/corrupt cache
// just yields the fallback table.
func LoadCached(cachePath string) *Table {
	t := Fallback()
	if b, err := os.ReadFile(cachePath); err == nil {
		_ = t.Overlay(b) // best-effort; ignore corrupt cache
	}
	return t
}

// cacheFresh reports whether the cache file exists and is younger than maxAge.
func cacheFresh(cachePath string, maxAge time.Duration, now time.Time) bool {
	fi, err := os.Stat(cachePath)
	if err != nil {
		return false
	}
	return now.Sub(fi.ModTime()) < maxAge
}

// RefreshIfStale fetches the live table and writes it to cachePath when the
// cache is missing or older than maxAge, then overlays it onto t. It is
// best-effort: any network/IO error is returned but t remains usable with
// whatever it already had. Safe to call in a background goroutine.
func RefreshIfStale(ctx context.Context, t *Table, cachePath string, maxAge time.Duration, now time.Time) error {
	if cacheFresh(cachePath, maxAge, now) {
		return nil
	}
	b, err := Fetch(ctx, LiteLLMURL)
	if err != nil {
		return err
	}
	if err := t.Overlay(b); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(cachePath, b, 0o644)
}
