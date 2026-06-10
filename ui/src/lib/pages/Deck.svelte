<script lang="ts">
  // Deck: the master panel (flow-gauge hero + account cluster) + the fleet strip
  // + a compact log console. The headline transfer is the most-active one; all
  // transfers also appear in the fleet. SPEC §3/§4.
  import { transfers, account, isStale } from '../stores/data';
  import { focalTransfer } from '../util/transfer';
  import TransferHero from '../components/TransferHero.svelte';
  import AccountCluster from '../components/AccountCluster.svelte';
  import FleetRow from '../components/FleetRow.svelte';
  import LogConsole from '../components/LogConsole.svelte';

  let { now }: { now: number } = $props();

  const focal = $derived(focalTransfer($transfers));
  const activeCount = $derived(
    $transfers.filter((t) => t.lifecycle_state === 'Initial' || t.lifecycle_state === 'Downloading')
      .length,
  );
</script>

<div class="panel">
  {#if focal}
    <TransferHero transfer={focal} {now} />
  {:else}
    <div class="bay">
      <div class="bayHead"><span class="chip queued">idle</span></div>
      <div class="bayName">no active transfers</div>
      <div class="baySub num">the deck is quiet</div>
      <div class="flowgauge">
        <svg viewBox="0 0 200 200" role="img" aria-label="idle">
          <circle class="fg-track" cx="100" cy="100" r="84" stroke-dasharray="395.8 527.8" />
          <line class="fg-handoff" x1="100" y1="8" x2="100" y2="23" />
        </svg>
        <div class="dial">
          <div class="big num dim">—</div>
          <div class="lab">idle</div>
        </div>
      </div>
    </div>
  {/if}

  <AccountCluster account={$account} transfers={$transfers} />
</div>

<section>
  <div class="eyebrow">
    <h2>Transfer Deck</h2>
    <div class="ln"></div>
    <div class="meta"><b>{$transfers.length}</b> tracked · {activeCount} active</div>
  </div>
  <div class="strip">
    {#if $transfers.length === 0}
      <div class="empty">Nothing in the queue. *arr will add transfers here.</div>
    {:else}
      {#each $transfers as t (t.id)}
        <FleetRow transfer={t} {now} />
      {/each}
    {/if}
  </div>
</section>

<section style="margin-top:42px">
  <div class="eyebrow">
    <h2>Console</h2>
    <div class="ln"></div>
    <div class="meta">{$isStale ? 'stream lost' : 'live'}</div>
  </div>
  <LogConsole />
</section>
