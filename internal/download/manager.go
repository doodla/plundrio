package download

import (
	"context"
	"sync"

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

	processor *TransferProcessor // Handles transfer processing
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

	// Override with user config if provided
	workerCount := cfg.WorkerCount
	if workerCount <= 0 {
		workerCount = dlConfig.DefaultWorkerCount
	}

	m := &Manager{
		cfg:         cfg,
		client:      client,
		dlConfig:    dlConfig,
		categories:  newCategoryStore(cfg.TargetDir),
		stopChan:    make(chan struct{}),
		jobs:        make(chan downloadJob, workerCount*dlConfig.BufferMultiple),
		activeFiles: sync.Map{},
	}

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
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	m.running = true
	m.mu.Unlock()

	m.ctx, m.cancel = context.WithCancel(context.Background())

	m.categories.Load()

	workerCount := m.cfg.WorkerCount
	if workerCount <= 0 {
		workerCount = m.dlConfig.DefaultWorkerCount
	}

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
