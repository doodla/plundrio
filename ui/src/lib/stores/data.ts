// The single live-data store. One EventSource to /api/events:
//  - `snapshot` seeds everything on connect (and re-seeds on every reconnect, so
//    no client-side replay is needed)
//  - `transfers` / `account` / `settings` / `log` patch their slices
//  - on error/disconnect the browser EventSource auto-reconnects; we surface a
//    'reconnecting' connection state and keep the last-known data painted
//    (never blank stale values).

import { writable, derived, get } from 'svelte/store';
import type { Account, LogEvent, SettingEntry, Snapshot, Transfer } from '../types';

export type ConnState = 'connecting' | 'live' | 'reconnecting';

// In-memory log cap (Pi memory). Snapshot seeds ~500; we keep a rolling window.
const LOG_CAP = 600;

// Each appended log carries a monotonic id so the viewer can flash only the
// newest arrivals without re-keying the whole list.
export interface LogLine extends LogEvent {
  _id: number;
}

export const transfers = writable<Transfer[]>([]);
export const account = writable<Account | null>(null);
export const settings = writable<SettingEntry[]>([]);
export const logs = writable<LogLine[]>([]);
export const connState = writable<ConnState>('connecting');

let logSeq = 0;
function tag(ev: LogEvent): LogLine {
  return { ...ev, _id: logSeq++ };
}

let es: EventSource | null = null;

function applySnapshot(snap: Snapshot) {
  transfers.set(snap.transfers ?? []);
  account.set(snap.account ?? null);
  settings.set(snap.settings ?? []);
  logs.set((snap.logs ?? []).map(tag));
  connState.set('live');
}

function appendLog(ev: LogEvent) {
  logs.update((cur) => {
    const next = cur.length >= LOG_CAP ? cur.slice(cur.length - LOG_CAP + 1) : cur.slice();
    next.push(tag(ev));
    return next;
  });
}

/** Open the SSE connection. Idempotent — a second call is a no-op. */
export function connect(): void {
  if (es) return;
  connState.set('connecting');
  es = new EventSource('/api/events');

  es.addEventListener('open', () => {
    // The server sends `snapshot` immediately; mark live once bytes flow.
    connState.set('live');
  });

  es.addEventListener('snapshot', (e) => {
    try {
      applySnapshot(JSON.parse((e as MessageEvent).data));
    } catch {
      // malformed snapshot — keep last-known state, wait for the next event.
    }
  });

  es.addEventListener('transfers', (e) => {
    try {
      transfers.set(JSON.parse((e as MessageEvent).data).transfers ?? []);
      connState.set('live');
    } catch {
      /* ignore a single bad frame */
    }
  });

  es.addEventListener('account', (e) => {
    try {
      account.set(JSON.parse((e as MessageEvent).data));
    } catch {
      /* ignore */
    }
  });

  es.addEventListener('settings', (e) => {
    try {
      settings.set(JSON.parse((e as MessageEvent).data).settings ?? []);
    } catch {
      /* ignore */
    }
  });

  es.addEventListener('log', (e) => {
    try {
      appendLog(JSON.parse((e as MessageEvent).data));
    } catch {
      /* ignore */
    }
  });

  // EventSource auto-reconnects on error; surface the lost state and let it
  // recover. A fresh `snapshot` on reconnect re-seeds and flips back to 'live'.
  es.addEventListener('error', () => {
    connState.set('reconnecting');
  });
}

export function disconnect(): void {
  if (es) {
    es.close();
    es = null;
  }
}

// Convenience derived store: are we showing stale data?
export const isStale = derived(connState, ($s) => $s === 'reconnecting');

/** Read the current settings list synchronously (for the settings page init). */
export function currentSettings(): SettingEntry[] {
  return get(settings);
}
