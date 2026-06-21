package download

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/doodla/plundrio/internal/log"
)

const stateFileName = ".plundrio-state.json"

// CategoryStore persists a hash → category mapping so that downloads land in
// the correct sub-directory (e.g. "tv", "movies") even across restarts.
type CategoryStore struct {
	mu        sync.RWMutex
	mapping   map[string]string
	stateFile string

	// saveMu serializes save() so two concurrent Set/Remove calls can't
	// interleave their temp-file writes or rename a stale snapshot over a fresh
	// one. Separate from mu so a save never blocks a Get.
	saveMu sync.Mutex
}

func newCategoryStore(targetDir string) *CategoryStore {
	return &CategoryStore{
		mapping:   make(map[string]string),
		stateFile: filepath.Join(targetDir, stateFileName),
	}
}

// Load reads persisted categories from disk. A missing file is not an error.
func (cs *CategoryStore) Load() {
	data, err := os.ReadFile(cs.stateFile)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Error("categories").Err(err).Msg("Failed to load category state")
		}
		return
	}

	cs.mu.Lock()
	defer cs.mu.Unlock()

	if err := json.Unmarshal(data, &cs.mapping); err != nil {
		log.Error("categories").Err(err).Msg("Failed to parse category state")
	}
}

// Set stores a category for the given hash and persists to disk.
func (cs *CategoryStore) Set(hash, category string) {
	if hash == "" || category == "" {
		return
	}

	cs.mu.Lock()
	cs.mapping[hash] = category
	cs.mu.Unlock()

	cs.save()
}

// Get returns the category for a hash, or "" if none is stored.
func (cs *CategoryStore) Get(hash string) string {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.mapping[hash]
}

// Remove deletes the category for a hash and persists to disk.
func (cs *CategoryStore) Remove(hash string) {
	cs.mu.Lock()
	delete(cs.mapping, hash)
	cs.mu.Unlock()

	cs.save()
}

// save persists the mapping atomically: marshal a consistent snapshot, write it
// to a temp file in the same directory, then rename over the state file. The
// rename is atomic on POSIX, so a crash or full disk mid-write leaves either the
// old complete file or the new one — never a truncated file that would lose
// every category mapping on the next Load (sending all downloads to the flat
// target dir). saveMu serializes concurrent saves so the last writer wins.
func (cs *CategoryStore) save() {
	cs.saveMu.Lock()
	defer cs.saveMu.Unlock()

	cs.mu.RLock()
	data, err := json.Marshal(cs.mapping)
	cs.mu.RUnlock()

	if err != nil {
		log.Error("categories").Err(err).Msg("Failed to marshal category state")
		return
	}

	dir := filepath.Dir(cs.stateFile)
	tmp, err := os.CreateTemp(dir, stateFileName+".tmp-*")
	if err != nil {
		log.Error("categories").Err(err).Msg("Failed to create temp category state file")
		return
	}
	tmpName := tmp.Name()
	// Best-effort cleanup if we bail before the rename; after a successful
	// rename the temp path no longer exists and Remove is a harmless no-op.
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		log.Error("categories").Err(err).Msg("Failed to write temp category state")
		return
	}
	if err := tmp.Close(); err != nil {
		log.Error("categories").Err(err).Msg("Failed to close temp category state")
		return
	}
	// CreateTemp makes the file 0600; match the previous 0644 so the state stays
	// world-readable as before.
	if err := os.Chmod(tmpName, 0644); err != nil {
		log.Error("categories").Err(err).Msg("Failed to chmod temp category state")
		return
	}
	if err := os.Rename(tmpName, cs.stateFile); err != nil {
		log.Error("categories").Err(err).Msg("Failed to save category state")
	}
}
