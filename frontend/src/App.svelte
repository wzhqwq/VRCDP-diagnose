<script lang="ts">
  import { onMount } from 'svelte'
  import { SvelteToast } from '@zerodevx/svelte-toast'
  import { filteredSessions, loadSessions, sessionFilter } from "./data/sessions.svelte"
  import Session from "./Session.svelte"
  import { loadStats } from "./data/stats.svelte"

  let selectedSessionId = $state('')
  let sessionVersion = $state(0)

  async function refresh() {
    const nextSessionId = await loadSessions(selectedSessionId)
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
    void refresh()
    const interval = window.setInterval(refresh, 5000)
    return () => {
      window.clearInterval(interval)
    }
  })
</script>

<svelte:head>
  <title>VRCDP Diagnostics</title>
</svelte:head>

<main class="diagnostics-shell">
  <SvelteToast/>
  <aside aria-label="Diagnostic sessions" class="session-sidebar">
    <div class="sidebar-header">
      <div>
        <p class="eyebrow">VRCDP</p>
        <h1>Diagnostics</h1>
      </div>
      <button aria-label="Refresh diagnostics" class="icon-button" onclick={refresh} type="button">↻</button>
    </div>

    <label class="search-field">
      <span>Search sessions</span>
      <input bind:value={$sessionFilter} placeholder="label, id, time"/>
    </label>

    <div aria-live="polite" class="session-list">
      {#each $filteredSessions as session (session.session_id)}
        <button
            class:selected={session.session_id === selectedSessionId}
            class:has-drops={session.chunk_events_dropped > 0}
            type="button"
            onclick={() => selectSession(session.session_id)}
        >
          <span class="session-title">{session.session_label || session.session_id}</span>
          <span class="session-time">{session.start_wall_time || 'start time unavailable'}</span>
          <span class="session-metrics">
            <span>{session.request_count} req</span>
            <span>{session.marker_count} markers</span>
            <span>{session.glitch_count} glitches</span>
          </span>
          <span class="session-metrics">
            <span>{session.chunk_events_recorded} chunks</span>
            <span>{session.chunk_events_dropped} dropped</span>
          </span>
        </button>
      {:else}
        <div class="empty-state">No sessions found</div>
      {/each}
    </div>
  </aside>
  <Session sessionId={selectedSessionId} version={sessionVersion}/>
</main>
