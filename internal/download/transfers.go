package download

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/elsbrock/go-putio"
	"github.com/elsbrock/plundrio/internal/log"
)

// TransferProcessor handles the processing of Put.io transfers
type TransferProcessor struct {
	manager            *Manager
	transfers          map[string][]*putio.Transfer // Status -> Transfers
	processedTransfers sync.Map                     // map[int64]bool - Tracks transfers that have been processed locally
	retryAttempts      sync.Map                     // map[int64]int - Tracks retry attempts for put.io ERROR transfers (processErroredTransfers)
	failedRetryStates  sync.Map                     // map[int64]*localRetryState - Tracks local retry attempts for TransferLifecycleFailed transfers (processFailedTransfers)
	folderID           int64
	targetDir          string
}

// localRetryState tracks a single failed transfer's progress through the
// local-retry cascade in processFailedTransfers. All mutations happen on the
// monitor goroutine (single-threaded — see manager.go monitorTransfers), so no
// internal locking is needed. Stored as *localRetryState in failedRetryStates
// so the cascade can mutate fields in place.
type localRetryState struct {
	Count                int       // local retry attempts; capped at maxLocalRetryAttempts
	LastAttempt          time.Time // when we last requeued (drives backoff window)
	PutioFallback        bool      // true once client.RetryTransfer has been called
	PutioFallbackAt      time.Time // when fallback fired (gates next post-fallback action)
	PutioFallbackRetried bool      // true once a post-fallback local requeue has run
	Permanent            bool      // terminal — short-circuits all further processing for this transfer
}

// localRetryBackoff is the delay between local requeues, indexed by Count-1
// (Count==0 is the immediate first retry).
var localRetryBackoff = []time.Duration{
	5 * time.Minute,
	30 * time.Minute,
	2 * time.Hour,
}

// maxLocalRetryAttempts is the number of local retries before falling back to
// put.io's RetryTransfer. Must equal len(localRetryBackoff).
const maxLocalRetryAttempts = 3

// putioFallbackWait is how long to wait after calling client.RetryTransfer
// before checking put.io status and deciding whether to do one final local
// requeue or mark the transfer permanently failed.
const putioFallbackWait = 30 * time.Minute

// GetTransfers returns a copy of all transfers for a given folder ID
func (p *TransferProcessor) GetTransfers() []*putio.Transfer {
	var allTransfers []*putio.Transfer
	for _, transfers := range p.transfers {
		for _, t := range transfers {
			if t.SaveParentID == p.folderID {
				allTransfers = append(allTransfers, t)
			}
		}
	}
	return allTransfers
}

// newTransferProcessor creates a new transfer processor
func newTransferProcessor(m *Manager) *TransferProcessor {
	return &TransferProcessor{
		manager:            m,
		transfers:          make(map[string][]*putio.Transfer),
		processedTransfers: sync.Map{},
		retryAttempts:      sync.Map{},
		folderID:           m.cfg.FolderID,
		targetDir:          m.cfg.TargetDir,
	}
}

// monitorTransfers periodically checks for completed transfers
func (m *Manager) monitorTransfers() {
	log.Debug("transfers").Msg("Starting transfer monitor")

	log.Debug("transfers").
		Int64("folder_id", m.processor.folderID).
		Str("target_dir", m.processor.targetDir).
		Msg("Transfer processor initialized")

	// Initial check
	m.processor.checkTransfers()

	ticker := time.NewTicker(m.dlConfig.TransferCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopChan:
			log.Debug("transfers").Msg("Transfer monitor stopping")
			return
		case <-ticker.C:
			m.processor.checkTransfers()
		}
	}
}

// checkTransfers looks for completed or seeding transfers and processes them
func (p *TransferProcessor) checkTransfers() {
	log.Debug("transfers").Msg("Checking transfers")

	transfers, err := p.manager.client.GetTransfers(p.manager.Context())
	if err != nil {
		log.Error("transfers").Err(err).Msg("Failed to get transfers")
		return
	}

	log.Debug("transfers").
		Int("api_transfers_count", len(transfers)).
		Msg("Retrieved transfers from API")

	// Reset transfer status tracking
	p.transfers = make(map[string][]*putio.Transfer)

	// Categorize transfers by status
	for _, t := range transfers {
		if t.SaveParentID != p.folderID {
			log.Debug("transfers").
				Int64("transfer_id", t.ID).
				Int64("parent_id", t.SaveParentID).
				Int64("target_folder", p.folderID).
				Msg("Skipping transfer from different folder")
			continue
		}
		p.transfers[t.Status] = append(p.transfers[t.Status], t)
	}

	// Log transfer summary
	p.logTransferSummary()

	// Process transfers by status
	p.processReadyTransfers()
	p.processFailedTransfers()
	p.processErroredTransfers()

	// Check for transfers that are in "Completed" state but haven't been fully cleaned up
	p.finalizeCompletedTransfers()
}

// logTransferSummary logs counts of transfers in each status and detailed information for all transfers
func (p *TransferProcessor) logTransferSummary() {
	counts := map[string]int{
		"IN_QUEUE":    len(p.transfers["IN_QUEUE"]),
		"WAITING":     len(p.transfers["WAITING"]),
		"PREPARING":   len(p.transfers["PREPARING"]),
		"DOWNLOADING": len(p.transfers["DOWNLOADING"]),
		"COMPLETING":  len(p.transfers["COMPLETING"]),
		"SEEDING":     len(p.transfers["SEEDING"]),
		"COMPLETED":   len(p.transfers["COMPLETED"]),
		"ERROR":       len(p.transfers["ERROR"]),
	}

	log.Info("transfers").
		Int("queued", counts["IN_QUEUE"]).
		Int("waiting", counts["WAITING"]).
		Int("preparing", counts["PREPARING"]).
		Int("downloading", counts["DOWNLOADING"]).
		Int("completing", counts["COMPLETING"]).
		Int("seeding", counts["SEEDING"]).
		Int("completed", counts["COMPLETED"]).
		Int("error", counts["ERROR"]).
		Msg("Transfer status summary")

	// Log detailed information for all transfers
	p.logAllTransfersDetails()
}

// logAllTransfersDetails logs detailed information for all transfers
func (p *TransferProcessor) logAllTransfersDetails() {
	allTransfers := p.GetTransfers()
	if len(allTransfers) == 0 {
		log.Debug("transfers").Msg("No transfers found for detailed logging")
		return
	}

	for _, t := range allTransfers {
		// Create a logger with common fields for all transfers
		transferLogger := log.Debug("transfers").
			Int64("id", t.ID).
			Str("name", t.Name).
			Str("status", t.Status).
			Int64("save_parent_id", t.SaveParentID).
			Int64("file_id", t.FileID).
			Int("size", t.Size).
			Str("type", t.Type).
			Str("status_message", t.StatusMessage).
			Int("availability", t.Availability).
			Bool("is_private", t.IsPrivate)

		// Add peer information if available
		if t.PeersConnected > 0 || t.PeersSendingToUs > 0 || t.PeersGettingFromUs > 0 {
			transferLogger = transferLogger.
				Int("peers_connected", t.PeersConnected).
				Int("peers_sending_to_us", t.PeersSendingToUs).
				Int("peers_getting_from_us", t.PeersGettingFromUs)
		}

		// Add speed information if available
		if t.DownloadSpeed > 0 || t.UploadSpeed > 0 {
			transferLogger = transferLogger.
				Int("download_speed", t.DownloadSpeed).
				Int("upload_speed", t.UploadSpeed).
				Int64("downloaded", t.Downloaded).
				Int64("uploaded", t.Uploaded)
		}

		// Add progress information if available
		if t.PercentDone > 0 && t.PercentDone < 100 {
			transferLogger = transferLogger.
				Int("percent_done", t.PercentDone).
				Int64("estimated_time", t.EstimatedTime)
		}

		// Add seeding information if available
		if t.SecondsSeeding > 0 {
			transferLogger = transferLogger.Int("seconds_seeding", t.SecondsSeeding)
		}

		// Add tracker information if available
		if t.Trackers != "" {
			transferLogger = transferLogger.Str("trackers", t.Trackers)
		}
		if t.TrackerMessage != "" {
			transferLogger = transferLogger.Str("tracker_message", t.TrackerMessage)
		}

		// Add torrent information if available
		if t.MagnetURI != "" {
			transferLogger = transferLogger.Str("magnet_uri", t.MagnetURI)
		}
		if t.TorrentLink != "" {
			transferLogger = transferLogger.Str("torrent_link", t.TorrentLink)
		}
		if t.CreatedTorrent {
			transferLogger = transferLogger.Bool("created_torrent", t.CreatedTorrent)
		}

		// Add error information if available
		if t.ErrorMessage != "" {
			transferLogger = transferLogger.Str("error_message", t.ErrorMessage)
		}

		// Add timestamps if available
		if t.CreatedAt != nil {
			transferLogger = transferLogger.Interface("created_at", t.CreatedAt)
		}
		if t.FinishedAt != nil {
			transferLogger = transferLogger.Interface("finished_at", t.FinishedAt)
		}

		// Add other miscellaneous fields
		if t.ClientIP != "" {
			transferLogger = transferLogger.Str("client_ip", t.ClientIP)
		}
		if t.CallbackURL != "" {
			transferLogger = transferLogger.Str("callback_url", t.CallbackURL)
		}
		if t.Extract {
			transferLogger = transferLogger.Bool("extract", t.Extract)
		}
		if t.DownloadID != 0 {
			transferLogger = transferLogger.Int64("download_id", t.DownloadID)
		}
		if t.SubscriptionID != 0 {
			transferLogger = transferLogger.Int("subscription_id", t.SubscriptionID)
		}

		// Check if this transfer is being processed locally
		_, processed := p.processedTransfers.Load(t.ID)
		transferLogger = transferLogger.Bool("processed_locally", processed)

		// Log the transfer details with a message that includes the status
		transferLogger.Msgf("Transfer details (%s)", t.Status)
	}
}

// processReadyTransfers handles completed and seeding transfers
func (p *TransferProcessor) processReadyTransfers() {
	readyTransfers := append(p.transfers["COMPLETED"], p.transfers["SEEDING"]...)
	now := time.Now()

	for _, transfer := range readyTransfers {
		select {
		case <-p.manager.stopChan:
			log.Debug("transfers").Msg("Stopping transfer processing")
			return
		default:
			if p.isTransferBeingProcessed(transfer.ID) {
				continue
			}
			if !CanStartDownloadNow(p.manager.cfg.DownloadStartWindow, now) {
				log.Debug("transfers").
					Int64("transfer_id", transfer.ID).
					Str("transfer_name", transfer.Name).
					Str("window_start", p.manager.cfg.DownloadStartWindow.Start).
					Str("window_end", p.manager.cfg.DownloadStartWindow.End).
					Msg("Skipping local download start because current time is outside the configured window")
				continue
			}
			p.startTransferProcessing(transfer)
		}
	}
}

// isTransferBeingProcessed checks if a transfer is already being handled
func (p *TransferProcessor) isTransferBeingProcessed(transferID int64) bool {
	if _, exists := p.manager.coordinator.GetTransferContext(transferID); exists {
		log.Debug("transfers").
			Int64("transfer_id", transferID).
			Msg("Transfer already being processed")
		return true
	}
	return false
}

// startTransferProcessing begins processing a transfer
func (p *TransferProcessor) startTransferProcessing(transfer *putio.Transfer) {
	log.Info("transfers").
		Str("name", transfer.Name).
		Str("status", transfer.Status).
		Int64("id", transfer.ID).
		Msg("Found ready transfer")

	p.manager.workerWg.Add(1)
	transferCopy := *transfer
	go func() {
		p.processTransfer(&transferCopy)
	}()
}

// processTransfer handles downloading of a completed or seeding transfer
func (p *TransferProcessor) processTransfer(transfer *putio.Transfer) {
	defer p.manager.workerWg.Done()

	log.Debug("transfers").
		Str("name", transfer.Name).
		Int64("id", transfer.ID).
		Int64("file_id", transfer.FileID).
		Msg("Processing transfer")

	files, err := p.manager.client.GetAllTransferFiles(p.manager.Context(), transfer.FileID)
	if err != nil {
		p.handleTransferError(transfer, err)
		return
	}

	if len(files) == 0 {
		err := NewNoFilesFoundError(transfer.ID)
		p.manager.coordinator.FailTransfer(transfer.ID, err)
		return
	}

	// Initialize transfer with total number of files
	if !p.initializeTransfer(transfer, len(files)) {
		return
	}

	// Queue files that need downloading
	filesToDownload := p.queueTransferFiles(transfer, files)

	// If no files need downloading (all exist), complete the transfer
	if filesToDownload == 0 {
		log.Info("transfers").
			Str("name", transfer.Name).
			Int64("id", transfer.ID).
			Msg("All files already exist, completing transfer")
		p.manager.coordinator.CompleteTransfer(transfer.ID)
		return
	}
}

// handleTransferError processes transfer errors appropriately.
//
// A 404 here means we re-discovered a transfer whose files plundrio already
// deleted (typically across a container restart). Without this branch the
// transfer would loop on every poll: GetAllTransferFiles → 404 → log → repeat.
// We treat it as the orphan-recovery path and run the cleanup hook to delete
// the dead transfer record.
func (p *TransferProcessor) handleTransferError(transfer *putio.Transfer, err error) {
	if isNotFoundError(err) {
		log.Info("transfers").
			Str("name", transfer.Name).
			Int64("id", transfer.ID).
			Msg("Files no longer exist on Put.io, cleaning up orphan transfer")

		p.initializeTransfer(transfer, 0)
		p.manager.cleanupTransfer(transfer.ID)
		return
	}

	log.Error("transfers").
		Str("name", transfer.Name).
		Int64("id", transfer.ID).
		Err(err).
		Msg("Failed to get transfer files")
}

// queueTransferFiles processes files in a transfer and queues them for download
func (p *TransferProcessor) queueTransferFiles(transfer *putio.Transfer, files []*putio.File) int {
	filesToDownload := 0

	// Get the transfer context to update total size
	ctx, exists := p.manager.coordinator.GetTransferContext(transfer.ID)
	if !exists {
		log.Error("transfers").
			Int64("transfer_id", transfer.ID).
			Msg("Transfer context not found when queueing files")
		return 0
	}

	// Calculate total size of all files
	var totalSize int64
	for _, file := range files {
		totalSize += file.Size
	}

	// Update the transfer context with total size
	ctx.SetTotalSize(totalSize)

	log.Info("transfers").
		Int64("transfer_id", transfer.ID).
		Int64("total_size", totalSize).
		Int("file_count", len(files)).
		Msg("Updated transfer with total file size")

	for _, file := range files {
		if p.shouldDownloadFile(transfer, file) {
			filesToDownload++
			p.queueFileDownload(transfer, file)
		} else {
			// For files we don't need to download (already exist), mark as completed
			if err := p.manager.coordinator.FileCompleted(transfer.ID); err != nil {
				log.Error("transfers").
					Int64("transfer_id", transfer.ID).
					Str("file_name", file.Name).
					Err(err).
					Msg("Failed to mark existing file as completed")
			}

			// For existing files, add their size to the downloaded size
			ctx.AddDownloadedBytes(file.Size)

			log.Debug("transfers").
				Int64("transfer_id", transfer.ID).
				Str("file_name", file.Name).
				Int64("file_size", file.Size).
				Msg("Added existing file size to downloaded total")
		}
	}
	return filesToDownload
}

// shouldDownloadFile determines if a file needs to be downloaded
func (p *TransferProcessor) shouldDownloadFile(transfer *putio.Transfer, file *putio.File) bool {
	category := p.manager.GetCategory(transfer.Hash)
	targetPath := filepath.Join(p.targetDir, category, transfer.Name, file.Name)
	info, err := os.Stat(targetPath)

	// Skip if file exists with correct size
	if err == nil && info.Size() == file.Size {
		log.Info("transfers").
			Str("file_name", file.Name).
			Int64("file_id", file.ID).
			Msg("File already exists, skipping download")
		return false
	}

	// Skip if already being downloaded
	if _, exists := p.manager.activeFiles.Load(file.ID); exists {
		log.Debug("transfers").
			Str("file_name", file.Name).
			Int64("file_id", file.ID).
			Msg("File already being downloaded")
		return false
	}

	return true
}

// queueFileDownload adds a file to the download queue
func (p *TransferProcessor) queueFileDownload(transfer *putio.Transfer, file *putio.File) {
	category := p.manager.GetCategory(transfer.Hash)
	p.manager.QueueDownload(downloadJob{
		FileID:     file.ID,
		Name:       filepath.Join(category, transfer.Name, file.Name),
		TransferID: transfer.ID,
	})
	log.Debug("transfers").
		Str("file_name", file.Name).
		Int64("file_id", file.ID).
		Int64("size", file.Size).
		Msg("Queued file for download")
}

// initializeTransfer sets up transfer tracking
func (p *TransferProcessor) initializeTransfer(transfer *putio.Transfer, filesToDownload int) bool {
	p.manager.coordinator.InitiateTransfer(transfer.ID, transfer.Name, transfer.FileID, filesToDownload)
	if err := p.manager.coordinator.StartDownload(transfer.ID); err != nil {
		log.Error("transfers").
			Str("name", transfer.Name).
			Int64("id", transfer.ID).
			Err(err).
			Msg("Failed to start transfer download")
		p.manager.coordinator.FailTransfer(transfer.ID, err)
		return false
	}

	if filesToDownload > 0 {
		log.Info("transfers").
			Str("name", transfer.Name).
			Int("files", filesToDownload).
			Msg("Queued files for download")
	}
	return true
}

// processErroredTransfers handles failed transfers with retry logic
func (p *TransferProcessor) processErroredTransfers() {
	const maxRetryAttempts = 3

	for _, transfer := range p.transfers["ERROR"] {
		// Get current retry count
		retryCountValue, exists := p.retryAttempts.Load(transfer.ID)
		retryCount := 0
		if exists {
			retryCount = retryCountValue.(int)
		}

		// Log the error with retry information
		logger := log.Error("transfers").
			Str("name", transfer.Name).
			Int64("id", transfer.ID).
			Str("error", transfer.ErrorMessage).
			Int("retry_count", retryCount)

		// Check if we should retry or delete
		if retryCount < maxRetryAttempts {
			// Increment retry count
			p.retryAttempts.Store(transfer.ID, retryCount+1)

			// Log retry attempt
			logger.Msgf("Transfer errored, retrying (attempt %d of %d)", retryCount+1, maxRetryAttempts)

			// Attempt to retry the transfer
			retried, err := p.manager.client.RetryTransfer(p.manager.Context(), transfer.ID)
			if err != nil {
				log.Error("transfers").
					Str("name", transfer.Name).
					Int64("id", transfer.ID).
					Err(err).
					Msgf("Failed to retry transfer (attempt %d of %d)", retryCount+1, maxRetryAttempts)
			} else {
				log.Info("transfers").
					Str("name", transfer.Name).
					Int64("id", transfer.ID).
					Str("new_status", retried.Status).
					Msgf("Successfully retried transfer (attempt %d of %d)", retryCount+1, maxRetryAttempts)
			}
		} else {
			// Log that we're giving up after max retries
			logger.Msgf("Transfer errored, giving up after %d retry attempts", maxRetryAttempts)

			// Delete the transfer after max retries
			if err := p.manager.client.DeleteTransfer(p.manager.Context(), transfer.ID); err != nil {
				log.Error("transfers").
					Str("name", transfer.Name).
					Int64("id", transfer.ID).
					Err(err).
					Msg("Failed to delete errored transfer after max retries")
			} else {
				// Clear retry counter after successful deletion
				p.retryAttempts.Delete(transfer.ID)
				log.Info("transfers").
					Str("name", transfer.Name).
					Int64("id", transfer.ID).
					Msg("Deleted errored transfer after max retries")
			}
		}
	}
}

// MarkTransferProcessed marks a transfer as processed locally
func (p *TransferProcessor) MarkTransferProcessed(transferID int64) {
	p.processedTransfers.Store(transferID, true)
	log.Debug("transfers").
		Int64("transfer_id", transferID).
		Msg("Marked transfer as processed locally")
}

// processFailedTransfers re-queues transfers stuck in TransferLifecycleFailed
// after downloadWithRetry exhausted its in-attempt retries. Without this loop,
// the "kept for retry" state from FileFailure is aspirational — nothing else
// re-pulls from put.io once the worker gives up. The cascade is documented on
// the localRetryState struct and in the project plan: short backoff loop ->
// put.io RetryTransfer fallback -> one final local retry -> permanent fail.
func (p *TransferProcessor) processFailedTransfers() {
	p.processFailedTransfersAt(time.Now())
}

// processFailedTransfersAt is a testable seam for processFailedTransfers. The
// codebase has no clock abstraction; this is a one-off testability hook, not
// a new pattern.
func (p *TransferProcessor) processFailedTransfersAt(now time.Time) {
	type candidate struct {
		id  int64
		ctx *TransferContext
	}
	var candidates []candidate

	p.manager.coordinator.RangeTransfers(func(id int64, ctx *TransferContext) bool {
		if ctx.GetState() == TransferLifecycleFailed {
			candidates = append(candidates, candidate{id, ctx})
		}
		return true
	})

	for _, c := range candidates {
		p.tryRetryFailedTransfer(c.id, c.ctx, now)
	}
}

// tryRetryFailedTransfer advances a single Failed transfer through the local
// retry cascade. Called only from processFailedTransfersAt on the monitor
// goroutine, so direct mutation of *localRetryState is safe.
func (p *TransferProcessor) tryRetryFailedTransfer(id int64, ctx *TransferContext, now time.Time) {
	// In-flight invariant: completed+failed must equal total before we touch
	// the context. RequeueFailedTransfer enforces this too, but checking here
	// avoids bumping retry counters, calling put.io, or stamping an empty
	// localRetryState into the map while a late FileCompleted from the
	// previous attempt is still in flight.
	completed, failed, total := ctx.GetCounters()
	if completed+failed < total {
		log.Debug("transfers").
			Int64("id", id).
			Str("name", ctx.Name).
			Int32("completed", completed).Int32("failed", failed).Int32("total", total).
			Msg("Failed transfer has workers in flight, deferring retry")
		return
	}

	rsValue, _ := p.failedRetryStates.LoadOrStore(id, &localRetryState{})
	rs := rsValue.(*localRetryState)

	// Terminal: stop processing this transfer entirely. Cleared only on
	// process restart (failedRetryStates is in-memory only).
	if rs.Permanent {
		return
	}

	// Confirm the put.io transfer still exists. Without it, requeueing is
	// pointless — GetAllTransferFiles will 404.
	putioTransfer := p.findPutioTransferByID(id)
	if putioTransfer == nil {
		log.Warn("transfers").
			Int64("id", id).Str("name", ctx.Name).
			Msg("Failed transfer no longer present on put.io, marking permanently failed")
		_ = p.manager.coordinator.FailTransfer(id, errors.New("transfer no longer present on put.io"))
		rs.Permanent = true
		return
	}

	// Branch A: post-fallback handling. Either we requeue once more (if put.io
	// recovered) or we give up.
	if rs.PutioFallback {
		if rs.PutioFallbackRetried {
			// Already used our one post-fallback local retry and we're back
			// in Failed. Game over.
			log.Error("transfers").
				Int64("id", id).Str("name", ctx.Name).
				Msg("Local retries and put.io fallback exhausted, marking permanently failed")
			_ = p.manager.coordinator.FailTransfer(id, errors.New("retries exhausted post put.io fallback"))
			rs.Permanent = true
			return
		}
		// Wait the post-fallback window before re-checking put.io status.
		if now.Before(rs.PutioFallbackAt.Add(putioFallbackWait)) {
			return
		}
		// put.io has had time to redeliver. Check its status.
		if putioTransfer.Status != "COMPLETED" && putioTransfer.Status != "SEEDING" {
			log.Error("transfers").
				Int64("id", id).Str("name", ctx.Name).Str("putio_status", putioTransfer.Status).
				Msg("put.io fallback did not recover transfer, marking permanently failed")
			_ = p.manager.coordinator.FailTransfer(id, fmt.Errorf("put.io status %s after fallback", putioTransfer.Status))
			rs.Permanent = true
			return
		}
		// put.io is ready — one final local requeue.
		log.Info("transfers").
			Int64("id", id).Str("name", ctx.Name).
			Msg("put.io fallback succeeded, requeueing locally one final time")
		rs.PutioFallbackRetried = true
		rs.LastAttempt = now
		p.dispatchRequeue(id, ctx, putioTransfer)
		return
	}

	// Branch B: backoff window for normal local retries. Count==0 falls
	// through (immediate first retry).
	if rs.Count > 0 {
		// Count-1 indexes the schedule. Safe because Count <= maxLocalRetryAttempts here.
		nextEligible := rs.LastAttempt.Add(localRetryBackoff[rs.Count-1])
		if now.Before(nextEligible) {
			log.Debug("transfers").
				Int64("id", id).Str("name", ctx.Name).
				Int("local_retry_count", rs.Count).Time("next_eligible", nextEligible).
				Msg("Failed transfer waiting for backoff window")
			return
		}
	}

	// Branch C: budget exhausted — fall back to put.io.
	if rs.Count >= maxLocalRetryAttempts {
		log.Warn("transfers").
			Int64("id", id).Str("name", ctx.Name).Int("local_retry_count", rs.Count).
			Msg("Local retries exhausted, falling back to put.io RetryTransfer")
		if _, err := p.manager.client.RetryTransfer(p.manager.Context(), id); err != nil {
			log.Error("transfers").
				Int64("id", id).Err(err).
				Msg("put.io RetryTransfer call failed")
			// Don't permanently fail yet; setting PutioFallback below means
			// we'll wait the full window before the next action (which can
			// re-evaluate based on put.io status).
		}
		rs.PutioFallback = true
		rs.PutioFallbackAt = now
		// DO NOT reset Count. Branch A handles all post-fallback transitions.
		return
	}

	// Branch D: within local retry budget — requeue.
	rs.Count++
	rs.LastAttempt = now
	log.Info("transfers").
		Int64("id", id).Str("name", ctx.Name).
		Int("local_retry_attempt", rs.Count).Int("max", maxLocalRetryAttempts).
		Msg("Re-queueing failed transfer for local retry")
	p.dispatchRequeue(id, ctx, putioTransfer)
}

// dispatchRequeue runs the file-fetch + coordinator state flip + queue work in
// a goroutine. The async dispatch keeps the monitor loop responsive:
// queueTransferFiles can block on m.jobs when the channel is full
// (workerCount * BufferMultiple buffered).
//
// Order matters: GetAllTransferFiles runs before RequeueFailedTransfer so a
// put.io API failure leaves the transfer in Failed with the in-flight
// invariant intact (completed+failed == total). If we flipped to Downloading
// first and then errored, FailTransfer wouldn't bump failedFiles, so the
// invariant check in tryRetryFailedTransfer would gate every future tick and
// the transfer would be stuck Failed forever.
//
// Note: rs.Count and rs.LastAttempt are mutated by the caller on the monitor
// goroutine *before* this function is invoked, so backoff is respected even
// if GetAllTransferFiles is slow.
func (p *TransferProcessor) dispatchRequeue(id int64, ctx *TransferContext, putioTransfer *putio.Transfer) {
	p.manager.workerWg.Add(1)
	go func() {
		defer p.manager.workerWg.Done()
		files, err := p.manager.client.GetAllTransferFiles(p.manager.Context(), putioTransfer.FileID)
		if err != nil {
			log.Error("transfers").Int64("id", id).Err(err).
				Msg("Retry: GetAllTransferFiles failed; leaving transfer in Failed for next tick")
			// State is still Failed and the in-flight invariant still holds
			// (we haven't touched the counters), so the next monitor tick
			// will re-evaluate per backoff.
			return
		}
		if len(files) == 0 {
			_ = p.manager.coordinator.FailTransfer(id, NewNoFilesFoundError(id))
			return
		}
		if err := p.manager.coordinator.RequeueFailedTransfer(id); err != nil {
			// Race: state moved from Failed under us. The in-flight invariant
			// check in tryRetryFailedTransfer should prevent this in practice;
			// treat as benign.
			log.Debug("transfers").
				Int64("id", id).Err(err).
				Msg("RequeueFailedTransfer no-op, state changed under us")
			return
		}
		queued := p.queueTransferFiles(putioTransfer, files)
		if queued == 0 {
			// Everything was already on disk — finalize.
			_ = p.manager.coordinator.CompleteTransfer(id)
		}
	}()
}

// findPutioTransferByID searches the cached put.io transfer list (populated
// by the most recent checkTransfers run) for a given ID.
func (p *TransferProcessor) findPutioTransferByID(id int64) *putio.Transfer {
	for _, byStatus := range p.transfers {
		for _, t := range byStatus {
			if t.ID == id {
				return t
			}
		}
	}
	return nil
}

// finalizeCompletedTransfers checks for transfers that are marked as completed in the
// internal tracking system but haven't been fully cleaned up yet.
func (p *TransferProcessor) finalizeCompletedTransfers() {
	// Get all active transfers from the coordinator
	var pendingCleanup []int64

	p.manager.coordinator.RangeTransfers(func(transferID int64, ctx *TransferContext) bool {

		state := ctx.GetState()

		if state == TransferLifecycleCompleted {
			log.Info("transfers").
				Int64("id", transferID).
				Str("name", ctx.Name).
				Msg("Found completed transfer pending cleanup")
			pendingCleanup = append(pendingCleanup, transferID)
		}
		return true
	})

	// Process any transfers that need cleanup
	for _, transferID := range pendingCleanup {
		log.Info("transfers").
			Int64("id", transferID).
			Msg("Finalizing completed transfer")

		if err := p.manager.coordinator.CompleteTransfer(transferID); err != nil {
			log.Error("transfers").
				Int64("id", transferID).
				Err(err).
				Msg("Failed to finalize completed transfer")
		}
	}
}
