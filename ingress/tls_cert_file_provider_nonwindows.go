//go:build !windows

package ingress

import (
	"path/filepath"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"
)

type certWatcher struct {
	fd      int
	onEvent func([]string)
	done    chan struct{}
	mu      sync.Mutex
	watches map[string]uint32
	paths   map[uint32]string
}

func newCertWatcher(certRoot string, onEvent func([]string)) (*certWatcher, error) {
	fd, err := unix.InotifyInit1(unix.IN_CLOEXEC)
	if err != nil {
		return nil, err
	}
	w := &certWatcher{
		fd:      fd,
		onEvent: onEvent,
		done:    make(chan struct{}),
		watches: make(map[string]uint32),
		paths:   make(map[uint32]string),
	}
	if err := w.refresh(certRoot); err != nil {
		w.close()
		return nil, err
	}
	go w.run(certRoot)
	return w, nil
}

func (w *certWatcher) close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	select {
	case <-w.done:
	default:
		close(w.done)
	}
	if w.fd >= 0 {
		_ = unix.Close(w.fd)
		w.fd = -1
	}
}

func (w *certWatcher) run(certRoot string) {
	buf := make([]byte, 16*1024)
	for {
		n, err := unix.Read(w.fd, buf)
		if err != nil {
			select {
			case <-w.done:
				return
			default:
			}
			if err == unix.EINTR {
				continue
			}
			return
		}
		changedPaths := w.parseChangedPaths(buf, n)
		if len(changedPaths) == 0 {
			continue
		}
		if err := w.refresh(certRoot); err == nil && w.onEvent != nil {
			w.onEvent(changedPaths)
		}
	}
}

func (w *certWatcher) refresh(certRoot string) error {
	paths := collectWatchDirectories(certRoot)

	w.mu.Lock()
	defer w.mu.Unlock()

	for path, wd := range w.watches {
		if _, ok := paths[path]; ok {
			continue
		}
		_, _ = unix.InotifyRmWatch(w.fd, wd)
		delete(w.watches, path)
		delete(w.paths, wd)
	}
	for path := range paths {
		if _, ok := w.watches[path]; ok {
			continue
		}
		wd, err := unix.InotifyAddWatch(w.fd, path, unix.IN_CREATE|unix.IN_DELETE|unix.IN_CLOSE_WRITE|unix.IN_MOVED_FROM|unix.IN_MOVED_TO|unix.IN_DELETE_SELF|unix.IN_MOVE_SELF)
		if err != nil {
			continue
		}
		watchID := uint32(wd)
		w.watches[path] = watchID
		w.paths[watchID] = path
	}
	return nil
}

func (w *certWatcher) parseChangedPaths(buf []byte, n int) []string {
	w.mu.Lock()
	defer w.mu.Unlock()

	offset := 0
	changed := make(map[string]struct{})
	for offset+unix.SizeofInotifyEvent <= n {
		event := (*unix.InotifyEvent)(unsafe.Pointer(&buf[offset]))
		basePath := w.paths[uint32(event.Wd)]
		nameBytes := buf[offset+unix.SizeofInotifyEvent : offset+unix.SizeofInotifyEvent+int(event.Len)]
		name := strings.TrimRight(string(nameBytes), "\x00")
		if basePath != "" {
			if name == "" {
				changed[basePath] = struct{}{}
			} else {
				changed[filepath.Join(basePath, name)] = struct{}{}
			}
		}
		offset += unix.SizeofInotifyEvent + int(event.Len)
	}
	out := make([]string, 0, len(changed))
	for path := range changed {
		out = append(out, path)
	}
	return out
}
