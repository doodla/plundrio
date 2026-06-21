package server

import (
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/doodla/go-putio"
	"github.com/doodla/plundrio/internal/config"
)

var errAccount = errors.New("account info unavailable")

// newTestServerWithMinFree builds a Server like newTestServer but with the
// opt-in free-space floor configured.
func newTestServerWithMinFree(t *testing.T, client *fakePutioClient, dl *fakeDownloadService, minFree string) *Server {
	t.Helper()
	cfg := &config.Config{
		TargetDir:    t.TempDir(),
		FolderID:     42,
		ListenAddr:   ":0",
		MinFreeSpace: minFree,
	}
	return New(cfg, client, dl)
}

func accountWithAvail(avail int64) *putio.AccountInfo {
	ai := &putio.AccountInfo{Username: "test"}
	ai.Disk.Avail = avail
	return ai
}

func addMagnet(t *testing.T, srv *Server) rpcResponse {
	t.Helper()
	req := rpcRequest(t, "torrent-add", map[string]interface{}{
		"magnetLink": "magnet:?xt=urn:btih:abc",
	})
	w := httptest.NewRecorder()
	srv.handleRPC(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 envelope, got %d, body=%s", w.Code, w.Body.String())
	}
	return decodeRPCResponse(t, w.Body.Bytes())
}

func TestTorrentAddRejectedBelowFreeSpaceFloor(t *testing.T) {
	client := &fakePutioClient{accountInfo: accountWithAvail(5_000_000_000)} // 5GB avail
	dl := newFakeDownloadService()
	srv := newTestServerWithMinFree(t, client, dl, "10GB")

	resp := addMagnet(t, srv)
	if resp.Result == "success" {
		t.Fatalf("expected rejection below floor, got success")
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.addTransferCalls) != 0 {
		t.Errorf("AddTransfer must not run when below the free-space floor; got %d calls", len(client.addTransferCalls))
	}
}

func TestTorrentAddMetainfoRejectedBelowFreeSpaceFloor(t *testing.T) {
	// The floor must apply to the .torrent/metainfo path too, not just magnets.
	client := &fakePutioClient{accountInfo: accountWithAvail(5_000_000_000)} // 5GB
	dl := newFakeDownloadService()
	srv := newTestServerWithMinFree(t, client, dl, "10GB")

	req := rpcRequest(t, "torrent-add", map[string]interface{}{
		"filename": "ubuntu.torrent",
		"metainfo": base64.StdEncoding.EncodeToString([]byte("d8:announce15:http://example.come")),
	})
	w := httptest.NewRecorder()
	srv.handleRPC(w, req)

	resp := decodeRPCResponse(t, w.Body.Bytes())
	if resp.Result == "success" {
		t.Fatalf("expected rejection below floor on metainfo path, got success")
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.uploadFileCalls) != 0 {
		t.Errorf("UploadFile must not run when below the free-space floor; got %d calls", len(client.uploadFileCalls))
	}
}

func TestTorrentAddAllowedAboveFreeSpaceFloor(t *testing.T) {
	client := &fakePutioClient{accountInfo: accountWithAvail(50_000_000_000)} // 50GB avail
	dl := newFakeDownloadService()
	srv := newTestServerWithMinFree(t, client, dl, "10GB")

	resp := addMagnet(t, srv)
	if resp.Result != "success" {
		t.Fatalf("expected success above floor, got %q", resp.Result)
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.addTransferCalls) != 1 {
		t.Errorf("AddTransfer calls = %d, want 1", len(client.addTransferCalls))
	}
}

func TestTorrentAddFreeSpaceCheckDisabledByDefault(t *testing.T) {
	// Tiny avail, but no floor configured: add proceeds and account info is
	// never even fetched.
	client := &fakePutioClient{accountInfo: accountWithAvail(1)}
	dl := newFakeDownloadService()
	srv := newTestServerWithMinFree(t, client, dl, "")

	resp := addMagnet(t, srv)
	if resp.Result != "success" {
		t.Fatalf("expected success with check disabled, got %q", resp.Result)
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.accountInfoCalls != 0 {
		t.Errorf("GetAccountInfo should not be called when check is disabled; got %d", client.accountInfoCalls)
	}
	if len(client.addTransferCalls) != 1 {
		t.Errorf("AddTransfer calls = %d, want 1", len(client.addTransferCalls))
	}
}

func TestTorrentAddFreeSpaceFailsOpenOnAccountError(t *testing.T) {
	// Floor configured, but the account lookup errors: the add must proceed
	// (fail open) rather than block downloads on a transient API hiccup.
	client := &fakePutioClient{getAccountInfoErr: errAccount}
	dl := newFakeDownloadService()
	srv := newTestServerWithMinFree(t, client, dl, "10GB")

	resp := addMagnet(t, srv)
	if resp.Result != "success" {
		t.Fatalf("expected fail-open success on account error, got %q", resp.Result)
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.addTransferCalls) != 1 {
		t.Errorf("AddTransfer calls = %d, want 1 (fail open)", len(client.addTransferCalls))
	}
}

func TestFreeSpaceCacheCollapsesBurst(t *testing.T) {
	client := &fakePutioClient{accountInfo: accountWithAvail(50_000_000_000)}
	dl := newFakeDownloadService()
	srv := newTestServerWithMinFree(t, client, dl, "10GB")

	for i := 0; i < 3; i++ {
		if resp := addMagnet(t, srv); resp.Result != "success" {
			t.Fatalf("add %d: expected success, got %q", i, resp.Result)
		}
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.accountInfoCalls != 1 {
		t.Errorf("expected 1 cached account.info call across a burst, got %d", client.accountInfoCalls)
	}
}
