# Direction: tarmac

**Identity statement:** An instrument cluster, not a dashboard of cards — the two-phase progress is a
270° gauge whose needle sweeps the steel left arc (put.io fetch) then the sodium-amber right arc
(local download), with the 12-o'clock mark as the put.io→local handoff and the combined % as the big
center readout.

## Why this is not the round-1 shape

The round-1 set was rejected as generic because the progress was always a *thin horizontal bar in a
list of rows*. `tarmac` changes the **shape language entirely**: the signature is radial, not linear.
A download is an instrument reading — like an airspeed dial — and the whole page is built as a panel
(one master gauge + an account instrument cluster + a transfer strip of mini-dials + a log console +
a settings rack), the way a cockpit groups instruments by function rather than tiling identical
cards. That radial hero is impossible to mistake for an admin template.

## Named type + color

- **UI face:** Inter (neutral grotesk) → system sans fallback.
- **Numerals:** **IBM Plex Mono**, tabular, engraved — the dial readout is 74px with a metal gradient
  clip; every gauge label and stat is Plex Mono.
- **Neutral ramp:** a *chosen graphite* (`#070809 → #2e3942`) with faint horizontal scanlines (a
  39px repeating hairline) so the panel feels like brushed instrument metal, not flat slate.
- **Accent (committed POV):** **sodium amber `#ffb020`** — a runway-lamp / warning-light amber — is
  the signature, owning the local-download arc, the disk fill, primary actions, and the brand sigil.
  **Steel-blue `#6ec1ff`** is the put.io fetch channel; **green `#46d18a`** is the live/connected and
  completed tell; red is failure. Color is strictly phase/state.

## The two-phase hero (signature)

A **270° SVG gauge** (gap at the bottom). The left half of the sweep is put.io fetch (steel gradient
arc); the right half is local download (amber gradient arc with a stronger glow); the **12-o'clock
midmark is the 50% handoff**. The combined `percent_done` is the giant center readout with the
combined rate below it, and a glinting amber tip marks the active arc frontier. Under the dial, a
two-cell legend gives the per-phase numbers (the finished phase muted). Each fleet member carries a
**mini version of the same dial** (50px) plus its two thin phase tracks, so the radial language is
consistent top to bottom. States: queued (track only, dim "0"), fetching (steel arc partial, blue
dot pulsing), local (steel arc full + amber arc filling past the midmark, amber dot pulsing),
completed (both arcs recolor green, "100"), failed/permanent (arcs recolor red and freeze; inline
`permanent` tag + error string — transient retries stay amber per the contract's precedence).

## Motion language

Motion is **instrument behavior** — calm, mechanical, every motion a data fact; all GPU-cheap, Pi-safe.

- **Arc-tip glint** (1.8s) on the master gauge marks where work is happening *right now*; gone the
  instant a transfer completes or fails.
- **Rate arrow nudge** (1.4s) on the center readout — a subtle "throughput is live" tic.
- **State dots pulse** (1.6s) only while fetching/downloading.
- **Live dot pulse** (1.8s) is the SSE-connected tell; on disconnect the build freezes it amber.
- **Log level color-bar:** each line carries a left bar tinted by level (steel/amber/red) — a quiet
  per-line level signal; new lines flash amber and fade (1s).
- **Arc/fill transitions** ease so each SSE tick advances the needle smoothly via `stroke-dasharray`
  (cheap on the Pi; flagged for the spec as the one SVG-stroke animation to watch).
- **Reduced-motion:** drops glint/nudge/pulse/flash; arcs render at their static value (the radial
  encoding survives with zero motion — motion is additive, never load-bearing).

## Custom detail craft

Segmented control for `log_level` (active cell amber-filled, glowing), a ± stepper for `workers`,
disk shown as a *ticked slider gauge* (sodium fill behind etched 10% ticks) rather than a stock
donut. Locked fields dim + badge + lock glyph; token masked `•••• set` with "Replace token…", never
read back.

## Contract wishes

None blocking. Same two client-side derivations as the other directions (aggregate throughput /
active-count for the account instruments; a human phase label) — both derivable from the contract
today. The gauge consumes `percent_done` for the combined readout and the two phase percentages for
the arc split, which is exactly the "expose combined *and* split" shape the contract already
provides, so this direction needs no new field. A server-supplied state label is the only
nice-to-have, flagged for consistency across clients if the orchestrator wants it.
