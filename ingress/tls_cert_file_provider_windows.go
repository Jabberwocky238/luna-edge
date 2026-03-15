//go:build windows

package ingress

import (
	"path/filepath"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

type certWatcher struct {
	onEvent func([]string)
	done    chan struct{}
	mu      sync.Mutex
	handles map[string]windows.Handle
	wg      sync.WaitGroup
}

func newCertWatcher(certRoot string, onEvent func([]string)) (*certWatcher, error) {
	w := &certWatcher{
		onEvent: onEvent,
		done:    make(chan struct{}),
		handles: make(map[string]windows.Handle),
	}
	if err := w.refresh(certRoot); err != nil {
		w.close()
		return nil, err
	}
	return w, nil
}

func (w *certWatcher) close() {
	w.mu.Lock()
	select {
	case <-w.done:
	default:
		close(w.done)
	}
	handles := make([]windows.Handle, 0, len(w.handles))
	for _, handle := range w.handles {
		handles = append(handles, handle)
	}
	w.handles = map[string]windows.Handle{}
	w.mu.Unlock()

	for _, handle := range handles {
		_ = windows.CancelIoEx(handle, nil)
		_ = windows.CloseHandle(handle)
	}
	w.wg.Wait()
}

func (w *certWatcher) refresh(certRoot string) error {
	paths := collectWatchDirectories(certRoot)

	w.mu.Lock()
	for path, handle := range w.handles {
		if _, ok := paths[path]; ok {
			continue
		}
		delete(w.handles, path)
		_ = windows.CancelIoEx(handle, nil)
		_ = windows.CloseHandle(handle)
	}
	w.mu.Unlock()

	for path := range paths {
		if err := w.ensureWatch(path, certRoot); err != nil {
			continue
		}
	}
	return nil
}

func (w *certWatcher) ensureWatch(path, certRoot string) error {
	w.mu.Lock()
	if _, ok := w.handles[path]; ok {
		w.mu.Unlock()
		return nil
	}
	w.mu.Unlock()

	ptr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	handle, err := windows.CreateFile(
		ptr,
		windows.FILE_LIST_DIRECTORY,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		return err
	}

	w.mu.Lock()
	select {
	case <-w.done:
		w.mu.Unlock()
		_ = windows.CloseHandle(handle)
		return nil
	default:
	}
	if existing, ok := w.handles[path]; ok {
		w.mu.Unlock()
		_ = windows.CloseHandle(handle)
		_ = existing
		return nil
	}
	w.handles[path] = handle
	w.wg.Add(1)
	w.mu.Unlock()

	go w.watchPath(path, certRoot, handle)
	return nil
}

func (w *certWatcher) watchPath(path, certRoot string, handle windows.Handle) {
	defer w.wg.Done()

	buffer := make([]byte, 64*1024)
	for {
		select {
		case <-w.done:
			return
		default:
		}

		var bytesReturned uint32
		err := windows.ReadDirectoryChanges(
			handle,
			&buffer[0],
			uint32(len(buffer)),
			false,
			windows.FILE_NOTIFY_CHANGE_FILE_NAME|
				windows.FILE_NOTIFY_CHANGE_DIR_NAME|
				windows.FILE_NOTIFY_CHANGE_LAST_WRITE|
				windows.FILE_NOTIFY_CHANGE_CREATION,
			&bytesReturned,
			nil,
			0,
		)
		if err != nil {
			select {
			case <-w.done:
				return
			default:
			}
			if err == windows.ERROR_OPERATION_ABORTED || err == windows.ERROR_INVALID_HANDLE {
				return
			}
			return
		}
		changedPaths := changedPathsFromNotifyBuffer(path, buffer, bytesReturned)
		if len(changedPaths) == 0 {
			continue
		}
		if err := w.refresh(certRoot); err == nil && w.onEvent != nil {
			w.onEvent(changedPaths)
		}
	}
}

func changedPathsFromNotifyBuffer(basePath string, buf []byte, n uint32) []string {
	changed := make(map[string]struct{})
	offset := uint32(0)
	for offset < n {
		info := (*windows.FileNotifyInformation)(unsafe.Pointer(&buf[offset]))
		nameLen := int(info.FileNameLength / 2)
		if nameLen > 0 {
			nameSlice := unsafe.Slice(&info.FileName, nameLen)
			name := windows.UTF16ToString(nameSlice)
			if name != "" {
				changed[filepath.Join(basePath, filepath.Clean(name))] = struct{}{}
			}
		} else {
			changed[basePath] = struct{}{}
		}
		if info.NextEntryOffset == 0 {
			break
		}
		offset += info.NextEntryOffset
	}
	out := make([]string, 0, len(changed))
	for path := range changed {
		out = append(out, path)
	}
	return out
}
