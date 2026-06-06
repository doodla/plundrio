# plundrio web dashboard — design

**Mental model:** the dashboard is a *second, read-mostly face* on the daemon's already-existing
state — a new `internal/dashboard` package boots its own HTTP listener (separate from the RPC
`:9091`), reads the same `Manager`/`Client` the RPC server already holds, tees the existing zerolog
stream to SSE subscribers, and writes a small JSON overrides file that layers on top of viper; the
RPC path, the put.io OAuth token, and the download engine's invariants are untouched.

## Stack choice

**Svelte 5 + Vite (static SPA, embedded via `go:embed`)** — its built-in `Tween`/`Spring`,
`transition:`, and `animate:flip` give the two-phase progress glide and live-log animations with
zero added animation dependency, and the compiler emits the smallest runtime for a Pi-class browser.
This adopts both research conclusions verbatim; no stated reason to deviate.

---

## Daemon changes

Each change below names the seam it touches and its blast radius (what breaks if it's wrong). The
ordering reflects dependency: the overrides layer and log fan-out are prerequisites the dashboard
package consumes.

### 1. `internal/dashboard` package — listener, embed, REST, SSE hub

**New package `internal/dashboard`** plus **new package `internal/web`** (holds `embed.go` with
`//go:embed all:dist` and a committed `dist/.gitkeep` placeholder). The `all:` prefix is
load-bearing: a plain `//go:embed dist` excludes names beginning with `.` or `_`, so on a bare
checkout / in CI where `dist/` contains only `.gitkeep` the pattern matches **zero** embeddable
files and `go build` fails with "contains no embeddable files". `all:dist` overrides the dotfile
exclusion (verified to build), so the directive compiles whether `dist/` holds just the placeholder
or the real Vite output. The dashboard package owns:

- A `*http.Server` on `cfg.DashboardListen` with `ReadHeaderTimeout` set (this is a *new* listener,
  so the CLAUDE.md slowloris note applies — set the timeout here even though `:9091` deliberately
  omits it; we are not changing `:9091`).
- A static file handler serving the embedded `dist/` with `index.html` SPA fallback for unknown
  paths (deep links). Vite emits hashed assets under `assets/` with plain names, so the served tree
  needs no special handling; keep the default `assetsDir` (a custom one beginning with `_` would be
  awkward to reference, though `all:dist` would still embed it).
- REST handlers (see `contract.md`) reading from `DashboardService` — a narrow interface (mirroring
  the existing `server.DownloadService` idiom) exposing `GetTransfers`, `GetTransferContext`,
  `GetCategory`, `GetAccountInfo`, plus the new `SetWorkerCount`. **The interface takes the same
  `*Manager` and `*api.Client` already constructed in `main.go`** — no second put.io client, no
  duplicate state.
- The SSE hub (below).

**Listener lifecycle in `cmd/plundrio/main.go`:** the seam is the `runCmd.Run` block immediately
after `srv := server.New(...)` (line ~209). When `cfg.DashboardListen != ""`, construct
`dashboard.New(cfg, client, dlManager, logHub)` and start it in its own goroutine exactly like the
RPC server's goroutine, and add its `Stop()` to the shutdown sequence after `srv.Stop()`. When
empty, nothing is constructed — default-off, zero new surface. **Blast radius:** confined to the new
goroutine; a dashboard `ListenAndServe` error must *not* `log.Fatal` (that would kill the daemon and
take down the RPC path) — it logs `Error` and returns. This is the one deliberate divergence from
the RPC server's `log.Fatal` on error: the RPC server is load-bearing, the dashboard is not.

**New config field + flag.** `Config.DashboardListen string \`mapstructure:"dashboard_listen"\``,
flag `--dashboard-listen` (default `""`), env `PLDR_DASHBOARD_LISTEN`. Register in
`registerRunFlags` and add to `flagViperBindings` as `"dashboard-listen": "dashboard_listen"`. The
v0.10.11/12 retention bug is the cautionary tale: the flag name is hyphenated, the viper/mapstructure
key is underscored, and the binding map is the single place they're reconciled — **a missing entry
here silently resolves the listener to empty and the dashboard never starts.** The endpoint prober's
first assertion is that the dashboard is reachable, which catches exactly this drift.

### 2. Log fan-out — `internal/log`

**Goal:** every log event that passes the global level gate reaches both the existing colored stdout
console *and* a structured JSON ring buffer that feeds SSE subscribers, with no change to call sites
and no change to level gating.

**Exact change to `internal/log/log.go`:** `configureLogger` currently builds
`zerolog.New(consoleWriter)`. Change it to build
`zerolog.New(zerolog.MultiLevelWriter(consoleWriter, hubWriter))` where `hubWriter` is a package-level
`io.Writer` that is a no-op until the dashboard registers a real sink. zerolog gates by
`SetGlobalLevel` *before* invoking any writer, so `hubWriter` only ever receives events at or above
the active level — the live `log_level` change (below) thus transparently widens/narrows the SSE log
stream too, for free.

- Add `log.SetHubWriter(w io.Writer)` — called once by `dashboard.New` to install the hub's writer.
  Default is a discard writer so non-dashboard builds and the pre-registration window cost nothing.
- The hub's writer receives **one JSON object per log line** (zerolog with a `MultiLevelWriter`
  feeds each writer the same per-event bytes; the console writer pretty-prints, the hub writer gets
  the raw JSON because zerolog's base encoding is JSON — the `ConsoleWriter` is only a formatter on
  the *console* leg). The hub parses out `level`, `component`, `time`, `message`, and remaining
  fields into the SSE `log` event payload (see contract).

**Blast radius:** `internal/log` is imported by every package. The change is additive (a wrapping
writer + one setter); existing `Debug/Info/Warn/Error/Fatal/SetLevel` signatures are unchanged. The
risk is a slow SSE subscriber back-pressuring the *logger* — **mitigated by the hub never blocking
the writer**: `hubWriter.Write` does a non-blocking fan-out (drop-or-disconnect per subscriber,
below) and always returns `len(p), nil`. A logger that can be blocked by an HTTP client is the bug
to avoid; the writer must be infallible from zerolog's perspective.

### 3. SSE broadcast hub (in `internal/dashboard`)

Known pattern, specified not researched:

- **Per-subscriber buffered channel** (`chan []byte`, cap ~256). On connect, the handler registers a
  subscriber, immediately writes a **snapshot** (current transfers + account, as a `snapshot`
  event), then streams subsequent events.
- **Backpressure = drop-or-disconnect slow client.** When a subscriber's buffer is full, the hub
  drops the oldest / disconnects that subscriber — it never blocks the producer. Log events come
  from the logger's write path (must never block, see above); transfer/account events come from a
  ticker goroutine. A slow browser must degrade only its own stream.
- **Context-driven shutdown.** The hub holds the daemon lifecycle context; `dashboard.Stop()` cancels
  it, closing all subscriber channels and ending their handler goroutines. Each subscriber also
  watches `r.Context().Done()` so a closed browser tab reaps its goroutine.
- **Log ring buffer.** The hub keeps the last N (~500) log events so a fresh connection's snapshot
  includes recent history (the log viewer isn't blank on load). Bounded — a Pi can't hold unbounded
  log history, and the frontend caps its own buffer too.

**Cadence & taxonomy:** fully specified in `contract.md`. Summary: `snapshot` once on connect;
`transfers` on a ~1s ticker (full list, recomputed via the *same* progress logic as `:9091`);
`account` on a slower ~15s ticker; `log` per log line as it happens; `settings` when a PUT mutates
live settings.

### 4. Runtime-overrides config layer

**File:** JSON, mirroring the existing `CategoryStore` precedent (`internal/download/category.go`
already persists JSON state to disk with `0644`). **Location:** flag-controlled
`--overrides-file` / `PLDR_OVERRIDES_FILE`, default `<TargetDir>/.plundrio-overrides.json` (same
directory the category state already lives in, so it inherits the existing writable-volume
assumption). Shape:

```json
{ "log_level": "debug", "workers": 8, "target": "/downloads",
  "folder": "plundrio", "listen": ":9091", "dashboard_listen": ":9092" }
```

Only keys the operator has actually edited are present; absent keys fall through to viper.

**Precedence — the exact seam in `cmd/plundrio/main.go` `loadConfig`:** today the order is
`viper.AutomaticEnv()` → optional config file → `viper.BindPFlag` per flag → `viper.Unmarshal`. The
overrides file must sit **above** flags/env/config-file for the *non-env-pinned* keys, but **env must
still win** (decision 2: env beats the overrides file because the daemon can't rewrite compose). The
clean way that respects viper's resolution without fighting it:

1. After `viper.AutomaticEnv()` and before `Unmarshal`, read the overrides JSON.
2. For each overrides key, apply it via `viper.Set(key, value)` **only if the key is not env-pinned**
   — i.e. only if `os.LookupEnv("PLDR_" + upper(key))` is absent. `viper.Set` is viper's highest
   precedence tier (`Override`), which is exactly the "highest precedence above viper's resolution"
   the decision requires. Skipping env-pinned keys preserves env-wins.
3. `viper.Unmarshal` then decodes the merged result into `Config` as it does now.

This adds a layer, it does **not** reorder the existing flag bindings — the v0.10.11/12 bug was
flags bound under the wrong key; we keep the canonical-key binding map intact and only `viper.Set`
canonical keys, so no new drift surface.

> **Non-negotiable for the backend-builder:** the `os.LookupEnv` env-skip guard in step 2 is
> load-bearing, not an optimization. `viper.Set` writes viper's **Override** tier, which sits
> *above* `AutomaticEnv`. If the overrides file is applied via `viper.Set` for an env-pinned key,
> the override silently beats the env var — directly inverting locked decision 2 (env must win,
> because the daemon can't rewrite compose). Every `viper.Set` of an overrides key MUST be gated by
> `os.LookupEnv("PLDR_"+upper(key))` being absent. Skip the guard and env-wins breaks with no error.
> The precedence-matrix unit test asserts exactly this case (env-pinned + overrides-file-present →
> env value wins).

**Env-pinned detection (`locked`):** a key is locked iff its `PLDR_*` env var is set. Surfaced to the
settings GET as `locked: true` (the daemon can write the overrides file but env still wins, so the
edit would be a silent no-op — the UI must disable the field). The detection is a pure
`os.LookupEnv` per key; no viper introspection needed.

**Live vs restart-required.** `log_level` and `workers` apply live (handlers call `log.SetLevel` /
`Manager.SetWorkerCount` *and* write the file). `target`, `folder`, `listen`, `dashboard_listen`
write the file and return `restart_required: true` — they are read once at boot
(`Config.TargetDir`/`PutioFolder`/`ListenAddr`/`DashboardListen`) and rewiring them live would mean
re-resolving the put.io folder, rebinding listeners, and revalidating the target dir mid-flight,
which is out of v1 scope. **Seam:** a new `internal/config` helper `LoadOverrides`/`WriteOverride`
owns the file I/O; `main.go` calls `LoadOverrides` inside `loadConfig`; the dashboard's settings PUT
handler calls `WriteOverride` + the live-apply hook.

**Blast radius:** the overrides layer changes config resolution for *all* entry points, so a bug
here mis-resolves `TargetDir`/`token`. Contained by: env-pinned keys bypass it entirely (the token
is `PLDR_TOKEN`-pinned in the real deployment, so it's never touched), and the file is optional
(missing = no-op, exactly like `CategoryStore.Load`). Unit tests assert the precedence matrix
(env > overrides > flag > config-file > default) explicitly.

### 5. Dynamic worker-pool resize — `Manager.SetWorkerCount(n)`

**The invariant to respect:** `m.jobs` is never closed (`QueueDownload` selects on `m.jobs <- job`
vs `<-m.stopChan` under `m.mu`; closing `jobs` races that send → panic, issue #2). Workers exit on
`<-m.stopChan`. So resize must *not* close `jobs` and must *not* close `stopChan` (that's the global
shutdown signal).

**Mechanism — `SetWorkerCount` owns the count; workers own only `workerWg`:**

- `Manager` gains a **buffered** `workerQuit chan struct{}` (cap ~256) to signal *individual* workers
  to retire — distinct from `stopChan`, which retires *all* — and an `int` `workerCount` field
  guarded by `m.mu`. **`workerCount` is the reported target count and `SetWorkerCount` is its single
  writer.** Workers never read or write it; they only `workerWg.Done()` on exit. This is the fix for
  the double-booking that would otherwise drive a shrink past its target (8→2 landing at −4 if both
  the setter and the retiring workers decremented).
- `downloadWorker`'s select gains a third case: `case <-m.workerQuit: return`. A worker that reads a
  quit token finishes its current job, loops, sees the token, and exits (calling its deferred
  `workerWg.Done()`). The channel is buffered, so `SetWorkerCount` pushes tokens without blocking.
- `SetWorkerCount(n)` runs entirely under `m.mu`:
  - no-op if `!running` (Stop already closed `stopChan`; workers are draining on it — adding/removing
    is meaningless).
  - **grow** (`n > workerCount`): spawn `n - workerCount` more `downloadWorker` goroutines under
    `workerWg.Add`, exactly as `Start` does; set `workerCount = n`.
  - **shrink** (`n < workerCount`): push `workerCount - n` tokens onto the buffered `workerQuit`
    (non-blocking best-effort — the buffer cap exceeds any realistic worker count); set
    `workerCount = n`.
- Never closes `jobs` or `stopChan`, so issue #2's "send on closed channel" invariant holds. A resize
  racing `Stop()` is safe: both take `m.mu`; whichever runs second observes the consistent
  `running`/`workerCount` state.

The reported `workerCount` updates synchronously; the actual goroutine count **converges
asynchronously** on shrink, because a worker only retires after finishing its in-flight job. A shrink
from 8→2 while 8 downloads are mid-flight retires workers as each completes, not instantly — the
contract's settings semantics document this so the UI doesn't promise instantaneous resize.

**Testing:** `manager_test.go`-style, standard `testing.T`, no testify. Grow: call
`SetWorkerCount(n+k)`, assert `workerWg` delta / a counter reaches `n+k`, and that queued jobs drain.
Shrink: seed long-ish fake jobs, `SetWorkerCount(n-k)`, assert the count converges to `n-k` after
jobs complete and **no panic** (the regression guard for issue #2 — the test must exercise resize
concurrent with `QueueDownload` and `Stop`). Race detector on (`go test -race`, matching CI).

---

## File layout

```
cmd/plundrio/main.go              # +dashboard listener wiring, +LoadOverrides call (edit)
internal/
  config/
    config.go                     # +DashboardListen field (edit)
    overrides.go                  # NEW: LoadOverrides / WriteOverride (JSON file I/O)
    overrides_test.go             # NEW: precedence matrix unit tests
  dashboard/
    dashboard.go                  # NEW: New(), listener lifecycle, Service interface
    rest.go                       # NEW: REST handlers (transfers, account, settings)
    sse.go                        # NEW: hub, subscribers, snapshot, ring buffer
    render.go                     # NEW: TransferContext+putio.Transfer -> contract DTO
    settings.go                   # NEW: GET/PUT settings, locked detection, live-apply
    *_test.go                     # NEW: handler + render + hub unit tests
  transferprog/
    transferprog.go               # NEW: moved calculateProgress/progressInput/progressResult +
                                  #      mapPutioStatusValue + trStatus* consts (from server)
    transferprog_test.go          # NEW: server's progress tests, moved/re-pointed here
  server/
    progress.go                   # EDIT: thin wrapper or removed; logic now in transferprog
    torrent.go                    # EDIT: import transferprog for calculateProgress
  web/
    embed.go                      # NEW: //go:embed all:dist  (var DistFS embed.FS)
    dist/.gitkeep                 # NEW: committed placeholder; all: prefix lets the directive
                                  #      compile on a bare checkout where dist/ holds only .gitkeep
  log/log.go                      # +MultiLevelWriter + SetHubWriter (edit)
  download/
    manager.go                    # +SetWorkerCount, workerQuit, workerCount (edit)
    download.go                   # +<-m.workerQuit case in downloadWorker (edit)
    manager_test.go               # +resize tests (edit)
ui/                               # NEW: Svelte 5 + Vite source tree
  package.json, package-lock.json, vite.config.ts, index.html
  src/                            # components, stores, SSE client, REST client
  (vite build -> ui/dist -> copied to internal/web/dist by Nix postPatch)
```

`render.go` is the single place the contract DTO is built so the SSE `transfers` event and the REST
`GET /api/transfers` produce **byte-identical** shapes — and it reuses the exact two-phase math from
the shared progress package. **Decided:** move the pure `calculateProgress` / `progressInput` /
`progressResult` (plus `mapPutioStatusValue` and the `trStatus*` constants they depend on) out of
`internal/server/progress.go` into a new shared package **`internal/transferprog`**, imported by both
`server` and `dashboard`. There is then exactly one two-phase implementation; the dashboard cannot
drift from `:9091`.

Consequence to handle in the same change: `internal/server` now imports `transferprog` (its
`handleTorrentGet` calls the moved `calculateProgress`), and the existing progress tests in
`internal/server` (`progress_test.go` if present, plus any progress assertions in the server suite)
**must be preserved** — move them into `internal/transferprog` alongside the moved code, or re-point
them at the new import path. The backend-builder verifies parity by keeping these tests green, not by
re-deriving the math.

## Test plan (milestone-level)

- **Endpoint prober (harness):** boots the daemon with `--dashboard-listen` against a fake put.io,
  asserts (1) the listener is reachable at all — the drift guard for the flag-binding bug; (2) every
  REST endpoint returns the contract's status + JSON schema (field presence, types, units); (3) the
  SSE endpoint emits a `snapshot` event on connect then named events; (4) **no response anywhere
  contains the OAuth token** (grep the token value across every payload — the security invariant).
- **Integration e2e (`integration-verifier`):** full seed-mode boot, drive a transfer through both
  phases, assert the SSE `transfers` stream shows put.io-phase progress climbing 0→50 then local-phase
  50→100 and the lifecycle state transitions; assert a live `log_level` PUT widens the SSE log stream;
  assert a `workers` PUT changes the reported count; assert a restart-required PUT returns
  `restart_required:true` and writes the overrides file.
- **Unit (no testify, standard `testing.T`, match existing files):**
  - `config/overrides_test.go`: precedence matrix (env > overrides > flag default; env-pinned key
    reported locked; missing file = no-op; malformed file = error surfaced not swallowed).
  - `download/manager_test.go`: worker grow/shrink convergence + no-panic under concurrent
    `QueueDownload`/`Stop` with `-race`.
  - `dashboard/sse_test.go`: hub fan-out, slow-subscriber drop-doesn't-block-producer, snapshot
    contents, ring-buffer bound.
  - `dashboard/render_test.go`: DTO field/unit correctness for each lifecycle state and the
    no-context (put.io-only) case. Parity with `:9091` is guaranteed structurally (both call
    `transferprog.calculateProgress`), and the moved `transferprog_test.go` covers the math.
  - `dashboard/settings_test.go`: live-apply hooks fire; token never serialized; locked fields
    reject writes.

## Risks / deferred

- **No auth (v1, locked).** The dashboard listener has the same network-isolation security model as
  `:9091` — it must only be bound to a private interface. The design *adds* `ReadHeaderTimeout` on
  the new listener (it's a new bind, the slowloris note applies) but does **not** add auth — that's
  the locked decision, not an oversight. **Biggest risk to flag:** if an operator binds
  `--dashboard-listen` to a LAN/public interface, the settings PUT lets anyone change `target`,
  `workers`, etc. and drive the daemon — strictly worse than `:9091` (which only proxies put.io). The
  contract/design keep the token out of every payload, but exposure is the operator's responsibility,
  same as RPC.
- **Worker shrink is eventual, not instant** (in-flight jobs finish first). Documented in the
  contract's settings semantics so the UI doesn't promise instantaneous resize.
- **Deferred:** historical/persisted metrics, multi-user, transfer *creation* from the dashboard
  (add-torrent stays an *arr/RPC concern in v1), and pause/resume of individual transfers (not in the
  underlying engine).
