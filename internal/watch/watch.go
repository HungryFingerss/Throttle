// Package watch turns OS file events into path callbacks. It watches each tool's
// log root recursively (fsnotify is per-directory, so we add a watch per subdir
// and pick up newly created dirs as they appear — e.g. Codex's YYYY/MM/DD tree).
// No polling: live updates are driven entirely by OS events.
package watch

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watcher reports file create/write paths via a callback.
type Watcher struct {
	fsw    *fsnotify.Watcher
	onPath func(string)

	// InitialCutoff: during the startup scan, only report files modified at or
	// after this time (skip ancient/idle logs). Zero reports everything.
	InitialCutoff time.Time

	mu      sync.Mutex
	watched map[string]struct{}
}

// New creates a Watcher. onPath is called (possibly concurrently with Start's
// caller, but serially from the event loop) for each changed file path.
func New(onPath func(string)) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &Watcher{fsw: fsw, onPath: onPath, watched: map[string]struct{}{}}, nil
}

// Start adds watches for the given roots (tolerating ones that don't exist yet
// by watching the nearest existing ancestor) and runs the event loop until ctx
// is cancelled.
func (w *Watcher) Start(ctx context.Context, roots []string) error {
	for _, root := range roots {
		w.addTree(root, true)
		// If the root itself doesn't exist yet, watch its nearest existing
		// ancestor so we notice when the tool is installed and the root appears.
		if !dirExists(root) {
			w.watchNearestAncestor(root)
		}
	}

	go w.loop(ctx)
	return nil
}

// Close stops the watcher.
func (w *Watcher) Close() error { return w.fsw.Close() }

func (w *Watcher) loop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			w.handle(ev)
		case _, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
			// fsnotify errors are non-fatal here; keep watching.
		}
	}
}

func (w *Watcher) handle(ev fsnotify.Event) {
	if ev.Op&(fsnotify.Create|fsnotify.Write) == 0 {
		return
	}
	fi, err := os.Stat(ev.Name)
	if err != nil {
		return // file may have vanished (rename/clear); ignore
	}
	if fi.IsDir() {
		if ev.Op&fsnotify.Create != 0 {
			w.addTree(ev.Name, false) // new dir (e.g. a new day folder) → watch it
		}
		return
	}
	w.onPath(ev.Name)
}

// addTree adds a watch to dir and every existing subdir. When initial is true,
// existing files are reported (subject to InitialCutoff); on a runtime new dir
// any files present are reported regardless of cutoff.
func (w *Watcher) addTree(dir string, initial bool) {
	if !dirExists(dir) {
		return
	}
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries, keep walking
		}
		if d.IsDir() {
			w.addWatch(path)
			return nil
		}
		// a file
		if initial && !w.InitialCutoff.IsZero() {
			info, e := d.Info()
			if e != nil || info.ModTime().Before(w.InitialCutoff) {
				return nil
			}
		}
		w.onPath(path)
		return nil
	})
}

func (w *Watcher) addWatch(dir string) {
	w.mu.Lock()
	_, done := w.watched[dir]
	if !done {
		w.watched[dir] = struct{}{}
	}
	w.mu.Unlock()
	if done {
		return
	}
	_ = w.fsw.Add(dir) // best-effort; ignore "already watching"/perm errors
}

func (w *Watcher) watchNearestAncestor(path string) {
	d := filepath.Dir(path)
	for d != "" {
		if dirExists(d) {
			w.addWatch(d)
			return
		}
		parent := filepath.Dir(d)
		if parent == d {
			return
		}
		d = parent
	}
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}
