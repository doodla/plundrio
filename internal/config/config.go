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

	// Dashboard enables the web dashboard HTTP listener. Default-off: the
	// dashboard adds zero surface unless explicitly enabled. Env PLDR_DASHBOARD,
	// flag --dashboard. A deploy-time toggle — not editable via the settings API.
	Dashboard bool `mapstructure:"dashboard"`

	// DashboardAddr is the address the dashboard listener binds when enabled
	// (separate from ListenAddr / the RPC :9091). Default ":9092". Env
	// PLDR_DASHBOARD_ADDR, flag --dashboard-addr. Restart-required; surfaced in
	// the settings API like ListenAddr.
	DashboardAddr string `mapstructure:"dashboard_addr"`

	// WorkerCount is the number of concurrent download workers (default: 4).
	WorkerCount int `mapstructure:"workers"`

	// TransferCheckInterval is how often the download manager polls put.io for
	// transfer state. Zero means "use the package default" (30s — production
	// behavior, unchanged). It exists primarily so demo mode can poll fast
	// (~1-2s) and the live dashboard actually shows transfers climbing through
	// the put.io-fetch → local-download phases instead of jumping
	// queued→completed between slow polls. download.New applies it only when
	// >0, so a default-constructed Config keeps the 30s cadence.
	TransferCheckInterval time.Duration `mapstructure:"transfer_check_interval"`

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

	// StallTimeout is how long a put.io transfer may sit in a non-terminal
	// status (IN_QUEUE/WAITING/PREPARING/DOWNLOADING) without making progress
	// before plundrio treats it as stalled — a torrent with no seeders never
	// reaches ERROR on put.io, so without this guard *arr waits forever.
	// On a stall the transfer is retried up to StallMaxRetries times, then
	// deleted so *arr can grab a different release.
	//
	// Default-construction footgun (same as PostCompleteRetention): the Go zero
	// value (0) means "use the package default" (1h), NOT "stall instantly".
	// download.New applies it only when >0; the CLI flag injects the real 1h
	// default. Keep the threshold generous — put.io legitimately queues for
	// minutes on a cold magnet.
	StallTimeout time.Duration `mapstructure:"stall_timeout"`

	// StallMaxRetries is how many times a stalled transfer is retried before
	// being deleted. 0 = delete on first stall (no retry). Only applied
	// alongside StallTimeout (see download.New); the CLI flag default is 1.
	StallMaxRetries int `mapstructure:"stall_max_retries"`

	// MinFreeSpace is an opt-in floor on put.io free space (AccountInfo.Disk.Avail)
	// below which torrent-add is rejected, so *arr fails the grab and tries
	// another release instead of queueing a download put.io has no room for.
	// Human-readable (e.g. "20GB", "10GiB"); empty string disables the check.
	// Stored as a string (parsed via ParseByteSize at use, like
	// DownloadStartWindow's "HH:MM" strings) so it plumbs through viper/env/yaml
	// with no custom decode hook. The check fails OPEN: if the put.io account
	// lookup errors, the add proceeds — this is a safety floor, not a hard gate.
	MinFreeSpace string `mapstructure:"min_free_space"`
}
