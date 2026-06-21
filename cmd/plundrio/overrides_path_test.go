package main

import (
	"path/filepath"
	"testing"
)

// TestResolveOverridesPath covers the precedence the dashboard and main.go rely
// on to find the runtime-overrides file: --overrides-file flag, then
// PLDR_OVERRIDES_FILE, then <target>/.plundrio-overrides.json, then "" when no
// target is known. A wrong result here would read/write overrides from the
// wrong place.
func TestResolveOverridesPath(t *testing.T) {
	t.Run("flag wins over env and target", func(t *testing.T) {
		t.Setenv("PLDR_OVERRIDES_FILE", "/from/env.json")
		cmd := newTestRunCmd()
		if err := cmd.ParseFlags([]string{"--overrides-file", "/from/flag.json"}); err != nil {
			t.Fatalf("ParseFlags: %v", err)
		}
		if got := resolveOverridesPath(cmd, "/some/target"); got != "/from/flag.json" {
			t.Errorf("got %q, want /from/flag.json", got)
		}
	})

	t.Run("env wins over target when no flag", func(t *testing.T) {
		t.Setenv("PLDR_OVERRIDES_FILE", "/from/env.json")
		cmd := newTestRunCmd()
		if err := cmd.ParseFlags(nil); err != nil {
			t.Fatalf("ParseFlags: %v", err)
		}
		if got := resolveOverridesPath(cmd, "/some/target"); got != "/from/env.json" {
			t.Errorf("got %q, want /from/env.json", got)
		}
	})

	t.Run("falls back to target dir", func(t *testing.T) {
		t.Setenv("PLDR_OVERRIDES_FILE", "") // treated as unset
		cmd := newTestRunCmd()
		if err := cmd.ParseFlags(nil); err != nil {
			t.Fatalf("ParseFlags: %v", err)
		}
		want := filepath.Join("/some/target", ".plundrio-overrides.json")
		if got := resolveOverridesPath(cmd, "/some/target"); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("empty when no flag, env, or target", func(t *testing.T) {
		t.Setenv("PLDR_OVERRIDES_FILE", "")
		cmd := newTestRunCmd()
		if err := cmd.ParseFlags(nil); err != nil {
			t.Fatalf("ParseFlags: %v", err)
		}
		if got := resolveOverridesPath(cmd, ""); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
}
