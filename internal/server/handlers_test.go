package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/doodla/go-putio"
	"github.com/doodla/plundrio/internal/config"
	"github.com/doodla/plundrio/internal/download"
)

func writeStubDir(t *testing.T, dir string) error {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "file.mkv"), []byte("data"), 0644)
}

func statPath(p string) (os.FileInfo, error) { return os.Stat(p) }

// fakePutioClient records calls and returns canned values, mirroring the
// shape of internal/download/cleanup_test.go's fake but for the smaller
// server-package PutioClient interface (UploadFile + AddTransfer instead of
// the download client's Retry/GetAllTransferFiles).
type fakePutioClient struct {
	mu sync.Mutex

	uploadFileCalls     []uploadFileCall
	addTransferCalls    []addTransferCall
	deleteFileCalls     []int64
	getTransfersCalls   int
	deleteTransferCalls []int64

	transfers []*putio.Transfer

	uploadFileErr     error
	addTransferErr    error
	deleteFileErr     error
	deleteTransferErr error
	getTransfersErr   error

	// accountInfo / getAccountInfoErr drive GetAccountInfo; accountInfoCalls
	// counts invocations so tests can assert the 60s cache collapses bursts.
	accountInfo       *putio.AccountInfo
	getAccountInfoErr error
	accountInfoCalls  int
}

type uploadFileCall struct {
	data     []byte
	filename string
	folderID int64
}

type addTransferCall struct {
	magnetLink string
	folderID   int64
}

func (f *fakePutioClient) GetAccountInfo(ctx context.Context) (*putio.AccountInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.accountInfoCalls++
	if f.getAccountInfoErr != nil {
		return nil, f.getAccountInfoErr
	}
	if f.accountInfo != nil {
		return f.accountInfo, nil
	}
	return &putio.AccountInfo{Username: "test"}, nil
}

func (f *fakePutioClient) GetTransfers(ctx context.Context) ([]*putio.Transfer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getTransfersCalls++
	return f.transfers, f.getTransfersErr
}

func (f *fakePutioClient) UploadFile(ctx context.Context, data []byte, filename string, folderID int64) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.uploadFileCalls = append(f.uploadFileCalls, uploadFileCall{data: data, filename: filename, folderID: folderID})
	return "uploaded-hash-" + filename, f.uploadFileErr
}

func (f *fakePutioClient) AddTransfer(ctx context.Context, magnetLink string, folderID int64) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.addTransferCalls = append(f.addTransferCalls, addTransferCall{magnetLink: magnetLink, folderID: folderID})
	return "magnet-hash", f.addTransferErr
}

func (f *fakePutioClient) DeleteFile(ctx context.Context, fileID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleteFileCalls = append(f.deleteFileCalls, fileID)
	return f.deleteFileErr
}

func (f *fakePutioClient) DeleteTransfer(ctx context.Context, transferID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleteTransferCalls = append(f.deleteTransferCalls, transferID)
	return f.deleteTransferErr
}

// fakeDownloadService implements server.DownloadService — six methods plus
// Stop, all backed by an in-memory map. The server package tests don't need
// the real download.Manager wiring, just the surface the handlers touch.
type fakeDownloadService struct {
	mu                 sync.Mutex
	transfers          []*putio.Transfer
	categories         map[string]string
	transferCtxs       map[int64]*download.TransferContext
	stopCalls          int
	removedHashes      []string
	setCategoryFns     []struct{ hash, category string }
	purgeTransferCalls []int64
	purgeTransferErr   error
}

func newFakeDownloadService() *fakeDownloadService {
	return &fakeDownloadService{
		categories:   map[string]string{},
		transferCtxs: map[int64]*download.TransferContext{},
	}
}

func (f *fakeDownloadService) GetTransfers() []*putio.Transfer {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.transfers
}

func (f *fakeDownloadService) GetTransferContext(transferID int64) (*download.TransferContext, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ctx, ok := f.transferCtxs[transferID]
	return ctx, ok
}

func (f *fakeDownloadService) SetCategory(hash, category string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.categories[hash] = category
	f.setCategoryFns = append(f.setCategoryFns, struct{ hash, category string }{hash, category})
}

func (f *fakeDownloadService) GetCategory(hash string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.categories[hash]
}

func (f *fakeDownloadService) RemoveCategory(hash string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.categories, hash)
	f.removedHashes = append(f.removedHashes, hash)
}

func (f *fakeDownloadService) PurgeTransfer(transferID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.purgeTransferCalls = append(f.purgeTransferCalls, transferID)
	return f.purgeTransferErr
}

func (f *fakeDownloadService) Stop() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopCalls++
}

// newTestServer builds a Server with the fakes, ready for httptest.
func newTestServer(t *testing.T, client *fakePutioClient, dl *fakeDownloadService) *Server {
	t.Helper()
	cfg := &config.Config{
		TargetDir:  t.TempDir(),
		FolderID:   42,
		ListenAddr: ":0",
	}
	return New(cfg, client, dl)
}

// rpcRequest builds a Transmission-RPC POST body for the given method and
// arguments, plus a session-id header so handleRPC doesn't 409-challenge.
func rpcRequest(t *testing.T, method string, args interface{}) *http.Request {
	t.Helper()
	body, err := json.Marshal(map[string]interface{}{
		"method":    method,
		"arguments": args,
		"tag":       7,
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/transmission/rpc", bytes.NewReader(body))
	req.Header.Set("X-Transmission-Session-Id", "123")
	return req
}

// decodeRPCResponse extracts the standard transmission-rpc envelope.
type rpcResponse struct {
	Tag       interface{}            `json:"tag,omitempty"`
	Result    string                 `json:"result"`
	Arguments map[string]interface{} `json:"arguments"`
}

func decodeRPCResponse(t *testing.T, body []byte) rpcResponse {
	t.Helper()
	var resp rpcResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode response: %v\nbody: %s", err, string(body))
	}
	return resp
}

func TestHandleRPCSendsSessionIDChallenge(t *testing.T) {
	srv := newTestServer(t, &fakePutioClient{}, newFakeDownloadService())

	req := httptest.NewRequest(http.MethodPost, "/transmission/rpc", strings.NewReader("{}"))
	// No X-Transmission-Session-Id header.
	w := httptest.NewRecorder()
	srv.handleRPC(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected 409 challenge, got %d", w.Code)
	}
	if got := w.Header().Get("X-Transmission-Session-Id"); got == "" {
		t.Errorf("expected session-id header on challenge, got empty")
	}
}

func TestHandleRPCGetIsTreatedAsSessionGet(t *testing.T) {
	srv := newTestServer(t, &fakePutioClient{}, newFakeDownloadService())

	req := httptest.NewRequest(http.MethodGet, "/transmission/rpc", nil)
	req.Header.Set("X-Transmission-Session-Id", "123")
	w := httptest.NewRecorder()
	srv.handleRPC(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", w.Code, w.Body.String())
	}
	resp := decodeRPCResponse(t, w.Body.Bytes())
	if resp.Result != "success" {
		t.Errorf("result = %q, want success", resp.Result)
	}
	// session-get must surface download-dir.
	if dd, _ := resp.Arguments["download-dir"].(string); dd != srv.cfg.TargetDir {
		t.Errorf("download-dir = %q, want %q", dd, srv.cfg.TargetDir)
	}
}

func TestHandleRPCRejectsMalformedPostBody(t *testing.T) {
	srv := newTestServer(t, &fakePutioClient{}, newFakeDownloadService())

	req := httptest.NewRequest(http.MethodPost, "/transmission/rpc", strings.NewReader("{not json"))
	req.Header.Set("X-Transmission-Session-Id", "123")
	w := httptest.NewRecorder()
	srv.handleRPC(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleRPCRejectsUnsupportedHTTPMethod(t *testing.T) {
	srv := newTestServer(t, &fakePutioClient{}, newFakeDownloadService())

	req := httptest.NewRequest(http.MethodPut, "/transmission/rpc", nil)
	req.Header.Set("X-Transmission-Session-Id", "123")
	w := httptest.NewRecorder()
	srv.handleRPC(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestHandleRPCUnknownMethodReturnsEmptySuccess(t *testing.T) {
	srv := newTestServer(t, &fakePutioClient{}, newFakeDownloadService())

	req := rpcRequest(t, "totally-unknown-method", map[string]interface{}{})
	w := httptest.NewRecorder()
	srv.handleRPC(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with empty success for unknown method, got %d, body=%s", w.Code, w.Body.String())
	}
	resp := decodeRPCResponse(t, w.Body.Bytes())
	if resp.Result != "success" {
		t.Errorf("result = %q, want success", resp.Result)
	}
}

func TestHandleTorrentAddViaMetainfo(t *testing.T) {
	client := &fakePutioClient{}
	dl := newFakeDownloadService()
	srv := newTestServer(t, client, dl)

	torrentBytes := []byte("d8:announce15:http://example.come")
	req := rpcRequest(t, "torrent-add", map[string]interface{}{
		"filename":    "ubuntu.torrent",
		"metainfo":    base64.StdEncoding.EncodeToString(torrentBytes),
		"downloadDir": "", // no category
	})
	w := httptest.NewRecorder()
	srv.handleRPC(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", w.Code, w.Body.String())
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.uploadFileCalls) != 1 {
		t.Fatalf("UploadFile calls = %d, want 1", len(client.uploadFileCalls))
	}
	got := client.uploadFileCalls[0]
	if got.filename != "ubuntu.torrent" {
		t.Errorf("filename = %q, want ubuntu.torrent", got.filename)
	}
	if got.folderID != 42 {
		t.Errorf("folderID = %d, want 42", got.folderID)
	}
	if !bytes.Equal(got.data, torrentBytes) {
		t.Errorf("uploaded bytes mismatch: got %q, want %q", got.data, torrentBytes)
	}
}

func TestHandleTorrentAddViaMagnetSetsCategory(t *testing.T) {
	client := &fakePutioClient{}
	dl := newFakeDownloadService()
	srv := newTestServer(t, client, dl)

	// downloadDir below targetDir produces "tv" as the category.
	downloadDir := srv.cfg.TargetDir + "/tv"
	req := rpcRequest(t, "torrent-add", map[string]interface{}{
		"magnetLink":  "magnet:?xt=urn:btih:abc",
		"downloadDir": downloadDir,
	})
	w := httptest.NewRecorder()
	srv.handleRPC(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", w.Code, w.Body.String())
	}
	client.mu.Lock()
	if len(client.addTransferCalls) != 1 {
		t.Fatalf("AddTransfer calls = %d, want 1", len(client.addTransferCalls))
	}
	if got := client.addTransferCalls[0].magnetLink; got != "magnet:?xt=urn:btih:abc" {
		t.Errorf("magnetLink = %q, want magnet:?xt=urn:btih:abc", got)
	}
	client.mu.Unlock()

	// Category from extractCategory was stored against the upstream-returned hash.
	if got := dl.GetCategory("magnet-hash"); got != "tv" {
		t.Errorf("category for magnet-hash = %q, want tv", got)
	}
}

func TestHandleTorrentAddRejectsRequestWithoutTorrentOrMagnet(t *testing.T) {
	client := &fakePutioClient{}
	dl := newFakeDownloadService()
	srv := newTestServer(t, client, dl)

	req := rpcRequest(t, "torrent-add", map[string]interface{}{
		"downloadDir": "",
	})
	w := httptest.NewRecorder()
	srv.handleRPC(w, req)

	// handlers package surfaces this as an error envelope, not an HTTP error.
	resp := decodeRPCResponse(t, w.Body.Bytes())
	if resp.Result == "success" {
		t.Errorf("expected error result, got success")
	}
}

func TestHandleTorrentGetReturnsEmptyListWhenNoTransfers(t *testing.T) {
	srv := newTestServer(t, &fakePutioClient{}, newFakeDownloadService())

	req := rpcRequest(t, "torrent-get", map[string]interface{}{})
	w := httptest.NewRecorder()
	srv.handleRPC(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	resp := decodeRPCResponse(t, w.Body.Bytes())
	torrents, ok := resp.Arguments["torrents"].([]interface{})
	if !ok {
		t.Fatalf("torrents field missing or wrong type: %#v", resp.Arguments)
	}
	if len(torrents) != 0 {
		t.Errorf("expected empty torrents list, got %d", len(torrents))
	}
}

func TestHandleTorrentGetReturnsTransferWithoutContext(t *testing.T) {
	dl := newFakeDownloadService()
	dl.transfers = []*putio.Transfer{
		{ID: 100, Hash: "abc", Name: "movie.mkv", Status: "DOWNLOADING", Size: 1024, PercentDone: 50},
	}
	srv := newTestServer(t, &fakePutioClient{}, dl)

	req := rpcRequest(t, "torrent-get", map[string]interface{}{})
	w := httptest.NewRecorder()
	srv.handleRPC(w, req)

	resp := decodeRPCResponse(t, w.Body.Bytes())
	torrents, _ := resp.Arguments["torrents"].([]interface{})
	if len(torrents) != 1 {
		t.Fatalf("expected 1 torrent, got %d", len(torrents))
	}
	tr := torrents[0].(map[string]interface{})
	if tr["hashString"] != "abc" {
		t.Errorf("hashString = %v, want abc", tr["hashString"])
	}
	if tr["name"] != "movie.mkv" {
		t.Errorf("name = %v, want movie.mkv", tr["name"])
	}
}

func TestHandleTorrentGetFiltersByID(t *testing.T) {
	dl := newFakeDownloadService()
	dl.transfers = []*putio.Transfer{
		{ID: 1, Hash: "wanted", Name: "a"},
		{ID: 2, Hash: "skipped", Name: "b"},
	}
	srv := newTestServer(t, &fakePutioClient{}, dl)

	req := rpcRequest(t, "torrent-get", map[string]interface{}{
		"ids": []string{"wanted"},
	})
	w := httptest.NewRecorder()
	srv.handleRPC(w, req)

	resp := decodeRPCResponse(t, w.Body.Bytes())
	torrents, _ := resp.Arguments["torrents"].([]interface{})
	if len(torrents) != 1 {
		t.Fatalf("expected 1 torrent (filtered), got %d", len(torrents))
	}
	if h := torrents[0].(map[string]interface{})["hashString"]; h != "wanted" {
		t.Errorf("hashString = %v, want wanted", h)
	}
}

// TestHandleTorrentGetSurfacesPermanentFailure verifies that when plundrio's
// retry cascade has given up on a transfer, the torrent-get response carries
// error=true so *arr's Failed Download Handling can drop the grab. Without
// this surface, Sonarr/Radarr wait forever for transfers plundrio has already
// abandoned — the operator only finds out by spotting missing episodes.
func TestHandleTorrentGetSurfacesPermanentFailure(t *testing.T) {
	dl := newFakeDownloadService()
	dl.transfers = []*putio.Transfer{
		{ID: 100, Hash: "abc", Name: "movie.mkv", Status: "COMPLETED", Size: 1024, PercentDone: 100},
	}
	// Build a real TransferContext, mark it permanently failed via the
	// coordinator (the only path that sets the flag).
	tc := download.NewTransferCoordinator(func(int64) {})
	ctx := tc.InitiateTransfer(100, "movie.mkv", 999, 1)
	ctx.SetTotalSize(1024)
	if err := tc.MarkPermanentlyFailed(100, errors.New("retries exhausted post put.io fallback")); err != nil {
		t.Fatalf("MarkPermanentlyFailed: %v", err)
	}
	dl.transferCtxs[100] = ctx

	srv := newTestServer(t, &fakePutioClient{}, dl)
	req := rpcRequest(t, "torrent-get", map[string]interface{}{})
	w := httptest.NewRecorder()
	srv.handleRPC(w, req)

	resp := decodeRPCResponse(t, w.Body.Bytes())
	torrents, _ := resp.Arguments["torrents"].([]interface{})
	if len(torrents) != 1 {
		t.Fatalf("expected 1 torrent, got %d", len(torrents))
	}
	tr := torrents[0].(map[string]interface{})
	if got, _ := tr["error"].(bool); !got {
		t.Errorf("error = %v, want true so *arr's failed-download handling fires", tr["error"])
	}
	if got, _ := tr["errorString"].(string); got == "" {
		t.Errorf("errorString = %q, want non-empty", got)
	}
}

func TestHandleTorrentRemoveDelegatesToPurgeTransfer(t *testing.T) {
	// handleTorrentRemove no longer calls DeleteFile/DeleteTransfer
	// directly; it routes the put.io-side delete through
	// dlService.PurgeTransfer so the same code path runs whether removal
	// is driven by an *arr client or by the retention janitor. The
	// FileID==0 cascade-protection and the put.io 404 tolerance now live
	// inside PurgeTransfer (covered by download package tests).
	client := &fakePutioClient{
		transfers: []*putio.Transfer{
			{ID: 100, Hash: "abc", FileID: 999, Name: "movie"},
		},
	}
	dl := newFakeDownloadService()
	dl.SetCategory("abc", "tv")
	srv := newTestServer(t, client, dl)

	req := rpcRequest(t, "torrent-remove", map[string]interface{}{
		"ids":               []string{"abc"},
		"delete-local-data": false,
	})
	w := httptest.NewRecorder()
	srv.handleRPC(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	dl.mu.Lock()
	purgeCalls := append([]int64(nil), dl.purgeTransferCalls...)
	dl.mu.Unlock()
	if len(purgeCalls) != 1 || purgeCalls[0] != 100 {
		t.Errorf("PurgeTransfer calls = %v, want [100]", purgeCalls)
	}
	// Category mapping should be cleaned up via dlService.RemoveCategory.
	if got := dl.GetCategory("abc"); got != "" {
		t.Errorf("category for abc after remove = %q, want empty", got)
	}
}

func TestHandleTorrentRemoveBatchFetchesTransfersOnce(t *testing.T) {
	// torrent-remove accepts an array of ids. Resolving each hash used to
	// trigger a full GetTransfers round trip per id (an N+1 against put.io);
	// the list is now fetched once and indexed by hash. Three ids => one call.
	client := &fakePutioClient{
		transfers: []*putio.Transfer{
			{ID: 100, Hash: "a", FileID: 901, Name: "one"},
			{ID: 200, Hash: "b", FileID: 902, Name: "two"},
			{ID: 300, Hash: "c", FileID: 903, Name: "three"},
		},
	}
	dl := newFakeDownloadService()
	srv := newTestServer(t, client, dl)

	req := rpcRequest(t, "torrent-remove", map[string]interface{}{
		"ids":               []string{"a", "b", "c"},
		"delete-local-data": false,
	})
	w := httptest.NewRecorder()
	srv.handleRPC(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	client.mu.Lock()
	calls := client.getTransfersCalls
	client.mu.Unlock()
	if calls != 1 {
		t.Errorf("GetTransfers calls = %d, want 1 (one fetch for the whole batch)", calls)
	}
	dl.mu.Lock()
	purgeCalls := append([]int64(nil), dl.purgeTransferCalls...)
	dl.mu.Unlock()
	if len(purgeCalls) != 3 {
		t.Errorf("PurgeTransfer calls = %v, want 3 purges", purgeCalls)
	}
}

func TestHandleTorrentRemovePartialBatchSkipsUnknown(t *testing.T) {
	// A batch mixing a known hash with one that isn't in the transfer list must
	// purge only the known transfer, skip the missing one, and still report
	// success — all from a single GetTransfers fetch.
	client := &fakePutioClient{
		transfers: []*putio.Transfer{
			{ID: 100, Hash: "known", FileID: 9, Name: "x"},
		},
	}
	dl := newFakeDownloadService()
	dl.SetCategory("known", "tv")
	srv := newTestServer(t, client, dl)

	req := rpcRequest(t, "torrent-remove", map[string]interface{}{
		"ids":               []string{"known", "missing"},
		"delete-local-data": false,
	})
	w := httptest.NewRecorder()
	srv.handleRPC(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	resp := decodeRPCResponse(t, w.Body.Bytes())
	if resp.Result != "success" {
		t.Errorf("result = %q, want success", resp.Result)
	}
	dl.mu.Lock()
	purgeCalls := append([]int64(nil), dl.purgeTransferCalls...)
	dl.mu.Unlock()
	if len(purgeCalls) != 1 || purgeCalls[0] != 100 {
		t.Errorf("PurgeTransfer calls = %v, want [100] (missing hash skipped)", purgeCalls)
	}
	client.mu.Lock()
	calls := client.getTransfersCalls
	client.mu.Unlock()
	if calls != 1 {
		t.Errorf("GetTransfers calls = %d, want 1 for the whole batch", calls)
	}
	// The known transfer's category mapping is cleaned up.
	if got := dl.GetCategory("known"); got != "" {
		t.Errorf("category for known after remove = %q, want empty", got)
	}
}

func TestHandleTorrentRemoveSurfacesListError(t *testing.T) {
	// When the transfer-list fetch fails, the handler must report an error
	// rather than silently returning success with nothing removed.
	client := &fakePutioClient{getTransfersErr: errors.New("boom")}
	dl := newFakeDownloadService()
	srv := newTestServer(t, client, dl)

	req := rpcRequest(t, "torrent-remove", map[string]interface{}{
		"ids":               []string{"a"},
		"delete-local-data": false,
	})
	w := httptest.NewRecorder()
	srv.handleRPC(w, req)

	resp := decodeRPCResponse(t, w.Body.Bytes())
	if resp.Result == "success" {
		t.Errorf("result = %q, want a non-success error result", resp.Result)
	}
	dl.mu.Lock()
	purgeCalls := len(dl.purgeTransferCalls)
	dl.mu.Unlock()
	if purgeCalls != 0 {
		t.Errorf("PurgeTransfer calls = %d, want 0 when list fetch fails", purgeCalls)
	}
}

func TestHandleTorrentRemoveWithDeleteLocalData(t *testing.T) {
	client := &fakePutioClient{
		transfers: []*putio.Transfer{
			{ID: 100, Hash: "abc", FileID: 999, Name: "movie"},
		},
	}
	dl := newFakeDownloadService()
	srv := newTestServer(t, client, dl)

	// Plant a directory to delete so the local-data branch has a real
	// target to remove (and we can verify it disappears).
	localDir := srv.cfg.TargetDir + "/movie"
	if err := writeStubDir(t, localDir); err != nil {
		t.Fatal(err)
	}

	req := rpcRequest(t, "torrent-remove", map[string]interface{}{
		"ids":               []string{"abc"},
		"delete-local-data": true,
	})
	w := httptest.NewRecorder()
	srv.handleRPC(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if _, err := statPath(localDir); err == nil {
		t.Errorf("expected %q to be deleted, but it still exists", localDir)
	}
}

func TestHandleTorrentRemoveStillPurgesWhenFileIDIsZero(t *testing.T) {
	// The cascade-delete-root protection (DeleteFile(0) wipes the put.io
	// account root) now lives inside Manager.PurgeTransfer rather than at
	// the RPC boundary. The server's job is just to delegate; PurgeTransfer
	// still gets called for FileID==0 transfers and decides what to do.
	client := &fakePutioClient{
		transfers: []*putio.Transfer{
			{ID: 100, Hash: "abc", FileID: 0, Name: "movie"},
		},
	}
	dl := newFakeDownloadService()
	srv := newTestServer(t, client, dl)

	req := rpcRequest(t, "torrent-remove", map[string]interface{}{
		"ids":               []string{"abc"},
		"delete-local-data": false,
	})
	w := httptest.NewRecorder()
	srv.handleRPC(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	dl.mu.Lock()
	purgeCalls := append([]int64(nil), dl.purgeTransferCalls...)
	dl.mu.Unlock()
	if len(purgeCalls) != 1 || purgeCalls[0] != 100 {
		t.Errorf("PurgeTransfer calls = %v, want [100]", purgeCalls)
	}
}
