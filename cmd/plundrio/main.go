package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/doodla/plundrio/internal/api"
	"github.com/doodla/plundrio/internal/config"
	"github.com/doodla/plundrio/internal/dashboard"
	"github.com/doodla/plundrio/internal/demo"
	"github.com/doodla/plundrio/internal/download"
	"github.com/doodla/plundrio/internal/log"
	"github.com/doodla/plundrio/internal/server"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// flagViperBindings maps each cobra flag name (hyphenated per CLI convention)
// to the canonical viper key (underscored, matching mapstructure tags + YAML +
// env-var-after-replacement). Explicit per-flag binding avoids the symptom of
// viper.BindPFlags(cmd.Flags()) registering keys under their flag names so that
// later viper.Unmarshal — which keys off the mapstructure tags — can't see the
// flag defaults. v0.10.11/12 shipped exactly that bug: GetDuration of
// "post_complete_retention" missed the flag bound under "post-complete-retention"
// and silently fell to 0, disabling the retention janitor for a full week.
//
// Keep this map's RHS aligned with the mapstructure tags on config.Config.
var flagViperBindings = map[string]string{
	"target":                  "target",
	"folder":                  "folder",
	"token":                   "token",
	"listen":                  "listen",
	"workers":                 "workers",
	"post-complete-retention": "post_complete_retention",
	"transfer-check-interval": "transfer_check_interval",
	"stall-timeout":           "stall_timeout",
	"stall-max-retries":       "stall_max_retries",
	"min-free-space":          "min_free_space",
	"log-level":               "log_level",
	"dashboard":               "dashboard",
	"dashboard-addr":          "dashboard_addr",
}

var (
	version = "dev"
)

// startupCallTimeout caps each blocking put.io call made during runCmd
// startup. Without it, an unreachable put.io (DNS not yet up after reboot,
// captive portal, transient outage) leaves Authenticate / EnsureFolder
// hanging on context.Background forever — systemd's Restart=on-failure
// never fires because the process stays alive. 30s is slow enough to
// tolerate a sluggish lookup, fast enough that systemd's restart loop
// will heal a transient failure.
const startupCallTimeout = 30 * time.Second

var rootCmd = &cobra.Command{
	Use:     "plundrio",
	Short:   "Put.io automation tool",
	Version: version,
}

// loadConfig wires viper to env/config/flags, then decodes into a Config via
// viper.Unmarshal. The mapstructure tags on Config are the single source of
// truth for key names. Returns the loaded config plus the resolved log level
// (separate because log-level is not a Config field — it sets a process-global
// log level at startup).
//
// Pure-ish: takes flags via the cobra command and reads env + filesystem for
// the optional config file, but does not validate or touch the network. The
// test suite calls it with a fresh viper to assert default resolution; the run
// command calls it then layers validation on top.
func loadConfig(cmd *cobra.Command) (*config.Config, string, error) {
	viper.SetEnvPrefix("PLDR")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	viper.AutomaticEnv()

	configFile, _ := cmd.Flags().GetString("config")
	if configFile != "" {
		viper.SetConfigFile(configFile)
		if err := viper.ReadInConfig(); err != nil {
			return nil, "", fmt.Errorf("read config file %q: %w", configFile, err)
		}
		log.Info("config").Str("file", viper.ConfigFileUsed()).Msg("Using config file")
	}

	for flagName, viperKey := range flagViperBindings {
		f := cmd.Flags().Lookup(flagName)
		if f == nil {
			return nil, "", fmt.Errorf("flag %q not registered on command", flagName)
		}
		if err := viper.BindPFlag(viperKey, f); err != nil {
			return nil, "", fmt.Errorf("bind %q -> %q: %w", flagName, viperKey, err)
		}
	}

	// Layer the runtime-overrides file on top, for non-env-pinned keys only.
	// Path: --overrides-file / PLDR_OVERRIDES_FILE, else <target>/.plundrio-overrides.json
	// where <target> is resolved from env/flag/config (the daemon's writable
	// volume). Applied via viper.Set (Override tier) AFTER the flag bindings so
	// it beats flag/config, but the env-skip guard in ApplyOverrides keeps
	// env-pinned keys winning. This adds a tier; it does NOT reorder the flag
	// bindings (the v0.10.11/12 drift cautionary tale).
	overridesPath := resolveOverridesPath(cmd, viper.GetString("target"))
	if overridesPath != "" {
		ov, err := config.LoadOverrides(overridesPath)
		if err != nil {
			return nil, "", fmt.Errorf("load overrides: %w", err)
		}
		if len(ov) > 0 {
			applied := config.ApplyOverrides(viper.GetViper(), ov)
			if len(applied) > 0 {
				log.Info("config").
					Str("file", overridesPath).
					Strs("applied_keys", applied).
					Msg("Applied runtime overrides")
			}
		}
	}

	var cfg config.Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, "", fmt.Errorf("decode config: %w", err)
	}

	cfg.PutioFolder = strings.ToLower(cfg.PutioFolder)

	token, err := resolveOAuthToken(cfg.OAuthToken, os.Getenv("PLDR_TOKEN_FILE"))
	if err != nil {
		return nil, "", fmt.Errorf("resolve oauth token: %w", err)
	}
	cfg.OAuthToken = token

	return &cfg, viper.GetString("log_level"), nil
}

// resolveOverridesPath resolves where the runtime-overrides JSON file lives:
// the --overrides-file flag, else PLDR_OVERRIDES_FILE, else
// <target>/.plundrio-overrides.json. target is the env/flag/config-resolved
// target dir (the daemon's writable volume); the overrides file lives there
// alongside the category state. Returns "" only when no target is known yet.
func resolveOverridesPath(cmd *cobra.Command, target string) string {
	if p, _ := cmd.Flags().GetString("overrides-file"); p != "" {
		return p
	}
	if env := os.Getenv("PLDR_OVERRIDES_FILE"); env != "" {
		return env
	}
	if target != "" {
		return filepath.Join(target, ".plundrio-overrides.json")
	}
	return ""
}

// putioClient is the union of the put.io methods the download manager and the
// RPC server require. Both *api.Client (real) and *demo.Client (--demo) satisfy
// it, so the boot path can swap one for the other behind a single variable.
type putioClient interface {
	download.PutioClient
	server.PutioClient
}

// validateRunConfig performs the pure (non-filesystem) validation of run-time
// config and returns the first problem found. Extracted from runCmd's Run so it
// is unit-testable without tripping log.Fatal / os.Exit; runCmd wraps the
// returned error in a log.Fatal.
func validateRunConfig(cfg *config.Config, demoMode bool) error {
	if err := download.ValidateStartWindow(cfg.DownloadStartWindow); err != nil {
		return fmt.Errorf("invalid download start window configuration: %w", err)
	}

	// Guard against an operator setting an aggressive prod poll that would
	// hammer the put.io API and risk HTTP 429 rate limiting. Demo mode is
	// exempt (it intentionally sets a 1s poll). 0 means "use the 30s package
	// default", so only a positive sub-5s value is rejected.
	if !demoMode && cfg.TransferCheckInterval > 0 && cfg.TransferCheckInterval < 5*time.Second {
		return fmt.Errorf("transfer_check_interval %s below 5s is not allowed outside demo mode", cfg.TransferCheckInterval)
	}

	// Validate the free-space floor up front so a typo ("10 PB", "abc") fails
	// fast at startup rather than silently disabling the check deep in
	// server.New.
	if _, err := config.ParseByteSize(cfg.MinFreeSpace); err != nil {
		return fmt.Errorf("invalid min_free_space value: %w", err)
	}

	return nil
}

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the download manager",
	Run: func(cmd *cobra.Command, args []string) {
		cfg, logLevel, err := loadConfig(cmd)
		if err != nil {
			log.Fatal("config").Err(err).Msg("Failed to load configuration")
		}

		// Demo mode swaps the real put.io client for an in-process fake that
		// serves synthetic data. Resolved from the flag OR PLDR_DEMO so it
		// works in compose without a flag. It is deliberately NOT a Config
		// field: it's a boot-time control, never persisted, and must never be
		// confusable with production (logged loudly below).
		demoMode, _ := cmd.Flags().GetBool("demo")
		if v := os.Getenv("PLDR_DEMO"); v == "1" || strings.EqualFold(v, "true") {
			demoMode = true
		}

		if logLevel != "" {
			log.SetLevel(log.LogLevel(logLevel))
		}

		log.Debug("startup").
			Str("version", version).
			Str("log_level", logLevel).
			Msg("Starting plundrio")

		// INFO-level so the resolved settings show up in `docker logs` on
		// startup without needing to flip log level. Silent drift between the
		// CLI defaults and the runtime values has produced subtle production
		// bugs (see flagViperBindings); surfacing the values keeps the next
		// instance of that class debuggable in seconds.
		log.Info("config").
			Str("target_dir", cfg.TargetDir).
			Str("putio_folder", cfg.PutioFolder).
			Str("listen_addr", cfg.ListenAddr).
			Int("workers", cfg.WorkerCount).
			Dur("post_complete_retention", cfg.PostCompleteRetention).
			Dur("stall_timeout", cfg.StallTimeout).
			Int("stall_max_retries", cfg.StallMaxRetries).
			Str("min_free_space", cfg.MinFreeSpace).
			Bool("download_start_window_enabled", cfg.DownloadStartWindow.Enabled).
			Str("download_start_window_start", cfg.DownloadStartWindow.Start).
			Str("download_start_window_end", cfg.DownloadStartWindow.End).
			Msg("Configuration loaded")

		if viper.ConfigFileUsed() != "" && viper.IsSet("token") {
			log.Warn("security").
				Str("file", viper.ConfigFileUsed()).
				Msg("OAuth token found in config file - consider using environment variable PLDR_TOKEN instead")
		}

		// In demo mode the OAuth token is irrelevant — the fake client never
		// touches put.io. Require only the values the demo still needs (a
		// target dir to write synthetic downloads into, and a folder name).
		if cfg.TargetDir == "" || cfg.PutioFolder == "" || (!demoMode && cfg.OAuthToken == "") {
			log.Error("config").Msg("Not all required configuration values were provided")
			_ = cmd.Usage()
			os.Exit(1)
		}

		stat, err := os.Stat(cfg.TargetDir)
		if err != nil {
			if os.IsNotExist(err) {
				log.Fatal("config").Str("dir", cfg.TargetDir).Msg("Target directory does not exist")
			}
			log.Fatal("config").Str("dir", cfg.TargetDir).Err(err).Msg("Error checking target directory")
		}
		if !stat.IsDir() {
			log.Fatal("config").Str("dir", cfg.TargetDir).Msg("Target path is not a directory")
		}

		if err := validateRunConfig(cfg, demoMode); err != nil {
			log.Fatal("config").Err(err).Msg("Invalid configuration")
		}

		// Initialize the put.io client. Demo mode swaps in the in-process fake
		// and skips every network call (auth + folder resolution); the fake
		// never reads a real token path. The variable is the interface union so
		// both download.New and server.New accept either implementation.
		var client putioClient
		if demoMode {
			const demoFolderID int64 = 99_000
			log.Warn("demo").
				Bool("fake_data", true).
				Str("target_dir", cfg.TargetDir).
				Msg("=== DEMO MODE: serving synthetic put.io data — NOT a real account, no token used ===")
			demoClient := demo.NewClient(demoFolderID)
			defer demoClient.Stop()
			client = demoClient
			cfg.FolderID = demoFolderID
			// Poll fast in demo mode so the dashboard shows transfers climbing
			// live through put.io-fetch → local-download instead of jumping
			// queued→completed between the production 30s polls (the demo virtual
			// clock advances ~8x, so a 30s poll skips the whole fetch phase).
			if cfg.TransferCheckInterval == 0 {
				cfg.TransferCheckInterval = 1 * time.Second
			}
		} else {
			realClient := api.NewClient(cfg.OAuthToken)

			// Authenticate and get account info
			log.Info("auth").Msg("Authenticating with Put.io...")
			authCtx, authCancel := context.WithTimeout(context.Background(), startupCallTimeout)
			err = realClient.Authenticate(authCtx)
			authCancel()
			if err != nil {
				log.Fatal("auth").Err(err).Msg("Failed to authenticate with Put.io")
			}
			log.Info("auth").Msg("Authentication successful")

			// Create/get folder ID
			log.Info("setup").Str("folder", cfg.PutioFolder).Msg("Setting up Put.io folder")
			folderCtx, folderCancel := context.WithTimeout(context.Background(), startupCallTimeout)
			folderID, err := realClient.EnsureFolder(folderCtx, cfg.PutioFolder)
			folderCancel()
			if err != nil {
				log.Fatal("setup").Str("folder", cfg.PutioFolder).Err(err).Msg("Failed to create/get folder")
			}
			cfg.FolderID = folderID
			log.Info("setup").
				Str("folder", cfg.PutioFolder).
				Int64("folder_id", folderID).
				Msg("Using Put.io folder")
			client = realClient
		}

		// Initialize download manager
		dlManager := download.New(cfg, client)
		dlManager.Start()
		defer dlManager.Stop()
		log.Info("manager").
			Int("workers", cfg.WorkerCount).
			Msg("Download manager started")

		// Initialize and start RPC server
		srv := server.New(cfg, client, dlManager)
		go func() {
			log.Info("server").
				Str("addr", cfg.ListenAddr).
				Msg("Starting transmission-rpc server")
			if err := srv.Start(); err != nil {
				log.Fatal("server").Err(err).Msg("Server error")
			}
		}()

		// Initialize and start the web dashboard listener, if configured.
		// Default-off: when --dashboard is false nothing is constructed, so
		// the dashboard adds zero surface. Unlike the RPC server above, a
		// dashboard ListenAndServe error logs Error and returns — it must NOT
		// log.Fatal, which would kill the daemon and take down the load-bearing
		// RPC path. The dashboard is a second, non-load-bearing face.
		var dash *dashboard.Dashboard
		if cfg.Dashboard {
			overridesPath := resolveOverridesPath(cmd, cfg.TargetDir)
			dash = dashboard.New(cfg, client, dlManager, overridesPath)
			go func() {
				log.Info("dashboard").
					Str("addr", cfg.DashboardAddr).
					Msg("Starting web dashboard")
				if err := dash.Start(); err != nil {
					log.Error("dashboard").Err(err).Msg("Dashboard server error (RPC path unaffected)")
				}
			}()
		}

		// Wait for interrupt signal
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigChan
		log.Info("shutdown").
			Str("signal", sig.String()).
			Msg("Received signal, shutting down...")

		// Cleanup and exit
		log.Info("shutdown").Msg("Stopping download manager...")
		dlManager.Stop()

		log.Info("shutdown").Msg("Stopping server...")
		if err := srv.Stop(); err != nil {
			log.Error("shutdown").Err(err).Msg("Error stopping server")
		}

		if dash != nil {
			log.Info("shutdown").Msg("Stopping dashboard...")
			if err := dash.Stop(); err != nil {
				log.Error("shutdown").Err(err).Msg("Error stopping dashboard")
			}
		}
	},
}

// sampleConfig is the YAML emitted by `generate-config`. Kept as a package
// const (not inline) so a test can parse it and assert every key still maps to
// a config.Config field — the same drift the flagViperBindings comment warns
// about, but for the documented sample. Keep keys in sync with config.Config's
// mapstructure tags.
const sampleConfig = `# Plundrio configuration
# Save as ~/.plundrio.yaml or specify with --config

target: /path/to/downloads   # Target directory for downloads
folder: "plundrio"           # Folder name on Put.io
token: ""                    # Get a token with get-token
listen: ":9091"              # Transmission RPC server address
workers: 4                   # Number of download workers
transfer_check_interval: 0s  # How often to poll put.io for transfer state.
                             # 0 = default (30s); values below 5s are rejected.
post_complete_retention: 24h # Grace before DeleteTransfer on put.io after local
                             # download completes (window for *arr's torrent-remove);
                             # 0 disables — rely on the client to remove.
stall_timeout: 1h            # No-progress window before a put.io transfer is
                             # treated as stalled (then retried, then deleted).
stall_max_retries: 1         # Stall retries before deleting (0 = delete on first stall).
min_free_space: ""           # Opt-in floor on put.io free space (e.g. "20GB",
                             # "10GiB"); reject torrent-add below it. Empty disables.
dashboard: false             # Enable the read-only web dashboard (default off).
dashboard_addr: ":9092"      # Address the dashboard binds when enabled.
download_start_window:       # Optional local download start window
  enabled: false
  start: "23:00"
  end: "05:00"
log_level: "info"            # Log level (debug,info,warn,error,fatal,none)

# Environment variables:
# PLDR_TARGET, PLDR_FOLDER, PLDR_TOKEN, PLDR_LISTEN, PLDR_WORKERS,
# PLDR_TRANSFER_CHECK_INTERVAL, PLDR_POST_COMPLETE_RETENTION,
# PLDR_STALL_TIMEOUT, PLDR_STALL_MAX_RETRIES, PLDR_MIN_FREE_SPACE,
# PLDR_DASHBOARD, PLDR_DASHBOARD_ADDR,
# PLDR_DOWNLOAD_START_WINDOW_ENABLED, PLDR_DOWNLOAD_START_WINDOW_START,
# PLDR_DOWNLOAD_START_WINDOW_END, PLDR_LOG_LEVEL
`

var generateConfigCmd = &cobra.Command{
	Use:   "generate-config",
	Short: "Generate sample configuration file",
	Run: func(cmd *cobra.Command, args []string) {
		cfg := sampleConfig

		outputPath := "plundrio-config.yaml"
		if len(args) > 0 {
			outputPath = args[0]
		}

		log.Debug("config").
			Str("path", outputPath).
			Msg("Generating sample configuration")

		if err := os.WriteFile(outputPath, []byte(cfg), 0644); err != nil {
			log.Fatal("config").
				Str("file", outputPath).
				Err(err).
				Msg("Failed to write config file")
		}
		log.Info("config").
			Str("file", outputPath).
			Msg("Sample config created")
	},
}

var getTokenCmd = &cobra.Command{
	Use:   "get-token",
	Short: "Get OAuth token using device code flow",
	Run: func(cmd *cobra.Command, args []string) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		log.Debug("auth").Msg("Starting OAuth device code flow")

		// Step 1: Get OOB code from Put.io
		resp, err := http.Get("https://api.put.io/v2/oauth2/oob/code?app_id=3270")
		if err != nil {
			log.Fatal("auth").Err(err).Msg("Failed to get OOB code")
		}
		defer func() { _ = resp.Body.Close() }()

		var codeResponse struct {
			Code      string `json:"code"`
			QrCodeURL string `json:"qr_code_url"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&codeResponse); err != nil {
			log.Fatal("auth").Err(err).Msg("Failed to decode code response")
		}

		log.Info("auth").
			Str("code", codeResponse.Code).
			Str("qr_url", codeResponse.QrCodeURL).
			Msg("Visit put.io/link and enter code")
		log.Info("auth").Msg("Waiting for authorization...")

		// Step 2: Poll for token
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				log.Fatal("auth").Msg("Authorization timed out")
			case <-ticker.C:
				tokenResp, err := http.Get("https://api.put.io/v2/oauth2/oob/code/" + codeResponse.Code)
				if err != nil {
					// A transient network blip during the (up to 5-minute) poll
					// must not abort the whole flow — the user may still be
					// entering the code. Log and retry on the next tick; the
					// ctx.Done() case enforces the overall timeout.
					log.Warn("auth").Err(err).Msg("Failed to check authorization status, retrying")
					continue
				}

				var tokenResult struct {
					OAuthToken string `json:"oauth_token"`
					Status     string `json:"status"`
				}
				if err := json.NewDecoder(tokenResp.Body).Decode(&tokenResult); err != nil {
					_ = tokenResp.Body.Close()
					continue
				}
				_ = tokenResp.Body.Close()

				if tokenResult.Status == "OK" && tokenResult.OAuthToken != "" {
					log.Info("auth").
						Str("token", tokenResult.OAuthToken).
						Msg("Successfully obtained access token")
					return
				}

				log.Debug("auth").
					Str("status", tokenResult.Status).
					Msg("Polling for authorization")
			}
		}
	},
}

// registerRunFlags registers the flags accepted by `plundrio run`. Extracted
// from init() so tests can construct fresh cobra commands with the same flag
// shape (pflag/cobra forbid re-registering flags on the same command, so the
// package-global runCmd is unusable across multiple test cases).
func registerRunFlags(cmd *cobra.Command) {
	cmd.Flags().String("config", "", "Config file (default $HOME/.plundrio.yaml)")
	cmd.Flags().StringP("target", "t", "", "Target directory for downloads (required)")
	cmd.Flags().StringP("folder", "f", "plundrio", "Put.io folder name")
	cmd.Flags().StringP("token", "k", "", "Put.io OAuth token (required)")
	cmd.Flags().StringP("listen", "l", ":9091", "Listen address")
	cmd.Flags().IntP("workers", "w", 4, "Number of workers")
	cmd.Flags().Duration("post-complete-retention", 24*time.Hour, "Grace period after local download completes before unilaterally calling DeleteTransfer on put.io. The transfer stays visible to the *arr client as Seeding/100% during this window so it has time to issue torrent-remove. Set to 0 to disable (rely on torrent-remove only — risk: put.io quota leak if RemoveCompletedDownloads=false on the client side).")
	cmd.Flags().Duration("transfer-check-interval", 0, "How often to poll put.io for transfer state. 0 = default (30s). Values below 5s are rejected outside demo mode to avoid hammering the put.io API (risks HTTP 429 rate limiting).")
	cmd.Flags().Duration("stall-timeout", time.Hour, "How long a put.io transfer may sit in a non-terminal status (queued/preparing/downloading) with no progress before it is treated as stalled and retried, then deleted. A no-seeder torrent never reaches ERROR on put.io, so without this *arr waits forever. Keep generous — put.io legitimately queues for minutes.")
	cmd.Flags().Int("stall-max-retries", 1, "How many times a stalled put.io transfer is retried (RetryTransfer) before being deleted. 0 = delete on first stall.")
	cmd.Flags().String("min-free-space", "", "Opt-in floor on put.io free space below which torrent-add is rejected so *arr tries another release. Human-readable (e.g. \"20GB\", \"10GiB\"); empty disables. The check fails open: a put.io account-info error lets the add proceed.")
	cmd.Flags().String("log-level", "", "Log level (debug,info,warn,error,fatal,none)")
	cmd.Flags().Bool("dashboard", false, "Enable the web dashboard (default off). Also via PLDR_DASHBOARD=true.")
	cmd.Flags().String("dashboard-addr", ":9092", "Address the web dashboard binds when enabled. Also via PLDR_DASHBOARD_ADDR.")
	cmd.Flags().String("overrides-file", "", "Path to the runtime-overrides JSON file (default <target>/.plundrio-overrides.json). Also via PLDR_OVERRIDES_FILE.")
	cmd.Flags().Bool("demo", false, "Run with a fake in-process put.io client serving synthetic data (also via PLDR_DEMO=1). For dashboard development/screenshots; never touches a real account or token.")
}

func init() {
	registerRunFlags(runCmd)

	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(getTokenCmd)
	rootCmd.AddCommand(generateConfigCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal("main").Err(err).Msg("Command execution failed")
	}
}
