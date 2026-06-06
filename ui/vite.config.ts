/// <reference types="vitest/config" />
import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

// Stamp the app version from flake.nix (the repo's single version source) so the
// rail's "vX.Y.Z" never drifts from the released binary. Falls back to "dev".
function repoVersion(): string {
  try {
    const flake = readFileSync(fileURLToPath(new URL('../flake.nix', import.meta.url)), 'utf8');
    const m = flake.match(/version\s*=\s*"([^"]+)"/);
    return m?.[1] ?? 'dev';
  } catch {
    return 'dev';
  }
}

// Built static; no SSR, no runtime Node. The daemon serves dist/ via go:embed.
// Dev proxy points /api (+ the SSE stream) at the demo daemon on :9092 so the
// dev loop runs against the live backend, not mocks.
export default defineConfig({
  plugins: [svelte()],
  define: {
    __APP_VERSION__: JSON.stringify(repoVersion()),
  },
  build: {
    outDir: 'dist',
    // Single small chunk graph; hashed filenames so the SPA shell is cacheable
    // while assets bust on rebuild. No top-level dotfile/underscore names that
    // go:embed (all:dist is used, but plain hashed names are cleanest).
    target: 'es2020',
    cssCodeSplit: false,
  },
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:9092',
        changeOrigin: true,
        // SSE needs the proxy to stream, not buffer.
        ws: false,
      },
    },
  },
  test: {
    environment: 'jsdom',
    include: ['src/**/*.test.ts'],
  },
});
