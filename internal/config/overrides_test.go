package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// newViper builds an isolated viper configured like loadConfig's: PLDR env
// prefix, "-"/"."→"_" key replacer, AutomaticEnv. Using a fresh *viper.Viper per
// test (not the global) keeps the precedence matrix hermetic.
func newViper() *viper.Viper {
	v := viper.New()
	v.SetEnvPrefix("PLDR")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	v.AutomaticEnv()
	return v
}

// clearPLDREnv removes every PLDR_* var on the runner so a CI host's env can't
// leak into the precedence assertions. It must genuinely UNSET them, not
// set-to-empty: IsEnvPinned uses os.LookupEnv, which reports a present-but-empty
// var as set, so t.Setenv(key,"") would falsely mark a key env-pinned. t.Setenv
// first registers the Cleanup that restores the original value; os.Unsetenv then
// makes it absent for the duration.
func clearPLDREnv(t *testing.T) {
	t.Helper()
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "PLDR_") {
			key := kv[:strings.IndexByte(kv, '=')]
			t.Setenv(key, "")
			_ = os.Unsetenv(key)
		}
	}
}

func TestLoadOverridesMissingFileIsNoOp(t *testing.T) {
	clearPLDREnv(t)
	// A path that doesn't exist must return (nil, nil), exactly like
	// CategoryStore.Load — absence means "no overrides," not an error.
	ov, err := LoadOverrides(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err != nil {
		t.Fatalf("missing file should be a no-op, got error: %v", err)
	}
	if ov != nil {
		t.Errorf("missing file should yield nil overrides, got %v", ov)
	}
}

func TestLoadOverridesEmptyPathIsNoOp(t *testing.T) {
	ov, err := LoadOverrides("")
	if err != nil || ov != nil {
		t.Fatalf("empty path: got (%v, %v), want (nil, nil)", ov, err)
	}
}

func TestLoadOverridesMalformedIsSurfaced(t *testing.T) {
	clearPLDREnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "overrides.json")
	// A present-but-corrupt file must ERROR — a silent mis-resolve of
	// target/listen is the blast-radius bug the design calls out.
	if err := os.WriteFile(path, []byte("{ this is not json"), 0644); err != nil {
		t.Fatalf("seed malformed file: %v", err)
	}
	if _, err := LoadOverrides(path); err == nil {
		t.Fatal("malformed overrides file must surface an error, not be swallowed")
	}
}

func TestLoadOverridesDropsUnknownKeys(t *testing.T) {
	clearPLDREnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "overrides.json")
	if err := os.WriteFile(path, []byte(`{"workers": 9, "bogus_key": "x"}`), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ov, err := LoadOverrides(path)
	if err != nil {
		t.Fatalf("LoadOverrides: %v", err)
	}
	if _, ok := ov["bogus_key"]; ok {
		t.Errorf("unknown key bogus_key should be dropped on load, got %v", ov)
	}
	if _, ok := ov["workers"]; !ok {
		t.Errorf("known key workers should survive, got %v", ov)
	}
}

// TestPrecedenceOverridesBeatsFlagAndDefault asserts the core ordering for a
// NON-env-pinned key: an overrides-file value beats both the flag-bound value
// and the compiled default. viper.Set (the Override tier) is exactly the
// "highest precedence above viper's resolution" the design requires.
func TestPrecedenceOverridesBeatsFlagAndDefault(t *testing.T) {
	clearPLDREnv(t)
	v := newViper()

	// Stand in for a flag binding: viper's flag tier sits below Override.
	v.SetDefault("workers", 4) // compiled default

	ov := Overrides{"workers": 8}
	applied := ApplyOverrides(v, ov)

	if got := v.GetInt("workers"); got != 8 {
		t.Errorf("workers = %d, want 8 (overrides beats default)", got)
	}
	if len(applied) != 1 || applied[0] != "workers" {
		t.Errorf("applied = %v, want [workers]", applied)
	}
}

// TestPrecedenceEnvBeatsOverrides is the LOCKED case the design's
// non-negotiable guard protects: an env-pinned key's env value must win over the
// overrides file. The os.LookupEnv env-skip guard in ApplyOverrides is what
// makes this hold — without it, viper.Set (Override tier) would silently beat
// AutomaticEnv and invert decision 2 (env must win; the daemon can't rewrite
// compose).
func TestPrecedenceEnvBeatsOverrides(t *testing.T) {
	clearPLDREnv(t)
	t.Setenv("PLDR_WORKERS", "16") // env-pin workers
	v := newViper()
	v.SetDefault("workers", 4)

	ov := Overrides{"workers": 8} // overrides file tries to set 8
	applied := ApplyOverrides(v, ov)

	if got := v.GetInt("workers"); got != 16 {
		t.Errorf("workers = %d, want 16 (env beats overrides for env-pinned key)", got)
	}
	for _, k := range applied {
		if k == "workers" {
			t.Errorf("env-pinned workers must be SKIPPED by ApplyOverrides, but it was applied: %v", applied)
		}
	}
}

// TestPrecedenceMatrix walks the full env > overrides > default ordering across
// several keys in one table so a regression in any tier is caught.
func TestPrecedenceMatrix(t *testing.T) {
	clearPLDREnv(t)
	// folder is env-pinned; workers and log_level come from the overrides file;
	// listen falls through to its default.
	t.Setenv("PLDR_FOLDER", "env-folder")
	v := newViper()
	v.SetDefault("folder", "default-folder")
	v.SetDefault("workers", 4)
	v.SetDefault("log_level", "info")
	v.SetDefault("listen", ":9091")

	ov := Overrides{
		"folder":    "file-folder", // must lose to env
		"workers":   12,            // must win over default
		"log_level": "debug",       // must win over default
	}
	ApplyOverrides(v, ov)

	cases := []struct {
		key  string
		want any
	}{
		{"folder", "env-folder"}, // env wins (pinned)
		{"workers", 12},          // overrides wins
		{"log_level", "debug"},   // overrides wins
		{"listen", ":9091"},      // default (no override, not pinned)
	}
	for _, c := range cases {
		switch want := c.want.(type) {
		case string:
			if got := v.GetString(c.key); got != want {
				t.Errorf("%s = %q, want %q", c.key, got, want)
			}
		case int:
			if got := v.GetInt(c.key); got != want {
				t.Errorf("%s = %d, want %d", c.key, got, want)
			}
		}
	}
}

func TestIsEnvPinned(t *testing.T) {
	clearPLDREnv(t)
	t.Setenv("PLDR_TARGET", "/x")
	if !IsEnvPinned("target") {
		t.Error("target should be env-pinned when PLDR_TARGET is set")
	}
	if IsEnvPinned("workers") {
		t.Error("workers should not be env-pinned when PLDR_WORKERS is unset")
	}
}

func TestEnvVarFor(t *testing.T) {
	if got := EnvVarFor("dashboard_listen"); got != "PLDR_DASHBOARD_LISTEN" {
		t.Errorf("EnvVarFor(dashboard_listen) = %q, want PLDR_DASHBOARD_LISTEN", got)
	}
}

// TestWriteOverrideRoundTrips persists keys, reloads, and asserts a merge
// preserves prior keys. Token is persistable (contract) but never boot-applied;
// it must survive a round-trip even though it's not in OverrideKeys.
func TestWriteOverrideRoundTrips(t *testing.T) {
	clearPLDREnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "overrides.json")

	if err := WriteOverride(path, map[string]any{"workers": 8}); err != nil {
		t.Fatalf("first write: %v", err)
	}
	// A second write merges, preserving workers.
	if err := WriteOverride(path, map[string]any{"log_level": "debug", "token": "secret"}); err != nil {
		t.Fatalf("second write: %v", err)
	}

	ov, err := LoadOverrides(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if ov["workers"] != float64(8) { // JSON numbers decode as float64
		t.Errorf("workers = %v, want 8 (merge preserved prior key)", ov["workers"])
	}
	if ov["log_level"] != "debug" {
		t.Errorf("log_level = %v, want debug", ov["log_level"])
	}
	if ov["token"] != "secret" {
		t.Errorf("token = %v, want it to round-trip through the file", ov["token"])
	}
}

// TestWriteOverrideRejectsUnknownKey guards against a typo silently persisting a
// dead key.
func TestWriteOverrideRejectsUnknownKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "overrides.json")
	if err := WriteOverride(path, map[string]any{"workerz": 8}); err == nil {
		t.Fatal("WriteOverride should reject an unknown key")
	}
}

// TestApplyOverridesTokenNeverBootApplied asserts a token sitting in the
// overrides file is NOT pushed into viper by ApplyOverrides — token stays
// owned by env/flag/config (the design's "env-pinned, never touched"
// invariant), so a stale file token can't become the resolved token.
func TestApplyOverridesTokenNeverBootApplied(t *testing.T) {
	clearPLDREnv(t)
	v := newViper()
	ov := Overrides{"token": "leaked-from-file"}
	applied := ApplyOverrides(v, ov)
	for _, k := range applied {
		if k == "token" {
			t.Fatalf("token must never be boot-applied from the overrides file, applied=%v", applied)
		}
	}
	if v.GetString("token") != "" {
		t.Errorf("viper token = %q, want empty (token not applied from file)", v.GetString("token"))
	}
}
