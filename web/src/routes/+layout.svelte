<script lang="ts">
  import '../app.css';
  import { page } from '$app/stores';

  let { children } = $props();

  const navItems = [
    { label: 'Requests', href: '/', icon: 'description' },
    { label: 'Audit', href: '/audit', icon: 'shield' }
  ];
</script>

<div class="min-h-screen bg-background flex flex-col">
  <!-- TopNavBar -->
  <header class="bg-[#131318] fixed top-0 z-50 w-full">
    <div class="flex justify-between items-center w-full px-6 py-4 mx-auto max-w-full">
      <div class="flex items-center gap-8">
        <!-- Logo -->
        <div class="flex items-center gap-2">
          <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 512 512" class="w-8 h-8 rounded">
            <rect width="512" height="512" fill="#232228"/>
            <g stroke-linecap="round" stroke-linejoin="round">
              <line x1="236.9" y1="103" x2="169.1" y2="142" stroke="#e6e9e9" stroke-width="12"/>
              <line x1="275.1" y1="103" x2="342.9" y2="142" stroke="#e6e9e9" stroke-width="12"/>
              <line x1="150" y1="175" x2="150" y2="253" stroke="#e6e9e9" stroke-width="12"/>
              <line x1="362" y1="175" x2="362" y2="253" stroke="#e6e9e9" stroke-width="12"/>
              <line x1="169.1" y1="286" x2="236.9" y2="325" stroke="#e6e9e9" stroke-width="12"/>
              <line x1="342.9" y1="286" x2="275.1" y2="325" stroke="#e6e9e9" stroke-width="12"/>
              <line x1="256" y1="114" x2="256" y2="188" stroke="#e6e9e9" stroke-width="12"/>
              <line x1="169.1" y1="164" x2="233.5" y2="201" stroke="#e6e9e9" stroke-width="12"/>
              <line x1="342.9" y1="164" x2="278.5" y2="201" stroke="#e6e9e9" stroke-width="12"/>
              <line x1="169.1" y1="264" x2="233.5" y2="227" stroke="#e6e9e9" stroke-width="12"/>
              <line x1="342.9" y1="264" x2="278.5" y2="227" stroke="#e6e9e9" stroke-width="12"/>
              <line x1="256" y1="314" x2="256" y2="240" stroke="#e6e9e9" stroke-width="12"/>
            </g>
            <circle cx="256" cy="92" r="19" fill="none" stroke="#e6e9e9" stroke-width="12"/>
            <circle cx="150" cy="153" r="20" fill="#418b9b"/>
            <circle cx="362" cy="153" r="20" fill="#418b9b"/>
            <circle cx="150" cy="275" r="19" fill="none" stroke="#e6e9e9" stroke-width="12"/>
            <circle cx="362" cy="275" r="20" fill="#418b9b"/>
            <circle cx="256" cy="336" r="20" fill="#418b9b"/>
            <circle cx="256" cy="214" r="24" fill="#d98f5d"/>
          </svg>
          <span class="text-xl font-bold tracking-tight text-[#2dd4bf] font-headline">Smart Proxy by Spio</span>
        </div>

        <!-- Navigation -->
        <nav class="hidden md:flex gap-6 font-label text-sm uppercase tracking-wider">
          {#each navItems as item}
            {@const isActive = item.href === '/' ? $page.url.pathname === '/' : $page.url.pathname.startsWith(item.href)}
            <a
              href={item.href}
              class="transition-colors pb-2 {isActive
                ? 'text-primary border-b-2 border-primary-container'
                : 'text-on-background hover:text-primary'}"
            >
              {item.label}
            </a>
          {/each}
        </nav>
      </div>

      <div class="flex items-center gap-4">
      </div>
    </div>
  </header>

  <!-- Main content -->
  <main class="pt-24 pb-12 px-6 max-w-[1400px] mx-auto w-full flex-1">
    {@render children()}
  </main>
</div>
