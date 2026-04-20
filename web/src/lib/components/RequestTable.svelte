<script lang="ts">
  import type { LogEntry } from '$lib/api';
  import { formatLatency } from '$lib/api';

  let { rows, onselect, selectedId = '' }: {
    rows: LogEntry[];
    onselect?: (entry: LogEntry) => void;
    selectedId?: string;
  } = $props();

  function statusColor(code: number): string {
    if (code >= 500) return 'bg-error/10 text-error';
    if (code >= 400) return 'bg-primary/10 text-primary';
    if (code >= 300) return 'bg-secondary/10 text-secondary';
    return 'bg-green-500/10 text-green-400';
  }

  function statusLabel(code: number): string {
    if (code === 200) return '200 OK';
    if (code === 201) return '201 Created';
    if (code === 204) return '204 No Content';
    if (code === 429) return '429 Rate Limited';
    return String(code);
  }

  function cacheBadge(status: string): { text: string; cls: string } {
    if (status === 'HIT') return { text: 'HIT', cls: 'text-secondary font-bold' };
    if (status === 'MISS') return { text: 'MISS', cls: 'text-outline font-bold' };
    if (status === 'BYPASS') return { text: 'BYPASS', cls: 'text-primary font-bold' };
    if (status === 'PII_EXCLUDED') return { text: 'PII', cls: 'text-tertiary font-bold' };
    return { text: status || '-', cls: 'text-outline' };
  }

  function cacheDotColor(status: string): string {
    if (status === 'HIT') return 'bg-secondary';
    if (status === 'MISS') return 'bg-outline-variant';
    if (status === 'BYPASS') return 'bg-primary';
    return 'bg-outline-variant';
  }

  function methodBadge(m: string): { cls: string } {
    if (m === 'GET') return { cls: 'bg-secondary-container/10 text-secondary border border-secondary/20' };
    if (m === 'POST') return { cls: 'bg-primary/10 text-primary border border-primary/20' };
    if (m === 'PUT') return { cls: 'bg-[#d98f5d]/10 text-primary border border-[#d98f5d]/20' };
    if (m === 'DELETE') return { cls: 'bg-error/10 text-error border border-error/20' };
    if (m === 'PATCH') return { cls: 'bg-tertiary/10 text-tertiary border border-tertiary/20' };
    return { cls: 'bg-surface-container-highest text-on-surface-variant' };
  }

  function latencyColor(ms: number): string {
    if (ms >= 2000) return 'text-error font-bold';
    if (ms >= 500) return 'text-primary';
    return 'text-on-surface-variant';
  }

  function formatTime(ts: string): string {
    return new Date(ts).toLocaleTimeString('en-US', { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' });
  }

  function formatMs(ts: string): string {
    const d = new Date(ts);
    return '.' + d.getMilliseconds().toString().padStart(3, '0');
  }

  function formatDate(ts: string): string {
    return new Date(ts).toLocaleDateString('en-US', { year: 'numeric', month: '2-digit', day: '2-digit' });
  }
</script>

<div class="bg-surface-container rounded-xl overflow-hidden border border-outline-variant/10 shadow-lg">
  <table class="w-full text-left border-collapse">
    <thead class="bg-surface-container-high/50 border-b border-outline-variant/10">
      <tr class="font-label text-xs uppercase tracking-[0.1em] text-outline">
        <th class="px-6 py-4 font-bold">Timestamp</th>
        <th class="px-6 py-4 font-bold">Method</th>
        <th class="px-6 py-4 font-bold">Path</th>
        <th class="px-6 py-4 font-bold">Status</th>
        <th class="px-6 py-4 font-bold text-center">Cache</th>
        <th class="px-6 py-4 font-bold text-right">Latency</th>
        <th class="px-6 py-4 font-bold text-right">Size</th>
      </tr>
    </thead>
    <tbody class="divide-y divide-outline-variant/5">
      {#each rows as row, i (row.id)}
        <tr
          class="group cursor-pointer transition-all
            {selectedId === row.id ? 'bg-primary/5 border-l-2 border-l-primary' : ''}
            {i % 2 === 1 ? 'bg-surface-container-low/30' : ''}
            hover:bg-surface-bright"
          onclick={() => onselect?.(row)}
        >
          <td class="px-6 py-4 font-label text-sm text-on-surface-variant">
            {formatDate(row.timestamp)} <span class="opacity-40">{formatTime(row.timestamp)}{formatMs(row.timestamp)}</span>
          </td>
          <td class="px-6 py-4">
            <span class="px-2 py-0.5 rounded-md text-[10px] font-bold font-label {methodBadge(row.method).cls}">{row.method}</span>
          </td>
          <td class="px-6 py-4 font-label text-sm font-medium text-on-surface max-w-md">
            <span class="block truncate" title={row.path}>{row.path}</span>
          </td>
          <td class="px-6 py-4">
            <span class="px-2.5 py-1 rounded-full text-[10px] font-bold font-label {statusColor(row.statusCode)}">{statusLabel(row.statusCode)}</span>
            {#if row.errorReason}
              <div class="text-[9px] text-error/70 truncate max-w-[8rem] mt-0.5">{row.errorReason}</div>
            {/if}
          </td>
          <td class="px-6 py-4 text-center">
            <div class="flex items-center justify-center gap-2">
              <span class="w-2 h-2 rounded-full {cacheDotColor(row.cacheStatus)}"></span>
              <span class="text-[10px] font-label {cacheBadge(row.cacheStatus).cls}">{cacheBadge(row.cacheStatus).text}</span>
            </div>
          </td>
          <td class="px-6 py-4 text-right font-label text-sm tabular-nums {latencyColor(row.totalLatencyMs)}">
            {formatLatency(row.totalLatencyMs)}
          </td>
          <td class="px-6 py-4 text-right font-label text-sm text-on-surface-variant tabular-nums">
            {row.responseContentLength > 0 ? (row.responseContentLength / 1024).toFixed(1) + 'kb' : '-'}
          </td>
        </tr>
      {/each}
      {#if rows.length === 0}
        <tr>
          <td colspan="7" class="px-6 py-16 text-center">
            <div class="text-on-surface-variant text-sm font-body">No requests found</div>
            <div class="text-outline text-xs font-label mt-1">Adjust filters or time range</div>
          </td>
        </tr>
      {/if}
    </tbody>
  </table>
</div>
