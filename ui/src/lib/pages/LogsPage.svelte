<script lang="ts">
  // Logs surface: the full-height live console. SPEC §4.2.
  import { logs, isStale } from '../stores/data';
  import LogConsole from '../components/LogConsole.svelte';
</script>

<div class="eyebrow">
  <h2>Console</h2>
  <div class="ln"></div>
  <div class="meta"><b>{$logs.length}</b> buffered · {$isStale ? 'stream lost' : 'live'}</div>
</div>

<LogConsole maxHeight="calc(100vh - 220px)" />
