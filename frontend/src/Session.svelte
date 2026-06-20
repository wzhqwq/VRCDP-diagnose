<script lang="ts">
  import { untrack } from "svelte"
  import { Tabs } from "bits-ui"
  import { Activity, Database, List, RefreshCw, Video as VideoIcon, X } from "@lucide/svelte"
  import {
    createSessionState,
    setSessionContext,
    type SessionRefreshMode,
  } from "./data/session-state.svelte"
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
    refreshMode: SessionRefreshMode
    sessionId: string
    version: number
  }

  const { refreshMode, sessionId, version }: Props = $props()

  let rightTab = $state("requests" as "requests" | "video")

  const sessionState = createSessionState()
  setSessionContext(sessionState)

  $effect(() => {
    const nextSessionId = sessionId
    const nextVersion = version
    const nextRefreshMode = refreshMode
    untrack(() => {
      void sessionState.switchSession(nextSessionId, nextVersion, nextRefreshMode)
    })
  })

  const {
    timeline: { maxMbps, currentDomain, setRange },
    requests: { totalBytesServed },
    loadAll,
  } = sessionState

  const session = $derived(sessionState.state.session)
  const loading = $derived(sessionState.state.loading)
  const selectedRange = $derived(sessionState.timeline.state.selectedRange)
  const servedBytes = $derived(formatBytes(totalBytesServed()))
  const maxMbpsFixed = $derived(maxMbps().toFixed(1))
  const rangeText = $derived(
    `${formatProcessTime(currentDomain().from)} to ${formatProcessTime(currentDomain().to)}`,
  )
</script>

<section aria-label="Current diagnostic session" class="min-w-0 p-[22px] max-[860px]:p-4">
  <header class="mb-3.5 flex min-h-[58px] items-center justify-between gap-4 max-[860px]:grid">
    <div>
      <p class="eyebrow">Current session</p>
      <h2 class="mt-0.5 text-[1.6rem] leading-[1.12] font-bold">
        {session?.session_label || session?.session_id || "No session selected"}
      </h2>
    </div>
    <div class="flex items-center gap-2.5">
      {#if loading}
        <span class="status-pill inline-flex items-center gap-1.5">
          <Activity aria-hidden="true" class="size-3.5 animate-pulse" />
          Refreshing
        </span>
      {/if}
      <button
        class="inline-flex min-h-8 items-center gap-2 rounded-md border border-gray-200 bg-white px-3 text-sm font-bold hover:border-gray-300 hover:bg-gray-50"
        onclick={loadAll}
        type="button"
      >
        <RefreshCw aria-hidden="true" class="size-4" />
        Refresh
      </button>
    </div>
  </header>

  {#if session}
    <section
      class="mb-2 grid grid-cols-1 xl:grid-cols-[2fr_minmax(0,1fr)] 2xl:grid-cols-[3fr_minmax(0,1fr)] gap-2"
    >
      <section class="flex grow flex-col gap-2">
        <section
          class="grid grid-cols-2 gap-2 md:grid-cols-3 2xl:grid-cols-6"
          aria-label="Session and runtime summary"
        >
          <article class="stat-card">
            <span>Runtime</span>
            <strong>{$stats?.enabled ? "enabled" : "disabled"}</strong>
          </article>
          <article class="stat-card">
            <span>Requests</span>
            <strong>{session.request_count}</strong>
          </article>
          <article class="stat-card">
            <span>Total served</span>
            <strong>{servedBytes}</strong>
          </article>
          <article class="stat-card">
            <span>Max Mbps</span>
            <strong>{maxMbpsFixed}</strong>
          </article>
          <article class={["stat-card", session.chunk_events_dropped > 0 && "text-blue-700"]}>
            <span>Dropped chunks</span>
            <strong>{session.chunk_events_dropped}</strong>
          </article>
          <article class="stat-card">
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
            <button
              class="inline-flex min-h-8 items-center gap-1.5 rounded-md border border-gray-200 bg-white px-3 text-sm font-bold hover:border-gray-300 hover:bg-gray-50 disabled:cursor-not-allowed disabled:opacity-55"
              type="button"
              onclick={() => setRange(null)}
              disabled={!selectedRange}
            >
              <X aria-hidden="true" class="size-4" />
              Range
            </button>
          </div>
          <TimelineChart />
          <Markers />
        </section>
      </section>

      <Tabs.Root bind:value={rightTab} class="flex flex-col gap-2 min-w-0" activationMode="manual">
        <Tabs.List class="grid h-10 grid-cols-2 gap-1" aria-label="Data source selection">
          <Tabs.Trigger
            class="inline-flex items-center justify-center gap-2 rounded-md px-3 text-sm font-bold text-gray-500 outline-none data-[state=active]:bg-white data-[state=active]:text-gray-900 data-[state=active]:shadow-sm focus-visible:ring-2 focus-visible:ring-blue-600/25"
            value="requests"
          >
            <List aria-hidden="true" class="size-4" />
            Requests
          </Tabs.Trigger>
          <Tabs.Trigger
            class="inline-flex items-center justify-center gap-2 rounded-md px-3 text-sm font-bold text-gray-500 outline-none data-[state=active]:bg-white data-[state=active]:text-gray-900 data-[state=active]:shadow-sm focus-visible:ring-2 focus-visible:ring-blue-600/25"
            value="video"
          >
            <VideoIcon aria-hidden="true" class="size-4" />
            Video
          </Tabs.Trigger>
        </Tabs.List>
        <Tabs.Content class="min-w-0" value="requests">
          <Requests />
        </Tabs.Content>
        <Tabs.Content class="min-w-0" value="video">
          <Video />
        </Tabs.Content>
      </Tabs.Root>
    </section>

    <section class="grid grid-cols-1 xl:grid-cols-[2fr_minmax(0,1fr)] 2xl:grid-cols-[3fr_minmax(0,1fr)] gap-2">
      <Zoomed />
      <!-- <GlitchForm/> -->
      <RequestDetail />
    </section>
  {:else}
    <div class="empty-workspace">
      <Database aria-hidden="true" class="mb-2 size-7 text-gray-500" />
      <span>No diagnostic session is available yet.</span>
    </div>
  {/if}
</section>
