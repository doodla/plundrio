package dashboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/doodla/plundrio/internal/log"
)

// clearPLDREnv removes every PLDR_* var so a host's env can't pin keys
// unexpectedly. It must genuinely UNSET them, not set-to-empty: env-pinned
// detection uses os.LookupEnv, which reports a present-but-empty var as set, so
// t.Setenv(key,"") would falsely trip "locked". t.Setenv first registers the
// Cleanup that restores the original value after the test; os.Unsetenv then
// makes it absent for the duration.
func clearPLDREnv(t *testing.T) {
	t.Helper()
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "PLDR_") {
			key := kv[:strings.IndexByte(kv, '=')]
			t.Setenv(key, "")
			_ = os.Unsetenv(key)
		}
	}
}

func doPut(t *testing.T, d *Dashboard, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	d.handleSettings(rec, req)
	return rec
}

func doGet(t *testing.T, d *Dashboard) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	rec := httptest.NewRecorder()
	d.handleSettings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET settings status = %d, want 200", rec.Code)
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	return out
}

func settingEntries(t *testing.T, m map[string]any) map[string]map[string]any {
	t.Helper()
	list, _ := m["settings"].([]any)
	byKey := map[string]map[string]any{}
	for _, raw := range list {
		e, _ := raw.(map[string]any)
		key, _ := e["key"].(string)
		byKey[key] = e
	}
	return byKey
}

// TestSettingsGetTokenNeverSerialized: the token entry reports value:null +
// is_set, and the raw body never contains the token string.
func TestSettingsGetTokenNeverSerialized(t *testing.T) {
	clearPLDREnv(t)
	d := newTestDashboard(newFakeService(), &fakeAccountClient{}, "")
	d.cfg.OAuthToken = "super-secret-token-value"

	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	rec := httptest.NewRecorder()
	d.handleSettings(rec, req)

	if strings.Contains(rec.Body.String(), "super-secret-token-value") {
		t.Fatal("settings response leaked the OAuth token")
	}

	byKey := settingEntries(t, mustJSON(t, rec.Body.Bytes()))
	tok := byKey["token"]
	if tok == nil {
		t.Fatal("missing token setting entry")
	}
	if tok["value"] != nil {
		t.Errorf("token.value = %v, want null", tok["value"])
	}
	if isSet, ok := tok["is_set"].(bool); !ok || !isSet {
		t.Errorf("token.is_set = %v, want true (a token resolved)", tok["is_set"])
	}
}

// TestSettingsLiveApplyHooksFire: a PUT of log_level + workers applies live
// (log.SetLevel + svc.SetWorkerCount) and reports them under "applied".
func TestSettingsLiveApplyHooksFire(t *testing.T) {
	clearPLDREnv(t)
	dir := t.TempDir()
	svc := newFakeService()
	d := newTestDashboard(svc, &fakeAccountClient{}, filepath.Join(dir, "ov.json"))

	defer log.SetLevel(log.LevelInfo) // restore global level after the test
	rec := doPut(t, d, `{"log_level":"debug","workers":8}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}

	if log.GetLevel() != log.LevelDebug {
		t.Errorf("log level = %q, want debug (live-applied)", log.GetLevel())
	}
	if svc.GetWorkerCount() != 8 {
		t.Errorf("worker count = %d, want 8 (live-applied)", svc.GetWorkerCount())
	}

	resp := mustJSON(t, rec.Body.Bytes())
	applied := toStringSlice(resp["applied"])
	if !contains(applied, "log_level") || !contains(applied, "workers") {
		t.Errorf("applied = %v, want both log_level and workers", applied)
	}
}

// TestSettingsRestartRequiredPersistsButDefers: a target PUT validates+persists
// and returns restart_required, without switching live.
func TestSettingsRestartRequiredPersists(t *testing.T) {
	clearPLDREnv(t)
	dir := t.TempDir()
	target := t.TempDir() // an existing directory to pass the os.Stat check
	ovPath := filepath.Join(dir, "ov.json")
	d := newTestDashboard(newFakeService(), &fakeAccountClient{}, ovPath)

	rec := doPut(t, d, `{"target":"`+target+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	resp := mustJSON(t, rec.Body.Bytes())
	if !contains(toStringSlice(resp["persisted"]), "target") {
		t.Errorf("persisted = %v, want target", resp["persisted"])
	}
	if !contains(toStringSlice(resp["restart_required"]), "target") {
		t.Errorf("restart_required = %v, want target", resp["restart_required"])
	}
	if contains(toStringSlice(resp["applied"]), "target") {
		t.Errorf("target must NOT be in applied (restart-required), got %v", resp["applied"])
	}

	// Persisted to the overrides file.
	data, err := os.ReadFile(ovPath)
	if err != nil {
		t.Fatalf("overrides file not written: %v", err)
	}
	if !strings.Contains(string(data), target) {
		t.Errorf("overrides file missing target value: %s", data)
	}
}

// TestSettingsLockedFieldRejected: an env-pinned key's PUT is rejected with 400 +
// key_locked, because the write would be a silent no-op (env wins).
func TestSettingsLockedFieldRejected(t *testing.T) {
	clearPLDREnv(t)
	t.Setenv("PLDR_WORKERS", "16") // pin workers via env
	svc := newFakeService()
	d := newTestDashboard(svc, &fakeAccountClient{}, "")

	rec := doPut(t, d, `{"workers":8}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("locked workers PUT status = %d, want 400", rec.Code)
	}
	resp := mustJSON(t, rec.Body.Bytes())
	errObj, _ := resp["error"].(map[string]any)
	if errObj["code"] != codeKeyLocked {
		t.Errorf("error.code = %v, want %s", errObj["code"], codeKeyLocked)
	}
	// The live hook must NOT have fired.
	if svc.GetWorkerCount() != 4 {
		t.Errorf("worker count = %d, want unchanged 4 (locked write rejected)", svc.GetWorkerCount())
	}
}

// TestSettingsLockedReportedInGet: an env-pinned key reports locked:true and
// source:env in the GET.
func TestSettingsLockedReportedInGet(t *testing.T) {
	clearPLDREnv(t)
	t.Setenv("PLDR_TARGET", "/data")
	d := newTestDashboard(newFakeService(), &fakeAccountClient{}, "")

	byKey := settingEntries(t, doGet(t, d))
	tgt := byKey["target"]
	if locked, _ := tgt["locked"].(bool); !locked {
		t.Errorf("target.locked = %v, want true (PLDR_TARGET set)", tgt["locked"])
	}
	if tgt["source"] != "env" {
		t.Errorf("target.source = %v, want env", tgt["source"])
	}
}

// TestSettingsValidationRejectsBadValues: each validator rejects with 400 +
// validation_failed and the offending field, with no partial apply.
func TestSettingsValidationRejectsBadValues(t *testing.T) {
	clearPLDREnv(t)
	cases := []struct {
		name, body, field string
	}{
		{"bad log_level", `{"log_level":"loud"}`, "log_level"},
		{"workers < 1", `{"workers":0}`, "workers"},
		{"empty folder", `{"folder":""}`, "folder"},
		{"bad listen", `{"listen":"not-a-host-port"}`, "listen"},
		{"empty token", `{"token":""}`, "token"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc := newFakeService()
			d := newTestDashboard(svc, &fakeAccountClient{}, "")
			rec := doPut(t, d, c.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body: %s)", rec.Code, rec.Body.String())
			}
			resp := mustJSON(t, rec.Body.Bytes())
			errObj, _ := resp["error"].(map[string]any)
			if errObj["code"] != codeValidationFailed {
				t.Errorf("error.code = %v, want validation_failed", errObj["code"])
			}
			if errObj["field"] != c.field {
				t.Errorf("error.field = %v, want %s", errObj["field"], c.field)
			}
		})
	}
}

// TestSettingsPutTokenNotEchoed: a token PUT persists + reports is_set but never
// echoes the value, including in the re-resolved settings list in the response.
func TestSettingsPutTokenNotEchoed(t *testing.T) {
	clearPLDREnv(t)
	dir := t.TempDir()
	d := newTestDashboard(newFakeService(), &fakeAccountClient{}, filepath.Join(dir, "ov.json"))
	d.cfg.OAuthToken = "" // token unset → not env-pinned → editable

	rec := doPut(t, d, `{"token":"brand-new-token-xyz"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("token PUT status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "brand-new-token-xyz") {
		t.Fatal("PUT response echoed the new token value")
	}
	resp := mustJSON(t, rec.Body.Bytes())
	if !contains(toStringSlice(resp["persisted"]), "token") {
		t.Errorf("persisted = %v, want token", resp["persisted"])
	}
}

// TestSettingsUnknownFieldRejected: a body with an unrecognized field is
// rejected (DisallowUnknownFields), guarding against typos silently no-op'ing.
func TestSettingsUnknownFieldRejected(t *testing.T) {
	clearPLDREnv(t)
	d := newTestDashboard(newFakeService(), &fakeAccountClient{}, "")
	rec := doPut(t, d, `{"workerz":8}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("unknown field PUT status = %d, want 400", rec.Code)
	}
}

// --- small helpers ---------------------------------------------------------

func mustJSON(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("decode json: %v (body: %s)", err, b)
	}
	return m
}

func toStringSlice(v any) []string {
	list, _ := v.([]any)
	out := make([]string, 0, len(list))
	for _, e := range list {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
