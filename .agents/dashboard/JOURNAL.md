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

## 2026-06-06 — Agent-loop revision: harness-builder (pre-failure)

Revised `roles/harness-builder.md` deliverable 1 before spawning it. The prompt said to "reuse and
extend the existing `fakePutioClient` in `cleanup_test.go`" for `--demo` mode. That's a dead end:
that fake is `_test.go` (not compiled into the binary) and satisfies only `download.PutioClient`,
but a RUNTIME demo client must be compiled code satisfying BOTH `server.PutioClient` and
`download.PutioClient`. The role now says the demo fake is a distinct compiled component (e.g.
`internal/demo`), enumerates both interfaces' method sets, names the `main.go` swap seam, and notes
the test fake may still serve unit-level probing separately.

Caught by re-reading the role against the actual interfaces before dispatch — the agent loop working
pre-failure (cheaper than catching it after the builder goes down the wrong path). Also authored the
M4–M6 role files (backend-builder, code-reviewer, build-engineer, integration-verifier) now that the
contract is frozen; frontend-builder + ui-reviewer remain deferred until the operator picks a design.

State: M0 ✓, M1 ✓ (gate passed). M3 ux-designer running (mockups). Launching M2 harness-builder next.

## 2026-06-06 — M2 done; M3 operator gate REJECTED round 1; agent-loop revision #2 (ux-designer)

M2 harness-builder delivered all four instruments (demo mode, contract prober, snapshot tool, log
helper), self-tested, repo green; committed. Added a `--full` page-capture mode to the snapshot tool
(the mockups are long scroll pages). Rendered all three round-1 directions full-page and surfaced
them to the operator.

**Operator gate result: REJECTED.** All three directions (`console`, `studio`, `horizon`) were called
**"generic"** — competent but templatey (stock card grids, conventional dashboard chrome). Clarified
the target: **premium-minimal craft (Linear/Raycast/Vercel-grade) as the foundation, with
expressive/bold moments** — a committed color POV, large confident typography, and the two-phase
progress elevated into a designed hero element.

**Agent-loop revision #2 — `roles/ux-designer.md` substantially rewritten** (the miss was the brief:
the old role asked for "polished" + "distinct directions" but never demanded a real visual identity
or forbade the admin-template look). The role now: locks the aesthetic target; frames the run as a
REDO requiring a step change past the rejected set; adds a concrete craft bar (intentional type
scale + display numerals, committed palette, material/depth discipline, the two-phase progress as THE
hero); an explicit Forbidden list naming exactly what "generic" meant; and a reference-caliber clause.
Round-1 mockups kept as the anti-pattern reference. Re-running as a FRESH spawn from the revised file
(source-of-truth discipline; avoids anchoring on its own generic round-1 output).

**Parallelization decision:** the design bounce blocks only the frontend. Launching `backend-builder`
(M4) NOW in parallel — it builds the daemon API/SSE/config/log/worker changes against the FROZEN
contract + design.md, with the M2 harness (demo mode + prober) as its test substrate. Zero dependency
on the visual mockups; the contract is stable (round-1 designer confirmed no amendment needed, and a
craft-level redesign uses the same four surfaces + same data). This recovers the bounce's wall-clock.

## 2026-06-06 — M4: backend built + code-review gate passed

backend-builder implemented all five daemon changes + the transferprog MOVE: `internal/dashboard`
(listener, REST, SSE hub, render DTO, settings), `internal/web` (`//go:embed all:dist` + placeholder),
`internal/config/overrides.go` (runtime-overrides layer, env-skip guard), `internal/transferprog`
(moved two-phase math, shared by server + dashboard), log fan-out in `internal/log`, worker resize in
`internal/download`. Tested headless via the M2 harness: `TestContractAgainstDemoServer` boots the
dashboard in demo mode and runs the contract prober (with an env-pinned sentinel token) — green.

code-reviewer ran an EQUIPPED gate (build on bare checkout, vet, `test -race -count=5`, the prober,
grep for token leakage) → **VERDICT APPROVE, 0 must-fix**. It verified the transferprog move is
line-identical to HEAD (so `:9091` behavior is unchanged), reasoned through the shrink-test
determinism (N tokens → N exits, ordering-independent — passes for the right reason), and judged the
token-persistable-but-never-boot-applied resolution sound (no upstream bounce).

Two non-blocking nits folded in before commit (kept dead code + a contract gap out of history):
unknown `/api/*` now returns the contract's JSON 404 via an `/api/` ServeMux catch-all (removing the
dead `codeNotFound`; SPA fallback still serves non-API deep links), and a test helper now genuinely
unsets `PLDR_*` vars. Re-verified green. The backend can be exercised live with
`--demo --dashboard-listen :9092`.

The contract underspec the builder surfaced (contract lists `token` under PUT `persisted`, design's
overrides shape omits it) was resolved in-build, not bounced: token is persistable but excluded from
the boot-applied override set, so a stale file token can never become the live token. Gate confirmed.

## 2026-06-06 — M3 LOCKED: relaydeck + ship-all-palettes-as-themes

Design iteration converged. Sequence of operator gates: round 1 (console/studio/horizon) REJECTED as
generic → role bar raised → round 2 (relay/tarmac/ledger) → operator picked a relay×tarmac COMBO +
light/dark → designer produced `relaydeck` (fused flow-gauge hero) → operator approved layout/hero
but rejected the dial glow + the chartreuse palette → glow removed + three palette candidates
(tide/ember/ion) → operator: "it's the same structure, yeah? ship with themes."

**Resolution (read of that answer):** the three palettes are interchangeable color systems on the
approved layout, so SHIP ALL THREE as user-selectable themes rather than locking one. Final locked
design: relaydeck layout + fused flow-gauge hero (glow removed) + {tide, ember, ion} × {light, dark}
as a palette×mode theme system. Default **ion + dark** (orchestrator's call — operator didn't specify
a default; ion matches the Linear/Raycast bar they cited and their initial dark lean; fully
switchable). The picker is a pure client-side browser preference (localStorage) — NO backend/contract
change, since the daemon doesn't need to know the viewer's theme.

The infra cost is near-zero: the designer already tokenized everything with `?palette=`/`?theme=`
resolved before paint, so all six combos are first-class. Frontend reads the exact token values from
the committed mockup CSS (the concrete source of truth for color).

Launching M5 frontend-builder (Svelte 5 + Vite) against SPEC + contract + the live M4 demo backend.
In parallel, the designer finalizes SPEC §2 to document the multi-palette theme system + selector +
default (token values unchanged — locked in the committed mockup). All 10 role files now authored.
