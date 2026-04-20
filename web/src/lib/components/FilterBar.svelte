<script lang="ts">
  import { getMerchants } from '$lib/api';

  let {
    merchant = $bindable(''),
    region = $bindable(''),
    endpoint = $bindable(''),
    status = $bindable(''),
    cacheStatus = $bindable(''),
    method = $bindable(''),
    queued = $bindable(''),
    minLatency = $bindable(''),
    maxLatency = $bindable(''),
    showStatus = false,
    showLatency = false,
    showMethod = false,
    onchange
  }: {
    merchant?: string;
    region?: string;
    endpoint?: string;
    status?: string;
    cacheStatus?: string;
    method?: string;
    queued?: string;
    minLatency?: string;
    maxLatency?: string;
    showStatus?: boolean;
    showLatency?: boolean;
    showMethod?: boolean;
    onchange?: () => void;
  } = $props();

  let suggestions = $state<string[]>([]);
  let showSuggestions = $state(false);
  let debounceTimer: ReturnType<typeof setTimeout> | null = null;

  const inputCls = 'h-8 rounded-lg bg-surface-container border border-outline-variant/5 px-4 py-2 text-sm font-body text-on-surface placeholder:text-outline focus:outline-none focus:ring-1 focus:ring-primary/40 transition-all';
  const selectCls = inputCls + ' pr-8 appearance-none';

  function handleInput() {
    onchange?.();
  }

  async function onMerchantInput() {
    const q = merchant;
    if (debounceTimer) clearTimeout(debounceTimer);
    debounceTimer = setTimeout(async () => {
      try {
        const data = await getMerchants(q || undefined);
        suggestions = data.merchants;
        showSuggestions = suggestions.length > 0;
      } catch {
        suggestions = [];
        showSuggestions = false;
      }
    }, 150);
    onchange?.();
  }

  function selectMerchant(m: string) {
    merchant = m;
    showSuggestions = false;
    onchange?.();
  }

  function onMerchantFocus() {
    if (suggestions.length > 0) showSuggestions = true;
    else onMerchantInput();
  }

  function onMerchantBlur() {
    setTimeout(() => { showSuggestions = false; }, 200);
  }

  let hasFilters = $derived(
    merchant !== '' || region !== '' || endpoint !== '' || status !== '' ||
    cacheStatus !== '' || method !== '' || queued !== '' || minLatency !== '' || maxLatency !== ''
  );

  function clearAll() {
    merchant = '';
    region = '';
    endpoint = '';
    status = '';
    cacheStatus = '';
    method = '';
    queued = '';
    minLatency = '';
    maxLatency = '';
    onchange?.();
  }
</script>

<div class="flex flex-wrap items-center gap-3">
  <!-- Merchant autocomplete -->
  <div class="relative">
    <div class="flex items-center gap-2 bg-surface-container px-4 py-2 rounded-lg border border-outline-variant/5">
      <span class="text-xs font-label text-outline uppercase tracking-tighter">Merchant</span>
      <input
        type="text"
        placeholder="All Entities"
        bind:value={merchant}
        oninput={onMerchantInput}
        onfocus={onMerchantFocus}
        onblur={onMerchantBlur}
        class="bg-transparent border-none p-0 text-sm font-body font-medium text-on-surface placeholder:text-on-surface focus:outline-none focus:ring-0 w-32"
      />
      <span class="material-symbols-outlined text-sm text-on-surface-variant">expand_more</span>
    </div>
    {#if showSuggestions && suggestions.length > 0}
      <div class="absolute top-full left-0 mt-1 w-56 rounded-xl bg-surface-container-highest shadow-xl z-50 max-h-40 overflow-y-auto border border-outline-variant/10">
        {#each suggestions as s}
          <button
            onmousedown={(e) => { e.preventDefault(); selectMerchant(s); }}
            class="block w-full text-left px-4 py-2 text-xs font-mono text-on-surface-variant hover:bg-surface-bright hover:text-on-surface transition-all truncate"
          >{s}</button>
        {/each}
      </div>
    {/if}
  </div>

  <div class="flex items-center gap-2 bg-surface-container px-4 py-2 rounded-lg border border-outline-variant/5">
    <span class="text-xs font-label text-outline uppercase tracking-tighter">Region</span>
    <select bind:value={region} onchange={handleInput} class="bg-transparent border-none p-0 text-sm font-body font-medium text-on-surface focus:outline-none focus:ring-0 appearance-none cursor-pointer">
      <option value="">Global Cluster</option>
      <option value="eu">EU</option>
      <option value="na">NA</option>
      <option value="fe">FE</option>
    </select>
    <span class="material-symbols-outlined text-sm text-on-surface-variant">expand_more</span>
  </div>

  <div class="flex items-center gap-2 bg-surface-container px-4 py-2 rounded-lg border border-outline-variant/5">
    <span class="text-xs font-label text-outline uppercase tracking-tighter">Endpoint</span>
    <input
      type="text"
      placeholder="All APIs"
      bind:value={endpoint}
      oninput={handleInput}
      class="bg-transparent border-none p-0 text-sm font-body font-medium text-on-surface placeholder:text-on-surface focus:outline-none focus:ring-0 w-24 font-mono"
    />
    <span class="material-symbols-outlined text-sm text-on-surface-variant">expand_more</span>
  </div>

  {#if showStatus}
    <div class="flex items-center gap-2 bg-surface-container px-4 py-2 rounded-lg border border-outline-variant/5">
      <span class="text-xs font-label text-outline uppercase tracking-tighter">Status</span>
      <select bind:value={status} onchange={handleInput} class="bg-transparent border-none p-0 text-sm font-body font-medium text-on-surface focus:outline-none focus:ring-0 appearance-none cursor-pointer">
        <option value="">All</option>
        <option value="2xx">2xx</option>
        <option value="3xx">3xx</option>
        <option value="4xx">4xx</option>
        <option value="5xx">5xx</option>
      </select>
    </div>

    <div class="flex items-center gap-2 bg-surface-container px-4 py-2 rounded-lg border border-outline-variant/5">
      <span class="text-xs font-label text-outline uppercase tracking-tighter">Cache</span>
      <select bind:value={cacheStatus} onchange={handleInput} class="bg-transparent border-none p-0 text-sm font-body font-medium text-on-surface focus:outline-none focus:ring-0 appearance-none cursor-pointer">
        <option value="">All Statuses</option>
        <option value="HIT">HIT</option>
        <option value="MISS">MISS</option>
        <option value="BYPASS">BYPASS</option>
        <option value="PII_EXCLUDED">PII</option>
      </select>
    </div>
  {/if}

  {#if showMethod}
    <div class="flex items-center gap-2 bg-surface-container px-4 py-2 rounded-lg border border-outline-variant/5">
      <span class="text-xs font-label text-outline uppercase tracking-tighter">Method</span>
      <select bind:value={method} onchange={handleInput} class="bg-transparent border-none p-0 text-sm font-body font-medium text-on-surface focus:outline-none focus:ring-0 appearance-none cursor-pointer">
        <option value="">All</option>
        <option value="GET">GET</option>
        <option value="POST">POST</option>
        <option value="PUT">PUT</option>
        <option value="PATCH">PATCH</option>
        <option value="DELETE">DELETE</option>
      </select>
    </div>

    <div class="flex items-center gap-2 bg-surface-container px-4 py-2 rounded-lg border border-outline-variant/5">
      <span class="text-xs font-label text-outline uppercase tracking-tighter">Queued</span>
      <select bind:value={queued} onchange={handleInput} class="bg-transparent border-none p-0 text-sm font-body font-medium text-on-surface focus:outline-none focus:ring-0 appearance-none cursor-pointer">
        <option value="">All</option>
        <option value="true">Yes</option>
        <option value="false">No</option>
      </select>
    </div>
  {/if}

  {#if showLatency}
    <div class="flex items-center gap-2 bg-surface-container px-4 py-2 rounded-lg border border-outline-variant/5">
      <span class="text-xs font-label text-outline uppercase tracking-tighter">Latency</span>
      <select
        onchange={(e) => {
          const v = (e.target as HTMLSelectElement).value;
          if (v === '') { minLatency = ''; maxLatency = ''; }
          else if (v === 'fast') { minLatency = ''; maxLatency = '200'; }
          else if (v === 'medium') { minLatency = '200'; maxLatency = '1000'; }
          else if (v === 'slow') { minLatency = '1000'; maxLatency = ''; }
          handleInput();
        }}
        class="bg-transparent border-none p-0 text-sm font-body font-medium text-on-surface focus:outline-none focus:ring-0 appearance-none cursor-pointer"
      >
        <option value="">Any Speed</option>
        <option value="fast">&lt; 100ms</option>
        <option value="medium">200ms - 1s</option>
        <option value="slow">&gt; 2s (Degraded)</option>
      </select>
    </div>
  {/if}

  {#if hasFilters}
    <button
      onclick={clearAll}
      class="text-sm font-bold text-primary hover:text-primary-container transition-colors font-label"
    >
      Clear All Filters
    </button>
  {/if}
</div>
