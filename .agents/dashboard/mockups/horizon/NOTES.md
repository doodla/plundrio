# Direction: horizon

**Concept (one line):** A warm-dark "mission control" with a left nav rail and a *single continuous* two-phase bar whose fill shifts blue→green as it crosses the 50% horizon — the put.io→local handoff reads as one journey, not two.

## Why this shape

The hybrid between console and studio: dark like the console (operator-native, easy on the eyes for a tab that's always open) but warm-neutral rather than clinical near-black, with studio's breathing room and a layout neither of the others use — a **left rail** instead of top tabs, which frames it as a place you live in rather than a page you visit. Medium density: more compact than studio's cards, more spacious than console's rows. The warm charcoal + amber/gold accents pull it away from the cold blue-grey default that most homelab dashboards land on.

## The two-phase progress treatment (signature element)

This is the direction's whole thesis and the reason it exists as a third option. Where console splits the track at a hard seam and studio splits into two separate bars, **horizon keeps one bar** and encodes the phase in *color along the fill*:

- The fill's gradient is anchored to the full track width (blue at 0%, teal at the 50% mark, green at 100%) and clipped by the current percentage. So a transfer at 31% shows a blue-leaning fill (still in put.io), and at 71% shows a green-dominant fill (well into local) — **the color tells you the phase without a label or a seam.**
- A faint `50%` tick marks the horizon line — the put.io→local handoff — so the structural boundary is still legible, but it's a reference mark, not a wall.
- A glowing "sun" — a soft white radial bloom at the leading edge — marks the active frontier. It pulses while downloading and vanishes on completion/failure.
- The combined `percent_done` is the headline numeral on the right; precise per-phase numbers (put.io rate/%/ETA, local rate/files/ETA) live in a `readout` strip below, with the inactive phase muted.

The argument for this treatment: a download is *one* thing to the operator (one episode arriving), and the two phases are stages of its journey, not two parallel jobs. The continuous bar matches that mental model — it fills once, left to right, the color quietly telling you where in the pipeline the work is.

States:
- **queued** — `.idle` (no fill, dimmed numeral).
- **put.io-fetching** — fill in the blue zone, sun pulsing, put.io readout lit / local muted.
- **local-downloading** — fill past the horizon into green, local readout lit / put.io muted to "fetched 100%".
- **completed** — full green fill, sun gone, both readouts muted.
- **failed/permanent** — fill recolors to red beyond its frozen point, sun gone, inline error strip with `permanent` badge.

## Motion language

Motion is **orbital / atmospheric** — it evokes a system that's running, tied to the sunrise metaphor:

- **The sun bloom** (1.8s opacity pulse at the fill's leading edge) is the signature live tell — it marks *where work is happening right now*. Gone the instant a transfer completes or fails, so its presence is exactly "active frontier exists."
- **Rail orb sunrise** — a soft green glow rises and falls beneath the brand orb (4s), the ambient "daemon is alive" heartbeat. Plus the rail's `Stream live` dot blips (2s) as the SSE-connected tell; on disconnect the build freezes both and the dot goes amber.
- **State dots blip** (1.5s) only while fetching/downloading — pulse == in-flight, same discipline as the other two directions.
- **Fill width** eases 0.7s `cubic-bezier(.2,.7,.2,1)` so the SSE tick glides; the disk bar shares the easing.
- **Log line `logflash`** — new line washes blue and fades (0.9s); each log row carries a left color-bar by level (a quieter level signal than full-row tinting — suits the denser log here).

All GPU-cheap (opacity / transform / background-position / width) — fine on a Pi browser. Reduced-motion drops the sun bloom, orb rise, and blips; keeps the eased width transitions and the static gradient (the color-as-phase encoding survives without any motion, which is the point — motion is additive here, never load-bearing for comprehension).

## Contract fields I wished existed

- **Nothing missing for rendering**, but one ergonomics note: the continuous-gradient treatment leans on `percent_done` (combined 0–1) as the single fill width and uses the two phases only for the readout strip. That's exactly what the contract provides (`percent_done` plus `putio_phase` / `local_phase`), so this direction is the *cleanest fit* to the contract's "expose both combined and split" design — no derivation gymnastics.
- The one nice-to-have, shared with the other directions: a **server-supplied human state label** would remove a small client-side mapping (`lifecycle_state` + `local_phase==null` + active-rate → "put.io fetch" / "Local download"). Derivable today; flagging only so the orchestrator can decide whether to push it server-side for consistency across the dashboard and any future clients.
