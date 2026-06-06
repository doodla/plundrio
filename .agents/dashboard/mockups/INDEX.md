# Dashboard mockups — Pass 1 directions

Three genuinely distinct directions for the plundrio web dashboard, each a self-contained static
page rendering all four core surfaces (transfers with two-phase progress, account/quota, live log
viewer, settings). No framework, no build, no network — seed values hand-placed so they screenshot
well. The operator picks one; Pass 2 (the design spec) follows the pick.

All four surfaces are on **one scrolling page per direction**, so each direction is a single URL to
screenshot at desktop, plus one mobile capture to confirm the responsive collapse.

## Directions

| Slug | Concept (one line) |
|------|--------------------|
| `console` | Dense, monospace **operator console** — terminal-grade density, all four surfaces on one scroll. Two-phase progress = one track split at a hard 50% seam (cyan put.io ▸ green local), active phase carries a sheen, finished phase mutes. |
| `studio` | Airy **light product** — generous whitespace, soft elevation, large type. Two-phase progress = two stacked *labelled* bars ("put.io" / "local") so you read words, not colors, plus one big combined numeral. |
| `horizon` | Warm-dark **mission control** with a left nav rail. Two-phase progress = one *continuous* bar whose fill shifts blue→green across the 50% horizon — the handoff is a sunrise, not a seam; a glowing "sun" marks the active frontier. |

Each direction differs in layout (top-nav scroll / top-nav cards / left rail), color (cold near-black /
tinted light / warm charcoal), density (high / low / medium), type (mono / sans / sans), and — most
importantly — the **two-phase progress treatment**, which is the deliberate axis of variation: hard
seam vs. two separate bars vs. one continuous color-shifting bar.

## URLs to screenshot

Open via `file://` (fully offline; no CDN/network needed). Each direction is one page; capture desktop
and mobile.

### console
- `/Users/doodla/Code/plundrio/.agents/dashboard/mockups/console/index.html`  — desktop (≥1180px wide recommended)
- same URL — mobile (~390px wide; top-nav and log component column collapse)

### studio
- `/Users/doodla/Code/plundrio/.agents/dashboard/mockups/studio/index.html`  — desktop (≥1080px)
- same URL — mobile (~390px; cards stack, combined numeral moves below the bars)

### horizon
- `/Users/doodla/Code/plundrio/.agents/dashboard/mockups/horizon/index.html`  — desktop (≥1100px; left rail visible)
- same URL — mobile (~390px; left rail becomes a top bar, tiles stack)

Suggested capture width for desktop: 1280px. Suggested mobile width: 390px. Full-page (not just the
fold) so the operator sees account → transfers → logs → settings in one image per direction.

## Per-direction notes

Each `<slug>/NOTES.md` carries the concept, the motion language (what animates, how, why it serves the
data), and any contract field the designer wished existed. Common thread across all three: every
animation is bound to a data fact (sheen/sun/pulse == "this phase is actively moving"; live dots ==
"SSE connected"), all motion is GPU-cheap for the Raspberry-Pi browser target, and the token is never
rendered (masked "set — replace?" pattern only). No contract amendment is required by any direction;
the only shared nice-to-have flagged is an optional server-supplied human state label (currently a
small client-side derivation).
