package lsmkvengine

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// ManifestState tracks versioning, active sstables across levels, and next IDs.
type ManifestState struct {
	NextFileID uint64           `json:"next_file_id"`
	Levels     map[int][]uint64 `json:"levels"`
}

// Manifest manages durable tracking of levels and sstable file numbers.
type Manifest struct {
	mu    sync.Mutex
	fs    FileSystem
	dir   string
	state ManifestState
}

// OpenManifest loads or initializes the manifest file.
func OpenManifest(fs FileSystem, dir string) (*Manifest, error) {
	mPath := fmt.Sprintf("%s/MANIFEST", dir)
	m := &Manifest{
		fs:  fs,
		dir: dir,
		state: ManifestState{
			NextFileID: 1,
			Levels:     make(map[int][]uint64),
		},
	}

	data, err := ReadAll(fs, mPath)
	if err != nil {
		if os.IsNotExist(err) {
			if err := m.save(); err != nil {
				return nil, err
			}
			return m, nil
		}
		return nil, newLSMError("corrupt_manifest", "unable to read manifest", err)
	}

	if len(data) > 0 {
		if err := json.Unmarshal(data, &m.state); err != nil {
			return nil, newLSMError("corrupt_manifest", "manifest json unmarshal failed", err)
		}
		if m.state.Levels == nil {
			m.state.Levels = make(map[int][]uint64)
		}
	}
	return m, nil
}

func (m *Manifest) save() error {
	data, err := json.Marshal(m.state)
	if err != nil {
		return err
	}
	tmpPath := fmt.Sprintf("%s/MANIFEST.tmp", m.dir)
	f, err := m.fs.OpenFile(tmpPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o666)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return m.fs.Rename(tmpPath, fmt.Sprintf("%s/MANIFEST", m.dir))
}

// AllocFileID atomically reserves the next file ID.
func (m *Manifest) AllocFileID() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := m.state.NextFileID
	m.state.NextFileID++
	_ = m.save()
	return id
}

// AddSSTable adds a table to level.
func (m *Manifest) AddSSTable(level int, id uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.Levels[level] = append(m.state.Levels[level], id)
	return m.save()
}

// ReplaceLevel replaces tables in a level with new tables.
func (m *Manifest) ReplaceLevel(level int, newTables []uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.Levels[level] = append([]uint64(nil), newTables...)
	return m.save()
}

// GetLevels returns a clone of current levels.
func (m *Manifest) GetLevels() map[int][]uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	res := make(map[int][]uint64)
	for k, v := range m.state.Levels {
		res[k] = append([]uint64(nil), v...)
	}
	return res
}
