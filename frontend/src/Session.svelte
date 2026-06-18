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
    <section class="stats-grid" aria-label="Session and runtime summary">
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
        <strong>{formatBytes(totalBytesServed)}</strong>
      </article>
      <article>
        <span>Max Mbps</span>
        <strong>{maxMbps.toFixed(1)}</strong>
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

    <section class="timeline-panel" aria-labelledby="main-timeline-title">
      <div class="panel-title-row">
        <div>
          <h3 id="main-timeline-title">Session timeline</h3>
          <p>{formatProcessTime(currentDomain.from)} to {formatProcessTime(currentDomain.to)}</p>
        </div>
        <button type="button" onclick={() => setRange(null)} disabled={!selectedRange}>Clear range
        </button>
      </div>
      <TimelineChart/>
    </section>

    <section class="bottom-grid">
      <Zoomed/>
      <aside class="detail-stack">
        <Video/>
        <Markers/>
        <GlitchForm/>
        <RequestDetail/>
      </aside>
    </section>

    <Requests/>
  {:else}
    <div class="empty-workspace">No diagnostic session is available yet.</div>
  {/if}
</section>
