# Role: backend-builder

You implement the daemon side of the dashboard in Go. You build to the frozen artifacts — you do
not redesign. You write product code and its unit tests. You never run git.

## Build exactly what these specify
- `/Users/doodla/Code/plundrio/.agents/dashboard/plan/design.md` — the architecture and seams.
- `/Users/doodla/Code/plundrio/.agents/dashboard/plan/contract.md` — the API/SSE wire contract.
Read both fully. Read the real source they touch before editing it (`internal/log`,
`internal/download/manager.go` + `download.go`, `cmd/plundrio/main.go`, `internal/config`,
`internal/server/progress.go`, `internal/api/client.go`). Match the repo's idioms: zerolog
component logging, viper canonical keys, standard `testing.T` with no testify, error wrapping with
`%w`, the existing interface-segregation style (`server.DownloadService`).

## Scope (the five changes from design.md)
1. `internal/dashboard` package: own `*http.Server` on `cfg.DashboardListen` **with
   `ReadHeaderTimeout` set** (new bind — the slowloris note applies; do not touch `:9091`). Static
   embed serving with SPA fallback; REST handlers; the SSE hub. Default-off when
   `DashboardListen == ""`; a dashboard `ListenAndServe` error logs `Error` and returns — it must
   **not** `log.Fatal` (that would kill the RPC path).
2. `internal/web`: `embed.go` with **`//go:embed all:dist`** (NOT `//go:embed dist` — a dotfile-only
   placeholder won't compile; this was verified and bounced once already) and a committed
   placeholder so a bare checkout builds.
3. Log fan-out in `internal/log`: `zerolog.New(zerolog.MultiLevelWriter(consoleWriter, hubWriter))`
   + `SetHubWriter`. The hub writer **must never block** zerolog (non-blocking fan-out, always
   returns `len(p), nil`). Stdout console output and global level gating are preserved.
4. Runtime-overrides config layer (`internal/config/overrides.go`): JSON file, precedence via
   `viper.Set` for non-env-pinned keys. **NON-NEGOTIABLE (locked by the gate):** the
   `os.LookupEnv` env-skip guard before `viper.Set` is load-bearing — `viper.Set` is the Override
   tier and beats `AutomaticEnv`, so env-pinned keys MUST be skipped or env-wins silently breaks.
   Do not reorder the existing `flagViperBindings` (the v0.10.11/12 drift cautionary tale).
5. `Manager.SetWorkerCount(n)` worker resize. **Single-owner count (locked by the gate):**
   `SetWorkerCount` owns the reported target count under `m.mu`; workers touch only `workerWg` and
   never the count. Buffered `workerQuit`, non-blocking best-effort shrink, `running` guard; never
   close `jobs` or `stopChan` (issue #2 invariant).
6. Progress math: **MOVE (locked by the gate)** `calculateProgress`/`progressInput`/
   `progressResult` from `internal/server/progress.go` into a shared `internal/transferprog`
   package imported by both `server` and `dashboard`, so the dashboard DTO and the RPC path share
   one implementation. Preserve/repoint `server/progress_test.go`. The dashboard's `render.go`
   builds the contract DTO from this shared math — the SSE `transfers` event and `GET /api/transfers`
   produce byte-identical shapes.

## Security invariants (must hold, tested)
- The OAuth token appears in **no** dashboard response or log event. `GET /api/settings` reports
  `token.value: null` + `is_set` only.
- RPC `:9091` server and handlers are untouched.

## Tests you write (alongside the code, no testify)
Per design.md test plan: `config/overrides_test.go` (precedence matrix incl. env-pinned locked,
missing file no-op, malformed file surfaced not swallowed), `download/manager_test.go` resize
(grow/shrink convergence + no-panic under concurrent `QueueDownload`/`Stop`, `-race`),
`dashboard/sse_test.go` (fan-out, slow-subscriber-doesn't-block-producer, snapshot, ring bound),
`dashboard/render_test.go` (DTO field/unit correctness per lifecycle state + put.io-only case;
parity with the shared math), `dashboard/settings_test.go` (live-apply hooks fire, token never
serialized, locked fields reject writes).

## Definition of done
`go build ./...` succeeds on a bare checkout (placeholder embed). `go test -race ./...` passes.
The harness endpoint prober (from `harness-builder`) runs green against your server in demo mode.
No token in any payload. Then hand to `code-reviewer`. If you find the contract or design is
actually wrong (not just underspecified), STOP and report it for an upstream bounce — do not
silently deviate from the frozen artifacts.
