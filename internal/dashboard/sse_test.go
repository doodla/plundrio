package dashboard

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/doodla/plundrio/internal/log"
	"github.com/doodla/plundrio/internal/logassert"
)

// TestHubFanOut: a frame broadcast reaches every registered subscriber.
func TestHubFanOut(t *testing.T) {
	d := newTestDashboard(newFakeService(), &fakeAccountClient{}, "")
	h := d.hub

	a := h.register()
	b := h.register()

	h.broadcast(encodeFrame("transfers", map[string]any{"transfers": []any{}}))

	for name, s := range map[string]*subscriber{"a": a, "b": b} {
		select {
		case f := <-s.ch:
			if got := string(f); got == "" {
				t.Errorf("subscriber %s got empty frame", name)
			}
		case <-time.After(time.Second):
			t.Errorf("subscriber %s did not receive the broadcast", name)
		}
	}
}

// TestHubSlowSubscriberDoesNotBlockProducer: a subscriber that never drains is
// dropped once its buffer fills; the broadcast call still returns promptly and
// other subscribers keep receiving. This is the core backpressure invariant —
// the log producer (the logger's write path) must never block.
func TestHubSlowSubscriberDoesNotBlockProducer(t *testing.T) {
	d := newTestDashboard(newFakeService(), &fakeAccountClient{}, "")
	h := d.hub

	slow := h.register() // never drained
	fast := h.register()

	// Overfill well past the buffer cap. If broadcast blocked on the slow
	// subscriber, this loop would hang; the test's overall timeout would fire.
	done := make(chan struct{})
	go func() {
		for i := 0; i < subscriberBuffer*3; i++ {
			h.broadcast(encodeFrame("log", map[string]any{"seq": i}))
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("broadcast blocked on a slow subscriber — producer was back-pressured")
	}

	// The slow subscriber must have been dropped (its closed channel signaled).
	select {
	case <-slow.closed:
	case <-time.After(time.Second):
		t.Error("slow subscriber was not dropped after its buffer filled")
	}

	// The fast subscriber's channel still holds frames (it wasn't dropped).
	if len(fast.ch) == 0 {
		t.Error("fast subscriber received nothing; it should still be live")
	}
}

// TestHubRingBufferBounded: the log ring never exceeds logRingCapacity and keeps
// the most-recent events (oldest evicted).
func TestHubRingBufferBounded(t *testing.T) {
	d := newTestDashboard(newFakeService(), &fakeAccountClient{}, "")
	h := d.hub

	for i := 0; i < logRingCapacity+50; i++ {
		h.appendRing(encodeFrame("log", map[string]any{"seq": i}))
	}

	h.mu.Lock()
	got := len(h.ring)
	first := h.ring[0]
	h.mu.Unlock()

	if got != logRingCapacity {
		t.Fatalf("ring length = %d, want capped at %d", got, logRingCapacity)
	}
	// The oldest retained event should be seq=50 (0..49 evicted).
	data, ok := frameData(first)
	if !ok {
		t.Fatal("ring[0] has no data line")
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal ring[0]: %v", err)
	}
	if seq, _ := payload["seq"].(float64); seq != 50 {
		t.Errorf("oldest retained seq = %v, want 50 (FIFO eviction)", payload["seq"])
	}
}

// TestSnapshotContents: the snapshot carries the four contract keys, with the
// recent ring events in logs.
func TestSnapshotContents(t *testing.T) {
	d := newTestDashboard(newFakeService(), &fakeAccountClient{}, "")
	h := d.hub
	h.appendRing(encodeFrame("log", map[string]any{"level": "info", "message": "hi"}))

	snap := h.snapshot()
	for _, k := range []string{"transfers", "account", "settings", "logs"} {
		if _, ok := snap[k]; !ok {
			t.Errorf("snapshot missing key %q", k)
		}
	}
	logs, ok := snap["logs"].([]json.RawMessage)
	if !ok {
		t.Fatalf("snapshot.logs type = %T, want []json.RawMessage", snap["logs"])
	}
	if len(logs) != 1 {
		t.Errorf("snapshot.logs len = %d, want 1", len(logs))
	}
}

// TestLogWriterFanOutStructured wires the hub's logWriter as the internal/log
// sink and asserts emitted log lines arrive as STRUCTURED events (parseable via
// the same logassert.ParseLine the integration-verifier uses) and respect the
// active level gate. The StdoutTap proves stdout is preserved in parallel.
func TestLogWriterFanOutStructured(t *testing.T) {
	d := newTestDashboard(newFakeService(), &fakeAccountClient{}, "")
	h := d.hub

	// Subscribe BEFORE installing the sink so we capture the fan-out.
	s := h.register()

	// Capture stdout too, proving the console leg is preserved. NewStdoutTap
	// rebinds the logger via SetLevel(Info); install our sink AFTER so it
	// survives that reconfigure (SetHubWriter is independent of configureLogger).
	tap := logassert.NewStdoutTap(log.LevelInfo)
	defer tap.Restore()
	log.SetHubWriter(h.logWriter())
	defer log.SetHubWriter(nil)

	log.Info("transfers").Str("name", "x").Msg("a structured info line")
	log.Debug("transfers").Msg("a debug line that must be gated out at info")

	// Drain frames from the subscriber; only the info line should arrive.
	var events []logassert.LogEvent
	deadline := time.After(2 * time.Second)
collect:
	for {
		select {
		case f := <-s.ch:
			data, ok := frameData(f)
			if !ok {
				continue
			}
			ev, err := logassert.ParseLine(data)
			if err != nil {
				t.Errorf("hub log frame is not a structured event: %v (%s)", err, data)
				continue
			}
			events = append(events, ev)
		case <-deadline:
			break collect
		case <-time.After(200 * time.Millisecond):
			// no more frames for a beat → done collecting
			break collect
		}
	}

	if len(events) == 0 {
		t.Fatal("no log events reached the hub subscriber")
	}
	logassert.AssertLevelGated(t, events, "info")
	for _, ev := range events {
		if ev.Level == "debug" {
			t.Errorf("debug line leaked past the info gate: %+v", ev)
		}
		if ev.Component == "" {
			t.Errorf("log event missing component: %+v", ev)
		}
	}

	// Stdout was preserved: the tap captured at least the info line.
	tap.Restore() // flush the drain goroutine before reading
	if len(tap.Lines()) == 0 {
		t.Error("stdout console leg captured nothing; fan-out must not replace stdout")
	}
}
