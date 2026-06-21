package download

import (
	"context"
	"sync"
	"time"

	"github.com/doodla/go-putio"
	"github.com/doodla/plundrio/internal/api"
	"github.com/doodla/plundrio/internal/config"
	"github.com/doodla/plundrio/internal/log"
)

// PutioClient abstracts the put.io API methods used by the download manager.
type PutioClient interface {
	GetTransfers(ctx context.Context) ([]*putio.Transfer, error)
	GetAllTransferFiles(ctx context.Context, fileID int64) ([]api.TransferFile, error)
	RetryTransfer(ctx context.Context, transferID int64) (*putio.Transfer, error)
	DeleteTransfer(ctx context.Context, transferID int64) error
	DeleteFile(ctx context.Context, fileID int64) error
	GetDownloadURL(ctx context.Context, fileID int64) (string, error)
}

// Manager handles downloading completed transfers from Put.io.
// It supports concurrent downloads, progress tracking, and automatic cleanup
// of completed transfers. The manager uses a worker pool pattern to process
// downloads efficiently while maintaining control over system resources.
type Manager struct {
	cfg      *config.Config
	client   PutioClient
	dlConfig *DownloadConfig // Download-specific configuration

	coordinator *TransferCoordinator // Coordinates transfer lifecycle
	categories  *CategoryStore       // Maps transfer hash → category subfolder
	activeFiles sync.Map             // map[int64]int64 - tracks files being downloaded, FileID -> TransferID

	ctx    context.Context
	cancel context.CancelFunc

	stopChan chan struct{}
	stopOnce sync.Once

	workerWg  sync.WaitGroup // tracks worker goroutines
	monitorWg sync.WaitGroup // tracks monitor goroutine

	jobs    chan downloadJob
	mu      sync.Mutex // protects job queueing
	running bool       // tracks if manager is running

	// workerQuit retires INDIVIDUAL workers (distinct from stopChan, which
	// retires all of them on shutdown). Buffered so SetWorkerCount can push
	// retire tokens without blocking under m.mu; a worker reads a token after
	// finishing its in-flight job and exits. Never closed — closing it would
	// race the workers' receive the same way closing jobs races QueueDownload.
	workerQuit chan struct{}
	// workerCount is the reported target worker count, guarded by m.mu.
	// SetWorkerCount is its SINGLE writer; workers never touch it (they own
	// only workerWg). This single-owner rule prevents the double-decrement that
	// would otherwise drive a shrink past its target (8→2 landing at -4 if both
	// the setter and each retiring worker decremented).
	workerCount int

	processor *TransferProcessor // Handles transfer processing

	// downloadFileFn performs a single download attempt; it is a seam (defaults
	// to m.downloadFile in New) so downloadWithRetry's retry/backoff/cancellation
	// logic is unit-testable without touching the network. Mirrors the
	// clock/sleeper injection used in the api package and processFailedTransfersAt.
	downloadFileFn func(*DownloadState) error
	// retryBackoffBase is the base unit of the linear download backoff (attempt N
	// waits N*base). Defaults to time.Second in New; tests set it low to keep the
	// retry path fast.
	retryBackoffBase time.Duration
}

// Context returns the manager's lifecycle context.
// Safe to call before Start (returns context.Background as fallback).
func (m *Manager) Context() context.Context {
	if m.ctx != nil {
		return m.ctx
	}
	return context.Background()
}

// GetTransfers returns all tracked transfers for the configured folder.
func (m *Manager) GetTransfers() []*putio.Transfer {
	if m.processor == nil {
		return nil
	}
	return m.processor.GetTransfers()
}

// GetTransferContext returns the lifecycle context for a transfer, if tracked.
func (m *Manager) GetTransferContext(transferID int64) (*TransferContext, bool) {
	return m.coordinator.GetTransferContext(transferID)
}

// SetCategory stores a category for a transfer hash.
func (m *Manager) SetCategory(hash, category string) {
	m.categories.Set(hash, category)
}

// GetCategory returns the category for a transfer hash, or "" if none.
func (m *Manager) GetCategory(hash string) string {
	return m.categories.Get(hash)
}

// RemoveCategory deletes the stored category for a transfer hash.
func (m *Manager) RemoveCategory(hash string) {
	m.categories.Remove(hash)
}

// New creates a new download manager
func New(cfg *config.Config, client PutioClient) *Manager {
	// Get default download configuration
	dlConfig := GetDefaultConfig()

	// Override the poll cadence from config when set (>0). Zero keeps the
	// package default (30s) so production behavior is unchanged; demo mode sets
	// it low so the dashboard shows live two-phase progress.
	if cfg.TransferCheckInterval > 0 {
		dlConfig.TransferCheckInterval = cfg.TransferCheckInterval
	}

	// Stall handling: a configured (>0) StallTimeout opts in to the operator's
	// values for both the timeout and the retry count; otherwise the package
	// defaults (1h / 1 retry) stand. StallMaxRetries==0 is a meaningful value
	// (delete on first stall), so it is only honored when StallTimeout signals
	// the operator configured stall handling — a default-constructed Config
	// (tests/demo) keeps the 1-retry default.
	if cfg.StallTimeout > 0 {
		dlConfig.StallTimeout = cfg.StallTimeout
		dlConfig.StallMaxRetries = cfg.StallMaxRetries
	}

	// Override with user config if provided
	workerCount := cfg.WorkerCount
	if workerCount <= 0 {
		workerCount = dlConfig.DefaultWorkerCount
	}

	m := &Manager{
		cfg:        cfg,
		client:     client,
		dlConfig:   dlConfig,
		categories: newCategoryStore(cfg.TargetDir),
		stopChan:   make(chan struct{}),
		jobs:       make(chan downloadJob, workerCount*dlConfig.BufferMultiple),
		// workerQuit is buffered well beyond any realistic worker count so a
		// shrink can push all its retire tokens without blocking under m.mu.
		workerQuit:  make(chan struct{}, 256),
		activeFiles: sync.Map{},
	}

	// Default the download seams to production behavior; tests override them.
	m.downloadFileFn = m.downloadFile
	m.retryBackoffBase = time.Second

	// Initialize coordinator and processor
	m.processor = newTransferProcessor(m)
	m.coordinator = NewTransferCoordinator(func(transferID int64) {
		m.processor.MarkTransferProcessed(transferID)
	})

	// Drop any localRetryState entry once the transfer is processed.
	// processFailedTransfers writes one entry per retried transfer; without
	// the cleanup, the map leaks entries for the lifetime of the process.
	// Lookup-only callers (Permanent check, etc.) tolerate the absence —
	// LoadOrStore re-creates a zero-value state if needed.
	m.coordinator.RegisterCleanupHook(func(transferID int64) error {
		m.processor.failedRetryStates.Delete(transferID)
		return nil
	})

	return m
}

// PurgeTransfer deletes the transfer's put.io record (file + transfer) and
// drops the in-memory tracking state.
//
// Why this is its own path rather than CompleteTransfer firing it inline:
// vanishing the transfer from torrent-get the moment local download finished
// violates *arr's Transmission contract. Sonarr/Radarr's TrackedDownloadService
// treats a torrent that disappears between polls (without first being observed
// in a Completed state) as "user removed it" — sets IsTrackable=false, emits
// no DownloadFailedEvent, doesn't fall back to disk. With Radarr's 60s poll
// and plundrio's sub-second cleanup, the catch window was a deterministic
// miss. PurgeTransfer instead waits for an explicit signal:
//
//   - server/torrent.go handleTorrentRemove: the *arr client issues
//     torrent-remove after a successful import (default-true
//     RemoveCompletedDownloads). This is the happy path.
//   - transfers.go purgeStaleProcessed: PostCompleteRetention elapsed
//     without a torrent-remove (safety net for misconfigured / absent clients).
//   - transfers.go handleTransferError: the orphan-recovery 404 path on a
//     transfer rediscovered post-restart whose put.io files are gone.
//
// Tolerates put.io-side NotFound (someone deleted via the put.io UI, or the
// orphan path) and a missing in-memory context (transfer came from a put.io
// snapshot but never went through CompleteTransfer locally). FileID == 0
// skips DeleteFile to avoid the cascade-delete-root bug fixed in #25.
func (m *Manager) PurgeTransfer(transferID int64) error {
	fileID := int64(0)
	if ctx, ok := m.coordinator.GetTransferContext(transferID); ok {
		fileID = ctx.FileID
	} else {
		// Fall back to the put.io snapshot for the file_id. Covers
		// torrent-remove on a transfer plundrio hasn't initialized
		// locally yet (rare: snapshot has it, coordinator doesn't).
		for _, t := range m.processor.GetTransfers() {
			if t.ID == transferID {
				fileID = t.FileID
				break
			}
		}
	}

	if fileID != 0 {
		if err := m.client.DeleteFile(m.Context(), fileID); err != nil {
			if isNotFoundError(err) {
				log.Debug("purge").
					Int64("transfer_id", transferID).
					Int64("file_id", fileID).
					Msg("Source file already gone")
			} else {
				log.Error("purge").
					Int64("transfer_id", transferID).
					Int64("file_id", fileID).
					Err(err).
					Msg("Failed to delete source file")
				return err
			}
		} else {
			log.Info("purge").
				Int64("transfer_id", transferID).
				Msg("Deleted source file")
		}
	}

	if err := m.client.DeleteTransfer(m.Context(), transferID); err != nil {
		if isNotFoundError(err) {
			log.Debug("purge").
				Int64("transfer_id", transferID).
				Msg("Transfer already gone on put.io")
		} else {
			log.Error("purge").
				Int64("transfer_id", transferID).
				Err(err).
				Msg("Failed to delete transfer")
			return err
		}
	} else {
		log.Info("purge").
			Int64("transfer_id", transferID).
			Msg("Deleted transfer")
	}

	m.coordinator.DropTransfer(transferID)
	m.processor.UnmarkTransferProcessed(transferID)
	return nil
}

// Start begins monitoring transfers and downloading completed ones
func (m *Manager) Start() {
	workerCount := m.cfg.WorkerCount
	if workerCount <= 0 {
		workerCount = m.dlConfig.DefaultWorkerCount
	}

	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	m.running = true
	// Record the initial target count so a later SetWorkerCount computes deltas
	// against the right baseline. m.mu guards workerCount; SetWorkerCount is its
	// only other writer and also takes m.mu, so the two never race.
	m.workerCount = workerCount
	m.mu.Unlock()

	m.ctx, m.cancel = context.WithCancel(context.Background())

	m.categories.Load()

	// Start download workers with proper synchronization
	for i := 0; i < workerCount; i++ {
		m.workerWg.Add(1)
		go func() {
			defer m.workerWg.Done()
			m.downloadWorker()
		}()
	}

	// Start transfer monitor
	m.monitorWg.Add(1)
	go func() {
		defer m.monitorWg.Done()
		m.monitorTransfers()
	}()
}

// Stop gracefully shuts down the manager
func (m *Manager) Stop() {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return
	}
	m.running = false
	m.mu.Unlock()

	m.stopOnce.Do(func() {
		// Cancel context first so in-flight API calls abort
		m.cancel()
		// Signal workers and queueing path to stop via stopChan. The jobs
		// channel is intentionally NOT closed: QueueDownload does
		// `select { case m.jobs <- job: ... case <-m.stopChan: ... }`
		// holding m.mu, so closing m.jobs would race with that send and
		// can panic with "send on closed channel" (issue #2). Workers
		// already exit on stopChan; pending buffered jobs are abandoned,
		// which is the right behavior on shutdown — activeFiles cleanup
		// is idempotent across restart.
		close(m.stopChan)
	})

	// Wait for all workers to finish
	m.workerWg.Wait()
	// Wait for monitor to finish
	m.monitorWg.Wait()
}

// SetWorkerCount resizes the worker pool to n (n >= 1). It is the single writer
// of the reported target count (m.workerCount) and runs entirely under m.mu so
// a resize racing Stop() is safe: whichever takes m.mu second observes a
// consistent running/workerCount pair.
//
//   - no-op if not running (Stop already closed stopChan; the pool is draining,
//     so adding/removing workers is meaningless) or if n == current.
//   - grow: spawn (n - current) more downloadWorker goroutines under workerWg,
//     exactly as Start does.
//   - shrink: push (current - n) retire tokens onto the buffered workerQuit.
//     Each token retires one worker AFTER it finishes its in-flight job, so the
//     goroutine count converges asynchronously while the reported count updates
//     now. Best-effort: if the buffer were somehow full the extra token is
//     dropped (the buffer cap far exceeds any realistic worker count).
//
// Never closes jobs or stopChan, preserving issue #2's send-on-closed invariant.
func (m *Manager) SetWorkerCount(n int) {
	if n < 1 {
		n = 1
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.running {
		return
	}

	cur := m.workerCount
	switch {
	case n > cur:
		for i := 0; i < n-cur; i++ {
			m.workerWg.Add(1)
			go func() {
				defer m.workerWg.Done()
				m.downloadWorker()
			}()
		}
	case n < cur:
		for i := 0; i < cur-n; i++ {
			select {
			case m.workerQuit <- struct{}{}:
			default:
				// Buffer full — unreachable for any realistic count (cap 256).
				// Dropping is safe: the count is already set to n below, and a
				// subsequent SetWorkerCount reconciles any residual drift.
				log.Warn("download").
					Int("target", n).
					Int("current", cur).
					Msg("workerQuit buffer full; shrink token dropped")
			}
		}
	}
	m.workerCount = n
}

// GetWorkerCount returns the current reported target worker count. This is the
// target SetWorkerCount last set, not necessarily the live goroutine count
// (shrink converges asynchronously as in-flight jobs finish).
func (m *Manager) GetWorkerCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.workerCount
}

// QueueDownload adds a download job to the queue if not already downloading
func (m *Manager) QueueDownload(job downloadJob) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if file is already being downloaded
	if _, exists := m.activeFiles.Load(job.FileID); exists {
		return
	}

	// Mark file as being downloaded before queueing, storing TransferID
	m.activeFiles.Store(job.FileID, job.TransferID)
	select {
	case m.jobs <- job:
		// Successfully queued
	case <-m.stopChan:
		// Manager is shutting down, just remove from active files
		m.activeFiles.Delete(job.FileID)
	}
}

// handleFileCompletion updates transfer state when a file completes downloading
func (m *Manager) handleFileCompletion(transferID int64, fileID int64) {
	// First increment the completion counter in the transfer coordinator
	if err := m.coordinator.FileCompleted(transferID); err != nil {
		log.Error("transfers").
			Int64("transfer_id", transferID).
			Int64("file_id", fileID).
			Err(err).
			Msg("Failed to handle file completion")
		return
	}

	log.Debug("transfers").
		Int64("transfer_id", transferID).
		Int64("file_id", fileID).
		Msg("File marked as completed")

	// Now that the counter has been incremented, remove the file from active tracking
	m.activeFiles.Delete(fileID)

	// Check if the transfer is marked as completed
	ctx, ok := m.coordinator.GetTransferContext(transferID)
	if !ok {
		log.Debug("transfers").
			Int64("transfer_id", transferID).
			Msg("Transfer context not found after completion")
		return
	}

	state := ctx.GetState()
	_, _, completedFiles, _ := ctx.GetProgress()

	log.Debug("transfers").
		Int64("id", transferID).
		Int32("completed_files", completedFiles).
		Int32("total_files", ctx.TotalFiles).
		Bool("is_completed_state", state == TransferLifecycleCompleted).
		Msg("Transfer completion status")

	// If the transfer is in completed state, check if all downloads are done
	if state == TransferLifecycleCompleted {
		// Count active files for this transfer
		activeCount := 0
		m.activeFiles.Range(func(key, value interface{}) bool {
			fileTransferID := value.(int64)
			if fileTransferID == transferID {
				activeCount++
			}
			return true
		})

		log.Debug("transfers").
			Int64("id", transferID).
			Int("active_files", activeCount).
			Msg("Active files for completed transfer")

		// Only if no active files remain for this transfer, finalize it
		if activeCount == 0 {
			log.Info("transfers").
				Int64("id", transferID).
				Msg("All downloads complete, finalizing transfer")

			if err := m.coordinator.CompleteTransfer(transferID); err != nil {
				log.Error("transfers").
					Int64("id", transferID).
					Err(err).
					Msg("Failed to finalize completed transfer")
			}
		}
	}
}

// handleFileFailure marks a file as failed in the transfer context
func (m *Manager) handleFileFailure(transferID int64) {
	if err := m.coordinator.FileFailure(transferID); err != nil {
		log.Error("transfers").
			Int64("transfer_id", transferID).
			Err(err).
			Msg("Failed to handle file failure")
	}
}
