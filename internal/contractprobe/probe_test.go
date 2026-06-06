package contractprobe

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"
)

// runtimeGoexit aborts the current goroutine, mirroring testing.T.Fatalf's
// behavior so a fatal probe assertion stops at that point.
func runtimeGoexit() { runtime.Goexit() }

// recordingT is a TB that records failures instead of failing a real test, so
// the self-test can assert the prober DETECTS a broken payload. Fatalf/Fatal
// abort via runtime.Goexit (like testing.T) — runProbe runs the probe in a
// goroutine so that Goexit ends only that goroutine.
type recordingT struct {
	failures []string
}

func (r *recordingT) Helper() {}
func (r *recordingT) Error(args ...any) {
	r.failures = append(r.failures, fmt.Sprint(args...))
}
func (r *recordingT) Errorf(format string, args ...any) {
	r.failures = append(r.failures, fmt.Sprintf(format, args...))
}
func (r *recordingT) Fatal(args ...any) {
	r.failures = append(r.failures, fmt.Sprint(args...))
	runtimeGoexit()
}
func (r *recordingT) Fatalf(format string, args ...any) {
	r.failures = append(r.failures, fmt.Sprintf(format, args...))
	runtimeGoexit()
}

// runProbe runs fn(rt) to completion (including a Fatal* Goexit) and returns the
// recorded failures.
func runProbe(fn func(t TB)) []string {
	rt := &recordingT{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn(rt)
	}()
	<-done
	return rt.failures
}

// --- stub payloads ---------------------------------------------------------

// correctServer serves contract-conformant REST + SSE. This is the reference a
// real M4/M6 dashboard must match; the prober must pass it clean.
func correctServer() *httptest.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/transfers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(correctTransfersJSON))
	})
	mux.HandleFunc("/api/account", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(correctAccountJSON))
	})
	mux.HandleFunc("/api/settings", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(correctSettingsJSON))
	})
	mux.HandleFunc("/api/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		// snapshot first, then an incremental transfers event. A real SSE
		// server emits compact (single-line) JSON in each data: field.
		fmt.Fprintf(w, "event: snapshot\ndata: %s\n\n", compactJSON(correctSnapshotJSON))
		if fl != nil {
			fl.Flush()
		}
		fmt.Fprintf(w, "event: transfers\ndata: %s\n\n", compactJSON(correctTransfersJSON))
		if fl != nil {
			fl.Flush()
		}
		// keep the stream open briefly so the client reads both events
		time.Sleep(200 * time.Millisecond)
	})
	return httptest.NewServer(mux)
}

const correctTransfersJSON = `{"transfers":[{
  "id":12345,"hash":"abcdef","name":"Show.S01E01","category":"tv",
  "size":1073741824,"putio_status":"DOWNLOADING","lifecycle_state":"Downloading",
  "percent_done":0.42,"left_until_done":620000000,
  "putio_phase":{"percent_done":84,"downloaded":900000000,"rate_download":5242880,"eta_seconds":120},
  "local_phase":{"downloaded":450000000,"total":1073741824,"completed_files":3,"failed_files":0,"total_files":5,"rate_download":8388608,"eta_seconds":60},
  "error":false,"error_string":"","permanent":false,
  "processed_at":null,"created_at":"2026-06-05T12:00:00Z","finished_at":null
},{
  "id":999,"hash":"ff00","name":"Fetching","category":"",
  "size":500,"putio_status":"DOWNLOADING","lifecycle_state":"None",
  "percent_done":0.10,"left_until_done":450,
  "putio_phase":{"percent_done":20,"downloaded":100,"rate_download":50,"eta_seconds":40},
  "local_phase":null,
  "error":false,"error_string":"","permanent":false,
  "processed_at":null,"created_at":"2026-06-05T12:00:00Z","finished_at":null
}]}`

const correctAccountJSON = `{
  "username":"demo-operator",
  "disk":{"size":1000000000000,"used":620000000000,"avail":380000000000},
  "used_percent":62.0,"over_quota":false,"days_until_files_deletion":0
}`

const correctSettingsJSON = `{"settings":[
  {"key":"log_level","value":"info","source":"default","locked":false,"live":true,"restart_required":false},
  {"key":"workers","value":4,"source":"file","locked":false,"live":true,"restart_required":false},
  {"key":"token","value":null,"source":"env","locked":true,"live":false,"restart_required":true,"is_set":true}
]}`

var correctSnapshotJSON = fmt.Sprintf(`{"transfers":%s,"account":%s,"settings":%s,"logs":[{"time":"2026-06-05T12:00:00Z","level":"info","component":"transfers","message":"hi","fields":{}}]}`,
	`[]`, correctAccountJSON, `[]`)

// --- tests -----------------------------------------------------------------

func TestProberPassesCorrectServer(t *testing.T) {
	srv := correctServer()
	defer srv.Close()

	// Run against the recording T: a conformant server must produce ZERO
	// failures. (Using the recorder rather than t directly keeps the assertion
	// symmetric with the failure tests and reports all failures at once.)
	if got := runProbe(func(tb TB) { ProbeContract(tb, srv.URL) }); len(got) != 0 {
		t.Fatalf("prober reported failures against a conformant server:\n%s", strings.Join(got, "\n"))
	}
}

// brokenServer serves the correct payloads except for the one endpoint named by
// `broken`, which gets a deliberately-wrong payload. This proves the prober
// FAILS — a gate that can't detect a violation manufactures false confidence.
func brokenServer(broken, payload string) *httptest.Server {
	bodies := map[string]string{
		"transfers": correctTransfersJSON,
		"account":   correctAccountJSON,
		"settings":  correctSettingsJSON,
	}
	bodies[broken] = payload

	mux := http.NewServeMux()
	mux.HandleFunc("/api/transfers", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(bodies["transfers"]))
	})
	mux.HandleFunc("/api/account", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(bodies["account"]))
	})
	mux.HandleFunc("/api/settings", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(bodies["settings"]))
	})
	return httptest.NewServer(mux)
}

func TestProberDetectsWrongType(t *testing.T) {
	// percent_done as a string instead of a number must be caught.
	bad := strings.Replace(correctTransfersJSON, `"percent_done":0.42`, `"percent_done":"forty-two"`, 1)
	srv := brokenServer("transfers", bad)
	defer srv.Close()

	got := runProbe(func(tb TB) { ProbeTransfers(tb, srv.URL) })
	if len(got) == 0 {
		t.Fatal("prober PASSED a payload with percent_done as a string — gate cannot detect failure")
	}
	if !containsSubstr(got, "percent_done") {
		t.Errorf("expected a failure mentioning percent_done, got:\n%s", strings.Join(got, "\n"))
	}
}

func TestProberDetectsMissingField(t *testing.T) {
	// Drop the required `hash` field.
	bad := strings.Replace(correctTransfersJSON, `"hash":"abcdef",`, ``, 1)
	srv := brokenServer("transfers", bad)
	defer srv.Close()

	got := runProbe(func(tb TB) { ProbeTransfers(tb, srv.URL) })
	if len(got) == 0 {
		t.Fatal("prober PASSED a payload missing the hash field")
	}
	if !containsSubstr(got, "hash") {
		t.Errorf("expected a failure mentioning hash, got:\n%s", strings.Join(got, "\n"))
	}
}

func TestProberDetectsBadUnit(t *testing.T) {
	// percent_done out of the 0.0–1.0 range (here 42, the 0–100 mistake).
	bad := strings.Replace(correctTransfersJSON, `"percent_done":0.42`, `"percent_done":42`, 1)
	srv := brokenServer("transfers", bad)
	defer srv.Close()

	got := runProbe(func(tb TB) { ProbeTransfers(tb, srv.URL) })
	if len(got) == 0 {
		t.Fatal("prober PASSED percent_done=42 (should be 0.0–1.0) — unit check missing")
	}
}

func TestProberDetectsTokenLeak(t *testing.T) {
	// The settings token entry leaks the secret into `value`.
	bad := strings.Replace(correctSettingsJSON,
		`{"key":"token","value":null`,
		`{"key":"token","value":"`+DemoTokenSentinel+`"`, 1)
	srv := brokenServer("settings", bad)
	defer srv.Close()

	got := runProbe(func(tb TB) { ProbeSettings(tb, srv.URL) })
	if len(got) == 0 {
		t.Fatal("prober PASSED a settings payload containing the token — security check missing")
	}
	if !containsSubstr(got, "token") {
		t.Errorf("expected a token-related failure, got:\n%s", strings.Join(got, "\n"))
	}
}

func TestProberDetectsWrongSSEFirstEvent(t *testing.T) {
	// Server sends `transfers` before `snapshot` — the prober requires snapshot
	// first.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		fmt.Fprintf(w, "event: transfers\ndata: %s\n\n", compactJSON(correctTransfersJSON))
		if fl != nil {
			fl.Flush()
		}
		fmt.Fprintf(w, "event: transfers\ndata: %s\n\n", compactJSON(correctTransfersJSON))
		if fl != nil {
			fl.Flush()
		}
		time.Sleep(200 * time.Millisecond)
	}))
	defer srv.Close()

	got := runProbe(func(tb TB) { ProbeSSE(tb, srv.URL, 3*time.Second) })
	if len(got) == 0 {
		t.Fatal("prober PASSED an SSE stream whose first event was not snapshot")
	}
	if !containsSubstr(got, "snapshot") {
		t.Errorf("expected a snapshot-ordering failure, got:\n%s", strings.Join(got, "\n"))
	}
}

func TestProberDetectsMissingSSEIncremental(t *testing.T) {
	// Server sends only the snapshot, never an incremental event. The prober's
	// timeout must fire and report it.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		fmt.Fprintf(w, "event: snapshot\ndata: %s\n\n", compactJSON(correctSnapshotJSON))
		if fl != nil {
			fl.Flush()
		}
		time.Sleep(2 * time.Second) // never send a second event within the probe timeout
	}))
	defer srv.Close()

	got := runProbe(func(tb TB) { ProbeSSE(tb, srv.URL, 1*time.Second) })
	if len(got) == 0 {
		t.Fatal("prober PASSED an SSE stream that never sent an incremental event")
	}
}

// compactJSON strips the source newlines from a JSON literal so it fits on one
// SSE data: line, mirroring how a real SSE server serializes events.
func compactJSON(s string) string {
	return strings.NewReplacer("\n", "", "\r", "").Replace(s)
}

func containsSubstr(ss []string, sub string) bool {
	for _, s := range ss {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
