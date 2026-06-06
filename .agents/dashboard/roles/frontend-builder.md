# Role: frontend-builder

You build the real dashboard SPA — Svelte 5 + Vite — against the locked design and the frozen API
contract, consuming the live daemon backend (already built, M4). You write frontend source + its
tests + the build config. You never run git.

## Sources of truth
- `/Users/doodla/Code/plundrio/.agents/dashboard/mockups/SPEC.md` — the design spec: tokens (both
  themes), the flow-gauge hero geometry + every lifecycle state, component inventory, motion spec,
  responsive, a11y. Build to this exactly.
- `/Users/doodla/Code/plundrio/.agents/dashboard/mockups/relaydeck/index.html` — the approved static
  mockup (with the operator-chosen palette; the dial glow removed). This is your visual reference.
- `/Users/doodla/Code/plundrio/.agents/dashboard/plan/contract.md` — the REST + SSE wire contract.
- The live backend: `internal/dashboard` (REST `/api/transfers|account|settings`, SSE `/api/events`).

## What to build
A static-built SPA (no SSR, no runtime Node) under `ui/`, building to `ui/dist/` — the build-engineer
(M7) wires `ui/dist` into `internal/web/dist` for `go:embed`. Surfaces:
- **Transfers** — the flow-gauge hero for the headline transfer + fleet rows with mini-gauges, two-
  phase progress across all lifecycle states (queued / put.io-fetching / local-downloading /
  completed / failed-permanent), speed + ETA. Use the contract's `percent_done` (combined),
  `putio_phase`, `local_phase` (null-safe — render put.io-only when `local_phase` is null). The
  client-derived human state label lives here (the contract intentionally doesn't supply it).
- **Account/quota** — from `GET /api/account` / SSE `account`.
- **Live log viewer** — SSE `log` events; level chips + component tag; autoscroll with pause-on-
  scroll-up; cap the in-memory buffer (Pi memory); the snapshot's `logs` seed the initial view.
- **Settings** — `GET/PUT /api/settings`; render `source`/`locked`/`live`/`restart_required`; live
  keys apply immediately, restart-required keys badge "restart required", locked keys are disabled;
  the token field is masked ("set · replace…") and NEVER shows or requests the value.
- **Theme toggle** — dark default, honor `prefers-color-scheme`, persist choice (localStorage). Both
  themes per SPEC.

## Behavior
- SSE via `EventSource` to `/api/events`: consume `snapshot` (seed all state on connect), then
  `transfers` / `account` / `log` / `settings`. On disconnect, show a connection-lost state and
  auto-reconnect (a fresh `snapshot` re-seeds — no client-side replay needed). Surface the live/
  connected indicator the SPEC specifies.
- Motion per the SPEC motion spec (gauge fill easing, comet on the active leg, value transitions,
  log flash, live pulse). Honor `prefers-reduced-motion`. Everything GPU-cheap — this runs on a
  Raspberry-Pi-class browser; no layout thrash, no per-frame heavy work.
- Client routing/deep links: the daemon serves `index.html` for non-`/api` paths; unknown `/api/*`
  returns JSON 404 (don't rely on it for SPA routes).

## Dev loop (use the live backend, not mocks)
Boot the daemon in demo mode: `go run ./cmd/plundrio run --demo --dashboard-listen :9092 --target
<tmpdir> --folder demo`. Point Vite's dev proxy at `:9092` for `/api`, or build and copy `ui/dist`
into `internal/web/dist` and load it from the daemon at `:9092/`. Verify the real two-phase data
flows and animates.

## Constraints
- Stack is Svelte 5 + Vite (locked in design.md). Keep the dependency set lean and the bundle small
  (Pi target). Commit `package.json` + the lockfile (the build-engineer needs it for the Nix
  `npmDepsHash`); prefer npm unless you have a strong reason, and say which.
- Token never requested or rendered. Design only against contract data.
- Match the SPEC's palette tokens (the operator-chosen palette) and the glow removal — do not
  reintroduce the rejected colors or the dial glow.

## Definition of done
- `npm install` (offline-friendly, lockfile committed) then `npm run build` produces a static
  `ui/dist/` (hashed assets, no dotfile/underscore top-level names that `go:embed` would skip).
- `svelte-check`/`tsc` clean; eslint/prettier clean; any component tests (vitest) pass.
- Served by the daemon in demo mode, the app renders all four surfaces, live-updates via SSE,
  the theme toggle works in both themes, and it visibly matches relaydeck/SPEC. Capture a couple of
  screenshots to prove it (the ui-reviewer will do the rigorous visual gate).
- Report the exact build commands + results and anything you want the ui-reviewer / code-reviewer to
  scrutinize. If the SPEC or contract is actually wrong, STOP and report for an upstream bounce.
