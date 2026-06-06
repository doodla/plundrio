VERDICT: APPROVE

# integration-1 — end-to-end verification of the embedded dashboard

Booted the real embedded binary (the Svelte build copied into `internal/web/dist/` exactly as the Nix
`postPatch` does, then `go build`/`go run` recompiling with the UI embedded) in demo mode with a leak
sentinel token, and proved the whole thing end-to-end against the frozen contract and the approved
relaydeck design. The demo's fast-poll (1s) plus 8× virtual clock makes the two-phase climb observable
on the live wire. All six checks pass; the embed tree is restored to the committed placeholder and no
daemon/Chrome is left running.

Evidence under `.agents/dashboard/screenshots/e2e/`.

## Check 1 — Boot + isolation: PASS
Dashboard reachable at `:9092`; the listener does **not** serve Transmission RPC — `POST
:9092/transmission/rpc` returns the dashboard JSON `405 method_not_allowed`, not an RPC response. The
RPC `:9091` is a separate listener and answers `session-get` correctly; `:9091` does **not** serve
`/api/transfers` (404). Demo banner is loud (`WRN DEMO MODE ACTIVE — serving synthetic put.io data;
no real account or token is used`).
Evidence: `e2e/check1-boot-isolation.txt`.

## Check 2 — Two-phase live progress: PASS
Captured the climb via SSE `/api/events` + a 50 ms poll race against a fresh boot. Transfer 4002
(Movie) climbs the put.io leg with `local_phase: null` throughout — `percent_done` 0 → 0.015 → 0.08 →
0.15 → 0.215 → 0.28 → 0.33 → 0.40 → 0.48 as `putio_phase.percent_done` 0 → 96. At the handoff (t≈9 s)
`lifecycle_state` flips None → Downloading, `putio%`=100, `percent_done`=0.5, and `local_phase` flips
null → present (`downloaded:0`), then reaches Processed/1.0. The contract math holds exactly
(`percent_done = putio/200 + local·0.5`: putio%=96 → 0.48; handoff → 0.50). The SSE taxonomy
(`snapshot`, `transfers`~1s, `account`~15s, `log`, `settings`) all observed on the single stream.
Mid-flight flow-gauge screenshot: combined 8 %, "FETCHING AT PUT.IO" chip, put.io comet on the active
leg, `LOCAL —` muted, fleet rows showing both phase bars mid-fetch.
Evidence: `e2e/check2-twophase.txt`, `e2e/poll-twophase.csv`, `e2e/sse-raw-capture.txt`,
`e2e/midflight-ion-dark/desktop.png`.

## Check 3 — Log stream respects levels + stdout preserved: PASS
At `info`: 4 SSE `log` events over 4 s, **0 debug**. `PUT /api/settings {"log_level":"debug"}` →
`applied:[log_level]` (live), source flipped default→file; at `debug`: 52 events incl **47 debug**
(stream widened live). stdout console leg still produced 122 colored `DBG` lines (MultiLevelWriter
tees both legs).
Evidence: `e2e/check3-loglevels.txt`, `e2e/sse-loglevel-info.txt`, `e2e/sse-loglevel-debug.txt`.

## Check 4 — Settings round-trip: PASS
- Live `workers`: `PUT {workers:8}` → `applied:[workers]`; GET reports value 4 → 8.
- Restart-required `target`: `PUT {target:<existing dir>}` → `applied:[]`, `persisted:[target]`,
  `restart_required:[target]`; the overrides file on disk now contains the new `target` (alongside
  the persisted `log_level`/`workers`).
- Locked `token` (env-pinned via `PLDR_TOKEN`): `PUT {token:…}` → **HTTP 400** with the exact contract
  shape `{"error":{"code":"key_locked","message":…,"field":"token"}}`.
Evidence: `e2e/check4-settings-roundtrip.txt`, `e2e/overrides-on-disk.json`.

## Check 5 — Security invariant: PASS
Daemon booted holding `PLDR_TOKEN=LEAKCANARY_DO_NOT_SHIP` (the token IS present in `cfg.OAuthToken`).
Grepping the sentinel across **every** captured REST + SSE payload — `/api/transfers`, `/api/account`,
settings GET, settings PUT, and the full SSE window (`snapshot`, `transfers`, `account`, `log`,
`settings` events) — yields **0 matches**. `/api/settings` shows `token.value:null`, `is_set:true`,
`locked:true` (source `env`) in both GET and PUT. Independently corroborated by
`go test ./internal/dashboard -run TestContractAgainstDemoServer` (seeds
`contractprobe.DemoTokenSentinel` and asserts `assertNoToken` everywhere) and by running
`contractprobe.ProbeContract` against the **live** `:9092` server (passed; the prober's own no-token
scan is built in).
Evidence: `e2e/check5-security.txt`, `e2e/payloads/`.

## Check 6 — Polish parity: PASS
Live embedded UI captured at desktop (1440×900) + mobile (390×844) in ion·dark (default) and
tide·light, compared against the approved reference mockup (`screenshots/relaydeck-ion-dark`,
`relaydeck-tide-light`). The embedded binary renders the approved relaydeck design: flow-gauge reads
as one instrument (dial + center combined % + two-colored arc split + handoff node + traveling comet,
verified live mid-flight), the rail (brand/nav/palette-picker/mode-toggle/STREAM-LIVE pill), account
cluster with ticked disk meter, and fleet rows with mini-gauges + two phase bars all match the mockup
IA. tide·light keeps the instrument (graphite-on-pale) identity, not editorial; recolor only, same
geometry. Mobile stacks the gauge bay full-width as the hero per spec §7. No regression from the
ui-reviewer's approved design (ui-review-2 = APPROVE).
Evidence: `e2e/polish-ion-dark/{desktop,mobile}.png`, `e2e/polish-tide-light/{desktop,mobile}.png`,
`e2e/check6-polish-parity.txt`.

## Contract prober
`contractprobe.ProbeContract` run two ways, both green:
- the canonical in-process gate `go test ./internal/dashboard -run TestContractAgainstDemoServer`
  (boots the real dashboard in demo mode, embedded build) — PASS;
- against the **live** running `:9092` embedded server via a throwaway `PLDR_LIVE_BASE` test (deleted
  afterward) — PASS.

## Tree state
`internal/web/dist/` restored byte-exact to the committed placeholder (`.gitkeep` + the stub
`index.html` whose body says the dashboard "has not been built into this binary yet"); `git status
internal/web/dist` shows no change. No daemon and no capture-Chrome processes remain. Temp binary and
scripts removed. The only untracked additions are the e2e evidence and this review.
