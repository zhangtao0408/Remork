package watch

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

type EventKind string

const (
	EventCreate   EventKind = "create"
	EventUpdate   EventKind = "update"
	EventDelete   EventKind = "delete"
	EventRename   EventKind = "rename"
	EventOverflow EventKind = "overflow"
)

type Event struct {
	Kind           EventKind `json:"kind"`
	Path           string    `json:"path,omitempty"`
	Revision       string    `json:"revision"`
	ResyncRequired bool      `json:"resync_required,omitempty"`
}

type Watcher struct {
	root   string
	fs     *fsnotify.Watcher
	events chan Event
	done   chan struct{}
}

func New(root string) (*Watcher, error) {
	fs, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &Watcher{root: root, fs: fs, events: make(chan Event, 32), done: make(chan struct{})}, nil
}

func (w *Watcher) Start() error {
	if err := w.addDirs(w.root); err != nil {
		return err
	}
	go w.loop()
	return nil
}

func (w *Watcher) Events() <-chan Event {
	return w.events
}

func (w *Watcher) Close() error {
	select {
	case <-w.done:
	default:
		close(w.done)
	}
	return w.fs.Close()
}

func Overflow() Event {
	return Event{Kind: EventOverflow, Revision: revision(), ResyncRequired: true}
}

func (w *Watcher) loop() {
	for {
		select {
		case ev, ok := <-w.fs.Events:
			if !ok {
				return
			}
			rel, err := filepath.Rel(w.root, ev.Name)
			if err != nil {
				if !w.emit(Overflow()) {
					return
				}
				continue
			}
			rel = filepath.ToSlash(rel)
			if rel == "." || ignoredPath(rel) {
				continue
			}
			if ev.Has(fsnotify.Create) {
				if info, err := os.Stat(ev.Name); err == nil && info.IsDir() && !ignoredPath(rel) {
					if err := w.addDirs(ev.Name); err != nil {
						if !w.emit(Overflow()) {
							return
						}
					}
				}
			}
			kind := EventUpdate
			if ev.Has(fsnotify.Create) {
				kind = EventCreate
			}
			if ev.Has(fsnotify.Remove) {
				kind = EventDelete
			}
			if ev.Has(fsnotify.Rename) {
				kind = EventRename
			}
			if !w.emit(Event{Kind: kind, Path: rel, Revision: revision()}) {
				return
			}
		case _, ok := <-w.fs.Errors:
			if !ok {
				return
			}
			if !w.emit(Overflow()) {
				return
			}
		case <-w.done:
			return
		}
	}
}

func (w *Watcher) emit(ev Event) bool {
	select {
	case w.events <- ev:
		return true
	case <-w.done:
		return false
	default:
	}
	if ev.Kind != EventOverflow {
		ev = Overflow()
	}
	select {
	case w.events <- ev:
	case <-w.done:
		return false
	default:
	}
	return true
}

func (w *Watcher) addDirs(root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(w.root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel != "." && ignoredPath(rel) {
			return filepath.SkipDir
		}
		return w.fs.Add(path)
	})
}

func ignoredPath(path string) bool {
	path = strings.TrimPrefix(filepath.ToSlash(path), "./")
	return path == ".git" || strings.HasPrefix(path, ".git/") || path == ".remork" || strings.HasPrefix(path, ".remork/")
}

func revision() string {
	return time.Now().UTC().Format("20060102150405.000000000")
}
