package demo

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"
)

const testFolderID int64 = 99000

// findTransfer returns the transfer with the given id from a snapshot, or nil.
func findTransfer(ts []*putioTransfer, id int64) *putioTransfer {
	for _, t := range ts {
		if t.ID == id {
			return t
		}
	}
	return nil
}

// putioTransfer aliases the put.io transfer type so the test reads cleanly
// without importing the put.io package name everywhere.
type putioTransfer = struct {
	ID            int64
	Status        string
	PercentDone   int
	Downloaded    int64
	DownloadSpeed int
	EstimatedTime int64
	ErrorMessage  string
	Name          string
}

// snapshot pulls GetTransfers into the lightweight alias above.
func snapshot(t *testing.T, c *Client) []*putioTransfer {
	t.Helper()
	raw, err := c.GetTransfers(context.Background())
	if err != nil {
		t.Fatalf("GetTransfers: %v", err)
	}
	out := make([]*putioTransfer, 0, len(raw))
	for _, r := range raw {
		out = append(out, &putioTransfer{
			ID: r.ID, Status: r.Status, PercentDone: r.PercentDone,
			Downloaded: r.Downloaded, DownloadSpeed: r.DownloadSpeed,
			EstimatedTime: r.EstimatedTime, ErrorMessage: r.ErrorMessage, Name: r.Name,
		})
	}
	return out
}

func TestDemoTransferTrajectory(t *testing.T) {
	c := NewClient(testFolderID)
	defer c.Stop()

	// t=0: scenario 4001 (putioStart 0) is mid-fetch or just starting; 4002
	// (putioStart 6s) is still IN_QUEUE.
	ts := snapshot(t, c)
	if got := findTransfer(ts, 4002); got == nil || got.Status != "IN_QUEUE" {
		t.Fatalf("at t=0, transfer 4002 should be IN_QUEUE, got %+v", got)
	}

	// Advance to mid put.io fetch for 4001 (putioDur 30s): expect DOWNLOADING
	// with a non-zero, sub-100 PercentDone and a plausible speed.
	c.AdvanceForTest(15 * time.Second)
	mid := findTransfer(snapshot(t, c), 4001)
	if mid == nil || mid.Status != "DOWNLOADING" {
		t.Fatalf("at t=15s, 4001 should be DOWNLOADING, got %+v", mid)
	}
	if mid.PercentDone <= 0 || mid.PercentDone >= 100 {
		t.Errorf("4001 PercentDone should be in (0,100), got %d", mid.PercentDone)
	}
	if mid.DownloadSpeed <= 0 {
		t.Errorf("4001 DownloadSpeed should be >0 during fetch, got %d", mid.DownloadSpeed)
	}
	if mid.EstimatedTime <= 0 {
		t.Errorf("4001 EstimatedTime should be >0 during fetch, got %d", mid.EstimatedTime)
	}

	// Advance well past every putioDur (longest 66s): all non-errored COMPLETED,
	// the errored scenario in ERROR with a message.
	c.AdvanceForTest(120 * time.Second)
	final := snapshot(t, c)
	for _, id := range []int64{4001, 4002, 4003} {
		got := findTransfer(final, id)
		if got == nil || got.Status != "COMPLETED" || got.PercentDone != 100 {
			t.Errorf("transfer %d should be COMPLETED at 100%%, got %+v", id, got)
		}
	}
	errored := findTransfer(final, 4004)
	if errored == nil || errored.Status != "ERROR" {
		t.Fatalf("transfer 4004 should be ERROR, got %+v", errored)
	}
	if errored.ErrorMessage == "" {
		t.Error("errored transfer must carry an ErrorMessage")
	}
}

func TestDemoDeterministic(t *testing.T) {
	// Two independently-constructed clients advanced by the same virtual delta
	// must produce identical snapshots — the reproducibility guarantee that
	// makes screenshots stable.
	a := NewClient(testFolderID)
	defer a.Stop()
	b := NewClient(testFolderID)
	defer b.Stop()

	a.AdvanceForTest(25 * time.Second)
	b.AdvanceForTest(25 * time.Second)

	sa, sb := snapshot(t, a), snapshot(t, b)
	if len(sa) != len(sb) {
		t.Fatalf("snapshot lengths differ: %d vs %d", len(sa), len(sb))
	}
	for _, id := range []int64{4001, 4002, 4003, 4004} {
		ta, tb := findTransfer(sa, id), findTransfer(sb, id)
		if ta == nil || tb == nil {
			t.Fatalf("transfer %d missing in one snapshot", id)
		}
		if ta.Status != tb.Status || ta.PercentDone != tb.PercentDone || ta.Downloaded != tb.Downloaded {
			t.Errorf("transfer %d not deterministic: %+v vs %+v", id, ta, tb)
		}
	}
}

func TestDemoSatisfiesAccountContract(t *testing.T) {
	c := NewClient(testFolderID)
	defer c.Stop()

	ai, err := c.GetAccountInfo(context.Background())
	if err != nil {
		t.Fatalf("GetAccountInfo: %v", err)
	}
	if ai.Username == "" {
		t.Error("account username must be set")
	}
	if ai.Disk.Size <= 0 || ai.Disk.Used <= 0 {
		t.Errorf("disk size/used must be positive, got size=%d used=%d", ai.Disk.Size, ai.Disk.Used)
	}
	if ai.Disk.Avail != ai.Disk.Size-ai.Disk.Used {
		t.Errorf("disk avail must equal size-used, got %d", ai.Disk.Avail)
	}
	// Used must be under the 95%% over-quota threshold so the default render is
	// the normal (non-warning) state.
	if pct := float64(ai.Disk.Used) / float64(ai.Disk.Size) * 100; pct >= 95 {
		t.Errorf("demo used_percent should be under 95%%, got %.1f", pct)
	}
}

func TestDemoFileServerServesDeclaredSize(t *testing.T) {
	c := NewClient(testFolderID)
	defer c.Stop()

	// Pull the leaf files for scenario 4003 (multi-file) and assert each file
	// the server serves matches the declared size grab would expect.
	files, err := c.GetAllTransferFiles(context.Background(), 4003)
	if err != nil {
		t.Fatalf("GetAllTransferFiles: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("scenario 4003 should have 3 files, got %d", len(files))
	}
	for _, f := range files {
		url, err := c.GetDownloadURL(context.Background(), f.File.ID)
		if err != nil {
			t.Fatalf("GetDownloadURL(%d): %v", f.File.ID, err)
		}
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", url, err)
		}
		n, _ := io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if n != f.File.Size {
			t.Errorf("file %d: server sent %d bytes, declared %d", f.File.ID, n, f.File.Size)
		}
		if cl := resp.Header.Get("Content-Length"); cl == "" {
			t.Errorf("file %d: missing Content-Length (grab needs it)", f.File.ID)
		}
	}
}

func TestDemoDeleteTransferRemovesFromSnapshot(t *testing.T) {
	c := NewClient(testFolderID)
	defer c.Stop()

	c.AdvanceForTest(120 * time.Second) // get everything to a stable state
	if got := findTransfer(snapshot(t, c), 4001); got == nil {
		t.Fatal("4001 should exist before delete")
	}
	if err := c.DeleteTransfer(context.Background(), 4001); err != nil {
		t.Fatalf("DeleteTransfer: %v", err)
	}
	// rebuild on the next advance must keep it gone (deleted is sticky).
	c.AdvanceForTest(2 * time.Second)
	if got := findTransfer(snapshot(t, c), 4001); got != nil {
		t.Errorf("4001 should be gone after DeleteTransfer, got %+v", got)
	}
}
