<script lang="ts">
  import type { LogDetail, LogBody } from '$lib/api';
  import { formatLatency, formatBytes } from '$lib/api';
  import JsonView from './JsonView.svelte';

  let { detail, body, detailLoading, onclose, onloadbody, onselectlog }: {
    detail: LogDetail;
    body: LogBody | null;
    detailLoading?: boolean;
    onclose: () => void;
    onloadbody: () => void;
    onselectlog: (id: string) => void;
  } = $props();

  let activeTab = $state<'overview' | 'headers' | 'body'>('overview');

  function statusColor(code: number): string {
    if (code >= 500) return 'text-error';
    if (code >= 400) return 'text-primary';
    return 'text-green-400';
  }

  function methodBadgeCls(m: string): string {
    if (m === 'GET') return 'bg-secondary/10 text-secondary border border-secondary/20';
    if (m === 'POST') return 'bg-primary/10 text-primary border border-primary/20';
    if (m === 'PUT') return 'bg-[#d98f5d]/10 text-primary border border-[#d98f5d]/20';
    if (m === 'DELETE') return 'bg-error/10 text-error border border-error/20';
    return 'bg-surface-container-highest text-on-surface-variant';
  }

  function cacheBadgeCls(s: string): string {
    if (s === 'HIT') return 'text-secondary bg-secondary/10';
    if (s === 'MISS') return 'text-outline bg-surface-container-highest';
    if (s === 'BYPASS') return 'text-primary bg-primary/10';
    return 'text-outline bg-surface-container-highest';
  }

  function onTabBody() {
    activeTab = 'body';
    if (!body && detail.hasBody) {
      onloadbody();
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') onclose();
  }

  let timingSegments = $derived.by(() => {
    const total = detail.totalLatencyMs || 1;
    const segments: { label: string; ms: number; pct: number; color: string }[] = [];

    if (detail.queued && detail.queueWaitMs > 0) {
      segments.push({ label: 'Queue', ms: detail.queueWaitMs, pct: (detail.queueWaitMs / total) * 100, color: 'bg-primary' });
    }

    const upstream = detail.upstreamLatencyMs || 0;
    if (upstream > 0) {
      segments.push({ label: 'Upstream', ms: upstream, pct: (upstream / total) * 100, color: 'bg-secondary' });
    }

    const overhead = total - (detail.queueWaitMs || 0) - upstream;
    if (overhead > 0) {
      segments.push({ label: 'Proxy', ms: overhead, pct: (overhead / total) * 100, color: 'bg-tertiary' });
    }

    return segments;
  });

  const importantHeaders = ['x-amzn-ratelimit-limit', 'x-amzn-requestid', 'x-sp-proxy-cache', 'retry-after'];
  function isImportantHeader(key: string): boolean {
    return importantHeaders.includes(key.toLowerCase());
  }

  let queryPairs = $derived.by(() => {
    if (!detail.queryParams) return [];
    const params = new URLSearchParams(detail.queryParams);
    const pairs: { key: string; value: string }[] = [];
    params.forEach((value, key) => {
      pairs.push({ key, value });
    });
    return pairs;
  });
</script>

<svelte:window onkeydown={handleKeydown} />

<!-- Backdrop -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<!-- svelte-ignore a11y_click_events_have_key_events -->
<div class="fixed inset-0 bg-black/60 backdrop-blur-sm z-40" onclick={onclose}></div>

<!-- Modal -->
<div class="fixed inset-4 lg:inset-y-6 lg:inset-x-[10%] z-50 flex items-start justify-center">
  <div class="w-full max-h-full glass-panel border border-outline-variant/10 rounded-2xl shadow-2xl flex flex-col overflow-hidden">

    <!-- Title bar -->
    <div class="flex items-center justify-between px-6 py-4 border-b border-outline-variant/10 shrink-0 bg-surface-container-high/40">
      <div class="flex items-center gap-3 min-w-0">
        {#if detailLoading}
          <div class="flex items-center gap-2 text-outline">
            <div class="w-4 h-4 rounded-full border-2 border-primary/30 border-t-primary animate-spin"></div>
            <span class="text-xs font-label">Loading...</span>
          </div>
        {:else}
          <span class="px-2 py-0.5 rounded-md text-xs font-bold font-label {methodBadgeCls(detail.method)}">{detail.method}</span>
          <span class="text-xl font-headline font-bold text-on-background truncate">{detail.path}</span>
          <span class="inline-flex px-1.5 py-0.5 rounded text-[10px] font-bold font-label {cacheBadgeCls(detail.cacheStatus)}">{detail.cacheStatus}</span>
        {/if}
      </div>
      <button
        onclick={onclose}
        aria-label="Close"
        class="material-symbols-outlined text-on-surface-variant hover:text-on-background transition-colors p-2"
      >close</button>
    </div>

    <!-- Tabs -->
    <div class="flex border-b border-outline-variant/10 px-6 shrink-0">
      {#each [
        { id: 'overview', label: 'Overview' },
        { id: 'headers', label: 'Headers' },
        { id: 'body', label: 'Body' }
      ] as tab}
        <button
          onclick={() => tab.id === 'body' ? onTabBody() : activeTab = tab.id as typeof activeTab}
          class="px-6 py-4 font-label text-sm transition-colors border-b-2 -mb-px
            {activeTab === tab.id
              ? 'text-primary border-primary font-bold'
              : 'text-on-surface-variant hover:text-on-background border-transparent'}"
        >
          {tab.label}
          {#if tab.id === 'body' && detail.hasBody}
            <span class="ml-1 w-1.5 h-1.5 inline-block rounded-full bg-primary"></span>
          {/if}
        </button>
      {/each}
    </div>

    <!-- Content (scrollable) -->
    <div class="flex-1 overflow-y-auto p-6 space-y-6 bg-surface-container-lowest">

      {#if activeTab === 'overview'}
        <!-- Timing Waterfall -->
        {#if timingSegments.length > 0}
          <div>
            <h3 class="text-[10px] uppercase font-label tracking-widest text-outline mb-3 font-bold">Timing Waterfall</h3>
            <div class="rounded-xl bg-surface-container border border-outline-variant/10 p-4">
              <div class="flex h-5 rounded-md overflow-hidden mb-3">
                {#each timingSegments as seg}
                  <div
                    class="{seg.color} opacity-80 hover:opacity-100 transition-opacity relative group"
                    style="width: {Math.max(seg.pct, 2)}%"
                    title="{seg.label}: {formatLatency(seg.ms)}"
                  >
                    <div class="absolute -top-8 left-1/2 -translate-x-1/2 hidden group-hover:block bg-surface-container-highest text-on-surface text-[10px] font-label px-2 py-1 rounded shadow-lg border border-outline-variant/10 whitespace-nowrap z-10">
                      {seg.label}: {formatLatency(seg.ms)}
                    </div>
                  </div>
                {/each}
              </div>
              <div class="flex items-center gap-4 text-[10px] font-label">
                {#each timingSegments as seg}
                  <div class="flex items-center gap-1.5">
                    <div class="w-2 h-2 rounded-sm {seg.color} opacity-80"></div>
                    <span class="text-outline">{seg.label}</span>
                    <span class="text-on-surface-variant font-mono tabular-nums">{formatLatency(seg.ms)}</span>
                  </div>
                {/each}
                <div class="ml-auto text-outline">
                  Total: <span class="text-on-surface font-mono font-medium">{formatLatency(detail.totalLatencyMs)}</span>
                </div>
              </div>
            </div>
          </div>
        {/if}

        <!-- Metadata grid -->
        <div class="grid grid-cols-2 lg:grid-cols-4 gap-x-6 gap-y-4">
          {#each [
            { label: 'Request ID', value: detail.id, mono: true, colSpan: 2 },
            { label: 'Timestamp', value: new Date(detail.timestamp).toLocaleString() },
            { label: 'Region', value: detail.region?.toUpperCase() },
            { label: 'Merchant', value: detail.merchantKey || '-', mono: true },
            { label: 'Upstream Latency', value: formatLatency(detail.upstreamLatencyMs) },
            { label: 'Total Latency', value: formatLatency(detail.totalLatencyMs) },
            { label: 'Cache Status', value: detail.cacheStatus },
            { label: 'Request Size', value: formatBytes(detail.requestContentLength) },
            { label: 'Response Size', value: formatBytes(detail.responseContentLength) },
            { label: 'PII Redacted', value: detail.piiRedacted ? 'Yes' : 'No', warn: detail.piiRedacted }
          ] as field}
            <div class={field.colSpan ? `col-span-${field.colSpan}` : ''}>
              <div class="text-[10px] font-label text-outline uppercase tracking-wider mb-1 font-bold">{field.label}</div>
              <div class="text-sm font-label {field.mono ? 'font-mono text-primary' : ''} {field.warn ? 'text-error' : 'text-on-surface'} break-all">
                {field.value}
              </div>
            </div>
          {/each}

          {#if detail.errorReason}
            <div class="col-span-2">
              <div class="text-[10px] font-label text-outline uppercase tracking-wider mb-1 font-bold">Error Reason</div>
              <div class="text-sm font-mono text-error break-all">{detail.errorReason}</div>
            </div>
          {/if}

          {#if detail.amazonRequestId}
            <div class="col-span-2">
              <div class="text-[10px] font-label text-outline uppercase tracking-wider mb-1 font-bold">Amazon Request ID</div>
              <div class="text-sm font-mono text-primary break-all">{detail.amazonRequestId}</div>
            </div>
          {/if}

          {#if detail.queued}
            <div>
              <div class="text-[10px] font-label text-outline uppercase tracking-wider mb-1 font-bold">Queue Wait</div>
              <div class="text-sm text-primary font-mono">{formatLatency(detail.queueWaitMs)}</div>
            </div>
          {/if}
        </div>

        <!-- Query Parameters -->
        {#if queryPairs.length > 0}
          <div>
            <h3 class="text-[10px] uppercase font-label tracking-widest text-outline mb-3 font-bold">Query Parameters</h3>
            <div class="rounded-xl bg-surface-container border border-outline-variant/10 overflow-hidden">
              <div class="grid grid-cols-[140px_1fr] gap-x-8 gap-y-2 font-label text-sm p-4">
                {#each queryPairs as pair}
                  <span class="text-on-surface-variant opacity-60">{pair.key}</span>
                  <span class="text-on-surface font-mono break-all">{pair.value}</span>
                {/each}
              </div>
            </div>
          </div>
        {/if}

        <!-- Cached from -->
        {#if detail.cachedFromId}
          <div class="rounded-xl bg-secondary/5 border border-secondary/20 p-4">
            <div class="text-[10px] font-label text-outline uppercase tracking-wider mb-2 font-bold">Served from cache</div>
            <div class="flex items-center gap-3 text-xs font-label">
              <button
                onclick={() => onselectlog(detail.cachedFromId!)}
                class="text-secondary hover:text-secondary-fixed font-mono underline underline-offset-2 transition-all"
              >{detail.cachedFromId.slice(0, 16)}&hellip;</button>
              {#if detail.cachedFromTimestamp}
                <span class="text-on-surface-variant">fetched {new Date(detail.cachedFromTimestamp).toLocaleString()}</span>
              {/if}
              {#if detail.cachedFromStatus}
                <span class="{statusColor(detail.cachedFromStatus)} font-bold tabular-nums">{detail.cachedFromStatus}</span>
              {/if}
            </div>
          </div>
        {/if}

      {:else if activeTab === 'headers'}
        <div class="grid grid-cols-1 lg:grid-cols-2 gap-6">
          {#if detail.requestHeaders && Object.keys(detail.requestHeaders).length > 0}
            <div>
              <h3 class="text-[10px] uppercase font-label tracking-widest text-outline mb-3 font-bold">Request Headers</h3>
              <div class="rounded-xl bg-surface-container border border-outline-variant/10 overflow-hidden">
                <div class="grid grid-cols-[140px_1fr] gap-x-8 gap-y-2 font-label text-sm p-4">
                  {#each Object.entries(detail.requestHeaders) as [key, value]}
                    <span class="{isImportantHeader(key) ? 'text-primary' : 'text-on-surface-variant opacity-60'}">{key}</span>
                    <span class="text-on-surface font-mono break-all truncate">{value}</span>
                  {/each}
                </div>
              </div>
            </div>
          {/if}
          {#if detail.responseHeaders && Object.keys(detail.responseHeaders).length > 0}
            <div>
              <h3 class="text-[10px] uppercase font-label tracking-widest text-outline mb-3 font-bold">Response Headers</h3>
              <div class="rounded-xl bg-surface-container border border-outline-variant/10 overflow-hidden">
                <div class="grid grid-cols-[140px_1fr] gap-x-8 gap-y-2 font-label text-sm p-4">
                  {#each Object.entries(detail.responseHeaders) as [key, value]}
                    <span class="{isImportantHeader(key) ? 'text-primary' : 'text-on-surface-variant opacity-60'}">{key}</span>
                    <span class="text-on-surface font-mono break-all truncate">{value}</span>
                  {/each}
                </div>
              </div>
            </div>
          {/if}
        </div>

      {:else if activeTab === 'body'}
        {#if !detail.hasBody}
          <div class="py-12 text-center">
            <p class="text-on-surface-variant text-sm font-body">No body stored for this request</p>
          </div>
        {:else if body}
          {#if detail.cachedFromId}
            <div class="rounded-xl bg-secondary/5 border border-secondary/20 p-3 text-xs text-secondary font-label flex items-center gap-2">
              <span class="material-symbols-outlined text-sm">info</span>
              Body from original request
              <button
                onclick={() => onselectlog(detail.cachedFromId!)}
                class="underline underline-offset-2 hover:text-secondary-fixed font-mono transition-all"
              >{detail.cachedFromId.slice(0, 12)}&hellip;</button>
            </div>
          {/if}
          <div class="space-y-4">
            {#if body.requestBody}
              <div>
                <h3 class="text-[10px] uppercase font-label tracking-widest text-outline mb-3 font-bold">Request Body</h3>
                <JsonView data={body.requestBody} />
              </div>
            {/if}
            {#if body.responseBody}
              <div>
                <h3 class="text-[10px] uppercase font-label tracking-widest text-outline mb-3 font-bold">Response Body</h3>
                <JsonView data={body.responseBody} />
              </div>
            {/if}
            {#if !body.requestBody && !body.responseBody}
              <p class="text-on-surface-variant text-sm py-8 text-center font-body">Body is empty</p>
            {/if}
          </div>
        {:else}
          <div class="flex items-center justify-center py-12">
            <div class="w-5 h-5 rounded-full border-2 border-primary/30 border-t-primary animate-spin"></div>
          </div>
        {/if}
      {/if}
    </div>

    <!-- Modal Footer -->
    <div class="p-4 bg-surface-container-high/40 border-t border-outline-variant/10 flex justify-between items-center px-6 shrink-0">
      <div class="flex gap-4">
        <div class="flex items-center gap-2">
          <span class="text-[10px] font-label text-outline uppercase font-bold">Latency</span>
          <span class="text-sm font-label {detail.totalLatencyMs >= 500 ? 'text-error' : 'text-green-400'} font-bold">{formatLatency(detail.totalLatencyMs)}</span>
        </div>
        <div class="w-px h-4 bg-outline-variant/30"></div>
        <div class="flex items-center gap-2">
          <span class="text-[10px] font-label text-outline uppercase font-bold">Size</span>
          <span class="text-sm font-label text-on-surface font-bold">{formatBytes(detail.responseContentLength)}</span>
        </div>
        <div class="w-px h-4 bg-outline-variant/30"></div>
        <div class="flex items-center gap-2">
          <span class="text-[10px] font-label text-outline uppercase font-bold">Status</span>
          <span class="text-sm font-label {statusColor(detail.statusCode)} font-bold">{detail.statusCode}</span>
        </div>
      </div>
      <button
        onclick={onclose}
        class="bg-surface-bright hover:bg-surface-container-highest text-on-surface px-4 py-2 rounded-lg transition-all text-xs font-label font-bold border border-outline-variant/20"
      >Close</button>
    </div>
  </div>
</div>
