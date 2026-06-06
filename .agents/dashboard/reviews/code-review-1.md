# Code review 1 — dashboard backend (M4)

VERDICT: APPROVE

The backend honors the frozen contract, the design's locked rulings, and the security model. Build,
vet, and `go test -race` are green on a clean checkout; the bare-checkout `all:dist` embed compiles
with only `.gitkeep`; the contract prober passes against the real demo-mode server and is proven
adversarial by its own self-test. The two highest-risk areas — token-never-serialized and the
worker-shrink determinism — both hold for the right reasons (analysis below). No must-fix findings.

## Evidence (ran at /Users/doodla/Code/plundrio)

- `go build ./...` → exit 0.
- Bare-checkout build (`dist/` wiped to only `.gitkeep`, full `go build ./...`) → SUCCESS. `all:dist`
  compiles with the placeholder; the directive is present at `internal/web/embed.go:20`.
- `go vet ./...` → exit 0.
- `go test -race -count=1 ./...` → all packages `ok` (cmd, config, contractprobe, dashboard, demo,
  download, server, transferprog, logassert). No races.
- `TestContractAgainstDemoServer` (`internal/dashboard/demo_contract_test.go`) → PASS. Boots the real
  wiring (demo client → `download.Manager` → `Dashboard`) on an ephemeral port, seeds
  `PLDR_TOKEN=DemoTokenSentinel` as an env-pinned token so the daemon has a real secret to leak, runs
  `contractprobe.ProbeContract`.
- Prober self-test (`internal/contractprobe/probe_test.go`) → all 7 PASS, including
  `TestProberDetectsTokenLeak`, `…WrongType`, `…MissingField`, `…BadUnit`, `…WrongSSEFirstEvent`,
  `…MissingSSEIncremental`. The prober demonstrably FAILS a broken payload — it is a real assertion,
  not a tautology.
- `TestSetWorkerCountShrinkConverges -count=5 -race` → 5/5 PASS, no races.
- golangci-lint: NOT run — the locally installed binary (built with go1.25) refuses this go1.26
  module ("language version lower than targeted"). CI uses a matching toolchain. `go vet` (the
  load-bearing correctness lints) is clean. See nit N3.

## Target-by-target verdict

1. **Token never serialized — HOLDS.** Three paths checked:
   - Settings GET (`settings.go:81-84`): the token entry hard-nils `entry.Value = nil` and sets
     `entry.IsSet = &isSet` (a bool). `resolvedValue` is only reached in the non-token `else` branch,
     so the token value never enters value resolution. `d.cfg.OAuthToken` is read only to derive the
     `is_set` bool.
   - Settings PUT echo (`settings.go:276,295,301-306`): response carries `persisted` (key *names*
     from `sortedKeys`, not values) and `settings = d.resolveSettings()` (re-resolved → token null).
     The submitted token in `updates["token"]` is written to file, never serialized.
   - SSE `settings`/`snapshot` (`sse.go:133-135,201-222`): both emit `d.resolveSettings()` — same
     null-token output.
   - Log fan-out: the only call site that logs a token is `get-token`
     (`cmd/plundrio/main.go:454`, `Str("token", …)`). It is a **separate cobra subcommand**
     (`main.go:398,489`); only `run` installs the SSE hub via `dashboard.New → log.SetHubWriter`. In a
     `get-token` process `hubDst` stays `io.Discard`, so the token reaches stdout only — exactly the
     contract's stated model. No live leak.
   - The demo client serves no token; the prober greps the sentinel across every REST + SSE payload
     and found none. Tests `TestSettingsGetTokenNeverSerialized` / `TestSettingsPutTokenNotEchoed`
     assert both the raw-body grep and the structured value:null.

2. **transferprog MOVE — CORRECT, `:9091` unchanged.** `transferprog.go` is line-for-line identical
   to the HEAD `server/progress.go` (verified via `git show HEAD:…`); only identifiers went from
   unexported to exported. `server/progress.go` is now thin aliases (`progressInput = transferprog.Input`
   etc.) and `torrent.go:206` still calls `calculateProgress(progressInput{…})` — the RPC two-phase
   behavior is untouched, just delegated. The moved `transferprog_test.go` is the deleted
   `server/progress_test.go` re-pointed: same 16 `TestCalculateProgress` table cases by name + the
   permanent-failure test. No coverage loss — the one cross-package limitation (`completedFiles=0`,
   `0.5` not `0.25`) already existed in the deleted server test (it was also cross-package-limited via
   the same `newTestTransferCtx` workaround); the file-count path remains covered by
   `coordinator_test.go`.

3. **Worker resize — CORRECT; shrink test is sound, not spurious.** `workerCount` is single-owner
   under `m.mu` (`Start` and `SetWorkerCount` are its only writers, both under the lock; workers touch
   only `workerWg`). `workerQuit` is buffered (256) and never closed; `jobs`/`stopChan` are never
   closed by resize (issue #2 invariant held). `downloadWorker` gains the third `case <-m.workerQuit`
   (`download.go:25`). Shrink-determinism analysis: after `SetWorkerCount(2)` pushes exactly 3 tokens
   and the queue is drained empty, the 5 workers loop back to a select whose only ready cases are
   `workerQuit` (3 tokens) and `ctx.Done()` (not done). Exactly 3 receives succeed → exactly 3 workers
   exit, regardless of which workers win the select race; the other 2 block on an empty select. The
   count of exits is deterministic (= token count), independent of select ordering. The 150ms/200ms
   sleeps are timing margins, not the assertion — the load-bearing check is `maxBusy <= 2` on wave 2,
   and a not-yet-retired surplus worker has no token to consume and an empty queue, so it can never
   park for wave 2. `-count=5 -race` passed 5/5. Tests also cover grow, the issue-#2 concurrent
   QueueDownload+Stop+resize regression guard, floor-at-1, and no-op-when-not-running.

4. **Overrides precedence + env-skip — CORRECT.** `ApplyOverrides` (`config/overrides.go:131-147`)
   iterates `OverrideKeys`, skips any key where `IsEnvPinned` (an `os.LookupEnv` check) is true, and
   only then `viper.Set`s. `TestPrecedenceEnvBeatsOverrides` asserts the env-pinned + overrides-present
   case (env wins, key SKIPPED); `TestPrecedenceMatrix` walks env > overrides > default across four
   keys. The flag-binding map is NOT reordered — `main.go` adds the overrides layer *after* the
   `flagViperBindings` loop and only `viper.Set`s canonical keys. `dashboard-listen` is added to
   `flagViperBindings` (`main.go:44`) and `TestFlagViperBindingsCoverAllRunFlags` still enforces
   coverage (with `overrides-file` correctly exempted as a non-Config locator flag). The v0.10.11/12
   drift class is guarded.

5. **token persistable-but-never-boot-applied — SOUND resolution; no upstream bounce.** The builder
   resolved the contract/design underspec correctly with two key sets: `overrideKeySet` (boot-applied,
   token EXCLUDED) and `persistableKeySet` (WriteOverride-accepted, token INCLUDED).
   `LoadOverrides` keeps token on load (so WriteOverride round-trips it) but `ApplyOverrides` iterates
   only `OverrideKeys`, so a file token never reaches viper. `TestApplyOverridesTokenNeverBootApplied`
   proves `v.GetString("token") == ""` even with a token in the file; env/flag/config keep owning the
   resolved token. This is the only sane reading of "contract lists token under PUT `persisted` but
   design's overrides shape omits it" — persist for re-auth via the UI, never silently re-resolve a
   stale secret at boot. No planner bounce needed.

6. **Log fan-out — CORRECT.** `configureLogger` builds `zerolog.MultiLevelWriter(console, hubLeg{})`
   (`log/log.go`). `hubLeg.Write` reads the swappable `hubDst` under an RLock, ignores the sink's
   return, and always returns `(len(p), nil)` — the logger can never be stalled or errored by a slow
   SSE subscriber. The console (stdout) leg is preserved and re-read fresh each `configureLogger` (the
   logassert tap relies on this). Level gating is unchanged: zerolog gates by global level before
   invoking any writer, so a live `log_level` PUT widens/narrows the SSE stream for free. `GetLevel`
   reads `zerolog.GlobalLevel()` (the honest live source) for the settings report.
   `TestLogWriterFanOutStructured` asserts structured arrival, the info/debug gate, component
   presence, and stdout preservation in parallel.

7. **Dashboard listener — CORRECT.** `ReadHeaderTimeout: 10s` set (`dashboard.go:93`). Default-off:
   `main.go` constructs the dashboard only when `cfg.DashboardListen != ""`. The goroutine logs
   `Error` (not `Fatal`) on `ListenAndServe` error (`main.go` + `dashboard.go:145-161`,
   `http.ErrServerClosed` normalized to nil) so a dashboard failure never kills the RPC daemon.
   `:9091` (`server/`, `handlers.go`) is untouched — no auth added/removed, no timeout change, locked
   v1 decisions intact. Production (non-demo) boot path is unchanged.

## Nits (non-blocking)

- `internal/dashboard/response.go:16` — minor — `codeNotFound = "not_found"` is defined but never
  referenced; unknown `/api/*` routes fall through the mux catch-all `/` to the SPA `index.html`
  (HTTP 200), so the contract's "`404 (unknown route)` / `not_found`" code is never emitted for API
  paths. The prober doesn't test this and the SPA-fallback-for-deep-links is the documented design, so
  it's not a contract-breaking defect for the only consumer (the frontend). Would pass cleaner by
  either emitting a JSON 404 for unmatched `/api/*` (register an `/api/` subtree guard) or dropping
  the unused const. Flag for the frontend builder's awareness, not a backend bounce.
- `internal/dashboard/settings_test.go:17` & `internal/config/overrides_test.go:25` — minor —
  `clearPLDREnv` uses `t.Setenv(key, "")` which leaves the var *present* (empty), so
  `IsEnvPinned`/`os.LookupEnv` still reports it pinned. On a CI host that exports a real `PLDR_*` var,
  a test expecting a key to be unpinned could see it locked. Harmless on the standard runner (no
  `PLDR_*` set) and the contract test uses an explicit allowlist, but `os.Unsetenv` would be the
  isolation-correct primitive. Test hygiene, not a code defect.
- golangci-lint could not run locally (toolchain version skew, go1.25 binary vs go1.26 module). CI
  must remain the linter gate; `go vet` is clean here.

## Bounce direction

No bounce. All faults-of-note are nits, none traced to the contract or design being wrong (the one
genuine spec tension — token persistable vs. overrides-shape omission — was resolved soundly by the
builder per finding 5).
