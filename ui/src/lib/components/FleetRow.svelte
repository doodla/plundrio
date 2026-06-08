<script lang="ts">
  // One fleet transfer: mini flow-gauge + name/status + two phase bars + rate/
  // ETA. All lifecycle states; failed rows get the red wash + permanent chip.
  // SPEC §3.3.
  import type { Transfer } from '../types';
  import MiniGauge from './MiniGauge.svelte';
  import {
    displayState,
    humanLabel,
    isActive,
    localFraction,
    type DisplayState,
  } from '../util/transfer';
  import { humanBytes, humanEta, humanRate, relativeAge } from '../util/format';

  let { transfer, now }: { transfer: Transfer; now: number } = $props();

  const state = $derived<DisplayState>(displayState(transfer));
  const active = $derived(isActive(transfer, state));
  const lp = $derived(transfer.local_phase);
  const putioPct = $derived(Math.min(transfer.putio_phase.percent_done, 100));
  const localPct = $derived(Math.round(localFraction(transfer) * 100));

  // Status dot class.
  const dotClass = $derived(
    state === 'putio-fetching'
      ? 'fetch'
      : state === 'local-downloading'
        ? 'local'
        : state === 'completed'
          ? 'done'
          : state === 'failed'
            ? 'fail'
            : 'queued',
  );

  // Status line text.
  const statusText = $derived(buildStatus(transfer, state));
  function buildStatus(t: Transfer, s: DisplayState): string {
    switch (s) {
      case 'queued':
        return 'queued · waiting for put.io';
      case 'putio-fetching':
        return `${humanLabel(s)} · ${putioPct}% fetched`;
      case 'local-downloading':
        return t.local_phase
          ? `downloading locally · ${t.local_phase.completed_files}/${t.local_phase.total_files} files`
          : 'downloading locally';
      case 'completed':
        return t.local_phase
          ? `completed · ${t.local_phase.completed_files}/${t.local_phase.total_files} files`
          : 'completed';
      case 'failed':
        return t.permanent ? 'failed · gave up after retries' : 'failed · put.io error';
    }
  }

  // put.io bar muting: dim the finished leg once handed off.
  const p1Muted = $derived(
    state === 'local-downloading' || state === 'completed' || state === 'failed',
  );

  // Right-aligned meta: rate while active, terminal note otherwise.
  const metaTop = $derived(buildMetaTop(transfer, state, active));
  const metaTopClass = $derived(state === 'completed' ? 'green' : state === 'failed' ? 'red' : '');
  function buildMetaTop(t: Transfer, s: DisplayState, isAct: boolean): string {
    if (s === 'completed') return 'done';
    if (s === 'failed') return 'frozen';
    if (s === 'queued') return humanBytes(t.size);
    const rate =
      s === 'local-downloading' ? (t.local_phase?.rate_download ?? 0) : t.putio_phase.rate_download;
    return isAct ? `↓ ${humanRate(rate)}` : 'stalled';
  }
  const metaBottom = $derived(buildMetaBottom(transfer, state));
  function buildMetaBottom(t: Transfer, s: DisplayState): string {
    if (s === 'completed') return relativeAge(t.finished_at ?? t.processed_at, now) || 'recently';
    if (s === 'failed') return t.permanent ? 'permanent' : 'errored';
    if (s === 'queued') return 'in queue';
    const eta =
      s === 'local-downloading' ? (t.local_phase?.eta_seconds ?? 0) : t.putio_phase.eta_seconds;
    return `ETA ${humanEta(eta)}`;
  }
</script>

<div class="tr" class:queued={state === 'queued'} class:fail={state === 'failed'}>
  <MiniGauge {transfer} />

  <div class="trN">
    <div class="nm" title={transfer.name}>{transfer.name}</div>
    <div class="st">
      <span class="d {dotClass}" class:active></span>
      {statusText}
    </div>
  </div>

  <div class="trBars">
    <div class="lab"><span>put.io</span><span>{putioPct > 0 ? `${putioPct}%` : '—'}</span></div>
    <div class="b p1">
      <i class:muted={p1Muted} class:red={state === 'failed'} style="width:{putioPct}%"></i>
    </div>
    <div class="lab"><span>local</span><span>{lp ? `${localPct}%` : '—'}</span></div>
    <div class="b p2">
      <i
        class:green={state === 'completed'}
        class:red={state === 'failed'}
        style="width:{lp ? localPct : 0}%"
      ></i>
    </div>
  </div>

  <div class="trMeta">
    <b class={metaTopClass}>{metaTop}</b>{metaBottom}
  </div>

  {#if state === 'failed'}
    <div class="failBar">
      <span class="pb">{transfer.permanent ? 'permanent' : 'put.io error'}</span>
      {transfer.error_string || 'transfer failed'}
    </div>
  {/if}
</div>
