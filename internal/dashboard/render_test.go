package dashboard

import (
	"testing"
	"time"

	"github.com/doodla/go-putio"
	"github.com/doodla/plundrio/internal/download"
	"github.com/doodla/plundrio/internal/transferprog"
)

func ptime(t time.Time) *putio.Time { return &putio.Time{Time: t} }

// TestRenderTransferPutioOnly covers the no-context case: local_phase must be
// nil (serialized null), lifecycle_state "None", and percent_done equal to the
// shared math's put.io-only value.
func TestRenderTransferPutioOnly(t *testing.T) {
	created := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	tr := &putio.Transfer{
		ID: 1, Hash: "abc", Name: "Show.S01E01", Status: "DOWNLOADING",
		Size: 1000, PercentDone: 50, Downloaded: 500, DownloadSpeed: 4096,
		EstimatedTime: 120, CreatedAt: ptime(created),
	}

	dto := renderTransfer(tr, nil, "tv")

	if dto.LocalPhase != nil {
		t.Errorf("local_phase must be nil (null) without a context, got %+v", dto.LocalPhase)
	}
	if dto.LifecycleState != "None" {
		t.Errorf("lifecycle_state = %q, want None", dto.LifecycleState)
	}
	if dto.Category != "tv" {
		t.Errorf("category = %q, want tv", dto.Category)
	}
	// Parity with the shared math.
	want := transferprog.CalculateProgress(transferprog.Input{
		PutioPercentDone: 50, PutioStatus: "DOWNLOADING", PutioSize: 1000,
	})
	if dto.PercentDone != want.PercentDone {
		t.Errorf("percent_done = %v, want %v (shared math)", dto.PercentDone, want.PercentDone)
	}
	if dto.LeftUntilDone != want.LeftUntilDone {
		t.Errorf("left_until_done = %d, want %d (shared math)", dto.LeftUntilDone, want.LeftUntilDone)
	}
	// putio_phase carries the raw 0–100 percent + bytes/speed/eta.
	if dto.PutioPhase.PercentDone != 50 {
		t.Errorf("putio_phase.percent_done = %d, want 50 (raw)", dto.PutioPhase.PercentDone)
	}
	if dto.PutioPhase.Downloaded != 500 {
		t.Errorf("putio_phase.downloaded = %d, want 500", dto.PutioPhase.Downloaded)
	}
	if dto.PutioPhase.ETASeconds != 120 {
		t.Errorf("putio_phase.eta_seconds = %d, want 120", dto.PutioPhase.ETASeconds)
	}
	if dto.CreatedAt == nil || *dto.CreatedAt != "2026-06-05T12:00:00Z" {
		t.Errorf("created_at = %v, want 2026-06-05T12:00:00Z", dto.CreatedAt)
	}
	if dto.FinishedAt != nil {
		t.Errorf("finished_at = %v, want nil (put.io FinishedAt unset)", dto.FinishedAt)
	}
	if dto.ProcessedAt != nil {
		t.Errorf("processed_at = %v, want nil (no ctx)", dto.ProcessedAt)
	}
}

// TestRenderTransferDownloadingWithContext covers the 50/50 split with a tracked
// context: local_phase populated, percent_done equals the shared math, units
// correct (rate_download is the rounded local speed int64 bytes/sec).
func TestRenderTransferDownloadingWithContext(t *testing.T) {
	ctx := download.NewTransferContext(7, 4, download.TransferLifecycleDownloading)
	ctx.SetTotalSize(2000)
	ctx.AddDownloadedBytes(500)
	ctx.SetLocalProgress(8_388_608.4, time.Now().Add(60*time.Second)) // ~8 MiB/s

	tr := &putio.Transfer{
		ID: 7, Hash: "h7", Name: "movie", Status: "COMPLETED",
		Size: 2000, PercentDone: 100,
	}
	dto := renderTransfer(tr, ctx, "movies")

	if dto.LocalPhase == nil {
		t.Fatal("local_phase must be populated with a context")
	}
	if dto.LifecycleState != "Downloading" {
		t.Errorf("lifecycle_state = %q, want Downloading", dto.LifecycleState)
	}
	lp := dto.LocalPhase
	if lp.Downloaded != 500 || lp.Total != 2000 {
		t.Errorf("local_phase downloaded/total = %d/%d, want 500/2000", lp.Downloaded, lp.Total)
	}
	if lp.TotalFiles != 4 {
		t.Errorf("local_phase.total_files = %d, want 4", lp.TotalFiles)
	}
	// rate_download is the rounded speed in bytes/sec (int64).
	if lp.RateDownload != 8388608 {
		t.Errorf("local_phase.rate_download = %d, want 8388608 (rounded)", lp.RateDownload)
	}
	if lp.ETASeconds <= 0 || lp.ETASeconds > 61 {
		t.Errorf("local_phase.eta_seconds = %d, want ~60", lp.ETASeconds)
	}

	want := transferprog.CalculateProgress(transferprog.Input{
		PutioPercentDone: 100, PutioStatus: "COMPLETED", PutioSize: 2000, TransferCtx: ctx,
	})
	if dto.PercentDone != want.PercentDone {
		t.Errorf("percent_done = %v, want %v (shared math)", dto.PercentDone, want.PercentDone)
	}
}

// TestRenderTransferProcessed: a Processed context pins percent_done to 1.0 and
// sets processed_at.
func TestRenderTransferProcessed(t *testing.T) {
	ctx := download.NewTransferContext(9, 1, download.TransferLifecycleProcessed)
	ctx.SetTotalSize(1000)
	ctx.AddDownloadedBytes(1000)
	procAt := time.Date(2026, 6, 5, 13, 0, 0, 0, time.UTC)
	ctx.SetProcessedAtForTest(procAt)

	tr := &putio.Transfer{ID: 9, Hash: "h9", Name: "done", Status: "COMPLETED", Size: 1000, PercentDone: 100}
	dto := renderTransfer(tr, ctx, "")

	if dto.PercentDone != 1.0 {
		t.Errorf("percent_done = %v, want 1.0 (Processed)", dto.PercentDone)
	}
	if dto.LeftUntilDone != 0 {
		t.Errorf("left_until_done = %d, want 0 (Processed)", dto.LeftUntilDone)
	}
	if dto.LifecycleState != "Processed" {
		t.Errorf("lifecycle_state = %q, want Processed", dto.LifecycleState)
	}
	if dto.ProcessedAt == nil || *dto.ProcessedAt != "2026-06-05T13:00:00Z" {
		t.Errorf("processed_at = %v, want 2026-06-05T13:00:00Z", dto.ProcessedAt)
	}
}

// TestRenderTransferPermanentError: a permanently-failed transfer surfaces
// error=true with plundrio's permanent string winning over put.io's
// ErrorMessage; a transient Failed surfaces error=false.
func TestRenderTransferPermanentError(t *testing.T) {
	tc := download.NewTransferCoordinator(func(int64) {})
	ctx := tc.InitiateTransfer(11, "movie", 111, 1)
	ctx.SetTotalSize(1000)
	if err := tc.MarkPermanentlyFailed(11, errFor("retries exhausted")); err != nil {
		t.Fatalf("MarkPermanentlyFailed: %v", err)
	}

	tr := &putio.Transfer{ID: 11, Hash: "h11", Name: "bad", Status: "COMPLETED", Size: 1000, PercentDone: 100, ErrorMessage: "putio says no seeders"}
	dto := renderTransfer(tr, ctx, "")
	if !dto.Error {
		t.Error("error must be true for a permanently-failed transfer")
	}
	if dto.ErrorString != "retries exhausted" {
		t.Errorf("error_string = %q, want plundrio's permanent string (wins over put.io's)", dto.ErrorString)
	}
	if !dto.Permanent {
		t.Error("permanent must be true")
	}

	// Transient Failed: error false, put.io ErrorMessage NOT surfaced as error.
	tc2 := download.NewTransferCoordinator(func(int64) {})
	ctx2 := tc2.InitiateTransfer(12, "movie", 112, 1)
	ctx2.SetTotalSize(1000)
	if err := tc2.FailTransfer(12, errFor("CDN reset")); err != nil {
		t.Fatalf("FailTransfer: %v", err)
	}
	tr2 := &putio.Transfer{ID: 12, Hash: "h12", Name: "retry", Status: "COMPLETED", Size: 1000, PercentDone: 100}
	dto2 := renderTransfer(tr2, ctx2, "")
	if dto2.Error {
		t.Error("transient Failed must report error=false (still retrying)")
	}
	if dto2.Permanent {
		t.Error("transient Failed must report permanent=false")
	}
}

// TestRenderTransferPutioErrorMessage: with no plundrio permanent error, put.io's
// ErrorMessage is the error_string and error=true.
func TestRenderTransferPutioErrorMessage(t *testing.T) {
	tr := &putio.Transfer{ID: 13, Hash: "h13", Name: "err", Status: "ERROR", Size: 1000, PercentDone: 0, ErrorMessage: "no seeders"}
	dto := renderTransfer(tr, nil, "")
	if !dto.Error || dto.ErrorString != "no seeders" {
		t.Errorf("error=%v error_string=%q, want true / 'no seeders'", dto.Error, dto.ErrorString)
	}
}

func errFor(s string) error { return &simpleErr{s} }

type simpleErr struct{ s string }

func (e *simpleErr) Error() string { return e.s }
