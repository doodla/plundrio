package download

import (
	"errors"
	"testing"
	"time"

	"github.com/elsbrock/go-putio"
)

// driveTransferToFailed runs a transfer through Initiate -> StartDownload ->
// FileFailure for every file so it lands in TransferLifecycleFailed with
// completed+failed == total (the in-flight invariant satisfied).
func driveTransferToFailed(t *testing.T, m *Manager, transferID int64, fileCount int) {
	t.Helper()
	m.coordinator.InitiateTransfer(transferID, "test-transfer", 999, fileCount)
	if err := m.coordinator.StartDownload(transferID); err != nil {
		t.Fatalf("StartDownload: %v", err)
	}
	for i := 0; i < fileCount; i++ {
		if err := m.coordinator.FileFailure(transferID); err != nil {
			t.Fatalf("FileFailure: %v", err)
		}
	}
	ctx, _ := m.coordinator.GetTransferContext(transferID)
	if ctx.GetState() != TransferLifecycleFailed {
		t.Fatalf("precondition: state = %s, want Failed", ctx.GetState())
	}
}

// putRetryStateForTest seeds a localRetryState into the processor's map. Used
// to set Count, PutioFallback, etc. before invoking processFailedTransfersAt.
func putRetryStateForTest(p *TransferProcessor, id int64, rs *localRetryState) {
	p.failedRetryStates.Store(id, rs)
}

func getRetryStateForTest(t *testing.T, p *TransferProcessor, id int64) *localRetryState {
	t.Helper()
	v, ok := p.failedRetryStates.Load(id)
	if !ok {
		t.Fatalf("no localRetryState for transfer %d", id)
	}
	return v.(*localRetryState)
}

// setPutioTransfers seeds the processor's per-status transfer cache so
// findPutioTransferByID returns a known *putio.Transfer for the given ID.
// Status "COMPLETED" by default; pass an explicit status when the test cares.
func setPutioTransfers(p *TransferProcessor, transfers ...*putio.Transfer) {
	p.transfers = make(map[string][]*putio.Transfer)
	for _, t := range transfers {
		status := t.Status
		if status == "" {
			status = "COMPLETED"
		}
		p.transfers[status] = append(p.transfers[status], t)
	}
}

// makePutioFile returns a putio.File with a non-existent name so
// shouldDownloadFile reports true (the file isn't on disk, so it'll be
// queued rather than counted as completed-already).
func makePutioFile(id int64, size int64) *putio.File {
	return &putio.File{
		ID:   id,
		Name: "nonexistent-test-file.bin",
		Size: size,
	}
}

func TestProcessFailedTransfersRequeuesImmediatelyOnFirstFailure(t *testing.T) {
	client := &fakePutioClient{
		getAllTransferFilesFn: func(fileID int64) ([]*putio.File, error) {
			return []*putio.File{makePutioFile(1, 100)}, nil
		},
	}
	m := newTestManagerWithClient(client)

	driveTransferToFailed(t, m, 1, 2)
	setPutioTransfers(m.processor, &putio.Transfer{ID: 1, FileID: 999, Status: "COMPLETED"})

	// Count==0 with zero-value LastAttempt: requeue should fire regardless of now.
	now := time.Now()
	m.processor.processFailedTransfersAt(now)
	m.workerWg.Wait()

	rs := getRetryStateForTest(t, m.processor, 1)
	if rs.Count != 1 {
		t.Errorf("Count = %d, want 1", rs.Count)
	}
	if !rs.LastAttempt.Equal(now) {
		t.Errorf("LastAttempt = %v, want %v", rs.LastAttempt, now)
	}
	ctx, _ := m.coordinator.GetTransferContext(1)
	if ctx.GetState() != TransferLifecycleDownloading {
		t.Errorf("state = %s, want Downloading", ctx.GetState())
	}
}

func TestProcessFailedTransfersRespectsBackoff(t *testing.T) {
	client := &fakePutioClient{
		getAllTransferFilesFn: func(fileID int64) ([]*putio.File, error) {
			return []*putio.File{makePutioFile(1, 100)}, nil
		},
	}
	m := newTestManagerWithClient(client)

	driveTransferToFailed(t, m, 1, 1)
	setPutioTransfers(m.processor, &putio.Transfer{ID: 1, FileID: 999, Status: "COMPLETED"})

	base := time.Now()
	// Count==1 with LastAttempt at base. Backoff[0] is 5m.
	putRetryStateForTest(m.processor, 1, &localRetryState{
		Count:       1,
		LastAttempt: base,
	})

	// 1 minute in: skip (< 5m).
	m.processor.processFailedTransfersAt(base.Add(1 * time.Minute))
	m.workerWg.Wait()
	rs := getRetryStateForTest(t, m.processor, 1)
	if rs.Count != 1 {
		t.Errorf("Count after early tick = %d, want 1 (no bump)", rs.Count)
	}
	ctx, _ := m.coordinator.GetTransferContext(1)
	if ctx.GetState() != TransferLifecycleFailed {
		t.Errorf("state after early tick = %s, want Failed", ctx.GetState())
	}

	// 6 minutes in: requeue (> 5m).
	m.processor.processFailedTransfersAt(base.Add(6 * time.Minute))
	m.workerWg.Wait()
	rs = getRetryStateForTest(t, m.processor, 1)
	if rs.Count != 2 {
		t.Errorf("Count after backoff tick = %d, want 2", rs.Count)
	}
	if ctx.GetState() != TransferLifecycleDownloading {
		t.Errorf("state after backoff tick = %s, want Downloading", ctx.GetState())
	}
}

func TestProcessFailedTransfersDefersWhenWorkersInFlight(t *testing.T) {
	client := &fakePutioClient{}
	m := newTestManagerWithClient(client)

	// Initiate, start, fail one file out of three -> Failed but completed+failed (0+1) < 3.
	m.coordinator.InitiateTransfer(1, "test", 999, 3)
	if err := m.coordinator.StartDownload(1); err != nil {
		t.Fatalf("StartDownload: %v", err)
	}
	if err := m.coordinator.FileFailure(1); err != nil {
		t.Fatalf("FileFailure: %v", err)
	}
	setPutioTransfers(m.processor, &putio.Transfer{ID: 1, FileID: 999, Status: "COMPLETED"})

	m.processor.processFailedTransfersAt(time.Now())
	m.workerWg.Wait()

	// Should have created the retry state (LoadOrStore happens before invariant
	// check), but Count must still be 0.
	rs := getRetryStateForTest(t, m.processor, 1)
	if rs.Count != 0 {
		t.Errorf("Count = %d, want 0 (no bump while workers in flight)", rs.Count)
	}
	ctx, _ := m.coordinator.GetTransferContext(1)
	if ctx.GetState() != TransferLifecycleFailed {
		t.Errorf("state = %s, want Failed (no requeue)", ctx.GetState())
	}
}

func TestProcessFailedTransfersFallsBackToPutioAfterCap(t *testing.T) {
	client := &fakePutioClient{}
	m := newTestManagerWithClient(client)

	driveTransferToFailed(t, m, 1, 1)
	setPutioTransfers(m.processor, &putio.Transfer{ID: 1, FileID: 999, Status: "COMPLETED"})

	base := time.Now()
	// Count==maxLocalRetryAttempts, last attempt 3h ago (past 2h backoff).
	putRetryStateForTest(m.processor, 1, &localRetryState{
		Count:       maxLocalRetryAttempts,
		LastAttempt: base.Add(-3 * time.Hour),
	})

	m.processor.processFailedTransfersAt(base)
	m.workerWg.Wait()

	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.retryTransferCalls) != 1 || client.retryTransferCalls[0] != 1 {
		t.Errorf("RetryTransfer calls = %v, want [1]", client.retryTransferCalls)
	}

	rs := getRetryStateForTest(t, m.processor, 1)
	if !rs.PutioFallback {
		t.Errorf("PutioFallback = false, want true")
	}
	if !rs.PutioFallbackAt.Equal(base) {
		t.Errorf("PutioFallbackAt = %v, want %v", rs.PutioFallbackAt, base)
	}
	if rs.Count != maxLocalRetryAttempts {
		t.Errorf("Count = %d, want %d (must not reset on fallback)", rs.Count, maxLocalRetryAttempts)
	}
}

func TestProcessFailedTransfersWaitsAfterPutioFallback(t *testing.T) {
	client := &fakePutioClient{}
	m := newTestManagerWithClient(client)

	driveTransferToFailed(t, m, 1, 1)
	setPutioTransfers(m.processor, &putio.Transfer{ID: 1, FileID: 999, Status: "COMPLETED"})

	base := time.Now()
	// PutioFallback set 5 minutes ago; window is 30m, so we should skip.
	putRetryStateForTest(m.processor, 1, &localRetryState{
		Count:           maxLocalRetryAttempts,
		PutioFallback:   true,
		PutioFallbackAt: base.Add(-5 * time.Minute),
	})

	m.processor.processFailedTransfersAt(base)
	m.workerWg.Wait()

	client.mu.Lock()
	if len(client.retryTransferCalls) != 0 {
		t.Errorf("RetryTransfer calls during wait = %v, want none", client.retryTransferCalls)
	}
	client.mu.Unlock()

	rs := getRetryStateForTest(t, m.processor, 1)
	if rs.PutioFallbackRetried {
		t.Errorf("PutioFallbackRetried = true, want false (still in wait window)")
	}
	if rs.Permanent {
		t.Errorf("Permanent = true, want false")
	}
	ctx, _ := m.coordinator.GetTransferContext(1)
	if ctx.GetState() != TransferLifecycleFailed {
		t.Errorf("state = %s, want Failed (still waiting)", ctx.GetState())
	}
}

func TestProcessFailedTransfersPermanentFailWhenPutioStuck(t *testing.T) {
	client := &fakePutioClient{}
	m := newTestManagerWithClient(client)

	driveTransferToFailed(t, m, 1, 1)
	// put.io status still ERROR after the wait window.
	setPutioTransfers(m.processor, &putio.Transfer{ID: 1, FileID: 999, Status: "ERROR"})

	base := time.Now()
	putRetryStateForTest(m.processor, 1, &localRetryState{
		Count:           maxLocalRetryAttempts,
		PutioFallback:   true,
		PutioFallbackAt: base.Add(-1 * time.Hour),
	})

	m.processor.processFailedTransfersAt(base)
	m.workerWg.Wait()

	rs := getRetryStateForTest(t, m.processor, 1)
	if !rs.Permanent {
		t.Errorf("Permanent = false, want true (put.io did not recover)")
	}
	ctx, _ := m.coordinator.GetTransferContext(1)
	if ctx.GetError() == nil {
		t.Errorf("err = nil, want non-nil after permanent fail")
	}
	if ctx.GetState() != TransferLifecycleFailed {
		t.Errorf("state = %s, want Failed", ctx.GetState())
	}
}

func TestProcessFailedTransfersRequeueAfterPutioRecovery(t *testing.T) {
	client := &fakePutioClient{
		getAllTransferFilesFn: func(fileID int64) ([]*putio.File, error) {
			return []*putio.File{makePutioFile(1, 100)}, nil
		},
	}
	m := newTestManagerWithClient(client)

	driveTransferToFailed(t, m, 1, 1)
	setPutioTransfers(m.processor, &putio.Transfer{ID: 1, FileID: 999, Status: "COMPLETED"})

	base := time.Now()
	putRetryStateForTest(m.processor, 1, &localRetryState{
		Count:           maxLocalRetryAttempts,
		PutioFallback:   true,
		PutioFallbackAt: base.Add(-1 * time.Hour),
	})

	m.processor.processFailedTransfersAt(base)
	m.workerWg.Wait()

	rs := getRetryStateForTest(t, m.processor, 1)
	if !rs.PutioFallbackRetried {
		t.Errorf("PutioFallbackRetried = false, want true after recovery requeue")
	}
	if rs.Permanent {
		t.Errorf("Permanent = true, want false (we just requeued)")
	}
	ctx, _ := m.coordinator.GetTransferContext(1)
	if ctx.GetState() != TransferLifecycleDownloading {
		t.Errorf("state = %s, want Downloading after recovery requeue", ctx.GetState())
	}
}

func TestProcessFailedTransfersPostFallbackPermanentOnSecondFailure(t *testing.T) {
	client := &fakePutioClient{}
	m := newTestManagerWithClient(client)

	driveTransferToFailed(t, m, 1, 1)
	setPutioTransfers(m.processor, &putio.Transfer{ID: 1, FileID: 999, Status: "COMPLETED"})

	base := time.Now()
	// Both fallback flags set: we already used our one post-fallback retry
	// and we're back in Failed. Next tick must mark permanent.
	putRetryStateForTest(m.processor, 1, &localRetryState{
		Count:                maxLocalRetryAttempts,
		PutioFallback:        true,
		PutioFallbackAt:      base.Add(-2 * time.Hour),
		PutioFallbackRetried: true,
	})

	m.processor.processFailedTransfersAt(base)
	m.workerWg.Wait()

	rs := getRetryStateForTest(t, m.processor, 1)
	if !rs.Permanent {
		t.Errorf("Permanent = false, want true after post-fallback failure")
	}
	ctx, _ := m.coordinator.GetTransferContext(1)
	if ctx.GetError() == nil {
		t.Errorf("err = nil, want non-nil after permanent fail")
	}
}

func TestProcessFailedTransfersPermanentSkipped(t *testing.T) {
	client := &fakePutioClient{
		getAllTransferFilesFn: func(fileID int64) ([]*putio.File, error) {
			t.Errorf("GetAllTransferFiles must not be called for Permanent transfers")
			return nil, nil
		},
		retryTransferFn: func(transferID int64) (*putio.Transfer, error) {
			t.Errorf("RetryTransfer must not be called for Permanent transfers")
			return nil, nil
		},
	}
	m := newTestManagerWithClient(client)

	driveTransferToFailed(t, m, 1, 1)
	setPutioTransfers(m.processor, &putio.Transfer{ID: 1, FileID: 999, Status: "ERROR"})

	putRetryStateForTest(m.processor, 1, &localRetryState{
		Permanent: true,
	})

	m.processor.processFailedTransfersAt(time.Now())
	m.workerWg.Wait()
}

func TestProcessFailedTransfersHandlesDeletedPutioTransfer(t *testing.T) {
	client := &fakePutioClient{}
	m := newTestManagerWithClient(client)

	driveTransferToFailed(t, m, 1, 1)
	// No put.io transfers cached -> findPutioTransferByID returns nil.
	setPutioTransfers(m.processor)

	m.processor.processFailedTransfersAt(time.Now())
	m.workerWg.Wait()

	rs := getRetryStateForTest(t, m.processor, 1)
	if !rs.Permanent {
		t.Errorf("Permanent = false, want true when put.io transfer is gone")
	}
	ctx, _ := m.coordinator.GetTransferContext(1)
	if ctx.GetError() == nil {
		t.Errorf("err = nil, want non-nil")
	}
}

func TestProcessFailedTransfersIgnoresNonFailedStates(t *testing.T) {
	client := &fakePutioClient{
		getAllTransferFilesFn: func(fileID int64) ([]*putio.File, error) {
			t.Errorf("GetAllTransferFiles must not be called for non-Failed states")
			return nil, nil
		},
	}
	m := newTestManagerWithClient(client)

	// Initial
	m.coordinator.InitiateTransfer(1, "initial", 100, 1)

	// Downloading
	m.coordinator.InitiateTransfer(2, "downloading", 200, 1)
	if err := m.coordinator.StartDownload(2); err != nil {
		t.Fatalf("StartDownload: %v", err)
	}

	// Completed (then leave it Completed by NOT calling CompleteTransfer)
	m.coordinator.InitiateTransfer(3, "completed", 300, 1)
	if err := m.coordinator.StartDownload(3); err != nil {
		t.Fatalf("StartDownload: %v", err)
	}
	if err := m.coordinator.FileCompleted(3); err != nil {
		t.Fatalf("FileCompleted: %v", err)
	}

	setPutioTransfers(m.processor,
		&putio.Transfer{ID: 1, Status: "WAITING"},
		&putio.Transfer{ID: 2, Status: "DOWNLOADING"},
		&putio.Transfer{ID: 3, Status: "COMPLETED"},
	)

	m.processor.processFailedTransfersAt(time.Now())
	m.workerWg.Wait()

	// No retry state should have been created for any of them.
	for _, id := range []int64{1, 2, 3} {
		if _, ok := m.processor.failedRetryStates.Load(id); ok {
			t.Errorf("failedRetryStates should not contain %d (state was not Failed)", id)
		}
	}
}

func TestProcessFailedTransfersRetryTransferAPIError(t *testing.T) {
	client := &fakePutioClient{
		retryTransferFn: func(transferID int64) (*putio.Transfer, error) {
			return nil, errors.New("put.io 503")
		},
	}
	m := newTestManagerWithClient(client)

	driveTransferToFailed(t, m, 1, 1)
	setPutioTransfers(m.processor, &putio.Transfer{ID: 1, FileID: 999, Status: "ERROR"})

	base := time.Now()
	putRetryStateForTest(m.processor, 1, &localRetryState{
		Count:       maxLocalRetryAttempts,
		LastAttempt: base.Add(-3 * time.Hour),
	})

	m.processor.processFailedTransfersAt(base)
	m.workerWg.Wait()

	rs := getRetryStateForTest(t, m.processor, 1)
	if !rs.PutioFallback {
		t.Errorf("PutioFallback = false, want true even when RetryTransfer errors")
	}
	if !rs.PutioFallbackAt.Equal(base) {
		t.Errorf("PutioFallbackAt = %v, want %v", rs.PutioFallbackAt, base)
	}
	if rs.Permanent {
		t.Errorf("Permanent = true, want false (not yet — we'll wait the window)")
	}
}
