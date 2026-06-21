package server

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/doodla/go-putio"
	"github.com/doodla/plundrio/internal/api"
	"github.com/doodla/plundrio/internal/config"
	"github.com/doodla/plundrio/internal/download"
)

// e2eClient is a put.io fake satisfying BOTH server.PutioClient and
// download.PutioClient, so an end-to-end test can drive the real download.Manager
// and the real RPC Server together with no network. GetDownloadURL points at a
// local httptest "CDN" so grab performs a genuine HTTP download to disk.
type e2eClient struct {
	mu          sync.Mutex
	transfers   map[int64]*putio.Transfer
	files       map[int64][]api.TransferFile
	downloadURL string
	account     *putio.AccountInfo

	deleteFileCalls     []int64
	deleteTransferCalls []int64
}

func (c *e2eClient) GetAccountInfo(context.Context) (*putio.AccountInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.account != nil {
		return c.account, nil
	}
	return &putio.AccountInfo{Username: "e2e"}, nil
}

func (c *e2eClient) GetTransfers(context.Context) ([]*putio.Transfer, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]*putio.Transfer, 0, len(c.transfers))
	for _, t := range c.transfers {
		out = append(out, t)
	}
	return out, nil
}

func (c *e2eClient) UploadFile(context.Context, []byte, string, int64) (string, error) {
	return "uploaded", nil
}

func (c *e2eClient) AddTransfer(context.Context, string, int64) (string, error) {
	return "magnet", nil
}

func (c *e2eClient) DeleteFile(_ context.Context, fileID int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deleteFileCalls = append(c.deleteFileCalls, fileID)
	return nil
}

func (c *e2eClient) DeleteTransfer(_ context.Context, transferID int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deleteTransferCalls = append(c.deleteTransferCalls, transferID)
	delete(c.transfers, transferID) // simulate put.io removing the transfer
	return nil
}

func (c *e2eClient) GetAllTransferFiles(_ context.Context, fileID int64) ([]api.TransferFile, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.files[fileID], nil
}

func (c *e2eClient) RetryTransfer(context.Context, int64) (*putio.Transfer, error) {
	return nil, nil
}

func (c *e2eClient) GetDownloadURL(context.Context, int64) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.downloadURL, nil
}

// waitFor polls cond until it returns true or the deadline elapses.
func waitFor(t *testing.T, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", d)
}

// TestE2ECompletedTransferDownloadsAndCleansUp exercises the full happy path
// with the real Manager + Server wired together:
//
//	put.io reports a COMPLETED transfer → Manager downloads its file (real grab
//	HTTP fetch) into the category/transfer subdir → torrent-get reports it done →
//	torrent-remove purges the put.io side and clears the category mapping.
func TestE2ECompletedTransferDownloadsAndCleansUp(t *testing.T) {
	content := []byte("plundrio end-to-end payload: the quick brown fox jumps\n")

	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content)
	}))
	defer cdn.Close()

	const folderID int64 = 42
	fake := &e2eClient{
		transfers: map[int64]*putio.Transfer{
			1: {
				ID: 1, Hash: "abc", Name: "Movie.2024", FileID: 10,
				SaveParentID: folderID, Status: "COMPLETED", PercentDone: 100,
				Size: len(content),
			},
		},
		files: map[int64][]api.TransferFile{
			10: {{
				File:    &putio.File{ID: 100, Name: "movie.mkv", Size: int64(len(content))},
				RelPath: "movie.mkv",
			}},
		},
		downloadURL: cdn.URL + "/movie.mkv",
	}

	dir := t.TempDir()
	cfg := &config.Config{
		TargetDir:             dir,
		FolderID:              folderID,
		WorkerCount:           2,
		TransferCheckInterval: 20 * time.Millisecond, // poll fast for a snappy test
	}

	mgr := download.New(cfg, fake)
	mgr.Start()
	defer mgr.Stop()

	// The file must land on disk under <target>/<transfer name>/<relpath> with
	// the exact bytes the CDN served.
	target := filepath.Join(dir, "Movie.2024", "movie.mkv")
	waitFor(t, 5*time.Second, func() bool {
		got, err := os.ReadFile(target)
		return err == nil && bytes.Equal(got, content)
	})

	srv := New(cfg, fake, mgr)

	// torrent-get must report the transfer (the put.io snapshot drives it).
	req := rpcRequest(t, "torrent-get", map[string]interface{}{"fields": []string{"id", "percentDone", "hashString"}})
	w := httptest.NewRecorder()
	srv.handleRPC(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("torrent-get status = %d", w.Code)
	}
	resp := decodeRPCResponse(t, w.Body.Bytes())
	torrents, _ := resp.Arguments["torrents"].([]interface{})
	if len(torrents) != 1 {
		t.Fatalf("torrent-get returned %d torrents, want 1", len(torrents))
	}
	got := torrents[0].(map[string]interface{})
	if got["hashString"] != "abc" {
		t.Errorf("hashString = %v, want abc", got["hashString"])
	}
	// Local download finished, so combined progress is 100% (1.0).
	if pd, _ := got["percentDone"].(float64); pd != 1.0 {
		t.Errorf("percentDone = %v, want 1.0", pd)
	}

	// torrent-remove must purge the put.io side (DeleteFile + DeleteTransfer).
	rmReq := rpcRequest(t, "torrent-remove", map[string]interface{}{
		"ids":               []string{"abc"},
		"delete-local-data": false,
	})
	rmW := httptest.NewRecorder()
	srv.handleRPC(rmW, rmReq)
	if rmW.Code != http.StatusOK {
		t.Fatalf("torrent-remove status = %d", rmW.Code)
	}

	fake.mu.Lock()
	delFiles := append([]int64(nil), fake.deleteFileCalls...)
	delTransfers := append([]int64(nil), fake.deleteTransferCalls...)
	fake.mu.Unlock()

	if !contains(delFiles, 10) {
		t.Errorf("DeleteFile calls = %v, want to include file 10", delFiles)
	}
	if !contains(delTransfers, 1) {
		t.Errorf("DeleteTransfer calls = %v, want to include transfer 1", delTransfers)
	}

	// The downloaded data is left on disk (delete-local-data was false).
	if _, err := os.Stat(target); err != nil {
		t.Errorf("local file should remain after remove without delete-local-data: %v", err)
	}
}

// TestE2EDeleteLocalDataRemovesDownloadedFile is the e2e variant where the
// client asks to also delete local data: the downloaded tree must be gone.
func TestE2EDeleteLocalDataRemovesDownloadedFile(t *testing.T) {
	content := []byte("delete-me payload\n")

	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		_, _ = w.Write(content)
	}))
	defer cdn.Close()

	const folderID int64 = 7
	fake := &e2eClient{
		transfers: map[int64]*putio.Transfer{
			1: {ID: 1, Hash: "h", Name: "Show.S01E01", FileID: 10, SaveParentID: folderID, Status: "SEEDING", PercentDone: 100, Size: len(content)},
		},
		files: map[int64][]api.TransferFile{
			10: {{File: &putio.File{ID: 100, Name: "ep.mkv", Size: int64(len(content))}, RelPath: "ep.mkv"}},
		},
		downloadURL: cdn.URL + "/ep.mkv",
	}

	dir := t.TempDir()
	cfg := &config.Config{TargetDir: dir, FolderID: folderID, WorkerCount: 1, TransferCheckInterval: 20 * time.Millisecond}
	mgr := download.New(cfg, fake)
	mgr.Start()
	defer mgr.Stop()

	transferDir := filepath.Join(dir, "Show.S01E01")
	target := filepath.Join(transferDir, "ep.mkv")
	waitFor(t, 5*time.Second, func() bool {
		got, err := os.ReadFile(target)
		return err == nil && bytes.Equal(got, content)
	})

	srv := New(cfg, fake, mgr)
	rmReq := rpcRequest(t, "torrent-remove", map[string]interface{}{
		"ids":               []string{"h"},
		"delete-local-data": true,
	})
	rmW := httptest.NewRecorder()
	srv.handleRPC(rmW, rmReq)
	if rmW.Code != http.StatusOK {
		t.Fatalf("torrent-remove status = %d", rmW.Code)
	}

	if _, err := os.Stat(transferDir); !os.IsNotExist(err) {
		t.Errorf("transfer dir should be deleted with delete-local-data, stat err = %v", err)
	}
}

func contains(xs []int64, x int64) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
