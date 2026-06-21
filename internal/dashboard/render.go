package dashboard

import (
	"math"
	"time"

	"github.com/doodla/go-putio"
	"github.com/doodla/plundrio/internal/download"
	"github.com/doodla/plundrio/internal/transferprog"
)

// This file is the SINGLE place the contract <Transfer> DTO is built, so the SSE
// `transfers` event, snapshot.transfers, and GET /api/transfers produce
// byte-identical shapes. The combined progress reuses transferprog (the same
// two-phase math :9091 calls) so the dashboard cannot drift from the RPC path.

// TransferDTO is the frozen <Transfer> render object (contract §Two-phase
// transfer model). Field names and JSON tags match the contract verbatim.
type TransferDTO struct {
	ID       int64  `json:"id"`
	Hash     string `json:"hash"`
	Name     string `json:"name"`
	Category string `json:"category"`

	Size           int    `json:"size"`
	PutioStatus    string `json:"putio_status"`
	LifecycleState string `json:"lifecycle_state"`

	PercentDone   float64 `json:"percent_done"`
	LeftUntilDone int64   `json:"left_until_done"`

	PutioPhase PutioPhaseDTO  `json:"putio_phase"`
	LocalPhase *LocalPhaseDTO `json:"local_phase"`

	Error       bool   `json:"error"`
	ErrorString string `json:"error_string"`
	Permanent   bool   `json:"permanent"`

	ProcessedAt *string `json:"processed_at"`
	CreatedAt   *string `json:"created_at"`
	FinishedAt  *string `json:"finished_at"`
}

// PutioPhaseDTO is the 0–50% leg (raw put.io fields).
type PutioPhaseDTO struct {
	PercentDone  int   `json:"percent_done"` // 0–100 int (raw put.io)
	Downloaded   int64 `json:"downloaded"`
	RateDownload int   `json:"rate_download"`
	ETASeconds   int64 `json:"eta_seconds"`
}

// LocalPhaseDTO is the 50–100% leg; null on the wire when no TransferContext.
type LocalPhaseDTO struct {
	Downloaded     int64 `json:"downloaded"`
	Total          int64 `json:"total"`
	CompletedFiles int32 `json:"completed_files"`
	FailedFiles    int32 `json:"failed_files"`
	TotalFiles     int32 `json:"total_files"`
	RateDownload   int64 `json:"rate_download"`
	ETASeconds     int64 `json:"eta_seconds"`
}

// renderTransfer builds the DTO for one put.io transfer plus its optional local
// context. category is Manager.GetCategory(t.Hash). ctx is nil when the download
// manager hasn't started local tracking yet (transfer still in put.io's fetch
// phase) — in that case local_phase serializes as null.
func renderTransfer(t *putio.Transfer, ctx *download.TransferContext, category string) TransferDTO {
	prog := transferprog.CalculateProgress(transferprog.Input{
		PutioPercentDone: t.PercentDone,
		PutioStatus:      t.Status,
		PutioSize:        t.Size,
		TransferCtx:      ctx,
	})

	dto := TransferDTO{
		ID:            t.ID,
		Hash:          t.Hash,
		Name:          t.Name,
		Category:      category,
		Size:          t.Size,
		PutioStatus:   t.Status,
		PercentDone:   prog.PercentDone,
		LeftUntilDone: prog.LeftUntilDone,
		PutioPhase: PutioPhaseDTO{
			PercentDone:  t.PercentDone,
			Downloaded:   t.Downloaded,
			RateDownload: t.DownloadSpeed,
			ETASeconds:   t.EstimatedTime,
		},
		CreatedAt:  rfc3339OrNil(timeOf(t.CreatedAt)),
		FinishedAt: rfc3339OrNil(timeOf(t.FinishedAt)),
	}

	// lifecycle_state: ctx.GetState().String() OR "None" when no ctx.
	if ctx != nil {
		dto.LifecycleState = ctx.GetState().String()
		dto.Permanent = ctx.IsPermanent()
		dto.ProcessedAt = rfc3339OrNil(ctx.GetProcessedAt())
		dto.LocalPhase = renderLocalPhase(ctx)
	} else {
		dto.LifecycleState = "None"
	}

	// Error precedence (shared with torrent.go:handleTorrentGet via
	// transferprog): plundrio's permanent failure string wins over put.io's own
	// error fields. error_string = prog.Error if the cascade marked it
	// permanent, else put.io's synthesized reason (tracker/error/status). A
	// transient Failed (still retrying) reports error:false and stays
	// mid-progress, identical to the RPC path.
	errString := transferprog.PutioErrorString(t)
	if prog.Error != "" {
		errString = prog.Error
	}
	dto.ErrorString = errString
	dto.Error = errString != ""

	return dto
}

// renderLocalPhase builds the 50–100% leg from a tracked context. ETA mirrors
// torrent.go: max(0, round(time.Until(localETA).Seconds())), 0 when localETA is
// zero.
func renderLocalPhase(ctx *download.TransferContext) *LocalPhaseDTO {
	downloaded, total, completed, failed := ctx.GetProgress()
	speed, localETA := ctx.GetLocalProgress()

	lp := &LocalPhaseDTO{
		Downloaded:     downloaded,
		Total:          total,
		CompletedFiles: completed,
		FailedFiles:    failed,
		TotalFiles:     ctx.TotalFiles,
		RateDownload:   int64(math.Round(speed)),
	}
	if !localETA.IsZero() {
		if secs := int64(math.Round(time.Until(localETA).Seconds())); secs > 0 {
			lp.ETASeconds = secs
		}
	}
	return lp
}

// timeOf dereferences a *putio.Time to a time.Time (zero if nil).
func timeOf(pt *putio.Time) time.Time {
	if pt == nil {
		return time.Time{}
	}
	return pt.Time
}

// rfc3339OrNil renders t as an RFC3339 string pointer, or nil when t is the zero
// value (contract: null when never set).
func rfc3339OrNil(t time.Time) *string {
	if t.IsZero() {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}
