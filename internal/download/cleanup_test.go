package download

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/doodla/go-putio"
	"github.com/doodla/plundrio/internal/api"
	"github.com/doodla/plundrio/internal/config"
)

// fakePutioClient records calls and returns canned errors. Each method
// has an optional override for tests that need to simulate a specific
// failure mode (e.g. 404 on DeleteFile).
type fakePutioClient struct {
	mu sync.Mutex

	deleteFileCalls     []int64
	deleteTransferCalls []int64
	retryTransferCalls  []int64

	deleteFileErr     error
	deleteTransferErr error

	// Optional programmable hooks for tests that need richer behavior
	// (transfers_test.go uses these to drive the failed-retry cascade).
	getAllTransferFilesFn func(fileID int64) ([]api.TransferFile, error)
	retryTransferFn       func(transferID int64) (*putio.Transfer, error)
	getTransfersFn        func() ([]*putio.Transfer, error)
}

func (f *fakePutioClient) GetTransfers(ctx context.Context) ([]*putio.Transfer, error) {
	f.mu.Lock()
	fn := f.getTransfersFn
	f.mu.Unlock()
	if fn != nil {
		return fn()
	}
	return nil, nil
}
func (f *fakePutioClient) GetAllTransferFiles(ctx context.Context, fileID int64) ([]api.TransferFile, error) {
	if f.getAllTransferFilesFn != nil {
		return f.getAllTransferFilesFn(fileID)
	}
	return nil, nil
}
func (f *fakePutioClient) RetryTransfer(ctx context.Context, transferID int64) (*putio.Transfer, error) {
	f.mu.Lock()
	f.retryTransferCalls = append(f.retryTransferCalls, transferID)
	fn := f.retryTransferFn
	f.mu.Unlock()
	if fn != nil {
		return fn(transferID)
	}
	return nil, nil
}
func (f *fakePutioClient) GetDownloadURL(ctx context.Context, fileID int64) (string, error) {
	return "", nil
}
func (f *fakePutioClient) DeleteFile(ctx context.Context, fileID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleteFileCalls = append(f.deleteFileCalls, fileID)
	return f.deleteFileErr
}
func (f *fakePutioClient) DeleteTransfer(ctx context.Context, transferID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleteTransferCalls = append(f.deleteTransferCalls, transferID)
	return f.deleteTransferErr
}

// putioErrorWith builds a wrapped *putio.ErrorResponse with the given
// HTTP status code, matching the shape returned by the real client
// (fmt.Errorf with %w). The Request field must be populated because
// ErrorResponse.Error() dereferences it during log formatting.
func putioErrorWith(prefix string, statusCode int) error {
	req, _ := http.NewRequest(http.MethodGet, "https://api.put.io/v2/files/999", nil)
	resp := &http.Response{StatusCode: statusCode, Request: req}
	return fmt.Errorf("%s: %w", prefix, &putio.ErrorResponse{Response: resp})
}

func notFoundError(prefix string) error {
	return putioErrorWith(prefix, http.StatusNotFound)
}

func TestIsNotFoundError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"plain_error", errors.New("boom"), false},
		{"wrapped_404", notFoundError("delete file"), true},
		{"wrapped_500", putioErrorWith("api", http.StatusInternalServerError), false},
		{
			"putio_error_with_nil_response",
			&putio.ErrorResponse{Response: nil},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isNotFoundError(tt.err); got != tt.want {
				t.Errorf("isNotFoundError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// newTestManagerWithClient builds a Manager with a fake PutioClient and the
// real cleanup hook wiring from New(), so cleanup-hook behavior can be
// exercised end-to-end without the actual put.io API.
func newTestManagerWithClient(client PutioClient) *Manager {
	cfg := &config.Config{TargetDir: "/tmp/plundrio-test", WorkerCount: 1}
	return New(cfg, client)
}

// newTestManagerWithClientAndTargetDir is like newTestManagerWithClient but
// uses the given target dir, for tests that need real on-disk files (e.g.
// the queueTransferFiles on-disk-file path in shouldDownloadFile).
func newTestManagerWithClientAndTargetDir(client PutioClient, targetDir string) *Manager {
	cfg := &config.Config{TargetDir: targetDir, WorkerCount: 1}
	return New(cfg, client)
}

// driveTransferToCleanup runs the coordinator state machine through to the
// point where cleanup hooks fire: Initiate → StartDownload → FileCompleted →
// CompleteTransfer. The fileCount of 1 keeps the test focused on cleanup,
// not file-by-file accounting.
func driveTransferToCleanup(t *testing.T, m *Manager, transferID, fileID int64) {
	t.Helper()
	m.coordinator.InitiateTransfer(transferID, "test-transfer", fileID, 1)
	if err := m.coordinator.StartDownload(transferID); err != nil {
		t.Fatalf("StartDownload: %v", err)
	}
	if err := m.coordinator.FileCompleted(transferID); err != nil {
		t.Fatalf("FileCompleted: %v", err)
	}
	if err := m.coordinator.CompleteTransfer(transferID); err != nil {
		t.Fatalf("CompleteTransfer: %v", err)
	}
}

func TestCompleteTransferDoesNotPurgePutio(t *testing.T) {
	// CompleteTransfer used to call DeleteFile + DeleteTransfer on put.io
	// synchronously, which violated the *arr Transmission contract: the
	// transfer would vanish from torrent-get before Radarr's next poll
	// could observe it as Completed and trigger Import. The put.io delete
	// is now deferred to PurgeTransfer (driven by torrent-remove RPC or
	// the retention janitor).
	client := &fakePutioClient{}
	m := newTestManagerWithClient(client)

	driveTransferToCleanup(t, m, 1, 100)

	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.deleteFileCalls) != 0 {
		t.Errorf("CompleteTransfer must not call DeleteFile; got %v", client.deleteFileCalls)
	}
	if len(client.deleteTransferCalls) != 0 {
		t.Errorf("CompleteTransfer must not call DeleteTransfer; got %v", client.deleteTransferCalls)
	}
	// State still advances and the local-state cleanup hook still runs:
	// the failedRetryStates entry (if any) was Delete()'d.
	ctx, ok := m.coordinator.GetTransferContext(1)
	if !ok {
		t.Fatal("transfer context missing after CompleteTransfer")
	}
	if ctx.GetState() != TransferLifecycleProcessed {
		t.Errorf("state = %s, want Processed", ctx.GetState())
	}
	if ctx.GetProcessedAt().IsZero() {
		t.Error("processedAt should be set after CompleteTransfer")
	}
}

func TestPurgeTransferDeletesFileThenTransfer(t *testing.T) {
	client := &fakePutioClient{}
	m := newTestManagerWithClient(client)

	driveTransferToCleanup(t, m, 1, 100)

	if err := m.PurgeTransfer(1); err != nil {
		t.Fatalf("PurgeTransfer: %v", err)
	}

	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.deleteFileCalls) != 1 || client.deleteFileCalls[0] != 100 {
		t.Errorf("DeleteFile calls = %v, want [100]", client.deleteFileCalls)
	}
	if len(client.deleteTransferCalls) != 1 || client.deleteTransferCalls[0] != 1 {
		t.Errorf("DeleteTransfer calls = %v, want [1]", client.deleteTransferCalls)
	}
	// Coordinator context should be dropped so the in-memory map doesn't
	// grow unbounded across the process lifetime.
	if _, ok := m.coordinator.GetTransferContext(1); ok {
		t.Error("coordinator context should be dropped after PurgeTransfer")
	}
}

func TestPurgeTransferTolerates404OnDeleteFile(t *testing.T) {
	// Orphan-recovery path: the file was already deleted on a previous run
	// (or a container restart re-discovered the transfer post-cleanup).
	// DeleteTransfer must still run.
	client := &fakePutioClient{
		deleteFileErr: notFoundError("delete file"),
	}
	m := newTestManagerWithClient(client)

	driveTransferToCleanup(t, m, 1, 100)

	if err := m.PurgeTransfer(1); err != nil {
		t.Fatalf("PurgeTransfer: %v", err)
	}

	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.deleteTransferCalls) != 1 {
		t.Errorf("expected DeleteTransfer to run despite file 404, got %d calls", len(client.deleteTransferCalls))
	}
}

func TestPurgeTransferStopsOnNon404DeleteFileError(t *testing.T) {
	// A real put.io 5xx: don't try DeleteTransfer (would orphan the file
	// against quota). Surface the error so callers can log/retry.
	client := &fakePutioClient{
		deleteFileErr: errors.New("internal server error"),
	}
	m := newTestManagerWithClient(client)

	driveTransferToCleanup(t, m, 1, 100)

	err := m.PurgeTransfer(1)
	if err == nil {
		t.Fatal("PurgeTransfer should return error on non-404 DeleteFile failure")
	}

	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.deleteTransferCalls) != 0 {
		t.Errorf("DeleteTransfer must not run when DeleteFile errors; got %d calls", len(client.deleteTransferCalls))
	}
}

func TestPurgeTransferTolerates404OnDeleteTransfer(t *testing.T) {
	// If the transfer was already removed (e.g. via the put.io UI), a 404
	// on DeleteTransfer is a clean no-op. Coordinator context still drops.
	client := &fakePutioClient{
		deleteTransferErr: notFoundError("cancel transfer"),
	}
	m := newTestManagerWithClient(client)

	driveTransferToCleanup(t, m, 1, 100)

	if err := m.PurgeTransfer(1); err != nil {
		t.Fatalf("PurgeTransfer: %v", err)
	}
	if _, ok := m.coordinator.GetTransferContext(1); ok {
		t.Error("coordinator context should be dropped despite 404")
	}
}

func TestPurgeTransferSkipsDeleteFileWhenFileIDIsZero(t *testing.T) {
	// Cascade-protection: DeleteFile(0) targets the put.io account root.
	// Tests the protection that used to live in handleTorrentRemove (#25).
	client := &fakePutioClient{}
	m := newTestManagerWithClient(client)

	// Construct a Processed context with FileID==0 directly — driveTransferToCleanup
	// would set FileID=100. This mirrors the "seeding-only" zombie shape.
	m.coordinator.InitiateTransfer(1, "zombie", 0, 1)
	if err := m.coordinator.StartDownload(1); err != nil {
		t.Fatalf("StartDownload: %v", err)
	}
	if err := m.coordinator.FileCompleted(1); err != nil {
		t.Fatalf("FileCompleted: %v", err)
	}
	if err := m.coordinator.CompleteTransfer(1); err != nil {
		t.Fatalf("CompleteTransfer: %v", err)
	}

	if err := m.PurgeTransfer(1); err != nil {
		t.Fatalf("PurgeTransfer: %v", err)
	}

	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.deleteFileCalls) != 0 {
		t.Errorf("DeleteFile must not be called when FileID is 0; got %v", client.deleteFileCalls)
	}
	if len(client.deleteTransferCalls) != 1 {
		t.Errorf("DeleteTransfer should still run; got %d calls", len(client.deleteTransferCalls))
	}
}

func TestHandleTransferErrorOn404PurgesOrphan(t *testing.T) {
	// Orphan-loop scenario: GetAllTransferFiles returns wrapped 404 after
	// a container restart re-discovers a transfer whose files plundrio
	// already deleted. Purge must run so the next poll doesn't see it.
	//
	// In production, checkTransfers populated the put.io snapshot before
	// processTransfer → handleTransferError fired, so PurgeTransfer's
	// fallback finds the FileID. We mirror that here by stamping the
	// snapshot directly. DeleteFile then returns 404 (files truly gone —
	// that's how we got into the orphan branch in the first place); the
	// 404 is tolerated and DeleteTransfer still runs.
	transfer := &putio.Transfer{ID: 42, Name: "Hoppers", FileID: 999}
	client := &fakePutioClient{
		deleteFileErr: notFoundError("delete file"),
	}
	m := newTestManagerWithClient(client)
	snapshot := map[string][]*putio.Transfer{"": {transfer}}
	m.processor.transfers.Store(&snapshot)

	wrappedErr := notFoundError("get transfer files")
	m.processor.handleTransferError(transfer, wrappedErr)

	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.deleteFileCalls) != 1 || client.deleteFileCalls[0] != 999 {
		t.Errorf("DeleteFile calls = %v, want [999] (404 tolerated)", client.deleteFileCalls)
	}
	if len(client.deleteTransferCalls) != 1 || client.deleteTransferCalls[0] != 42 {
		t.Errorf("DeleteTransfer calls = %v, want [42] (orphan must be purged)", client.deleteTransferCalls)
	}
}

func TestHandleTransferErrorOnNon404DoesNotCleanup(t *testing.T) {
	// A transient API failure (e.g. 503) should not be misclassified as
	// "files are gone." We log and move on; the next poll retries.
	client := &fakePutioClient{}
	m := newTestManagerWithClient(client)

	transfer := &putio.Transfer{ID: 42, Name: "Hoppers", FileID: 999}
	transientErr := putioErrorWith("get transfer files", http.StatusServiceUnavailable)

	m.processor.handleTransferError(transfer, transientErr)

	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.deleteTransferCalls) != 0 {
		t.Errorf("expected no cleanup on non-404 error, got DeleteTransfer calls = %v", client.deleteTransferCalls)
	}
	if len(client.deleteFileCalls) != 0 {
		t.Errorf("expected no cleanup on non-404 error, got DeleteFile calls = %v", client.deleteFileCalls)
	}
}

func TestPurgeStaleProcessedAgesOutCompleted(t *testing.T) {
	// Retention janitor: a Processed transfer whose processedAt is older
	// than retention gets PurgeTransfered unilaterally. Younger entries
	// are skipped. Non-Processed states (Downloading, Failed) are skipped
	// regardless of age.
	client := &fakePutioClient{}
	m := newTestManagerWithClient(client)

	// Transfer 1: Processed, old (will purge)
	driveTransferToCleanup(t, m, 1, 100)
	oldCtx, _ := m.coordinator.GetTransferContext(1)
	oldCtx.SetProcessedAtForTest(oldCtx.GetProcessedAt().Add(-2 * time.Hour))

	// Transfer 2: Processed, recent (will be skipped)
	driveTransferToCleanup(t, m, 2, 200)

	// Transfer 3: Downloading (will be skipped — retention only fires on
	// Processed, not arbitrary stuck states)
	m.coordinator.InitiateTransfer(3, "downloading", 300, 5)
	if err := m.coordinator.StartDownload(3); err != nil {
		t.Fatalf("StartDownload: %v", err)
	}

	m.purgeStaleProcessed(1*time.Hour, time.Now())

	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.deleteTransferCalls) != 1 || client.deleteTransferCalls[0] != 1 {
		t.Errorf("DeleteTransfer calls = %v, want [1]", client.deleteTransferCalls)
	}
}

func TestPurgeStaleProcessedDisabledByZero(t *testing.T) {
	// retention <= 0 disables the janitor entirely (operator opt-out).
	client := &fakePutioClient{}
	m := newTestManagerWithClient(client)

	driveTransferToCleanup(t, m, 1, 100)
	oldCtx, _ := m.coordinator.GetTransferContext(1)
	oldCtx.SetProcessedAtForTest(oldCtx.GetProcessedAt().Add(-100 * time.Hour))

	m.purgeStaleProcessed(0, time.Now())

	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.deleteTransferCalls) != 0 {
		t.Errorf("zero retention must disable the janitor; got DeleteTransfer = %v", client.deleteTransferCalls)
	}
}
