package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTokenFile(t *testing.T, dir, contents string) string {
	t.Helper()
	path := filepath.Join(dir, "token")
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestResolveOAuthToken(t *testing.T) {
	dir := t.TempDir()
	tokenPath := writeTokenFile(t, dir, "from-file\n")

	tests := []struct {
		name      string
		explicit  string
		tokenFile string
		want      string
		wantErr   bool
	}{
		{
			name:     "both empty",
			want:     "",
		},
		{
			name:     "explicit only",
			explicit: "from-env",
			want:     "from-env",
		},
		{
			name:      "file only — trims trailing newline",
			tokenFile: tokenPath,
			want:      "from-file",
		},
		{
			name:      "explicit wins over file",
			explicit:  "from-env",
			tokenFile: tokenPath,
			want:      "from-env",
		},
		{
			name:      "missing file returns error",
			tokenFile: filepath.Join(dir, "does-not-exist"),
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveOAuthToken(tt.explicit, tt.tokenFile)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("resolveOAuthToken(%q, %q) = %q, want %q", tt.explicit, tt.tokenFile, got, tt.want)
			}
		})
	}
}

func TestResolveOAuthTokenStripsTrailingWhitespaceVariants(t *testing.T) {
	cases := map[string]string{
		"trailing LF":     "secret\n",
		"trailing CRLF":   "secret\r\n",
		"trailing tab":    "secret\t",
		"trailing spaces": "secret   ",
		"no trailing":     "secret",
	}
	dir := t.TempDir()
	for name, contents := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name)
			if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := resolveOAuthToken("", path)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != "secret" {
				t.Errorf("got %q, want %q", got, "secret")
			}
		})
	}
}
