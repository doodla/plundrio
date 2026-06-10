# dashboard-snapshot

Captures PNGs of a URL or local HTML file at desktop + mobile breakpoints into
`.agents/dashboard/screenshots/<label>/`. Used by the dashboard build to
snapshot mockups, build progress, and the final e2e separately (that's why
`<label>` is required).

## Requirements

- **Node ≥ 22** (uses the global `WebSocket` + `fetch`; verified on Node 24).
- **Chrome or Chromium** binary. Located via `$CHROME_PATH`, else platform
  defaults (macOS `Google Chrome.app`, Linux `google-chrome`/`chromium`).
  Verified on Google Chrome 149 (`--headless=new`).

No npm install. **No `node_modules`, no bundled browser, runs offline** — it
drives the already-installed Chrome over the DevTools Protocol with Node
built-ins only. The mockups inline their CSS, so nothing touches the network.

## Invocation

```bash
node tools/dashboard-snapshot/snapshot.mjs <target> <label> [--selector <css>] [--settle <ms>] [--full]
```

- `<target>` — a URL (`http://…`) or a local path / `file://` to an HTML file.
- `<label>`  — subdirectory under `.agents/dashboard/screenshots/`.
- `--selector <css>` — wait until this selector exists before capturing (for the
  live SPA; mockups need only the load event). Optional.
- `--settle <ms>` — extra wait after load/selector before capture (fonts,
  transitions). Default `400`.
- `--full` — capture the **entire scroll height** instead of just the breakpoint
  viewport. Use it for the long single-scroll mockups (account → transfers →
  logs → settings stacked) so the operator sees each direction's whole design,
  not just the fold. Opt-in: the **default is viewport-only**, which is what the
  live SPA e2e wants. Height is capped at 20000px.

Writes `<label>/desktop.png` and `<label>/mobile.png`. Default heights are the
viewport (1440×900 / 390×844 @2x); with `--full` the height is the measured
content height (width unchanged). Exits non-zero with a message on failure,
including a blank-PNG guard (capture smaller than 256 bytes fails).

## Examples

```bash
# A long single-scroll mockup — full page so all four surfaces are captured:
node tools/dashboard-snapshot/snapshot.mjs \
  .agents/dashboard/mockups/console/index.html console --full

# The live demo-mode dashboard (M6), viewport-only, waiting for a transfer row:
node tools/dashboard-snapshot/snapshot.mjs \
  http://127.0.0.1:9092/ e2e --selector "[data-transfer-row]" --settle 800
```

## Self-test

```bash
node tools/dashboard-snapshot/snapshot.mjs \
  tools/dashboard-snapshot/selftest.html _selftest --selector "[data-snapshot-ready]"
# -> .agents/dashboard/screenshots/_selftest/{desktop,mobile}.png (non-blank)
```

`selftest.html` is a tiny self-contained page proving the tool produces a
non-blank PNG. Delete the `_selftest` output after verifying.
