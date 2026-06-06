<script lang="ts">
  // Live log viewer: level color-bar + filter chips + component tag + message/
  // fields, autoscroll with pause-on-scroll-up, fresh-line flash. The level
  // chips are a client-side VIEW filter over the received stream (distinct from
  // the live log_level PUT, which changes what the server SENDS). SPEC §4.2.
  import type { LogLine } from '../stores/data';
  import { logs } from '../stores/data';
  import { clockTime, formatFields } from '../util/format';

  let { maxHeight = '330px' }: { maxHeight?: string } = $props();

  const LEVELS = ['debug', 'info', 'warn', 'error'] as const;
  // Default-on chips (debug off by default, matching the mockup).
  let shown = $state<Record<string, boolean>>({
    debug: false,
    info: true,
    warn: true,
    error: true,
  });

  // error chip also covers fatal.
  function visible(line: LogLine): boolean {
    const lvl = line.level === 'fatal' ? 'error' : line.level;
    return shown[lvl] ?? true;
  }

  const filtered = $derived($logs.filter(visible));

  // Autoscroll, paused when the user scrolls up.
  let autoscroll = $state(true);
  let body = $state<HTMLDivElement | null>(null);
  // Track the newest id we've seen so only genuinely-new lines flash.
  let lastSeenId = $state(-1);

  function onScroll() {
    if (!body) return;
    const atBottom = body.scrollHeight - body.scrollTop - body.clientHeight < 24;
    autoscroll = atBottom;
  }

  $effect(() => {
    // React to the filtered list length; scroll after the DOM updates.
    void filtered.length;
    if (autoscroll && body) {
      queueMicrotask(() => {
        if (body) body.scrollTop = body.scrollHeight;
      });
    }
    const newest = $logs.length ? $logs[$logs.length - 1]._id : -1;
    lastSeenId = newest;
  });

  function isFresh(line: LogLine): boolean {
    return line._id === lastSeenId;
  }

  function toggle(level: string) {
    shown = { ...shown, [level]: !shown[level] };
  }
</script>

<div class="console">
  <div class="cHead">
    {#each LEVELS as lvl (lvl)}
      <button
        class="lvl"
        class:on={shown[lvl]}
        aria-pressed={shown[lvl]}
        onclick={() => toggle(lvl)}
      >
        {lvl}
      </button>
    {/each}
    <button
      class="auto"
      class:paused={!autoscroll}
      onclick={() => {
        autoscroll = true;
        if (body) body.scrollTop = body.scrollHeight;
      }}
      title={autoscroll ? 'autoscrolling' : 'paused — click to resume'}
    >
      <span class="d"></span>
      {autoscroll ? 'tail · autoscroll' : 'paused'}
    </button>
  </div>

  <div class="cBody" bind:this={body} onscroll={onScroll} style="max-height:{maxHeight}">
    {#if filtered.length === 0}
      <div class="empty">Waiting for log events…</div>
    {:else}
      {#each filtered as line (line._id)}
        <div class="ln {line.level}" class:fresh={isFresh(line)}>
          <span class="bar"></span>
          <span class="t num">{clockTime(line.time)}</span>
          <span class="l {line.level}">{line.level}</span>
          <span class="c">{line.component}</span>
          <span class="m">
            <b>{line.message}</b>
            {#if Object.keys(line.fields).length}
              <span class="f">{formatFields(line.fields)}</span>
            {/if}
          </span>
        </div>
      {/each}
    {/if}
  </div>
</div>
