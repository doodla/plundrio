# Direction: relay

**Identity statement:** One transfer is the headline — rendered as a tall vertical *pour* where
put.io fetches into the upper vessel and the work falls through a handoff throat to land on disk —
and the rest of the system is quiet supporting matter arranged around it.

## Why this is not the round-1 shape

Round 1 (`console`/`studio`/`horizon`) was rejected as generic because all three were the same
skeleton: a flat list of equal rows, each row a thin horizontal bar, with admin chrome around it.
`relay` breaks that at the information-architecture level, not the recolor level. There is a **focal
hero + fleet** hierarchy: the most-active transfer is enlarged to masthead scale and given a
vertical two-phase vessel; the others collapse into a compact secondary list. The eye lands on one
thing, the way Linear lands you on the active issue, not on a uniform grid.

## Named type + color

- **Display / numerals:** Space Grotesk (grotesk geometry, confident at large sizes) for headings;
  **Space Mono** for every numeric, tabular-figured, negative-tracked. The combined % is set at 84px
  with a subtle top-light gradient clip so it reads as machined metal, not flat text.
- **UI face:** Space Grotesk → system sans fallback.
- **Neutral ramp:** a *chosen cool-graphite* (`#07090c → #2a3543`), bluer and deeper than default
  slate, lit by two faint corner radials (cyan top-right, chartreuse bottom-left).
- **Accent (committed POV):** **chartreuse `#c2f546`** is the signature — it means "landed locally /
  yours now." **Steel-blue `#5fb2ff`** is the put.io fetch channel. Amber/red carry restart/fail.
  Color is strictly phase/state; nothing is colored for decoration.

## The two-phase hero (signature)

A **vertical pour vessel**. The tube's upper half is the put.io fetch (steel-blue, fills downward);
the lower half is the local download (chartreuse, fills upward from the floor). The **50% mark is a
physical throat** — a hairline with a diamond notch — and chartreuse beads *drip* through it while
local download is active, so the put.io→local handoff is literally a substance changing vessels.
A rising sheen sits on the active fill. Beside the vessel: the giant combined `percent_done` (71%),
the combined rate, and a two-cell phase readout where the finished phase is muted to 50%. In the
fleet, the same idea compresses to a 9px rail with a hard center seam (put.io left, chartreuse-glow
local right; the finished put.io leg mutes). States: queued (empty, dim), fetching (upper fills,
lower empty), local (upper muted-full, lower fills + drips), completed (both full, muted, green
numeral), failed/permanent (active leg recolors red and freezes; inline `permanent` tag + error
string replaces the stat row — transient retries stay chartreuse per the contract's precedence).

## Motion language

Motion is **substance in transit** — every animation maps to a data fact, GPU-cheap (opacity /
transform / background-position / width), Pi-safe.

- **Drip + rising sheen** on the focal vessel: present iff a phase is actively moving (rate > 0). A
  stalled transfer drops `.active`, the drip stops — motion presence == throughput presence.
- **State dots breathe** (1.6s) only while fetching/downloading; queued/done/failed dots are static.
- **Brand mark + live dot** breathe as the SSE-connected tell; on disconnect the build freezes both
  and the pill goes amber "reconnecting."
- **Fill transitions** ease 0.6s `cubic-bezier(.2,.7,.2,1)` so each ~1s SSE tick glides, not jumps.
- **New log line** flashes chartreuse and fades (1s) — draws the eye to the newest line on a fast
  tail without a slide.
- **Reduced-motion:** drops drip/sheen/breathe; keeps only the eased fill transitions.

## Custom detail craft

Segmented control for `log_level` (active cell is chartreuse-filled, glowing) and a real ± stepper
for `workers` — neither looks like a browser/Tailwind default. Locked fields (`target`, `token`)
dim, badge `locked`, and show a lock glyph; the token is masked `•••• set` with a "Replace token…"
action and is never read back.

## Contract wishes

None blocking. Two client-side derivations I lean on (flagging, not requesting): **aggregate
throughput / active-count** in the live pill + fleet header (sum `putio_phase.rate_download` +
`local_phase.rate_download`, count non-terminal `lifecycle_state`s); and a **human phase label**
("landing on disk", "fetching at put.io") derived from `lifecycle_state` + `local_phase` presence +
which phase's rate is non-zero. Both are derivable today. A server-supplied state label would remove
that small mapping if the orchestrator wants one source of truth across clients.
