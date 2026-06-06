package download

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/doodla/go-putio"
	"github.com/doodla/plundrio/internal/api"
	"github.com/doodla/plundrio/internal/config"
)

// gateClient is a PutioClient whose GetDownloadURL blocks until released, so a
// worker that picks up a job parks there and counts as "busy." This makes the
// number of concurrently-busy workers directly observable (the live-worker
// gauge) without needing real byte transfers: a shrink can't retire a busy
// worker until its in-flight job returns, which is exactly what releasing the
// gate lets the test drive deterministically.
//
// The release barrier is a generation: openGate() arms a fresh barrier and
// resets the high-water mark, releaseAll() trips the current one. This lets one
// test run two waves of blocking jobs on a single client and measure each
// wave's concurrency independently.
type gateClient struct {
	mu      sync.Mutex
	busy    int32 // workers currently parked in GetDownloadURL
	maxBusy int32 // high-water mark of concurrent busy workers (current wave)
	entered chan struct{}
	release chan struct{} // current barrier; closed by releaseAll
}

func newGateClient() *gateClient {
	return &gateClient{
		entered: make(chan struct{}, 1024),
		release: make(chan struct{}),
	}
}

func (g *gateClient) GetDownloadURL(ctx context.Context, fileID int64) (string, error) {
	g.mu.Lock()
	barrier := g.release
	g.mu.Unlock()

	n := atomic.AddInt32(&g.busy, 1)
	for {
		old := atomic.LoadInt32(&g.maxBusy)
		if n <= old || atomic.CompareAndSwapInt32(&g.maxBusy, old, n) {
			break
		}
	}
	g.entered <- struct{}{}
	// Park until this wave's barrier is released OR the manager's context is
	// cancelled (Stop). Returning a non-URL afterward makes downloadFile fail
	// fast (no real grab transfer) — the test cares about occupancy, not bytes.
	select {
	case <-barrier:
	case <-ctx.Done():
	}
	atomic.AddInt32(&g.busy, -1)
	return "", context.Canceled
}

// releaseAll trips the current barrier, unparking every worker waiting on it.
func (g *gateClient) releaseAll() {
	g.mu.Lock()
	defer g.mu.Unlock()
	select {
	case <-g.release:
		// already released
	default:
		close(g.release)
	}
}

// openGate arms a fresh barrier and resets the concurrency high-water mark so
// the next wave is measured independently of the last. Drains stale entered
// tokens too.
func (g *gateClient) openGate() {
	g.mu.Lock()
	g.release = make(chan struct{})
	g.mu.Unlock()
	atomic.StoreInt32(&g.maxBusy, 0)
	for {
		select {
		case <-g.entered:
		default:
			return
		}
	}
}

// Remaining PutioClient methods are inert for the resize tests.
func (g *gateClient) GetTransfers(ctx context.Context) ([]*putio.Transfer, error) { return nil, nil }
func (g *gateClient) GetAllTransferFiles(ctx context.Context, fileID int64) ([]api.TransferFile, error) {
	return nil, nil
}
func (g *gateClient) RetryTransfer(ctx context.Context, transferID int64) (*putio.Transfer, error) {
	return nil, nil
}
func (g *gateClient) DeleteTransfer(ctx context.Context, transferID int64) error { return nil }
func (g *gateClient) DeleteFile(ctx context.Context, fileID int64) error         { return nil }

// waitBusy blocks until `want` distinct workers have parked in GetDownloadURL,
// or fails after a timeout. Each park sends one token on g.entered.
func waitBusy(t *testing.T, g *gateClient, want int) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	got := 0
	for got < want {
		select {
		case <-g.entered:
			got++
		case <-deadline:
			t.Fatalf("only %d/%d workers became busy before timeout", got, want)
		}
	}
}

func newResizeManager(client PutioClient, workers int) *Manager {
	cfg := &config.Config{TargetDir: "/tmp/plundrio-resize-test", WorkerCount: workers}
	return New(cfg, client)
}

// TestSetWorkerCountGrow asserts a grow spawns enough workers to run the new
// target's worth of jobs concurrently. Starting at 2, growing to 5, then
// queueing 5 blocking jobs must park all 5 simultaneously — proving 5 live
// workers, not 2.
func TestSetWorkerCountGrow(t *testing.T) {
	g := newGateClient()
	m := newResizeManager(g, 2)
	m.Start()
	defer func() { g.releaseAll(); m.Stop() }()

	if got := m.GetWorkerCount(); got != 2 {
		t.Fatalf("initial GetWorkerCount = %d, want 2", got)
	}

	m.SetWorkerCount(5)
	if got := m.GetWorkerCount(); got != 5 {
		t.Fatalf("after grow GetWorkerCount = %d, want 5", got)
	}

	for i := 0; i < 5; i++ {
		m.QueueDownload(downloadJob{FileID: int64(i + 1), Name: "f", TransferID: 1})
	}
	waitBusy(t, g, 5) // all five run at once ⇒ five live workers
}

// TestSetWorkerCountShrinkConverges asserts the goroutine count converges to the
// shrunk target after in-flight jobs finish. Five busy workers, shrink to 2,
// release the in-flight jobs: the three surplus workers retire. A second batch
// of blocking jobs must then park at most 2 concurrently.
func TestSetWorkerCountShrinkConverges(t *testing.T) {
	g := newGateClient()
	m := newResizeManager(g, 5)
	m.Start()
	defer m.Stop()

	for i := 0; i < 5; i++ {
		m.QueueDownload(downloadJob{FileID: int64(i + 1), Name: "f", TransferID: 1})
	}
	waitBusy(t, g, 5)

	m.SetWorkerCount(2)
	if got := m.GetWorkerCount(); got != 2 {
		t.Fatalf("after shrink GetWorkerCount = %d, want 2 (reported target updates synchronously)", got)
	}

	// Release the in-flight jobs. Each worker's GetDownloadURL returns, its job
	// fails fast, and it loops back to the select. With the queue now empty, the
	// only ready case for the 3 surplus workers is workerQuit, so they exit; the
	// 2 survivors block on an empty select. Wait for all workers to unwind
	// (busy → 0) and a settle window for the surplus to retire before measuring.
	g.releaseAll()
	waitIdle(t, g)
	time.Sleep(150 * time.Millisecond) // let the 3 surplus workers read workerQuit and exit

	// Second wave on a fresh barrier: only the 2 surviving workers can pick
	// these up, so concurrency must peak at exactly 2.
	g.openGate()
	const wave = 6
	for i := 0; i < wave; i++ {
		m.QueueDownload(downloadJob{FileID: int64(100 + i), Name: "f", TransferID: 2})
	}
	waitBusy(t, g, 2)

	// Give any erroneously-surviving worker a window to also park, then assert
	// the high-water mark never exceeded the target.
	time.Sleep(200 * time.Millisecond)
	if hi := atomic.LoadInt32(&g.maxBusy); hi > 2 {
		t.Fatalf("post-shrink concurrent busy workers peaked at %d, want <= 2 (pool did not converge)", hi)
	}

	g.releaseAll()
}

// waitIdle blocks until no worker is parked in GetDownloadURL (busy == 0).
func waitIdle(t *testing.T, g *gateClient) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for atomic.LoadInt32(&g.busy) != 0 {
		select {
		case <-deadline:
			t.Fatalf("workers did not unwind to idle (busy=%d) before timeout", atomic.LoadInt32(&g.busy))
		default:
			time.Sleep(2 * time.Millisecond)
		}
	}
}

// TestSetWorkerCountConcurrentWithQueueAndStop is the issue-#2 regression guard
// under the race detector: SetWorkerCount (grow + shrink), QueueDownload, and
// Stop all run concurrently. The invariant is no panic — neither jobs nor
// stopChan may be closed by a resize, so a send racing a resize can never hit a
// closed channel.
func TestSetWorkerCountConcurrentWithQueueAndStop(t *testing.T) {
	g := newGateClient()
	m := newResizeManager(g, 4)
	m.Start()

	var wg sync.WaitGroup

	// Resizers: hammer grow/shrink.
	wg.Add(2)
	for r := 0; r < 2; r++ {
		go func(seed int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				m.SetWorkerCount(1 + (i+seed)%8)
			}
		}(r)
	}

	// Queuers: constant job pressure so a sender is parked in QueueDownload's
	// select when Stop closes stopChan.
	wg.Add(4)
	for q := 0; q < 4; q++ {
		go func(base int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				m.QueueDownload(downloadJob{
					FileID:     int64(base*100000 + i),
					Name:       "f",
					TransferID: int64(base),
				})
			}
		}(q + 1)
	}

	// Let pressure build, then Stop concurrently with everything.
	time.Sleep(2 * time.Millisecond)
	g.releaseAll() // unpark any workers so they can observe stopChan
	m.Stop()

	wg.Wait()
}

// TestSetWorkerCountNoopWhenNotRunning asserts a resize on a stopped manager is
// a no-op (no goroutines spawned, no tokens pushed) — Stop has already closed
// stopChan and the pool is draining.
func TestSetWorkerCountNoopWhenNotRunning(t *testing.T) {
	g := newGateClient()
	m := newResizeManager(g, 4)
	// Never Started: running is false.
	m.SetWorkerCount(8)
	if got := m.GetWorkerCount(); got != 0 {
		t.Fatalf("SetWorkerCount on non-running manager changed count to %d, want 0 (no-op)", got)
	}
}

// TestSetWorkerCountFloorsAtOne asserts a sub-1 target is floored to 1 rather
// than spawning zero workers (which would wedge the queue).
func TestSetWorkerCountFloorsAtOne(t *testing.T) {
	g := newGateClient()
	m := newResizeManager(g, 4)
	m.Start()
	defer func() { g.releaseAll(); m.Stop() }()

	m.SetWorkerCount(0)
	if got := m.GetWorkerCount(); got != 1 {
		t.Fatalf("SetWorkerCount(0) → GetWorkerCount = %d, want 1 (floored)", got)
	}
}
