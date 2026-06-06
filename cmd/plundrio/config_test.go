package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// newTestRunCmd builds a fresh cobra command with the same flag set as the
// package-global runCmd. Each test gets its own command so flag registration
// (which pflag disallows on the same command twice) doesn't conflict.
func newTestRunCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "run"}
	registerRunFlags(cmd)
	return cmd
}

// resetEnvAndViper isolates each test from package-global viper state plus any
// PLDR_* env vars on the test runner. Without this, a test running in CI on a
// host that has PLDR_TOKEN set (or a stale viper from a prior test) would see
// values it didn't ask for.
func resetEnvAndViper(t *testing.T) {
	t.Helper()
	viper.Reset()
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "PLDR_") {
			key := kv[:strings.IndexByte(kv, '=')]
			t.Setenv(key, "")
		}
	}
}

// TestLoadConfigDefaults asserts that when no env vars, config file, or CLI
// flags override anything, the loaded Config reflects the flag defaults wired
// up in init(). This is the regression guard for the v0.10.11/12 bug: the
// retention janitor silently no-op'd because viper.GetDuration on the
// underscored key didn't see the flag bound under the hyphenated name. If
// flagViperBindings drifts again, this test catches it before deploy instead
// of weeks later via a 190 GB put.io quota overflow.
func TestLoadConfigDefaults(t *testing.T) {
	resetEnvAndViper(t)

	cmd := newTestRunCmd()
	if err := cmd.ParseFlags(nil); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}

	cfg, logLevel, err := loadConfig(cmd)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}

	if cfg.PostCompleteRetention != 24*time.Hour {
		t.Errorf("PostCompleteRetention = %v, want 24h", cfg.PostCompleteRetention)
	}
	if cfg.ListenAddr != ":9091" {
		t.Errorf("ListenAddr = %q, want :9091", cfg.ListenAddr)
	}
	if cfg.WorkerCount != 4 {
		t.Errorf("WorkerCount = %d, want 4", cfg.WorkerCount)
	}
	if cfg.PutioFolder != "plundrio" {
		t.Errorf("PutioFolder = %q, want plundrio", cfg.PutioFolder)
	}
	if logLevel != "" {
		t.Errorf("log level default = %q, want empty (caller decides whether to override)", logLevel)
	}
	if cfg.TargetDir != "" {
		t.Errorf("TargetDir = %q, want empty (no flag default; runCmd validates)", cfg.TargetDir)
	}
	if cfg.OAuthToken != "" {
		t.Errorf("OAuthToken = %q, want empty (no flag default; runCmd validates)", cfg.OAuthToken)
	}
	if cfg.DownloadStartWindow.Enabled {
		t.Errorf("DownloadStartWindow.Enabled = true, want false")
	}
}

// TestLoadConfigEnvOverride covers the env-var path: PLDR_-prefixed variables
// with hyphen<->underscore replacement should reach the corresponding mapstructure
// keys. This is the secondary path that would have masked the production bug
// if the deployment had set PLDR_POST_COMPLETE_RETENTION explicitly.
func TestLoadConfigEnvOverride(t *testing.T) {
	resetEnvAndViper(t)
	t.Setenv("PLDR_POST_COMPLETE_RETENTION", "2h")
	t.Setenv("PLDR_WORKERS", "8")
	t.Setenv("PLDR_TARGET", "/tmp/whatever")
	t.Setenv("PLDR_FOLDER", "Mixed-Case-Folder")
	t.Setenv("PLDR_TOKEN", "from-env")

	cmd := newTestRunCmd()
	if err := cmd.ParseFlags(nil); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}

	cfg, _, err := loadConfig(cmd)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}

	if cfg.PostCompleteRetention != 2*time.Hour {
		t.Errorf("PostCompleteRetention = %v, want 2h", cfg.PostCompleteRetention)
	}
	if cfg.WorkerCount != 8 {
		t.Errorf("WorkerCount = %d, want 8", cfg.WorkerCount)
	}
	if cfg.TargetDir != "/tmp/whatever" {
		t.Errorf("TargetDir = %q, want /tmp/whatever", cfg.TargetDir)
	}
	if cfg.PutioFolder != "mixed-case-folder" {
		t.Errorf("PutioFolder = %q, want mixed-case-folder (lowercased)", cfg.PutioFolder)
	}
	if cfg.OAuthToken != "from-env" {
		t.Errorf("OAuthToken = %q, want from-env", cfg.OAuthToken)
	}
}

// TestLoadConfigConfigFile covers the YAML path: keys in a config file should
// reach the same mapstructure fields the flags + env vars do. Asserting all
// three paths converge on the same struct is the point of viper.Unmarshal — it
// guarantees there's no per-source key drift.
func TestLoadConfigConfigFile(t *testing.T) {
	resetEnvAndViper(t)

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "plundrio.yaml")
	body := `target: /from/yaml
folder: yaml-folder
token: yaml-token
listen: ":10091"
workers: 16
post_complete_retention: 6h
download_start_window:
  enabled: true
  start: "22:00"
  end: "06:00"
log_level: debug
`
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cmd := newTestRunCmd()
	if err := cmd.ParseFlags([]string{"--config", cfgPath}); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}

	cfg, logLevel, err := loadConfig(cmd)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}

	if cfg.TargetDir != "/from/yaml" {
		t.Errorf("TargetDir = %q, want /from/yaml", cfg.TargetDir)
	}
	if cfg.WorkerCount != 16 {
		t.Errorf("WorkerCount = %d, want 16", cfg.WorkerCount)
	}
	if cfg.PostCompleteRetention != 6*time.Hour {
		t.Errorf("PostCompleteRetention = %v, want 6h", cfg.PostCompleteRetention)
	}
	if !cfg.DownloadStartWindow.Enabled {
		t.Errorf("DownloadStartWindow.Enabled = false, want true")
	}
	if cfg.DownloadStartWindow.Start != "22:00" {
		t.Errorf("DownloadStartWindow.Start = %q, want 22:00", cfg.DownloadStartWindow.Start)
	}
	if logLevel != "debug" {
		t.Errorf("log level = %q, want debug", logLevel)
	}
}

// TestFlagViperBindingsCoverAllRunFlags asserts the binding map keeps pace
// with the flags registered on runCmd. A new flag added to runCmd without a
// binding here would silently bypass the canonical-key path and reproduce the
// original bug.
func TestFlagViperBindingsCoverAllRunFlags(t *testing.T) {
	cmd := newTestRunCmd()
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		// `config` and `demo` are special-cased: both are read directly via
		// cmd.Flags() (GetString / GetBool) rather than viper, so they don't
		// need a viper binding. `demo` is a boot-time control (also via
		// PLDR_DEMO), never persisted or unmarshaled into Config.
		if f.Name == "config" || f.Name == "demo" {
			return
		}
		if _, ok := flagViperBindings[f.Name]; !ok {
			t.Errorf("flag %q is registered on runCmd but has no entry in flagViperBindings", f.Name)
		}
	})
}
