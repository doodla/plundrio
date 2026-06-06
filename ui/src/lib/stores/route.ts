// Minimal client router. The daemon serves index.html for every non-/api path,
// so deep links work; this store maps the path to one of the four surfaces and
// pushes history on nav. No router dependency (lean bundle, Pi target).

import { writable } from 'svelte/store';

export type Surface = 'deck' | 'account' | 'logs' | 'settings';

const PATHS: Record<string, Surface> = {
  '/': 'deck',
  '/deck': 'deck',
  '/account': 'account',
  '/logs': 'logs',
  '/settings': 'settings',
};

function fromLocation(): Surface {
  return PATHS[location.pathname] ?? 'deck';
}

export const route = writable<Surface>(fromLocation());

export function navigate(surface: Surface): void {
  const path = surface === 'deck' ? '/' : `/${surface}`;
  if (location.pathname !== path) {
    // Preserve theme/palette query params across navigation.
    const url = new URL(location.href);
    url.pathname = path;
    history.pushState({ surface }, '', url);
  }
  route.set(surface);
}

// Back/forward buttons.
window.addEventListener('popstate', () => route.set(fromLocation()));
