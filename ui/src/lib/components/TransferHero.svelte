<script lang="ts">
  // The gauge bay: the focal transfer's flow-gauge hero with header chip,
  // name/sub line, phase keys, and the two-cell handoff legend. SPEC §3.1/§3.2.
  import type { Transfer } from '../types';
  import FlowGauge from './FlowGauge.svelte';
  import { displayState, humanLabel, localFraction, type DisplayState } from '../util/transfer';
  import { humanBytes, humanEta, relativeAge } from '../util/format';

  let { transfer, now }: { transfer: Transfer; now: number } = $props();

  const state = $derived<DisplayState>(displayState(transfer));
  const label = $derived(humanLabel(state));

  // Header chip: glyph + label, colored by state.
  const chipClass = $derived(
    state === 'queued'
      ? 'queued'
      : state === 'putio-fetching'
        ? 'fetch'
        : state === 'completed'
          ? 'done'
          : state === 'failed'
            ? 'fail'
            : '',
  );
  const chipText = $derived(
    state === 'putio-fetching'
      ? `▸ ${label}`
      : state === 'local-downloading'
        ? `▸ ${label}`
        : label,
  );

  const lp = $derived(transfer.local_phase);
  const putioPct = $derived(Math.min(transfer.putio_phase.percent_done, 100));
  const localPct = $derived(Math.round(localFraction(transfer) * 100));

  // Legend muting per §3.1/§3.2.
  const p1Muted = $derived(state !== 'queued' && state !== 'putio-fetching');
  const p2Muted = $derived(
    state === 'queued' || state === 'putio-fetching' || state === 'completed',
  );

  const subLine = $derived(buildSub(transfer, now));
  function buildSub(t: Transfer, n: number): string {
    const parts = [humanBytes(t.size)];
    if (t.local_phase) parts.push(`${t.local_phase.total_files} files`);
    const age = relativeAge(t.created_at, n);
    if (age) parts.push(`added ${age}`);
    parts.push(`put.io ${t.id}`);
    return parts.join(' · ');
  }
</script>

<div class="bay">
  <div class="bayHead">
    <span class="chip {chipClass}">{chipText}</span>
    {#if transfer.category}
      <span class="cat">{transfer.category}</span>
    {/if}
  </div>
  <div class="bayName" title={transfer.name}>{transfer.name}</div>
  <div class="baySub num">{subLine}</div>

  <FlowGauge {transfer} />

  <div class="fgKeys">
    <span class="fgKey p1" class:muted={p1Muted}><i></i> put.io {putioPct}%</span>
    <span class="fgKey p2" class:muted={p2Muted}>
      <i></i>
      local {lp ? `${localPct}%` : '—'}
    </span>
  </div>

  {#if state === 'failed'}
    <div class="legend">
      <div class="lg p1 muted">
        <div class="k"><i></i> put.io fetch</div>
        <div class="v"><b>{putioPct}%</b> · {humanBytes(transfer.putio_phase.downloaded)}</div>
        <div class="v2">fetched</div>
      </div>
      <div class="lg p2">
        <div class="k"><i></i> local download</div>
        <div class="v" style="color:var(--red-ink)">
          <b style="color:var(--red-ink)">frozen</b> at {localPct}%
        </div>
        <div class="v2" style="color:var(--red-ink)">
          {transfer.error_string || 'permanent failure'}
        </div>
      </div>
    </div>
  {:else}
    <div class="legend">
      <div class="lg p1" class:muted={p1Muted}>
        <div class="k"><i></i> put.io fetch</div>
        {#if state === 'queued'}
          <div class="v"><b>—</b></div>
          <div class="v2">waiting for put.io</div>
        {:else}
          <div class="v"><b>{putioPct}%</b> · {humanBytes(transfer.putio_phase.downloaded)}</div>
          <div class="v2">{p1Muted ? 'fetched, handed off' : 'fetching'}</div>
        {/if}
      </div>
      <div class="lg p2" class:muted={p2Muted}>
        <div class="k"><i></i> local download</div>
        {#if lp}
          <div class="v">
            <b>{lp.completed_files} / {lp.total_files}</b> files · ETA {humanEta(lp.eta_seconds)}
          </div>
          <div class="v2">{humanBytes(lp.downloaded)} landed</div>
        {:else if state === 'completed'}
          <div class="v"><b>done</b></div>
          <div class="v2">complete on put.io</div>
        {:else}
          <div class="v"><b>—</b></div>
          <div class="v2">not started</div>
        {/if}
      </div>
    </div>
  {/if}
</div>
