package download

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/elsbrock/go-putio"
	"github.com/elsbrock/plundrio/internal/config"
)

// fakePutioClient records calls and returns canned errors. Each method
// has an optional override for tests that need to simulate a specific
// failure mode (e.g. 404 on DeleteFile).
type fakePutioClient struct {
	mu sync.Mutex

	deleteFileCalls     []int64
	deleteTransferCalls []int64

	deleteFileErr     error
	deleteTransferErr error
}

func (f *fakePutioClient) GetTransfers(ctx context.Context) ([]*putio.Transfer, error) {
	return nil, nil
}
func (f *fakePutioClient) GetAllTransferFiles(ctx context.Context, fileID int64) ([]*putio.File, error) {
	return nil, nil
}
func (f *fakePutioClient) RetryTransfer(ctx context.Context, transferID int64) (*putio.Transfer, error) {
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

func TestCleanupHookDeletesFileThenTransfer(t *testing.T) {
	client := &fakePutioClient{}
	m := newTestManagerWithClient(client)

	driveTransferToCleanup(t, m, 1, 100)

	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.deleteFileCalls) != 1 || client.deleteFileCalls[0] != 100 {
		t.Errorf("DeleteFile calls = %v, want [100]", client.deleteFileCalls)
	}
	if len(client.deleteTransferCalls) != 1 || client.deleteTransferCalls[0] != 1 {
		t.Errorf("DeleteTransfer calls = %v, want [1]", client.deleteTransferCalls)
	}
}

func TestCleanupHookProceedsWhenFileAlreadyGone(t *testing.T) {
	// Orphan-recovery path: file was deleted in a prior run. DeleteFile
	// returns 404, but cleanup must still delete the transfer record —
	// otherwise the orphan-poll loop persists.
	client := &fakePutioClient{
		deleteFileErr: notFoundError("delete file"),
	}
	m := newTestManagerWithClient(client)

	driveTransferToCleanup(t, m, 1, 100)

	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.deleteFileCalls) != 1 {
		t.Errorf("expected DeleteFile to be attempted, got %d calls", len(client.deleteFileCalls))
	}
	if len(client.deleteTransferCalls) != 1 {
		t.Errorf("expected DeleteTransfer to run despite file 404, got %d calls", len(client.deleteTransferCalls))
	}
}

func TestCleanupHookSkipsTransferDeleteOnFileError(t *testing.T) {
	// Non-404 DeleteFile failure (e.g. 500 from put.io). Skipping the
	// transfer delete prevents leaving dangling files behind on quota.
	client := &fakePutioClient{
		deleteFileErr: errors.New("internal server error"),
	}
	m := newTestManagerWithClient(client)

	m.coordinator.InitiateTransfer(1, "test-transfer", 100, 1)
	if err := m.coordinator.StartDownload(1); err != nil {
		t.Fatalf("StartDownload: %v", err)
	}
	if err := m.coordinator.FileCompleted(1); err != nil {
		t.Fatalf("FileCompleted: %v", err)
	}
	// CompleteTransfer logs the hook error but doesn't propagate it; the
	// state still advances to Processed.
	if err := m.coordinator.CompleteTransfer(1); err != nil {
		t.Fatalf("CompleteTransfer: %v", err)
	}

	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.deleteFileCalls) != 1 {
		t.Errorf("expected one DeleteFile attempt, got %d", len(client.deleteFileCalls))
	}
	if len(client.deleteTransferCalls) != 0 {
		t.Errorf("expected DeleteTransfer to be skipped when DeleteFile errors, got %d calls", len(client.deleteTransferCalls))
	}
}

func TestCleanupHookTolerates404OnDeleteTransfer(t *testing.T) {
	// If the transfer was already removed (e.g. by the user via the put.io
	// UI), a 404 on DeleteTransfer should be a clean no-op, not an error.
	client := &fakePutioClient{
		deleteTransferErr: notFoundError("cancel transfer"),
	}
	m := newTestManagerWithClient(client)

	driveTransferToCleanup(t, m, 1, 100)

	// State should still reach Processed despite the 404 on DeleteTransfer.
	ctx, ok := m.coordinator.GetTransferContext(1)
	if !ok {
		t.Fatal("transfer context missing")
	}
	if ctx.GetState() != TransferLifecycleProcessed {
		t.Errorf("state = %s, want Processed", ctx.GetState())
	}
}

func TestHandleTransferErrorOn404TriggersCleanup(t *testing.T) {
	// The orphan-loop scenario: GetAllTransferFiles returns wrapped 404
	// after a container restart re-discovers a transfer whose files
	// plundrio already deleted. Cleanup must run so the next poll
	// doesn't see this transfer again.
	client := &fakePutioClient{}
	m := newTestManagerWithClient(client)

	transfer := &putio.Transfer{ID: 42, Name: "Hoppers", FileID: 999}
	wrappedErr := notFoundError("get transfer files")

	m.processor.handleTransferError(transfer, wrappedErr)

	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.deleteTransferCalls) != 1 || client.deleteTransferCalls[0] != 42 {
		t.Errorf("DeleteTransfer calls = %v, want [42] (orphan must be cleaned up)", client.deleteTransferCalls)
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
