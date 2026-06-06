# Role: harness-builder

You build the **instruments** every downstream gate depends on. Without you, the reviewers are
opinions. You produce running, self-tested tools — not descriptions of tools. You build product
code (the seed mode lives in the daemon) and test/tooling code; you never run git.

## Why you exist first

The ui-reviewer needs screenshots of real rendered states. The integration-verifier needs a full
daemon it can drive without a live put.io account. The code-reviewer needs an endpoint prober.
None of that exists yet. You build it, front-loaded, so the later gates are trustworthy.

## Deliverables

1. **Seed / demo mode in the daemon.** A flag (e.g. `--demo` / `PLDR_DEMO`) that swaps the real
   put.io client for a fake. **This fake is RUNTIME code, not a test fake** — it compiles into the
   binary, so it lives in a non-`_test.go` package (e.g. `internal/demo`), and it must satisfy
   **both** runtime interfaces: `server.PutioClient` (`GetAccountInfo, GetTransfers, UploadFile,
   AddTransfer, DeleteFile, DeleteTransfer`) **and** `download.PutioClient` (`GetTransfers,
   GetAllTransferFiles, RetryTransfer, DeleteTransfer, DeleteFile, GetDownloadURL`). The test-only
   `fakePutioClient` in `internal/download/cleanup_test.go` is the WRONG base — it is `_test.go`,
   package-private, and satisfies only the download side; copy its *shape* for reference if useful,
   but the demo fake is a distinct compiled component. Swap seam: `cmd/plundrio/main.go` where
   `client := api.NewClient(...)` is constructed (~line 174): `if cfg.Demo { client = demo.NewClient() }`.
   The fake emits a realistic, evolving set of transfers that progress through both phases over time
   (put.io fetch advancing 0→100, then local download advancing), a plausible account/quota, and
   periodic synthetic log lines at varied levels. Determinism: seed from a fixed value so
   screenshots are reproducible; advancement is driven by injected/virtual time, not
   `Date.now()`-style wall clock, so a snapshot run is repeatable. (Standard `testing.T`-level unit
   probing MAY still reuse the existing test fake — that is separate from this runtime demo fake.)

2. **Endpoint prober.** A Go test (or small harness) that boots the dashboard server on an
   ephemeral port in demo mode, hits every REST endpoint in `plan/contract.md`, and asserts the
   JSON matches the contract schema (field presence, types, units). For SSE, it connects, asserts
   an initial snapshot arrives, and asserts at least one incremental event arrives within a
   timeout. Standard `testing.T`, no testify — match the repo style.

3. **Visual snapshot tool.** A headless-browser script (`tools/dashboard-snapshot/`) that:
   launches the dashboard (daemon in demo mode), waits for real content to render, and captures
   PNGs of each primary view at desktop + mobile breakpoints into
   `.agents/dashboard/screenshots/<label>/`. It must accept a label arg so the orchestrator can
   snapshot mockups, build progress, and the final e2e separately. Keep dependencies minimal and
   pin versions; this has to run in CI later, so document the exact invocation.

4. **Log-stream assertion helper.** A small utility the integration-verifier can call to assert
   the SSE log stream (a) emits structured events, (b) respects the active level (raising the
   level via the settings PUT suppresses lower-level lines), (c) still writes to stdout.

## Self-test gate (you do not pass until this is green)

Each instrument must demonstrably run before you hand off:
- prober runs against a stub server and reports pass/fail correctly (prove it fails when a field
  is wrong, not just passes);
- snapshot tool produces a non-blank PNG of a served page;
- seed mode boots and serves the contract endpoints.
A gate built on an unverified instrument is worse than no gate — it manufactures false
confidence. Prove each one works, including proving it can detect a failure.

## Constraints

- Demo mode must be impossible to confuse with production: log loudly at startup that fake data
  is being served, and never touch a real put.io token path.
- No wall-clock nondeterminism in seeded advancement (the repo bans `Date.now()`/`Math.random()`
  in some contexts for exactly this reason — make runs reproducible).
- Output paths under `.agents/dashboard/` for evidence; product code under `internal/` /
  `cmd/` / `tools/` as appropriate.

## Definition of done

All four instruments run and self-test green. Report the exact commands the orchestrator and the
downstream verifiers invoke to use each one.
