<script lang="ts">
  // Account / quota instrument cluster: identity, active-transfer count,
  // aggregate throughput, and the ticked put.io disk meter. SPEC §4.1.
  import type { Account, Transfer } from '../types';
  import { aggregate } from '../util/transfer';
  import { humanBytes, humanRate } from '../util/format';

  let { account, transfers }: { account: Account | null; transfers: Transfer[] } = $props();

  const agg = $derived(aggregate(transfers));
  const initial = $derived((account?.username ?? '?').charAt(0) || '?');

  // Aggregate ↓ split into value + unit for the read-xl numeral styling.
  const aggParts = $derived(splitRate(agg.totalRate));
  function splitRate(bps: number): { v: string; u: string } {
    const r = humanRate(bps); // e.g. "28.4 MB/s" or "—"
    if (r === '—') return { v: '0', u: 'B/s' };
    const [v, u] = r.split(' ');
    return { v, u };
  }

  const usedPct = $derived(account ? account.used_percent : 0);
  const overQuota = $derived(account?.over_quota ?? false);
  const purgeDays = $derived(account?.days_until_files_deletion ?? 0);
</script>

<div class="cluster">
  <div class="gid">
    <div class="av">{initial}</div>
    <div class="who">{account?.username ?? '—'}<small>put.io account</small></div>
  </div>

  <div class="inst">
    <div class="lab">active transfers</div>
    <div class="read num">{agg.activeCount}</div>
    <div class="sub">{agg.queuedCount} queued · {agg.stalledCount} stalled</div>
  </div>

  <div class="inst">
    <div class="lab">aggregate ↓</div>
    <div class="read num">{aggParts.v}<span class="u">{aggParts.u}</span></div>
    <div class="sub">put.io {humanRate(agg.putioRate)} · local {humanRate(agg.localRate)}</div>
  </div>

  <div class="inst disk">
    <div class="diskHead">
      <span class="lab">put.io disk</span>
      {#if overQuota}<span class="diskBadge">over quota</span>{/if}
      {#if purgeDays > 0}<span class="diskNote">files purge in {purgeDays} days</span>{/if}
    </div>
    <div class="diskBar">
      <div class="ticks"></div>
      {#if overQuota}<div class="danger"></div>{/if}
      <i class:over={overQuota} style="width:{Math.min(usedPct, 100)}%"></i>
    </div>
    <div class="diskLegend">
      <span>used <b>{humanBytes(account?.disk.used ?? 0)}</b> · {usedPct.toFixed(1)}%</span>
      <span
        >free <b>{humanBytes(account?.disk.avail ?? 0)}</b> of {humanBytes(
          account?.disk.size ?? 0,
        )}</span
      >
    </div>
  </div>
</div>
