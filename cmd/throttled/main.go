// Command throttled is the resident Throttle daemon. It watches each tool's log
// root, maintains live per-session spend, serves the dashboard over WebSocket,
// and exposes the localhost hook API. It only ever READS tool logs and writes
// solely to its own state dir; it fails open by construction.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jagannivas/throttle/internal/adapters/claude"
	"github.com/jagannivas/throttle/internal/adapters/codex"
	"github.com/jagannivas/throttle/internal/api"
	"github.com/jagannivas/throttle/internal/config"
	"github.com/jagannivas/throttle/internal/core"
	"github.com/jagannivas/throttle/internal/enforce"
	"github.com/jagannivas/throttle/internal/prices"
	"github.com/jagannivas/throttle/internal/store"
	"github.com/jagannivas/throttle/internal/tally"
	"github.com/jagannivas/throttle/web"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:7878", "dashboard/API listen address (localhost only)")
	startupWindow := flag.Duration("startup-window", 6*time.Hour, "only attach to sessions written within this window at startup")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("throttled ")

	throttleDir := config.ThrottleDir()
	if err := os.MkdirAll(throttleDir, 0o755); err != nil {
		log.Fatalf("cannot create state dir %s: %v", throttleDir, err)
	}

	// --- prices: embedded fallback, overlaid with cache, refreshed in background.
	priceTable := prices.LoadCached(config.PriceCachePath())
	log.Printf("priced models loaded: %d", priceTable.Len())
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := prices.RefreshIfStale(ctx, priceTable, config.PriceCachePath(), 7*24*time.Hour, time.Now()); err != nil {
			log.Printf("price refresh skipped: %v", err)
		} else {
			log.Printf("price table refreshed: %d models", priceTable.Len())
		}
	}()

	// --- adapters (Claude + Codex; Gemini/Aider wired in M5).
	adapters := []core.Adapter{claude.New(), codex.New()}

	// --- tracker + restore persisted offsets.
	tracker := tally.New(priceTable, adapters)
	if sessions, err := store.Load(config.StatePath()); err != nil {
		log.Printf("state load failed (starting fresh): %v", err)
	} else if len(sessions) > 0 {
		tracker.Import(sessions)
		log.Printf("restored %d sessions from state", len(sessions))
	}

	// --- enforcer: caps + kill-switch (the Checker the hook calls).
	enforcer := enforce.New(tracker)

	// --- api server; tracker pushes updates to the dashboard via the sink.
	srv := api.New(tracker, enforcer, web.FS())
	srv.SetControls(enforcer)
	tracker.SetSink(srv.Broadcast)

	// --- watcher: instant discovery + live tailing, no polling.
	w, err := watchStart(tracker, adapters, time.Now().Add(-*startupWindow))
	if err != nil {
		log.Fatalf("watcher: %v", err)
	}
	defer w()

	// --- background: idle sweep + periodic state save.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go backgroundLoops(ctx, tracker)

	httpSrv := &http.Server{Addr: *addr, Handler: srv.Handler()}
	go func() {
		log.Printf("dashboard: http://%s", *addr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http: %v", err)
		}
	}()

	<-ctx.Done()
	log.Printf("shutting down…")

	// Persist final state so the next start resumes from current offsets.
	if err := store.Save(config.StatePath(), tracker.ExportAll()); err != nil {
		log.Printf("final state save failed: %v", err)
	}
	shutCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutCtx)
}

func backgroundLoops(ctx context.Context, tracker *tally.Tracker) {
	idle := time.NewTicker(15 * time.Second)
	save := time.NewTicker(10 * time.Second)
	defer idle.Stop()
	defer save.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-idle.C:
			tracker.SweepIdle()
		case <-save.C:
			if err := store.Save(config.StatePath(), tracker.ExportAll()); err != nil {
				log.Printf("state save failed: %v", err)
			}
		}
	}
}
