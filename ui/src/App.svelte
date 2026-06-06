<script lang="ts">
  import { onMount } from 'svelte';
  import Rail from './lib/components/Rail.svelte';
  import Deck from './lib/pages/Deck.svelte';
  import AccountPage from './lib/pages/AccountPage.svelte';
  import LogsPage from './lib/pages/LogsPage.svelte';
  import SettingsPage from './lib/pages/SettingsPage.svelte';
  import { route } from './lib/stores/route';
  import { connect, disconnect, isStale } from './lib/stores/data';
  import { railClock } from './lib/util/format';

  // Build-time version stamped by Vite (define), falling back to a placeholder.
  const version = __APP_VERSION__;

  // A 1s clock tick drives the rail clock and `now` for relative ages, without
  // each component owning its own timer.
  let clock = $state(railClock());
  let now = $state(Date.now());

  onMount(() => {
    connect();
    const id = setInterval(() => {
      clock = railClock();
      now = Date.now();
    }, 1000);
    return () => {
      clearInterval(id);
      disconnect();
    };
  });
</script>

<Rail {version} {clock} />

{#if $isStale}
  <div class="ribbon">stream lost — showing last known state · reconnecting</div>
{/if}

<main>
  {#if $route === 'deck'}
    <Deck {now} />
  {:else if $route === 'account'}
    <AccountPage />
  {:else if $route === 'logs'}
    <LogsPage />
  {:else if $route === 'settings'}
    <SettingsPage />
  {/if}

  <footer>plundrio · put.io ⇄ *arr bridge</footer>
</main>
