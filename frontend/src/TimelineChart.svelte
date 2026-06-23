<script lang="ts">
  import {
    buildTimelineChartData,
    createTimelineChartAssignments,
    type TimelineChartData,
  } from './timelineChartData'
  import type { TimelineSummary } from './api'
  import {
    glitchesInRange,
    markersInRange,
    type RangeNs,
    requestsInRange,
    timelineDomain,
    windowsInRange,
  } from './timelineData'
  import { getSessionContext } from "./data/session-state.svelte"
  import { TimelineChartManager, type ChartLayout } from './timelineChartManager'

  interface Props {
    zoom?: boolean
    height?: number
  }

  const {
    state: sessionState,
    timeline: {
      state: timelineState,
      hasWindowRange,
      preferredWindowMs,
      setRange,
    },
    video: {
      playbackCursorNs,
    },
    selectRequest
  } = getSessionContext()

  const {
    zoom = false,
    height = 100,
  }: Props = $props()

  const selectedRequestId = $derived(sessionState.selectedRequestId)
  const fullTimeline = $derived(timelineState.data)
  const zoomTimeline = $derived(timelineState.zoomTimeline)
  const selectedRange = $derived(timelineState.selectedRange)

  let chartManager = $state<TimelineChartManager | null>(null)
  let width = $state(0)
  let lastStructureKey = ''

  const requestPaletteSize = 2
  const openRequestWidth = 18
  const chartAssignments = createTimelineChartAssignments()

  const timeline = $derived.by(() => {
    if (!zoom) return fullTimeline
    if (!selectedRange) return fullTimeline

    const preferredWindowMsForRange = preferredWindowMs(selectedRange)
    if (preferredWindowMsForRange === 50 && zoomTimeline && hasWindowRange(selectedRange, 50)) {
      return timelineForRange(zoomTimeline, selectedRange, 50)
    }

    return timelineForRange(fullTimeline, selectedRange, 100)
  })
  const navigationDomain = $derived(fullTimeline ? timelineDomain(fullTimeline, null) : null)
  const chartData = $derived.by(() => timeline
    ? buildTimelineChartData({
      timeline,
      assignments: chartAssignments,
      paletteSize: requestPaletteSize,
    })
    : null)
  const hasChartData = $derived(chartData !== null)
  const laneCount = $derived(chartData?.lanes.length ?? 0)
  const chartDomain = $derived(timeline ? timelineDomain(timeline, zoom ? selectedRange : null) : null)
  const chartLayout = $derived(hasChartData && width >= 320 ? buildChartLayout(laneCount) : null)

  function timelineForRange(
    source: TimelineSummary | null,
    range: RangeNs,
    windowMs: 50 | 100,
  ): TimelineSummary | null {
    if (!source) return null
    return {
      session_id: source.session_id,
      requests: requestsInRange(source.requests ?? [], range),
      windows: windowsInRange(source.windows ?? [], range).filter((window) => window.window_ms === windowMs),
      markers: markersInRange(source.markers ?? [], range),
      glitches: glitchesInRange(source.glitches ?? [], range),
    }
  }

  $effect(() => {
    if (!chartManager || !chartLayout) return
    const structureKey = chartStructureKey(chartLayout, width)
    if (structureKey === lastStructureKey) return
    lastStructureKey = structureKey
    chartManager.updateStructure({
      layout: chartLayout,
      width,
    })
  })

  $effect(() => {
    if (chartData) chartManager?.updateData(chartData)
  })

  $effect(() => {
    if (chartDomain) chartManager?.updateDomain(chartDomain)
  })

  $effect(() => {
    chartManager?.updateSelectedRequest(selectedRequestId)
  })

  $effect(() => {
    chartManager?.updatePlaybackCursor(playbackCursorNs())
  })

  $effect(() => {
    chartManager?.updateSelectedRange(selectedRange)
  })

  $effect(() => {
    chartManager?.updateNavigationDomain(navigationDomain)
  })

  function chartSvg(node: SVGSVGElement) {
    const manager = new TimelineChartManager(
      node,
      {
        zoom,
        height,
        openRequestWidth,
      },
      {
        setRange,
        selectRequest,
      },
    )
    chartManager = manager

    return {
      destroy() {
        manager.destroy()
        if (chartManager === manager) {
          chartManager = null
          lastStructureKey = ''
        }
      },
    }
  }

  function chartStructureKey(layout: ChartLayout, width: number): string {
    return [
      width,
      layout.margin.top,
      layout.margin.right,
      layout.margin.bottom,
      layout.margin.left,
      layout.laneHeight,
      layout.laneGap,
      layout.markerLabelSlotCount,
      layout.metricTop,
      layout.metricHeight,
      layout.renderedHeight,
      layout.axisTicks,
      layout.markerY1,
      layout.markerY2,
    ].join(':')
  }

  function buildChartLayout(laneCount: number): ChartLayout {
    const laneHeight = 20
    const laneGap = 2
    const markerY1 = 18
    const markerLabelSlotCount = zoom ? 3 : 6
    const markerLabelBandHeight = markerLabelSlotCount * (laneHeight + laneGap) + 8
    const margin = { top: markerY1 + markerLabelBandHeight, right: 20, bottom: zoom ? 38 : 58, left: 80 }
    const requestBandHeight = Math.max(30, laneCount * (laneHeight + laneGap) + 8)
    const metricTop = margin.top + requestBandHeight + 14
    const metricHeight = zoom ? 130 : 92
    const innerHeight = metricTop + metricHeight + margin.bottom
    const renderedHeight = Math.max(height, innerHeight)
    const plotWidth = Math.max(1, width - margin.left - margin.right)
    const axisTicks = Math.max(3, Math.min(8, Math.floor(plotWidth / 80)))

    return {
      margin,
      laneHeight,
      laneGap,
      markerLabelSlotCount,
      metricTop,
      metricHeight,
      renderedHeight,
      axisTicks,
      markerY1,
      markerY2: metricTop + metricHeight,
    }
  }
</script>

<div bind:clientWidth={width} class="timeline-host">
  {#if timeline}
    <svg use:chartSvg class="timeline-svg" aria-label={zoom ? 'Zoomed session timeline' : 'Session timeline'}></svg>
  {:else}
    <div class="timeline-empty">No timeline selected</div>
  {/if}
</div>
