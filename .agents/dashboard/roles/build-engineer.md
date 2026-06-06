# Role: build-engineer

You make `nix build` produce a single binary with the UI embedded, across both arches, reproducibly
and offline. You own the flake, the gomod2nix/npm lockfile hygiene, and CI. You write build config
(Nix, CI YAML, lockfiles) and the embed glue; you never run git.

## Read first
- `/Users/doodla/Code/plundrio/.agents/research/nix-frontend-build-go-embed.md` — the researched
  pattern (two derivations; `buildNpmPackage`+`npmDepsHash` emits `dist/`; fed into
  `buildGoApplication` via `postPatch` into `internal/web/dist`; built ONCE natively and shared
  across arches; hash-regen via `nix run nixpkgs#prefetch-npm-deps`).
- `/Users/doodla/Code/plundrio/.agents/dashboard/plan/design.md` (file layout, `ui/` tree).
- `/Users/doodla/Code/plundrio/flake.nix` — the existing `makePlundrio`/`buildGoApplication` setup,
  the four outputs (native, aarch64, docker, docker-aarch64), and `version`.
- `/Users/doodla/Code/plundrio/CLAUDE.md` — "Build & CI" (Nix-only, gomod2nix regen rule) and
  "Releasing".

## Deliverables
1. **Frontend derivation** in `flake.nix`: `buildNpmPackage` (or `pnpm.fetchDeps`+`pnpmConfigHook`
   if the UI commits `pnpm-lock.yaml`) building `ui/` to `dist/`, with `npmDepsHash` pinned. Built
   with **native `pkgs`** and shared by both Go arches (the frontend is architecture-independent —
   never cross-build the JS).
2. **Wire into `makePlundrio`**: `postPatch` (or equivalent) copies the frontend derivation's
   `dist/` into `internal/web/dist` so `//go:embed all:dist` picks up the real assets. Confirm the
   committed placeholder is overwritten, not appended-to.
3. **Embed directive sanity:** confirm `internal/web/embed.go` uses `//go:embed all:dist` (not
   `dist`) so the bare-checkout placeholder compiles — this was a bounced defect; do not let it
   regress.
4. **Lockfile hygiene + docs:** document the exact `npmDepsHash` regen command (the analog to
   `gomod2nix generate`) in `CLAUDE.md`'s Build & CI section, so the next dep change is a known step.
   If Go deps changed, regenerate `gomod2nix.toml` per the existing rule.
5. **CI** (`.github/workflows/build.yml`): the `go` job must still pass with the embedded UI (the
   placeholder keeps bare `go build`/`go test` working without Nix); the `nix` job builds
   `.#plundrio` with the real UI. Add the frontend build/lint to CI if cheap, but do not break the
   existing three-job shape without reason. Note (do not necessarily change) that `release.yml`'s
   multi-arch matrix will reuse the one native frontend build.

## Sharp edges (from the research) to actively guard
- esbuild/rollup native-binary postinstall cannot phone home in Nix's sandbox — keep the lockfile
  complete from the build platform (the Linux `@esbuild/linux-*` packages must be present, even if
  the lockfile is generated on macOS). A `prefetch`/hash mismatch with no lockfile change signals
  non-determinism — pin node + the pnpm fetcher version if using pnpm.
- Verify `importNpmLock` / the chosen helper actually exists in this flake's pinned nixpkgs before
  relying on it (the research flagged this as version-sensitive).

## Definition of done (your gate)
- `nix build .#plundrio` succeeds and the resulting binary, run with `--dashboard-listen`, serves a
  non-empty `index.html` (the real UI, not the placeholder). Prove it (e.g. `nix build` then curl
  the served root in demo mode, or assert the binary's embedded FS is non-trivial).
- `nix build .#plundrio-aarch64` succeeds reusing the same frontend derivation (no JS cross-build).
- Bare `go build ./...` and `go test ./...` still pass without Nix (placeholder embed).
- The hash-regen step is documented.
Report the exact commands you ran and their results. If the offline build is infeasible with the
chosen stack as-is, STOP and report it as an upstream bounce (it may force a stack reconsideration)
rather than committing a build that only works with network access.
