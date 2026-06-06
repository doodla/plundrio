# Dashboard mockups — Pass 1 directions

Self-contained static pages for the plundrio web dashboard, each rendering all four core surfaces
(transfers with the two-phase progress hero across every lifecycle state, account/quota, live log
viewer, settings). No framework, no build, no network — seed values hand-placed so they screenshot
offline. All four surfaces are on one scrolling page per direction. The operator picks one; Pass 2
(the design spec) follows the pick.

---

## Round 1 — REJECTED (generic)

The operator reviewed `console`, `studio`, and `horizon` and rejected **all three as "generic"** —
competent but templatey: the same admin skeleton (top-nav/rail → account row → a flat list of equal
transfer rows, each a thin horizontal bar → dark log box → stacked settings form), varied only by the
progress-bar treatment. Default-admin-template feel. Kept on disk for reference / as the anti-pattern;
**not** candidates.

| Slug | Status | Why rejected |
|------|--------|--------------|
| `console` | REJECTED (generic) | Dense mono console; two-phase = one hard-seam track. Templatey list-of-rows chrome. |
| `studio`  | REJECTED (generic) | Light SaaS cards; two-phase = two stacked labelled bars. Stock card grid. |
| `horizon` | REJECTED (generic) | Warm-dark left-rail; two-phase = one color-shifting bar. Default mission-control admin chrome. |

---

## Round 2 — new directions (candidates)

Three fresh identities. Each breaks the round-1 failure at the **information-architecture and
shape-language** level, not by recolor: a different layout rhythm, a different hero *geometry*, and a
committed palette. All clear the craft bar (real type scale, tabular numerals, committed accent,
material/depth, custom controls, the two-phase progress as a designed centerpiece — never a thin bar).

| Slug | Identity (one line) | Hero geometry | Palette |
|------|--------------------|---------------|---------|
| `relay`  | One transfer is the headline — a tall vertical **pour** vessel; put.io fetches into the top, work falls through a handoff throat to land on disk; the rest is a quiet fleet around it. | Vertical pour vessel + drip-through-throat handoff | Cool graphite + **chartreuse** accent (dark) |
| `tarmac` | An **instrument cluster**, not cards — the two-phase progress is a 270° gauge; needle sweeps the steel put.io arc then the amber local arc; 12-o'clock is the handoff. | 270° radial gauge (master + mini-dials) | Graphite + **sodium-amber** accent (dark) |
| `ledger` | A printed **manifest** set in type — serif masthead, numbered running-heads, huge tabular numerals; two-phase = a relay **baton** with a capsule riding the put.io→local seam. | Horizontal relay baton + traveling capsule | Warm bone/paper + **oxblood** accent (light) |

The deliberate axis of variation is the **two-phase hero**: a vertical pour (relay) vs. a radial dial
(tarmac) vs. a traveling baton-capsule (ledger) — three different shape languages, plus three
distinct layouts (focal-hero+fleet / instrument-panel / editorial-manifest) and three committed
palettes (one of which is light). Quality and conviction over breadth.

### Files to screenshot

Open via `file://` (fully offline). Each direction is one scrolling page; capture **full-page**
(account → transfers → logs → settings in one image) at desktop, plus one mobile capture for the
responsive collapse. Suggested desktop width **1320px**; mobile **390px**.

#### relay (dark · graphite + chartreuse)
- `/Users/doodla/Code/plundrio/.agents/dashboard/mockups/relay/index.html` — desktop (≥1180px) + mobile (~390px)

#### tarmac (dark · graphite + sodium amber)
- `/Users/doodla/Code/plundrio/.agents/dashboard/mockups/tarmac/index.html` — desktop (≥1180px; gauge bay visible) + mobile (~390px)

#### ledger (light · bone + oxblood)
- `/Users/doodla/Code/plundrio/.agents/dashboard/mockups/ledger/index.html` — desktop (≥1080px) + mobile (~390px)

### Per-direction notes

Each `<slug>/NOTES.md` carries the one-sentence identity statement, the named type + color choices,
the two-phase hero treatment, the motion language (what animates, how, why it serves the data), and
any contract field wished for. Common thread: every animation is bound to a data fact (drip / arc-tip
glint / capsule bob == "this phase is actively moving"; live dot == "SSE connected"), all motion is
GPU-cheap for the Raspberry-Pi browser target with a `prefers-reduced-motion` fallback, and the token
is never rendered (masked "•••• set / Replace…" only). No contract amendment is required by any
direction; the only shared nice-to-have flagged is an optional server-supplied human state label
(currently a small client-side derivation).
