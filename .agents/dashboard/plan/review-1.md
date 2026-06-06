# plan-reviewer verdict — iteration 1

VERDICT: BOUNCE

Two defects, both build/correctness-class. The plan is ~95% sound: every put.io field
citation is real, the progress math is quoted faithfully, the config-precedence seam is
correct *as written*, and the worker-resize invariant story holds — except for a single
double-accounting contradiction in who owns the reported worker count. Fix those two and
this approves.

---

## Defects (ordered by severity)

### 1. `go:embed dist` + committed `dist/.gitkeep` does not compile on a bare checkout

`design.md §1` / `File layout` (`internal/web/embed.go` = `//go:embed dist`, `internal/web/dist/.gitkeep`)
and `design.md §1` claim "`dist/.gitkeep` … committed so `go build` parses the embed on bare checkout."

**Why it fails:** `go:embed` excludes names beginning with `.` or `_`. A `dist/` directory whose
*only* content is `.gitkeep` matches zero embeddable files, and `go build` then errors:

```
pattern dist: cannot embed directory dist: contains no embeddable files
```

Verified empirically (Go, isolated module): dotfile-only `dist/` → build **fails**; this is the
exact pre-frontend state on a clean clone and in CI's `go build` job. The design's own parenthetical
("`go:embed` cannot embed dotfile/underscore names") is correct but draws the wrong conclusion — it
assumes a dotfile placeholder *satisfies* the pattern when it's precisely what the pattern skips.

**What would pass — either:**
- Commit a **non-dotfile** placeholder (e.g. `internal/web/dist/index.html` with a stub, or rename
  the keepfile to `internal/web/dist/placeholder`). Verified: builds clean. Or
- Change the directive to `//go:embed all:dist`. The `all:` prefix overrides the dotfile/underscore
  exclusion, so `dist/.gitkeep` alone then satisfies it. Verified: builds clean.

Prefer `all:dist` *and* a real index stub is unnecessary; pick one. Note `all:dist` also means the
real Vite build's dotfiles (if any) get embedded — harmless for a static SPA. Recommend `all:dist`
as the smaller diff that keeps `.gitkeep` as the committed marker. The Nix `postPatch` that copies
`ui/dist` → `internal/web/dist` is unaffected either way.

### 2. Worker-count ownership is double-booked — `workerCount` goes wrong on shrink

`design.md §5`. The design names two writers for the same variable with no reconciliation:

- bullet 2: "A worker that reads `workerQuit` **decrements the live count** and exits."
- final para: "`SetWorkerCount` sets `workerCount` under `m.mu`" and "the live `workerCount` is the
  source of truth the settings GET reports."

**Why it fails:** if `SetWorkerCount(n)` assigns the *target* (`workerCount = n`) and retiring
workers *also* decrement, the two accountings collide. Shrink 8→2: `SetWorkerCount` sets
`workerCount=2`, then the 6 retiring workers each decrement → reported count lands at `2-6 = -4`.
Grow has the symmetric problem only if growth also adjusts the same counter the workers touch. The
settings GET (`contract.md` `workers` value) then reports a wrong/negative number, and the
`workers` PUT round-trip e2e assertion (`design.md` test plan) would be checking a corrupt value.

This is *not* a races-the-jobs-channel problem — the buffered `workerQuit` + non-blocking shrink +
`running` guard genuinely preserve the issue-#2 invariant (jobs never closed, stopChan untouched,
no send after stop). The flaw is purely the count's ownership.

**What would pass:** pick a single owner. The clean model: **`workerCount` is the operator-set
target, written only by `SetWorkerCount` under `m.mu`; workers never touch it.** On shrink,
`SetWorkerCount` sets the new target and pushes `(old-new)` tokens onto buffered `workerQuit`; on
grow it sets the target and spawns `(new-old)` goroutines under `workerWg.Add`. Workers retiring on
`workerQuit` adjust **only the live-goroutine bookkeeping** (`workerWg`, which `Stop` already waits
on) — not the reported `workerCount`. The settings GET reports the target; the design's own honesty
note ("shrink is eventual; in-flight jobs drain") already covers the gap between target and live
goroutine count, so reporting the target is the right semantic. State this single-owner rule
explicitly in §5 so the builder can't reintroduce the worker-side decrement.

---

## Ruling: progress-math extraction — MOVE to `internal/transferprog` (the plan's default)

Move `calculateProgress`, `calculateProgressWithContext`, `mapPutioStatusValue`, `progressInput`,
and `progressResult` out of `internal/server/progress.go` into a new `internal/transferprog`
package imported by both `server` and `dashboard`.

Reasoning:
- **Parity by construction beats parity by test.** The contract's correctness ("do not reinvent —
  copy `server/progress.go`") rests on the dashboard producing byte-identical progress to the RPC
  path. A shared function makes drift *impossible*; duplicate-and-parity-test only catches drift
  that a test author remembered to pin, and the two-phase math has six branches
  (with-ctx/no-ctx × Processed/Completed/Failed-permanent/Failed-transient/default plus
  COMPLETED/SEEDING). A future tweak to one copy silently desyncs the dashboard from *arr — the
  exact "dashboard and *arr never disagree" invariant the contract leans on.
- **The moved symbols are pure.** They take a `progressInput` and read `*download.TransferContext`
  via its public methods; they have no `server`-package state. The only `server`-local coupling is
  the Transmission status constants (`trStatus*`) and `mapPutioStatusValue` returning a Transmission
  code — those move cleanly into `transferprog` as well, and `server` keeps importing them. The
  dashboard ignores the `Status int` field (it renders `putio_status` raw + `lifecycle_state`), so
  the Transmission coupling is inert there, not leaked.
- **Churn to the load-bearing `:9091` file is mechanical, not behavioral.** `progress.go` becomes a
  re-export or `server/torrent.go` switches to `transferprog.CalculateProgress`. No logic changes,
  and the existing `server` tests pin the behavior through the move. The "zero churn to `:9091`"
  argument for duplicate-and-test is real but buys less than the permanent-parity guarantee costs
  to give up.

Builder note: keep the moved API surface minimal and exported (`CalculateProgress`,
`ProgressInput`, `ProgressResult`); `render.go` and `server/torrent.go` both call the one function.

---

## Verified clean (evidence, not vibes)

**put.io field citations — all correct** (`go doc github.com/doodla/go-putio`):
- `Transfer`: `ID int64`, `Hash string`, `Name string`, `Size int`, `Status string`,
  `PercentDone int`, `Downloaded int64`, `DownloadSpeed int`, `EstimatedTime int64`,
  `ErrorMessage string`, `CreatedAt *Time`, `FinishedAt *Time` — every cited field exists with the
  type the contract claims (the contract correctly flags `Size`/`DownloadSpeed` as Go `int`, and
  `CreatedAt`/`FinishedAt` as nullable → "RFC3339 or null", matching `*putio.Time` wrapping
  `time.Time`).
- `AccountInfo`: `Username string`, `DaysUntilFilesDeletion int`, and the anonymous
  `Disk` struct with `Size int64`, `Used int64`, `Avail int64` — all present, all int64 bytes as
  the contract states. (`go doc …Disk` errors only because `Disk` is an anonymous field, not a named
  type — the fields are real.)

**Existing server symbols — all correct** (`internal/server/progress.go`, `utils.go`,
`torrent.go`):
- `calculateProgress` + `calculateProgressWithContext` exist; `progressResult` has `PercentDone`,
  `LeftUntilDone`, `Error` (plus `Status`, `LocalETA`, `LocalSpeed`).
- Two-phase math matches the contract verbatim: with-ctx `putio/200 + local*0.5`; `Processed`
  pins `1.0`/left=0; no-ctx `PutioPercentDone/200`; `COMPLETED`/`SEEDING` → `1.0`.
- `checkDiskQuota` over-quota threshold is `usagePercent >= 95` — the contract's `>= 95` is exact.
- Error precedence (`torrent.go:223-226`) is `errString = ErrorMessage; if prog.Error != "" → prog.Error`
  — the contract's "permanent string wins, else ErrorMessage, `error = error_string != ""`" is faithful.
- `local_phase.eta_seconds` mirrors `torrent.go:230` (`int64(time.Until(LocalETA).Seconds())`, `>0`
  guard); `local_phase.rate_download` cites `localSpeed float64 // bytes/sec` (`types.go:76`) — exact.

**`TransferContext` DTO mapping — all backing methods exist** (`internal/download/types.go`):
`GetProgress()` (downloadedSize/totalSize int64, completedFiles/failedFiles int32), `TotalFiles`
int32, `GetLocalProgress()` (speed float64/eta), `GetState().String()`, `IsPermanent()`,
`GetProcessedAt()`. `Manager.GetCategory(hash)` exists (`manager.go:80`).
(Minor, non-blocking: contract lists `lifecycle_state` as `None|Initial|…|Processed`, but
`String()` returns `"Unknown"` for an unmatched state, never `"None"` — the contract already says
`"None"` is the render layer's no-ctx sentinel, so this is consistent; the builder must not expect
`String()` to emit `"None"`.)

**Config precedence seam — correct as written, but the env-skip is load-bearing** (`main.go`
`loadConfig`/`flagViperBindings`, viper v1.21.0, empirically tested):
- `viper.Set(key)` is viper's Override (highest) tier — verified it beats a *changed* bound pflag
  (`OVERRIDE` wins over `--workers FLAG`).
- **It also beats env in raw viper** (`viper.Set` + `PLDR_WORKERS` set → `OVERRIDE`, not env). The
  design's `os.LookupEnv` skip is therefore *the entire mechanism* that preserves decision 2's
  "env beats overrides," not a nicety — if a future edit drops the skip, env-pinned keys silently
  lose to the overrides file. The design specifies the skip correctly; flagging it so the builder
  treats the `os.LookupEnv("PLDR_"+upper(key))` guard as non-negotiable and tests it.
- This does **not** reintroduce the v0.10.11/12 drift: the existing canonical-key
  `BindPFlag(viperKey, …)` map is untouched; only canonical keys are `viper.Set`. No new
  flag/key-name surface.
- **Consequence to record (not a defect):** an explicitly-set CLI **flag** does *not* beat the
  overrides file (`override + changed-flag → OVERRIDE`). Per decision 1 the overrides file is
  highest precedence, so this is intended. It's benign for the real deploy because the Docker/compose
  deployment configures via `PLDR_*` **env** (which the skip protects), not flags — flags aren't in
  the runtime path there. Worth one sentence in the design so it's a documented choice, not a
  latent surprise.

**Worker-resize invariant (the part that's right):** `m.jobs` never closed; `QueueDownload` selects
`m.jobs <- job` vs `<-m.stopChan` under `m.mu` (`manager.go:281-299`); workers exit on `<-m.stopChan`
(`download.go:21`); `Stop` sets `running=false` + closes `stopChan` under `m.mu` then `workerWg.Wait`.
Buffered `workerQuit` (cap 256) + non-blocking shrink + `running` guard preserve all of this and can't
panic or deadlock under concurrent `QueueDownload`/`Stop`. Only the *count ownership* (defect 2) is wrong.

**Log fan-out:** `configureLogger` builds `zerolog.New(consoleWriter)` (`log.go:46`); wrapping in
`MultiLevelWriter(consoleWriter, hubWriter)` preserves stdout + level gating (zerolog gates on
`SetGlobalLevel` before any writer is invoked, `log.go:61-78`), and the additive `SetHubWriter` +
discard default leaves all existing signatures intact. Plan's claim holds. `TimeFieldFormat =
time.RFC3339` and the `.Str("component", …)` on every `Debug/Info/Warn/Error/Fatal` confirm the
`<LogEvent>` schema (`time`/`level`/`component`/`message`/`fields`) is parseable from the JSON line.

**Listener isolation / locked decisions:** separate `--dashboard-listen` listener, RPC `:9091`
untouched, no auth v1 (with `ReadHeaderTimeout` added on the *new* bind per the slowloris note —
correctly scoped to the new listener only), token never serialized (settings reports `is_set` bool,
prober greps the token value across every payload). All four features covered (two-phase transfers,
account/quota, level-respecting log stream, editable settings with live/restart split + locked).

**Test plan** pins the behaviors that matter: prober (reachability drift-guard, schema, snapshot,
token-absence grep), e2e (two-phase climb 0→50→100, live `log_level` widening, `workers` PUT,
restart-required round-trip), unit (precedence matrix incl. env-pinned-locked + malformed-surfaced,
worker grow/shrink convergence + no-panic under `-race`, hub fan-out/drop, render parity, settings
live-apply + locked-reject). The render parity test is what backstops the progress-math MOVE ruling.

---

## No un-captured operator decisions

Both defects are plan-fixable without operator input. The progress-math extraction was explicitly
handed up and is ruled above. The flag-vs-overrides consequence is a documentation ask, not a
decision — decision 1 already settled overrides as highest precedence.
