<script lang="ts">
  import { onMount } from 'svelte';
  import TimeRangeSelector from '$lib/components/TimeRangeSelector.svelte';
  import { rangeToISO, getAuditEvents, type TimeRange, type AuditEvent } from '$lib/api';

  let selectedRange: TimeRange = $state('24h');
  let customFrom = $state('');
  let customTo = $state('');
  let loading = $state(false);
  let rows: AuditEvent[] = $state([]);
  let total = $state(0);
  let currentPage = $state(0);
  let eventTypeFilter = $state('');
  let expandedId = $state<string | null>(null);
  const PAGE_SIZE = 50;

  async function refresh(page = 0) {
    loading = true;
    currentPage = page;
    try {
      const { from, to } = rangeToISO(selectedRange, customFrom, customTo);
      const data = await getAuditEvents({
        from, to,
        eventType: eventTypeFilter || undefined,
        limit: String(PAGE_SIZE),
        offset: String(page * PAGE_SIZE),
      });
      rows = data.rows || [];
      total = data.total;
    } catch {
      rows = [];
      total = 0;
    } finally {
      loading = false;
    }
  }

  onMount(() => refresh());

  let totalPages = $derived(Math.ceil(total / PAGE_SIZE));

  function toggleExpand(id: string) {
    expandedId = expandedId === id ? null : id;
  }

  function setEventFilter(type: string) {
    eventTypeFilter = eventTypeFilter === type ? '' : type;
    refresh(0);
  }

  async function exportLogs() {
    const { from, to } = rangeToISO(selectedRange, customFrom, customTo);
    try {
      const data = await getAuditEvents({
        from, to,
        eventType: eventTypeFilter || undefined,
        limit: String(10000),
        offset: '0',
      });
      const json = JSON.stringify(data.rows || [], null, 2);
      const blob = new Blob([json], { type: 'application/json' });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `audit-log-${new Date().toISOString().slice(0, 10)}.json`;
      a.click();
      URL.revokeObjectURL(url);
    } catch {
      // silently fail
    }
  }

  // Event type badge styling
  function eventBadge(eventType: string): { cls: string; label: string } {
    const et = eventType.toLowerCase();
    if (et.includes('error') || et.includes('fail'))
      return { cls: 'bg-error-container text-on-error-container', label: 'Error' };
    if (et.includes('warn'))
      return { cls: 'bg-primary/20 text-primary border border-primary/20', label: 'Warning' };
    if (et.includes('security') || et.includes('auth'))
      return { cls: 'bg-secondary-container text-on-secondary-container', label: 'Security' };
    if (et.includes('config'))
      return { cls: 'bg-primary-container/20 text-primary-container border border-primary-container/40', label: 'Config' };
    return { cls: 'bg-surface-variant text-on-surface-variant', label: 'Info' };
  }

  // Event type filter buttons
  const filterButtons = [
    { type: 'error', label: 'Error', cls: 'bg-error-container/20 text-error border-error/20', dotCls: 'bg-error' },
    { type: 'warn', label: 'Warning', cls: 'bg-primary/10 text-primary border-primary/20', dotCls: 'bg-primary' },
    { type: 'security', label: 'Security', cls: 'bg-secondary/10 text-secondary border-secondary/20', dotCls: 'bg-secondary' },
    { type: 'config', label: 'Config Change', cls: 'bg-primary-container/10 text-primary-container border-primary-container/20', dotCls: 'bg-primary-container' },
  ];

  // Pagination buttons
  let pageButtons = $derived.by(() => {
    const current = currentPage;
    const t = totalPages;
    if (t <= 7) return Array.from({ length: t }, (_, i) => i);
    const pages: number[] = [0];
    const start = Math.max(1, current - 1);
    const end = Math.min(t - 2, current + 1);
    if (start > 1) pages.push(-1);
    for (let i = start; i <= end; i++) pages.push(i);
    if (end < t - 2) pages.push(-1);
    pages.push(t - 1);
    return pages;
  });
</script>

<div class="space-y-8">
  <!-- Header Section -->
  <header class="flex flex-col md:flex-row md:items-end justify-between gap-6">
    <div>
      <h1 class="text-4xl font-extrabold font-headline text-on-surface tracking-tight mb-2">Audit Log</h1>
      <p class="text-on-surface-variant font-body max-w-2xl">Full traceability for system-wide configuration and security events.</p>
    </div>
    <div class="flex gap-3">
      <button onclick={exportLogs} class="gradient-primary text-on-primary px-6 py-2 rounded-lg font-bold flex items-center gap-2 hover:shadow-[0_0_15px_rgba(255,182,135,0.4)] transition-all active:scale-95">
        <span class="material-symbols-outlined text-sm" style="font-variation-settings: 'FILL' 1;">download</span>
        <span class="font-label text-xs uppercase tracking-widest">Export Logs</span>
      </button>
    </div>
  </header>

  <!-- Controls / Filters -->
  <section class="space-y-6">
    <div class="flex flex-wrap items-center gap-6">
      <!-- Time Range Selector -->
      <TimeRangeSelector bind:selected={selectedRange} bind:customFrom bind:customTo onchange={() => refresh()} />

      <!-- Event Type Filters -->
      <div class="flex flex-wrap items-center gap-2">
        {#each filterButtons as btn}
          <button
            onclick={() => setEventFilter(btn.type)}
            class="px-3 py-1.5 rounded-md text-[10px] font-bold font-label uppercase tracking-wider border flex items-center gap-2 transition-all
              {eventTypeFilter === btn.type ? btn.cls + ' ring-1 ring-current' : btn.cls + ' opacity-60 hover:opacity-100'}"
          >
            <span class="w-1.5 h-1.5 rounded-full {btn.dotCls}"></span> {btn.label}
          </button>
        {/each}
        <button
          onclick={() => setEventFilter('')}
          class="px-3 py-1.5 rounded-md text-[10px] font-bold font-label uppercase tracking-wider border border-outline-variant/30 text-on-surface-variant hover:bg-surface-variant transition-colors
            {eventTypeFilter === '' ? 'bg-surface-variant' : 'bg-transparent'}"
        >
          All
        </button>
      </div>

      <!-- Total count -->
      <span class="text-[11px] text-outline tabular-nums font-label ml-auto">{total.toLocaleString()} events</span>
    </div>
  </section>

  <!-- Audit Table -->
  {#if loading}
    <div class="h-64 rounded-xl bg-surface-container flex items-center justify-center">
      <div class="flex items-center gap-2 text-outline">
        <div class="w-4 h-4 rounded-full border-2 border-primary/30 border-t-primary animate-spin"></div>
        <span class="text-xs font-label">Loading...</span>
      </div>
    </div>
  {:else}
    <section class="bg-surface-container rounded-xl overflow-hidden shadow-2xl shadow-black/60">
      <!-- Table Header -->
      <div class="grid grid-cols-[180px_140px_160px_1fr_100px] bg-surface-container-high px-6 py-4">
        <span class="text-[10px] font-bold font-label uppercase tracking-widest text-on-surface-variant">Timestamp</span>
        <span class="text-[10px] font-bold font-label uppercase tracking-widest text-on-surface-variant">Event Type</span>
        <span class="text-[10px] font-bold font-label uppercase tracking-widest text-on-surface-variant">Source</span>
        <span class="text-[10px] font-bold font-label uppercase tracking-widest text-on-surface-variant">Message</span>
        <span class="text-[10px] font-bold font-label uppercase tracking-widest text-on-surface-variant text-right">Action</span>
      </div>

      <!-- Rows -->
      <div class="divide-y divide-transparent">
        {#each rows as event (event.id)}
          {@const badge = eventBadge(event.eventType)}
          {@const isExpanded = expandedId === event.id}
          {@const hasMeta = event.metadata && Object.keys(event.metadata).length > 0}

          <div class="group border-none {isExpanded ? 'bg-surface-container-low' : ''}">
            <!-- Row -->
            <button
              class="grid grid-cols-[180px_140px_160px_1fr_100px] items-center px-6 py-4 w-full text-left hover:bg-surface-bright transition-all cursor-pointer"
              onclick={() => hasMeta && toggleExpand(event.id)}
            >
              <span class="text-xs font-label text-on-surface/80">{new Date(event.timestamp).toLocaleString()}</span>
              <div>
                <span class="text-[9px] font-bold px-2 py-0.5 rounded uppercase {badge.cls}">{event.eventType}</span>
              </div>
              <span class="text-xs font-label text-secondary truncate">{event.source}</span>
              <span class="text-sm font-body text-on-surface truncate">{event.message}</span>
              <div class="text-right">
                {#if hasMeta}
                  <span class="material-symbols-outlined text-outline group-hover:text-primary transition-colors {isExpanded ? 'text-primary' : ''}"
                    style={isExpanded ? "font-variation-settings: 'FILL' 1;" : ''}>
                    {isExpanded ? 'unfold_less' : 'unfold_more'}
                  </span>
                {/if}
              </div>
            </button>

            <!-- Expanded Metadata -->
            {#if isExpanded && hasMeta}
              <div class="px-6 pb-6 pt-0">
                <div class="bg-surface-container-lowest p-6 rounded-lg font-label text-xs leading-relaxed overflow-x-auto border-l-2 border-primary/40">
                  <pre class="whitespace-pre">{#each JSON.stringify(event.metadata, null, 2).split('\n') as line}{@html colorizeJsonLine(line)}
{/each}</pre>
                </div>
              </div>
            {/if}
          </div>
        {/each}
        {#if rows.length === 0}
          <div class="px-6 py-16 text-center">
            <div class="text-on-surface-variant text-sm font-body">No audit events found</div>
            <div class="text-outline text-xs font-label mt-1">Adjust filters or time range</div>
          </div>
        {/if}
      </div>
    </section>

    <!-- Pagination -->
    {#if total > 0}
      <footer class="flex items-center justify-between bg-surface-container p-4 rounded-xl">
        <div class="flex items-center gap-4">
          <span class="text-xs font-label text-on-surface-variant">
            Showing {currentPage * PAGE_SIZE + 1}-{Math.min((currentPage + 1) * PAGE_SIZE, total)} of {total.toLocaleString()} events
          </span>
        </div>
        {#if totalPages > 1}
          <div class="flex items-center gap-2">
            <button
              onclick={() => refresh(currentPage - 1)}
              disabled={currentPage === 0}
              class="p-2 rounded-lg hover:bg-surface-bright text-on-surface-variant disabled:opacity-30 transition-colors"
            >
              <span class="material-symbols-outlined">chevron_left</span>
            </button>
            <div class="flex items-center gap-1">
              {#each pageButtons as p}
                {#if p === -1}
                  <span class="px-2 text-on-surface-variant">...</span>
                {:else}
                  <button
                    onclick={() => refresh(p)}
                    class="w-8 h-8 flex items-center justify-center text-xs font-label rounded-lg transition-colors tabular-nums
                      {currentPage === p
                        ? 'bg-primary text-on-primary'
                        : 'hover:bg-surface-bright text-on-surface-variant'}"
                  >
                    {p + 1}
                  </button>
                {/if}
              {/each}
            </div>
            <button
              onclick={() => refresh(currentPage + 1)}
              disabled={currentPage + 1 >= totalPages}
              class="p-2 rounded-lg hover:bg-surface-bright text-on-surface-variant disabled:opacity-30 transition-colors"
            >
              <span class="material-symbols-outlined">chevron_right</span>
            </button>
          </div>
        {/if}
      </footer>
    {/if}
  {/if}
</div>

<script lang="ts" module>
  // HTML-escape every character that could break out of a text context.
  // Pentest finding F-14: colorizeJsonLine feeds the result into {@html ...}
  // so any '<', '"', '&' in audit-event metadata would render as raw HTML
  // without escaping. No remote path currently writes user-controlled
  // content into auditLogger.Log, but the latent stored-XSS becomes real
  // the moment one is added.
  function escapeHtml(s: string): string {
    const map: Record<string, string> = {
      '&': '&amp;',
      '<': '&lt;',
      '>': '&gt;',
      '"': '&quot;',
      "'": '&#39;',
    };
    return s.replace(/[&<>"']/g, (c) => map[c]);
  }

  // JSON syntax coloring helper for inline rendering. The input is escaped
  // first so subsequent regex replacements operate on safe entity-encoded
  // strings; the regexes match against &quot; (the escaped form) where the
  // original used ".
  function colorizeJsonLine(line: string): string {
    return escapeHtml(line)
      // Quoted key followed by colon: "key":
      .replace(/&quot;([^&]+?)&quot;(\s*:)/g, '<span class="text-secondary">&quot;$1&quot;</span>$2')
      // Quoted string value: : "value"
      .replace(/:\s*&quot;([^&]+?)&quot;/g, ': <span class="text-primary">&quot;$1&quot;</span>')
      // Numeric value: : 42
      .replace(/:\s*(\d+)/g, ': <span class="text-primary">$1</span>')
      // Boolean
      .replace(/:\s*(true|false)/g, ': <span class="text-tertiary">$1</span>')
      // null
      .replace(/:\s*(null)/g, ': <span class="text-outline">$1</span>')
      // Brackets / braces (already escaped to entities? no -- [{}[\]] are
      // not escaped by escapeHtml, they're not HTML-meaningful)
      .replace(/([{}[\]])/g, '<span class="text-secondary opacity-80">$1</span>');
  }
</script>
