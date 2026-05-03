package download

import (
	"context"
	"sync"
	"testing"
	"time"
)

// setupForStopRace prepares a Manager for a Stop-race test without spawning
// workers or the monitor. We set running and ctx manually so Stop's preflight
// + stopOnce.Do path runs through; without the goroutines, workerWg/monitorWg
// waits are no-ops.
func setupForStopRace(t *testing.T) *Manager {
	t.Helper()
	m := newTestManagerWithClient(&fakePutioClient{})
	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.mu.Lock()
	m.running = true
	m.mu.Unlock()
	return m
}

// TestStopDoesNotPanicWithConcurrentQueueDownload reproduces issue #2. Before
// the fix, Stop closed m.jobs racing with QueueDownload's `m.jobs <- job`
// send, occasionally panicking with "send on closed channel". With close
// removed, the same workload runs cleanly.
//
// Setup mirrors production: a draining goroutine plays the role of the worker
// pool (otherwise QueueDownload blocks holding m.mu and Stop deadlocks at
// m.mu.Lock — that path is unrelated to the panic). Many senders produce
// constant pressure so a sender is parked in select when stopChan closes.
func TestStopDoesNotPanicWithConcurrentQueueDownload(t *testing.T) {
	m := setupForStopRace(t)

	// Drain m.jobs in a loop so QueueDownload never permanently parks
	// holding m.mu. Stand-in for the real worker pool.
	drainerDone := make(chan struct{})
	go func() {
		defer close(drainerDone)
		for {
			select {
			case <-m.stopChan:
				return
			case _, ok := <-m.jobs:
				if !ok {
					return
				}
			}
		}
	}()

	const senders = 64
	var wg sync.WaitGroup
	wg.Add(senders)
	for i := 0; i < senders; i++ {
		fileID := int64(i + 1)
		go func() {
			defer wg.Done()
			// A few iterations per sender to extend the race window.
			for j := 0; j < 50; j++ {
				m.QueueDownload(downloadJob{
					FileID:     fileID*1000 + int64(j),
					Name:       "file",
					TransferID: 1,
				})
			}
		}()
	}

	// Give senders a head start, then trigger Stop concurrently with the
	// in-flight sends.
	time.Sleep(2 * time.Millisecond)
	m.Stop()

	wg.Wait()
	<-drainerDone
}

// TestStopMultipleTimesIsSafe asserts stopOnce still guards repeated Stop
// calls.
func TestStopMultipleTimesIsSafe(t *testing.T) {
	m := setupForStopRace(t)
	m.Stop()
	m.Stop()
	m.Stop()
}
