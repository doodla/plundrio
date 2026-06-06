# plundrio dashboard — API & SSE contract (frozen)

This is the source of truth both backend and frontend build against. Every render field cites its
backing source as `file:symbol`. Units are explicit; drift here is a downstream bug.

**Conventions (apply to every payload):**
- All sizes/bytes are **int64 bytes** unless the field name ends in a unit.
- All speeds are **bytes/sec** (`rate_*`), serialized as JSON numbers. Backing Go types differ:
  `putio.Transfer.DownloadSpeed` is `int`, `TransferContext.localSpeed` is `float64`
  (`internal/download/types.go` — "bytes/sec") rounded to an integer on the wire.
- All timestamps are **RFC3339 strings** (zerolog already uses `time.RFC3339`,
  `internal/log/log.go`). ETAs are **seconds remaining (int64)**, sign-positive; `0`/absent = unknown.
- **Percent**: two distinct conventions, named to disambiguate.
  - `percent_done` is **0.0–1.0 float** (the combined two-phase value, identical to
    `progressResult.PercentDone`, `internal/transferprog` — "0.0–1.0"; the two-phase logic is moved
    there from `internal/server/progress.go` so `:9091` and the dashboard share one implementation).
  - `putio_percent_done` is **0–100 int** (raw put.io, `putio.Transfer.PercentDone` — "0–100 int").
- The **OAuth token is never present in any response** (locked decision). Settings reports it as a
  boolean set/unset only.
- `Content-Type: application/json` for REST; `text/event-stream` for SSE.

---

## REST endpoints

Base path `/api`. Served on the `--dashboard-listen` listener only.

### `GET /api/transfers`

List of all tracked transfers for the configured folder, with two-phase render fields.

**Request:** none.

**Response 200:**
```json
{ "transfers": [ <Transfer>, ... ] }
```

The `<Transfer>` object is defined once in [Two-phase transfer model](#two-phase-transfer-model)
below; it is byte-identical to each element of the SSE `transfers` event and `snapshot.transfers`.

### `GET /api/account`

Put.io account + quota. Sourced from `Client.GetAccountInfo`
(`internal/api/client.go:GetAccountInfo` → `putio.AccountInfo`).

**Response 200:**
```json
{
  "username": "string",            // putio.AccountInfo.Username
  "disk": {
    "size":  9223372036854775807,  // int64 bytes — putio.AccountInfo.Disk.Size  (total)
    "used":  0,                     // int64 bytes — putio.AccountInfo.Disk.Used
    "avail": 0                      // int64 bytes — putio.AccountInfo.Disk.Avail
  },
  "used_percent": 0.0,             // float 0.0–100.0, = Used/Size*100 (server/utils.go:checkDiskQuota)
  "over_quota": false,             // bool, used_percent >= 95 (matches server/utils.go threshold)
  "days_until_files_deletion": 0   // int — putio.AccountInfo.DaysUntilFilesDeletion (0 = n/a)
}
```
Units: `size`/`used`/`avail` are **bytes** (the put.io fields are int64 bytes; `server/server.go`
divides by `1024/1024` only for its log line — do **not** carry that division into the API). The
account's other put.io fields (mail, plan dates, user_id) are intentionally omitted; add later if a
mockup needs them.

### `GET /api/settings`

Resolved settings with source + lock state (locked decision 2).

**Response 200:**
```json
{
  "settings": [
    { "key": "log_level",        "value": "info",       "source": "default", "locked": false, "live": true,  "restart_required": false },
    { "key": "workers",          "value": 4,            "source": "file",    "locked": false, "live": true,  "restart_required": false },
    { "key": "target",           "value": "/downloads", "source": "env",     "locked": true,  "live": false, "restart_required": true  },
    { "key": "folder",           "value": "plundrio",   "source": "flag",    "locked": false, "live": false, "restart_required": true  },
    { "key": "listen",           "value": ":9091",      "source": "default", "locked": false, "live": false, "restart_required": true  },
    { "key": "dashboard_listen", "value": ":9092",      "source": "file",    "locked": false, "live": false, "restart_required": true  },
    { "key": "token",            "value": null,         "source": "env",     "locked": true,  "live": false, "restart_required": true, "is_set": true }
  ]
}
```

Field semantics:
- `key` — canonical viper/mapstructure key (`config.Config` mapstructure tags + the new
  `dashboard_listen`).
- `value` — resolved value (`config.Config` field post-`Unmarshal`). **`token.value` is always
  `null`**; `token.is_set` (bool) reports whether a token resolved (non-empty `OAuthToken`). No other
  key has `is_set`.
- `source` — one of `env` | `file` | `flag` | `default`, where `file` = the runtime-overrides file.
  Computed: env-pinned → `env`; else present in overrides file → `file`; else flag changed from
  default → `flag`; else `default`.
- `locked` — `true` iff the key's `PLDR_*` env var is set (`os.LookupEnv`). A locked key's PUT is
  rejected (env beats the overrides file; the daemon can't rewrite compose).
- `live` — `true` for `log_level`, `workers` (apply immediately on PUT); `false` for the rest.
- `restart_required` — `true` for non-live keys; informs the UI to badge "restart required".

### `PUT /api/settings`

Edit one or more settings. Persists to the overrides file (`config.WriteOverride`); live keys also
apply immediately.

**Request:**
```json
{ "log_level": "debug", "workers": 8, "target": "/data", "token": "<new-token>" }
```
Only the keys present are changed. Accepted keys: `log_level`, `workers`, `target`, `folder`,
`listen`, `dashboard_listen`, `token`.

**Validation (reject the whole request with 400 + error shape on any failure — no partial apply):**
- `log_level` ∈ {`debug`,`info`,`warn`,`error`,`fatal`,`none`} (`internal/log` LogLevel constants).
- `workers` integer ≥ 1.
- `target` must be an existing directory (mirror `main.go`'s `os.Stat` + `IsDir` check) — but since
  it's restart-required, validate path syntax/existence and persist; do not switch live.
- `listen` / `dashboard_listen` must parse as a host:port (`net.SplitHostPort` tolerating empty host).
- `folder` non-empty.
- `token` non-empty; **never echoed back** — response reports `token.is_set: true`.
- Any **locked** key (its `PLDR_*` env set) → 400, because the write would be a silent no-op.

**Response 200:**
```json
{
  "applied":           ["log_level", "workers"],   // live keys that took effect now
  "persisted":         ["target", "token"],        // written to overrides file
  "restart_required":  ["target"],                 // persisted but inert until restart
  "settings":          [ <SettingEntry>, ... ]     // full re-resolved settings list (post-write)
}
```
`workers` apply is **eventual on shrink** (in-flight downloads finish before workers retire — see
design §5); `applied` means the new target count is set and reported, not that goroutine count has
already converged.

---

## Two-phase transfer model

The `<Transfer>` render object. **Progress reuses the exact two-phase computation in
`internal/transferprog`** (`calculateProgress` / `calculateProgressWithContext`, moved there from
`internal/server/progress.go` and imported by both `server` and `dashboard`): put.io fetch maps to
0–50%, local download maps to 50–100%, combined into `percent_done` (0.0–1.0). The contract exposes
both the combined value *and* the two phases separately so the UI can render two bars.

Backing sources: `t` = `putio.Transfer` (`GetTransfers`, `internal/api/client.go`); `ctx` =
`*download.TransferContext` (`internal/download/types.go`, looked up by `GetTransferContext`).

```json
{
  "id":               12345,            // int64 — putio.Transfer.ID
  "hash":             "abcdef...",      // string — putio.Transfer.Hash (the *arr-facing torrent id)
  "name":             "Show.S01E01...", // string — putio.Transfer.Name (ctx.Name mirrors it)
  "category":         "tv",             // string — Manager.GetCategory(t.Hash); "" if none

  "size":             1073741824,       // int bytes — putio.Transfer.Size (Go `int`; whole torrent)
  "putio_status":     "DOWNLOADING",    // string — putio.Transfer.Status (raw put.io enum)
  "lifecycle_state":  "Downloading",    // string — ctx.GetState().String() OR "None" if no ctx
                                        //   one of: None|Initial|Downloading|Completed|Failed|
                                        //           Cancelled|Processed (types.go:String())

  "percent_done":     0.42,             // float 0.0–1.0 — progressResult.PercentDone (COMBINED)
  "left_until_done":  620000000,        // int64 bytes — progressResult.LeftUntilDone

  "putio_phase": {                      // 0–50% leg
    "percent_done": 84,                 // int 0–100 — putio.Transfer.PercentDone (raw)
    "downloaded":   900000000,          // int64 bytes — putio.Transfer.Downloaded
    "rate_download": 5242880,           // bytes/sec — putio.Transfer.DownloadSpeed (Go `int`)
    "eta_seconds":  120                 // int64 sec — putio.Transfer.EstimatedTime
  },

  "local_phase": {                      // 50–100% leg; null when ctx absent (no local tracking yet)
    "downloaded":     450000000,        // int64 bytes — ctx.GetProgress() downloadedSize
    "total":          1073741824,       // int64 bytes — ctx.GetProgress() totalSize
    "completed_files": 3,               // int32 — ctx.GetProgress() completedFiles
    "failed_files":    0,               // int32 — ctx.GetProgress() failedFiles
    "total_files":     5,               // int32 — ctx.TotalFiles (write-once)
    "rate_download":   8388608,         // int64 bytes/sec — ctx.GetLocalProgress() speed (rounded)
    "eta_seconds":     60               // int64 sec — derived from ctx.GetLocalProgress() eta:
                                        //   max(0, round(time.Until(localETA).Seconds()));
                                        //   0 when localETA is zero (matches torrent.go logic)
  },

  "error":            false,            // bool — see error precedence below
  "error_string":     "",               // string — see error precedence below
  "permanent":        false,            // bool — ctx.IsPermanent() (cascade gave up); false w/o ctx

  "processed_at":     null,             // RFC3339 string or null — ctx.GetProcessedAt();
                                        //   null when zero (never reached Processed)
  "created_at":       "2026-06-05T12:00:00Z", // RFC3339 or null — putio.Transfer.CreatedAt
  "finished_at":      null              // RFC3339 or null — putio.Transfer.FinishedAt
}
```

**Combined progress rules (do not reinvent — call `internal/transferprog`, the moved-from-`server/progress.go` logic):**
- With a tracked ctx that has `TotalFiles > 0`: `percent_done = putio/200 + local*0.5`
  (`calculateProgressWithContext`). `Processed` state pins `percent_done=1.0`, `left_until_done=0`.
- Without a ctx: put.io-only, `percent_done = putio_percent_done/200`; `COMPLETED`/`SEEDING` →
  `1.0` (`calculateProgress`).

**Error precedence (copy `torrent.go:handleTorrentGet`):** plundrio's permanent-failure string wins
over put.io's `ErrorMessage`. `error_string` = `progressResult.Error` if the cascade marked it
permanent (`ctx.IsPermanent()`), else `putio.Transfer.ErrorMessage`. `error` = `error_string != ""`.
A *transient* Failed (still retrying, `permanent:false`) reports `error:false` and stays mid-progress
— same as the RPC path, so the dashboard and *arr never disagree.

**`local_phase: null`** when there is no `TransferContext` yet (transfer is still in put.io's fetch
phase and the download manager hasn't started local tracking). The UI renders only the put.io bar in
that case.

---

## SSE event taxonomy

Single endpoint: `GET /api/events` (`text/event-stream`). The browser `EventSource` auto-reconnects;
the server need not implement `Last-Event-ID` replay in v1 — **reconnection re-sends a fresh
`snapshot`** (snapshot-on-connect, not event replay). Events use SSE named events (`event:` field).

| `event:` | Payload (`data:`) | Cadence | Notes |
|---|---|---|---|
| `snapshot` | `{ "transfers": [<Transfer>...], "account": <Account>, "settings": [<SettingEntry>...], "logs": [<LogEvent>...] }` | **once, immediately on connect** | Full initial state so no view is blank on load. `logs` = ring buffer's last ~500 events. |
| `transfers` | `{ "transfers": [<Transfer>...] }` | ~1s ticker | Full list each tick (small N; simplest correct shape). Same `<Transfer>` as REST. |
| `account` | `<Account>` (same shape as `GET /api/account`) | ~15s ticker | Matches existing 15-min RPC quota cadence loosely; 15s is fine for a dashboard. |
| `log` | `<LogEvent>` (below) | per log line, as emitted | Fed by the `internal/log` MultiLevelWriter hub leg; only lines passing the global level gate arrive (so a live `log_level` change widens/narrows this stream for free). |
| `settings` | `{ "settings": [<SettingEntry>...] }` | on any successful `PUT /api/settings` | Pushes the re-resolved settings to all clients so multiple tabs stay consistent. |

**`<LogEvent>`** (parsed from the zerolog JSON line by the hub):
```json
{
  "time":      "2026-06-05T12:00:00Z", // RFC3339 — zerolog timestamp (TimeFieldFormat=RFC3339)
  "level":     "info",                 // string — zerolog level ("debug".."error"/"fatal")
  "component": "transfers",            // string — the .Str("component", ...) every log.* call adds
  "message":   "Found ready transfer", // string — zerolog "message" field
  "fields":    { "id": 123, "name": "..." } // object — remaining structured fields, verbatim
}
```
The hub keeps a **bounded ring buffer** (~500) for the snapshot; the frontend also caps its own
in-memory log list (Pi-class memory). No log event ever carries the OAuth token — no log call site
logs it (`get-token` prints it to stdout only, which is not on the SSE path).

**Backpressure (design §3):** each subscriber has a buffered channel; a subscriber that can't keep up
is dropped/disconnected rather than blocking the producer. The log producer is the *logger's write
path* and must never block, so the hub's writer fan-out is non-blocking and the logger always sees a
successful write.

---

## Error response shape

REST errors use a single shape across all endpoints (distinct from the Transmission RPC
`{result, message}` shape in `internal/server/response.go` — the dashboard is its own API):

```json
{ "error": { "code": "validation_failed", "message": "workers must be >= 1", "field": "workers" } }
```
- HTTP status: `400` (validation / bad request / locked-key write), `404` (unknown route),
  `405` (wrong method), `500` (put.io call failed, e.g. `GetAccountInfo` error), `503` (dashboard
  disabled — but if disabled the listener simply isn't bound, so this is mostly N/A).
- `code` — stable machine string (`validation_failed`, `key_locked`, `not_found`,
  `method_not_allowed`, `upstream_error`).
- `field` — optional; present for per-field validation errors.
- The body **never** includes the token or any secret, even in `message`.
