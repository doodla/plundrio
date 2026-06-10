// Package demo provides a self-contained, deterministic fake put.io client for
// the dashboard's `--demo` mode. It compiles into the binary (it is NOT a
// _test.go fake) and satisfies BOTH runtime put.io interfaces:
//
//   - server.PutioClient   (GetAccountInfo, GetTransfers, UploadFile,
//     AddTransfer, DeleteFile, DeleteTransfer)
//   - download.PutioClient (GetTransfers, GetAllTransferFiles, RetryTransfer,
//     DeleteTransfer, DeleteFile, GetDownloadURL)
//
// The Client emits a realistic, evolving set of transfers that progress through
// both phases over time: put.io fetch (PercentDone 0→100, status
// IN_QUEUE→DOWNLOADING→COMPLETED) followed by the local download phase, which
// the real download.Manager drives by fetching bytes from this Client's own
// in-process HTTP file server. A plausible account/quota is reported and
// synthetic log lines are emitted at varied levels.
//
// Determinism: all evolving state is driven by a virtual clock advanced by a
// fixed step on each background tick (no time.Now() in the advancement logic),
// and synthetic content is generated from a fixed seed. Screenshot runs are
// therefore reproducible. The Client never reads a real OAuth token path and
// logs loudly at construction that fake data is being served.
package demo

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/doodla/go-putio"
	"github.com/doodla/plundrio/internal/api"
	"github.com/doodla/plundrio/internal/log"
)

// demoSeed seeds synthetic content generation. Fixed so byte streams (and thus
// downloaded sizes / on-disk files) are identical across runs.
const demoSeed = 0x70_75_74_69_6f // "putio"

// virtualStep is how much virtual time advances per background tick. The tick
// fires on wall time but the *amount* advanced is fixed, so the transfer
// trajectory is wall-clock independent: a run that ticks 100 times always sees
// the same virtual elapsed time regardless of host speed.
const (
	virtualStep   = 2 * time.Second
	tickInterval  = 250 * time.Millisecond
	fileChunkSize = 1 << 16 // 64 KiB synthetic file-server response chunk
)

// scenario describes one demo transfer's deterministic trajectory. Times are
// virtual-clock offsets from t0 (when advancement starts).
type scenario struct {
	id         int64
	hash       string
	name       string
	category   string
	sizeBytes  int64
	files      []demoFile // leaf files served for the local phase
	putioStart time.Duration
	// putioDur is how long (virtual) the put.io fetch phase takes; PercentDone
	// ramps 0→100 linearly across it.
	putioDur time.Duration
	// errored, when set, holds a put.io ERROR status + message instead of
	// completing. Exercises the dashboard's error rendering path.
	errored    bool
	errMessage string
}

type demoFile struct {
	id      int64
	relPath string
	size    int64
}

// demoScenarios is the fixed cast of transfers. Chosen to exercise multiple
// dashboard states simultaneously: one mid-put.io-fetch, one ready for local
// download, one multi-file with a Subs/ subdir, and one errored.
//
// Sizes are deliberately MODEST (a handful of MB each, not the GBs a real
// 1080p/2160p release would be). The local phase ACTUALLY writes these bytes to
// disk via grab, so realistic GB sizes would mean GBs of synthetic data per
// demo/screenshot run. A demo dashboard showing a 12 MB "episode" is obviously
// a demo; correctness (grab's Content-Length check, the manager's on-disk
// size compare) requires displayed size == served size, so the two can't
// diverge — modest sizes are the lever.
var demoScenarios = []scenario{
	{
		id: 4001, hash: "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2", category: "tv",
		name:      "Foundation.S02E05.1080p.WEB.H264-DEMO",
		sizeBytes: 12_000_000,
		files: []demoFile{
			{id: 50001, relPath: "Foundation.S02E05.1080p.WEB.H264-DEMO.mkv", size: 12_000_000},
		},
		putioStart: 0, putioDur: 30 * time.Second,
	},
	{
		id: 4002, hash: "b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3", category: "movies",
		name:      "The.Demo.Movie.2026.2160p.UHD.BluRay-FAKE",
		sizeBytes: 40_000_000,
		files: []demoFile{
			{id: 50002, relPath: "The.Demo.Movie.2026.2160p.UHD.BluRay-FAKE.mkv", size: 40_000_000},
		},
		putioStart: 6 * time.Second, putioDur: 60 * time.Second,
	},
	{
		id: 4003, hash: "c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4", category: "tv",
		name:      "Severance.S02E01.MULTI.1080p-DEMO",
		sizeBytes: 15_136_000,
		files: []demoFile{
			{id: 50003, relPath: "Severance.S02E01.MULTI.1080p-DEMO.mkv", size: 15_000_000},
			{id: 50004, relPath: "Subs/2_English.srt", size: 64_000},
			{id: 50005, relPath: "Subs/3_French.srt", size: 72_000},
		},
		putioStart: 2 * time.Second, putioDur: 20 * time.Second,
	},
	{
		id: 4004, hash: "d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5", category: "",
		name:       "Broken.Torrent.Demo-DEAD",
		sizeBytes:  7_000_000,
		putioStart: 4 * time.Second, putioDur: 18 * time.Second,
		errored: true, errMessage: "torrent has no seeders (demo)",
	},
}

// Client is the deterministic demo put.io client. Safe for concurrent use.
type Client struct {
	mu sync.Mutex

	// virtual is the accumulated virtual time since advancement started. All
	// transfer progress is a pure function of this value + the scenarios.
	virtual time.Time
	t0      time.Time // virtual epoch; fixed, never wall-clock

	// transfers is the current rendered snapshot, rebuilt each tick from the
	// virtual clock. Keyed by transfer ID.
	transfers map[int64]*putio.Transfer
	// deletedTransfers tracks transfers removed via DeleteTransfer so they stay
	// gone across subsequent rebuilds, mirroring real put.io behavior.
	deletedTransfers map[int64]bool

	folderID int64

	fileSrv  *http.Server
	fileAddr string

	stop     chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup

	// logTick cycles synthetic log lines across components/levels.
	logTick int
}

// NewClient constructs a demo Client, starts its in-process file server and the
// background advancement+log loops. folderID is the put.io folder the daemon
// resolved at startup; demo transfers report SaveParentID == folderID so the
// download manager's folder filter admits them.
//
// Callers MUST call Stop() on shutdown to release the file-server listener and
// background goroutines.
func NewClient(folderID int64) *Client {
	c := &Client{
		// t0 is a FIXED virtual epoch, not time.Now(): rendered created_at /
		// finished_at timestamps are then reproducible across runs.
		t0:               time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		transfers:        make(map[int64]*putio.Transfer),
		deletedTransfers: make(map[int64]bool),
		folderID:         folderID,
		stop:             make(chan struct{}),
	}
	c.virtual = c.t0

	log.Warn("demo").
		Bool("fake_data", true).
		Int64("folder_id", folderID).
		Msg("DEMO MODE ACTIVE — serving synthetic put.io data; no real account or token is used")

	c.startFileServer()
	c.rebuild() // publish an initial snapshot before the first tick

	c.wg.Add(1)
	go c.advanceLoop()
	c.wg.Add(1)
	go c.syntheticLogLoop()

	return c
}

// Stop halts the background loops and shuts down the file server. Idempotent.
func (c *Client) Stop() {
	c.stopOnce.Do(func() {
		close(c.stop)
		if c.fileSrv != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = c.fileSrv.Shutdown(ctx)
			cancel()
		}
	})
	c.wg.Wait()
}

// startFileServer binds an ephemeral loopback listener that serves synthetic
// deterministic bytes for any file ID. The real download.Manager fetches these
// via GetDownloadURL → grab, which drives the visible local-download phase.
func (c *Client) startFileServer() {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		// A loopback bind failing is unrecoverable for demo mode; surface it
		// loudly rather than serving a half-broken demo.
		log.Fatal("demo").Err(err).Msg("failed to bind demo file server")
	}
	c.fileAddr = ln.Addr().String()

	mux := http.NewServeMux()
	mux.HandleFunc("/file/", c.handleFile)
	c.fileSrv = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		_ = c.fileSrv.Serve(ln)
	}()
}

// handleFile streams `size` deterministic bytes for the requested file ID. The
// size is taken from the matching scenario file so grab sees a Content-Length
// and writes a complete file of the expected size.
func (c *Client) handleFile(w http.ResponseWriter, r *http.Request) {
	var id int64
	if _, err := fmt.Sscanf(r.URL.Path, "/file/%d", &id); err != nil {
		http.Error(w, "bad file id", http.StatusBadRequest)
		return
	}
	size := c.fileSize(id)
	if size < 0 {
		http.Error(w, "unknown file", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", size))
	if r.Method == http.MethodHead {
		return
	}

	// Stream deterministic bytes derived from the seed + file id. No allocation
	// of the whole file; chunked so an 18 GB demo file doesn't cost 18 GB RAM.
	chunk := make([]byte, fileChunkSize)
	for i := range chunk {
		chunk[i] = byte((int64(i) + id + demoSeed) % 251)
	}
	var written int64
	for written < size {
		n := int64(len(chunk))
		if remaining := size - written; remaining < n {
			n = remaining
		}
		if _, err := w.Write(chunk[:n]); err != nil {
			return // client (grab) went away; nothing to do
		}
		written += n
	}
}

// fileSize returns the declared size of a demo file ID, or -1 if unknown.
func (c *Client) fileSize(id int64) int64 {
	for _, s := range demoScenarios {
		for _, f := range s.files {
			if f.id == id {
				return f.size
			}
		}
	}
	return -1
}

// advanceLoop ticks the virtual clock forward by a fixed step and rebuilds the
// transfer snapshot. Wall-clock only paces the loop; the trajectory depends on
// virtual time only.
func (c *Client) advanceLoop() {
	defer c.wg.Done()
	t := time.NewTicker(tickInterval)
	defer t.Stop()
	for {
		select {
		case <-c.stop:
			return
		case <-t.C:
			c.mu.Lock()
			c.virtual = c.virtual.Add(virtualStep)
			c.rebuild()
			c.mu.Unlock()
		}
	}
}

// AdvanceForTest advances the virtual clock by d and rebuilds the snapshot,
// without waiting on the wall-clock ticker. Test-only seam (mirrors the
// SetProcessedAtForTest precedent in internal/download) so tests can drive the
// deterministic trajectory in one step instead of sleeping.
func (c *Client) AdvanceForTest(d time.Duration) {
	c.mu.Lock()
	c.virtual = c.virtual.Add(d)
	c.rebuild()
	c.mu.Unlock()
}

// rebuild recomputes the transfer snapshot from the current virtual clock.
// Caller holds c.mu.
func (c *Client) rebuild() {
	elapsed := c.virtual.Sub(c.t0)
	for _, s := range demoScenarios {
		if c.deletedTransfers[s.id] {
			delete(c.transfers, s.id)
			continue
		}
		c.transfers[s.id] = c.renderScenario(s, elapsed)
	}
}

// renderScenario maps virtual elapsed time to a putio.Transfer for one
// scenario. The put.io fetch phase ramps PercentDone 0→100 over putioDur, then
// the transfer reports COMPLETED (status the download manager treats as ready)
// so the manager begins the local phase. An errored scenario reports ERROR.
func (c *Client) renderScenario(s scenario, elapsed time.Duration) *putio.Transfer {
	created := putio.Time{Time: c.t0.Add(s.putioStart)}
	t := &putio.Transfer{
		ID:           s.id,
		Hash:         s.hash,
		Name:         s.name,
		Size:         int(s.sizeBytes),
		FileID:       s.id, // file_id == transfer id for the demo (root listing keyed off it)
		SaveParentID: c.folderID,
		CreatedAt:    &created,
	}

	switch {
	case elapsed < s.putioStart:
		t.Status = "IN_QUEUE"
		t.PercentDone = 0
	case s.errored && elapsed >= s.putioStart+s.putioDur:
		t.Status = "ERROR"
		t.ErrorMessage = s.errMessage
		t.PercentDone = 0
	case elapsed < s.putioStart+s.putioDur:
		// Mid put.io fetch: ramp PercentDone, report DOWNLOADING with a
		// plausible speed + ETA.
		frac := float64(elapsed-s.putioStart) / float64(s.putioDur)
		if frac < 0 {
			frac = 0
		}
		pct := int(frac * 100)
		if pct > 99 {
			pct = 99 // reserve 100 for the COMPLETED state
		}
		t.Status = "DOWNLOADING"
		t.PercentDone = pct
		t.Downloaded = int64(float64(s.sizeBytes) * frac)
		t.DownloadSpeed = 8_500_000 // ~8.5 MB/s, plausible put.io fetch rate
		remaining := s.putioDur - (elapsed - s.putioStart)
		t.EstimatedTime = int64(remaining.Seconds())
	default:
		// put.io fetch complete; ready for local download.
		t.Status = "COMPLETED"
		t.PercentDone = 100
		t.Downloaded = s.sizeBytes
		finished := putio.Time{Time: c.t0.Add(s.putioStart + s.putioDur)}
		t.FinishedAt = &finished
	}
	return t
}

// --- shared put.io interface methods -------------------------------------

// GetTransfers returns the current demo snapshot (server + download interface).
func (c *Client) GetTransfers(ctx context.Context) ([]*putio.Transfer, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]*putio.Transfer, 0, len(c.transfers))
	for _, t := range c.transfers {
		cp := *t
		out = append(out, &cp)
	}
	return out, nil
}

// GetAccountInfo returns a plausible, fixed account + quota (server interface).
func (c *Client) GetAccountInfo(ctx context.Context) (*putio.AccountInfo, error) {
	ai := &putio.AccountInfo{
		AccountActive:          true,
		Username:               "demo-operator",
		DaysUntilFilesDeletion: 0,
	}
	// ~1 TB plan, 62% used — comfortably under the 95% over-quota threshold so
	// the normal (not warning) account state renders by default.
	const tb = int64(1_000_000_000_000)
	ai.Disk.Size = tb
	ai.Disk.Used = 620_000_000_000
	ai.Disk.Avail = ai.Disk.Size - ai.Disk.Used
	return ai, nil
}

// --- server.PutioClient-only methods -------------------------------------

// UploadFile is a no-op in demo mode (the dashboard cannot create transfers in
// v1; add-torrent stays an *arr/RPC concern). Returns an empty hash.
func (c *Client) UploadFile(ctx context.Context, data []byte, filename string, folderID int64) (string, error) {
	log.Info("demo").Str("filename", filename).Msg("demo UploadFile (no-op)")
	return "", nil
}

// AddTransfer is a no-op in demo mode. Returns an empty hash.
func (c *Client) AddTransfer(ctx context.Context, magnetLink string, folderID int64) (string, error) {
	log.Info("demo").Msg("demo AddTransfer (no-op)")
	return "", nil
}

// --- download.PutioClient-only methods -----------------------------------

// GetAllTransferFiles returns the leaf files for a demo transfer (download
// interface). fileID is the transfer's FileID (== scenario id in the demo).
func (c *Client) GetAllTransferFiles(ctx context.Context, fileID int64) ([]api.TransferFile, error) {
	for _, s := range demoScenarios {
		if s.id != fileID {
			continue
		}
		out := make([]api.TransferFile, 0, len(s.files))
		for _, f := range s.files {
			out = append(out, api.TransferFile{
				File: &putio.File{
					ID:          f.id,
					Name:        baseName(f.relPath),
					Size:        f.size,
					ContentType: "video/x-matroska",
				},
				RelPath: f.relPath,
			})
		}
		return out, nil
	}
	return nil, nil
}

// GetDownloadURL returns a URL pointing at this Client's in-process file server
// so grab fetches deterministic synthetic bytes (download interface).
func (c *Client) GetDownloadURL(ctx context.Context, fileID int64) (string, error) {
	return fmt.Sprintf("http://%s/file/%d", c.fileAddr, fileID), nil
}

// RetryTransfer is a no-op that echoes the current transfer back (download
// interface). The demo scenarios don't drive the failed-retry cascade.
func (c *Client) RetryTransfer(ctx context.Context, transferID int64) (*putio.Transfer, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if t, ok := c.transfers[transferID]; ok {
		cp := *t
		return &cp, nil
	}
	return nil, nil
}

// --- methods in BOTH interfaces ------------------------------------------

// DeleteFile is a tolerant no-op in demo mode (both interfaces); the rendered
// snapshot is keyed off scenarios + deletedTransfers, so deleting a single file
// has no visible effect. Matches real put.io's idempotent-ish delete.
func (c *Client) DeleteFile(ctx context.Context, fileID int64) error {
	log.Debug("demo").Int64("file_id", fileID).Msg("demo DeleteFile")
	return nil
}

// DeleteTransfer marks a demo transfer deleted so it vanishes from subsequent
// GetTransfers (both interfaces).
func (c *Client) DeleteTransfer(ctx context.Context, transferID int64) error {
	c.mu.Lock()
	c.deletedTransfers[transferID] = true
	delete(c.transfers, transferID)
	c.mu.Unlock()
	log.Debug("demo").Int64("transfer_id", transferID).Msg("demo DeleteTransfer")
	return nil
}

// --- synthetic logs -------------------------------------------------------

// syntheticLogLoop emits log lines at varied levels/components on a steady
// cadence so the dashboard's live-log view has flowing content even before a
// real transfer reaches the local phase.
func (c *Client) syntheticLogLoop() {
	defer c.wg.Done()
	t := time.NewTicker(800 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-c.stop:
			return
		case <-t.C:
			c.emitSyntheticLog()
		}
	}
}

// syntheticLogLines is a fixed, cycled set covering debug/info/warn/error so
// the log stream exercises every level the dashboard renders.
var syntheticLogLines = []struct {
	level     string
	component string
	msg       string
}{
	{"info", "transfers", "Transfer status summary"},
	{"debug", "download", "Flushed remaining download bytes"},
	{"info", "server", "Put.io account status"},
	{"warn", "transfers", "Failed transfer waiting for backoff window"},
	{"debug", "transfers", "Checking transfers"},
	{"error", "download", "Retrying download after error"},
	{"info", "purge", "Deleted transfer"},
}

func (c *Client) emitSyntheticLog() {
	c.mu.Lock()
	i := c.logTick % len(syntheticLogLines)
	c.logTick++
	c.mu.Unlock()

	l := syntheticLogLines[i]
	var ev = log.Info(l.component)
	switch l.level {
	case "debug":
		ev = log.Debug(l.component)
	case "warn":
		ev = log.Warn(l.component)
	case "error":
		ev = log.Error(l.component)
	}
	ev.Bool("demo", true).Int("seq", i).Msg(l.msg)
}

// baseName returns the final path element of a forward-slash relative path.
func baseName(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[i+1:]
		}
	}
	return p
}
