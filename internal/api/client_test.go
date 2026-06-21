package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
)

// TestAuthenticateRejectsEmptyUsername: a 200 response whose account has no
// username is treated as an auth failure (a malformed/invalid-token response),
// while a populated username authenticates.
func TestAuthenticateRejectsEmptyUsername(t *testing.T) {
	t.Run("empty username fails", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"info":{"username":""}}`))
		}))
		defer srv.Close()
		c := newTestClient(t, srv.URL)
		if err := c.Authenticate(context.Background()); err == nil {
			t.Error("expected error for empty username, got nil")
		}
	})

	t.Run("populated username succeeds", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"info":{"username":"alice"}}`))
		}))
		defer srv.Close()
		c := newTestClient(t, srv.URL)
		if err := c.Authenticate(context.Background()); err != nil {
			t.Errorf("Authenticate: %v", err)
		}
	})
}

// newTestClient builds an api.Client whose underlying go-putio client is pointed
// at baseURL (an httptest server) instead of the real put.io. It reaches into
// the unexported client field — legal within the package — because there is no
// public BaseURL setter on api.Client.
func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	c := NewClient("test-token")
	u, err := url.Parse(baseURL)
	if err != nil {
		t.Fatalf("parse base url: %v", err)
	}
	c.client.BaseURL = u
	return c
}

// fileJSON renders one put.io file/folder object. Folders are identified by the
// x-directory content type, which is what File.IsDir keys on.
func fileJSON(id int64, name string, size int64, dir bool) string {
	ct := "video/x-matroska"
	if dir {
		ct = "application/x-directory"
	}
	i := strconv.FormatInt
	return `{"id":` + i(id, 10) + `,"name":"` + name + `","size":` + i(size, 10) + `,"content_type":"` + ct + `"}`
}

// TestGetAllTransferFilesWalksTree verifies the recursive walk flattens a nested
// put.io folder into leaf files with forward-slash relative paths rooted at the
// transfer (the path-collision fix's contract).
func TestGetAllTransferFilesWalksTree(t *testing.T) {
	// Tree:
	//   root(10, dir)
	//   ├── movie.mkv(11)
	//   └── Subs(12, dir)
	//       └── english.srt(13)
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/files/10", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"file":` + fileJSON(10, "Movie", 0, true) + `}`))
	})
	mux.HandleFunc("/v2/files/list", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("parent_id") {
		case "10":
			_, _ = w.Write([]byte(`{"files":[` + fileJSON(11, "movie.mkv", 100, false) + `,` + fileJSON(12, "Subs", 0, true) + `],"cursor":""}`))
		case "12":
			_, _ = w.Write([]byte(`{"files":[` + fileJSON(13, "english.srt", 50, false) + `],"cursor":""}`))
		default:
			http.Error(w, "unexpected parent_id", http.StatusBadRequest)
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	files, err := c.GetAllTransferFiles(context.Background(), 10)
	if err != nil {
		t.Fatalf("GetAllTransferFiles: %v", err)
	}

	got := map[string]int64{} // relPath -> size
	for _, f := range files {
		got[f.RelPath] = f.File.Size
	}
	want := map[string]int64{
		"movie.mkv":        100,
		"Subs/english.srt": 50,
	}
	if len(got) != len(want) {
		t.Fatalf("got %d leaves %v, want %d %v", len(got), got, len(want), want)
	}
	for rel, size := range want {
		if got[rel] != size {
			t.Errorf("leaf %q size = %d, want %d", rel, got[rel], size)
		}
	}
}

// TestGetAllTransferFilesSingleFile covers the non-directory root: a transfer
// whose file_id points straight at a leaf returns that one file, RelPath == name.
func TestGetAllTransferFilesSingleFile(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/files/20", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"file":` + fileJSON(20, "single.mkv", 4096, false) + `}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	files, err := c.GetAllTransferFiles(context.Background(), 20)
	if err != nil {
		t.Fatalf("GetAllTransferFiles: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1", len(files))
	}
	if files[0].RelPath != "single.mkv" || files[0].File.Size != 4096 {
		t.Errorf("got {RelPath:%q size:%d}, want {single.mkv 4096}", files[0].RelPath, files[0].File.Size)
	}
}

// TestEnsureFolderReturnsExisting: when a folder with the name already exists at
// the put.io root, EnsureFolder returns its ID without creating a new one.
func TestEnsureFolderReturnsExisting(t *testing.T) {
	var createCalled bool
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/files/list", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"files":[` + fileJSON(555, "plundrio", 0, true) + `],"cursor":""}`))
	})
	mux.HandleFunc("/v2/files/create-folder", func(w http.ResponseWriter, r *http.Request) {
		createCalled = true
		http.Error(w, "should not create", http.StatusBadRequest)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	id, err := c.EnsureFolder(context.Background(), "plundrio")
	if err != nil {
		t.Fatalf("EnsureFolder: %v", err)
	}
	if id != 555 {
		t.Errorf("folder id = %d, want 555 (existing)", id)
	}
	if createCalled {
		t.Error("create-folder called for an already-existing folder")
	}
}

// TestEnsureFolderCreatesWhenMissing: with no matching folder at the root,
// EnsureFolder creates one and returns the new ID.
func TestEnsureFolderCreatesWhenMissing(t *testing.T) {
	var createCalled bool
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/files/list", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"files":[` + fileJSON(1, "other", 0, true) + `],"cursor":""}`))
	})
	mux.HandleFunc("/v2/files/create-folder", func(w http.ResponseWriter, r *http.Request) {
		createCalled = true
		_, _ = w.Write([]byte(`{"file":` + fileJSON(777, "plundrio", 0, true) + `}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	id, err := c.EnsureFolder(context.Background(), "plundrio")
	if err != nil {
		t.Fatalf("EnsureFolder: %v", err)
	}
	if id != 777 {
		t.Errorf("folder id = %d, want 777 (created)", id)
	}
	if !createCalled {
		t.Error("create-folder not called for a missing folder")
	}
}

// TestAddTransferRejectsErrorStatus: a transfer that put.io immediately marks
// ERROR (e.g. a bad magnet) must surface as an error from AddTransfer, not a
// silent empty hash.
func TestAddTransferRejectsErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"transfer":{"id":1,"status":"ERROR","error_message":"bad magnet"}}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	_, err := c.AddTransfer(context.Background(), "magnet:?xt=bad", 42)
	if err == nil {
		t.Fatal("expected error for ERROR-status transfer, got nil")
	}
}

// TestAddTransferReturnsHash: a healthy add returns the transfer hash.
func TestAddTransferReturnsHash(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"transfer":{"id":1,"status":"IN_QUEUE","hash":"deadbeef"}}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	hash, err := c.AddTransfer(context.Background(), "magnet:?xt=ok", 42)
	if err != nil {
		t.Fatalf("AddTransfer: %v", err)
	}
	if hash != "deadbeef" {
		t.Errorf("hash = %q, want deadbeef", hash)
	}
}

// TestGetAllTransferFilesPropagatesError ensures an API failure on the root Get
// surfaces as an error rather than an empty list.
func TestGetAllTransferFilesPropagatesError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error_type":"NotFound"}`, http.StatusNotFound)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	if _, err := c.GetAllTransferFiles(context.Background(), 99); err == nil {
		t.Fatal("expected error for 404 root file, got nil")
	}
}
