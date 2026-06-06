VERDICT: BOUNCE

# ui-review-1 — live dashboard visual & interaction review

Reviewed the **live running app**, not the static mockup: built `ui/dist`, embedded it into the
daemon (`internal/web/dist`, restored to the committed placeholder afterward), ran
`go run ./cmd/plundrio run --demo --dashboard-listen :9092`, and screenshotted `:9092/` over the
snapshot tool. All six palette×mode combos, desktop + mobile, plus dedicated surface pages and
behavior probes.

**The build is a faithful, high-quality realization of the relaydeck SPEC** — the flow-gauge reads as
one instrument, the three palettes are total recolors with no layout drift, the rejected dial glow is
absent, and every surface + behavior the SPEC demands is present and works live. It is **not** the
rejected round-1 "generic" look. The bounce is for **two narrow but real light-mode contrast
violations that the SPEC §8 explicitly told the build not to commit** — small body/meta text colored
with `--local` / `--green` on a pale surface, where those tokens are AA-large/UI only. Everything else
passes. This is a localized fix, not a redesign.

---

## Why this is a bounce, in one sentence

SPEC §8 constraint #1 is explicit: *"`--local` and `--green` as text are AA-large/UI only in light
modes … never for small body/meta text on a light surface."* The build uses `--local` for the
**12.5px log component tag** and `--green` for the **13px "done" status** on light surfaces — both
measure ~4.0:1, below the 4.5:1 AA floor for text that small. The dark modes are fine; only the three
light palettes are affected.

---

## MUST-FIX (a11y — light mode only)

### 1. Log surface · log component tag — a11y fail — `--local` as 12.5px text on a light surface
`.ln .c { color: var(--local) }` (`ui/src/styles/app.css:1153`) at `--cBody font-size:12.5px` regular
(`:1086`). Measured `--local` on `--surf-1` in light: **tide-L 4.00**, ember-L 4.68, ion-L 5.82
(SPEC §8 table). 12.5px regular is small text → AA needs ≥4.5, so **tide-light fails (4.00)** and the
others sit on the edge. SPEC §8 names the log component tag as the canonical `--local`-as-text case
and clears it *for dark modes only*; the same token must not paint it in light.
**What would pass:** in light modes, color the component tag `--ink-2` (clears AAA everywhere) — or
darken the per-palette light `--local` until the tag clears 4.5 at 12.5px. Keep dark mode as-is.
Evidence: `.agents/dashboard/screenshots/ui-review-1/tide-light-fold.png`,
`.agents/dashboard/screenshots/ui-review-1/page-logs-ion-dark/desktop.png` (dark, fine).

### 2. Fleet row · "done" status label — a11y fail — `--green` as 13px-bold text on a light surface
`.trMeta b.green { color: var(--green) }` (`ui/src/styles/app.css:987`) at `.trMeta b font-size:13px`
bold. Measured `--green` on `--surf-1` in light: **tide-L 4.03, ember-L 4.19, ion-L 4.03** — all
AA-large/UI only (SPEC §8). WCAG "large" is ≥18.66px regular or ≥14px bold; **13px bold is below the
bold-large threshold**, so this is small text at ~4.0 and **fails AA in all three light palettes**.
**What would pass:** either bump that label to ≥14px bold (then 4.0 clears the 3:1 large/UI class the
SPEC allows for `--green`), or color the word "done" with `--ink-2` and keep the `--green` signal on
the adjacent dot/mini-gauge (color is already not the sole signal — the row says "completed"). Keep
the completed gauge's large `100%` numeral in `--green` (that one is display-size, allowed).
Evidence: `.agents/dashboard/screenshots/ui-review-1/deck-ember light-/desktop.png`,
`.agents/dashboard/screenshots/ui-review-1/fold-ember-light/desktop.png`.

> Both findings are the *exact* cases SPEC §8 pre-flagged; the tokens themselves are correct
> (byte-matched to the locked table) — the defect is using them as small text on light surfaces. The
> dark modes are unaffected and pass.

---

## POLISH NITS (non-blocking)

### A. Hero comet diameter — polish — 14px vs SPEC's 16px
`.comet { width:14px; height:14px }` (`ui/src/styles/app.css:442`); SPEC §3 specifies a 16px disc on
the hero. Reads fine and premium at 14px; flagging only for byte-fidelity. The mini-gauge comet has no
size called out, so no issue there.
Evidence: `.agents/dashboard/screenshots/ui-review-1/lifecycle-fetch-fold/desktop.png`.

### B. Palette picker — polish — swatch+text pills vs SPEC's "three swatch dots"
SPEC §5 describes "a pill group of three swatch dots, the active one ringed with
`0 0 0 3.5px currentColor`." The build ships swatch **+ text label** pills (ION / TIDE / EMBER) and the
active ring measures `0 0 0 1px` (lighter than the 3.5px called out). Active state is unambiguous and
the control is keyboard-reachable with a visible focus ring, so this is cosmetic. Consider thickening
the active ring toward the spec'd 3.5px for a stronger "selected" read.
Evidence: `.agents/dashboard/screenshots/ui-review-1/fold-tide-dark/desktop.png` (TIDE active, ringed).

---

## What PASSES (with evidence)

**1. Design fidelity — PASS.** The flow-gauge hero reads as ONE instrument across every state: 270°
arc (gap at bottom), 12-o'clock handoff tick, put.io leg / local leg color split, comet riding the
active leg's leading edge, combined-% metal-gradient numeral, dual legend below. The rejected diffuse
**dial glow blob is absent** — arcs/comet use a crisp `drop-shadow(0 0 2–3px)` edge sheen only
(`app.css:408/418/446`), and `theme.css:7` documents the radial glow removal. Type scale, 4px spacing
rhythm, tabular mono numerals, two-step elevation discipline all hold. Not the round-1 generic look.
Evidence: `fold-ion-dark/desktop.png`, `lifecycle-fetch-fold/desktop.png`.

**2. Palette × mode system — PASS (all six combos).** Identical IA + gauge geometry; only color
changes. Phase contract holds everywhere: `--putio` (azure/periwinkle/steel) vs `--local`
(aqua/coral/indigo) stay distinct; `--amber` distinct from both; `--green`/`--red` semantic. Light
mode keeps the graphite-instrument identity (not editorial). Disk meter, avatar, chips, comet all
recolor per palette.
Evidence: `fold-ion-dark`, `fold-tide-dark`, `fold-ember-light`, `tide-light-fold`,
`lifecycle-tide-dark-fold`, `lifecycle-ember-light-fold`, and the six `deck-*` full-page captures.

**3. All four surfaces — PASS.**
- **Transfers two-phase across lifecycle states:** queued (empty arc, dimmed 0%, "waiting for
  put.io"), put.io-fetching (lit put.io leg + comet, "fetching at put.io · 62% fetched"),
  local-downloading (put.io leg muted/handed-off + lit local leg + comet, "2/4 files · ETA 1:56 ·
  1.0 GB landed"), completed (both legs green, 100%, "source cleaned"), failed (red wash, PERMANENT
  chip + error string). Verified live (`lifecycle-early` = queued; live deck = completed/failed) and
  via a held EventSource snapshot for the two transient states the demo's 30s-monitor-vs-8×-clock
  timing makes unobservable on the wire.
  Evidence: `lifecycle-early/desktop.png`, `lifecycle-all-ion-dark/{desktop,mobile}.png`,
  `lifecycle-fetch-fold/desktop.png`.
- **Account/quota — PASS:** avatar (put.io→local gradient), name + "put.io account", UNLIMITED pill,
  ACTIVE TRANSFERS + AGGREGATE ↓ instrument tiles, ticked disk meter (10% ticks, fill = used_percent),
  "used … · %" / "free … of …". Evidence: `page-account-ion-dark/desktop.png`.
- **Live log — PASS:** 5px level color-bar (info=putio, warn=amber, error=red), HH:MM:SS timestamp,
  LEVEL chip, component tag, message+fields; header filter chips (debug/info/warn/error) + autoscroll
  indicator with live dot + buffered count; mobile drops the LEVEL column per §7.
  Evidence: `page-logs-fold/mobile.png`, `page-logs-ion-dark/desktop.png`.
- **Settings — PASS:** per-key slots with source/LIVE/RESTART/LOCKED badges; log_level segmented
  control (active `--local` fill), workers ± stepper, locked `target` read-only + lock glyph, masked
  `•••• set` token (value never rendered), dirty save bar ("● 1 pending" / Discard / Save).
  Evidence: `page-settings-ion-dark/desktop.png`, `harness-settings-dirty.png` (LOCKED + masked
  token + dirty bar), `harness-settings-token.png`.

**4. Behavior — PASS.**
- **SSE live-updates:** confirmed real two-phase flow on the wire — `transfers` SSE drives
  `lifecycle_state` + `putio_phase` + `local_phase`, aggregate throughput live (saw 8.5 MB/s during a
  fresh-boot fetch; 20.9 MB/s = put.io 11.5 + local 9.4 derived correctly). Gauge + log update live.
- **Theme persistence:** clicked ember + light → `localStorage = ember/light` → reloaded with **no URL
  params** → page returned `ember/light`. The pre-paint head script reads localStorage; the two keys
  are independent. Evidence: `persist-after-reload.png` + console log.
- **Connection-lost:** SSE error → rail pill swaps to amber RECONNECTING (no pulse), amber ribbon
  "stream lost — showing last known state · reconnecting", **stale data stays painted** (not blanked).
  Evidence: `connection-lost.png`.
- **Reduced-motion:** with `prefers-reduced-motion: reduce`, comet + live-pill `animation-name`
  computed to `none`; layout/state fully legible static. Evidence: `reduced-motion.png`.

**5. A11y floor — partial (see must-fix).** Focus: visible `outline:2px solid var(--local)` on
interactive elements, keyboard-reachable in DOM order (Tab into rail focuses nav button with crisp
indigo ring). Color never sole signal (status text + LEVEL chip + human label). Contrast holds in dark
modes and for `--ink/--ink-2/--amber/--red-ink` everywhere; the two light-mode small-text misuses above
are the only failures. Evidence: `focus-ring.png`.

**6. Token at the glass — PASS.** `/api/settings` reports `token:{value:null,is_set:…}` only; SSE +
REST payloads never carry a token value; settings UI shows `•••• set` / `not set`, never a value
(hint: "value never read back, only replaced").

---

## Bounce direction
→ **frontend-builder.** Two localized light-mode contrast fixes (swap `--local`/`--green` small-text
to `--ink-2`, or bump size/darken token) + optional polish (16px comet, thicker palette active ring).
The design itself is sound and live-validated — no ux-designer revision needed; the SPEC already
predicted these two cases and the fix is to honor §8 constraint #1, not to change the design.

## Notes for the operator (real running dashboard)
Key views to surface:
- Default (ion · dark), full deck: `.agents/dashboard/screenshots/ui-review-1/fold-ion-dark/desktop.png`
- Two-phase hero mid-flight (local-downloading + fetching fleet row):
  `.agents/dashboard/screenshots/ui-review-1/lifecycle-fetch-fold/desktop.png`
- All five lifecycle states at once: `.agents/dashboard/screenshots/ui-review-1/lifecycle-all-ion-dark/desktop.png`
- Palettes: `fold-tide-dark/desktop.png`, `fold-ember-light/desktop.png`, `deck-ion light-/desktop.png`
- Settings (badges + masked token + dirty bar): `harness-settings-dirty.png`
- Logs: `page-logs-fold/mobile.png`; Account: `page-account-ion-dark/desktop.png`
- Connection-lost: `connection-lost.png`

**Theme picker + persistence: WORK LIVE.** Palette swatches + ☀/☾ toggle apply instantly, write
localStorage, and survive a param-less reload; the two axes are independent.
