# plundrio dashboard — Design Spec (Pass 2)

Direction: **`relaydeck`** — the operator's combo of `relay` + `tarmac`, with full light/dark theme
support. This is the contract the frontend-builder builds to. Every render field maps to
`plan/contract.md`; where this spec says "derive," the derivation is named so no design decision is
left to implementation. The reference mockup is `mockups/relaydeck/index.html` (offline,
`?theme=light` / `?theme=dark`).

**The mental model in one sentence:** plundrio is an *instrument console* — graphite, precise,
cockpit-legible — whose headline transfer is a single **flow-gauge** that is simultaneously a
glanceable combined-% dial (tarmac) and an explicit put.io→local journey (relay); everything else
(fleet, account, logs, settings) is supporting instrumentation in the same material.

---

## 1. Identity & accent

- **Base aesthetic:** instrument console (tarmac DNA). Graphite surfaces, hairline borders, layered
  soft shadows, tabular mono numerals, a radial hero. The light theme is the **same instrument in
  daylight** (cool graphite-on-pale-steel), *not* an editorial/paper look.
- **Primary accent: chartreuse.** One-line justification: amber is needed as a *semantic* state color
  (restart-required / caution), so promoting it to brand would collide with meaning; chartreuse is
  semantically unclaimed, reads as "live / go / energy," doubles cleanly as the local-download phase
  color, and can be darkened to hold contrast in light theme without losing identity.
- **Phase color system (must stay legible in BOTH themes):**
  - **put.io fetch = steel-blue.** The 0–50% leg / "in transit, not yours yet."
  - **local download = chartreuse.** The 50–100% leg / "landing on disk, becoming yours" — same hue
    as the brand accent, so the accent *means* "local/yours."
  - State colors, never used for phase: **amber** = restart-required/caution, **green** = live &
    completed, **red** = permanent failure.
  - In light theme, `putio` and `local` are *darkened* (see tokens) so they clear contrast on pale
    surfaces; the hue identity is preserved.

---

## 2. Design tokens — BOTH themes

> **⚠ PALETTE PENDING PICK.** The operator approved the layout + fused flow-gauge but rejected the
> chartreuse/steel palette below and is choosing among three recolor candidates (`tide` / `ember` /
> `ion`) rendered in `mockups/relaydeck/index.html` (`?palette=&theme=`). **The color values in §2.1
> (surfaces) and §2.3 (phase & state) are STALE pending that pick** — only the structure (token
> names, ramp shape, roles) is final. The hero dial glow (former `.bay` radial bloom) has been
> **removed**; the gauge bay is a flat instrument surface and arc/comet glows are restrained to a
> crisp edge. This SPEC will be finalized to the chosen palette's exact values after the operator
> picks. §2.5 (type), §2.6 (spacing/radius), §3–§10 are unaffected by the palette pick.

Implement as CSS custom properties keyed on `:root[data-palette="<pal>"][data-theme="<dark|light>"]`
(theme default dark; palette default = the chosen one). Token names are the contract; values become
exact once the palette is picked.

### 2.1 Color — neutral ramp & surfaces

| Token | Dark | Light | Role |
|---|---|---|---|
| `--bg` | `#07090c` | `#e8ecf1` | page background (under the radial wash) |
| `--surf-0` | `#0b0e13` | `#eef1f5` | recessed panels, gauge bay, log/settings wells |
| `--surf-1` | `#101620` | `#ffffff` | cards, rows, slots (the default surface) |
| `--surf-2` | `#141b27` | `#f4f6f9` | raised / row-hover |
| `--surf-3` | `#1b2433` | `#e4e9ef` | control wells, track background, ghost button |
| `--surf-4` | `#28344a` | `#d4dce6` | segmented-active bg, strongest hover |
| `--hair` | `rgba(170,200,235,.08)` | `rgba(30,50,75,.10)` | hairline dividers |
| `--hair-2` | `rgba(170,200,235,.14)` | `rgba(30,50,75,.16)` | stronger hairline / focus-less borders |

### 2.2 Color — ink (text)

| Token | Dark | Light | Role | Min use |
|---|---|---|---|---|
| `--ink` | `#eaf0f8` | `#11161d` | primary text, numerals | any size |
| `--ink-2` | `#aab8ca` | `#3c4654` | secondary text | any size |
| `--ink-3` | `#6f7e92` | `#5f6c7d` | labels, tertiary, log timestamps | ≥12px |
| `--ink-4` | `#4a5667` | `#93a0b0` | faint / disabled / version string | decorative + ≥12px non-essential |

### 2.3 Color — phase & state

| Token | Dark | Light | Role |
|---|---|---|---|
| `--putio` | `#5fb2ff` | `#1f6fc4` | put.io fetch arc/bar/dot, info-level log |
| `--putio-2` | `#1f5285` | `#93c4f2` | gradient deep-stop for put.io fill |
| `--putio-glow` | `rgba(95,178,255,.45)` | `rgba(31,111,196,.28)` | put.io drop-shadow |
| `--local` | `#c2f546` | `#5f8a0e` | local arc/bar/dot, brand accent, primary button bg |
| `--local-2` | `#5a7a13` | `#b6e34a` | gradient deep-stop for local fill |
| `--local-ink` | `#08120a` | `#ffffff` | text on a `--local` fill (button/seg label) |
| `--local-glow` | `rgba(194,245,70,.5)` | `rgba(95,138,14,.3)` | local glow / comet bloom |
| `--amber` | `#ffba3d` | `#9a6a16` | restart-required badge, dirty-state, warn log |
| `--green` | `#46d18a` | `#2f8f5e` | live dot, completed state |
| `--green-glow` | `rgba(70,209,138,.55)` | `rgba(47,143,94,.3)` | live-pill glow |
| `--red` | `#ff6b6b` | `#c0392f` | failure arc/bar/dot, error log, danger tick |
| `--red-ink` | `#ff9b9b` | `#a02a22` | failure text (on `--surf-1`) |
| `--red-bg` | `rgba(255,107,107,.1)` | `rgba(192,57,47,.08)` | failed-row wash, permanent chip bg |
| `--track` | `#1b2433` | `#d7dee6` | gauge & bar track well |

### 2.4 Elevation & numeral gradient

| Token | Dark | Light |
|---|---|---|
| `--sh-card` | `0 24px 60px -34px rgba(0,0,0,.85)` | `0 18px 44px -30px rgba(20,40,70,.40)` |
| `--sh-pop` | `0 3px 12px rgba(0,0,0,.40)` | `0 3px 12px rgba(20,40,70,.16)` |
| `--metal-a` / `--metal-b` (big-numeral text gradient) | `#ffffff` / `#bcc8d6` | `#1a2330` / `#46566b` |

Inset wells use `inset 0 0 0 1px var(--hair)` plus, where recessed, `inset 0 2px 6px rgba(0,0,0,.18)`
(dark) — drop the inner shadow toward 0 in light. **Discipline:** at most two elevation steps visible
in any one viewport region; glow (`drop-shadow`/`box-shadow` with a `*-glow` token) is reserved for
*active* phase elements and the live indicator only — never decorative.

### 2.5 Typography

- **UI face:** `Inter`, fallback `ui-sans-serif,-apple-system,"Segoe UI",Roboto,Helvetica,Arial,
  sans-serif`. Self-host Inter (no CDN); fallback must render acceptably for offline screenshots.
- **Numeral / mono face:** `IBM Plex Mono`, fallback `ui-monospace,"SF Mono","JetBrains Mono",
  Menlo,Consolas,monospace`. **All numerics use mono + `font-variant-numeric:tabular-nums` +
  `letter-spacing:-.01em`** (class `.num`). This is non-negotiable: every %, byte count, rate, ETA,
  timestamp, port, worker count.

Type scale (px / weight / tracking). Tabular figures everywhere a number appears.

| Token | Size | Weight | Tracking | Use |
|---|---|---|---|---|
| `display` | 78 (mobile 64) | 600 | -.05em | the flow-gauge combined % (metal-gradient text) |
| `read-xl` | 34 | 600 | -.04em | account instrument readouts |
| `read-l` | 22 | 600 | -.02em | mini-gauge area / section numerals |
| `h-sec` | 11 | 600 | .20em / UPPERCASE | section eyebrow labels (mono) |
| `title` | 16 | 600 | -.01em | account name, panel titles |
| `name` | 14–15 | 500–600 | -.01em | transfer names |
| `body` | 14 | 400 | 0 | default text |
| `meta` | 11–12.5 | 400 | 0–.06em | mono metadata, log lines, hints |
| `label` | 9.5–10.5 | 600 | .06–.16em / UPPERCASE | badges, instrument labels (mono) |

### 2.6 Spacing, radius

- **Spacing:** strict 4px base; the rhythm used is `4, 6, 8, 10, 13, 14, 16, 18, 22, 26, 42` — page
  section gap `42px`, card padding `24–28px`, control height `40px`, row padding `14–18px`, badge
  padding `3px 7–9px`. Don't introduce off-grid values.
- **Radius:** `--r-s 8px` (controls, badges→`5–6px`), `--r-m 12px` (cards, slots, wells),
  `--r-l 16px` (master panel, log console). Pills/dots fully rounded (`999px`/`50%`).

---

## 3. The fused two-phase hero — the flow-gauge (signature component)

**Concept:** ONE circular instrument. A 270° arc (15px stroke, `r=84` on a 200-unit viewBox), SVG
rotated 135° so the gap is at the bottom and the sweep starts bottom-left. The arc is read as the
transfer's journey:

```
        ┌── handoff node (12 o'clock) = 50% combined ──┐
   put.io fetch leg                              local download leg
   (steel-blue, first 135°)                      (chartreuse, second 135°)
        └──────── combined % readout in center ────────┘
                  comet head rides the ACTIVE leg's leading edge
```

This is the fusion: the **dial + center %** delivers tarmac's glance; the **two-colored arc split at
the handoff node + the traveling comet** delivers relay's explicit put.io→local flow. It must read as
one instrument — the comet is the relay "pour/handoff" expressed radially, not a second widget.

### 3.1 Geometry & data mapping (exact)

- Full 270° arc length = `2·π·84·0.75 = 395.8` units (the visible track `stroke-dasharray:"395.8 527.8"`).
- Each half-leg spans 50% of combined → `197.9` units.
- **put.io leg** (`--putio` gradient `fgP1`): length = `min(putio_percent_done, 100)/100 · 197.9`,
  starting at the arc origin. Drawn full whenever the fetch is done.
- **local leg** (`--local` gradient `fgP2`): length = `local_fraction · 197.9` where
  `local_fraction = (percent_done·2 − 1)` clamped to `[0,1]` (i.e. how far past 50% combined we are);
  `stroke-dashoffset:-197.9` so it begins at the handoff node.
  - Practical: drive `local_fraction` from the contract's `local_phase` (`downloaded/total`) when
    present; cross-check that `putio/200 + local·0.5 == percent_done` per `internal/transferprog`.
- **Center readout** = `round(percent_done·100)` with a superscript `%`, metal-gradient text. Below
  it: the combined rate `↓ <human(putio_phase.rate_download + local_phase.rate_download)>`.
- **handoff node**: a 2.5px `--ink-4` tick at 12 o'clock — always present, marks the 50% boundary.
- **comet head**: an absolutely-positioned 16px disc at the active leg's leading edge (position
  computed from the active fraction along the 270° arc), `--local` with `--local-glow` bloom when the
  local leg is active, `--putio`+`--putio-glow` when the put.io leg is active.
- **fgKeys / legend**: two-cell readout under the gauge (relay's explicit handoff legend). The
  *inactive* phase cell is muted to 50% opacity. put.io cell: `<putio_percent_done>% · <downloaded>`;
  local cell: `<completed_files>/<total_files> files · ETA <human(local eta_seconds)>` +
  `<downloaded> landed`.

### 3.2 Every lifecycle state (drive off `lifecycle_state` + `local_phase` presence + active rate)

`lifecycle_state` ∈ `None|Initial|Downloading|Completed|Failed|Cancelled|Processed`. Derived display
state and the **human phase label** (client-side; see §9):

| Display state | Condition | put.io leg | local leg | comet | center | legend | chip text |
|---|---|---|---|---|---|---|---|
| **queued** | `Initial`/`None`, or put.io status `IN_QUEUE` and `putio_percent_done==0` | empty | empty | hidden | `0%`, dimmed (`--ink-3`) | both muted, "waiting for put.io" | `queued` |
| **put.io-fetching** | `local_phase==null` (no ctx) and put.io fetch < 100% | fills steel; dot pulses | empty | on put.io leg edge, steel bloom, pulsing | combined % (= putio/200) | put.io lit, local muted "—" | `▸ fetching at put.io` |
| **local-downloading** | `Downloading` with `local_phase!=null` and local fraction <1 | full, **muted to 50%** | fills chartreuse; dot pulses | on local leg edge, chartreuse bloom, pulsing | combined % | put.io muted "100% · handed off", local lit | `▸ local download` |
| **completed** | `Completed`/`Processed` (or no-ctx `COMPLETED`/`SEEDING`) → `percent_done==1.0` | full, recolor `--green`, muted | full, recolor `--green` | hidden | `100%` in `--green` | both muted, "source cleaned" | `completed` |
| **failed / permanent** | `permanent==true` (`error==true`) | full | frozen at last fraction; both legs recolor `--red` | hidden | frozen %, `--red-ink` | error string + `permanent` chip | `failed` |

**Transient failure** (`Failed` but `permanent==false`, `error==false`): render exactly as its
underlying phase (still mid-progress, no red) — matches the RPC path so dashboard and *arr never
disagree (contract "Error precedence"). Only `permanent:true` turns the instrument red.

### 3.3 Fleet mini-gauge (the same instrument, compact)

Each non-focal transfer row carries a **52px mini flow-gauge** (5px stroke, `r=24`, same 270°
rotation, same put.io→local arc split, combined % centered) + two thin phase bars (5px, hard center
seam; put.io left with the finished leg muted, chartreuse local right) + a status dot + right-aligned
rate/ETA. Same color + state rules as §3.2 at small scale. Failed row gets a `--red-bg` wash, red
arcs, and a full-width `permanent` chip + error string beneath.

The headline (focal) transfer is the most-active one (prefer a `local-downloading`, else
`put.io-fetching`, else most-recent). All transfers including the focal also appear in the fleet for a
complete list; the focal is the enlarged headline, not removed from the deck.

---

## 4. Other components

### 4.1 Account / quota cluster
Instrument cluster, not a donut. `--surf-0` inset tiles:
- **active transfers** = count of non-terminal `lifecycle_state` (derive); sub-line "N queued · M stalled."
- **aggregate ↓** = `read-xl` mono, sum of all `rate_download` across phases (derive); sub "put.io X · local Y."
- **put.io disk** = a **ticked horizontal meter** (10% etched ticks), fill width = `used_percent`,
  fill `--local` gradient. Legend "used `<used>` · `<used_percent>%`" / "free `<avail>` of `<size>`."
  When `over_quota` (`used_percent ≥ 95`): fill recolors `--red`, a danger tick at 95% becomes solid,
  and a `over quota` `--red` badge appears by the disk label. `days_until_files_deletion>0` shows a
  "files purge in N days" `--amber` note.
- account name from `username`; avatar = first initial on a put.io→local gradient; `unlimited`/plan
  pill (plan label is decorative here — contract omits plan; show `put.io account` if unknown).

### 4.2 Log line + level chip
Grid: `[level color-bar | timestamp | LEVEL | component | message+fields]`.
- **level color-bar**: 5px left bar tinted by level — `debug`→transparent, `info`→`--putio`,
  `warn`→`--amber`, `error`/`fatal`→`--red`. Quiet per-line level signal.
- **LEVEL chip** text colored to match; **component** in `--local`; **message** `--ink-2` with the
  zerolog `message` in `--ink` and trailing `fields` rendered `k=v` in `--ink-4`, single-line
  ellipsis.
- Header carries level **filter chips** (`debug/info/warn/error`, `.on` = `--surf-4`) — client-side
  view filter over the received stream (the live `log_level` PUT changes what the server *sends*; the
  chips narrow what's *shown*). An `autoscroll` indicator with a `--green` live dot.
- Map from `<LogEvent>`: `time`→timestamp (HH:MM:SS), `level`, `component`, `message`, `fields`.
- Cap the in-memory list (Pi memory); snapshot seeds ~500.

### 4.3 Settings field (live / restart-required / locked variants)
One `slot` per `<SettingEntry>`. Header: `key` (mono) + right-aligned **badges**:
- `source` badge (`default/file/flag/env`) — neutral.
- `live` badge (`--green`) iff `live==true`.
- `restart` badge (`--amber`) iff `restart_required==true`.
- `locked` badge (neutral-strong) iff `locked==true`.

Controls by key (no browser-default form chrome):
- `log_level` → **segmented control** (`debug…none`); active cell `--local` fill, `--local-ink` label.
- `workers` → **± stepper**, mono value, min 1.
- `target` / `folder` / `listen` / `dashboard_listen` → text-style `inp`; **locked** variant is
  read-only, dimmed (`--surf-3` bg), lock glyph, no caret.
- `token` → **masked** `•••••••••••••••••••• set` + a "Replace token…" ghost button that opens a
  replace flow. The value is **never** rendered or read back (`token.value` is always null,
  `is_set` only). When `is_set==false`, show `not set` + a "Set token…" button.
- Hint line (`--ink-4` mono) per field explains live vs restart vs locked.

**Save bar:** a dirty indicator (`--amber` "● N pending"), Discard (ghost), Save (primary `--local`).
On PUT response, badge `applied` keys momentarily `--green`, `persisted`+`restart_required` keys get a
persistent "restart required" note until the daemon restarts. Locked-key edits are blocked client-side
(field is read-only) — never sent (server would 400 with `key_locked`).

### 4.4 Empty / error / connection-lost states
- **Empty transfers:** the flow-gauge bay shows an idle dial (track only, centered `—`, "no active
  transfers") and the deck shows "Nothing in the queue. *arr will add transfers here."
- **Empty logs:** "Waiting for log events…" with the live dot.
- **REST/upstream error** (e.g. `GET /api/account` 500, `code:upstream_error`): the affected tile
  shows an inline `--red` strip "couldn't reach put.io — retrying" with the `error.message`; the rest
  of the deck keeps rendering from the last snapshot.
- **Connection-lost (SSE drop):** the `STREAM LIVE` pill swaps to a static `--amber` "RECONNECTING"
  pill (animation removed), and a thin `--amber` ribbon under the rail reads "stream lost — showing
  last known state · reconnecting." On reconnect the server re-sends `snapshot` (no replay), the
  ribbon clears, the pill returns to green. Stale values keep their last paint (do not blank).
- **Validation error** on a settings PUT (`400 validation_failed`): the offending field (by
  `error.field`) gets a `--red` ring + the `error.message` inline; no partial apply.

---

## 5. Theme switch mechanism

- **Resolution order (before first paint):** an inline `<head>` script reads `?theme=` (`light`/
  `dark` win), else `window.matchMedia('(prefers-color-scheme: light)')`, else **dark**. It sets
  `document.documentElement.setAttribute('data-theme', t)` synchronously so there is no flash.
- **Persistence (build adds):** also read/write `localStorage['pldr-theme']`; precedence becomes
  `?theme=` > `localStorage` > `prefers-color-scheme` > dark. (The mockup omits localStorage so the
  `?theme=` URL screenshots deterministically — keep `?theme=` authoritative for captures.)
- **Toggle UI:** a pill segmented `☀ / ☾` control in the top rail, right side; the active half is
  filled (`--surf-1` on `--surf-3`). Clicking sets the attribute + persists + updates the URL param.
- **Transition:** `body` transitions `background` and `color` `.3s ease`; component surfaces inherit
  via tokens (no per-component theme branches — only the two `:root` token blocks differ).
- All color references in component CSS use tokens **only** — no hard-coded hex outside the two
  `:root` blocks — so a third theme would be one more token block.

---

## 6. Motion spec

Every animation is bound to a data fact (motion presence == that phase is moving / SSE connected).
All are GPU-cheap (opacity / transform / background-position / `stroke-dashoffset` / width). Pi-class
target: no layout-thrashing properties animate.

| Animation | Trigger | Property | Duration / easing | GPU cost |
|---|---|---|---|---|
| Comet pulse | active leg exists (rate>0) | `opacity`+`transform:scale` | 1.7s ease-in-out loop | cheap |
| Rate-arrow nudge | combined rate > 0 | `transform:translateY` | 1.4s ease-in-out loop | cheap |
| State dot pulse | `put.io-fetching` / `local-downloading` only | `opacity` | 1.6s ease-in-out loop | cheap |
| Live pill pulse | SSE connected | `opacity` | 1.8s ease-in-out loop | cheap |
| Log autoscroll "fresh" flash | new log line arrives | `background-color` fade | 1.0s ease-out once | cheap |
| Arc advance | per ~1s `transfers` SSE tick | `stroke-dashoffset`/`stroke-dasharray` | 0.6s `cubic-bezier(.2,.7,.2,1)` | **moderate** (SVG stroke; the one to watch on Pi — keep to ≤ a handful of visible gauges; mini-gauges in the fleet animate too, so cap simultaneous animated gauges or step them) |
| Bar / disk fill | value change | `width` | 0.6–0.7s `cubic-bezier(.2,.7,.2,1)` | cheap |
| Count-up on big % | value change (optional) | JS number tween | 0.4s, snap if delta>5 | cheap |
| Row hover | hover | `background`,`border-color`,`transform:translate` | .15s | cheap |
| Theme cross-fade | theme change | `background`,`color` on body | .3s ease | cheap |

**Stall semantics:** when a transfer's active-phase `rate_download==0`, drop the `.active` class so
the comet + dot animations stop — a frozen instrument == no throughput, which is information.

**`prefers-reduced-motion: reduce`:** disable ALL of the above (`*{animation:none!important;
transition:none!important}`). Every state remains fully legible statically — the arc split, comet
position, colors, and numerals all encode the state with zero motion. Motion is additive only.

---

## 7. Responsive behavior

- **Desktop (≥1040px):** master panel is `400px` gauge bay + fluid account cluster, 2-col. Fleet
  rows show mini-gauge + name/status + two phase bars + rate. Settings rack 2-col. Log full grid.
- **Tablet (≤1040px):** master panel stacks (gauge bay on top, full-width; cluster below). Fleet
  rows drop the two phase bars (mini-gauge + name + rate kept). Settings rack 1-col.
- **Mobile (≤560px):** rail nav + clock hide (brand, live pill, theme toggle stay); account cluster
  1-col; gauge `display`→64px; log grid drops the LEVEL column (color-bar + component carry level);
  fleet rows are mini-gauge + name (rate hidden, available on the row's detail). Master gauge stays
  the hero. Min tap target 40px (control height).
- Content reflows by container width (the mockup uses width media queries; the build may use
  container queries — same breakpoints).

---

## 8. Accessibility floor

Contrast ratios are measured (sRGB, WCAG 2.1) for both themes:

**Body / secondary text — clears AA (≥4.5), most AAA:**

| Pair | Dark | Light |
|---|---|---|
| `--ink` on `--surf-1` | 15.8 | 18.2 |
| `--ink-2` on `--surf-1` | 9.0 | 9.6 |
| `--ink-3` (labels, ≥12px) on `--surf-1` | 4.4 (AA-large/UI) | 5.4 (AA) |
| `--putio` text on `--surf-1` | 8.0 | 5.1 |
| `--red-ink`/`--red` text on `--surf-1` | 9.0 | 5.4 |
| `--amber` text on `--surf-1` | 10.7 | 4.7 |
| `--local-ink` on `--local` (seg/button label) | 14.9 | 4.1 (AA-large — labels are ≥11px bold/uppercase, OK) |

**Constraint the build MUST honor:** in **light** theme, `--local` (4.1) and `--green` (4.0) clear
the 3:1 threshold for **UI components, icons, fills, and large text** but sit just under 4.5 for small
body copy. Therefore **never set small body/meta text in `--local` or `--green` on a light surface** —
use `--ink`/`--ink-2` for such text and reserve `--local`/`--green` for fills, dots, arcs, the
completed-% (large), and component boundaries. (`dark` theme `--local` as the log `component` color is
fine: it sits on `--surf-0` at high contrast.) `--ink-3` is the floor for any text and only at ≥12px.

- **Focus:** visible `outline:2px solid var(--local); outline-offset:2px` on every interactive
  element (nav links, theme toggle, segmented buttons, stepper, ghost/primary buttons, replace-token).
  Never remove outline without an equal-or-better replacement.
- **Keyboard:** all controls reachable in DOM order; segmented control is a radio-group (arrow keys
  move, Enter/Space select); stepper buttons are buttons; theme toggle is a 2-option group.
- **Semantics:** status dots/levels carry text too (the status line and LEVEL chip), so color is
  never the sole signal — phase is also stated in the chip/legend text and the human label.
- **Reduced motion:** §6 fallback; nothing depends on animation for comprehension.
- **Targets:** interactive controls ≥40px in the primary axis (control height); badges are
  non-interactive.

---

## 9. Contract notes & client-side derivations (no amendment required)

The contract carries everything the UI renders. Two derivations the frontend owns (named so they're
not re-decided):

1. **Aggregate throughput & active-count** (rail pill, account cluster, section meta): sum
   `putio_phase.rate_download + local_phase.rate_download` across transfers; active-count = transfers
   whose `lifecycle_state` is non-terminal (`Initial|Downloading`). Pure client-side over the
   `transfers` SSE list.
2. **Human phase label** (chip text + status line, e.g. "fetching at put.io" / "local download" /
   "completed" / "failed"): derived from `lifecycle_state` + whether `local_phase==null` + which
   phase's `rate_download` is non-zero, per the table in §3.2.

**Flagged nice-to-have (not blocking):** a server-supplied `state_label` on `<Transfer>` would let the
dashboard and any future client share one source of truth instead of the §9.2 mapping. Defer unless
the orchestrator wants it; the mapping is small and lives in one frontend function.

No field is missing for any surface in this spec.

---

## 10. Screenshot / review checklist (for the ui-reviewer)

Capture `mockups/relaydeck/index.html?theme=dark` and `?theme=light`, full-page desktop (1320px) +
mobile (390px). Grade against:
- flow-gauge reads as ONE instrument (dial + arc-split + comet + handoff node), all five lifecycle
  states correct, combined % matches the arc split;
- accent is chartreuse, phase colors steel/chartreuse legible in BOTH themes, amber only on
  restart/caution, light theme keeps the instrument (not editorial) identity;
- tabular mono numerals everywhere; type scale + 4px spacing rhythm respected;
- log level color-bars + chips; settings live/restart/locked badges + masked token (never a value);
- empty/error/connection-lost states present; focus rings visible; contrast holds per §8.
