package lsmkvengine

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// FileSystem abstracts OS file operations for testing crash durability.
type FileSystem interface {
	Create(name string) (File, error)
	OpenFile(name string, flag int, perm os.FileMode) (File, error)
	Remove(name string) error
	Rename(oldPath, newPath string) error
	ReadDir(name string) ([]os.DirEntry, error)
	MkdirAll(path string, perm os.FileMode) error
	Stat(name string) (os.FileInfo, error)
}

// File represents an open file within a FileSystem.
type File interface {
	io.Reader
	io.ReaderAt
	io.Writer
	io.Closer
	Sync() error
	Stat() (os.FileInfo, error)
}

// DefaultOSFS is standard OS-backed filesystem.
type DefaultOSFS struct{}

var DefaultFS FileSystem = &DefaultOSFS{}

func (DefaultOSFS) Create(name string) (File, error) {
	return os.Create(name)
}

func (DefaultOSFS) OpenFile(name string, flag int, perm os.FileMode) (File, error) {
	return os.OpenFile(name, flag, perm)
}

func (DefaultOSFS) Remove(name string) error {
	return os.Remove(name)
}

func (DefaultOSFS) Rename(oldPath, newPath string) error {
	return os.Rename(oldPath, newPath)
}

func (DefaultOSFS) ReadDir(name string) ([]os.DirEntry, error) {
	return os.ReadDir(name)
}

func (DefaultOSFS) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (DefaultOSFS) Stat(name string) (os.FileInfo, error) {
	return os.Stat(name)
}

// MemFS is an in-memory crash-injectable FileSystem for testing.
type MemFS struct {
	mu    sync.RWMutex
	files map[string]*memFileEntry
}

type memFileEntry struct {
	mu       sync.RWMutex
	synced   []byte
	unsynced []byte
	isDir    bool
}

// NewMemFS creates an in-memory mock filesystem.
func NewMemFS() *MemFS {
	return &MemFS{
		files: make(map[string]*memFileEntry),
	}
}

func (m *MemFS) clean(path string) string {
	return filepath.Clean(path)
}

// Crash discards all unsynced bytes from all files, simulating a sudden power failure.
func (m *MemFS) Crash() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, f := range m.files {
		f.mu.Lock()
		f.unsynced = nil
		f.mu.Unlock()
	}
}

func (m *MemFS) MkdirAll(path string, perm os.FileMode) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.clean(path)
	if p == "." || p == "/" {
		return nil
	}
	m.files[p] = &memFileEntry{isDir: true}
	return nil
}

func (m *MemFS) Create(name string) (File, error) {
	return m.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o666)
}

func (m *MemFS) OpenFile(name string, flag int, perm os.FileMode) (File, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.clean(name)
	f, exists := m.files[p]
	if !exists {
		if flag&os.O_CREATE == 0 {
			return nil, os.ErrNotExist
		}
		f = &memFileEntry{}
		m.files[p] = f
	} else if flag&os.O_TRUNC != 0 {
		f.mu.Lock()
		f.synced = nil
		f.unsynced = nil
		f.mu.Unlock()
	}

	mf := &memFileHandle{
		fs:     m,
		path:   p,
		entry:  f,
		offset: 0,
	}
	if flag&os.O_APPEND != 0 {
		f.mu.RLock()
		mf.offset = int64(len(f.synced) + len(f.unsynced))
		f.mu.RUnlock()
	}
	return mf, nil
}

func (m *MemFS) Remove(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.clean(name)
	if _, exists := m.files[p]; !exists {
		return os.ErrNotExist
	}
	delete(m.files, p)
	return nil
}

func (m *MemFS) Rename(oldPath, newPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	oldClean := m.clean(oldPath)
	newClean := m.clean(newPath)
	f, exists := m.files[oldClean]
	if !exists {
		return os.ErrNotExist
	}
	m.files[newClean] = f
	delete(m.files, oldClean)
	return nil
}

func (m *MemFS) ReadDir(name string) ([]os.DirEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p := m.clean(name)
	var entries []os.DirEntry
	for fname, entry := range m.files {
		dir := filepath.Dir(fname)
		if dir == p {
			base := filepath.Base(fname)
			entries = append(entries, &memDirEntry{name: base, isDir: entry.isDir})
		}
	}
	return entries, nil
}

func (m *MemFS) Stat(name string) (os.FileInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p := m.clean(name)
	f, exists := m.files[p]
	if !exists {
		return nil, os.ErrNotExist
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	size := int64(len(f.synced) + len(f.unsynced))
	return &memFileInfo{name: filepath.Base(p), size: size, isDir: f.isDir}, nil
}

type memFileHandle struct {
	mu     sync.Mutex
	fs     *MemFS
	path   string
	entry  *memFileEntry
	offset int64
	closed bool
}

func (h *memFileHandle) Read(p []byte) (n int, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return 0, os.ErrClosed
	}
	h.entry.mu.RLock()
	defer h.entry.mu.RUnlock()
	data := append([]byte(nil), h.entry.synced...)
	data = append(data, h.entry.unsynced...)
	if h.offset >= int64(len(data)) {
		return 0, io.EOF
	}
	n = copy(p, data[h.offset:])
	h.offset += int64(n)
	return n, nil
}

func (h *memFileHandle) ReadAt(p []byte, off int64) (n int, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return 0, os.ErrClosed
	}
	h.entry.mu.RLock()
	defer h.entry.mu.RUnlock()
	data := append([]byte(nil), h.entry.synced...)
	data = append(data, h.entry.unsynced...)
	if off >= int64(len(data)) {
		return 0, io.EOF
	}
	n = copy(p, data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (h *memFileHandle) Write(p []byte) (n int, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return 0, os.ErrClosed
	}
	h.entry.mu.Lock()
	defer h.entry.mu.Unlock()
	currLen := int64(len(h.entry.synced) + len(h.entry.unsynced))
	if h.offset < currLen {
		// overwrite in memory
		full := append([]byte(nil), h.entry.synced...)
		full = append(full, h.entry.unsynced...)
		end := h.offset + int64(len(p))
		if end > int64(len(full)) {
			extra := end - int64(len(full))
			full = append(full, make([]byte, extra)...)
		}
		copy(full[h.offset:], p)
		h.entry.synced = nil
		h.entry.unsynced = full
		h.offset += int64(len(p))
		return len(p), nil
	}
	h.entry.unsynced = append(h.entry.unsynced, p...)
	h.offset += int64(len(p))
	return len(p), nil
}

func (h *memFileHandle) Sync() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return os.ErrClosed
	}
	h.entry.mu.Lock()
	defer h.entry.mu.Unlock()
	h.entry.synced = append(h.entry.synced, h.entry.unsynced...)
	h.entry.unsynced = nil
	return nil
}

func (h *memFileHandle) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	return nil
}

func (h *memFileHandle) Stat() (os.FileInfo, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.entry.mu.RLock()
	defer h.entry.mu.RUnlock()
	size := int64(len(h.entry.synced) + len(h.entry.unsynced))
	return &memFileInfo{name: filepath.Base(h.path), size: size, isDir: h.entry.isDir}, nil
}

type memFileInfo struct {
	name  string
	size  int64
	isDir bool
}

func (m *memFileInfo) Name() string       { return m.name }
func (m *memFileInfo) Size() int64        { return m.size }
func (m *memFileInfo) Mode() os.FileMode  { return 0o666 }
func (m *memFileInfo) ModTime() time.Time { return time.Time{} }
func (m *memFileInfo) IsDir() bool        { return m.isDir }
func (m *memFileInfo) Sys() any           { return nil }

type memDirEntry struct {
	name  string
	isDir bool
}

func (d *memDirEntry) Name() string      { return d.name }
func (d *memDirEntry) IsDir() bool       { return d.isDir }
func (d *memDirEntry) Type() os.FileMode { return 0 }
func (d *memDirEntry) Info() (os.FileInfo, error) {
	return &memFileInfo{name: d.name, isDir: d.isDir}, nil
}

// ReadAll reads all bytes from a file in fs.
func ReadAll(fs FileSystem, path string) ([]byte, error) {
	f, err := fs.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var buf bytes.Buffer
	_, err = io.Copy(&buf, f)
	return buf.Bytes(), err
}
