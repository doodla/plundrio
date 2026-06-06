package server

import (
	"github.com/doodla/plundrio/internal/transferprog"
)

// The two-phase progress math moved to internal/transferprog so the RPC server
// and the web dashboard share one implementation (see design §6). These aliases
// keep the server's call sites (torrent.go) unchanged while the single source of
// truth lives in the shared package. The dashboard imports transferprog directly.
type (
	progressInput  = transferprog.Input
	progressResult = transferprog.Result
)

// Transmission status constants, re-exported from the shared package so the
// server's handlers and tests keep referring to them by their original names.
const (
	trStatusStopped         = transferprog.TrStatusStopped
	trStatusDownloadWaiting = transferprog.TrStatusDownloadWaiting
	trStatusDownload        = transferprog.TrStatusDownload
	trStatusSeed            = transferprog.TrStatusSeed
)

// calculateProgress delegates to the shared two-phase implementation.
func calculateProgress(in progressInput) progressResult {
	return transferprog.CalculateProgress(in)
}
