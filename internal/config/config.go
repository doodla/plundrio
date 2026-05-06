package config

import "time"

// DownloadStartWindowConfig gates when new local downloads may begin.
// It only affects the start of local downloads, not ongoing transfers.
type DownloadStartWindowConfig struct {
	Enabled bool
	Start   string
	End     string
}

// Config holds the runtime configuration
type Config struct {
	// TargetDir is where completed downloads will be stored
	TargetDir string

	// PutioFolder is the name of the folder in Put.io
	PutioFolder string

	// FolderID is the Put.io folder ID (set after creation/lookup)
	FolderID int64

	// OAuthToken is the Put.io OAuth token
	OAuthToken string

	// ListenAddr is the address to listen for transmission-rpc requests
	ListenAddr string

	// WorkerCount is the number of concurrent download workers (default: 4)
	WorkerCount int

	// DownloadStartWindow optionally restricts when new local downloads may start.
	DownloadStartWindow DownloadStartWindowConfig

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
	PostCompleteRetention time.Duration
}
