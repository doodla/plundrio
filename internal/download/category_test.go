package download

import (
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
)

func TestCategoryStore_SetGetRemove(t *testing.T) {
	dir := t.TempDir()
	cs := newCategoryStore(dir)

	// Get on empty store returns ""
	if got := cs.Get("abc123"); got != "" {
		t.Errorf("Get on empty store = %q, want %q", got, "")
	}

	// Set and Get
	cs.Set("abc123", "tv")
	if got := cs.Get("abc123"); got != "tv" {
		t.Errorf("Get after Set = %q, want %q", got, "tv")
	}

	// Overwrite
	cs.Set("abc123", "movies")
	if got := cs.Get("abc123"); got != "movies" {
		t.Errorf("Get after overwrite = %q, want %q", got, "movies")
	}

	// Remove
	cs.Remove("abc123")
	if got := cs.Get("abc123"); got != "" {
		t.Errorf("Get after Remove = %q, want %q", got, "")
	}
}

func TestCategoryStore_SetIgnoresEmpty(t *testing.T) {
	dir := t.TempDir()
	cs := newCategoryStore(dir)

	cs.Set("", "tv")
	cs.Set("abc", "")

	// State file should not exist since nothing was persisted
	if _, err := os.Stat(filepath.Join(dir, stateFileName)); !os.IsNotExist(err) {
		t.Error("expected no state file for empty hash/category Set calls")
	}
}

func TestCategoryStore_Persistence(t *testing.T) {
	dir := t.TempDir()

	// Create and populate a store
	cs1 := newCategoryStore(dir)
	cs1.Set("hash1", "tv")
	cs1.Set("hash2", "movies")

	// Create a new store pointing at the same dir and load
	cs2 := newCategoryStore(dir)
	cs2.Load()

	if got := cs2.Get("hash1"); got != "tv" {
		t.Errorf("After reload Get(hash1) = %q, want %q", got, "tv")
	}
	if got := cs2.Get("hash2"); got != "movies" {
		t.Errorf("After reload Get(hash2) = %q, want %q", got, "movies")
	}
}

func TestCategoryStore_LoadMissingFile(t *testing.T) {
	dir := t.TempDir()
	cs := newCategoryStore(dir)

	// Load on missing file should not panic or error
	cs.Load()

	if got := cs.Get("anything"); got != "" {
		t.Errorf("Get after Load of missing file = %q, want %q", got, "")
	}
}

func TestCategoryStore_SaveLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	cs := newCategoryStore(dir)

	cs.Set("hash1", "tv")
	cs.Set("hash2", "movies")
	cs.Remove("hash1")

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	// Only the state file should remain — the atomic temp file is renamed away.
	for _, e := range entries {
		if e.Name() != stateFileName {
			t.Errorf("unexpected leftover file in state dir: %q", e.Name())
		}
	}
}

func TestCategoryStore_ConcurrentSavesPersistConsistently(t *testing.T) {
	dir := t.TempDir()
	cs := newCategoryStore(dir)

	// Hammer Set from many goroutines. With atomic temp+rename and saveMu, the
	// on-disk file must always be complete, parseable JSON — never a truncated
	// half-write. The final reload must see every key.
	const n = 50
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cs.Set("hash"+strconv.Itoa(i), "cat"+strconv.Itoa(i))
		}(i)
	}
	wg.Wait()

	cs2 := newCategoryStore(dir)
	cs2.Load()
	for i := 0; i < n; i++ {
		want := "cat" + strconv.Itoa(i)
		if got := cs2.Get("hash" + strconv.Itoa(i)); got != want {
			t.Errorf("after concurrent saves Get(hash%d) = %q, want %q", i, got, want)
		}
	}
}

func TestCategoryStore_RemovePersists(t *testing.T) {
	dir := t.TempDir()

	cs1 := newCategoryStore(dir)
	cs1.Set("hash1", "tv")
	cs1.Set("hash2", "movies")
	cs1.Remove("hash1")

	cs2 := newCategoryStore(dir)
	cs2.Load()

	if got := cs2.Get("hash1"); got != "" {
		t.Errorf("After reload removed hash1 = %q, want %q", got, "")
	}
	if got := cs2.Get("hash2"); got != "movies" {
		t.Errorf("After reload hash2 = %q, want %q", got, "movies")
	}
}
