<script lang="ts">
  import { formatProcessTime } from "../utils/format"
  import TimelineChart from "../TimelineChart.svelte"
  import { summarizeRange } from "../timelineData"
  import { getSessionContext } from "../data/session-state.svelte"
  import { X } from "@lucide/svelte"

  const {
    state: sessionState,
    timeline: {
      state: timelineState,
      setRange,
    }
  } = getSessionContext()

  const session = $derived(sessionState.session)
  const timeline = $derived(timelineState.data)
  const zoomTimeline = $derived(timelineState.zoomTimeline)
  const selectedRange = $derived(timelineState.selectedRange)

  const detailTimeline = $derived(zoomTimeline ?? timeline)
  const rangeSummary = $derived(summarizeRange(detailTimeline, selectedRange, (session?.chunk_events_dropped ?? 0) > 0))
</script>

<article aria-labelledby="zoom-title" class="panel">
  <div class="panel-title-row">
    <div>
      <h3 id="zoom-title">Zoomed range</h3>
      <p>
        {#if selectedRange}
          {formatProcessTime(selectedRange.from)} to {formatProcessTime(selectedRange.to)}
        {:else}
          Select a range on the main timeline
        {/if}
      </p>
    </div>
    <button
        class="inline-flex min-h-8 items-center gap-1.5 rounded-md border border-gray-200 bg-white px-3 text-sm font-bold hover:border-gray-300 hover:bg-gray-50 disabled:cursor-not-allowed disabled:opacity-55"
        type="button"
        onclick={() => setRange(null)}
        disabled={!selectedRange}
    >
      <X aria-hidden="true" class="size-4"/>
      Range
    </button>
  </div>
  {#if selectedRange}
    <TimelineChart height={300} zoom/>
    <dl class="mt-2 grid grid-cols-4 gap-2 border-t border-gray-200 pt-2 max-[1180px]:grid-cols-2 max-[640px]:grid-cols-1"
        aria-label="Summary statistics for the zoomed range">
      <div>
        <dt>Duration</dt>
        <dd>{rangeSummary.durationMs.toFixed(0)} ms</dd>
      </div>
      <div>
        <dt>Requests</dt>
        <dd>{rangeSummary.requestCount} total, {rangeSummary.restartCount} starts</dd>
      </div>
      <div>
        <dt>Events</dt>
        <dd>{rangeSummary.markerCount} markers, {rangeSummary.glitchCount} glitches</dd>
      </div>
      <div>
        <dt>Max Mbps</dt>
        <dd>{rangeSummary.maxMbps.toFixed(1)}</dd>
      </div>
      <div>
        <dt>Read / flush</dt>
        <dd>{rangeSummary.maxReadMs.toFixed(2)} / {rangeSummary.maxFlushMs.toFixed(2)} ms</dd>
      </div>
      <div>
        <dt>Sleep max</dt>
        <dd>{rangeSummary.maxSleepMs.toFixed(2)} ms</dd>
      </div>
      <div>
        <dt>Allowance</dt>
        <dd>
          {rangeSummary.minAllowance ?? 'n/a'} to {rangeSummary.maxAllowance ?? 'n/a'}
        </dd>
      </div>
      <div>
        <dt>Dropped</dt>
        <dd>{rangeSummary.droppedChunks ? 'yes' : 'no'}</dd>
      </div>
    </dl>
  {/if}
</article>
