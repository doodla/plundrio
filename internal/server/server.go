package server

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/doodla/go-putio"
	"github.com/doodla/plundrio/internal/config"
	"github.com/doodla/plundrio/internal/download"
	"github.com/doodla/plundrio/internal/log"
)

// accountCacheTTL bounds how stale the cached AccountInfo used by the
// torrent-add free-space check may be. A burst of grabs from *arr would
// otherwise fire one account.info call per add; 60s collapses that to at most
// one call per minute while keeping the free-space figure fresh enough for a
// floor check.
const accountCacheTTL = 60 * time.Second

// PutioClient abstracts the put.io API methods used by the RPC server.
type PutioClient interface {
	GetAccountInfo(ctx context.Context) (*putio.AccountInfo, error)
	GetTransfers(ctx context.Context) ([]*putio.Transfer, error)
	UploadFile(ctx context.Context, data []byte, filename string, folderID int64) (string, error)
	AddTransfer(ctx context.Context, magnetLink string, folderID int64) (string, error)
	DeleteFile(ctx context.Context, fileID int64) error
	DeleteTransfer(ctx context.Context, transferID int64) error
}

// DownloadService abstracts the download manager for the RPC server.
type DownloadService interface {
	GetTransfers() []*putio.Transfer
	GetTransferContext(transferID int64) (*download.TransferContext, bool)
	SetCategory(hash, category string)
	GetCategory(hash string) string
	RemoveCategory(hash string)
	// PurgeTransfer triggers the put.io-side delete + drops in-memory state
	// for transferID. Tolerates put.io 404 and missing context. Called from
	// handleTorrentRemove so the same code path runs whether removal is
	// driven by an *arr client or by the retention janitor.
	PurgeTransfer(transferID int64) error
	Stop()
}

// Server handles transmission-rpc requests
type Server struct {
	cfg          *config.Config
	client       PutioClient
	srv          *http.Server
	quotaTicker  *time.Ticker
	stopChan     chan struct{}
	dlService    DownloadService
	quotaWarning atomic.Bool // tracks if we've already warned about quota

	// minFreeSpace is cfg.MinFreeSpace parsed to bytes once at construction.
	// 0 disables the torrent-add free-space check.
	minFreeSpace int64

	// accountMu guards the cached AccountInfo used by the free-space check.
	// Refreshes happen under the lock (single-flight): a burst of concurrent
	// adds blocks on at most one account.info fetch per accountCacheTTL.
	accountMu       sync.Mutex
	cachedAccount   *putio.AccountInfo
	cachedAccountAt time.Time
}

// New creates a new RPC server
func New(cfg *config.Config, client PutioClient, dlService DownloadService) *Server {
	// cfg.MinFreeSpace is validated at startup (cmd/plundrio), so a parse error
	// here is not expected; fail open (disable the floor) and warn rather than
	// block all adds on a bad value that slipped through.
	minFree, err := config.ParseByteSize(cfg.MinFreeSpace)
	if err != nil {
		log.Warn("server").
			Str("min_free_space", cfg.MinFreeSpace).
			Err(err).
			Msg("Invalid min_free_space; disabling free-space check")
		minFree = 0
	}
	return &Server{
		cfg:          cfg,
		client:       client,
		stopChan:     make(chan struct{}),
		dlService:    dlService,
		quotaTicker:  time.NewTicker(15 * time.Minute),
		minFreeSpace: minFree,
	}
}

// cachedAccountInfo returns put.io AccountInfo no older than accountCacheTTL,
// fetching a fresh copy under accountMu when the cache is cold or stale.
func (s *Server) cachedAccountInfo(ctx context.Context) (*putio.AccountInfo, error) {
	s.accountMu.Lock()
	defer s.accountMu.Unlock()

	if s.cachedAccount != nil && time.Since(s.cachedAccountAt) < accountCacheTTL {
		return s.cachedAccount, nil
	}
	account, err := s.client.GetAccountInfo(ctx)
	if err != nil {
		return nil, err
	}
	s.cachedAccount = account
	s.cachedAccountAt = time.Now()
	return account, nil
}

// ensureFreeSpace rejects an add when put.io free space is below the configured
// floor. Disabled when minFreeSpace == 0. Fails OPEN: a put.io lookup error logs
// a warning and returns nil so a transient API hiccup never blocks downloads.
func (s *Server) ensureFreeSpace(ctx context.Context) error {
	if s.minFreeSpace <= 0 {
		return nil
	}
	account, err := s.cachedAccountInfo(ctx)
	if err != nil {
		log.Warn("server").
			Err(err).
			Msg("Free-space check skipped: could not fetch put.io account info")
		return nil
	}
	if account.Disk.Avail < s.minFreeSpace {
		log.Warn("server").
			Int64("avail_bytes", account.Disk.Avail).
			Int64("min_free_bytes", s.minFreeSpace).
			Msg("Rejecting torrent-add: put.io free space below configured floor")
		return fmt.Errorf("put.io free space %d bytes is below the configured minimum of %d bytes", account.Disk.Avail, s.minFreeSpace)
	}
	return nil
}

// Start begins listening for RPC requests
func (s *Server) Start() error {
	// Initialize server first
	mux := http.NewServeMux()
	mux.HandleFunc("/transmission/rpc", s.handleRPC)

	s.srv = &http.Server{
		Addr:    s.cfg.ListenAddr,
		Handler: mux,
	}

	// Get and log account info
	account, err := s.client.GetAccountInfo(context.Background())
	if err != nil {
		log.Warn("server").Err(err).Msg("Failed to get account info")
	} else {
		log.Info("server").
			Str("username", account.Username).
			Int64("storage_used_mb", account.Disk.Used/1024/1024).
			Int64("storage_total_mb", account.Disk.Size/1024/1024).
			Int64("storage_available_mb", account.Disk.Avail/1024/1024).
			Msg("Put.io account status")
	}

	// Check initial disk quota
	if overQuota, err := s.checkDiskQuota(); err != nil {
		log.Warn("server").Err(err).Msg("Failed to check initial disk quota")
	} else if overQuota {
		log.Warn("server").Msg("Put.io account is over quota on startup")
	}

	// Start quota monitoring
	go func() {
		for {
			select {
			case <-s.quotaTicker.C:
				if _, err := s.checkDiskQuota(); err != nil {
					log.Error("server").Err(err).Msg("Failed to check disk quota")
				}
			case <-s.stopChan:
				return
			}
		}
	}()

	log.Info("server").Str("addr", s.cfg.ListenAddr).Msg("Starting transmission-rpc server")
	return s.srv.ListenAndServe()
}

// Stop gracefully shuts down the server
func (s *Server) Stop() error {
	s.quotaTicker.Stop()
	close(s.stopChan)

	// Stop the download service
	s.dlService.Stop()

	if s.srv != nil {
		return s.srv.Close()
	}
	return nil
}
