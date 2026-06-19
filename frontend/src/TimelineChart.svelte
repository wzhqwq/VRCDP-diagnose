<script lang="ts">
  import * as d3 from 'd3'
  import {
    buildTimelineChartData,
    createTimelineChartAssignments,
    type MetricSeries,
    type MetricPoint,
    type RequestMark,
    type TimelineChartData,
  } from './timelineChartData'
  import { type RangeNs, timelineDomain, } from './timelineData'
  import { getSessionContext } from "./data/session-state.svelte"
  import { formatProcessTime } from "./utils/format"

  interface Props {
    zoom?: boolean
    height?: number
  }

  const {
    state: sessionState,
    timeline: {
      state: timelineState,
      setRange,
    },
    video: {
      playbackCursorNs,
    },
    selectRequest
  } = getSessionContext()

  const {
    zoom = false,
    height = 360,
  }: Props = $props()

  const selectedRequestId = $derived(sessionState.selectedRequestId)
  const fullTimeline = $derived(timelineState.data)
  const zoomTimeline = $derived(timelineState.zoomTimeline)
  const selectedRange = $derived(timelineState.selectedRange)

  let svgElement = $state<SVGSVGElement | undefined>(undefined)
  let width = $state(0)

  const requestColors = ['#1d766f', '#b7652c']
  const requestMuted = ['#d8ece9', '#f3dfce']
  const fallbackMetricColor = '#415f9f'
  const sleepColor = '#8f579a'
  const gridColor = '#dbe2e7'
  const textColor = '#52616b'
  const chartAssignments = createTimelineChartAssignments()

  const timeline = $derived(zoom ? (zoomTimeline ?? fullTimeline) : fullTimeline)
  const chartData = $derived.by(() => timeline
    ? buildTimelineChartData({
      timeline,
      assignments: chartAssignments,
      activeColors: requestColors,
      mutedColors: requestMuted,
      fallbackMetricColor,
    })
    : null)
  const chartDomain = $derived(timeline ? timelineDomain(timeline, zoom ? selectedRange : null) : null)
  const chartLayout = $derived(chartData && chartDomain && width >= 320 ? buildChartLayout(chartData, chartDomain) : null)

  type ChartSvg = d3.Selection<SVGSVGElement, unknown, null, undefined>

  interface ChartMargin {
    top: number
    right: number
    bottom: number
    left: number
  }

  interface ChartLayout {
    margin: ChartMargin
    laneHeight: number
    laneGap: number
    metricTop: number
    metricHeight: number
    renderedHeight: number
    x: d3.ScaleLinear<number, number>
    axisTicks: number
    markerY1: number
    markerY2: number
  }

  $effect(() => {
    if (!svgElement || !chartData || !chartLayout || !chartDomain) return
    renderChart(chartData, chartLayout, chartDomain)
  })

  $effect(() => {
    if (!svgElement || !chartData) return
    applySelectionStyles(d3.select(svgElement), selectedRequestId)
  })

  $effect(() => {
    if (!svgElement || !chartLayout) return
    syncPlaybackCursor(d3.select(svgElement), chartLayout, playbackCursorNs())
  })

  $effect(() => {
    if (!svgElement || !chartLayout || zoom) return
    syncSelectionBrush(d3.select(svgElement), chartLayout)
  })

  function chartSvg(node: SVGSVGElement) {
    svgElement = node
    return {
      destroy() {
        if (svgElement === node) svgElement = undefined
      },
    }
  }

  function renderChart(chartData: TimelineChartData, layout: ChartLayout, domain: RangeNs) {
    if (!svgElement) return
    const svg = d3.select(svgElement)

    setupSvg(svg, layout)
    drawTimeAxis(svg, layout)
    drawMetricDivider(svg, layout)
    drawEmptyState(svg, layout, chartData)
    drawRequestBars(svg, layout, chartData)
    drawEventMarks(svg, layout, chartData)
    drawMetricBand(svg, layout, chartData)

    if (!zoom) {
      drawSelectionLayer(svg, layout, domain)
    }
  }

  function buildChartLayout(chartData: TimelineChartData, domain: RangeNs): ChartLayout {
    const margin = { top: 18, right: 20, bottom: zoom ? 38 : 58, left: 80 }
    const laneHeight = 20
    const laneGap = 2
    const requestBandHeight = Math.max(96, chartData.lanes.length * (laneHeight + laneGap) + 8)
    const metricTop = margin.top + requestBandHeight + 24
    const metricHeight = zoom ? 118 : 92
    const innerHeight = metricTop + metricHeight + margin.bottom
    const renderedHeight = Math.max(height, innerHeight)
    const plotWidth = Math.max(1, width - margin.left - margin.right)
    const x = d3.scaleLinear().domain([domain.from, domain.to]).range([margin.left, width - margin.right])
    const axisTicks = Math.max(3, Math.min(8, Math.floor(plotWidth / 80)))

    return {
      margin,
      laneHeight,
      laneGap,
      metricTop,
      metricHeight,
      renderedHeight,
      x,
      axisTicks,
      markerY1: margin.top,
      markerY2: metricTop + metricHeight,
    }
  }

  function setupSvg(svg: ChartSvg, layout: ChartLayout) {
    svg.selectAll('*').remove()
    svg.attr('width', width).attr('height', layout.renderedHeight).attr('viewBox', `0 0 ${width} ${layout.renderedHeight}`)
  }

  function drawTimeAxis(svg: ChartSvg, layout: ChartLayout) {
    const xAxis = d3
      .axisBottom(layout.x)
      .ticks(layout.axisTicks)
      .tickFormat((value: d3.NumberValue) => formatProcessTime(Number(value)))

    svg
      .append('g')
      .attr('transform', `translate(0,${layout.renderedHeight - layout.margin.bottom + 12})`)
      .attr('class', 'axis')
      .call(xAxis)
  }

  function drawMetricDivider(svg: ChartSvg, layout: ChartLayout) {
    svg
      .append('line')
      .attr('x1', layout.margin.left)
      .attr('x2', width - layout.margin.right)
      .attr('y1', layout.metricTop - 14)
      .attr('y2', layout.metricTop - 14)
      .attr('stroke', gridColor)
  }

  function drawEmptyState(svg: ChartSvg, layout: ChartLayout, chartData: TimelineChartData) {
    if (chartData.lanes.length !== 0) return
    svg
      .append('text')
      .attr('x', layout.margin.left)
      .attr('y', layout.margin.top + 28)
      .attr('fill', textColor)
      .attr('font-size', 13)
      .text('No requests in this range')
  }

  function drawRequestBars(svg: ChartSvg, layout: ChartLayout, chartData: TimelineChartData) {
    svg
      .append('g')
      .attr('class', 'request-lanes')
      .selectAll('rect.request')
      .data(chartData.requestMarks)
      .join('rect')
      .attr('class', 'request')
      .attr('x', (mark) => layout.x(mark.startNs))
      .attr('y', (mark) => layout.margin.top + mark.laneIndex * (layout.laneHeight + layout.laneGap) + 2)
      .attr('width', (mark) => Math.max(3, layout.x(mark.endNs) - layout.x(mark.startNs)))
      .attr('height', layout.laneHeight - 6)
      .attr('rx', 4)
      .attr('fill', (mark) => mark.mutedFill)
      .attr('stroke', (mark) => mark.stroke)
      .attr('stroke-width', 1)
      .attr('tabindex', 0)
      .attr('role', 'button')
      .attr('aria-label', (mark) => `Select request ${mark.requestId}`)
      .on('click', (_event: MouseEvent, mark: RequestMark) => selectRequest(mark.request))
      .append('title')
      .text((mark) => mark.title)
  }

  function drawEventMarks(svg: ChartSvg, layout: ChartLayout, chartData: TimelineChartData) {
    for (const marker of chartData.markerMarks) {
      if (!isInScaleDomain(marker.ns, layout.x)) continue
      const line = svg.append('g').attr('class', 'marker')
      line
        .append('line')
        .attr('x1', layout.x(marker.ns))
        .attr('x2', layout.x(marker.ns))
        .attr('y1', layout.markerY1)
        .attr('y2', layout.markerY2)
        .attr('stroke', '#c99720')
        .attr('stroke-width', 1.5)
        .attr('stroke-dasharray', '4 4')
      line
        .append('text')
        .attr('x', layout.x(marker.ns) + 5)
        .attr('y', layout.markerY1 + 12)
        .attr('fill', '#8a6310')
        .attr('font-size', 11)
        .text(marker.label)
    }

    for (const glitch of chartData.glitchMarks) {
      if (!isInScaleDomain(glitch.ns, layout.x)) continue
      const group = svg.append('g').attr('class', 'glitch')
      group
        .append('line')
        .attr('x1', layout.x(glitch.ns))
        .attr('x2', layout.x(glitch.ns))
        .attr('y1', layout.markerY1)
        .attr('y2', layout.markerY2)
        .attr('stroke', '#c93f3f')
        .attr('stroke-width', 2)
      group
        .append('path')
        .attr('d', `M ${layout.x(glitch.ns)} ${layout.markerY1 - 2} l 7 12 h -14 z`)
        .attr('fill', '#c93f3f')
      group.append('title').text(glitch.title)
    }
  }

  function applySelectionStyles(svg: ChartSvg, selectedId: string | null) {
    svg
      .selectAll<SVGRectElement, RequestMark>('rect.request')
      .attr('fill', (mark) => mark.requestId === selectedId ? mark.activeFill : mark.mutedFill)
      .attr('stroke-width', (mark) => mark.requestId === selectedId ? 2 : 1)

    svg
      .selectAll<SVGPathElement, MetricSeries>('g.metric-lines path')
      .attr('stroke-opacity', (series) => !selectedId || series.requestId === selectedId ? 0.95 : 0.45)
      .attr('stroke-width', (series) => series.requestId === selectedId ? 2.2 : 1.8)

    svg
      .selectAll<SVGPathElement, MetricSeries>('g.sleep-lines path')
      .attr('stroke-opacity', (series) => !selectedId || series.requestId === selectedId ? 0.8 : 0.35)
      .attr('stroke-width', (series) => series.requestId === selectedId ? 1.8 : 1.4)
  }

  function syncPlaybackCursor(svg: ChartSvg, layout: ChartLayout, playbackCursor: number | null) {
    const cursorData = playbackCursor !== null && isInScaleDomain(playbackCursor, layout.x) ? [playbackCursor] : []
    const cursorGroups = svg
      .selectAll<SVGGElement, number>('g.playback-cursor')
      .data(cursorData, () => 'playback')

    cursorGroups.exit().remove()

    const entered = cursorGroups
      .enter()
      .append('g')
      .attr('class', 'playback-cursor')

    entered
      .append('line')
      .attr('stroke', '#172027')
      .attr('stroke-width', 2)

    entered
      .append('text')
      .attr('fill', '#172027')
      .attr('font-size', 11)
      .attr('font-weight', 760)
      .text('playback')

    const merged = entered.merge(cursorGroups)
    merged
      .select('line')
      .attr('x1', (cursor) => layout.x(cursor))
      .attr('x2', (cursor) => layout.x(cursor))
      .attr('y1', layout.markerY1)
      .attr('y2', layout.markerY2)

    merged
      .select('text')
      .attr('x', (cursor) => layout.x(cursor) + 6)
      .attr('y', layout.markerY2 - 8)
  }

  function drawMetricBand(svg: ChartSvg, layout: ChartLayout, chartData: TimelineChartData) {
    const y = d3.scaleLinear().domain([0, chartData.maxMbps]).range([layout.metricTop + layout.metricHeight, layout.metricTop])

    svg
      .append('text')
      .attr('x', 12)
      .attr('y', layout.metricTop + 16)
      .attr('fill', textColor)
      .attr('font-size', 12)
      .attr('font-weight', 650)
      .text('Mbps')

    svg
      .append('g')
      .attr('transform', `translate(${layout.margin.left},0)`)
      .attr('class', 'axis metric-axis')
      .call(d3.axisLeft(y).ticks(3))

    svg
      .append('g')
      .attr('class', 'metric-grid')
      .selectAll('line')
      .data(y.ticks(3))
      .join('line')
      .attr('x1', layout.margin.left)
      .attr('x2', width - layout.margin.right)
      .attr('y1', (tick) => y(tick))
      .attr('y2', (tick) => y(tick))
      .attr('stroke', gridColor)
      .attr('stroke-dasharray', '2 5')

    const metricLine = d3
      .line<MetricPoint>()
      .x((point) => layout.x(point.xNs))
      .y((point) => y(point.effectiveMbps))

    svg
      .append('g')
      .attr('class', 'metric-lines')
      .selectAll('path')
      .data(chartData.metricSeries)
      .join('path')
      .attr('fill', 'none')
      .attr('stroke', (series) => series.color)
      .attr('stroke-opacity', 0.95)
      .attr('stroke-width', 1.8)
      .attr('d', (series) => metricLine(series.points))
      .append('title')
      .text((series) => series.title)

    if (zoom) {
      const sleepY = d3.scaleLinear().domain([0, chartData.maxSleepActualNs]).range([layout.metricTop + layout.metricHeight, layout.metricTop])
      const sleepLine = d3
        .line<MetricPoint>()
        .x((point) => layout.x(point.xNs))
        .y((point) => sleepY(point.maxSleepActualNs))

      svg
        .append('g')
        .attr('class', 'sleep-lines')
        .selectAll('path')
        .data(chartData.metricSeries)
        .join('path')
        .attr('fill', 'none')
        .attr('stroke', sleepColor)
        .attr('stroke-opacity', 0.8)
        .attr('stroke-width', 1.4)
        .attr('stroke-dasharray', '5 4')
        .attr('d', (series) => sleepLine(series.points))

      svg
        .append('text')
        .attr('x', width - layout.margin.right - 132)
        .attr('y', layout.metricTop + 16)
        .attr('fill', sleepColor)
        .attr('font-size', 11)
        .text('sleep actual')
    }
  }

  function isInScaleDomain(ns: number, x: d3.ScaleLinear<number, number>): boolean {
    const [from, to] = x.domain()
    return ns >= from && ns <= to
  }

  function drawSelectionLayer(
    svg: ChartSvg,
    layout: ChartLayout,
    domain: RangeNs,
  ) {
    const brushHeight = 34
    const brushY = layout.metricTop + layout.metricHeight + 20
    const brush = d3
      .brushX()
      .extent([
        [layout.margin.left, brushY],
        [width - layout.margin.right, brushY + brushHeight],
      ])
      .on('end', (event: d3.D3BrushEvent<unknown>) => {
        if (!event.selection) {
          setRange(null)
          return
        }
        const [x0, x1] = event.selection as [number, number]
        setRange({
          from: Math.max(domain.from, layout.x.invert(x0)),
          to: Math.min(domain.to, layout.x.invert(x1)),
        })
      })

    svg.append('g').attr('class', 'timeline-brush').call(brush)

    svg
      .append('text')
      .attr('x', layout.margin.left)
      .attr('y', brushY - 7)
      .attr('fill', textColor)
      .attr('font-size', 11)
      .text('Select a range for bottom zoom')
  }

  function syncSelectionBrush(svg: ChartSvg, layout: ChartLayout) {
    const brushGroup = svg.select<SVGGElement>('g.timeline-brush')
    if (brushGroup.empty()) return

    const brushY = layout.metricTop + layout.metricHeight + 20
    const brush = d3.brushX().extent([
      [layout.margin.left, brushY],
      [width - layout.margin.right, brushY + 34],
    ])
    brushGroup.call(brush.move, selectedRange ? [layout.x(selectedRange.from), layout.x(selectedRange.to)] : null)
  }
</script>

<div bind:clientWidth={width} class="timeline-host">
  {#if timeline}
    <svg use:chartSvg class="timeline-svg" aria-label={zoom ? 'Zoomed session timeline' : 'Session timeline'}></svg>
  {:else}
    <div class="timeline-empty">No timeline selected</div>
  {/if}
</div>
