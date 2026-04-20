<script lang="ts">
  type Column = {
    key: string;
    label: string;
    align?: 'left' | 'right';
    format?: (value: any, row: any) => string;
    class?: (value: any, row: any) => string;
    mono?: boolean;
  };

  let { columns, rows, emptyMessage = 'No data' }: {
    columns: Column[];
    rows: any[];
    emptyMessage?: string;
  } = $props();

  let sortKey = $state('');
  let sortDir = $state<'asc' | 'desc'>('desc');

  function toggleSort(key: string) {
    if (sortKey === key) {
      sortDir = sortDir === 'asc' ? 'desc' : 'asc';
    } else {
      sortKey = key;
      sortDir = 'desc';
    }
  }

  let sortedRows = $derived.by(() => {
    if (!sortKey) return rows;
    const col = columns.find(c => c.key === sortKey);
    if (!col) return rows;
    return [...rows].sort((a, b) => {
      const av = a[sortKey];
      const bv = b[sortKey];
      if (typeof av === 'number' && typeof bv === 'number') {
        return sortDir === 'asc' ? av - bv : bv - av;
      }
      const as = String(av ?? '');
      const bs = String(bv ?? '');
      return sortDir === 'asc' ? as.localeCompare(bs) : bs.localeCompare(as);
    });
  });
</script>

<div class="bg-surface-container rounded-xl overflow-hidden">
  <div class="overflow-x-auto">
    <table class="w-full text-left border-collapse">
      <thead>
        <tr class="bg-surface-container-high/30">
          {#each columns as col}
            <th
              class="p-4 text-xs font-label text-outline uppercase tracking-wider cursor-pointer hover:text-on-surface-variant transition-all select-none
                {col.align === 'right' ? 'text-right' : 'text-left'}"
              onclick={() => toggleSort(col.key)}
            >
              <span class="inline-flex items-center gap-1">
                {col.label}
                {#if sortKey === col.key}
                  <span class="material-symbols-outlined text-primary" style="font-size: 14px;">
                    {sortDir === 'asc' ? 'arrow_upward' : 'arrow_downward'}
                  </span>
                {/if}
              </span>
            </th>
          {/each}
        </tr>
      </thead>
      <tbody class="divide-y divide-outline-variant/5">
        {#each sortedRows as row (JSON.stringify(row))}
          <tr class="hover:bg-surface-bright/20 transition-colors">
            {#each columns as col}
              {@const value = row[col.key]}
              {@const formatted = col.format ? col.format(value, row) : String(value ?? '-')}
              {@const extraClass = col.class ? col.class(value, row) : ''}
              <td class="p-4 font-label text-sm tabular-nums {col.align === 'right' ? 'text-right' : ''} {col.mono ? 'font-mono text-xs text-primary' : 'text-on-surface'} {extraClass}">
                {formatted}
              </td>
            {/each}
          </tr>
        {/each}
        {#if sortedRows.length === 0}
          <tr><td colspan={columns.length} class="p-8 text-center text-outline font-label">{emptyMessage}</td></tr>
        {/if}
      </tbody>
    </table>
  </div>
</div>
