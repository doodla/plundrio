# Direction: ledger

**Identity statement:** A printed manifest set in type — heavy editorial hierarchy, a single oxblood
accent on warm bone, reading top-to-bottom like a document — where the two-phase progress is a
substantial relay *baton*: a capsule that rides the put.io→local seam as ink hands off to oxblood.

## Why this is not the round-1 shape

The round-1 set was rejected as generic admin chrome — boxes, tabs, thin bars. `ledger` rejects the
*dashboard idiom altogether*. It's the only light direction, and it's typeset like a printed
register: a serif masthead ("Transfer Manifest"), numbered running-heads (01 / 02 / 03), 2px rules
as section dividers, and huge tabular numerals doing the hierarchy work that cards did in round 1.
Nothing here looks like a Tailwind template because almost nothing is a "card" — it's a document.
This is the maximum-distance option from the dark instrument/console lane, deliberately, so the
operator's pick spans a real range.

## Named type + color

- **Display / headings:** **Spectral** (a contrasty serif) → Iowan/Georgia fallback — the masthead,
  section titles, and transfer names are serif, which is what makes it read as *editorial* rather
  than *admin*.
- **Numerals:** **IBM Plex Mono**, tabular — the combined % is set at 96px, the largest type on the
  page, as the document's headline figure.
- **Body / controls:** Inter → system sans.
- **Neutral ramp:** a *chosen warm bone/paper* (`#f4f0e8 → #fbf8f2`) with warm ink (`#1a1714`), not a
  cool grey. This is the committed point of view: a paper document, not a screen.
- **Accent (committed POV):** **oxblood `#9c2b2b`** is the single accent — it marks the local /
  "landed, yours now" leg, the live dot, and primary actions. The put.io leg stays **monochrome ink**
  (hatched), so the manifest is ink-on-bone until oxblood signals the work is local. One accent,
  load-bearing.

## The two-phase hero (signature)

A **relay baton**: a thick (26px) handoff track. The put.io leg fills with **ink + a diagonal hatch**
(the work is in transit, not yet yours); a 2px paper seam marks 50%; the local leg fills **oxblood**
with a moving sheen. A **physical relay capsule** — a paper disc with an oxblood core — rides the
leading edge of the active leg, *bobbing* gently, and sits exactly at the seam during the handoff
moment. Beside it, the 96px combined % is the page's headline figure with the combined rate in
oxblood. In the manifest rows the baton compresses to 12px with a smaller capsule, keeping the
metaphor consistent. States: queued (empty track, seam only, dim "0"), fetching (ink leg fills,
capsule on the ink leg, ink dot pulsing), local (ink leg muted-full, oxblood leg fills, capsule past
the seam, oxblood dot pulsing), completed (oxblood leg → green full, no capsule, green "100"),
failed/permanent (legs recolor soft-oxblood and freeze; the row tints to oxblood-wash and a
`permanent` chip + error string sit under the name — transient retries stay oxblood per the contract's
precedence).

## Motion language

Motion is **paper physics** — minimal and confirmatory, suiting the calm document tone; all
GPU-cheap, Pi-safe.

- **Capsule bob** (2.4s) and **oxblood sheen** (1.9s) on the active baton — present iff a phase is
  moving; a stalled transfer drops them, so motion == throughput.
- **State dots beat** (1.6s) only while fetching/downloading; the live dot beats as the
  SSE-connected tell (on disconnect the build freezes it and swaps the pill to amber "reconnecting").
- **Baton width** eases on each ~1s SSE tick so the capsule glides along the track, not jumps.
- **New log line** flashes oxblood-wash and fades (1s). The log "Record" is intentionally a **dark
  inset** even in light mode — a log is a console, and the contrast makes levels pop against bone.
- **Reduced-motion:** drops bob/sheen/beat/flash; the baton and capsule render at their static
  position, the document is fully legible with zero motion.

## Custom detail craft

Quota is a *hatched scale* with a 95% danger tick (not a donut). `log_level` is a segmented control
(active cell oxblood-filled), `workers` a ± stepper. Settings are a typeset **register**: key +
badges in the left column, a plain-language description in the middle, the control on the right —
reads like form rows in a printed manual, not a stacked web form. Locked `target`/`token` dim and
badge `locked`; the token is masked `•••• set` with a "Replace…" action, never read back.

## Contract wishes

None blocking. Same two client-side derivations (aggregate throughput / active-count for the doc
head; a human phase label like "landing locally" / "fetching at put.io") — derivable from
`lifecycle_state` + `local_phase` presence + active rate today. The baton consumes `percent_done` for
the headline figure and the two phases for the leg split, matching the contract's combined+split
design exactly. A server-supplied state label is the only nice-to-have, flagged for cross-client
consistency if the orchestrator wants one source of truth.
