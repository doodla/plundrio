#!/usr/bin/env node
// dashboard-snapshot — capture PNGs of a URL/file at desktop + mobile
// breakpoints, into .agents/dashboard/screenshots/<label>/.
//
// WHY this shape: the snapshot tool feeds three downstream gates — the
// ui-reviewer (mockups), build-progress checks, and the final e2e — so it MUST
// take a (target, label) pair and write reproducible PNGs at fixed breakpoints.
//
// WHY zero npm dependencies: it has to run in CI later and OFFLINE (the mockups
// inline their CSS; nothing fetches the network). It drives the ALREADY-INSTALLED
// Chrome over the Chrome DevTools Protocol using only Node built-ins (global
// WebSocket + fetch, Node >= 22). No Playwright/Puppeteer, no bundled browser
// download, no node_modules. The only external requirement is a Chrome/Chromium
// binary, located via $CHROME_PATH or the platform defaults below.
//
// Pinned versions (documented, not vendored):
//   - Node    : >= 22 (global WebSocket). Verified on Node 24.
//   - Chrome  : any recent stable. Verified on Google Chrome 149 (headless=new).
//
// Usage:
//   node tools/dashboard-snapshot/snapshot.mjs <target> <label> [--selector <css>] [--settle <ms>]
//
//   <target>   a URL (http://…) or a local path / file:// to an HTML file.
//   <label>    subdirectory under .agents/dashboard/screenshots/ to write into.
//   --selector wait until this CSS selector exists before capturing (for the
//              live SPA; mockups need only the load event). Optional.
//   --settle   extra milliseconds to wait after load/selector before capture
//              (lets transitions/fonts settle). Default 400.
//
// Examples:
//   node tools/dashboard-snapshot/snapshot.mjs \
//     .agents/dashboard/mockups/console/index.html console
//   node tools/dashboard-snapshot/snapshot.mjs \
//     http://127.0.0.1:9092/ e2e --selector "[data-transfer-row]" --settle 800
//
// Output: .agents/dashboard/screenshots/<label>/desktop.png and mobile.png
// Exit code 0 on success; non-zero (with a message) on any failure.

import { spawn } from "node:child_process";
import { mkdir, rm, writeFile, stat } from "node:fs/promises";
import { existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve, isAbsolute } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const BREAKPOINTS = [
  { name: "desktop", width: 1440, height: 900, scale: 1, mobile: false },
  { name: "mobile", width: 390, height: 844, scale: 2, mobile: true }, // iPhone 12-ish
];

// Repo root = three levels up from this file (tools/dashboard-snapshot/snapshot.mjs).
const __dirname = fileURLToPath(new URL(".", import.meta.url));
const REPO_ROOT = resolve(__dirname, "..", "..");
const SCREENSHOT_ROOT = join(REPO_ROOT, ".agents", "dashboard", "screenshots");

function die(msg) {
  console.error(`dashboard-snapshot: ${msg}`);
  process.exit(1);
}

function parseArgs(argv) {
  const positional = [];
  const opts = { selector: null, settle: 400 };
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (a === "--selector") opts.selector = argv[++i];
    else if (a === "--settle") opts.settle = parseInt(argv[++i], 10);
    else if (a.startsWith("--")) die(`unknown flag ${a}`);
    else positional.push(a);
  }
  if (positional.length < 2) {
    die("usage: snapshot.mjs <target-url-or-path> <label> [--selector css] [--settle ms]");
  }
  return { target: positional[0], label: positional[1], ...opts };
}

// resolveTargetURL turns a CLI target into a navigable URL. http(s):// and
// file:// pass through; anything else is treated as a local path (relative to
// CWD) and converted to a file:// URL after asserting it exists.
async function resolveTargetURL(target) {
  if (/^https?:\/\//.test(target) || target.startsWith("file://")) return target;
  const abs = isAbsolute(target) ? target : resolve(process.cwd(), target);
  if (!existsSync(abs)) die(`target file does not exist: ${abs}`);
  return pathToFileURL(abs).href;
}

function findChrome() {
  if (process.env.CHROME_PATH && existsSync(process.env.CHROME_PATH)) {
    return process.env.CHROME_PATH;
  }
  const candidates = [
    "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
    "/Applications/Chromium.app/Contents/MacOS/Chromium",
    "/usr/bin/google-chrome-stable",
    "/usr/bin/google-chrome",
    "/usr/bin/chromium",
    "/usr/bin/chromium-browser",
  ];
  for (const c of candidates) if (existsSync(c)) return c;
  die("no Chrome/Chromium binary found; set $CHROME_PATH");
}

// launchChrome starts headless Chrome with an ephemeral debugging port and
// resolves once the DevTools WebSocket URL appears on stderr.
async function launchChrome(chromePath) {
  const userDataDir = await mkdtempSafe();
  const args = [
    "--headless=new",
    "--disable-gpu",
    "--no-first-run",
    "--no-default-browser-check",
    "--disable-extensions",
    "--hide-scrollbars",
    "--remote-debugging-port=0",
    `--user-data-dir=${userDataDir}`,
    "about:blank",
  ];
  const proc = spawn(chromePath, args, { stdio: ["ignore", "ignore", "pipe"] });

  const wsURL = await new Promise((resolveWs, rejectWs) => {
    let buf = "";
    const timer = setTimeout(() => rejectWs(new Error("timed out waiting for Chrome DevTools port")), 20000);
    proc.stderr.on("data", (d) => {
      buf += d.toString();
      const m = buf.match(/ws:\/\/[^\s]+/);
      if (m) {
        clearTimeout(timer);
        resolveWs(m[0]);
      }
    });
    proc.on("exit", (code) => {
      clearTimeout(timer);
      rejectWs(new Error(`Chrome exited early (code ${code}): ${buf}`));
    });
  });

  return { proc, wsURL, userDataDir };
}

async function mkdtempSafe() {
  const dir = join(tmpdir(), `dashsnap-${process.pid}-${Date.now()}`);
  await mkdir(dir, { recursive: true });
  return dir;
}

// CDP is a minimal Chrome DevTools Protocol client over a single WebSocket.
// Each command gets an incrementing id; the matching response resolves its
// promise. Events are dispatched to per-method listeners.
class CDP {
  constructor(ws) {
    this.ws = ws;
    this.id = 0;
    this.pending = new Map();
    this.listeners = new Map();
    ws.addEventListener("message", (ev) => {
      const msg = JSON.parse(ev.data);
      if (msg.id !== undefined && this.pending.has(msg.id)) {
        const { resolve, reject } = this.pending.get(msg.id);
        this.pending.delete(msg.id);
        if (msg.error) reject(new Error(`${msg.error.message} (${JSON.stringify(msg.params || {})})`));
        else resolve(msg.result);
      } else if (msg.method) {
        const l = this.listeners.get(msg.method);
        if (l) l(msg.params);
      }
    });
  }
  static async connect(wsURL) {
    const ws = new WebSocket(wsURL);
    await new Promise((res, rej) => {
      ws.addEventListener("open", res, { once: true });
      ws.addEventListener("error", () => rej(new Error("CDP websocket error")), { once: true });
    });
    return new CDP(ws);
  }
  send(method, params = {}, sessionId) {
    const id = ++this.id;
    const payload = { id, method, params };
    if (sessionId) payload.sessionId = sessionId;
    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject });
      this.ws.send(JSON.stringify(payload));
    });
  }
  on(method, fn) {
    this.listeners.set(method, fn);
  }
  close() {
    this.ws.close();
  }
}

// captureBreakpoint navigates a fresh page at one breakpoint and returns the
// screenshot bytes. A new target per breakpoint keeps viewport/emulation state
// from leaking between captures.
async function captureBreakpoint(browser, url, bp, { selector, settle }) {
  // Create a page target and attach a flat session to it.
  const { targetId } = await browser.send("Target.createTarget", { url: "about:blank" });
  const { sessionId } = await browser.send("Target.attachToTarget", { targetId, flatten: true });

  const sess = {
    send: (method, params) => browser.send(method, params, sessionId),
  };

  await sess.send("Page.enable");
  await sess.send("Runtime.enable");
  await sess.send("Emulation.setDeviceMetricsOverride", {
    width: bp.width,
    height: bp.height,
    deviceScaleFactor: bp.scale,
    mobile: bp.mobile,
  });

  // Navigate and wait for the load event.
  const loaded = new Promise((res) => browser.on("Page.loadEventFired", res));
  await sess.send("Page.navigate", { url });
  await Promise.race([loaded, sleep(15000)]);

  // Optionally wait for a selector to exist (live SPA content).
  if (selector) {
    await waitForSelector(sess, selector, 15000);
  }
  // Settle delay for fonts/transitions.
  await sleep(settle);

  const { data } = await sess.send("Page.captureScreenshot", { format: "png", captureBeyondViewport: false });

  await browser.send("Target.closeTarget", { targetId });
  return Buffer.from(data, "base64");
}

async function waitForSelector(sess, selector, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  const expr = `document.querySelector(${JSON.stringify(selector)}) !== null`;
  while (Date.now() < deadline) {
    const { result } = await sess.send("Runtime.evaluate", { expression: expr, returnByValue: true });
    if (result && result.value === true) return;
    await sleep(150);
  }
  throw new Error(`selector ${selector} did not appear within ${timeoutMs}ms`);
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function main() {
  const { target, label, selector, settle } = parseArgs(process.argv.slice(2));
  const url = await resolveTargetURL(target);
  const chromePath = findChrome();

  const outDir = join(SCREENSHOT_ROOT, label);
  await mkdir(outDir, { recursive: true });

  const { proc, wsURL, userDataDir } = await launchChrome(chromePath);
  let browser;
  try {
    browser = await CDP.connect(wsURL);
    for (const bp of BREAKPOINTS) {
      const png = await captureBreakpoint(browser, url, bp, { selector, settle });
      const outPath = join(outDir, `${bp.name}.png`);
      await writeFile(outPath, png);
      const { size } = await stat(outPath);
      if (size < 256) throw new Error(`captured ${bp.name}.png is suspiciously small (${size} bytes) — likely blank`);
      console.log(`wrote ${outPath} (${bp.width}x${bp.height}, ${size} bytes)`);
    }
  } finally {
    if (browser) browser.close();
    proc.kill("SIGKILL");
    await rm(userDataDir, { recursive: true, force: true }).catch(() => {});
  }
}

main().catch((err) => die(err.stack || String(err)));
