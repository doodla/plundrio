<script lang="ts">
  // The same flow-gauge instrument, compact (52px, 5px stroke, r=24). Same 270°
  // rotation + put.io→local arc split + combined % centered. SPEC §3.3.
  import type { Transfer } from '../types';
  import { MINI_FULL, displayState, gaugeArcs, type DisplayState } from '../util/transfer';

  let { transfer }: { transfer: Transfer } = $props();

  const state = $derived<DisplayState>(displayState(transfer));
  const arcs = $derived(gaugeArcs(transfer, MINI_FULL));
  const pct = $derived(Math.round(transfer.percent_done * 100));

  const a1Class = $derived(
    state === 'failed'
      ? 'red'
      : state === 'completed'
        ? 'green'
        : state === 'local-downloading'
          ? 'muted'
          : '',
  );
  const a2Class = $derived(state === 'failed' ? 'red' : state === 'completed' ? 'green' : '');
  const pcClass = $derived(
    state === 'queued' ? 'dim' : state === 'completed' ? 'green' : state === 'failed' ? 'red' : '',
  );
  // Hide the local leg arc entirely until the handoff so a queued/fetching gauge
  // shows only the put.io leg.
  const showP2 = $derived(arcs.p2Len > 0.01);
</script>

<div class="mini">
  <svg viewBox="0 0 60 60" role="img" aria-label="{pct}%">
    <circle class="t" cx="30" cy="30" r="24" stroke-dasharray="113.1 150.8" />
    {#if arcs.p1Len > 0.01}
      <circle class="a1 {a1Class}" cx="30" cy="30" r="24" stroke-dasharray="{arcs.p1Len} 150.8" />
    {/if}
    {#if showP2}
      <circle
        class="a2 {a2Class}"
        cx="30"
        cy="30"
        r="24"
        stroke-dasharray="{arcs.p2Len} 150.8"
        stroke-dashoffset="-56.5"
      />
    {/if}
  </svg>
  <span class="pc num {pcClass}">{pct}</span>
</div>
