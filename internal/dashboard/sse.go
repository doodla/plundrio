package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"
)

const (
	// subscriberBuffer is the per-subscriber channel capacity. A subscriber that
	// can't drain this fast is dropped (backpressure = drop slow client) rather
	// than blocking the producer.
	subscriberBuffer = 256
	// logRingCapacity bounds the snapshot log history. A Pi can't hold unbounded
	// log history and the frontend caps its own buffer too.
	logRingCapacity = 500
	// transfersTick / accountTick are the broadcast cadences (contract taxonomy).
	transfersTick = 1 * time.Second
	accountTick   = 15 * time.Second
)

// logEvent is the parsed form of one zerolog line, matching the contract's
// <LogEvent> (and internal/logassert.LogEvent): time/level/component/message
// plus remaining structured fields verbatim.
type logEvent struct {
	Time      string         `json:"time"`
	Level     string         `json:"level"`
	Component string         `json:"component"`
	Message   string         `json:"message"`
	Fields    map[string]any `json:"fields"`
}

// frame is a serialized SSE event ready to write: the `event:`/`data:` lines
// already formatted. Subscribers receive frames, not raw payloads, so each
// subscriber's slow write doesn't re-serialize.
type frame []byte

// hub is the SSE broadcast hub: it owns the subscriber set, the bounded log ring
// buffer, and the transfers/account ticker goroutines. The log fan-out arrives
// via logWriter (installed as the internal/log sink) and must never block the
// logger.
type hub struct {
	ctx context.Context
	d   *Dashboard

	mu          sync.Mutex
	subscribers map[*subscriber]struct{}
	ring        []frame // bounded log history for the snapshot

	startOnce sync.Once
}

type subscriber struct {
	ch     chan frame
	closed chan struct{} // closed when the subscriber is dropped/disconnected
}

func newHub(ctx context.Context, d *Dashboard) *hub {
	return &hub{
		ctx:         ctx,
		d:           d,
		subscribers: make(map[*subscriber]struct{}),
		ring:        make([]frame, 0, logRingCapacity),
	}
}

// start launches the periodic broadcast goroutines (transfers + account). Safe
// to call once; subsequent calls are no-ops.
func (h *hub) start() {
	h.startOnce.Do(func() {
		go h.transfersLoop()
		go h.accountLoop()
	})
}

// transfersLoop broadcasts the full transfer list on a ~1s ticker.
func (h *hub) transfersLoop() {
	t := time.NewTicker(transfersTick)
	defer t.Stop()
	for {
		select {
		case <-h.ctx.Done():
			return
		case <-t.C:
			payload := map[string]any{"transfers": h.d.renderTransfers()}
			h.broadcast(encodeFrame("transfers", payload))
		}
	}
}

// accountLoop broadcasts the account/quota on a ~15s ticker. A failed put.io
// call is skipped (the next tick retries); it never tears down the loop.
func (h *hub) accountLoop() {
	t := time.NewTicker(accountTick)
	defer t.Stop()
	for {
		select {
		case <-h.ctx.Done():
			return
		case <-t.C:
			acc, err := h.d.fetchAccount()
			if err != nil {
				continue
			}
			h.broadcast(encodeFrame("account", acc))
		}
	}
}

// logWriter returns the io.Writer installed as the internal/log fan-out sink. It
// parses each zerolog JSON line into a logEvent, appends to the ring buffer, and
// broadcasts a `log` frame. It MUST NOT block: broadcast drops slow subscribers
// rather than waiting, and a parse failure is ignored (the console leg still got
// the line). zerolog feeds one JSON object per Write.
func (h *hub) logWriter() io.Writer {
	return writerFunc(func(p []byte) (int, error) {
		ev, ok := parseLogLine(p)
		if ok {
			f := encodeFrame("log", ev)
			h.appendRing(f)
			h.broadcast(f)
		}
		return len(p), nil
	})
}

// broadcastSettings pushes a re-resolved settings list to all subscribers (on a
// successful PUT) so multiple tabs stay consistent.
func (h *hub) broadcastSettings(settings []SettingEntry) {
	h.broadcast(encodeFrame("settings", map[string]any{"settings": settings}))
}

// appendRing adds a log frame to the bounded ring buffer, evicting the oldest
// when full.
func (h *hub) appendRing(f frame) {
	h.mu.Lock()
	if len(h.ring) >= logRingCapacity {
		// Drop the oldest. copy keeps the slice header stable and bounded.
		copy(h.ring, h.ring[1:])
		h.ring[len(h.ring)-1] = f
	} else {
		h.ring = append(h.ring, f)
	}
	h.mu.Unlock()
}

// broadcast sends f to every subscriber without blocking: a subscriber whose
// buffer is full is dropped (its channel closed, its handler goroutine reaped).
// A slow browser degrades only its own stream.
func (h *hub) broadcast(f frame) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.subscribers {
		select {
		case s.ch <- f:
		default:
			// Buffer full → drop this slow subscriber. Closing `closed` signals
			// its handler to return; removing it here prevents further sends.
			h.dropLocked(s)
		}
	}
}

// dropLocked removes a subscriber and signals its handler to exit. Caller holds
// h.mu. Idempotent: closing an already-closed `closed` is guarded.
func (h *hub) dropLocked(s *subscriber) {
	if _, ok := h.subscribers[s]; !ok {
		return
	}
	delete(h.subscribers, s)
	close(s.closed)
}

// register adds a new subscriber and returns it.
func (h *hub) register() *subscriber {
	s := &subscriber{
		ch:     make(chan frame, subscriberBuffer),
		closed: make(chan struct{}),
	}
	h.mu.Lock()
	h.subscribers[s] = struct{}{}
	h.mu.Unlock()
	return s
}

// unregister removes a subscriber (e.g. the browser tab closed). Idempotent.
func (h *hub) unregister(s *subscriber) {
	h.mu.Lock()
	h.dropLocked(s)
	h.mu.Unlock()
}

// snapshot builds the one-time on-connect payload: current transfers + account +
// settings + the ring buffer's recent log events. account errors degrade to a
// zero-value account so the snapshot still lands (the next account tick fixes
// it); the contract requires the four keys present.
func (h *hub) snapshot() map[string]any {
	acc, err := h.d.fetchAccount()
	if err != nil {
		acc = AccountDTO{} // keep the key present; the field types stay valid
	}

	h.mu.Lock()
	logs := make([]json.RawMessage, 0, len(h.ring))
	for _, f := range h.ring {
		if data, ok := frameData(f); ok {
			logs = append(logs, data)
		}
	}
	h.mu.Unlock()

	return map[string]any{
		"transfers": h.d.renderTransfers(),
		"account":   acc,
		"settings":  h.d.resolveSettings(),
		"logs":      logs,
	}
}

// handleSSE serves GET /api/events: registers a subscriber, writes a `snapshot`
// event immediately, then streams subsequent frames until the client
// disconnects, the subscriber is dropped, or the hub context is cancelled.
func (h *hub) handleSSE(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, codeMethodNotAllowed, "method not allowed", "")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, codeUpstreamError, "streaming unsupported", "")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	// Snapshot first so no view is blank on load (reconnection re-sends a fresh
	// snapshot; no Last-Event-ID replay in v1).
	if _, err := w.Write(encodeFrame("snapshot", h.snapshot())); err != nil {
		return
	}
	flusher.Flush()

	s := h.register()
	defer h.unregister(s)

	for {
		select {
		case <-h.ctx.Done(): // dashboard.Stop()
			return
		case <-r.Context().Done(): // browser tab closed
			return
		case <-s.closed: // dropped as a slow subscriber
			return
		case f := <-s.ch:
			if _, err := w.Write(f); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// --- encoding helpers ------------------------------------------------------

// encodeFrame serializes a named SSE event. The payload is JSON-marshaled onto
// a single data: line (the contract's events are single-line JSON). A marshal
// failure yields an empty-data frame rather than panicking — the producer must
// never crash.
func encodeFrame(event string, payload any) frame {
	data, err := json.Marshal(payload)
	if err != nil {
		data = []byte("{}")
	}
	var b bytes.Buffer
	b.WriteString("event: ")
	b.WriteString(event)
	b.WriteString("\ndata: ")
	b.Write(data)
	b.WriteString("\n\n")
	return b.Bytes()
}

// frameData extracts the JSON payload from a frame's data: line, for the
// snapshot's logs array (which carries the raw log objects, not re-wrapped
// frames). Returns false if the frame has no data line.
func frameData(f frame) (json.RawMessage, bool) {
	const marker = "\ndata: "
	i := bytes.Index(f, []byte(marker))
	if i < 0 {
		return nil, false
	}
	rest := f[i+len(marker):]
	end := bytes.Index(rest, []byte("\n\n"))
	if end < 0 {
		return nil, false
	}
	return json.RawMessage(rest[:end]), true
}

// parseLogLine parses one zerolog JSON line into a logEvent. Reserved top-level
// keys (time/level/component/message) are lifted; everything else collapses into
// Fields. Returns false if the line isn't a JSON object or lacks a level.
func parseLogLine(line []byte) (logEvent, bool) {
	var raw map[string]any
	if err := json.Unmarshal(line, &raw); err != nil {
		return logEvent{}, false
	}
	ev := logEvent{Fields: map[string]any{}}
	for k, v := range raw {
		switch k {
		case "time":
			ev.Time, _ = v.(string)
		case "level":
			ev.Level, _ = v.(string)
		case "component":
			ev.Component, _ = v.(string)
		case "message":
			ev.Message, _ = v.(string)
		default:
			ev.Fields[k] = v
		}
	}
	if ev.Level == "" {
		return logEvent{}, false
	}
	return ev, true
}

// writerFunc adapts a func to io.Writer.
type writerFunc func(p []byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }
