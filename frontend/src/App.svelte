<script lang="ts">
  import { onMount } from 'svelte'
  import { SvelteToast } from '@zerodevx/svelte-toast'
  import { Activity, RefreshCw, Search } from '@lucide/svelte'
  import { filteredSessions, loadSessions, sessionFilter } from "./data/sessions.svelte"
  import Session from "./Session.svelte"
  import { loadStats } from "./data/stats.svelte"
  import type { SessionRefreshMode } from "./data/session-state.svelte"

  let selectedSessionId = $state('')
  let sessionVersion = $state(0)
  let sessionRefreshMode = $state<SessionRefreshMode>('manual')
  let autoRefresh = $state(true)

  async function refresh(mode: SessionRefreshMode = 'manual') {
    const nextSessionId = await loadSessions(selectedSessionId)
    sessionRefreshMode = mode
    if (nextSessionId == selectedSessionId) {
      sessionVersion++
    } else {
      selectedSessionId = nextSessionId
    }
    await loadStats()
  }

  function selectSession(session_id: string) {
    selectedSessionId = session_id
  }

  onMount(() => {
    void refresh('manual')
    const interval = window.setInterval(() => {
      if (autoRefresh) void refresh('auto')
    }, 5000)
    return () => {
      window.clearInterval(interval)
    }
  })
</script>

<svelte:head>
  <title>VRCDP Diagnostics</title>
</svelte:head>

<main class="grid min-h-svh grid-cols-[320px_minmax(0,1fr)] bg-gray-100 text-gray-900 max-[1180px]:grid-cols-[280px_minmax(0,1fr)] max-[860px]:block">
  <SvelteToast/>
  <aside aria-label="Diagnostic sessions" class="sticky top-0 grid h-svh grid-rows-[auto_auto_minmax(0,1fr)] border-r border-gray-200 bg-gray-100 p-4.5 max-[860px]:static max-[860px]:h-auto max-[860px]:max-h-[48svh] max-[860px]:border-r-0 max-[860px]:border-b">
    <div class="flex items-start justify-between gap-4">
      <div>
        <p class="eyebrow">VRCDP</p>
        <h1 class="mt-0.5 text-[1.45rem] leading-[1.1] font-bold">Diagnostics</h1>
      </div>
      <div class="flex items-center gap-2">
        <label class="inline-flex min-h-8 items-center gap-1.5 rounded-md border border-gray-200 bg-white px-2 text-xs font-bold text-gray-500" title="Auto refresh">
          <Activity aria-hidden="true" class="size-4"/>
          <input class="size-3.5 accent-blue-600" bind:checked={autoRefresh} aria-label="Auto refresh" type="checkbox"/>
        </label>
        <button aria-label="Refresh diagnostics" class="grid size-8 place-items-center rounded-md border border-gray-200 bg-white text-gray-900 hover:border-gray-300 hover:bg-gray-50" onclick={() => refresh('manual')} title="Refresh diagnostics" type="button">
          <RefreshCw aria-hidden="true" class="size-4"/>
        </button>
      </div>
    </div>

    <label class="mt-4.5 mb-3 grid gap-1.5 text-xs font-bold text-gray-500">
      <span class="relative block">
        <Search aria-hidden="true" class="pointer-events-none absolute left-2 top-1/2 size-4 -translate-y-1/2 text-gray-500"/>
        <input class="w-full rounded-md border border-gray-200 bg-white pl-8! text-sm" bind:value={$sessionFilter} placeholder="label, id, time"/>
      </span>
    </label>

    <div aria-live="polite" class="grid min-h-0 content-start gap-2 overflow-auto pr-1">
      {#each $filteredSessions as session (session.session_id)}
        <button
            class={[
              "grid min-h-36 w-full gap-1.5 rounded-md border bg-white p-3 text-left hover:border-gray-300 hover:bg-gray-50",
              session.session_id === selectedSessionId ? "border-blue-600" : (
                session.chunk_events_dropped > 0 ? "border-blue-300" : "border-gray-200"
              ),
            ]}
            type="button"
            onclick={() => selectSession(session.session_id)}
        >
          <span class="truncate font-bold">{session.session_label || session.session_id}</span>
          <span class="truncate text-xs text-gray-500">{session.start_wall_time || 'start time unavailable'}</span>
          <span class="session-metrics flex flex-wrap gap-1.5">
            <span>{session.request_count} req</span>
            <span>{session.marker_count} markers</span>
            <span>{session.glitch_count} glitches</span>
          </span>
          <span class="session-metrics flex flex-wrap gap-1.5">
            <span>{session.chunk_events_recorded} chunks</span>
            <span>{session.chunk_events_dropped} dropped</span>
          </span>
        </button>
      {:else}
        <div class="empty-state">No sessions found</div>
      {/each}
    </div>
  </aside>
  <Session refreshMode={sessionRefreshMode} sessionId={selectedSessionId} version={sessionVersion}/>
</main>
