# Direction: console

**Concept (one line):** A dense, monospace operator console — terminal-grade information density where every transfer, log line, and setting is on one scrolling surface, built for someone who reads logs for a living.

## Why this shape

The operator already lives in `docker logs` and `htop`. This direction leans into that: tabular numerics, a hairline grid baseline, component tags in the log that echo zerolog's `component` field, and a color language borrowed from a good terminal theme (cyan + green on near-black). Nothing is hidden behind tabs you have to hunt through — the four surfaces stack so a single scroll is the whole system.

## The two-phase progress treatment (signature element)

One track per transfer, split at a hard **seam at 50%**. Left half is the put.io fetch (cyan), right half is the local download (green). The seam is a literal 1px rule with a small diamond notch so the 50% handoff reads as a structural boundary, not just a color change.

- **queued** — both halves empty, dim 0% label.
- **put.io-fetching** — left half fills cyan with a moving sheen; right half is empty; combined % sits over the whole track (so 62% put.io reads as 31% combined, matching `percent_done`).
- **local-downloading** — left half is full but *muted* (desaturated, 32% opacity) to say "done, behind us"; right half fills green with the active sheen.
- **completed** — both halves full and muted (the work is in the past tense).
- **failed/permanent** — the active half turns red, grayscaled, frozen at its last value; an inline `permanent` tag + the error string replaces the stat row.

The muted-vs-active distinction is the key idea: at a glance you see *which phase owns the transfer right now* without reading the state tag. The combined `percent_done` label floats centered so the single number is always legible over either fill.

## Motion language

Motion here is **diagnostic, not decorative** — every animation maps to a data fact:

- **Active-phase sheen** — a 1.6s left-to-right gradient sweep on the currently-filling segment only. It's the "this is moving right now" signal; a stalled transfer (rate 0) would drop the `.active` class and the sheen stops, so motion presence == throughput presence.
- **State dots pulse** (1.4s) only while put.io-fetching or local-downloading; queued/done/failed dots are static. Same principle: pulse == in-flight.
- **Brand glyph heartbeat + connection dot pulse** — the SSE liveness tell. If the stream drops, these would freeze (the build can hard-stop the animation on disconnect).
- **Log `flashIn`** — a new log line briefly washes cyan then fades (0.9s). Cheap to do (background-color transition), and it draws the eye to the newest line during a fast tail without a jarring slide.
- **Progress fills** use a 0.6s `cubic-bezier(.2,.7,.2,1)` width transition so the ~1s SSE tick lands as a smooth glide, not a jump.

All of it is GPU-cheap (opacity/transform/background-position), which matters for the Raspberry-Pi browser target. Reduced-motion would drop the sheen and pulses and keep only the width transitions.

## Contract fields I wished existed

- **Aggregate throughput / active-count for the section header.** The header shows "28.4 MB/s aggregate" and "2 active" — both are derivable client-side by summing `putio_phase.rate_download` + `local_phase.rate_download` across transfers and counting non-terminal `lifecycle_state`s, so no amendment is strictly needed. Flagging only so the orchestrator knows it's a client-side derivation, not a field.
- **A per-transfer `added_at` distinct from `created_at`.** The row footer wants "added 30s ago / started 4m ago." `created_at` (put.io transfer creation) covers "added"; "started local" would need `local_phase` start time, which the contract doesn't carry. Minor — could derive "started Xm ago" from `created_at` alone and drop the local-start nuance. Noting in case the operator wants the local-phase start time surfaced later.
