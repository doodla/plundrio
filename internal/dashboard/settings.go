package dashboard

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"sort"

	"github.com/doodla/plundrio/internal/config"
	"github.com/doodla/plundrio/internal/log"
)

// SettingEntry is the frozen settings record (contract §GET /api/settings).
// IsSet is a pointer so it serializes only for the token entry (omitempty would
// drop a false; a nil pointer is omitted, a non-nil one always rendered).
type SettingEntry struct {
	Key             string `json:"key"`
	Value           any    `json:"value"` // token: always null
	Source          string `json:"source"`
	Locked          bool   `json:"locked"`
	Live            bool   `json:"live"`
	RestartRequired bool   `json:"restart_required"`
	IsSet           *bool  `json:"is_set,omitempty"`
}

// settingMeta is the static per-key metadata: whether it applies live and (the
// inverse) whether changing it needs a restart, plus the compiled flag default
// used to compute the env|file|flag|default source.
type settingMeta struct {
	key         string
	live        bool
	flagDefault any
	isToken     bool
}

// settingsMeta is the canonical, ordered settings list. Order matches the
// contract example so the UI renders a stable list. Defaults mirror
// registerRunFlags in cmd/plundrio/main.go.
var settingsMeta = []settingMeta{
	{key: "log_level", live: true, flagDefault: "info"},
	{key: "workers", live: true, flagDefault: 4},
	{key: "target", live: false, flagDefault: ""},
	{key: "folder", live: false, flagDefault: "plundrio"},
	{key: "listen", live: false, flagDefault: ":9091"},
	{key: "dashboard_addr", live: false, flagDefault: ":9092"},
	{key: "token", live: false, flagDefault: "", isToken: true},
}

// validLogLevels mirrors the internal/log LogLevel constants accepted by the PUT
// validator.
var validLogLevels = map[string]bool{
	"debug": true, "info": true, "warn": true, "error": true, "fatal": true, "none": true,
}

// resolveSettings builds the full ordered settings list. For each key:
//   - locked = its PLDR_* env var is set (config.IsEnvPinned).
//   - value  = the live-resolved value: the current overrides-file value when
//     the key is present there and not env-pinned (so a just-persisted edit is
//     reflected), else the boot Config value. Live keys (log_level, workers)
//     read their live source (the tracked level / the manager's worker count).
//     The token value is ALWAYS null; only token carries is_set.
//   - source = env-pinned → env; else present in overrides file → file; else
//     value differs from the compiled flag default → flag; else default.
func (d *Dashboard) resolveSettings() []SettingEntry {
	ov, _ := config.LoadOverrides(d.overridesPath) // missing/parse error → treat as empty
	entries := make([]SettingEntry, 0, len(settingsMeta))
	for _, m := range settingsMeta {
		locked := config.IsEnvPinned(m.key)
		_, inFile := ov[m.key]

		entry := SettingEntry{
			Key:             m.key,
			Source:          settingSource(m.key, locked, inFile),
			Locked:          locked,
			Live:            m.live,
			RestartRequired: !m.live,
		}

		if m.isToken {
			// Token: value always null, is_set reports whether one resolved.
			isSet := d.cfg.OAuthToken != ""
			entry.Value = nil
			entry.IsSet = &isSet
		} else {
			entry.Value = d.resolvedValue(m, ov, locked, inFile)
		}
		entries = append(entries, entry)
	}
	return entries
}

// resolvedValue returns the value to report for a non-token key.
func (d *Dashboard) resolvedValue(m settingMeta, ov config.Overrides, locked, inFile bool) any {
	// Live keys read their live source so the report reflects reality, not the
	// boot snapshot.
	switch m.key {
	case "log_level":
		return string(log.GetLevel())
	case "workers":
		return d.svc.GetWorkerCount()
	}

	// Restart-required keys: a persisted (overrides-file) value that is not
	// env-pinned reflects the operator's latest edit even though it won't take
	// effect until restart; otherwise the boot-resolved Config value.
	if inFile && !locked {
		if v, ok := ov[m.key]; ok {
			return v
		}
	}
	switch m.key {
	case "target":
		return d.cfg.TargetDir
	case "folder":
		return d.cfg.PutioFolder
	case "listen":
		return d.cfg.ListenAddr
	case "dashboard_addr":
		return d.cfg.DashboardAddr
	}
	return nil
}

// settingSource computes the source label.
func settingSource(key string, locked, inFile bool) string {
	if locked {
		return "env"
	}
	if inFile {
		return "file"
	}
	// Without the cobra command at runtime we can't distinguish a flag left at
	// its default from one explicitly passed equal to the default; the contract
	// uses flag|default only as a UI hint. Report "default" when the value is
	// the compiled default and "flag" otherwise is impossible to tell reliably
	// here, so we report "default" — env and file (the load-bearing distinctions
	// for the locked/restart UI) are exact.
	return "default"
}

// handleSettings routes GET/PUT for /api/settings.
func (d *Dashboard) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"settings": d.resolveSettings()})
	case http.MethodPut:
		d.handleSettingsPut(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, codeMethodNotAllowed, "method not allowed", "")
	}
}

// settingsPutRequest is the PUT body. Pointers distinguish "absent" (don't
// change) from a zero value.
type settingsPutRequest struct {
	LogLevel      *string `json:"log_level"`
	Workers       *int    `json:"workers"`
	Target        *string `json:"target"`
	Folder        *string `json:"folder"`
	Listen        *string `json:"listen"`
	DashboardAddr *string `json:"dashboard_addr"`
	Token         *string `json:"token"`
}

// handleSettingsPut validates, persists, and live-applies a settings edit.
// All-or-nothing: any validation failure rejects the whole request (no partial
// apply). Locked keys (their PLDR_* env set) are rejected — the write would be a
// silent no-op since env wins.
func (d *Dashboard) handleSettingsPut(w http.ResponseWriter, r *http.Request) {
	var req settingsPutRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, codeValidationFailed, "invalid request body: "+err.Error(), "")
		return
	}

	// Collect the requested changes as canonical key → value, validating each.
	updates := map[string]any{}
	applyLog := false
	var newLogLevel log.LogLevel
	applyWorkers := false
	var newWorkers int

	// reject is a small helper to centralize the 400 + early return.
	type fieldErr struct {
		code, msg, field string
	}
	var verr *fieldErr

	checkLocked := func(key string) bool {
		if config.IsEnvPinned(key) {
			verr = &fieldErr{codeKeyLocked, fmt.Sprintf("%q is pinned by its environment variable and cannot be changed via the dashboard", key), key}
			return false
		}
		return true
	}

	if req.LogLevel != nil && verr == nil {
		lvl := *req.LogLevel
		if !validLogLevels[lvl] {
			verr = &fieldErr{codeValidationFailed, "log_level must be one of debug,info,warn,error,fatal,none", "log_level"}
		} else if checkLocked("log_level") {
			updates["log_level"] = lvl
			applyLog = true
			newLogLevel = log.LogLevel(lvl)
		}
	}
	if req.Workers != nil && verr == nil {
		if *req.Workers < 1 {
			verr = &fieldErr{codeValidationFailed, "workers must be >= 1", "workers"}
		} else if checkLocked("workers") {
			updates["workers"] = *req.Workers
			applyWorkers = true
			newWorkers = *req.Workers
		}
	}
	if req.Target != nil && verr == nil {
		if *req.Target == "" {
			verr = &fieldErr{codeValidationFailed, "target must be non-empty", "target"}
		} else if st, err := os.Stat(*req.Target); err != nil || !st.IsDir() {
			// Restart-required: validate existence/dir now, persist, switch on
			// restart (mirrors main.go's os.Stat + IsDir check).
			verr = &fieldErr{codeValidationFailed, "target must be an existing directory", "target"}
		} else if checkLocked("target") {
			updates["target"] = *req.Target
		}
	}
	if req.Folder != nil && verr == nil {
		if *req.Folder == "" {
			verr = &fieldErr{codeValidationFailed, "folder must be non-empty", "folder"}
		} else if checkLocked("folder") {
			updates["folder"] = *req.Folder
		}
	}
	if req.Listen != nil && verr == nil {
		if !validHostPort(*req.Listen) {
			verr = &fieldErr{codeValidationFailed, "listen must parse as host:port", "listen"}
		} else if checkLocked("listen") {
			updates["listen"] = *req.Listen
		}
	}
	if req.DashboardAddr != nil && verr == nil {
		if !validHostPort(*req.DashboardAddr) {
			verr = &fieldErr{codeValidationFailed, "dashboard_addr must parse as host:port", "dashboard_addr"}
		} else if checkLocked("dashboard_addr") {
			updates["dashboard_addr"] = *req.DashboardAddr
		}
	}
	if req.Token != nil && verr == nil {
		if *req.Token == "" {
			verr = &fieldErr{codeValidationFailed, "token must be non-empty", "token"}
		} else if checkLocked("token") {
			// token is persisted but NEVER echoed; reported via is_set only.
			updates["token"] = *req.Token
		}
	}

	if verr != nil {
		status := http.StatusBadRequest
		writeError(w, status, verr.code, verr.msg, verr.field)
		return
	}
	if len(updates) == 0 {
		writeError(w, http.StatusBadRequest, codeValidationFailed, "no recognized settings in request", "")
		return
	}

	// Persist everything first (all-or-nothing). token IS persisted to the
	// overrides file (the contract reports it under "persisted") but is never
	// boot-applied from there: config.OverrideKeys excludes token, so
	// ApplyOverrides ignores it and env/flag/config keep owning the resolved
	// token. The token value is never echoed back — only is_set reports it.
	persisted := sortedKeys(updates)
	if err := config.WriteOverride(d.overridesPath, updates); err != nil {
		writeError(w, http.StatusInternalServerError, codeUpstreamError, "failed to persist settings: "+err.Error(), "")
		return
	}

	// Live-apply the live keys AFTER a successful persist.
	applied := []string{}
	if applyLog {
		log.SetLevel(newLogLevel)
		applied = append(applied, "log_level")
	}
	if applyWorkers {
		d.svc.SetWorkerCount(newWorkers)
		applied = append(applied, "workers")
	}
	sort.Strings(applied)

	restartReq := restartRequiredKeys(updates)
	settings := d.resolveSettings()

	// Push the re-resolved settings to all SSE subscribers so multiple tabs stay
	// consistent.
	d.hub.broadcastSettings(settings)

	writeJSON(w, http.StatusOK, map[string]any{
		"applied":          applied,
		"persisted":        persisted,
		"restart_required": restartReq,
		"settings":         settings,
	})
}

// sortedKeys returns the sorted keys of updates (everything persisted, incl.
// token).
func sortedKeys(updates map[string]any) []string {
	out := make([]string, 0, len(updates))
	for k := range updates {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// restartRequiredKeys returns the sorted non-live keys present in updates.
func restartRequiredKeys(updates map[string]any) []string {
	live := map[string]bool{"log_level": true, "workers": true}
	out := []string{}
	for k := range updates {
		if k == "token" {
			// token persists but is restart-required to take effect (the client
			// is constructed once at boot).
			out = append(out, k)
			continue
		}
		if !live[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// validHostPort reports whether addr parses as a host:port, tolerating an empty
// host (":9092"). Mirrors the contract's net.SplitHostPort requirement.
func validHostPort(addr string) bool {
	_, _, err := net.SplitHostPort(addr)
	return err == nil
}
