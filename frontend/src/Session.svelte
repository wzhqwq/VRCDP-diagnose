<script lang="ts">
  import { createSessionState, setSessionContext } from "./data/session-state.svelte"
  import Video from "./parts/Video.svelte"
  import Markers from "./parts/Markers.svelte"
  import { formatBytes, formatProcessTime } from "./utils/format"
  import RequestDetail from "./parts/RequestDetail.svelte"
  import Requests from "./parts/Requests.svelte"
  import Zoomed from "./parts/Zoomed.svelte"
  import TimelineChart from "./TimelineChart.svelte"
  import { stats } from "./data/stats.svelte"
  import GlitchForm from "./parts/GlitchForm.svelte"

  interface Props {
    sessionId: string;
    version: number;
  }

  const { sessionId, version }: Props = $props()

  let rightTab = $state('requests' as 'requests' | 'video')

  const sessionState = createSessionState()
  setSessionContext(sessionState)

  $effect(() => {
    sessionState.switchSession(sessionId, version)
  })

  const {
    timeline: {
      maxMbps,
      currentDomain,
      setRange
    },
    requests: {
      totalBytesServed
    },
    loadAll
  } = sessionState

  const session = $derived(sessionState.state.session)
  const loading = $derived(sessionState.state.loading)
  const selectedRange = $derived(sessionState.timeline.state.selectedRange)
  const servedBytes = $derived(formatBytes(totalBytesServed()))
  const maxMbpsFixed = $derived(maxMbps().toFixed(1))
  const rangeText = $derived(`${formatProcessTime(currentDomain().from)} to ${formatProcessTime(currentDomain().to)}`)
</script>

<section aria-label="Current diagnostic session" class="session-workspace">
  <header class="workspace-top">
    <div>
      <p class="eyebrow">Current session</p>
      <h2>{session?.session_label || session?.session_id || 'No session selected'}</h2>
    </div>
    <div class="workspace-actions">
      {#if loading}
        <span class="status-pill">Refreshing</span>
      {/if}
      <button onclick={loadAll} type="button">Refresh</button>
    </div>
  </header>

  {#if session}
    <section class="grid grid-cols-[1fr_minmax(0,360px)] gap-2 mb-2">
      <session class="flex flex-col gap-2 min-w-4xl">
        <section class="stats-grid grid grid-cols-6 gap-2" aria-label="Session and runtime summary">
          <article>
            <span>Runtime</span>
            <strong>{$stats?.enabled ? 'enabled' : 'disabled'}</strong>
          </article>
          <article>
            <span>Requests</span>
            <strong>{session.request_count}</strong>
          </article>
          <article>
            <span>Total served</span>
            <strong>{servedBytes}</strong>
          </article>
          <article>
            <span>Max Mbps</span>
            <strong>{maxMbpsFixed}</strong>
          </article>
          <article class:warning={session.chunk_events_dropped > 0}>
            <span>Dropped chunks</span>
            <strong>{session.chunk_events_dropped}</strong>
          </article>
          <article>
            <span>Queue</span>
            <strong>{$stats?.queue_length ?? 0}</strong>
          </article>
        </section>
        <section class="panel" aria-labelledby="main-timeline-title">
          <div class="panel-title-row">
            <div>
              <h3 id="main-timeline-title">Session timeline</h3>
              <p>{rangeText}</p>
            </div>
            <button type="button" onclick={() => setRange(null)} disabled={!selectedRange}>
              Clear range
            </button>
          </div>
          <TimelineChart/>
          <Markers/>
        </section>
      </session>
      <section class="flex flex-col gap-2">
        <section class="flex gap-2 tab-list" aria-label="Data source selection">
          <button class:active={rightTab === 'requests'} onclick={() => rightTab = 'requests'} type="button">
            Requests
          </button>
          <button class:active={rightTab === 'video'} onclick={() => rightTab = 'video'} type="button">
            Video
          </button>
        </section>
        {#if rightTab == "requests"}
          <Requests/>
        {:else if rightTab == "video"}
          <Video/>
        {/if}
      </section>
    </section>

    <section class="grid grid-cols-[1fr_minmax(0,360px)] gap-2">
      <Zoomed/>
      <aside class="detail-stack">
        <!-- <GlitchForm/> -->
        <RequestDetail/>
      </aside>
    </section>

  {:else}
    <div class="empty-workspace">No diagnostic session is available yet.</div>
  {/if}
</section>
