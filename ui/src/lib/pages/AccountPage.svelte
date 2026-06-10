<script lang="ts">
  // Account surface: the quota cluster on its own, plus an upstream-error strip
  // when the account couldn't be reached. SPEC §4.1/§4.4.
  import { account, transfers } from '../stores/data';
  import AccountCluster from '../components/AccountCluster.svelte';

  // A null account after connect means put.io was unreachable (the snapshot
  // degrades account to zero-value, but a genuinely failed fetch leaves the
  // username empty). Surface a retry strip; keep the cluster rendering.
  const unreachable = $derived(
    $account !== null && $account.username === '' && $account.disk.size === 0,
  );
</script>

<div class="eyebrow">
  <h2>Account</h2>
  <div class="ln"></div>
  <div class="meta">put.io quota</div>
</div>

{#if unreachable}
  <div class="errstrip" style="margin-bottom:16px">couldn't reach put.io — retrying</div>
{/if}

<div class="panel" style="grid-template-columns:1fr">
  <AccountCluster account={$account} transfers={$transfers} />
</div>
