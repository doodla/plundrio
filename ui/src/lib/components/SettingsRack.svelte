<script lang="ts">
  // Settings rack: one slot per SettingEntry, controls keyed by key, badges for
  // source/live/restart/locked, a masked token field that NEVER shows or reads
  // the value, and a save bar with dirty tracking + applied/restart feedback +
  // validation-error rings. SPEC §4.3. Settings come live via the SSE `settings`
  // event; this component reads the store and edits a local draft.
  import type { SettingEntry } from '../types';
  import { settings } from '../stores/data';
  import { putSettings, ApiRequestError } from '../util/api';

  const LOG_LEVELS = ['debug', 'info', 'warn', 'error', 'fatal', 'none'];
  const TEXT_KEYS = ['target', 'folder', 'listen', 'dashboard_listen'];

  // Draft holds in-progress edits keyed by setting key. Absent = unchanged.
  let draft = $state<Record<string, string | number>>({});
  // Token replace flow: when active, an input is shown; the value is write-only.
  let replacingToken = $state(false);
  let tokenInput = $state('');

  // Per-key transient feedback from the last PUT.
  let appliedKeys = $state<Set<string>>(new Set());
  let restartPending = $state<Set<string>>(new Set());
  let fieldError = $state<{ field: string; message: string } | null>(null);
  let saveError = $state<string | null>(null);
  let saving = $state(false);

  const entries = $derived($settings);

  function entry(key: string): SettingEntry | undefined {
    return entries.find((e) => e.key === key);
  }

  // Effective (draft-or-resolved) value for a key.
  function effective(key: string): string | number {
    if (key in draft) return draft[key];
    const e = entry(key);
    return (e?.value as string | number) ?? '';
  }

  function isDirty(key: string): boolean {
    if (!(key in draft)) return false;
    const e = entry(key);
    return draft[key] !== (e?.value ?? '');
  }

  // The token counts as pending only while the replace field actually holds a
  // value — typing then clearing it must not leave a stale "● pending". (The
  // value itself is never read into display or persisted from here.)
  const dirtyKeys = $derived(
    Object.keys(draft).filter((k) => (k === 'token' ? tokenInput.length > 0 : isDirty(k))),
  );
  const pendingCount = $derived(dirtyKeys.length);

  function setDraft(key: string, value: string | number) {
    // Locked keys are read-only — never enter the draft (server would 400).
    const e = entry(key);
    if (e?.locked) return;
    draft = { ...draft, [key]: value };
    // Editing clears that field's prior error/feedback.
    if (fieldError?.field === key) fieldError = null;
  }

  function discard() {
    draft = {};
    replacingToken = false;
    tokenInput = '';
    fieldError = null;
    saveError = null;
  }

  async function save() {
    if (pendingCount === 0 || saving) return;
    saving = true;
    saveError = null;
    fieldError = null;

    const body: Record<string, string | number> = {};
    for (const k of Object.keys(draft)) {
      if (k === 'token') {
        if (tokenInput.length > 0) body.token = tokenInput;
        continue;
      }
      if (isDirty(k)) body[k] = draft[k];
    }
    if (Object.keys(body).length === 0) {
      saving = false;
      return;
    }

    try {
      const res = await putSettings(body);
      // The SSE `settings` event will refresh the store; reflect feedback now.
      appliedKeys = new Set(res.applied);
      restartPending = new Set([...restartPending, ...res.restart_required]);
      // Clear the draft for keys that were accepted.
      const next: Record<string, string | number> = {};
      for (const k of Object.keys(draft)) {
        if (!res.persisted.includes(k) && !(k === 'token' && res.persisted.includes('token'))) {
          next[k] = draft[k];
        }
      }
      draft = next;
      replacingToken = false;
      tokenInput = '';
      // Applied (green) badges are momentary; clear after a beat.
      setTimeout(() => (appliedKeys = new Set()), 2500);
    } catch (err) {
      if (err instanceof ApiRequestError) {
        if (err.field) fieldError = { field: err.field, message: err.message };
        else saveError = err.message;
      } else {
        saveError = 'request failed';
      }
    } finally {
      saving = false;
    }
  }

  function hint(key: string): string {
    switch (key) {
      case 'log_level':
        return 'applies immediately · widens/narrows the live stream above';
      case 'workers':
        return 'concurrent local downloads · shrink retires as jobs finish';
      case 'target':
        return 'download directory · restart required to switch';
      case 'folder':
        return 'put.io folder · restart required to re-scope';
      case 'listen':
        return 'Transmission RPC bind · restart required';
      case 'dashboard_listen':
        return "this dashboard's bind · restart required";
      case 'token':
        return 'value never read back, only replaced';
      default:
        return '';
    }
  }

  const workers = $derived(Number(effective('workers')) || 1);

  // Per-key entry refs (derived so top-level template needs no {@const}).
  const ll = $derived(entry('log_level'));
  const wk = $derived(entry('workers'));
  const tk = $derived(entry('token'));
</script>

<div class="rack">
  <!-- log_level: segmented -->
  {#if ll}
    <div class="slot">
      <div class="sh">
        <span class="key">log_level</span>
        <span class="bdgs">
          {#if appliedKeys.has('log_level')}<span class="bdg applied">applied</span>{/if}
          <span class="bdg src">{ll.source}</span>
          {#if ll.live}<span class="bdg live">live</span>{/if}
          {#if ll.locked}<span class="bdg locked">locked</span>{/if}
        </span>
      </div>
      <div class="seg" role="radiogroup" aria-label="log level">
        {#each LOG_LEVELS as lvl (lvl)}
          <button
            role="radio"
            aria-checked={effective('log_level') === lvl}
            class:on={effective('log_level') === lvl}
            disabled={ll.locked}
            onclick={() => setDraft('log_level', lvl)}
          >
            {lvl}
          </button>
        {/each}
      </div>
      <div class="hint">{hint('log_level')}</div>
    </div>
  {/if}

  <!-- workers: stepper -->
  {#if wk}
    <div class="slot">
      <div class="sh">
        <span class="key">workers</span>
        <span class="bdgs">
          {#if appliedKeys.has('workers')}<span class="bdg applied">applied</span>{/if}
          <span class="bdg src">{wk.source}</span>
          {#if wk.live}<span class="bdg live">live</span>{/if}
          {#if wk.locked}<span class="bdg locked">locked</span>{/if}
        </span>
      </div>
      <div class="step">
        <button
          aria-label="decrease workers"
          disabled={wk.locked || workers <= 1}
          onclick={() => setDraft('workers', Math.max(1, workers - 1))}>−</button
        >
        <span class="val num">{workers}</span>
        <button
          aria-label="increase workers"
          disabled={wk.locked}
          onclick={() => setDraft('workers', workers + 1)}>+</button
        >
      </div>
      <div class="hint">{hint('workers')}</div>
    </div>
  {/if}

  <!-- text keys: target / folder / listen / dashboard_listen -->
  {#each TEXT_KEYS as key (key)}
    {@const e = entry(key)}
    {#if e}
      <div class="slot" class:lockedF={e.locked} class:invalid={fieldError?.field === key}>
        <div class="sh">
          <span class="key">{key}</span>
          <span class="bdgs">
            {#if restartPending.has(key)}<span class="bdg restart">restart required</span>{/if}
            <span class="bdg src">{e.source}</span>
            {#if e.live}<span class="bdg live">live</span>{/if}
            {#if e.locked}<span class="bdg locked">locked</span>{/if}
            {#if e.restart_required && !restartPending.has(key)}<span class="bdg restart"
                >restart</span
              >{/if}
          </span>
        </div>
        {#if e.locked}
          <div class="inp locked">{e.value} <span class="x">🔒</span></div>
        {:else}
          <div class="inp">
            <input
              type="text"
              value={effective(key)}
              oninput={(ev) => setDraft(key, (ev.currentTarget as HTMLInputElement).value)}
              aria-label={key}
            />
          </div>
        {/if}
        <div class="hint" class:err={fieldError?.field === key}>
          {fieldError?.field === key ? fieldError.message : hint(key)}
        </div>
      </div>
    {/if}
  {/each}

  <!-- token: masked, never shows/reads the value -->
  {#if tk}
    <div class="slot wide" class:lockedF={tk.locked} class:invalid={fieldError?.field === 'token'}>
      <div class="sh">
        <span class="key">token</span>
        <span class="bdgs">
          {#if restartPending.has('token')}<span class="bdg restart">restart required</span>{/if}
          <span class="bdg src">{tk.source}</span>
          {#if tk.locked}<span class="bdg locked">locked</span>{/if}
        </span>
      </div>
      <div class="tokenRow">
        {#if replacingToken && !tk.locked}
          <input
            type="password"
            placeholder="paste new token…"
            bind:value={tokenInput}
            oninput={() => setDraft('token', '')}
            aria-label="new token"
          />
          <button
            class="btn ghost"
            onclick={() => {
              replacingToken = false;
              tokenInput = '';
              const { token: _t, ...rest } = draft;
              void _t;
              draft = rest;
            }}>Cancel</button
          >
        {:else}
          <span class="tokenMask">
            {tk.is_set ? '•••••••••••••••••••• set' : 'not set'}
          </span>
          {#if !tk.locked}
            <button class="btn ghost" onclick={() => (replacingToken = true)}>
              {tk.is_set ? 'Replace token…' : 'Set token…'}
            </button>
          {/if}
        {/if}
      </div>
      <div class="hint" class:err={fieldError?.field === 'token'}>
        {#if fieldError?.field === 'token'}
          {fieldError.message}
        {:else if tk.locked}
          pinned by PLDR_TOKEN · {hint('token')}
        {:else}
          {hint('token')}
        {/if}
      </div>
    </div>
  {/if}
</div>

<div class="rackFoot">
  {#if pendingCount > 0}
    <span class="dirty">● {pendingCount} pending</span>
  {/if}
  {#if restartPending.size > 0}
    <span class="restart-note">restart required for {[...restartPending].join(', ')}</span>
  {/if}
  {#if saveError}
    <span class="dirty" style="color:var(--red-ink)">{saveError}</span>
  {/if}
  <span class="sp"></span>
  <button class="btn ghost" disabled={pendingCount === 0 || saving} onclick={discard}
    >Discard</button
  >
  <button class="btn prim" disabled={pendingCount === 0 || saving} onclick={save}>
    {saving ? 'Saving…' : 'Save changes'}
  </button>
</div>
