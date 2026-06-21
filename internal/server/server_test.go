package server

import (
	"net"
	"testing"
	"time"

	"github.com/doodla/go-putio"
)

// TestCheckDiskQuotaZeroSize guards against the division-by-zero edge case:
// an account reporting Disk.Size == 0 (unlimited plan, or a zero-value Disk
// from an API hiccup) must not produce a NaN/+Inf percentage and a spurious
// "over quota" result.
func TestCheckDiskQuotaZeroSize(t *testing.T) {
	acc := &putio.AccountInfo{Username: "u"}
	acc.Disk.Size = 0
	acc.Disk.Used = 1234 // Used>0, Size==0 would be +Inf% without the guard
	srv := newTestServer(t, &fakePutioClient{accountInfo: acc}, newFakeDownloadService())

	over, err := srv.checkDiskQuota()
	if err != nil {
		t.Fatalf("checkDiskQuota: %v", err)
	}
	if over {
		t.Error("over quota = true for zero-size disk, want false")
	}
	if srv.quotaWarning.Load() {
		t.Error("quotaWarning set for zero-size disk, want unset")
	}
}

// TestCheckDiskQuotaLatchAndRecovery covers the warning latch: usage >= 95%
// reports over-quota and sets the one-shot warning; when usage later drops the
// latch resets so a future spike can warn again.
func TestCheckDiskQuotaLatchAndRecovery(t *testing.T) {
	acc := &putio.AccountInfo{Username: "u"}
	acc.Disk.Size = 1000
	acc.Disk.Used = 960 // 96% — at/over the 95% threshold
	fake := &fakePutioClient{accountInfo: acc}
	srv := newTestServer(t, fake, newFakeDownloadService())

	// Pin a controllable clock so we can expire the 60s account cache between
	// the over-quota and recovery observations.
	base := time.Now()
	srv.now = func() time.Time { return base }

	over, err := srv.checkDiskQuota()
	if err != nil {
		t.Fatalf("checkDiskQuota: %v", err)
	}
	if !over {
		t.Error("over = false at 96% usage, want true")
	}
	if !srv.quotaWarning.Load() {
		t.Error("quotaWarning latch not set after crossing threshold")
	}

	// Usage drops; expire the cache so the new figure is fetched.
	fake.mu.Lock()
	fake.accountInfo.Disk.Used = 500 // 50%
	fake.mu.Unlock()
	srv.now = func() time.Time { return base.Add(2 * time.Minute) }

	over, err = srv.checkDiskQuota()
	if err != nil {
		t.Fatalf("checkDiskQuota (recovery): %v", err)
	}
	if over {
		t.Error("over = true at 50% usage, want false")
	}
	if srv.quotaWarning.Load() {
		t.Error("quotaWarning latch not reset after usage dropped")
	}
}

// TestServerStartReturnsNilOnCleanStop guards the regression where Start
// returned http.ErrServerClosed on a normal Stop. main.go log.Fatals on any
// non-nil Start error, so leaking ErrServerClosed turned every clean SIGTERM
// shutdown into a fatal exit(1). A clean Stop must make Start return nil.
func TestServerStartReturnsNilOnCleanStop(t *testing.T) {
	// Reserve a free localhost port, then hand it to the server.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	srv := newTestServer(t, &fakePutioClient{}, newFakeDownloadService())
	srv.cfg.ListenAddr = addr

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Start() }()

	// Wait until the listener is actually accepting before stopping, so we
	// exercise the ListenAndServe → ErrServerClosed path rather than a pre-bind
	// race.
	deadline := time.Now().Add(3 * time.Second)
	for {
		c, derr := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if derr == nil {
			_ = c.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server did not start listening on %s", addr)
		}
		time.Sleep(10 * time.Millisecond)
	}

	if err := srv.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("Start returned %v, want nil after a clean Stop", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after Stop")
	}
}
