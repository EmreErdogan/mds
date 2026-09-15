// Package watch notifies subscribers when a file or directory changes.
package watch

import (
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
)

// Hub multiplexes filesystem events to per-path subscribers. Only the parent
// directories of subscribed paths are watched, so watching stays cheap even
// when serving a large tree.
type Hub struct {
	w    *fsnotify.Watcher
	mu   sync.Mutex
	subs map[string]map[chan struct{}]struct{} // absolute path -> subscribers
	dirs map[string]int                        // watched dir -> subscriber count
}

// New starts a Hub.
func New() (*Hub, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	h := &Hub{
		w:    w,
		subs: make(map[string]map[chan struct{}]struct{}),
		dirs: make(map[string]int),
	}
	go h.loop()
	return h, nil
}

// Close stops the Hub.
func (h *Hub) Close() error { return h.w.Close() }

// Subscribe returns a channel that receives a value whenever path (a file or
// a directory) changes. Call cancel to unsubscribe.
func (h *Hub) Subscribe(path string) (<-chan struct{}, func()) {
	path = filepath.Clean(path)
	ch := make(chan struct{}, 1)
	dir := watchDir(path)

	h.mu.Lock()
	if h.subs[path] == nil {
		h.subs[path] = make(map[chan struct{}]struct{})
	}
	h.subs[path][ch] = struct{}{}
	if h.dirs[dir] == 0 {
		_ = h.w.Add(dir) // best effort: a missing dir simply never fires
	}
	h.dirs[dir]++
	h.mu.Unlock()

	cancel := func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if set := h.subs[path]; set != nil {
			delete(set, ch)
			if len(set) == 0 {
				delete(h.subs, path)
			}
		}
		h.dirs[dir]--
		if h.dirs[dir] <= 0 {
			delete(h.dirs, dir)
			_ = h.w.Remove(dir)
		}
	}
	return ch, cancel
}

// watchDir returns the directory to watch for path: the path itself when it
// is a subscribed directory, otherwise its parent. fsnotify only reports
// events for direct children of a watched directory.
func watchDir(path string) string {
	if isDir(path) {
		return path
	}
	return filepath.Dir(path)
}

func (h *Hub) loop() {
	for {
		select {
		case ev, ok := <-h.w.Events:
			if !ok {
				return
			}
			if ev.Op == fsnotify.Chmod {
				continue
			}
			name := filepath.Clean(ev.Name)
			h.notify(name)
			h.notify(filepath.Dir(name))
		case _, ok := <-h.w.Errors:
			if !ok {
				return
			}
		}
	}
}

func (h *Hub) notify(path string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[path] {
		select {
		case ch <- struct{}{}:
		default: // a signal is already pending
		}
	}
}
