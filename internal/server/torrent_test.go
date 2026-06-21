package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/doodla/go-putio"
)

func TestPutioErrorString(t *testing.T) {
	tests := []struct {
		name     string
		transfer putio.Transfer
		want     string
	}{
		{
			name:     "non-error status ignores tracker and status messages",
			transfer: putio.Transfer{Status: "DOWNLOADING", TrackerMessage: "1 seeders 2 leechers", StatusMessage: "getting metadata"},
			want:     "",
		},
		{
			name:     "non-error status still surfaces ErrorMessage if set",
			transfer: putio.Transfer{Status: "DOWNLOADING", ErrorMessage: "leftover error"},
			want:     "leftover error",
		},
		{
			name:     "error with tracker message only",
			transfer: putio.Transfer{Status: "ERROR", TrackerMessage: "torrent not found"},
			want:     "torrent not found",
		},
		{
			name:     "error precedence tracker then error then status",
			transfer: putio.Transfer{Status: "ERROR", TrackerMessage: "DMCA notice", ErrorMessage: "download failed", StatusMessage: "no peers"},
			want:     "DMCA notice; download failed; no peers",
		},
		{
			name:     "error de-dups repeated message across fields",
			transfer: putio.Transfer{Status: "ERROR", ErrorMessage: "dead torrent", StatusMessage: "dead torrent"},
			want:     "dead torrent",
		},
		{
			name:     "error trims whitespace and skips blank fields",
			transfer: putio.Transfer{Status: "ERROR", TrackerMessage: "  ", ErrorMessage: " banned ", StatusMessage: ""},
			want:     "banned",
		},
		{
			name:     "error with no messages yields empty",
			transfer: putio.Transfer{Status: "ERROR"},
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := putioErrorString(&tt.transfer)
			if got != tt.want {
				t.Errorf("putioErrorString() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDeleteLocalData(t *testing.T) {
	tests := []struct {
		name         string
		transferName string
		setup        func(t *testing.T, targetDir string)
		wantErr      bool
		wantDeleted  bool
	}{
		{
			name:         "deletes transfer directory",
			transferName: "My.Show.S01E01",
			setup: func(t *testing.T, targetDir string) {
				dir := filepath.Join(targetDir, "My.Show.S01E01")
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "episode.mkv"), []byte("data"), 0644); err != nil {
					t.Fatal(err)
				}
			},
			wantDeleted: true,
		},
		{
			name:         "deletes single file transfer",
			transferName: "movie.mkv",
			setup: func(t *testing.T, targetDir string) {
				if err := os.WriteFile(filepath.Join(targetDir, "movie.mkv"), []byte("data"), 0644); err != nil {
					t.Fatal(err)
				}
			},
			wantDeleted: true,
		},
		{
			name:         "no error when path does not exist",
			transferName: "nonexistent",
			setup:        func(t *testing.T, targetDir string) {},
			wantDeleted:  false,
		},
		{
			name:         "rejects path traversal with ..",
			transferName: "../../etc/passwd",
			setup:        func(t *testing.T, targetDir string) {},
			wantErr:      true,
		},
		{
			name:         "absolute path in transfer name is safe",
			transferName: "/tmp/evil",
			setup: func(t *testing.T, targetDir string) {
				// filepath.Join strips leading / so this resolves inside targetDir
			},
			wantDeleted: false,
		},
		{
			name:         "deletes nested directory structure",
			transferName: "Show.S01",
			setup: func(t *testing.T, targetDir string) {
				dir := filepath.Join(targetDir, "Show.S01", "Subs")
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "english.srt"), []byte("subs"), 0644); err != nil {
					t.Fatal(err)
				}
				parent := filepath.Join(targetDir, "Show.S01")
				if err := os.WriteFile(filepath.Join(parent, "episode.mkv"), []byte("video"), 0644); err != nil {
					t.Fatal(err)
				}
			},
			wantDeleted: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			targetDir := t.TempDir()
			tt.setup(t, targetDir)

			err := deleteLocalData(targetDir, tt.transferName)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if tt.wantDeleted {
				localPath := filepath.Join(targetDir, tt.transferName)
				if _, err := os.Stat(localPath); !os.IsNotExist(err) {
					t.Errorf("expected %q to be deleted, but it still exists", localPath)
				}
			}
		})
	}
}

func TestExtractCategory(t *testing.T) {
	tests := []struct {
		name        string
		targetDir   string
		downloadDir string
		want        string
	}{
		{
			name:        "with category",
			targetDir:   "/downloads",
			downloadDir: "/downloads/tv",
			want:        "tv",
		},
		{
			name:        "empty downloadDir",
			targetDir:   "/downloads",
			downloadDir: "",
			want:        "",
		},
		{
			name:        "same as targetDir",
			targetDir:   "/downloads",
			downloadDir: "/downloads",
			want:        "",
		},
		{
			name:        "nested category",
			targetDir:   "/downloads",
			downloadDir: "/downloads/media/tv",
			want:        "media/tv",
		},
		{
			name:        "trailing slash on downloadDir",
			targetDir:   "/downloads",
			downloadDir: "/downloads/tv/",
			want:        "tv",
		},
		{
			name:        "trailing slash on targetDir",
			targetDir:   "/downloads/",
			downloadDir: "/downloads/tv",
			want:        "tv",
		},
		{
			name:        "both trailing slashes",
			targetDir:   "/downloads/",
			downloadDir: "/downloads/tv/",
			want:        "tv",
		},
		// Path-traversal cases: extractCategory must reject anything that
		// escapes targetDir, otherwise the category gets joined into the
		// download path and filepath.Join normalizes "../" — letting a
		// misconfigured (or malicious) *arr write outside TargetDir.
		{
			name:        "escape to absolute path outside",
			targetDir:   "/downloads",
			downloadDir: "/etc",
			want:        "",
		},
		{
			name:        "escape via dotdot in absolute path",
			targetDir:   "/downloads",
			downloadDir: "/downloads/../etc",
			want:        "",
		},
		{
			name:        "relative dotdot",
			targetDir:   "/downloads",
			downloadDir: "../foo",
			want:        "",
		},
		{
			name:        "deeper escape",
			targetDir:   "/downloads",
			downloadDir: "/downloads/sub/../../etc",
			want:        "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractCategory(tt.targetDir, tt.downloadDir)
			if got != tt.want {
				t.Errorf("extractCategory(%q, %q) = %q, want %q", tt.targetDir, tt.downloadDir, got, tt.want)
			}
		})
	}
}

func TestDeleteLocalDataDoesNotAffectSiblings(t *testing.T) {
	targetDir := t.TempDir()

	// Create two transfer directories
	for _, name := range []string{"transfer-a", "transfer-b"} {
		dir := filepath.Join(targetDir, name)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "file.mkv"), []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Delete only transfer-a
	if err := deleteLocalData(targetDir, "transfer-a"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// transfer-a should be gone
	if _, err := os.Stat(filepath.Join(targetDir, "transfer-a")); !os.IsNotExist(err) {
		t.Error("transfer-a should have been deleted")
	}

	// transfer-b should still exist
	if _, err := os.Stat(filepath.Join(targetDir, "transfer-b", "file.mkv")); err != nil {
		t.Error("transfer-b should not have been affected")
	}
}
