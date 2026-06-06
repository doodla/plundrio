# Frontend (M5) code review — plundrio dashboard `ui/`

VERDICT: BOUNCE

Scope: client-side correctness, contract adherence, silent failures, the no-token invariant.
Visual polish is the ui-reviewer's; flagged crossovers are marked. I did not boot the daemon or
touch :9092, did not run `npm run build`, did not edit code or run git.

## Tooling results (all green)

- `npm run check` (svelte-check) — **284 files, 0 errors, 0 warnings.**
- `npm test` (vitest) — **2 files, 21/21 passing** (`format.test.ts`, `transfer.test.ts`).
- `npm run lint` (eslint + prettier) — **clean**: no eslint findings, all files match Prettier style.
- tsconfig is `strict` + `noUnusedLocals`/`noUnusedParameters`; svelte-check clean ⇒ no dead locals.

## Ruling: the `displayState` `error`-vs-`permanent` interpretation — CORRECT

`ui/src/lib/util/transfer.ts:33` keys the `failed` (red) trigger on `t.error`, not `t.permanent`.
Validated against the contract's "Error precedence" section (contract.md:199–203):

- The contract defines `error == (error_string != "")`, and `error_string` is the permanent-cascade
  string when `ctx.IsPermanent()` **else** put.io's `ErrorMessage`. So `error:true` already captures
  BOTH a permanent local-cascade give-up AND a put.io-side ERROR (no ctx, `permanent:false`). Keying
  on `permanent` alone would mis-render a put.io ERROR as `putio-fetching`. Keying on `error` is the
  correct, complete reading.
- A transient Failed (`permanent:false`, `error:false`) correctly falls through to its underlying
  phase — verified by the `lifecycle_state === 'Downloading'` branch and the explicit test at
  `transfer.test.ts:117`.
- `permanent` is then used only to refine chip wording, not the red trigger: FleetRow.svelte:54/77/117
  ("permanent" / "gave up after retries" vs "put.io error" / "errored"), and TransferHero's failed
  legend shows `error_string`. Consistent across hero + fleet.
- Lifecycle×error matrix walked: `completed` (percent==1 / Completed / Processed / no-ctx
  COMPLETED|SEEDING), `local-downloading`, `putio-fetching`, `queued`, and both error variants all
  resolve to the contract-correct display state. No contract-valid state mis-renders.

Conclusion: not a bounce. **Upstream doc note:** SPEC §3.2's failed row literally reads
`permanent==true`; it should be reconciled to "failed when `error==true`; `permanent` refines wording"
to match the contract's error precedence. Doc-only — the code is right.

---

## Must-fix

1. `ui/src/styles/app.css:1258` and `:1373` — **accessibility (a11y floor, SPEC §8)** — the two
   settings text `<input>`s (`.inp input` = target/folder/listen/dashboard_listen; `.tokenRow input` =
   the token-replace field) set `outline: none` with **no `:focus` / `:focus-visible` replacement**
   (no ring, border, or box-shadow change on focus). The global focus-visible rule (app.css:110–119)
   covers buttons only — `.rail nav button`, `.toggle`, `.palpick`, `.seg`, `.step`, `.btn`, `.lvl` —
   not these inputs. SPEC §8: "Never remove outline without an equal-or-better replacement." Keyboard
   users get zero visible focus on the settings text fields. — **Pass:** add an `:focus`/`:focus-within`
   indicator on `.inp input` and `.tokenRow input` (the `outline:2px solid var(--local); outline-offset:2px`
   already used elsewhere, or a `box-shadow` ring on `.inp` / `.tokenRow`).
   (Crossover note: focus visibility is partly ui-reviewer territory, but this is a concrete
   outline-removed-without-replacement defect against a SPEC-mandated a11y constraint, so it gates here.)

## Nits (non-blocking)

- `ui/src/lib/stores/theme.ts:40` + `index.html:22` — localStorage mode key is `pldr-theme`; SPEC §5
  names it `pldr-mode`. **Read and write agree** (head script reads `pldr-theme`, `setMode` writes
  `pldr-theme`), so persistence works — purely a deviation from the SPEC's named key. Upstream doc/
  rename nit, not a functional break. Pick one name and reconcile SPEC §5 ↔ code.
- `ui/src/lib/components/MiniGauge.svelte:13,16` — failed-state arc classes (`a1Class`/`a2Class`)
  recolor only `completed`→green; no `red` recolor for `failed`. SPEC §3.3 says the failed fleet row
  gets "red arcs." The row already gets the `--red-bg` wash + chip (FleetRow), and the bars recolor
  red, but the mini-gauge arcs themselves stay phase-colored. Visual fidelity — defer to ui-reviewer;
  not a correctness bounce.
- `ui/src/lib/components/SettingsRack.svelte:74-83` — if the user opens "Replace token…", types, then
  clears the field to empty, `draft.token=''` keeps the "● 1 pending" indicator lit while Save is a
  silent no-op (early return on empty body). Minor UX wart; never leaks/reads the value. Optional:
  drop `token` from the draft when `tokenInput` is empty.

---

## What passed (verified, not asserted)

**No-token invariant — HOLDS.** Token `value` is `null` per contract and is never rendered: the token
slot (SettingsRack.svelte:239–289) only ever shows `tk.is_set` ("•••••••••••••••••••• set" / "not set"),
never `tk.value`. The replace input is write-only via a separate `tokenInput` local
(`bind:value`, never seeded from the server). `effective('token')` is never wired to the token field's
display. Token is sent on PUT only when `tokenInput.length > 0` (SettingsRack.svelte:74-75). The locked
text-key renderer (`{e.value}`, line 220) is in the TEXT_KEYS block; token is never in TEXT_KEYS, so
the null token value can't render there. Grep of all `src/` token references confirms no read-back.

**Contract parsing / units — CORRECT.**
- `putio_phase.percent_done` treated as 0–100 (`Math.min(...,100)/100` in transfer.ts:187,207;
  rendered raw as `{putioPct}%`); `percent_done` treated as 0.0–1.0 (`*100` for display). No 0–1/0–100
  mixups. (`types.ts` documents both conventions correctly.)
- Bytes via `humanBytes` (SI/1000, matches put.io GB/TB display); rates bytes/sec via `humanRate`;
  ETAs seconds via `humanEta` (0 → "—" per contract "0/absent = unknown"). `format.test.ts` covers.
- RFC3339 parsed via `new Date(...)` with `isNaN` guards returning sentinel strings, not crashes
  (`clockTime` → "--:--:--", `relativeAge` → "").
- `local_phase: null` handled null-safe everywhere via `?.` / explicit `=== null` branches
  (transfer.ts:81,87,96; FlowGauge, MiniGauge, FleetRow, TransferHero all guard); `localFraction`
  falls back to the combined `percent_done*2-1` identity when `local_phase` is null or `total===0`.
  Renders put.io-only without crashing.
- `error`/`permanent` used per contract (see ruling above). Account SSE is the bare `<Account>`,
  `transfers`/`settings` SSE are `{transfers}`/{settings}`-wrapped — all parsed with the right shape.

**SSE handling — CORRECT.** Reconnect re-snapshots: `applySnapshot` (data.ts:36) replaces every slice
(`set`, not merge) so a fresh `snapshot` on reconnect cannot duplicate or leave stale state.
Connection-lost is surfaced, not swallowed: the `error` listener flips `connState` to `reconnecting`
(data.ts:106), which drives the Rail "RECONNECTING" pill (Rail.svelte:43, `role="status"`) and the
App-level "stream lost" ribbon (App.svelte:35-37). The log buffer is bounded: `LOG_CAP = 600`, rolling
window in `appendLog` (data.ts:14,44-49); LogConsole caps view via the store. The per-event `catch {}`
blocks ignore a single malformed SSE frame (one bad frame shouldn't kill the stream) — they do NOT hide
connection failure (that path is the dedicated `error` listener), so this is intended subscriber-side
degradation, not producer/error swallowing.

**General CLAUDE.md hygiene — CLEAN.** No dead code (strict + noUnusedLocals, svelte-check clean).
No swallowed promise rejections: `putSettings` rejects with a typed `ApiRequestError` carrying the
contract's `{code,message,field}`; SettingsRack's `save()` catches and surfaces it to `fieldError`
(by `error.field`, → red ring) or `saveError`. The api.ts `catch {}` only guards a non-JSON error body
before re-throwing a fallback `ApiRequestError` — failure still propagates. Reduced-motion honored in
code: `@media (prefers-reduced-motion: reduce){ *{animation:none!important;transition:none!important} }`
(app.css:1490) — the SPEC §6 universal kill-switch. ARIA: status pills `role="status"`, log/segmented/
stepper controls have `aria-pressed`/`aria-label`/`role="radiogroup"`/`role="radio"`/`aria-checked`,
gauges `role="img"` + `aria-label`. (The text-input focus gap is the one a11y miss — must-fix #1.)
