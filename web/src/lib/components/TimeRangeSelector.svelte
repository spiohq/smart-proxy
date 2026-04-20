<script lang="ts">
  import type { TimeRange } from '$lib/api';

  let {
    selected = $bindable<TimeRange>('1h'),
    customFrom = $bindable(''),
    customTo = $bindable(''),
    onchange
  }: {
    selected: TimeRange;
    customFrom?: string;
    customTo?: string;
    onchange?: (range: TimeRange) => void;
  } = $props();

  const presets: { value: TimeRange; label: string }[] = [
    { value: '15m', label: '15M' },
    { value: '1h', label: '1H' },
    { value: '6h', label: '6H' },
    { value: '24h', label: '24H' },
    { value: '7d', label: '7D' },
    { value: '30d', label: '30D' }
  ];

  function select(range: TimeRange) {
    selected = range;
    onchange?.(range);
  }

  function onCustomChange() {
    if (customFrom && customTo) {
      selected = 'custom';
      onchange?.('custom');
    }
  }

  // Current datetime in YYYY-MM-DDTHH:mm format for max attribute.
  let nowLocal = $derived.by(() => {
    const d = new Date();
    return d.getFullYear() + '-' +
      String(d.getMonth() + 1).padStart(2, '0') + '-' +
      String(d.getDate()).padStart(2, '0') + 'T' +
      String(d.getHours()).padStart(2, '0') + ':' +
      String(d.getMinutes()).padStart(2, '0');
  });
</script>

<div class="flex items-center gap-3 flex-wrap">
  <div class="flex bg-surface-container rounded-lg p-1">
    {#each presets as range}
      <button
        onclick={() => select(range.value)}
        class="px-4 py-1.5 text-xs font-label font-bold transition-colors
          {selected === range.value
            ? 'bg-surface-bright text-primary rounded-md shadow-sm'
            : 'text-on-surface-variant hover:text-on-background'}"
      >
        {range.label}
      </button>
    {/each}
    <button
      onclick={() => { selected = 'custom'; }}
      class="px-4 py-1.5 text-xs font-label font-bold transition-colors
        {selected === 'custom'
          ? 'bg-surface-bright text-primary rounded-md shadow-sm'
          : 'text-on-surface-variant hover:text-on-background'}"
    >
      Custom
    </button>
  </div>

  {#if selected === 'custom'}
    <div class="flex items-center gap-2">
      <input
        type="datetime-local"
        bind:value={customFrom}
        max={nowLocal}
        onchange={onCustomChange}
        class="h-8 rounded-lg bg-surface-container-highest border-none px-3 text-xs font-label text-on-surface
          focus:ring-1 focus:ring-primary/40"
      />
      <span class="text-outline text-xs font-label">to</span>
      <input
        type="datetime-local"
        bind:value={customTo}
        max={nowLocal}
        onchange={onCustomChange}
        class="h-8 rounded-lg bg-surface-container-highest border-none px-3 text-xs font-label text-on-surface
          focus:ring-1 focus:ring-primary/40"
      />
    </div>
  {/if}
</div>
