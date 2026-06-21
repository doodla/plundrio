package download

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// TestDownloadFileE2ESucceeds drives the real grab download path end-to-end:
// downloadWithRetry → downloadFile → grab fetches bytes from a local httptest
// "CDN" and lands them at <target>/<name>, and the transfer context's
// downloaded-byte accounting reflects the full file.
func TestDownloadFileE2ESucceeds(t *testing.T) {
	content := bytes.Repeat([]byte("plundrio-e2e-"), 1024) // ~13KB

	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		_, _ = w.Write(content)
	}))
	defer cdn.Close()

	dir := t.TempDir()
	fake := &fakePutioClient{downloadURL: cdn.URL + "/movie.mkv"}
	m := newTestManagerWithClientAndTargetDir(fake, dir)

	m.coordinator.InitiateTransfer(1, "Movie", 10, 1)
	if err := m.coordinator.StartDownload(1); err != nil {
		t.Fatalf("StartDownload: %v", err)
	}

	state := &DownloadState{
		FileID:     100,
		Name:       filepath.Join("Movie", "movie.mkv"),
		TransferID: 1,
		StartTime:  time.Now(),
	}
	if err := m.downloadWithRetry(state); err != nil {
		t.Fatalf("downloadWithRetry: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "Movie", "movie.mkv"))
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("downloaded %d bytes, want %d (content mismatch)", len(got), len(content))
	}

	// The flush-on-complete path should have recorded the full size on the ctx.
	ctx, _ := m.coordinator.GetTransferContext(1)
	downloaded, _, _, _ := ctx.GetProgress()
	if downloaded != int64(len(content)) {
		t.Errorf("ctx downloaded = %d, want %d", downloaded, len(content))
	}
}

// TestDownloadFileE2ERetriesTransientThenSucceeds proves downloadWithRetry
// recovers from a transient HTTP 503 on the CDN: the first attempt fails with a
// retryable status, the second succeeds, and the file lands intact.
func TestDownloadFileE2ERetriesTransientThenSucceeds(t *testing.T) {
	content := []byte("recovered after a 503\n")

	var hits int32
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&hits, 1) == 1 {
			http.Error(w, "busy", http.StatusServiceUnavailable) // transient
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		_, _ = w.Write(content)
	}))
	defer cdn.Close()

	dir := t.TempDir()
	fake := &fakePutioClient{downloadURL: cdn.URL + "/ep.mkv"}
	m := newTestManagerWithClientAndTargetDir(fake, dir)
	m.retryBackoffBase = time.Millisecond // keep the retry near-instant

	state := &DownloadState{
		FileID:     100,
		Name:       filepath.Join("Show", "ep.mkv"),
		TransferID: 1,
		StartTime:  time.Now(),
	}
	if err := m.downloadWithRetry(state); err != nil {
		t.Fatalf("downloadWithRetry: %v", err)
	}

	if n := atomic.LoadInt32(&hits); n != 2 {
		t.Errorf("CDN hits = %d, want 2 (one 503, one success)", n)
	}
	got, err := os.ReadFile(filepath.Join(dir, "Show", "ep.mkv"))
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("content mismatch after retry: got %q", got)
	}
}

// TestDownloadFileE2EPermanentErrorDoesNotRetry confirms a non-retryable CDN
// status (404) fails fast without burning the retry budget.
func TestDownloadFileE2EPermanentErrorDoesNotRetry(t *testing.T) {
	var hits int32
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer cdn.Close()

	dir := t.TempDir()
	fake := &fakePutioClient{downloadURL: cdn.URL + "/missing.mkv"}
	m := newTestManagerWithClientAndTargetDir(fake, dir)
	m.retryBackoffBase = time.Millisecond

	state := &DownloadState{FileID: 100, Name: filepath.Join("X", "missing.mkv"), TransferID: 1, StartTime: time.Now()}
	if err := m.downloadWithRetry(state); err == nil {
		t.Fatal("expected error for 404 download, got nil")
	}
	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Errorf("CDN hits = %d, want 1 (404 is not retried)", n)
	}
}
