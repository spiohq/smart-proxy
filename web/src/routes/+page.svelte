<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import TimeRangeSelector from '$lib/components/TimeRangeSelector.svelte';
  import FilterBar from '$lib/components/FilterBar.svelte';
  import RequestTable from '$lib/components/RequestTable.svelte';
  import RequestDetailModal from '$lib/components/RequestDetailModal.svelte';
  import { rangeToISO, type TimeRange, type LogEntry } from '$lib/api';
  import {
    logs, totalLogs, selectedLog, logBody, loading, detailLoading, currentPage, newCount,
    fetchLogs, pollNewLogs, fetchLogDetail, fetchLogBody, clearSelection,
    type LogFilters
  } from '$lib/stores/requests';
  import { createAutoRefresh } from '$lib/stores/autoRefresh';

  let selectedRange: TimeRange = $state('1h');
  let customFrom = $state('');
  let customTo = $state('');
  let merchant = $state('');
  let region = $state('');
  let endpoint = $state('');
  let status = $state('');
  let cacheStatus = $state('');
  let method = $state('');
  let queued = $state('');
  let minLatency = $state('');
  let maxLatency = $state('');
  let autoRefreshEnabled = $state(true);

  const REFRESH_INTERVAL = 5_000;
  const PAGE_SIZE = 50;

  const refreshCtrl = createAutoRefresh(() => {
    if (selectedRange !== 'custom' && $currentPage === 0) {
      pollNewLogs(getFilters());
    }
  }, REFRESH_INTERVAL);

  function getFilters(): LogFilters {
    const { from, to } = rangeToISO(selectedRange, customFrom, customTo);
    return {
      from, to,
      merchant: merchant || undefined,
      region: region || undefined,
      endpoint: endpoint || undefined,
      status: status || undefined,
      cacheStatus: cacheStatus || undefined,
      method: method || undefined,
      queued: queued || undefined,
      minLatency: minLatency || undefined,
      maxLatency: maxLatency || undefined
    };
  }

  function syncAutoRefresh() {
    if (autoRefreshEnabled && selectedRange !== 'custom' && $currentPage === 0) {
      refreshCtrl.start();
    } else {
      refreshCtrl.stop();
    }
  }

  function refresh() {
    fetchLogs(getFilters(), 0);
    syncAutoRefresh();
  }

  function goToPage(page: number) {
    fetchLogs(getFilters(), page);
    if (page !== 0) refreshCtrl.stop();
    else syncAutoRefresh();
  }

  function onSelectRow(entry: LogEntry) {
    fetchLogDetail(entry.id);
  }

  function onLoadBody() {
    if ($selectedLog) fetchLogBody($selectedLog.id);
  }

  function onSelectLinkedLog(id: string) {
    fetchLogDetail(id);
  }

  function closeModal() {
    clearSelection();
  }

  function toggleAutoRefresh() {
    autoRefreshEnabled = !autoRefreshEnabled;
    syncAutoRefresh();
  }

  onMount(() => {
    refresh();
  });

  onDestroy(() => {
    refreshCtrl.stop();
  });

  let totalPages = $derived(Math.ceil($totalLogs / PAGE_SIZE));

  let pageButtons = $derived.by(() => {
    const current = $currentPage;
    const total = totalPages;
    if (total <= 7) return Array.from({ length: total }, (_, i) => i);
    const pages: number[] = [0];
    const start = Math.max(1, current - 1);
    const end = Math.min(total - 2, current + 1);
    if (start > 1) pages.push(-1);
    for (let i = start; i <= end; i++) pages.push(i);
    if (end < total - 2) pages.push(-1);
    pages.push(total - 1);
    return pages;
  });
</script>

<div class="space-y-6">
  <!-- Page Header -->
  <div class="flex justify-between items-end">
    <div>
      <h1 class="text-4xl font-extrabold font-headline tracking-tight text-on-background mb-2">Request Log Browser</h1>
      <p class="text-on-surface-variant font-body">Real-time inspection of inbound and outbound API traffic across the SP ecosystem.</p>
    </div>
    <div class="flex gap-3">
      {#if $newCount > 0}
        <span class="text-[10px] font-semibold text-secondary tabular-nums animate-pulse font-label self-center">+{$newCount} new</span>
      {/if}
      <button
        onclick={toggleAutoRefresh}
        class="bg-surface-container hover:bg-surface-bright text-primary px-4 py-2 rounded-xl transition-all flex items-center gap-2 font-label text-sm border border-outline-variant/20"
      >
        <span class="relative flex h-2 w-2">
          {#if autoRefreshEnabled}
            <span class="animate-ping absolute inline-flex h-full w-full rounded-full bg-[#2dd4bf] opacity-75"></span>
          {/if}
          <span class="relative inline-flex rounded-full h-2 w-2 {autoRefreshEnabled ? 'bg-[#2dd4bf]' : 'bg-outline'}"></span>
        </span>
        {autoRefreshEnabled ? 'Auto-refresh' : 'Paused'}
      </button>
      <button
        onclick={() => refresh()}
        class="gradient-primary text-on-primary px-4 py-2 rounded-xl transition-all flex items-center gap-2 font-label text-sm font-bold"
      >
        <span class="material-symbols-outlined text-sm">refresh</span> Refresh
      </button>
    </div>
  </div>

  <!-- Controls -->
  <div class="flex items-start gap-4 flex-wrap justify-between">
    <TimeRangeSelector bind:selected={selectedRange} bind:customFrom bind:customTo onchange={() => refresh()} />
  </div>

  <!-- Advanced Filter Bar -->
  <section class="bg-surface-container rounded-xl p-6 border border-outline-variant/10 shadow-sm">
    <FilterBar
      bind:merchant bind:region bind:endpoint bind:status bind:cacheStatus bind:method bind:queued bind:minLatency bind:maxLatency
      showStatus showLatency showMethod onchange={refresh}
    />
  </section>

  <!-- Table -->
  {#if $loading}
    <div class="h-96 rounded-xl bg-surface-container flex items-center justify-center">
      <div class="flex items-center gap-2 text-outline">
        <div class="w-4 h-4 rounded-full border-2 border-primary/30 border-t-primary animate-spin"></div>
        <span class="text-xs font-label">Loading requests...</span>
      </div>
    </div>
  {:else}
    <RequestTable rows={$logs} onselect={onSelectRow} selectedId={$selectedLog?.id ?? ''} />
  {/if}

  <!-- Pagination -->
  {#if $totalLogs > 0}
    <div class="flex items-center justify-between py-4 font-label text-sm text-on-surface-variant">
      <div class="flex items-center gap-4">
        <span class="text-xs uppercase tracking-widest text-outline">
          Showing {$currentPage * PAGE_SIZE + 1}-{Math.min(($currentPage + 1) * PAGE_SIZE, $totalLogs)} of {$totalLogs.toLocaleString()} requests
        </span>
      </div>
      {#if totalPages > 1}
        <div class="flex items-center gap-1">
          <button
            onclick={() => goToPage($currentPage - 1)}
            disabled={$currentPage === 0}
            class="w-8 h-8 flex items-center justify-center rounded-lg hover:bg-surface-container-highest transition-colors disabled:opacity-30 disabled:cursor-not-allowed"
          >
            <span class="material-symbols-outlined text-sm">chevron_left</span>
          </button>
          {#each pageButtons as p}
            {#if p === -1}
              <span class="px-1 opacity-40 text-outline">...</span>
            {:else}
              <button
                onclick={() => goToPage(p)}
                class="w-8 h-8 flex items-center justify-center rounded-lg font-bold tabular-nums transition-colors
                  {$currentPage === p
                    ? 'bg-primary text-on-primary'
                    : 'hover:bg-surface-container-highest text-on-surface-variant'}"
              >
                {p + 1}
              </button>
            {/if}
          {/each}
          <button
            onclick={() => goToPage($currentPage + 1)}
            disabled={$currentPage + 1 >= totalPages}
            class="w-8 h-8 flex items-center justify-center rounded-lg hover:bg-surface-container-highest transition-colors disabled:opacity-30 disabled:cursor-not-allowed"
          >
            <span class="material-symbols-outlined text-sm">chevron_right</span>
          </button>
        </div>
      {/if}
    </div>
  {/if}
</div>

<!-- Detail Modal -->
{#if $selectedLog}
  <RequestDetailModal
    detail={$selectedLog}
    body={$logBody}
    detailLoading={$detailLoading}
    onclose={closeModal}
    onloadbody={onLoadBody}
    onselectlog={onSelectLinkedLog}
  />
{/if}
