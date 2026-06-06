<script lang="ts">
  // The signature component: ONE circular instrument — a 270° dial that is also
  // the put.io→local journey. put.io leg (steel) first 135°, handoff node at 12
  // o'clock = 50% combined, local leg (accent) second 135°, comet on the active
  // leg's leading edge, combined % in the center. SPEC §3.
  import type { Transfer } from '../types';
  import {
    ARC_FULL,
    cometPosition,
    displayState,
    gaugeArcs,
    isActive,
    type DisplayState,
  } from '../util/transfer';
  import { humanRate } from '../util/format';

  let { transfer }: { transfer: Transfer } = $props();

  const state = $derived<DisplayState>(displayState(transfer));
  const arcs = $derived(gaugeArcs(transfer, ARC_FULL));
  const active = $derived(isActive(transfer, state));
  const pct = $derived(Math.round(transfer.percent_done * 100));
  const rate = $derived(
    transfer.putio_phase.rate_download + (transfer.local_phase?.rate_download ?? 0),
  );

  // Comet center offset in viewBox units (gauge stroke centerline r=84 on a
  // 200-unit box → the wrapper is 320px, so 1 unit = 320/200 = 1.6px). The
  // comet is absolutely positioned from the wrapper center.
  const RADIUS_PX = (84 / 200) * 320; // 134.4px
  const comet = $derived(cometPosition(state, transfer, RADIUS_PX));

  // Recolor + mute rules per the §3.2 table.
  const p1Class = $derived(
    state === 'completed'
      ? 'green'
      : state === 'failed'
        ? 'red muted'
        : state === 'local-downloading'
          ? 'muted'
          : '',
  );
  const p2Class = $derived(state === 'completed' ? 'green' : state === 'failed' ? 'red' : '');
  const bigClass = $derived(
    state === 'queued' ? 'dim' : state === 'completed' ? 'green' : state === 'failed' ? 'red' : '',
  );
  const cometClass = $derived(
    (state === 'putio-fetching' ? 'fetch ' : '') + (active ? 'active' : ''),
  );
</script>

<div class="flowgauge">
  <svg viewBox="0 0 200 200" role="img" aria-label="combined progress {pct}%">
    <defs>
      <linearGradient id="fgP1" x1="0" y1="0" x2="1" y2="0">
        <stop offset="0" stop-color="var(--putio-2)" />
        <stop offset="1" stop-color="var(--putio)" />
      </linearGradient>
      <linearGradient id="fgP2" x1="0" y1="0" x2="1" y2="1">
        <stop offset="0" stop-color="var(--local-2)" />
        <stop offset="1" stop-color="var(--local)" />
      </linearGradient>
    </defs>
    <circle class="fg-track" cx="100" cy="100" r="84" stroke-dasharray="395.8 527.8" />
    <circle
      class="fg-p1 {p1Class}"
      cx="100"
      cy="100"
      r="84"
      stroke-dasharray="{arcs.p1Len} 527.8"
    />
    <circle
      class="fg-p2 {p2Class}"
      cx="100"
      cy="100"
      r="84"
      stroke-dasharray="{arcs.p2Len} 527.8"
      stroke-dashoffset="-197.9"
    />
    <line class="fg-handoff" x1="100" y1="8" x2="100" y2="23" />
  </svg>

  <div class="dial">
    <div class="big num {bigClass}">{pct}<sup>%</sup></div>
    <div class="lab">combined</div>
    {#if rate > 0}
      <div class="rate num {state === 'putio-fetching' ? 'fetch' : ''}">
        <span class="arr">↓</span>
        {humanRate(rate)}
      </div>
    {/if}
  </div>

  {#if comet}
    <div
      class="comet {cometClass}"
      style="left:calc(50% + {comet.x}px); top:calc(50% + {comet.y}px); margin-left:-8px; margin-top:-8px;"
    ></div>
  {/if}
</div>
