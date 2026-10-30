// Package logrotate is a size-based rotating log file writer.
//
// Mirrors the --log-file/--log-max-mb/--log-backups behavior of
// Flowseal/tg-ws-proxy (v1.7.x): the file never grows past maxBytes, rotating
// to file.1 ... file.N and dropping the oldest. At least one backup is kept,
// because rotation without a backup cannot bound the size.
package logrotate

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type Writer struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	backups  int
	f        *os.File
	size     int64
}

// New opens (appending to) path. maxBytes <= 0 disables rotation.
func New(path string, maxBytes int64, backups int) (*Writer, error) {
	if backups < 1 {
		backups = 1
	}
	w := &Writer{path: path, maxBytes: maxBytes, backups: backups}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *Writer) open() error {
	if dir := filepath.Dir(w.path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	w.f, w.size = f, st.Size()
	return nil
}

func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return 0, os.ErrClosed
	}
	if w.maxBytes > 0 && w.size > 0 && w.size+int64(len(p)) > w.maxBytes {
		if err := w.rotate(); err != nil {
			// Keep logging to the current file rather than losing lines.
			n, werr := w.f.Write(p)
			w.size += int64(n)
			return n, werr
		}
	}
	n, err := w.f.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *Writer) rotate() error {
	if err := w.f.Close(); err != nil {
		return err
	}
	w.f = nil
	// path.(N-1) -> path.N, ..., path -> path.1 ; the oldest is overwritten.
	for i := w.backups; i >= 1; i-- {
		src := w.path
		if i > 1 {
			src = fmt.Sprintf("%s.%d", w.path, i-1)
		}
		dst := fmt.Sprintf("%s.%d", w.path, i)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		_ = os.Remove(dst)
		if err := os.Rename(src, dst); err != nil {
			_ = w.open() // reopen whatever is there
			return err
		}
	}
	return w.open()
}

func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return nil
	}
	err := w.f.Close()
	w.f = nil
	return err
}
