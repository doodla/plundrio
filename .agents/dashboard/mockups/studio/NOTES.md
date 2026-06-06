# Direction: studio

**Concept (one line):** An airy, light-mode product — generous whitespace, soft elevation, large friendly type — where the two-phase progress reads as two clearly-labelled words ("put.io" / "local") rather than a color you have to decode.

## Why this shape

The console direction optimizes for an operator already fluent in the system. This one optimizes for *legibility under glance* — the dashboard you keep open in a pinned tab and check while doing something else. Cards float on a tinted-white field, the type is large enough to read across a room, and the density is deliberately low: one transfer is a generous card, not a 4px row. The aesthetic is the modern *arr-adjacent SaaS (Overseerr / Tautulli territory) the operator already runs, so it sits naturally beside them.

## The two-phase progress treatment (signature element)

Each transfer card shows the two phases as **two stacked labelled bars** plus one big combined numeral on the right:

```
put.io  ████████████  fetched · 100%
local   ████████░░░░  8.4 MB/s · 4/5 files · ETA 0:54        71%
```

The split-track-into-two-rows choice trades the console's compactness for zero ambiguity: the phase names are always on screen, so you never have to remember "cyan is put.io." Per-phase meta (rate, files, ETA) sits on the right of its own bar, so put.io speed and local speed never get conflated. The big right-hand numeral is the combined `percent_done`, the single number *arr would show — present so the operator can reconcile the dashboard against Sonarr at a glance.

States:
- **queued** — both bars idle (flat grey, 0 width), combined numeral dimmed.
- **put.io-fetching** — top bar fills blue with a flowing sheen; bottom bar idle, meta reads "waiting for put.io."
- **local-downloading** — top bar full and quiet; bottom bar fills green with the sheen.
- **completed** — both full, green numeral, source-cleaned note in the footer.
- **failed/permanent** — the local bar freezes red; a soft red error panel with a `permanent` badge carries the error string (transient retries would stay green per the contract's error precedence — only `permanent:true` turns red here).

## Motion language

Motion is **calm and confirmatory** — it reassures rather than alerts, matching the light, low-stress aesthetic:

- **Flowing sheen** on the *active* bar only (1.8s, GPU-cheap `background-position`). Same semantic as console — sheen presence == this phase is actively moving — but slower and softer to suit the calmer tone.
- **`breathe`** — the live pill dot, the active state dots: a gentle 2s scale+fade. The header "Live" pill is the SSE-connected tell; on disconnect the build swaps it to a static amber "Reconnecting" pill (no animation).
- **Bar width transition** 0.7s `cubic-bezier(.2,.7,.2,1)` so the ~1s SSE tick eases in. The disk donut uses the same easing on `stroke-dashoffset` for a satisfying sweep on first paint / value change.
- **Card hover** lifts elevation (`sh-1`→`sh-2`) — affordance only, not data.
- **New log line** `logIn` — a brief blue wash that fades (0.8s); the log panel intentionally stays *dark* even in light mode, because a log is a console and the contrast makes levels pop.

Everything is opacity/transform/background-position/box-shadow — no layout thrash, fine on a Pi browser. Reduced-motion drops sheen and breathe, keeps the eased width/donut transitions (they're slow and non-distracting).

## Contract fields I wished existed

- **A friendly state label.** The contract gives `lifecycle_state` (Initial/Downloading/Completed/…) and `putio_status` (raw enum). This direction shows human phrasing ("Fetching at put.io", "Downloading locally") derived client-side from `lifecycle_state` + whether `local_phase` is null + which phase's rate is non-zero. Derivable, no amendment needed — flagging that the mapping (state + local_phase presence → label) is a small client-side function the frontend owns.
- **Per-phase "done" booleans would be nice-to-have but aren't necessary.** "put.io fetched · 100%" is inferred from `putio_phase.percent_done == 100` (or `local_phase != null`, which means fetch finished). Works as-is.
- No genuinely missing field. The contract carries everything this layout renders.
