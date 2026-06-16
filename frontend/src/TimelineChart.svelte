<script lang="ts">
  import * as d3 from 'd3'
  import type { RequestSummary, TimelineSummary, WindowMetric } from './api'
  import {
    buildResourceLanes,
    formatProcessTime,
    requestEndNs,
    requestStartNs,
    timelineDomain,
    type RangeNs,
  } from './timelineData'

  interface Props {
    timeline: TimelineSummary | null
    selectedRange?: RangeNs | null
    zoom?: boolean
    height?: number
    selectedRequestId?: string | null
    playbackCursorNs?: number | null
    onRangeChange?: (range: RangeNs | null) => void
    onSelectRequest?: (request: RequestSummary) => void
  }

  let {
    timeline,
    selectedRange = null,
    zoom = false,
    height = 360,
    selectedRequestId = null,
    playbackCursorNs = null,
    onRangeChange,
    onSelectRequest,
  }: Props = $props()

  let svgElement = $state<SVGSVGElement | undefined>(undefined)
  let width = $state(0)

  const requestColors = ['#1d766f', '#b7652c']
  const requestMuted = ['#d8ece9', '#f3dfce']
  const gridColor = '#dbe2e7'
  const textColor = '#52616b'

  $effect(() => {
    if (!svgElement || !timeline || width < 320) return
    renderChart()
  })

  function chartSvg(node: SVGSVGElement) {
    svgElement = node
    return {
      destroy() {
        if (svgElement === node) svgElement = undefined
      },
    }
  }

  function renderChart() {
    if (!svgElement || !timeline) return

    const lanes = buildResourceLanes(timeline.requests)
    const domain = timelineDomain(timeline, zoom ? selectedRange : null)
    const margin = { top: 18, right: 20, bottom: zoom ? 38 : 58, left: 150 }
    const laneHeight = 28
    const laneGap = 8
    const requestBandHeight = Math.max(96, lanes.length * (laneHeight + laneGap) + 8)
    const metricTop = margin.top + requestBandHeight + 24
    const metricHeight = zoom ? 118 : 92
    const innerHeight = metricTop + metricHeight + margin.bottom
    const renderedHeight = Math.max(height, innerHeight)

    const svg = d3.select(svgElement)
    svg.selectAll('*').remove()
    svg.attr('width', width).attr('height', renderedHeight).attr('viewBox', `0 0 ${width} ${renderedHeight}`)

    const plotWidth = Math.max(1, width - margin.left - margin.right)
    const x = d3.scaleLinear().domain([domain.from, domain.to]).range([margin.left, width - margin.right])
    const axisTicks = Math.max(3, Math.min(8, Math.floor(plotWidth / 150)))
    const xAxis = d3
      .axisBottom(x)
      .ticks(axisTicks)
      .tickFormat((value: d3.NumberValue) => formatProcessTime(Number(value)))

    svg
      .append('g')
      .attr('transform', `translate(0,${renderedHeight - margin.bottom + 12})`)
      .attr('class', 'axis')
      .call(xAxis)

    svg
      .append('line')
      .attr('x1', margin.left)
      .attr('x2', width - margin.right)
      .attr('y1', metricTop - 14)
      .attr('y2', metricTop - 14)
      .attr('stroke', gridColor)

    if (lanes.length === 0) {
      svg
        .append('text')
        .attr('x', margin.left)
        .attr('y', margin.top + 28)
        .attr('fill', textColor)
        .attr('font-size', 13)
        .text('No requests in this range')
    }

    const laneGroups = svg
      .append('g')
      .attr('class', 'request-lanes')
      .selectAll('g')
      .data(lanes)
      .join('g')
      .attr('transform', (lane) => `translate(0,${margin.top + lane.index * (laneHeight + laneGap)})`)

    laneGroups
      .append('text')
      .attr('x', 12)
      .attr('y', 18)
      .attr('fill', textColor)
      .attr('font-size', 12)
      .attr('font-weight', 650)
      .text((lane) => lane.label)

    laneGroups
      .append('line')
      .attr('x1', margin.left)
      .attr('x2', width - margin.right)
      .attr('y1', laneHeight + 4)
      .attr('y2', laneHeight + 4)
      .attr('stroke', '#edf1f3')

    laneGroups
      .selectAll('rect.request')
      .data((lane) => lane.requests.map((request) => ({ request, lane })))
      .join('rect')
      .attr('class', 'request')
      .attr('x', ({ request }) => x(requestStartNs(request)))
      .attr('y', 2)
      .attr('width', ({ request }) => Math.max(3, x(requestEndNs(request)) - x(requestStartNs(request))))
      .attr('height', laneHeight - 6)
      .attr('rx', 4)
      .attr('fill', ({ request, lane }) =>
        request.request_id === selectedRequestId ? requestColors[lane.colorIndex] : requestMuted[lane.colorIndex],
      )
      .attr('stroke', ({ lane }) => requestColors[lane.colorIndex])
      .attr('stroke-width', ({ request }) => (request.request_id === selectedRequestId ? 2 : 1))
      .attr('tabindex', 0)
      .attr('role', 'button')
      .attr('aria-label', ({ request }) => `Select request ${request.request_id}`)
      .on('click', (_event: MouseEvent, { request }) => onSelectRequest?.(request))
      .append('title')
      .text(({ request }) => `${request.request_id}\n${request.start.url_path}\n${request.start.pacing_profile_name || 'profile unknown'}`)

    const markerY1 = margin.top
    const markerY2 = metricTop + metricHeight
    for (const marker of timeline.markers) {
      const ns = marker.marker.time?.process_uptime_ns ?? 0
      if (ns < domain.from || ns > domain.to) continue
      const line = svg.append('g').attr('class', 'marker')
      line
        .append('line')
        .attr('x1', x(ns))
        .attr('x2', x(ns))
        .attr('y1', markerY1)
        .attr('y2', markerY2)
        .attr('stroke', '#c99720')
        .attr('stroke-width', 1.5)
        .attr('stroke-dasharray', '4 4')
      line
        .append('text')
        .attr('x', x(ns) + 5)
        .attr('y', markerY1 + 12)
        .attr('fill', '#8a6310')
        .attr('font-size', 11)
        .text(marker.marker.label)
    }

    for (const glitch of timeline.glitches) {
      const ns = glitch.glitch.time?.process_uptime_ns ?? 0
      if (ns < domain.from || ns > domain.to) continue
      const group = svg.append('g').attr('class', 'glitch')
      group
        .append('line')
        .attr('x1', x(ns))
        .attr('x2', x(ns))
        .attr('y1', markerY1)
        .attr('y2', markerY2)
        .attr('stroke', '#c93f3f')
        .attr('stroke-width', 2)
      group
        .append('path')
        .attr('d', `M ${x(ns)} ${markerY1 - 2} l 7 12 h -14 z`)
        .attr('fill', '#c93f3f')
      group.append('title').text(glitch.glitch.corruption_type || glitch.glitch.severity || 'glitch')
    }

    if (playbackCursorNs !== null && playbackCursorNs >= domain.from && playbackCursorNs <= domain.to) {
      const cursorX = x(playbackCursorNs)
      const group = svg.append('g').attr('class', 'playback-cursor')
      group
        .append('line')
        .attr('x1', cursorX)
        .attr('x2', cursorX)
        .attr('y1', markerY1)
        .attr('y2', markerY2)
        .attr('stroke', '#172027')
        .attr('stroke-width', 2)
      group
        .append('text')
        .attr('x', cursorX + 6)
        .attr('y', markerY2 - 8)
        .attr('fill', '#172027')
        .attr('font-size', 11)
        .attr('font-weight', 760)
        .text('playback')
    }

    drawMetricBand(svg, x, metricTop, metricHeight, width, margin)

    if (!zoom) {
      drawSelection(svg, x, metricTop + metricHeight + 20, margin, width, domain)
    }
  }

  function drawMetricBand(
    svg: d3.Selection<SVGSVGElement, unknown, null, undefined>,
    x: d3.ScaleLinear<number, number>,
    metricTop: number,
    metricHeight: number,
    chartWidth: number,
    margin: { top: number; right: number; bottom: number; left: number },
  ) {
    if (!timeline) return
    const metrics = timeline.windows
    const maxMbps = Math.max(1, d3.max(metrics, (metric) => metric.effective_mbps) ?? 1)
    const y = d3.scaleLinear().domain([0, maxMbps]).range([metricTop + metricHeight, metricTop])

    svg
      .append('text')
      .attr('x', 12)
      .attr('y', metricTop + 16)
      .attr('fill', textColor)
      .attr('font-size', 12)
      .attr('font-weight', 650)
      .text('Mbps')

    svg
      .append('g')
      .attr('transform', `translate(${margin.left},0)`)
      .attr('class', 'axis metric-axis')
      .call(d3.axisLeft(y).ticks(3))

    svg
      .append('g')
      .attr('class', 'metric-grid')
      .selectAll('line')
      .data(y.ticks(3))
      .join('line')
      .attr('x1', margin.left)
      .attr('x2', chartWidth - margin.right)
      .attr('y1', (tick) => y(tick))
      .attr('y2', (tick) => y(tick))
      .attr('stroke', gridColor)
      .attr('stroke-dasharray', '2 5')

    const metricLine = d3
      .line<WindowMetric>()
      .x((metric) => x((metric.window_start_ns + metric.window_end_ns) / 2))
      .y((metric) => y(metric.effective_mbps))

    svg
      .append('path')
      .datum(metrics)
      .attr('fill', 'none')
      .attr('stroke', '#415f9f')
      .attr('stroke-width', 1.8)
      .attr('d', metricLine)

    if (zoom) {
      const sleepMax = Math.max(1, d3.max(metrics, (metric) => metric.max_sleep_actual_ns) ?? 1)
      const sleepY = d3.scaleLinear().domain([0, sleepMax]).range([metricTop + metricHeight, metricTop])
      const sleepLine = d3
        .line<WindowMetric>()
        .x((metric) => x((metric.window_start_ns + metric.window_end_ns) / 2))
        .y((metric) => sleepY(metric.max_sleep_actual_ns))

      svg
        .append('path')
        .datum(metrics)
        .attr('fill', 'none')
        .attr('stroke', '#8f579a')
        .attr('stroke-width', 1.4)
        .attr('stroke-dasharray', '5 4')
        .attr('d', sleepLine)

      svg
        .append('text')
        .attr('x', chartWidth - margin.right - 132)
        .attr('y', metricTop + 16)
        .attr('fill', '#8f579a')
        .attr('font-size', 11)
        .text('sleep actual')
    }
  }

  function drawSelection(
    svg: d3.Selection<SVGSVGElement, unknown, null, undefined>,
    x: d3.ScaleLinear<number, number>,
    brushY: number,
    margin: { top: number; right: number; bottom: number; left: number },
    chartWidth: number,
    domain: RangeNs,
  ) {
    const brushHeight = 34
    let restoringSelection = false
    const brush = d3
      .brushX()
      .extent([
        [margin.left, brushY],
        [chartWidth - margin.right, brushY + brushHeight],
      ])
      .on('end', (event: d3.D3BrushEvent<unknown>) => {
        if (restoringSelection) return
        if (!event.selection) {
          onRangeChange?.(null)
          return
        }
        const [x0, x1] = event.selection as [number, number]
        onRangeChange?.({
          from: Math.max(domain.from, x.invert(x0)),
          to: Math.min(domain.to, x.invert(x1)),
        })
      })

    const brushGroup = svg.append('g').attr('class', 'timeline-brush').call(brush)
    if (selectedRange) {
      restoringSelection = true
      brushGroup.call(brush.move, [x(selectedRange.from), x(selectedRange.to)])
      restoringSelection = false
    }

    svg
      .append('text')
      .attr('x', margin.left)
      .attr('y', brushY - 7)
      .attr('fill', textColor)
      .attr('font-size', 11)
      .text('Select a range for bottom zoom')
  }
</script>

<div class="timeline-host" bind:clientWidth={width}>
  {#if timeline}
    <svg use:chartSvg class="timeline-svg" aria-label={zoom ? 'Zoomed session timeline' : 'Session timeline'}></svg>
  {:else}
    <div class="timeline-empty">No timeline selected</div>
  {/if}
</div>
