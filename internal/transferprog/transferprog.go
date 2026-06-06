// Package transferprog holds the single, shared two-phase progress computation
// for a put.io transfer. It was moved out of internal/server/progress.go so the
// RPC server (:9091) and the web dashboard build the same numbers from the same
// code: a transfer is at exactly one combined percent, whichever face reports it.
//
// Progress is split 50/50:
//   - put.io fetching the torrent (0–50%)
//   - plundrio downloading it locally from put.io (50–100%)
//
// The exported names (Input, Result, CalculateProgress, MapPutioStatusValue and
// the TrStatus* Transmission codes) are the same logic the server package used
// under unexported names; the server now imports them.
package transferprog

import (
	"time"

	"github.com/doodla/plundrio/internal/download"
)

// Transmission status constants. These are the Transmission RPC status codes the
// server reports to *arr; the dashboard ignores them (it renders lifecycle_state
// instead) but they ride along in Result so the server path is unchanged.
const (
	TrStatusStopped         = 0
	TrStatusDownloadWaiting = 3
	TrStatusDownload        = 4
	TrStatusSeed            = 6
)

// Input holds the data needed to calculate transfer progress.
type Input struct {
	// Put.io side
	PutioPercentDone int    // 0–100
	PutioStatus      string // e.g. "DOWNLOADING", "COMPLETED", "SEEDING"
	PutioSize        int    // total torrent size in bytes

	// Local side (nil when no transfer context exists)
	TransferCtx *download.TransferContext
}

// Result contains the calculated progress values.
type Result struct {
	PercentDone   float64   // 0.0–1.0
	Status        int       // Transmission status code
	LeftUntilDone int64     // bytes remaining
	LocalETA      time.Time // local ETA override (zero if not applicable)
	LocalSpeed    float64   // local download speed override in bytes/sec (0 if not applicable)
	// Error is populated when plundrio's retry cascade has permanently
	// abandoned the transfer (vs. put.io's own ErrorMessage, which the
	// caller already surfaces). The handler maps this to Transmission
	// error=true so *arr's Failed Download Handling fires instead of
	// waiting for a transfer plundrio is no longer working on.
	Error string
}

// CalculateProgress computes the combined progress for a transfer.
//
// When a transfer context exists it indicates the transfer is actively being
// tracked by the download manager. Otherwise we rely solely on the Put.io
// transfer metadata.
func CalculateProgress(in Input) Result {
	// When we have a transfer context with files, calculate the 50/50 split.
	if in.TransferCtx != nil && in.TransferCtx.TotalFiles > 0 {
		return calculateProgressWithContext(in)
	}

	// Completed/seeding on Put.io without local context → already done.
	if in.PutioStatus == "COMPLETED" || in.PutioStatus == "SEEDING" {
		return Result{
			PercentDone:   1.0,
			LeftUntilDone: 0,
			Status:        TrStatusSeed,
		}
	}

	// No context — put.io only progress (0–50%).
	putioProgress := float64(in.PutioPercentDone) / 200.0
	leftUntilDone := int64(float64(in.PutioSize) * (1.0 - float64(in.PutioPercentDone)/100.0))

	return Result{
		PercentDone:   putioProgress,
		LeftUntilDone: leftUntilDone,
		Status:        MapPutioStatusValue(in.PutioStatus),
	}
}

// calculateProgressWithContext handles the case where we have a local transfer context.
func calculateProgressWithContext(in Input) Result {
	ctx := in.TransferCtx

	downloadedSize, totalSize, completedFiles, _ := ctx.GetProgress()
	totalFiles := ctx.TotalFiles // write-once, safe without lock
	state := ctx.GetState()
	localSpeed, localETA := ctx.GetLocalProgress()

	// Put.io progress (0–50%)
	putioProgress := float64(in.PutioPercentDone) / 200.0

	// Local download progress (0–50%)
	var localProgress float64
	if totalSize > 0 {
		localProgress = float64(downloadedSize) / float64(totalSize) * 0.5
	} else if totalFiles > 0 {
		localProgress = float64(completedFiles) / float64(totalFiles) * 0.5
	}

	percentDone := putioProgress + localProgress

	// Bytes left on Put.io side
	putioLeftBytes := int64(float64(in.PutioSize) * (1.0 - float64(in.PutioPercentDone)/100.0))
	// Bytes left on local side
	localLeftBytes := totalSize - downloadedSize
	leftUntilDone := putioLeftBytes + localLeftBytes
	if leftUntilDone < 0 {
		leftUntilDone = 0
	}

	var status int
	var permanentErr string
	switch state {
	case download.TransferLifecycleProcessed:
		percentDone = 1.0
		leftUntilDone = 0
		status = TrStatusSeed
	case download.TransferLifecycleCompleted:
		status = MapPutioStatusValue(in.PutioStatus)
	case download.TransferLifecycleFailed:
		// Only report failure to *arr once the cascade has given up. A
		// transient Failed (still being retried) reports as Downloading so
		// progress doesn't regress.
		if ctx.IsPermanent() {
			status = TrStatusStopped
			if e := ctx.GetError(); e != nil {
				permanentErr = e.Error()
			} else {
				permanentErr = "transfer permanently failed"
			}
		} else {
			status = TrStatusDownload
		}
	default:
		status = TrStatusDownload
	}

	result := Result{
		PercentDone:   percentDone,
		Status:        status,
		LeftUntilDone: leftUntilDone,
		Error:         permanentErr,
	}

	if !localETA.IsZero() {
		result.LocalETA = localETA
		result.LocalSpeed = localSpeed
	}

	return result
}

// MapPutioStatusValue maps a Put.io transfer status string to a Transmission status code.
func MapPutioStatusValue(status string) int {
	switch status {
	case "IN_QUEUE":
		return TrStatusDownloadWaiting
	case "DOWNLOADING", "COMPLETING":
		return TrStatusDownload
	case "SEEDING", "COMPLETED":
		return TrStatusSeed
	case "ERROR":
		return TrStatusStopped
	default:
		return TrStatusStopped
	}
}
