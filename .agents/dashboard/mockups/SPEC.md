# plundrio dashboard — Design Spec (Pass 2)

Direction: **`relaydeck`** — the operator's combo of `relay` + `tarmac`, shipping with a **selectable
theme system: three palettes (`tide`/`ember`/`ion`) × light/dark**. This is the contract the
frontend-builder builds to. Every render field maps to `plan/contract.md`; where this spec says
"derive," the derivation is named so no design decision is left to implementation. The reference
mockup is `mockups/relaydeck/index.html` (offline; `?palette=tide|ember|ion&theme=light|dark`).

**The mental model in one sentence:** plundrio is an *instrument console* — graphite, precise,
cockpit-legible — whose headline transfer is a single **flow-gauge** that is simultaneously a
glanceable combined-% dial (tarmac) and an explicit put.io→local journey (relay); everything else
(fleet, account, logs, settings) is supporting instrumentation in the same material, **recolorable
across three committed palettes without changing any layout.**

---

## 1. Identity & palette system

- **Base aesthetic (constant across all palettes):** instrument console (tarmac DNA). Graphite
  surfaces, hairline borders, layered soft shadows, tabular mono numerals, a radial hero. Light mode
  is the **same instrument in daylight** (graphite-on-pale), *not* an editorial/paper look.
- **Three shipping palettes (user-selectable; default `ion`).** Each is a committed, harmonious system
  in both modes. They are recolors only — identical IA and flow-gauge geometry.

  | Palette | Mood | put.io (phase 1) | local (phase 2 + accent) |
  |---|---|---|---|
  | `tide` | cool marine instrument | azure | aqua-teal |
  | `ember` | refined warm graphite (cool→warm handoff reinforces the journey) | periwinkle | coral |
  | `ion` *(default)* | restrained near-mono graphite + one bold accent | muted steel (recedes) | electric indigo |

- **Phase color contract (holds in every palette × mode):**
  - **`--putio`** = put.io fetch, the 0–50% leg / "in transit, not yours yet."
  - **`--local`** = local download, the 50–100% leg / "landing on disk, becoming yours" — and the
    brand accent, so the accent always *means* "local/yours" whatever palette is active.
  - The two phase hues are kept clearly distinct within each palette (verified legible in §8).
  - **State colors are never used for a phase, and `--amber` is held distinct from both phase hues in
    every palette:** `--amber` = restart-required/caution, `--green` = live & completed, `--red` =
    permanent failure.
  - In light mode, phase hues are *darkened* per palette to hold contrast on pale surfaces; the hue
    identity is preserved (see §8 for the per-palette caveats on `--local`/`--green` as small text).

---

## 2. Design tokens — three shipping palettes × two modes

**Decision (locked):** the dashboard ships **all three palettes as user-selectable themes** —
`tide`, `ember`, `ion` — each a complete system in both **light** and **dark** mode. There is no
single "the" palette; the user picks. **Default = `ion` + `dark`.** The layout and the fused
flow-gauge are identical across every palette; only the color system changes.

The token values below are **locked and authoritative** — they are copied byte-for-byte from the
committed mockup CSS (`mockups/relaydeck/index.html`), which is the single source of truth the
frontend-builder pulls from. Do not re-derive or "improve" them; if a value must change, change the
mockup and re-sync this table.

Implement as CSS custom properties keyed on
`:root[data-palette="tide|ember|ion"][data-theme="dark|light"]` (six blocks). The no-JS CSS fallback
`:root:not([data-palette])` aliases **ion · dark**. Structural tokens are palette-independent
(`--sans`, `--mono`, `--r-s 8`, `--r-m 12`, `--r-l 16`).

Every palette obeys the same role contract: `--putio` = phase 1 (put.io fetch), `--local` = phase 2
**and** the brand accent (local download), `--amber` = semantic state (restart/caution) and is held
distinct from both phase hues, `--green` = live/completed, `--red` = permanent failure. The hero dial
radial glow is **removed**; arc/comet glows are a crisp edge sheen only (see §3).

### 2.1 Palette: `tide` — cool marine instrument (slate-teal · azure put.io · aqua-teal local)

| Token | Dark | Light |
|---|---|---|
| `--bg` | `#06090c` | `#e6edf1` |
| `--bg-grad-a` / `--bg-grad-b` | `rgba(78,163,245,.05)` / `rgba(47,217,192,.045)` | `rgba(31,116,196,.07)` / `rgba(15,143,126,.06)` |
| `--surf-0` | `#0a0f15` | `#edf2f5` |
| `--surf-1` | `#0e151c` | `#ffffff` |
| `--surf-2` | `#121b24` | `#f3f7f9` |
| `--surf-3` | `#19242f` | `#e2ebef` |
| `--surf-4` | `#243646` | `#d2e0e6` |
| `--hair` / `--hair-2` | `rgba(150,195,215,.08)` / `rgba(150,195,215,.14)` | `rgba(20,55,75,.1)` / `rgba(20,55,75,.16)` |
| `--ink` | `#e9f1f5` | `#0f1820` |
| `--ink-2` | `#a6bccb` | `#39505e` |
| `--ink-3` | `#6b8294` | `#5c7585` |
| `--ink-4` | `#455866` | `#92a8b4` |
| `--putio` / `--putio-2` | `#4ea3f5` / `#1a5489` | `#1f74c4` / `#9ec9ef` |
| `--putio-glow` | `rgba(78,163,245,.42)` | `rgba(31,116,196,.26)` |
| `--local` / `--local-2` | `#2fd9c0` / `#147c70` | `#0f8f7e` / `#74dccb` |
| `--local-ink` | `#04201c` | `#ffffff` |
| `--local-glow` | `rgba(47,217,192,.45)` | `rgba(15,143,126,.28)` |
| `--amber` | `#f4b740` | `#946312` |
| `--amber-bg` / `--amber-br` | `rgba(244,183,64,.1)` / `rgba(244,183,64,.24)` | `rgba(148,99,18,.1)` / `rgba(148,99,18,.26)` |
| `--green` / `--green-glow` | `#46d18a` / `rgba(70,209,138,.5)` | `#2f8f5e` / `rgba(47,143,94,.3)` |
| `--red` | `#ff6f6f` | `#c0392f` |
| `--red-ink` | `#ff9d9d` | `#a02a22` |
| `--red-bg` / `--red-deep` | `rgba(255,111,111,.1)` / `#481f24` | `rgba(192,57,47,.08)` / `#f0d6d3` |
| `--track` | `#19242f` | `#d6e0e5` |
| `--sh-card` | `0 24px 60px -34px rgba(0,0,0,.85)` | `0 18px 44px -30px rgba(15,40,60,.38)` |
| `--sh-pop` | `0 3px 12px rgba(0,0,0,.4)` | `0 3px 12px rgba(15,40,60,.15)` |
| `--metal-a` / `--metal-b` | `#ffffff` / `#bcccd6` | `#11202c` / `#3f5566` |

### 2.2 Palette: `ember` — refined warm graphite (warm charcoal · periwinkle put.io · coral local)

| Token | Dark | Light |
|---|---|---|
| `--bg` | `#0c0a08` | `#efe9e1` |
| `--bg-grad-a` / `--bg-grad-b` | `rgba(138,160,216,.045)` / `rgba(255,138,107,.05)` | `rgba(77,95,168,.06)` / `rgba(198,74,46,.06)` |
| `--surf-0` | `#120f0c` | `#f4efe8` |
| `--surf-1` | `#16130f` | `#fffdf9` |
| `--surf-2` | `#1c1813` | `#f7f2ea` |
| `--surf-3` | `#241f18` | `#ece4d8` |
| `--surf-4` | `#352c22` | `#ddd2c2` |
| `--hair` / `--hair-2` | `rgba(220,200,175,.08)` / `rgba(220,200,175,.14)` | `rgba(70,55,40,.1)` / `rgba(70,55,40,.16)` |
| `--ink` | `#f3ece2` | `#1c1611` |
| `--ink-2` | `#c4b6a6` | `#4a4034` |
| `--ink-3` | `#8c8073` | `#6f6557` |
| `--ink-4` | `#5d5346` | `#a39786` |
| `--putio` / `--putio-2` | `#8aa0d8` / `#3a4a78` | `#4d5fa8` / `#aab6e0` |
| `--putio-glow` | `rgba(138,160,216,.4)` | `rgba(77,95,168,.26)` |
| `--local` / `--local-2` | `#ff8a6b` / `#a8482f` | `#c64a2e` / `#f0a890` |
| `--local-ink` | `#2a0f08` | `#ffffff` |
| `--local-glow` | `rgba(255,138,107,.45)` | `rgba(198,74,46,.28)` |
| `--amber` | `#e8c14e` | `#8a6a14` |
| `--amber-bg` / `--amber-br` | `rgba(232,193,78,.1)` / `rgba(232,193,78,.24)` | `rgba(138,106,20,.1)` / `rgba(138,106,20,.26)` |
| `--green` / `--green-glow` | `#7bc98a` / `rgba(123,201,138,.45)` | `#3a8a52` / `rgba(58,138,82,.3)` |
| `--red` | `#f4716a` | `#bf3b2c` |
| `--red-ink` | `#ffa097` | `#9e2e20` |
| `--red-bg` / `--red-deep` | `rgba(244,113,106,.1)` / `#4a201c` | `rgba(191,59,44,.08)` / `#f0d5d0` |
| `--track` | `#241f18` | `#e2d8ca` |
| `--sh-card` | `0 24px 60px -34px rgba(0,0,0,.85)` | `0 18px 44px -30px rgba(60,40,20,.34)` |
| `--sh-pop` | `0 3px 12px rgba(0,0,0,.42)` | `0 3px 12px rgba(60,40,20,.14)` |
| `--metal-a` / `--metal-b` | `#fff8ef` / `#d4c4b2` | `#241a12` / `#5a4a3a` |

### 2.3 Palette: `ion` — restrained near-mono graphite + electric indigo (DEFAULT)

(muted steel put.io recedes; electric indigo is the single bold local/accent)

| Token | Dark | Light |
|---|---|---|
| `--bg` | `#08090b` | `#e9eaee` |
| `--bg-grad-a` / `--bg-grad-b` | `rgba(139,151,173,.035)` / `rgba(124,108,255,.06)` | `rgba(90,102,120,.05)` / `rgba(91,78,224,.06)` |
| `--surf-0` | `#0c0e11` | `#eeeff2` |
| `--surf-1` | `#111317` | `#ffffff` |
| `--surf-2` | `#16191e` | `#f4f5f7` |
| `--surf-3` | `#1d2128` | `#e6e8ec` |
| `--surf-4` | `#2a3038` | `#d6d9df` |
| `--hair` / `--hair-2` | `rgba(190,200,220,.08)` / `rgba(190,200,220,.14)` | `rgba(40,45,60,.1)` / `rgba(40,45,60,.16)` |
| `--ink` | `#edeef2` | `#13151b` |
| `--ink-2` | `#aeb4c0` | `#3e434f` |
| `--ink-3` | `#737a88` | `#5e6573` |
| `--ink-4` | `#4b515c` | `#949aa6` |
| `--putio` / `--putio-2` | `#8b97ad` / `#454d5c` | `#5a6678` / `#aeb6c4` |
| `--putio-glow` | `rgba(139,151,173,.34)` | `rgba(90,102,120,.24)` |
| `--local` / `--local-2` | `#8b7cff` / `#4a3da8` | `#5b4ee0` / `#a99fef` |
| `--local-ink` | `#0b0820` | `#ffffff` |
| `--local-glow` | `rgba(124,108,255,.5)` | `rgba(91,78,224,.28)` |
| `--amber` | `#f0b53e` | `#8a6614` |
| `--amber-bg` / `--amber-br` | `rgba(240,181,62,.1)` / `rgba(240,181,62,.24)` | `rgba(138,102,20,.1)` / `rgba(138,102,20,.26)` |
| `--green` / `--green-glow` | `#4cc98a` / `rgba(76,201,138,.48)` | `#2f8f5e` / `rgba(47,143,94,.3)` |
| `--red` | `#ff6f7a` | `#c0392f` |
| `--red-ink` | `#ff9aa2` | `#a02a22` |
| `--red-bg` / `--red-deep` | `rgba(255,111,122,.1)` / `#46202a` | `rgba(192,57,47,.08)` / `#f0d6d3` |
| `--track` | `#1d2128` | `#dde0e5` |
| `--sh-card` | `0 24px 60px -34px rgba(0,0,0,.85)` | `0 18px 44px -30px rgba(25,30,50,.36)` |
| `--sh-pop` | `0 3px 12px rgba(0,0,0,.42)` | `0 3px 12px rgba(25,30,50,.15)` |
| `--metal-a` / `--metal-b` | `#ffffff` / `#c2c6d2` | `#161922` / `#454c5c` |

### 2.4 Token roles (palette-independent)

These roles hold for all three palettes; only the hex differs per §2.1–2.3.

| Token | Role | Min text size for AA (see §8 for per-palette caveats) |
|---|---|---|
| `--ink` | primary text, numerals | any |
| `--ink-2` | secondary text | any |
| `--ink-3` | labels, tertiary, log timestamps | ≥12px (some palettes AA-large only — §8) |
| `--ink-4` | faint / disabled / version / decorative | decorative + ≥12px non-essential |
| `--putio` / `--putio-2` | put.io fetch arc/bar/dot, info-level log; `-2` = gradient deep-stop | text ≥12px |
| `--local` / `--local-2` | local arc/bar/dot, brand accent, primary button bg, segmented-active bg; `-2` = gradient deep-stop | **fills/large/UI only in some light palettes — §8** |
| `--local-ink` | text/label on a `--local` fill (segmented-active label, primary button) | label must be large/bold-treated — §8 |
| `--amber` / `--amber-bg` / `--amber-br` | restart-required badge, dirty-state, warn-level log | text ≥12px |
| `--green` / `--green-glow` | live dot, completed state | **fills/large/UI only in light — §8** |
| `--red` / `--red-ink` / `--red-bg` / `--red-deep` | failure arc/bar/dot, error log, danger tick, failed-row wash; `-ink` = failure body text | `--red-ink` for small text; `--red` for fills/large |
| `--track` | gauge & bar track well | n/a (surface) |
| `--*-glow` | crisp edge sheen on **active** phase elements + live indicator only — never decorative | n/a |
| `--sh-card` / `--sh-pop` | panel / popover elevation | n/a |
| `--metal-a` / `--metal-b` | big-numeral text gradient (the combined %) | display size only |

Inset wells use `inset 0 0 0 1px var(--hair)` plus, where recessed, `inset 0 2px 6px rgba(0,0,0,.18)`
(dark) — drop the inner shadow toward 0 in light. **Discipline:** at most two elevation steps visible
in any one viewport region; glow is reserved for *active* phase elements and the live indicator only.

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
   (--putio, first 135°)                         (--local, second 135°)
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
| **put.io-fetching** | `local_phase==null` (no ctx) and put.io fetch < 100% | fills `--putio`; dot pulses | empty | on put.io leg edge, `--putio` edge sheen, pulsing | combined % (= putio/200) | put.io lit, local muted "—" | `▸ fetching at put.io` |
| **local-downloading** | `Downloading` with `local_phase!=null` and local fraction <1 | full, **muted to 50%** | fills `--local`; dot pulses | on local leg edge, `--local` edge sheen, pulsing | combined % | put.io muted "100% · handed off", local lit | `▸ local download` |
| **completed** | `Completed`/`Processed` (or no-ctx `COMPLETED`/`SEEDING`) → `percent_done==1.0` | full, recolor `--green`, muted | full, recolor `--green` | hidden | `100%` in `--green` | both muted, "source cleaned" | `completed` |
| **failed / permanent** | `permanent==true` (`error==true`) | full | frozen at last fraction; both legs recolor `--red` | hidden | frozen %, `--red-ink` | error string + `permanent` chip | `failed` |

**Transient failure** (`Failed` but `permanent==false`, `error==false`): render exactly as its
underlying phase (still mid-progress, no red) — matches the RPC path so dashboard and *arr never
disagree (contract "Error precedence"). Only `permanent:true` turns the instrument red.

### 3.3 Fleet mini-gauge (the same instrument, compact)

Each non-focal transfer row carries a **52px mini flow-gauge** (5px stroke, `r=24`, same 270°
rotation, same put.io→local arc split, combined % centered) + two thin phase bars (5px, hard center
seam; `--putio` left with the finished leg muted, `--local` right) + a status dot + right-aligned
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

## 5. Theme system — palette × mode

The "theme" is two orthogonal axes: **palette** (`tide`/`ember`/`ion`) × **mode** (`light`/`dark`),
expressed as `data-palette` and `data-theme` on `<html>`. It is a **pure client-side browser
preference — it is NOT a daemon setting** and never appears in `/api/settings`, the SSE stream, or any
request to the daemon. (It lives only in `localStorage` + the URL params; the daemon neither stores
nor reads it.)

- **Resolution order (computed in an inline `<head>` script, before first paint, no flash):**
  - **mode:** `?theme=` (`light`/`dark`) → `localStorage['pldr-mode']` → `prefers-color-scheme` →
    **`dark`**.
  - **palette:** `?palette=` (`tide`/`ember`/`ion`) → `localStorage['pldr-palette']` → **`ion`**.
  - The script sets both `data-*` attributes synchronously. `?theme=` / `?palette=` are
    **authoritative** — they let every palette×mode combo be screenshotted from the one file.
  - No-JS fallback: the CSS `:root:not([data-palette])` block aliases **ion · dark**, so a
    script-disabled load still renders the default theme.
- **Persistence:** on any picker click, write `localStorage['pldr-mode']` / `localStorage['pldr-palette']`
  and update the `data-*` attribute live (no reload needed in the shipping build; the mockup uses
  param links so captures stay deterministic). The two keys are independent — changing mode keeps the
  palette and vice-versa.
- **Picker affordances + placement (top rail, right side, before the live pill or grouped at the
  far right):**
  - **Palette picker:** a pill group of **three swatch dots** (tide=teal→aqua, ember=periwinkle→coral,
    ion=steel→indigo gradients), the active one ringed with `0 0 0 3.5px currentColor`. `role="group"
    aria-label="Color theme"`; each swatch is a link/button with `aria-label="<Palette> theme"` and a
    `title`. Keyboard-reachable; visible focus ring.
  - **Mode toggle:** the existing pill segmented `☀ / ☾`; active half filled (`--surf-1` on
    `--surf-3`). `role="group" aria-label="Light or dark"`.
  - Both are obvious and reachable; on mobile they remain visible (only the nav links + clock hide).
  - The picker may instead/also live in Settings as a "Appearance" row — but it is **not** a daemon
    setting and must be visually separated from the `/api/settings` fields (it has no save/restart
    semantics; it applies instantly and is per-browser).
- **Transition:** `body` transitions `background` and `color` `.3s ease`; component surfaces inherit
  via tokens. There are **no per-component theme branches** — only the six `:root[data-palette][data-theme]`
  token blocks differ, so adding a palette is one more pair of blocks.
- All color references in component CSS use tokens **only** — no hard-coded hex outside the palette
  token blocks (verified in the mockup). This is what makes palette×mode total.

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

Contrast must hold in **all three palettes × both modes**. The ratios below are measured (sRGB,
WCAG 2.1) against `--surf-1` (the default card surface) for every shipping palette. `--ink` and
`--ink-2` clear AAA everywhere and are omitted for brevity (all ≥8.4). The table flags everything that
is **not** plain AAA so the build knows where each token may and may not be used.

| Token on `--surf-1` | tide D | tide L | ember D | ember L | ion D | ion L |
|---|---|---|---|---|---|---|
| `--ink-3` (labels, ≥12px) | 4.59 AA | 4.84 AA | 4.81 AA | 5.62 AA | **4.31 AA-large/UI** | 5.86 AA |
| `--putio` (text ≥12px) | 6.90 | 4.82 | 7.14 | 5.88 | 6.31 | 5.82 |
| `--local` (text) | 10.33 | **4.00 AA-large/UI** | 8.02 | 4.68 AA | 5.69 AA | 5.82 AA |
| `--amber` (text ≥12px) | 10.23 | 5.18 | 10.73 | 4.98 | 10.08 | 5.26 |
| `--green` (text) | 9.41 | **4.03 AA-large/UI** | 9.31 | **4.19 AA-large/UI** | 8.88 | **4.03 AA-large/UI** |
| `--red` (text/fill) | 6.79 | 5.43 | 6.54 | 5.33 | 6.91 | 5.43 |
| `--red-ink` (failure body text) | 9.24 | 7.37 | 9.46 | 7.21 | 9.21 | 7.37 |
| `--local-ink` on `--local` (seg/btn label) | 9.61 | **4.00 AA-large** | 7.78 | 4.75 AA | 6.01 AA | 5.82 AA |

(D = dark mode, L = light mode. Plain numbers ≥4.5 clear AA for any text; values flagged
`AA-large/UI` clear only the 3:1 large-text / UI-component / graphics threshold.)

**Constraints the build MUST honor across every palette:**

1. **`--local` and `--green` as text are AA-large/UI only in light modes** (worst cases:
   tide-L `--local` 4.00, all-L `--green` ~4.0–4.2). Use them for **fills, arcs, dots, large/display
   numerals (e.g. the completed `100%`), and component boundaries** — **never for small body/meta
   text on a light surface.** Small text uses `--ink`/`--ink-2`/`--red-ink`. (In dark modes `--local`
   as text — e.g. the log `component` tag — clears AA comfortably.)
2. **The segmented-active label (`--local-ink` on a `--local` fill) is 4.00 in tide-light** — below
   AA for small text. The active-segment label is 11px uppercase bold; treat it as **large/bold UI
   text** (which is the 3:1 class) — acceptable per WCAG large-text, but the build must keep that
   label bold and ≥11px and must NOT shrink it. If a future change makes that label smaller/lighter,
   darken tide-light `--local` or lighten the label. ember-L (4.75) and ion-L (5.82) clear AA outright.
3. **`--ink-3` is the text floor and only at ≥12px.** In **ion-dark** it is 4.31 (AA-large/UI) — keep
   ion-dark `--ink-3` usage to labels/timestamps at ≥12px (it is); do not set ion-dark body copy in
   `--ink-3`. All other palette/mode `--ink-3` clear AA.
4. **`--ink-4` is decorative / ≥12px non-essential only** in every palette (version string, faint
   metadata) — never load-bearing text.

These hold by construction in the mockup; the reviewer can verify by sampling each palette×mode
screenshot.

- **Focus:** visible `outline:2px solid var(--local); outline-offset:2px` on every interactive
  element (nav links, palette swatches, mode toggle, segmented buttons, stepper, ghost/primary
  buttons, replace-token). Never remove outline without an equal-or-better replacement. (Because
  `--local` is the focus color and it shifts per palette, the focus ring is always on-brand and
  clears the 3:1 non-text contrast against the adjacent surface in every palette.)
- **Keyboard:** all controls reachable in DOM order; segmented control is a radio-group (arrow keys
  move, Enter/Space select); stepper buttons are buttons; mode toggle is a 2-option group; the
  palette picker is a 3-option group.
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

Capture `mockups/relaydeck/index.html` across **all six combos** — `?palette=tide|ember|ion` ×
`?theme=dark|light` — full-page desktop (1320px); add mobile (390px) for the default (`ion`+`dark`)
and at least one light palette. Grade against:
- flow-gauge reads as ONE instrument (dial + arc-split + comet + handoff node), all five lifecycle
  states correct, combined % matches the arc split — **the layout/hero is byte-identical across all
  three palettes** (only color changes);
- each palette is harmonious in both modes; `--putio` vs `--local` distinct and legible in every
  combo; `--amber` visibly distinct from both phase hues; light mode keeps the instrument (not
  editorial) identity;
- **no glow blob under the hero dial** (flat instrument bay; arc/comet are a crisp edge sheen, not a
  diffuse bloom);
- palette picker (3 swatches, active ringed) + mode toggle (☀/☾) both present, accessible, and
  marked active correctly; switching one axis preserves the other; default with no params is
  `ion`+`dark` (mode may follow `prefers-color-scheme` on first visit);
- tabular mono numerals everywhere; type scale + 4px spacing rhythm respected;
- log level color-bars + chips; settings live/restart/locked badges + masked token (never a value);
  the appearance picker is visibly separate from `/api/settings` fields (it has no save/restart);
- empty/error/connection-lost states present; focus rings visible in every palette; contrast holds
  per §8 (sample `--local`/`--green` are NOT used for small body text on light surfaces).
