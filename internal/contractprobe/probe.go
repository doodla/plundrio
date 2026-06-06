// Package contractprobe is the endpoint prober: a reusable, self-tested harness
// that asserts a running dashboard server matches the FROZEN API/SSE contract
// in .agents/dashboard/plan/contract.md.
//
// It is written against the contract, not against the (M4) dashboard package,
// so it self-tests NOW against a stub server (probe_test.go) and is pointed at
// the real demo-mode server later by the integration-verifier. ProbeContract
// takes a base URL + a TB (a *testing.T or the self-test's recording fake) and
// asserts:
//
//   - GET /api/transfers   — { "transfers": [<Transfer>...] }, each Transfer's
//     fields present with the contract's types/units (two-phase model).
//   - GET /api/account     — username + disk{size,used,avail} bytes,
//     used_percent, over_quota, days_until_files_deletion.
//   - GET /api/settings    — { "settings": [<SettingEntry>...] } with the
//     token entry reporting value:null + is_set, never the secret.
//   - GET /api/events (SSE) — a `snapshot` event arrives on connect, then at
//     least one incremental named event within a timeout.
//   - Security: NO response body (REST or SSE) contains the demo token value.
//
// The prober is deliberately strict on field PRESENCE and TYPE but lenient on
// VALUE (a demo server's exact numbers vary): a contract gate proves the shape
// is right, not that a particular transfer is at 42%.
package contractprobe

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// TB is the slice of *testing.T the prober uses. *testing.T satisfies it, and so
// does the recording fake in probe_test.go — that fake is how the self-test
// proves the prober can DETECT a contract violation (a gate that can only pass
// is worthless). Fatalf must abort the calling goroutine (runtime.Goexit), like
// testing.T.Fatalf, so a fatal assertion stops the probe at that point.
type TB interface {
	Helper()
	Error(args ...any)
	Errorf(format string, args ...any)
	Fatal(args ...any)
	Fatalf(format string, args ...any)
}

// DemoTokenSentinel is a value the integration harness can seed as the demo
// "token" so the security scan has a concrete needle to grep for across every
// payload. The real demo client never serves a token, so finding this string in
// any response is a contract violation (the token leaked).
const DemoTokenSentinel = "DEMO-OAUTH-TOKEN-SHOULD-NEVER-APPEAR-a1b2c3"

// httpGet fetches a URL and returns status + body, failing the test on transport
// error. Short timeout so a hung server fails the gate instead of hanging CI.
func httpGet(t TB, url string) (int, []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("build request %s: %v", url, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body %s: %v", url, err)
	}
	return resp.StatusCode, body
}

// requireFields asserts every key in want is present in obj. kind labels the
// object in failure messages. Returns false if any are missing (caller may skip
// deeper checks).
func requireFields(t TB, kind string, obj map[string]any, want ...string) bool {
	t.Helper()
	ok := true
	for _, k := range want {
		if _, present := obj[k]; !present {
			t.Errorf("%s: missing required field %q", kind, k)
			ok = false
		}
	}
	return ok
}

// isNumber reports whether v decoded from JSON as a number (json.Number or
// float64). The probe decodes with UseNumber so int64 byte fields don't lose
// precision and so "is it a number" is unambiguous.
func isNumber(v any) bool {
	switch v.(type) {
	case json.Number, float64:
		return true
	default:
		return false
	}
}

func requireNumber(t TB, kind, field string, v any) {
	t.Helper()
	if !isNumber(v) {
		t.Errorf("%s: field %q must be a number, got %T (%v)", kind, field, v, v)
	}
}

func requireString(t TB, kind, field string, v any) {
	t.Helper()
	if _, ok := v.(string); !ok {
		t.Errorf("%s: field %q must be a string, got %T (%v)", kind, field, v, v)
	}
}

func requireBool(t TB, kind, field string, v any) {
	t.Helper()
	if _, ok := v.(bool); !ok {
		t.Errorf("%s: field %q must be a bool, got %T (%v)", kind, field, v, v)
	}
}

// decodeObject unmarshals body into a map with number precision preserved.
func decodeObject(t TB, kind string, body []byte) map[string]any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.UseNumber()
	var obj map[string]any
	if err := dec.Decode(&obj); err != nil {
		t.Fatalf("%s: body is not a JSON object: %v\nbody: %s", kind, err, body)
	}
	return obj
}

// ProbeTransfers asserts GET /api/transfers and the per-Transfer two-phase shape.
func ProbeTransfers(t TB, base string) {
	t.Helper()
	status, body := httpGet(t, base+"/api/transfers")
	if status != http.StatusOK {
		t.Fatalf("/api/transfers: status %d, want 200 (body: %s)", status, body)
	}
	assertNoToken(t, "/api/transfers", body)

	obj := decodeObject(t, "/api/transfers", body)
	if !requireFields(t, "/api/transfers", obj, "transfers") {
		return
	}
	list, ok := obj["transfers"].([]any)
	if !ok {
		t.Fatalf("/api/transfers: \"transfers\" must be an array, got %T", obj["transfers"])
	}
	for i, raw := range list {
		tr, ok := raw.(map[string]any)
		if !ok {
			t.Errorf("transfers[%d] must be an object, got %T", i, raw)
			continue
		}
		assertTransfer(t, fmt.Sprintf("transfers[%d]", i), tr)
	}
}

// assertTransfer checks the frozen <Transfer> object's fields, types and the
// nested putio_phase / local_phase legs.
func assertTransfer(t TB, kind string, tr map[string]any) {
	t.Helper()
	requireFields(t, kind, tr,
		"id", "hash", "name", "category",
		"size", "putio_status", "lifecycle_state",
		"percent_done", "left_until_done",
		"putio_phase", "local_phase",
		"error", "error_string", "permanent",
		"processed_at", "created_at", "finished_at",
	)
	requireNumber(t, kind, "id", tr["id"])
	requireString(t, kind, "hash", tr["hash"])
	requireString(t, kind, "name", tr["name"])
	requireString(t, kind, "category", tr["category"])
	requireNumber(t, kind, "size", tr["size"])
	requireString(t, kind, "putio_status", tr["putio_status"])
	requireString(t, kind, "lifecycle_state", tr["lifecycle_state"])
	requireNumber(t, kind, "percent_done", tr["percent_done"])
	requireNumber(t, kind, "left_until_done", tr["left_until_done"])
	requireBool(t, kind, "error", tr["error"])
	requireString(t, kind, "error_string", tr["error_string"])
	requireBool(t, kind, "permanent", tr["permanent"])

	// percent_done must be the 0.0–1.0 float convention.
	if n, ok := asFloat(tr["percent_done"]); ok && (n < 0 || n > 1) {
		t.Errorf("%s: percent_done must be 0.0–1.0, got %v", kind, n)
	}

	// putio_phase is always present (0–50% leg).
	if pp, ok := tr["putio_phase"].(map[string]any); ok {
		ppKind := kind + ".putio_phase"
		requireFields(t, ppKind, pp, "percent_done", "downloaded", "rate_download", "eta_seconds")
		requireNumber(t, ppKind, "percent_done", pp["percent_done"])
		requireNumber(t, ppKind, "downloaded", pp["downloaded"])
		requireNumber(t, ppKind, "rate_download", pp["rate_download"])
		requireNumber(t, ppKind, "eta_seconds", pp["eta_seconds"])
		// putio_phase.percent_done is the 0–100 int convention.
		if n, ok := asFloat(pp["percent_done"]); ok && (n < 0 || n > 100) {
			t.Errorf("%s.percent_done must be 0–100, got %v", ppKind, n)
		}
	} else {
		t.Errorf("%s: putio_phase must be an object, got %T", kind, tr["putio_phase"])
	}

	// local_phase is null when there's no TransferContext yet; an object once
	// local tracking starts. Both are contract-valid.
	switch lp := tr["local_phase"].(type) {
	case nil:
		// valid (no local tracking yet)
	case map[string]any:
		lpKind := kind + ".local_phase"
		requireFields(t, lpKind, lp,
			"downloaded", "total", "completed_files", "failed_files",
			"total_files", "rate_download", "eta_seconds")
		requireNumber(t, lpKind, "downloaded", lp["downloaded"])
		requireNumber(t, lpKind, "total", lp["total"])
		requireNumber(t, lpKind, "completed_files", lp["completed_files"])
		requireNumber(t, lpKind, "failed_files", lp["failed_files"])
		requireNumber(t, lpKind, "total_files", lp["total_files"])
		requireNumber(t, lpKind, "rate_download", lp["rate_download"])
		requireNumber(t, lpKind, "eta_seconds", lp["eta_seconds"])
	default:
		t.Errorf("%s: local_phase must be an object or null, got %T", kind, tr["local_phase"])
	}
}

// ProbeAccount asserts GET /api/account.
func ProbeAccount(t TB, base string) {
	t.Helper()
	status, body := httpGet(t, base+"/api/account")
	if status != http.StatusOK {
		t.Fatalf("/api/account: status %d, want 200 (body: %s)", status, body)
	}
	assertNoToken(t, "/api/account", body)
	assertAccount(t, "/api/account", decodeObject(t, "/api/account", body))
}

// assertAccount checks the Account object (shared by REST + SSE `account`).
func assertAccount(t TB, kind string, obj map[string]any) {
	t.Helper()
	requireFields(t, kind, obj, "username", "disk", "used_percent", "over_quota", "days_until_files_deletion")
	requireString(t, kind, "username", obj["username"])
	requireNumber(t, kind, "used_percent", obj["used_percent"])
	requireBool(t, kind, "over_quota", obj["over_quota"])
	requireNumber(t, kind, "days_until_files_deletion", obj["days_until_files_deletion"])
	if disk, ok := obj["disk"].(map[string]any); ok {
		dKind := kind + ".disk"
		requireFields(t, dKind, disk, "size", "used", "avail")
		requireNumber(t, dKind, "size", disk["size"])
		requireNumber(t, dKind, "used", disk["used"])
		requireNumber(t, dKind, "avail", disk["avail"])
	} else {
		t.Errorf("%s: disk must be an object, got %T", kind, obj["disk"])
	}
}

// ProbeSettings asserts GET /api/settings and the token-never-echoed invariant.
func ProbeSettings(t TB, base string) {
	t.Helper()
	status, body := httpGet(t, base+"/api/settings")
	if status != http.StatusOK {
		t.Fatalf("/api/settings: status %d, want 200 (body: %s)", status, body)
	}
	assertNoToken(t, "/api/settings", body)

	obj := decodeObject(t, "/api/settings", body)
	if !requireFields(t, "/api/settings", obj, "settings") {
		return
	}
	list, ok := obj["settings"].([]any)
	if !ok {
		t.Fatalf("/api/settings: \"settings\" must be an array, got %T", obj["settings"])
	}
	sawToken := false
	for i, raw := range list {
		e, ok := raw.(map[string]any)
		if !ok {
			t.Errorf("settings[%d] must be an object, got %T", i, raw)
			continue
		}
		kind := fmt.Sprintf("settings[%d]", i)
		requireFields(t, kind, e, "key", "value", "source", "locked", "live", "restart_required")
		requireString(t, kind, "key", e["key"])
		requireString(t, kind, "source", e["source"])
		requireBool(t, kind, "locked", e["locked"])
		requireBool(t, kind, "live", e["live"])
		requireBool(t, kind, "restart_required", e["restart_required"])

		if key, _ := e["key"].(string); key == "token" {
			sawToken = true
			// token.value MUST be null and is_set MUST be present (bool).
			if e["value"] != nil {
				t.Errorf("settings token.value must be null, got %v (token must never be echoed)", e["value"])
			}
			if _, present := e["is_set"]; !present {
				t.Errorf("settings token entry must carry is_set")
			} else {
				requireBool(t, kind, "is_set", e["is_set"])
			}
		}
	}
	if !sawToken {
		t.Error("/api/settings: expected a 'token' setting entry")
	}
}

// ProbeSSE connects to GET /api/events and asserts (1) a `snapshot` event
// arrives first with the expected sub-payloads, and (2) at least one further
// named event arrives within timeout. No payload may contain the token.
func ProbeSSE(t TB, base string, timeout time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/events", nil)
	if err != nil {
		t.Fatalf("build SSE request: %v", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("connect SSE: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("/api/events: Content-Type %q, want text/event-stream", ct)
	}

	events := make(chan sseEvent, 16)
	go scanSSE(resp.Body, events)

	// First event must be `snapshot` with transfers/account/settings/logs.
	first, ok := waitEvent(ctx, events)
	if !ok {
		t.Fatal("/api/events: no event received before timeout (expected snapshot)")
	}
	assertNoToken(t, "sse:snapshot", []byte(first.data))
	if first.event != "snapshot" {
		t.Errorf("/api/events: first event %q, want snapshot", first.event)
	} else {
		assertSnapshot(t, first.data)
	}

	// At least one incremental named event must follow.
	next, ok := waitEvent(ctx, events)
	if !ok {
		t.Fatal("/api/events: no incremental event after snapshot before timeout")
	}
	assertNoToken(t, "sse:"+next.event, []byte(next.data))
	switch next.event {
	case "transfers", "account", "log", "settings", "snapshot":
		// valid named events per the taxonomy
	default:
		t.Errorf("/api/events: unexpected event name %q", next.event)
	}
}

// assertSnapshot checks the snapshot payload's four sub-objects.
func assertSnapshot(t TB, data string) {
	t.Helper()
	obj := decodeObject(t, "sse:snapshot", []byte(data))
	requireFields(t, "sse:snapshot", obj, "transfers", "account", "settings", "logs")
	if _, ok := obj["transfers"].([]any); !ok {
		t.Errorf("sse:snapshot.transfers must be an array, got %T", obj["transfers"])
	}
	if acc, ok := obj["account"].(map[string]any); ok {
		assertAccount(t, "sse:snapshot.account", acc)
	} else {
		t.Errorf("sse:snapshot.account must be an object, got %T", obj["account"])
	}
	if _, ok := obj["settings"].([]any); !ok {
		t.Errorf("sse:snapshot.settings must be an array, got %T", obj["settings"])
	}
	if _, ok := obj["logs"].([]any); !ok {
		t.Errorf("sse:snapshot.logs must be an array, got %T", obj["logs"])
	}
}

// ProbeContract runs every REST + SSE assertion against base. This is the single
// entry point the integration-verifier calls against the real demo server.
func ProbeContract(t TB, base string) {
	t.Helper()
	ProbeTransfers(t, base)
	ProbeAccount(t, base)
	ProbeSettings(t, base)
	ProbeSSE(t, base, 5*time.Second)
}

// --- helpers ---------------------------------------------------------------

func asFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

// assertNoToken fails if the demo token sentinel appears anywhere in body —
// the security invariant from the contract (token never in any payload).
func assertNoToken(t TB, where string, body []byte) {
	t.Helper()
	if strings.Contains(string(body), DemoTokenSentinel) {
		t.Errorf("%s: response contains the OAuth token sentinel — token leaked into a payload", where)
	}
}

type sseEvent struct {
	event string
	data  string
}

// scanSSE parses a text/event-stream into events. Only `event:` and `data:`
// fields are needed for the contract; multi-line data is joined with newlines
// per the SSE spec. Closes nothing; the caller owns r.
func scanSSE(r io.Reader, out chan<- sseEvent) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	var ev sseEvent
	var dataLines []string
	flush := func() {
		if len(dataLines) == 0 && ev.event == "" {
			return
		}
		ev.data = strings.Join(dataLines, "\n")
		if ev.event == "" {
			ev.event = "message" // SSE default
		}
		out <- ev
		ev = sseEvent{}
		dataLines = nil
	}
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, "event:"):
			ev.event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
}

func waitEvent(ctx context.Context, events <-chan sseEvent) (sseEvent, bool) {
	select {
	case e := <-events:
		return e, true
	case <-ctx.Done():
		return sseEvent{}, false
	}
}
