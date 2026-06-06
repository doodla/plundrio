# plundrio web dashboard — orchestration

This directory is **build scaffolding**, not product code. It holds the role prompts,
the journal, the plan/contract, and the visual-gate evidence for building the dashboard.
It lives under `.agents/` so it is trivially droppable from the final PR if undesired.

## What is being built

A polished web dashboard for plundrio, served by the daemon itself:

- **Live transfers** with two-phase progress (put.io fetch 0–50% → local download 50–100%).
- **Account / quota** state from `GetAccountInfo`.
- **Live-streaming log viewer** that respects the existing log levels.
- **Editable settings** (target, folder, workers, log level, listen addr) — see Decisions.

## Decisions (locked with the operator)

1. **Settings = full edit + persist.** The daemon owns a writable runtime-overrides file at
   highest precedence. `log_level` and `workers` apply live; `target`/`folder`/`listen`/
   `dashboard-listen` persist and take effect on restart (UI marks these "restart required").
   Keys pinned by `PLDR_*` env vars are shown locked, because env beats the overrides file and
   the daemon cannot rewrite compose.
2. **Exposure = separate dashboard port.** UI + JSON/SSE API bind to their own listener
   (`--dashboard-listen`, default off). Transmission RPC stays isolated on `:9091`. No auth in
   v1. The put.io OAuth token is never rendered — settings shows set/unset and allows replace.
3. **Visual = mockups-first.** The design stage produces 2–3 directions; the operator picks
   from screenshots before any product UI code is written.

## Rules

- **Orchestrator commits only.** Subagents never run git. The orchestrator commits in small,
  coherent increments as each artifact clears its gate. The branch stays green.
- **Trust artifacts, not claims.** Every role is the `.md` file in `roles/`, not a memory of it.
  Reviewer verdicts are backed by instruments (tests, prober, screenshots), never vibes.
- **Verifiers are equipped.** If a gate can't actually check what it's asked to check, building
  that instrument is itself a task (see `harness-builder`).
- **Bounce upstream.** A downstream reviewer that finds the fault is upstream (contract wrong,
  plan wrong) sends the work back there, not just to the immediate builder.

## Branch

`sv/web-dashboard` off `main`.

## Index

- `JOURNAL.md` — append-only log of role-file revisions (what changed, why, which iteration).
- `roles/` — one prompt per role; the source of truth for every spawned agent.
- `plan/` — `design.md` (architecture + stack justification) and `contract.md` (API/SSE schema).
- `mockups/` — static design directions for the visual gate.
- `screenshots/` — Playwright captures backing the ui-reviewer and integration gates.

## Roles

| Role | Loop stage | Produces | Gated by |
|------|-----------|----------|----------|
| `planner` | design | `plan/design.md`, `plan/contract.md`, stack choice | `plan-reviewer` |
| `plan-reviewer` | design gate | approve / bounce verdict | — (the gate) |
| `harness-builder` | instruments | seed mode, fake put.io, endpoint prober, snapshot tool | self-test |
| `ux-designer` | design | 2–3 static mockups + chosen design spec | operator (HITL) + `ui-reviewer` |
| `backend-builder` | build | `internal/dashboard`, config-overrides, log fan-out, worker resize | `code-reviewer` + prober |
| `frontend-builder` | build | the SPA against the frozen contract + design spec | `ui-reviewer` + `code-reviewer` |
| `code-reviewer` | build gate | findings (Go/TS correctness, CLAUDE.md, security, silent failures) | — (the gate) |
| `ui-reviewer` | build gate | polish verdict vs. design spec, from screenshots | — (the gate) |
| `build-engineer` | build | Nix frontend build + `go:embed` + gomod2nix + CI | `nix build` |
| `integration-verifier` | ship gate | e2e verdict from full seed-mode boot | — (the gate) |

Builder/reviewer role files for M4+ are authored after the contract freezes (M1) so they can
reference the concrete stack and schema — see JOURNAL for when each lands.
