package main

import (
	"context"
	"time"

	"github.com/jagannivas/throttle/internal/core"
	"github.com/jagannivas/throttle/internal/tally"
	"github.com/jagannivas/throttle/internal/watch"
)

// watchStart builds the fsnotify watcher over every adapter's roots, attaching
// only to sessions written since initialCutoff at startup. It returns a close
// func that stops the watcher.
func watchStart(tracker *tally.Tracker, adapters []core.Adapter, initialCutoff time.Time) (func(), error) {
	w, err := watch.New(tracker.HandlePath)
	if err != nil {
		return nil, err
	}
	w.InitialCutoff = initialCutoff

	var roots []string
	for _, a := range adapters {
		roots = append(roots, a.Roots()...)
	}

	ctx, cancel := context.WithCancel(context.Background())
	if err := w.Start(ctx, roots); err != nil {
		cancel()
		_ = w.Close()
		return nil, err
	}
	return func() {
		cancel()
		_ = w.Close()
	}, nil
}
