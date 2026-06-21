package atomicfile

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
)

func TestWriteFileWritesContentWithPerm(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	if err := WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !bytes.Equal(got, []byte("hello")) {
		t.Errorf("content = %q, want hello", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("perm = %o, want 644", info.Mode().Perm())
	}
}

func TestWriteFileReplacesAtomicallyLeavingNoTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	if err := WriteFile(path, []byte("first"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := WriteFile(path, []byte("second"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, []byte("second")) {
		t.Errorf("content = %q, want second", got)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "state.json" {
			t.Errorf("leftover temp file: %q", e.Name())
		}
	}
}

func TestWriteFileConcurrentWritersDoNotCorrupt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Each writer writes a complete, valid payload; with unique temp
			// names + atomic rename the final file must be exactly one of them,
			// never a mix.
			_ = WriteFile(path, []byte("payload-"+strconv.Itoa(i)), 0o644)
		}(i)
	}
	wg.Wait()

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !bytes.HasPrefix(got, []byte("payload-")) {
		t.Errorf("final file is not a complete payload: %q", got)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("expected only the target file, got %d entries", len(entries))
	}
}

func TestWriteFileErrorsOnUnwritableDir(t *testing.T) {
	// A non-existent parent directory makes os.CreateTemp fail, surfaced as an
	// error rather than a panic.
	if err := WriteFile(filepath.Join(t.TempDir(), "nope", "state.json"), []byte("x"), 0o644); err == nil {
		t.Error("expected error writing into a missing directory, got nil")
	}
}
