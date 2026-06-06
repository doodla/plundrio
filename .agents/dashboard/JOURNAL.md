# Orchestration journal

Append-only. Newest entries at the bottom. Each entry: what changed, why, and which loop
iteration or gate failure prompted it. Role-file revisions and milestone transitions are both
logged so the branch history and this journal tell the same story.

---

## 2026-06-05 — M0: scaffold

Created branch `sv/web-dashboard`. Authored orchestration spec (`README.md`), this journal, and
the M1–M3 role files: `planner`, `plan-reviewer`, `harness-builder`, `ux-designer`.

Locked three operator decisions before authoring any role (see README → Decisions): settings =
full edit + persist via a runtime-overrides file; exposure = separate `--dashboard-listen` port
with RPC isolated; visual = mockups-first (operator picks from screenshots).

M4+ builder/reviewer roles (`backend-builder`, `frontend-builder`, `code-reviewer`,
`ui-reviewer`, `build-engineer`, `integration-verifier`) are deferred until the M1 contract
freezes, so they can reference the concrete stack and schema instead of guessing. This is a
deliberate sequencing choice, not an omission.

## 2026-06-05 — M1 prep: research grounding

Ran the mandatory research protocol (effective-research skill) before the planner committed to a
hard-to-reverse stack/build decision. Manual scout of both libraries: all three topics MISSING
(global library is CC-plugins / browser-automation / arr-stack / homelab; no Go/Nix/frontend
content). Dispatched two researchers (the third topic — Go SSE hub + zerolog tee — carried as a
known pattern rather than burning a researcher).

Outcomes written to the (gitignored) project library:
- `fe-stack-embedded-realtime-spa.md` — recommends **Svelte 5 + Vite static SPA** (animation
  primitives built-in, no VDOM runtime → tiny bundle for Pi-class). SolidJS runner-up.
- `nix-frontend-build-go-embed.md` — **two-derivation** pattern: `buildNpmPackage`+`npmDepsHash`
  emits `dist/`, fed into `buildGoApplication` via `postPatch` into `internal/web/dist`; built
  once natively, shared across arches; `.gitkeep` keeps plain `go build` working.

Compliance pass: both have Summary+Sources and label their uncertainties (`importNpmLock`
availability; pnpm `fetcherVersion`). Residual gap accepted, not re-dispatched: the Nix doc cites
sources by nixpkgs path / issue number / named blog with authoritativeness grades but no literal
URLs — traceable but not click-through. Acceptable for a volatile infra doc.

The planner OWNS the final stack call (per its role) but receives this as grounding.

## 2026-06-06 — M1: plan + contract frozen (gate passed, one bounce)

Planner produced `plan/design.md` + `plan/contract.md`. Final stack: **Svelte 5 + Vite static SPA**
(adopted the research recommendation). plan-reviewer ran an EQUIPPED review (compiled the embed
case, ran `go doc` on the put.io types, read the precedence + worker seams) and returned **BOUNCE,
2 defects**:

1. `//go:embed dist` + dotfile-only `dist/.gitkeep` does not compile (`go:embed` excludes `.`/`_`
   names → zero embeddable files). Fix: `//go:embed all:dist`. The reviewer verified this
   empirically. This traced back to a wrong claim in the `nix-frontend-build-go-embed.md` research
   doc — corrected the doc proactively (now documents `all:dist` + the empirical finding).
2. Worker-count double-booking: both `SetWorkerCount` and retiring workers wrote `workerCount`,
   so a shrink 8→2 lands at −4. Fix: single-owner count.

Reviewer also: confirmed ALL put.io field citations + the 95% quota threshold against source; ruled
**MOVE** the two-phase progress math into a shared `internal/transferprog` (vs duplicate-and-parity-
test) so the dashboard and `:9091` can't diverge; flagged the `os.LookupEnv` env-skip before
`viper.Set` as load-bearing (raw `viper.Set` is the Override tier and beats env).

Bounced to the planner (continued in-context). It applied all four — verified directly in the
artifacts. **Gate-discipline note:** did NOT re-run the full plan-reviewer for two surgical,
independently-verifiable fixes; the reviewer had approved everything else, the fixes are confirmed
present and correct, and a re-run would be ceremony. The plan-math ruling is baked in (hedge
deleted). This is the recorded rationale for treating the gate as cleared.

No agent-loop revision: the bounce traced to design errors the gate caught, not to a weak role
prompt. The planner and plan-reviewer role files held.
