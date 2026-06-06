// Theme = palette (tide|ember|ion) × mode (dark|light). PURELY client-side: the
// viewer's choice is a browser preference (localStorage + URL param), never a
// daemon setting. The pre-paint <head> script already resolved + applied the
// attributes (no flash); this store reads them back, then mutates + persists on
// user action.

import { writable } from 'svelte/store';

export type Mode = 'dark' | 'light';
export type Palette = 'tide' | 'ember' | 'ion';

export const PALETTES: { id: Palette; label: string }[] = [
  { id: 'ion', label: 'ion' },
  { id: 'tide', label: 'tide' },
  { id: 'ember', label: 'ember' },
];

function readAttr<T extends string>(name: string, fallback: T): T {
  const v = document.documentElement.getAttribute(name);
  return (v as T) || fallback;
}

export const mode = writable<Mode>(readAttr<Mode>('data-theme', 'dark'));
export const palette = writable<Palette>(readAttr<Palette>('data-palette', 'ion'));

function syncUrl(param: string, value: string) {
  try {
    const url = new URL(location.href);
    url.searchParams.set(param, value);
    history.replaceState(null, '', url);
  } catch {
    // history not available (e.g. file://) — attribute + storage still applied.
  }
}

export function setMode(next: Mode) {
  mode.set(next);
  document.documentElement.setAttribute('data-theme', next);
  try {
    localStorage.setItem('pldr-mode', next);
  } catch {
    // storage blocked — the in-memory store still drives this session.
  }
  syncUrl('theme', next);
}

export function setPalette(next: Palette) {
  palette.set(next);
  document.documentElement.setAttribute('data-palette', next);
  try {
    localStorage.setItem('pldr-palette', next);
  } catch {
    // storage blocked — the in-memory store still drives this session.
  }
  syncUrl('palette', next);
}
