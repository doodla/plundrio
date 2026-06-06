<script lang="ts">
  // Top rail: brand, nav, clock, live/reconnecting pill, palette picker, theme
  // toggle. The picker + toggle are PURELY client-side browser prefs. SPEC §5.
  import { route, navigate, type Surface } from '../stores/route';
  import { connState } from '../stores/data';
  import { mode, palette, setMode, setPalette, PALETTES, type Palette } from '../stores/theme';
  import { railClock } from '../util/format';

  let { version, clock }: { version: string; clock: string } = $props();

  const nav: { id: Surface; label: string }[] = [
    { id: 'deck', label: 'Deck' },
    { id: 'account', label: 'Account' },
    { id: 'logs', label: 'Logs' },
    { id: 'settings', label: 'Settings' },
  ];

  // Per-palette local accent swatch so the picker previews the palette's color.
  const swatch: Record<Palette, string> = {
    tide: '#2fd9c0',
    ember: '#ff8a6b',
    ion: '#8b7cff',
  };
</script>

<div class="rail">
  <div class="brand">
    <span class="sig"></span>
    <b>plundrio</b>
    <span class="v">v{version}</span>
  </div>

  <nav aria-label="Sections">
    {#each nav as n (n.id)}
      <button class:on={$route === n.id} onclick={() => navigate(n.id)}>{n.label}</button>
    {/each}
  </nav>

  <div class="railR">
    <span class="clock num">{clock || railClock()}</span>

    {#if $connState === 'reconnecting'}
      <span class="live reconnect" role="status"><span class="d"></span> RECONNECTING</span>
    {:else if $connState === 'live'}
      <span class="live" role="status"><span class="d"></span> STREAM LIVE</span>
    {:else}
      <span class="live reconnect" role="status"><span class="d"></span> CONNECTING</span>
    {/if}

    <div class="palpick" role="group" aria-label="Palette">
      {#each PALETTES as p (p.id)}
        <button
          class:on={$palette === p.id}
          aria-pressed={$palette === p.id}
          title="Palette: {p.label}"
          onclick={() => setPalette(p.id)}
        >
          <span class="sw" style="background:{swatch[p.id]}"></span>
          <span class="txt">{p.label}</span>
        </button>
      {/each}
    </div>

    <div class="toggle" role="group" aria-label="Theme">
      <button
        class:on={$mode === 'light'}
        aria-pressed={$mode === 'light'}
        title="Light"
        onclick={() => setMode('light')}>☀</button
      >
      <button
        class:on={$mode === 'dark'}
        aria-pressed={$mode === 'dark'}
        title="Dark"
        onclick={() => setMode('dark')}>☾</button
      >
    </div>
  </div>
</div>
