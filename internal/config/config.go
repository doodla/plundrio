package config

import "time"

// DownloadStartWindowConfig gates when new local downloads may begin.
// It only affects the start of local downloads, not ongoing transfers.
type DownloadStartWindowConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Start   string `mapstructure:"start"`
	End     string `mapstructure:"end"`
}

// Config holds the runtime configuration.
//
// The mapstructure tags are the canonical key form: they match the YAML
// keys in the generated sample config, the env-var suffixes under the
// PLDR_ prefix (after SetEnvKeyReplacer normalizes "_" <-> "-"), and the
// keys that cmd/plundrio/main.go binds CLI flags under via viper.BindPFlag.
// Drift between these surfaces is the bug that produced v0.10.11/12's silent
// retention-janitor no-op — keep them in sync.
type Config struct {
	// TargetDir is where completed downloads will be stored.
	TargetDir string `mapstructure:"target"`

	// PutioFolder is the name of the folder in Put.io.
	PutioFolder string `mapstructure:"folder"`

	// FolderID is the Put.io folder ID (resolved at startup; not read from
	// config).
	FolderID int64 `mapstructure:"-"`

	// OAuthToken is the Put.io OAuth token. May also be supplied via
	// PLDR_TOKEN_FILE for systemd LoadCredential-style delivery; see
	// resolveOAuthToken in cmd/plundrio.
	OAuthToken string `mapstructure:"token"`

	// ListenAddr is the address to listen for transmission-rpc requests.
	ListenAddr string `mapstructure:"listen"`

	// DashboardListen is the address the web dashboard's HTTP listener binds
	// to (separate from ListenAddr / the RPC :9091). Empty disables the
	// dashboard entirely — default-off, no new listener. Env
	// PLDR_DASHBOARD_LISTEN, flag --dashboard-listen.
	DashboardListen string `mapstructure:"dashboard_listen"`

	// WorkerCount is the number of concurrent download workers (default: 4).
	WorkerCount int `mapstructure:"workers"`

	// DownloadStartWindow optionally restricts when new local downloads may start.
	DownloadStartWindow DownloadStartWindowConfig `mapstructure:"download_start_window"`

	// PostCompleteRetention is the grace period between local download
	// completion and unilateral DeleteTransfer on put.io. While the timer
	// runs, the transfer remains visible in torrent-get as Seeding/100% so
	// the *arr client (Sonarr/Radarr/etc.) has a window to observe completion
	// and issue torrent-remove. Zero disables the safety net entirely —
	// transfers stay until the client removes them, which leaks put.io
	// quota if the client is misconfigured (RemoveCompletedDownloads=false)
	// or absent.
	//
	// Default-construction footgun: the Go zero value (0) means "disabled,"
	// not "use a sane default." The CLI in cmd/plundrio/main.go injects 24h
	// via the flag default. Any future entry point that constructs Config
	// directly must set this field explicitly or the janitor silently
	// won't run.
	PostCompleteRetention time.Duration `mapstructure:"post_complete_retention"`
}
