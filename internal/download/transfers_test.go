package download

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
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
	snapshot := make(map[string][]*putio.Transfer)
	for _, t := range transfers {
		status := t.Status
		if status == "" {
			status = "COMPLETED"
		}
		snapshot[status] = append(snapshot[status], t)
	}
	p.transfers.Store(&snapshot)
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

	// Invariant check now runs before LoadOrStore, so no retry-state entry
	// should have been created during the worker-drain window.
	if _, ok := m.processor.failedRetryStates.Load(int64(1)); ok {
		t.Errorf("failedRetryStates created entry while workers were in flight; should have deferred before LoadOrStore")
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
	if !ctx.IsPermanent() {
		t.Errorf("ctx.IsPermanent() = false, want true so the server surfaces an error to *arr")
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
	if !ctx.IsPermanent() {
		t.Errorf("ctx.IsPermanent() = false, want true so the server surfaces an error to *arr")
	}
}

func TestProcessFailedTransfersPermanentSkipped(t *testing.T) {
	// Track API calls via atomic counters rather than t.Errorf inside the
	// fake — getAllTransferFilesFn runs from dispatchRequeue's goroutine,
	// and t.Errorf isn't safe from non-test goroutines (race detector
	// flags it). Assert after workerWg.Wait().
	var getFilesCalls, retryCalls atomic.Int32
	client := &fakePutioClient{
		getAllTransferFilesFn: func(fileID int64) ([]*putio.File, error) {
			getFilesCalls.Add(1)
			return nil, nil
		},
		retryTransferFn: func(transferID int64) (*putio.Transfer, error) {
			retryCalls.Add(1)
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

	if got := getFilesCalls.Load(); got != 0 {
		t.Errorf("GetAllTransferFiles called %d times; want 0 for Permanent transfer", got)
	}
	if got := retryCalls.Load(); got != 0 {
		t.Errorf("RetryTransfer called %d times; want 0 for Permanent transfer", got)
	}
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
	if !ctx.IsPermanent() {
		t.Errorf("ctx.IsPermanent() = false, want true so the server surfaces an error to *arr")
	}
}

func TestProcessFailedTransfersIgnoresNonFailedStates(t *testing.T) {
	// See TestProcessFailedTransfersPermanentSkipped for why we count
	// instead of t.Errorf'ing inside the fake.
	var getFilesCalls atomic.Int32
	client := &fakePutioClient{
		getAllTransferFilesFn: func(fileID int64) ([]*putio.File, error) {
			getFilesCalls.Add(1)
			return nil, nil
		},
	}
	m := newTestManagerWithClient(client)
	defer func() {
		if got := getFilesCalls.Load(); got != 0 {
			t.Errorf("GetAllTransferFiles called %d times; want 0 for non-Failed transfers", got)
		}
	}()

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

// TestProcessFailedTransfersDoesNotTriggerCleanupForOnDiskFiles is a
// regression for the cleanup-race bug. Setup: a 3-file transfer where file A
// completed on the prior attempt (and is still on disk), B and C failed.
// Pre-fix RequeueFailedTransfer preserved completedFiles=1; queueTransferFiles
// then re-detected A on disk and called FileCompleted again, double-counting
// to 2. When B's new worker eventually completed (3 == total, failed == 0),
// the Completed transition fired and the cleanup hook deleted the put.io
// file out from under C's still-running worker.
//
// After the fix, RequeueFailedTransfer zeros all per-file counters and
// queueTransferFiles re-establishes them from disk. A is counted once, B and
// C are queued, no cleanup hook fires until B and C actually complete.
func TestProcessFailedTransfersDoesNotTriggerCleanupForOnDiskFiles(t *testing.T) {
	targetDir := t.TempDir()
	transferName := "test-transfer"
	fileAName := "file-a.bin"
	fileASize := int64(123)

	// Plant file A on disk at the path queueTransferFiles will check.
	transferDir := filepath.Join(targetDir, transferName)
	if err := os.MkdirAll(transferDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(transferDir, fileAName), make([]byte, fileASize), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	files := []*putio.File{
		{ID: 101, Name: fileAName, Size: fileASize},
		{ID: 102, Name: "file-b.bin", Size: 200},
		{ID: 103, Name: "file-c.bin", Size: 300},
	}
	client := &fakePutioClient{
		getAllTransferFilesFn: func(fileID int64) ([]*putio.File, error) {
			return files, nil
		},
	}
	m := newTestManagerWithClientAndTargetDir(client, targetDir)

	// Drive the transfer to Failed with completed=1 (A succeeded), failed=2
	// (B and C). This is the state pre-requeue would have left behind.
	m.coordinator.InitiateTransfer(1, transferName, 999, 3)
	if err := m.coordinator.StartDownload(1); err != nil {
		t.Fatalf("StartDownload: %v", err)
	}
	if err := m.coordinator.FileCompleted(1); err != nil {
		t.Fatalf("FileCompleted: %v", err)
	}
	if err := m.coordinator.FileFailure(1); err != nil {
		t.Fatalf("FileFailure: %v", err)
	}
	if err := m.coordinator.FileFailure(1); err != nil {
		t.Fatalf("FileFailure: %v", err)
	}

	ctx, _ := m.coordinator.GetTransferContext(1)
	if ctx.GetState() != TransferLifecycleFailed {
		t.Fatalf("precondition: state = %s, want Failed", ctx.GetState())
	}

	setPutioTransfers(m.processor, &putio.Transfer{
		ID:     1,
		Name:   transferName,
		FileID: 999,
		Status: "COMPLETED",
	})

	m.processor.processFailedTransfersAt(time.Now())
	m.workerWg.Wait()

	// After requeue + queueTransferFiles: A is counted once, B and C are
	// queued (no worker running, so they sit in m.jobs).
	completed, failed, total := ctx.GetCounters()
	if completed != 1 || failed != 0 || total != 3 {
		t.Errorf("counters = (%d, %d, %d), want (1, 0, 3) — A on disk counted exactly once",
			completed, failed, total)
	}

	// State must NOT have advanced past Downloading (no completion check
	// can have fired).
	if state := ctx.GetState(); state != TransferLifecycleDownloading {
		t.Errorf("state = %s, want Downloading (cleanup must not have fired)", state)
	}

	// The cleanup hook deletes the put.io source file. If it fired, the
	// fake client would have recorded a DeleteFile call.
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.deleteFileCalls) != 0 {
		t.Errorf("cleanup hook fired prematurely: DeleteFile calls = %v", client.deleteFileCalls)
	}
	if len(client.deleteTransferCalls) != 0 {
		t.Errorf("cleanup hook fired prematurely: DeleteTransfer calls = %v", client.deleteTransferCalls)
	}

	// B and C should be in the queue (FileID 102 and 103).
	if got := len(m.jobs); got != 2 {
		t.Errorf("queued jobs = %d, want 2 (B and C)", got)
	}
}

// TestProcessFailedTransfersClearsRetryStateOnSuccess verifies the
// cleanup-hook entry that prunes failedRetryStates. Without it, every
// retried transfer leaves a *localRetryState behind in the sync.Map for
// the life of the process — unbounded growth on a long-running daemon.
func TestProcessFailedTransfersClearsRetryStateOnSuccess(t *testing.T) {
	targetDir := t.TempDir()
	transferName := "test-transfer"
	fileName := "file.bin"
	fileSize := int64(50)

	// Plant the file on disk so queueTransferFiles short-circuits to
	// FileCompleted and CompleteTransfer fires synchronously in the
	// dispatch goroutine.
	if err := os.MkdirAll(filepath.Join(targetDir, transferName), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, transferName, fileName), make([]byte, fileSize), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	client := &fakePutioClient{
		getAllTransferFilesFn: func(fileID int64) ([]*putio.File, error) {
			return []*putio.File{{ID: 200, Name: fileName, Size: fileSize}}, nil
		},
	}
	m := newTestManagerWithClientAndTargetDir(client, targetDir)

	driveTransferToFailed(t, m, 1, 1)
	setPutioTransfers(m.processor, &putio.Transfer{
		ID:     1,
		Name:   transferName,
		FileID: 999,
		Status: "COMPLETED",
	})

	m.processor.processFailedTransfersAt(time.Now())
	m.workerWg.Wait()

	if _, ok := m.processor.failedRetryStates.Load(int64(1)); ok {
		t.Errorf("failedRetryStates still contains entry for transfer 1; expected cleanup hook to have removed it")
	}

	ctx, _ := m.coordinator.GetTransferContext(1)
	if ctx.GetState() != TransferLifecycleProcessed {
		t.Errorf("state = %s, want Processed (precondition for cleanup-hook firing)", ctx.GetState())
	}
}

// TestProcessFailedTransfersStaysFailedOnGetFilesError exercises the
// dispatchRequeue ordering rule: if GetAllTransferFiles errors, the transfer
// must remain in Failed with completed+failed == total so the in-flight
// invariant still holds and the next backoff tick can retry. Flipping state
// before the API call would have left the transfer stuck (failedFiles still
// 0, invariant fails, every future tick defers).
func TestProcessFailedTransfersStaysFailedOnGetFilesError(t *testing.T) {
	var callCount int
	client := &fakePutioClient{
		getAllTransferFilesFn: func(fileID int64) ([]*putio.File, error) {
			callCount++
			if callCount == 1 {
				return nil, errors.New("put.io 503")
			}
			return []*putio.File{makePutioFile(1, 100)}, nil
		},
	}
	m := newTestManagerWithClient(client)

	driveTransferToFailed(t, m, 1, 1)
	setPutioTransfers(m.processor, &putio.Transfer{ID: 1, FileID: 999, Status: "COMPLETED"})

	base := time.Now()
	// First tick: Count == 0 path, so no backoff gate.
	m.processor.processFailedTransfersAt(base)
	m.workerWg.Wait()

	ctx, _ := m.coordinator.GetTransferContext(1)
	if ctx.GetState() != TransferLifecycleFailed {
		t.Errorf("state after API error = %s, want Failed", ctx.GetState())
	}
	completed, failed, total := ctx.GetCounters()
	if completed != 0 || failed != 1 || total != 1 {
		t.Errorf("counters after API error = (%d, %d, %d), want (0, 1, 1) — invariant must still hold",
			completed, failed, total)
	}
	rs := getRetryStateForTest(t, m.processor, 1)
	if rs.Count != 1 {
		t.Errorf("Count = %d, want 1 (must have been bumped on monitor goroutine)", rs.Count)
	}

	// Second tick after the 5-minute backoff: the cascade must be able to
	// retry. This proves the transfer wasn't stuck.
	m.processor.processFailedTransfersAt(base.Add(6 * time.Minute))
	m.workerWg.Wait()

	if callCount != 2 {
		t.Errorf("GetAllTransferFiles call count = %d, want 2 (second tick should have retried)", callCount)
	}
	rs = getRetryStateForTest(t, m.processor, 1)
	if rs.Count != 2 {
		t.Errorf("Count after second tick = %d, want 2", rs.Count)
	}
	if ctx.GetState() != TransferLifecycleDownloading {
		t.Errorf("state after second tick = %s, want Downloading", ctx.GetState())
	}
}

// TestCheckTransfersAndGetTransfersRace exercises the publish/read pattern on
// TransferProcessor.transfers under -race. checkTransfers swaps the snapshot
// each tick; GetTransfers reads it on every *arr poll. Before the
// atomic.Pointer switch, this was a write-vs-read on a plain map field. The
// test only asserts no race / no panic — the contents reader sees on a given
// call are intentionally racy in time but must always be a coherent snapshot.
func TestCheckTransfersAndGetTransfersRace(t *testing.T) {
	// Two snapshots that differ in length and Status keys. checkTransfers
	// alternates between them, so any torn read would surface as a slice
	// length mismatch or a transfer with the wrong SaveParentID — but mostly
	// the test is here to give -race something to catch. Statuses are
	// limited to IN_QUEUE/WAITING/DOWNLOADING/PREPARING to avoid the
	// side-effect-heavy ready/errored paths; this test is about the
	// publish/read pattern, not transfer processing.
	snapshots := [][]*putio.Transfer{
		{
			{ID: 1, Status: "DOWNLOADING", SaveParentID: 42},
			{ID: 2, Status: "WAITING", SaveParentID: 42},
			{ID: 3, Status: "PREPARING", SaveParentID: 42},
		},
		{
			{ID: 1, Status: "DOWNLOADING", SaveParentID: 42},
			{ID: 4, Status: "IN_QUEUE", SaveParentID: 42},
		},
	}
	var pick atomic.Int32
	client := &fakePutioClient{
		getTransfersFn: func() ([]*putio.Transfer, error) {
			n := pick.Add(1) - 1
			return snapshots[int(n)%len(snapshots)], nil
		},
	}
	m := newTestManagerWithClient(client)
	m.processor.folderID = 42

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// One writer goroutine driving checkTransfers in a tight loop.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				m.processor.checkTransfers()
			}
		}
	}()

	// Several reader goroutines hammering GetTransfers.
	const readers = 8
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					ts := m.processor.GetTransfers()
					// Coherent-snapshot check: every transfer returned must
					// belong to folderID. A torn read could surface as a stale
					// pointer with a wrong SaveParentID.
					for _, tr := range ts {
						if tr.SaveParentID != 42 {
							t.Errorf("got transfer with SaveParentID=%d, want 42", tr.SaveParentID)
							return
						}
					}
				}
			}
		}()
	}

	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// processErroredTransfers covers a different path from
// processFailedTransfers — see boundary comments in transfers.go. These
// tests exercise that path end-to-end: retries up to the cap, deletion
// after the cap, API errors during RetryTransfer, and the no-op behavior
// for transfers from a different folder.

func setErroredTransfers(p *TransferProcessor, transfers ...*putio.Transfer) {
	snapshot := map[string][]*putio.Transfer{"ERROR": transfers}
	p.transfers.Store(&snapshot)
}

func TestProcessErroredTransfersRetriesUpToCap(t *testing.T) {
	client := &fakePutioClient{
		retryTransferFn: func(id int64) (*putio.Transfer, error) {
			return &putio.Transfer{ID: id, Status: "IN_QUEUE"}, nil
		},
	}
	m := newTestManagerWithClient(client)

	transfer := &putio.Transfer{ID: 1, SaveParentID: m.processor.folderID, Status: "ERROR", Name: "x"}
	setErroredTransfers(m.processor, transfer)

	// Three retry ticks, each must call RetryTransfer.
	for i := 0; i < 3; i++ {
		m.processor.processErroredTransfers()
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if got := len(client.retryTransferCalls); got != 3 {
		t.Errorf("RetryTransfer calls = %d, want 3", got)
	}
	if len(client.deleteTransferCalls) != 0 {
		t.Errorf("DeleteTransfer must not run before cap; got %v", client.deleteTransferCalls)
	}
}

func TestProcessErroredTransfersDeletesAfterCap(t *testing.T) {
	client := &fakePutioClient{
		retryTransferFn: func(id int64) (*putio.Transfer, error) {
			return &putio.Transfer{ID: id, Status: "IN_QUEUE"}, nil
		},
	}
	m := newTestManagerWithClient(client)

	transfer := &putio.Transfer{ID: 1, SaveParentID: m.processor.folderID, Status: "ERROR", Name: "x"}
	setErroredTransfers(m.processor, transfer)

	// 3 retry ticks + 1 delete tick = 4 calls total. After the cap is
	// reached, the next tick deletes (no further RetryTransfer call).
	for i := 0; i < 4; i++ {
		m.processor.processErroredTransfers()
	}

	client.mu.Lock()
	defer client.mu.Unlock()
	if got := len(client.retryTransferCalls); got != 3 {
		t.Errorf("RetryTransfer calls = %d, want 3 (cap)", got)
	}
	if len(client.deleteTransferCalls) != 1 || client.deleteTransferCalls[0] != 1 {
		t.Errorf("DeleteTransfer calls = %v, want [1]", client.deleteTransferCalls)
	}
	// retryAttempts entry was cleared after successful delete.
	if _, ok := m.processor.retryAttempts.Load(int64(1)); ok {
		t.Errorf("retryAttempts entry must be cleared after delete")
	}
}

func TestProcessErroredTransfersToleratesRetryAPIError(t *testing.T) {
	// RetryTransfer returns an error; the retry counter still advances
	// (the failure was logged at error level), and a future tick will try
	// again until the cap. This tests the err != nil branch.
	client := &fakePutioClient{
		retryTransferFn: func(id int64) (*putio.Transfer, error) {
			return nil, errors.New("upstream busy")
		},
	}
	m := newTestManagerWithClient(client)

	transfer := &putio.Transfer{ID: 1, SaveParentID: m.processor.folderID, Status: "ERROR", Name: "x"}
	setErroredTransfers(m.processor, transfer)

	m.processor.processErroredTransfers()

	client.mu.Lock()
	defer client.mu.Unlock()
	if got := len(client.retryTransferCalls); got != 1 {
		t.Errorf("RetryTransfer calls = %d, want 1", got)
	}
	count, _ := m.processor.retryAttempts.Load(int64(1))
	if count != 1 {
		t.Errorf("retryAttempts after API error = %v, want 1 (counter advances on attempt, regardless of error)", count)
	}
}

func TestProcessErroredTransfersToleratesNilReturnFromRetry(t *testing.T) {
	// Some put.io API responses return (nil, nil) for RetryTransfer.
	// The success path used to dereference retried.Status unconditionally
	// and would panic; this test guards the nil-safe rewrite.
	client := &fakePutioClient{
		retryTransferFn: func(id int64) (*putio.Transfer, error) {
			return nil, nil
		},
	}
	m := newTestManagerWithClient(client)

	transfer := &putio.Transfer{ID: 1, SaveParentID: m.processor.folderID, Status: "ERROR", Name: "x"}
	setErroredTransfers(m.processor, transfer)

	// Must not panic.
	m.processor.processErroredTransfers()
}

func TestProcessErroredTransfersDeleteFailureKeepsCounter(t *testing.T) {
	// If DeleteTransfer fails on the post-cap tick, the retry counter
	// must NOT be cleared — otherwise the next tick re-enters the
	// retry-up-to-cap loop and would re-call RetryTransfer 3 more times.
	client := &fakePutioClient{
		retryTransferFn: func(id int64) (*putio.Transfer, error) {
			return &putio.Transfer{ID: id, Status: "IN_QUEUE"}, nil
		},
		deleteTransferErr: errors.New("upstream"),
	}
	m := newTestManagerWithClient(client)

	transfer := &putio.Transfer{ID: 1, SaveParentID: m.processor.folderID, Status: "ERROR", Name: "x"}
	setErroredTransfers(m.processor, transfer)

	for i := 0; i < 4; i++ {
		m.processor.processErroredTransfers()
	}
	if v, ok := m.processor.retryAttempts.Load(int64(1)); !ok || v.(int) != 3 {
		t.Errorf("retryAttempts = %v (loaded=%v), want 3 (counter must persist across failed delete)", v, ok)
	}
}
