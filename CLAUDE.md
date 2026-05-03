# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What is plundrio?

plundrio is a put.io download client that integrates with the *arr stack (Sonarr, Radarr, Lidarr) by implementing the Transmission RPC protocol. It acts as a bridge: *arr apps send download requests via Transmission RPC, plundrio uploads torrents/magnets to put.io, monitors transfers, downloads completed files locally, and reports progress back to the *arr apps.

## Build & CI

This project uses **Nix flakes** exclusively for building — there is no Makefile or goreleaser.

```bash
# Build native binary
nix build .#plundrio

# Build for aarch64
nix build .#plundrio-aarch64

# Build Docker images
nix build .#plundrio-docker
nix build .#plundrio-docker-aarch64

# Enter dev shell (Go, gopls, golangci-lint)
nix develop

# Run directly with Go (during development)
go build ./cmd/plundrio && ./plundrio run --help
```

**CI** (`.github/workflows/build.yml`) runs three jobs on every push/PR:

- `go` — `go build` and `go test -race` against the version in `go.mod`.
- `lint` — `golangci-lint` (default linters; no `.golangci.yml` yet).
- `nix` — `nix build .#plundrio` (native only — full multi-arch matrix runs at release time via `release.yml`, which fires `on: release: published`).

`release.yml` builds all four targets (native, aarch64, docker, docker-aarch64) at release publish.

**Important**: When Go dependencies change (`go.mod`/`go.sum`), regenerate `gomod2nix.toml` — `nix develop -c gomod2nix generate` from the repo root, or `nix run github:nix-community/gomod2nix` if not in the dev shell. The flake reads `modules = ./gomod2nix.toml` (no `vendorHash` — `buildGoApplication` resolves modules from the lockfile).

## Fork-only

All work — branches, commits, PRs, issues, releases — happens on `doodla/plundrio`. We do not contribute back to `elsbrock/plundrio`. The `upstream` remote exists for rebasing only. When filing issues or opening PRs with `gh`, always pass `--repo doodla/plundrio` (the gotcha below explains why).

## Issue labels

The label scheme is shaped for Claude-as-maintainer (no human team coordination), so it omits things humans expect (priority labels, size estimates) and adds things only a cold-starting agent needs (decision-state, context-blocked).

**Area** — `area:download`, `area:server`, `area:api`, `area:config`, `area:ci`, `area:nixos`. Multi-select. Used to load relevant context, not to route work. Apply one or more to every issue.

**Workflow state** — sparse, high-signal:

- `urgent` — drop everything. Pair with GitHub's pin feature (max 3 pinned). Filed in priority order, so issue numbers also encode rough priority — `urgent` is just for the top of the heap.
- `needs:decision` — the body lists real options and the operator must pick before Claude implements. Don't apply when the body recommends an option and the alternatives are minor; only when the choice is genuinely open.
- `needs:context` — Claude started, hit a question, parked the work. Waiting on operator response. Removed when work resumes.

**Type** — keep GitHub defaults (`bug`, `enhancement`, `documentation`) plus `security` for issues with security implications (different urgency than a generic bug).

**Don't add:**

- `P0`/`P1`/`P2`/`P3` — issue numbers + `urgent` + pinning cover priority. A parallel scheme rots.
- `size:*` — scope is in the issue body. Effort is unknowable until the work starts.
- `status:*` — open/closed + assignee + the workflow labels above are enough.

When filing a new issue: at least one `area:*` and a type. Add `urgent` only if it's actually urgent. Add `needs:decision` only if the operator must choose between real options before any code can be written.

## Releasing

This repo is a fork of `elsbrock/plundrio` deployed from `ghcr.io/doodla/plundrio:<tag>`. The release process is governed by [homelab ADR 0017](https://github.com/doodla/homelab/blob/main/docs/decisions/0017-plundrio-fork.md).

**Tag scheme.** Track upstream's version when rebased clean; bump to a **higher** version than upstream when the fork carries forward-only patches. **No suffix** (`-doodla.N` etc.) — the `ghcr.io/doodla/…` namespace communicates fork status, and a suffix fragments the tag space and forces homelab's compose pin into a non-standard form. If upstream later releases the version we already used, leapfrog further. Diverging from upstream's number line is expected.

**Procedure:**

1. Update `version` in `flake.nix` (line 14) to the next version (no `v` prefix). Commit `chore: bump version to X.Y.Z`.
2. `git tag vX.Y.Z` and `git push origin main && git push origin vX.Y.Z`.
3. **Create the GitHub release** explicitly: `gh release create vX.Y.Z --repo doodla/plundrio --notes "..."`. Pushing the tag is not enough — `release.yml` only runs `on: release: published`, so without the explicit release the image build never fires.
4. Watch the workflow: `gh run watch <run-id> --repo doodla/plundrio --exit-status`. ~7–10 min based on prior runs.
5. Image lands as a multi-arch manifest at `ghcr.io/doodla/plundrio:vX.Y.Z` (amd64 + arm64).

**Default `gh` repo gotcha.** This clone has both `origin` (the fork) and `upstream` (elsbrock) remotes. `gh release create` without `--repo doodla/plundrio` defaults to upstream and errors with `tag … has not been pushed to elsbrock/plundrio`. Always pass `--repo doodla/plundrio` for fork-side releases.

**Correcting a bad release.** If a wrong tag was pushed:

1. `gh release delete vBAD --repo doodla/plundrio --yes`
2. `git tag -d vBAD && git push origin :refs/tags/vBAD`
3. Bump `flake.nix` again, new commit (do **not** force-push main — leave the bump-and-correct sequence in history; the linear log is more honest than a rewritten one), retag, redo the release.

The GHCR image at the bad tag stays in the registry but is harmless if no one references it. Delete optionally via `gh api -X DELETE /user/packages/container/plundrio/versions/<id>`.

## Architecture

```
cmd/plundrio/main.go    Entry point, CLI (cobra + viper), wires everything together
internal/
  config/               Config struct (TargetDir, FolderID, OAuthToken, ListenAddr, WorkerCount)
  api/                  Put.io API client wrapper (uploads, transfers, files, auth)
  server/               Transmission RPC server (HTTP on :9091)
  download/             Download manager, transfer coordinator, worker pool
  log/                  Zerolog wrapper with component-based logging
```

### Request Flow

1. **Inbound**: *arr app sends Transmission RPC to `server/handlers.go` which routes to `torrent.go` handlers
2. **torrent-add**: Uploads `.torrent` or adds magnet to put.io folder (`cfg.FolderID`)
3. **Monitoring**: `Manager.monitorTransfers()` polls put.io every 30s, `TransferProcessor.checkTransfers()` categorizes transfers by status
4. **Download**: Ready transfers get files queued as `downloadJob`s, processed by worker pool via `grab` library
5. **Coordination**: `TransferCoordinator` tracks lifecycle states (Initial -> Downloading -> Completed -> Processed), `TransferContext` holds per-transfer state
6. **Cleanup**: On completion, cleanup hook deletes source file from put.io but keeps transfer record for *arr visibility
7. **torrent-remove**: *arr app requests removal; plundrio deletes put.io file + transfer

### Progress Reporting

Progress is split 50/50: put.io download (0-50%) + local download (50-100%). This is calculated in `handleTorrentGet` and reported via standard Transmission fields.

### Transfer Lifecycle States

`TransferLifecycleState` in `types.go`: Initial -> Downloading -> Completed -> Processed (or Failed/Cancelled). The "Processed" state means files are downloaded and put.io source cleaned up; the transfer record stays for *arr to query until `torrent-remove`.

### Key Types

- `Manager` (`manager.go`): Orchestrates workers, monitor loop, coordinator
- `TransferCoordinator` (`coordinator.go`): State machine for transfer lifecycle, cleanup hooks
- `TransferProcessor` (`transfers.go`): Categorizes and processes put.io transfers, handles retries
- `TransferContext` (`types.go`): Per-transfer state (files, progress, bytes)
- `Server` (`server.go`): HTTP server implementing Transmission RPC subset

## Configuration

Environment prefix: `PLDR_` (e.g., `PLDR_TOKEN`, `PLDR_TARGET`, `PLDR_FOLDER`). Config file via `--config`. Flags override env vars override config file.

## Testing

Unit tests live alongside their packages (e.g. `internal/download/coordinator_test.go`, `transfers_test.go`, `cleanup_test.go`). No integration tests against live put.io. Style: standard `testing.T`, no testify or other assertion libs — match the existing files. The `fakePutioClient` in `cleanup_test.go` is the shared in-memory fake; extend it rather than introducing a parallel one.

## Security model

Several choices in the codebase assume the operator runs plundrio behind a private network boundary (e.g. on an internal Docker network reachable only from the *arr peers, with no published port). Don't "harden" these without first confirming the deployment still has that boundary:

- `handleRPC` accepts any non-empty session ID and has no HTTP Basic auth (`internal/server/handlers.go`). Network isolation is the auth substitute. **If `:9091` is ever bound to a LAN-reachable interface, this is exploitable** — anyone on the network can drive `torrent-add`/`torrent-remove` against the operator's put.io account.
- `http.Server` is constructed without `ReadHeaderTimeout` etc. (`server.go`). Same story: slowloris is irrelevant on an internal interface, but would matter on a public bind.
- The Docker image runs as root with no PUID/PGID handling. Works when the peer containers it shares a volume with are equally permissive; not a safe default for arbitrary deploys.
