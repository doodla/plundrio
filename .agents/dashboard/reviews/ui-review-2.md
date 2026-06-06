VERDICT: APPROVE

# ui-review-2 — focused re-verification of the round-1 fixes

Scope: confirm the seven items (3 must-fix + 4 nits from ui-review-1 + the code-reviewer) now pass and
nothing regressed. Same live setup as round-1: built `ui/dist`, embedded into the daemon
(`internal/web/dist`, restored to the committed placeholder afterward),
`go run … run --demo --dashboard-listen :9092`, screenshotted `:9092/`. The demo now fast-polls (1s
`TransferCheckInterval` in demo mode, `cmd/plundrio/main.go:259`), so the live wire climbs through the
put.io-fetch phase — captured a live two-phase mid-download deck without a held snapshot. Contrast was
measured in-browser (WCAG 2.1 relative-luminance, fg flattened over the resolved card surface).

**All seven pass. No regression.** The two light-mode contrast misuses are now comfortably AA; the
focus, motion, and interaction fixes are present and behave correctly.

---

## MUST-FIX — now passing (measured)

### 1. Settings text inputs — visible focus indicator — PASS
`.inp:focus-within` and `.tokenRow input:focus-visible` (`ui/src/styles/app.css:1282`, `:1402`) both
draw `inset 1px --hair-2` + `0 0 0 2px --local`. Measured on the focused `target` input (ion-dark):
`box-shadow: …inset 1px rgba(190,200,220,.14), rgb(139,124,255) 0 0 0 2px` — a crisp 2px indigo ring.
The token replace input shows the same ring once "Set token…" opens it (measured present after click).
Previously these had no focus indicator. Keyboard-reachable.
Evidence: `ui-review-2/settings-input-focus.png` (target input ringed), `ui-review-2/token-input-focus.png`.

### 2. Log component tag in LIGHT mode — now `--ink-2`, clears AA — PASS
`:root[data-theme='light'] .ln .c { color: var(--ink-2) }` (`app.css:1170`) at 12.5px regular.
Measured contrast on the light log surface (was 4.00 in tide-light, the round-1 fail):
- **tide-light 7.50** · **ember-light 8.85** · **ion-light 8.61** — all ≥4.5 AA (graphite secondary).
Dark mode still uses `--local` (the teal component tag), which already cleared AA there.
Evidence: in-browser measurement above; `ui-review-2/hero-tide-light/desktop.png` shows graphite logs.

### 3. Fleet "done" status in LIGHT mode — now `--ink-2`, clears AA — PASS
`:root[data-theme='light'] .trMeta b.green { color: var(--ink-2) }` (`app.css:998`) at 13px bold.
Measured (was ~4.03 in light, the round-1 fail):
- **tide-light 8.46** · **ember-light 9.97** · **ion-light 9.90** — all ≥4.5 AA. The completed signal
  is still carried by the green mini-gauge dot (color is not the sole signal).
Evidence: `ui-review-2/hero-tide-light/desktop.png` ("done" rows render graphite, green 100 dot intact).

---

## NITS — now passing

### 4. MiniGauge failed arcs recolor red (standalone) — PASS
MiniGauge now derives `a1Class`/`a2Class`/`pcClass` from `displayState(transfer)` directly
(`ui/src/lib/components/MiniGauge.svelte`), not via a parent prop, and `.mini .a1.red`/`.a2.red`
(`app.css:858`) apply. Verified on a mid-progress failed transfer (held snapshot — the live demo's
failed transfer errors at 0% put.io with no arcs to recolor, so it can't exercise the arc path): both
arc strokes compute `rgb(255,111,122)` = `--red`, center `.pc` is red. The live failed row also shows
the red center + red `PUT.IO ERROR` chip + red row wash correctly.
Evidence: `ui-review-2/minigauge-failed-midprogress.png` (red hero + red mini-gauge arcs),
`ui-review-2/minigauge-failed.png` (live failed row, red center + wash).

### 5. Hero comet is 16px — PASS
`.comet { width:16px; height:16px }` (`app.css:441`). Measured live comet on the put.io-fetch hero:
`16px x 16px` (was 14). Evidence: `ui-review-2/hero-live-twophase/desktop.png`.

### 6. Palette active ring is 3.5px — PASS
`.palpick button.on { box-shadow: 0 0 0 3.5px var(--hair-2) }` (`app.css:225`). Measured on the active
TIDE swatch: `…0 0 0 3.5px` (was 1px). Note: the ring uses `--hair-2` rather than the SPEC's literal
`currentColor`, but the requested 3.5px thickness is met and the active state reads clearly.
Evidence: `ui-review-2/hero-tide-light/desktop.png` (TIDE ringed).

### 7. Token "● pending" clears when the replace field is typed-then-emptied — PASS
`dirtyKeys` counts token as pending only while `tokenInput.length > 0`
(`ui/src/lib/components/SettingsRack.svelte:49-50`). Driven live: initial `none` → type "secret-xyz" →
`● 1 pending` → clear the field → `none`. No stale pending left behind.
Evidence: in-browser interaction trace (`{initial:none, afterType:"● 1 pending", afterEmpty:none}`).

---

## Regression spot-check — clean

- **Default ion-dark deck:** flow-gauge hero (completed 100% green, no glow blob), account cluster,
  ticked disk meter, fleet rows — unchanged from round-1. `ui-review-2/hero-ion-dark/desktop.png`.
- **tide-light + ember-light:** instrument identity in daylight preserved, palette recolor total
  (aqua-teal / coral disk + chips), completed gauge green. `ui-review-2/hero-tide-light/desktop.png`,
  `ui-review-2/hero-ember-light/desktop.png`.
- **Live two-phase flow:** put.io-fetch captured live on the wire (gauge climbing 6–13%, comet on the
  put.io leg, AGGREGATE 34.0 MB/s, fleet put.io bars filling). The local-download focal-hero dwell is
  still sub-100ms on the wire for the small demo files (manager picks up → grab downloads 12–40 MB →
  Processed within one 1s tick), so that focal state remains screenshot-only via held snapshot; the
  FlowGauge code for it is unchanged and was proven in round-1. `ui-review-2/hero-live-twophase/desktop.png`.
- **Token never visible:** `/api/settings` + SSE snapshot report `token.value: null`; settings UI shows
  `not set` / `•••• set`, never a value.

---

## Operator hero shots (real running dashboard)
- Default deck (ion · dark): `.agents/dashboard/screenshots/ui-review-2/hero-ion-dark/desktop.png`
- Light palette (tide · light): `.agents/dashboard/screenshots/ui-review-2/hero-tide-light/desktop.png`
- Live mid-download two-phase: `.agents/dashboard/screenshots/ui-review-2/hero-live-twophase/desktop.png`
