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
