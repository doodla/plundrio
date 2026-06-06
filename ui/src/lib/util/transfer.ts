// Client-side derivations the contract intentionally does NOT supply (SPEC §9):
// the display state, the human phase label, aggregate throughput / active-count,
// and the flow-gauge geometry. Pure functions, unit-tested.

import type { Transfer } from '../types';

// Display state drives every gauge/row variant (SPEC §3.2). It is derived from
// lifecycle_state + local_phase presence + which phase's rate is non-zero +
// permanent — NOT a 1:1 of lifecycle_state.
export type DisplayState =
  | 'queued'
  | 'putio-fetching'
  | 'local-downloading'
  | 'completed'
  | 'failed';

/**
 * Derive the display state per SPEC §3.2.
 *
 * Precedence:
 *  - failed (red) when `error` is set. Per the contract's error precedence,
 *    `error == (error_string != "")`, which is true for BOTH a permanent
 *    download cascade giving up AND a put.io-side ERROR (no ctx). A *transient*
 *    Failed (still retrying) reports `error:false` and so renders as its
 *    underlying phase — never red. `permanent` then only refines the chip
 *    wording (see TransferHero/FleetRow), not whether we go red.
 *  - completed (Completed/Processed, or no-ctx COMPLETED/SEEDING, percent==1)
 *  - local-downloading (Downloading with a local_phase, local fraction < 1)
 *  - put.io-fetching (no local_phase yet and fetch < 100%)
 *  - queued (Initial/None at 0%, or IN_QUEUE at 0%)
 */
export function displayState(t: Transfer): DisplayState {
  if (t.error) return 'failed';

  const completedByState = t.lifecycle_state === 'Completed' || t.lifecycle_state === 'Processed';
  const completedNoCtx =
    t.local_phase === null && (t.putio_status === 'COMPLETED' || t.putio_status === 'SEEDING');
  if (completedByState || completedNoCtx || t.percent_done >= 1) return 'completed';

  if (t.local_phase !== null && t.lifecycle_state === 'Downloading') {
    return 'local-downloading';
  }

  // No local context yet: still in put.io's fetch phase. Queued when nothing has
  // moved (Initial/None/IN_QUEUE at 0%), otherwise actively fetching.
  const fetchStarted = t.putio_phase.percent_done > 0;
  const queuedState =
    t.lifecycle_state === 'Initial' ||
    t.lifecycle_state === 'None' ||
    t.putio_status === 'IN_QUEUE';
  if (!fetchStarted && queuedState) return 'queued';

  return 'putio-fetching';
}

/**
 * Human phase label (SPEC §9.2) — the chip/status text the contract omits.
 */
export function humanLabel(state: DisplayState): string {
  switch (state) {
    case 'queued':
      return 'queued';
    case 'putio-fetching':
      return 'fetching at put.io';
    case 'local-downloading':
      return 'local download';
    case 'completed':
      return 'completed';
    case 'failed':
      return 'failed';
  }
}

/**
 * Is the transfer's active phase actually moving? Stall semantics (SPEC §6):
 * when the active-phase rate is 0, motion stops (a frozen instrument == no
 * throughput, which is information).
 */
export function isActive(t: Transfer, state: DisplayState): boolean {
  if (state === 'putio-fetching') return t.putio_phase.rate_download > 0;
  if (state === 'local-downloading') return (t.local_phase?.rate_download ?? 0) > 0;
  return false;
}

/** Combined download rate across both phases (bytes/sec). */
export function combinedRate(t: Transfer): number {
  return t.putio_phase.rate_download + (t.local_phase?.rate_download ?? 0);
}

/**
 * Local fraction (0–1): how far past the 50% handoff we are.
 * Prefer the local_phase downloaded/total ratio when present; fall back to the
 * combined percent_done identity (percent_done*2 - 1) per SPEC §3.1.
 */
export function localFraction(t: Transfer): number {
  if (t.local_phase && t.local_phase.total > 0) {
    return clamp01(t.local_phase.downloaded / t.local_phase.total);
  }
  return clamp01(t.percent_done * 2 - 1);
}

function clamp01(x: number): number {
  if (x < 0) return 0;
  if (x > 1) return 1;
  return x;
}

// --- aggregate derivations (SPEC §9.1) -------------------------------------

export interface Aggregate {
  activeCount: number; // non-terminal lifecycle (Initial|Downloading)
  queuedCount: number; // queued display state
  stalledCount: number; // active display state but rate==0
  putioRate: number; // bytes/sec
  localRate: number; // bytes/sec
  totalRate: number; // bytes/sec
}

export function aggregate(transfers: Transfer[]): Aggregate {
  let activeCount = 0;
  let queuedCount = 0;
  let stalledCount = 0;
  let putioRate = 0;
  let localRate = 0;
  for (const t of transfers) {
    if (t.lifecycle_state === 'Initial' || t.lifecycle_state === 'Downloading') activeCount++;
    const st = displayState(t);
    if (st === 'queued') queuedCount++;
    if ((st === 'putio-fetching' || st === 'local-downloading') && !isActive(t, st)) stalledCount++;
    putioRate += t.putio_phase.rate_download;
    localRate += t.local_phase?.rate_download ?? 0;
  }
  return {
    activeCount,
    queuedCount,
    stalledCount,
    putioRate,
    localRate,
    totalRate: putioRate + localRate,
  };
}

/**
 * Pick the focal (headline) transfer: prefer a local-downloading one, else a
 * put.io-fetching one, else the most-recently-created (SPEC §3.3). All transfers
 * still appear in the fleet; the focal is the enlarged headline.
 */
export function focalTransfer(transfers: Transfer[]): Transfer | null {
  if (transfers.length === 0) return null;
  const byState = (want: DisplayState) => transfers.filter((t) => displayState(t) === want);
  const local = byState('local-downloading');
  if (local.length > 0) return mostRecent(local);
  const fetching = byState('putio-fetching');
  if (fetching.length > 0) return mostRecent(fetching);
  return mostRecent(transfers);
}

function mostRecent(transfers: Transfer[]): Transfer {
  return [...transfers].sort((a, b) => {
    const ta = a.created_at ? new Date(a.created_at).getTime() : 0;
    const tb = b.created_at ? new Date(b.created_at).getTime() : 0;
    return tb - ta;
  })[0];
}

// --- flow-gauge geometry (SPEC §3.1) ---------------------------------------

// Full 270° arc on the hero (r=84): 2·π·84·0.75.
export const ARC_FULL = 395.8;
export const ARC_HALF = ARC_FULL / 2; // 197.9 — one half-leg = 50% combined
// Mini gauge (r=24): 2·π·24·0.75.
export const MINI_FULL = 113.1;
export const MINI_HALF = MINI_FULL / 2; // 56.5

export interface GaugeArcs {
  p1Len: number; // put.io leg dash length
  p2Len: number; // local leg dash length (offset by -half to start at handoff)
}

/**
 * Arc dash lengths for a transfer at a given gauge scale.
 * put.io leg = min(putio%,100)/100 · half.
 * local leg  = localFraction · half.
 */
export function gaugeArcs(t: Transfer, full = ARC_FULL): GaugeArcs {
  const half = full / 2;
  const putioPct = Math.min(t.putio_phase.percent_done, 100) / 100;
  return {
    p1Len: putioPct * half,
    p2Len: localFraction(t) * half,
  };
}

/**
 * Comet head position along the 270° arc for the active leg's leading edge.
 * Returns left/top offsets (px) relative to the gauge center, accounting for the
 * 135° SVG rotation (gap at bottom, sweep starts bottom-left). radius is the
 * gauge's pixel radius (stroke centerline).
 */
export function cometPosition(
  state: DisplayState,
  t: Transfer,
  radiusPx: number,
): { x: number; y: number } | null {
  let frac: number; // 0..1 along the full 270° sweep
  if (state === 'putio-fetching') {
    frac = (Math.min(t.putio_phase.percent_done, 100) / 100) * 0.5;
  } else if (state === 'local-downloading') {
    frac = 0.5 + localFraction(t) * 0.5;
  } else {
    return null; // no comet for queued/completed/failed
  }
  // The arc spans 270°, rotated so it begins bottom-left. In the unrotated SVG
  // the arc starts at angle 0 (3 o'clock) sweeping 270° clockwise; the wrapper
  // is rotated 135°. Net start angle = 135°, sweeping +270°.
  const startDeg = 135;
  const sweepDeg = 270;
  const angle = ((startDeg + frac * sweepDeg) * Math.PI) / 180;
  return {
    x: Math.cos(angle) * radiusPx,
    y: Math.sin(angle) * radiusPx,
  };
}
