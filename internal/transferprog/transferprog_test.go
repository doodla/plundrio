package transferprog

import (
	"errors"
	"testing"

	"github.com/doodla/plundrio/internal/download"
)

// newTestTransferCtx creates a TransferContext for testing with the given parameters.
func newTestTransferCtx(state download.TransferLifecycleState, totalFiles int32, completedFiles int32, totalSize int64, downloadedSize int64) *download.TransferContext {
	ctx := download.NewTransferContext(0, totalFiles, state)
	ctx.SetTotalSize(totalSize)
	ctx.AddDownloadedBytes(downloadedSize)
	// completedFiles is unexported and only the same-package coordinator can
	// advance it; cross-package tests that need a nonzero completedFiles rely
	// instead on totalSize/downloadedSize (the primary progress path). The
	// file-count fallback (totalSize==0) is covered in coordinator_test.go.
	return ctx
}

func TestCalculateProgress(t *testing.T) {
	tests := []struct {
		name              string
		input             Input
		wantPercentDone   float64
		wantStatus        int
		wantLeftUntilDone int64
	}{
		// ---------------------------------------------------------------
		// No context, put.io only — maps 0–100% → 0–50%
		// ---------------------------------------------------------------
		{
			name: "no context, putio 0%",
			input: Input{
				PutioPercentDone: 0,
				PutioStatus:      "DOWNLOADING",
				PutioSize:        1000,
			},
			wantPercentDone:   0.0,
			wantStatus:        TrStatusDownload,
			wantLeftUntilDone: 1000,
		},
		{
			name: "no context, putio 50%",
			input: Input{
				PutioPercentDone: 50,
				PutioStatus:      "DOWNLOADING",
				PutioSize:        1000,
			},
			wantPercentDone:   0.25,
			wantStatus:        TrStatusDownload,
			wantLeftUntilDone: 500,
		},
		{
			name: "no context, putio 100%",
			input: Input{
				PutioPercentDone: 100,
				PutioStatus:      "DOWNLOADING",
				PutioSize:        1000,
			},
			wantPercentDone:   0.5,
			wantStatus:        TrStatusDownload,
			wantLeftUntilDone: 0,
		},
		// ---------------------------------------------------------------
		// No context, terminal put.io statuses → 100%
		// ---------------------------------------------------------------
		{
			name: "no context, COMPLETED status",
			input: Input{
				PutioPercentDone: 100,
				PutioStatus:      "COMPLETED",
				PutioSize:        1000,
			},
			wantPercentDone:   1.0,
			wantStatus:        TrStatusSeed,
			wantLeftUntilDone: 0,
		},
		{
			name: "no context, SEEDING status",
			input: Input{
				PutioPercentDone: 100,
				PutioStatus:      "SEEDING",
				PutioSize:        1000,
			},
			wantPercentDone:   1.0,
			wantStatus:        TrStatusSeed,
			wantLeftUntilDone: 0,
		},
		// ---------------------------------------------------------------
		// With context, partial local download (50/50 split)
		// ---------------------------------------------------------------
		{
			name: "with context, putio 100% + local 50%",
			input: Input{
				PutioPercentDone: 100,
				PutioStatus:      "COMPLETED",
				PutioSize:        1000,
				TransferCtx:      newTestTransferCtx(download.TransferLifecycleDownloading, 2, 1, 1000, 500),
			},
			wantPercentDone:   0.75, // 0.5 (putio) + 0.25 (local 500/1000 * 0.5)
			wantStatus:        TrStatusDownload,
			wantLeftUntilDone: 500, // 0 putio + 500 local
		},
		{
			name: "with context, putio 50% + local 0%",
			input: Input{
				PutioPercentDone: 50,
				PutioStatus:      "DOWNLOADING",
				PutioSize:        2000,
				TransferCtx:      newTestTransferCtx(download.TransferLifecycleDownloading, 4, 0, 2000, 0),
			},
			wantPercentDone:   0.25, // 0.25 (putio) + 0.0 (local)
			wantStatus:        TrStatusDownload,
			wantLeftUntilDone: 3000, // 1000 putio + 2000 local
		},
		// ---------------------------------------------------------------
		// With context, processed state → 100%, status=seed
		// ---------------------------------------------------------------
		{
			name: "with context, processed state",
			input: Input{
				PutioPercentDone: 100,
				PutioStatus:      "COMPLETED",
				PutioSize:        1000,
				TransferCtx:      newTestTransferCtx(download.TransferLifecycleProcessed, 3, 3, 1000, 1000),
			},
			wantPercentDone:   1.0,
			wantStatus:        TrStatusSeed,
			wantLeftUntilDone: 0,
		},
		// ---------------------------------------------------------------
		// With context, completed state → uses mapPutioStatus
		// ---------------------------------------------------------------
		{
			name: "with context, completed state + COMPLETED status",
			input: Input{
				PutioPercentDone: 100,
				PutioStatus:      "COMPLETED",
				PutioSize:        1000,
				TransferCtx:      newTestTransferCtx(download.TransferLifecycleCompleted, 2, 2, 1000, 1000),
			},
			wantPercentDone:   1.0, // 0.5 + 0.5
			wantStatus:        TrStatusSeed,
			wantLeftUntilDone: 0,
		},
		{
			name: "with context, completed state + IN_QUEUE status",
			input: Input{
				PutioPercentDone: 100,
				PutioStatus:      "IN_QUEUE",
				PutioSize:        1000,
				TransferCtx:      newTestTransferCtx(download.TransferLifecycleCompleted, 1, 1, 1000, 1000),
			},
			wantPercentDone:   1.0,
			wantStatus:        TrStatusDownloadWaiting,
			wantLeftUntilDone: 0,
		},
		// ---------------------------------------------------------------
		// Zero size falls back to file count progress
		// Note: completedFiles is 0 from cross-package constructor,
		// so local progress is 0 (not 0.25 as with same-package access).
		// The file-count path is tested more thoroughly in coordinator_test.go.
		// ---------------------------------------------------------------
		{
			name: "with context, zero size uses file count (cross-package: completedFiles=0)",
			input: Input{
				PutioPercentDone: 100,
				PutioStatus:      "COMPLETED",
				PutioSize:        0,
				TransferCtx:      newTestTransferCtx(download.TransferLifecycleDownloading, 4, 0, 0, 0),
			},
			wantPercentDone:   0.5, // 0.5 (putio) + 0.0 (0/4 * 0.5)
			wantStatus:        TrStatusDownload,
			wantLeftUntilDone: 0, // both putio and local are 0 bytes
		},
		// ---------------------------------------------------------------
		// Edge cases
		// ---------------------------------------------------------------
		{
			name: "with context, zero size + zero files",
			input: Input{
				PutioPercentDone: 50,
				PutioStatus:      "DOWNLOADING",
				PutioSize:        1000,
				// TotalFiles is 0 → falls through to no-context path
				TransferCtx: download.NewTransferContext(0, 0, download.TransferLifecycleDownloading),
			},
			// No context path because TotalFiles == 0, status is DOWNLOADING
			wantPercentDone:   0.25,
			wantStatus:        TrStatusDownload,
			wantLeftUntilDone: 500,
		},
		{
			name: "with context, putio 100% + local 0%",
			input: Input{
				PutioPercentDone: 100,
				PutioStatus:      "COMPLETED",
				PutioSize:        5000,
				TransferCtx:      newTestTransferCtx(download.TransferLifecycleDownloading, 10, 0, 5000, 0),
			},
			wantPercentDone:   0.5, // 0.5 (putio) + 0.0 (local)
			wantStatus:        TrStatusDownload,
			wantLeftUntilDone: 5000, // 0 putio + 5000 local
		},
		{
			name: "no context, IN_QUEUE status",
			input: Input{
				PutioPercentDone: 0,
				PutioStatus:      "IN_QUEUE",
				PutioSize:        2000,
			},
			wantPercentDone:   0.0,
			wantStatus:        TrStatusDownloadWaiting,
			wantLeftUntilDone: 2000,
		},
		{
			name: "no context, ERROR status",
			input: Input{
				PutioPercentDone: 30,
				PutioStatus:      "ERROR",
				PutioSize:        1000,
			},
			wantPercentDone:   0.15,
			wantStatus:        TrStatusStopped,
			wantLeftUntilDone: 700,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateProgress(tt.input)

			const epsilon = 1e-9
			if diff := got.PercentDone - tt.wantPercentDone; diff > epsilon || diff < -epsilon {
				t.Errorf("PercentDone = %f, want %f", got.PercentDone, tt.wantPercentDone)
			}
			if got.Status != tt.wantStatus {
				t.Errorf("Status = %d, want %d", got.Status, tt.wantStatus)
			}
			if got.LeftUntilDone != tt.wantLeftUntilDone {
				t.Errorf("LeftUntilDone = %d, want %d", got.LeftUntilDone, tt.wantLeftUntilDone)
			}
		})
	}
}

// TestCalculateProgressPermanentFailureSurfaces covers the path that lets *arr
// give up: a transfer in TransferLifecycleFailed with the permanent flag set
// (cascade exhausted in transfers.go) must report Stopped + non-empty Error.
// A Failed transfer *without* the permanent flag is still being retried and
// must look like Downloading so progress doesn't regress mid-cascade.
func TestCalculateProgressPermanentFailureSurfaces(t *testing.T) {
	// Use a real coordinator to set the permanent flag — there's no
	// cross-package setter, which is the point: only the cascade can declare
	// a transfer permanently failed.
	tc := download.NewTransferCoordinator(func(int64) {})
	ctx := tc.InitiateTransfer(1, "movie", 999, 1)
	ctx.SetTotalSize(1000)
	if err := tc.MarkPermanentlyFailed(1, errors.New("retries exhausted post put.io fallback")); err != nil {
		t.Fatalf("MarkPermanentlyFailed: %v", err)
	}

	got := CalculateProgress(Input{
		PutioPercentDone: 100,
		PutioStatus:      "COMPLETED",
		PutioSize:        1000,
		TransferCtx:      ctx,
	})
	if got.Status != TrStatusStopped {
		t.Errorf("Status = %d, want TrStatusStopped (%d)", got.Status, TrStatusStopped)
	}
	if got.Error == "" {
		t.Errorf("Error = %q, want non-empty so handler can populate Transmission errorString", got.Error)
	}

	// Transient Failed (still being retried) should NOT surface as stopped —
	// no permanent flag → looks like Downloading so the cascade can keep
	// working without flapping the *arr UI.
	tc2 := download.NewTransferCoordinator(func(int64) {})
	ctx2 := tc2.InitiateTransfer(2, "movie", 999, 1)
	ctx2.SetTotalSize(1000)
	if err := tc2.FailTransfer(2, errors.New("CDN reset")); err != nil {
		t.Fatalf("FailTransfer: %v", err)
	}
	got2 := CalculateProgress(Input{
		PutioPercentDone: 100,
		PutioStatus:      "COMPLETED",
		PutioSize:        1000,
		TransferCtx:      ctx2,
	})
	if got2.Status != TrStatusDownload {
		t.Errorf("transient Failed: Status = %d, want TrStatusDownload (%d)", got2.Status, TrStatusDownload)
	}
	if got2.Error != "" {
		t.Errorf("transient Failed: Error = %q, want empty (still retrying)", got2.Error)
	}
}
