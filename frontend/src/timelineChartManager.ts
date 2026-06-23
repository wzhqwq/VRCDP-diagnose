import * as d3 from 'd3'
import type { RequestSummary } from './api'
import type { RangeNs } from './timelineData'
import type { MarkerMark, MetricPoint, MetricSeries, RequestMark, TimelineChartData } from './timelineChartData'
import { formatProcessTime } from './utils/format'

export interface ChartMargin {
  top: number
  right: number
  bottom: number
  left: number
}

export interface ChartLayout {
  margin: ChartMargin
  laneHeight: number
  laneGap: number
  markerLabelSlotCount: number
  metricTop: number
  metricHeight: number
  renderedHeight: number
  axisTicks: number
  markerY1: number
  markerY2: number
}

export interface TimelineChartManagerOptions {
  zoom: boolean
  height: number
  openRequestWidth: number
}

export interface TimelineChartManagerCallbacks {
  setRange(range: RangeNs | null, options?: { loadZoom?: boolean; deferLoad?: boolean }): void
  selectRequest(request: RequestSummary): void
}

export interface TimelineChartStructureUpdate {
  layout: ChartLayout
  width: number
}

type ChartSvg = d3.Selection<SVGSVGElement, unknown, null, undefined>
type Group = d3.Selection<SVGGElement, unknown, null, undefined>
type BrushBehavior = d3.BrushBehavior<unknown>
type DragBehavior = d3.DragBehavior<SVGRectElement, unknown, unknown>

interface MarkerLabelSource {
  marker: MarkerMark
  label: string
  width: number
  markerX: number
  labelGroup?: MarkerLabelGroup
}

interface MarkerLabelGroup {
  id: string
  slotIndex: number
  markers: MarkerLabelSource[]
  label: string
  labelX: number
  labelWidth: number
  startX: number
  endX: number
  title: string
}

const markerLabelFontSize = 11
const markerLabelAverageCharWidth = 6.2
const markerLabelPaddingX = 5
const markerLabelBackgroundHeight = 17
const markerLabelGap = 8
const markerLabelOffsetX = 5
const minMergedMarkerLabelWidth = 58

let timelineChartManagerId = 0

export class TimelineChartManager {
  private readonly id = ++timelineChartManagerId
  private readonly svg: ChartSvg
  private readonly options: TimelineChartManagerOptions
  private readonly callbacks: TimelineChartManagerCallbacks
  private baseLayer: Group | null = null
  private overlayLayer: Group | null = null

  private width = 0
  private layout: ChartLayout | null = null
  private domain: RangeNs | null = null
  private data: TimelineChartData | null = null
  private pendingStructureUpdate: TimelineChartStructureUpdate | null = null
  private x: d3.ScaleLinear<number, number> | null = null
  private selectedRange: RangeNs | null = null
  private navigationDomain: RangeNs | null = null
  private selectedRequestId: string | null = null
  private playbackCursorNs: number | null = null

  private brush: BrushBehavior | null = null
  private drag: DragBehavior | null = null
  private brushGroup: Group | null = null
  private navigationRect: d3.Selection<SVGRectElement, unknown, null, undefined> | null = null
  private programmaticBrushMove = false
  private userBrushActive = false
  private navigationDragActive = false
  private navigationDragStartX: number | null = null
  private navigationDragStartRange: RangeNs | null = null
  private navigationDragStartDomain: RangeNs | null = null
  private wheelInteractionActive = false
  private wheelInteractionTimeout: ReturnType<typeof setTimeout> | null = null

  constructor(
    node: SVGSVGElement,
    options: TimelineChartManagerOptions,
    callbacks: TimelineChartManagerCallbacks,
  ) {
    this.svg = d3.select(node)
    this.options = options
    this.callbacks = callbacks
  }

  updateData(chartData: TimelineChartData) {
    this.data = chartData
    this.drawData()
  }

  updateDomain(domain: RangeNs): void {
    this.domain = domain
    this.updateScale()
    if (!this.layout || !this.x) return

    this.drawTimeAxis(this.layout)
    this.drawData()
    this.updatePlaybackCursor(this.playbackCursorNs)
    this.updateSelectedRange(this.selectedRange)
  }

  updateStructure(update: TimelineChartStructureUpdate) {
    if (this.isUserManipulating()) {
      this.pendingStructureUpdate = update
      return
    }

    this.applyStructure(update)
  }

  private drawData() {
    if (!this.layout || !this.data) return

    this.drawEmptyState(this.layout, this.data)
    this.drawRequestBars(this.layout, this.data)
    this.drawEventMarks(this.layout, this.data)
    this.drawMetricBand(this.layout, this.data)

    this.applySelectedRequest()
  }

  private applyStructure({ layout, width }: TimelineChartStructureUpdate) {
    this.layout = layout
    this.width = width
    this.updateScale()

    this.setupSvg(layout)
    this.drawTimeAxis(layout)
    this.drawMetricDivider(layout)

    this.drawData()

    if (this.options.zoom) {
      this.drawZoomNavigationLayer(layout)
    } else {
      this.drawSelectionLayer(layout)
    }

    this.flushTransientUpdates()
  }

  updateSelectedRequest(selectedRequestId: string | null) {
    this.selectedRequestId = selectedRequestId || null
    if (this.isUserManipulating()) return

    this.applySelectedRequest()
  }

  updatePlaybackCursor(playbackCursorNs: number | null) {
    this.playbackCursorNs = playbackCursorNs
    if (this.isUserManipulating()) return

    this.applyPlaybackCursor()
  }

  updateSelectedRange(selectedRange: RangeNs | null) {
    this.selectedRange = selectedRange
    if (this.isUserManipulating()) return

    this.applySelectedRange()
  }

  updateNavigationDomain(navigationDomain: RangeNs | null) {
    this.navigationDomain = navigationDomain
  }

  isUserManipulating(): boolean {
    return this.userBrushActive || this.navigationDragActive || this.wheelInteractionActive
  }

  destroy() {
    this.clearWheelInteractionTimeout()
    this.brushGroup?.on('.brush', null)
    this.navigationRect?.on('.drag', null).on('wheel', null)
    this.svg.selectAll('*').remove()
    this.baseLayer = null
    this.overlayLayer = null
    this.brushGroup = null
    this.navigationRect = null
    this.brush = null
    this.drag = null
    this.layout = null
    this.domain = null
    this.x = null
    this.pendingStructureUpdate = null
    this.userBrushActive = false
    this.navigationDragActive = false
    this.navigationDragStartX = null
    this.navigationDragStartRange = null
    this.navigationDragStartDomain = null
    this.wheelInteractionActive = false
  }

  private applySelectedRequest() {
    this.svg
      .selectAll<SVGRectElement, RequestMark>('rect.request')
      .classed('selected', (mark) => mark.requestId === this.selectedRequestId)
      .attr('stroke-width', (mark) => mark.requestId === this.selectedRequestId ? 2 : 1)

    this.svg
      .selectAll<SVGPathElement, MetricSeries>('g.metric-lines path')
      .attr('stroke-opacity', (series) => !this.selectedRequestId || series.requestId === this.selectedRequestId ? 0.95 : 0.45)
      .attr('stroke-width', (series) => series.requestId === this.selectedRequestId ? 2.2 : 1.8)

    this.svg
      .selectAll<SVGPathElement, MetricSeries>('g.sleep-lines path')
      .attr('stroke-opacity', (series) => !this.selectedRequestId || series.requestId === this.selectedRequestId ? 0.8 : 0.35)
      .attr('stroke-width', (series) => series.requestId === this.selectedRequestId ? 1.8 : 1.4)
  }

  private applyPlaybackCursor() {
    if (!this.layout || !this.x) return

    const layout = this.layout
    const cursorData = this.playbackCursorNs !== null && this.isInScaleDomain(this.playbackCursorNs) ? [this.playbackCursorNs] : []
    const cursorGroups = this.overlay()
      .selectAll<SVGGElement, number>('g.playback-cursor')
      .data(cursorData, () => 'playback')

    cursorGroups.exit().remove()

    const entered = cursorGroups
      .enter()
      .append('g')
      .attr('class', 'playback-cursor')

    entered
      .append('line')
      .attr('class', 'playback-cursor-line')
      .attr('stroke-width', 2)

    entered
      .append('text')
      .attr('class', 'playback-cursor-label')
      .attr('font-size', 11)
      .attr('font-weight', 760)
      .text('playback')

    const merged = entered.merge(cursorGroups)
    merged
      .select('line')
      .attr('x1', (cursor) => this.x?.(cursor) ?? 0)
      .attr('x2', (cursor) => this.x?.(cursor) ?? 0)
      .attr('y1', layout.markerY1)
      .attr('y2', layout.markerY2)

    merged
      .select('text')
      .attr('x', (cursor) => (this.x?.(cursor) ?? 0) + 6)
      .attr('y', layout.markerY2 - 8)
  }

  private applySelectedRange() {
    if (this.options.zoom || this.userBrushActive || !this.brush || !this.brushGroup || !this.x) return

    const selection = this.selectedRange
      ? [this.x(this.selectedRange.from), this.x(this.selectedRange.to)] satisfies [number, number]
      : null

    this.programmaticBrushMove = true
    this.brushGroup.call(this.brush.move, selection)
    this.programmaticBrushMove = false
  }

  private flushTransientUpdates() {
    if (this.isUserManipulating()) return
    if (this.pendingStructureUpdate) {
      const pendingStructureUpdate = this.pendingStructureUpdate
      this.pendingStructureUpdate = null
      this.applyStructure(pendingStructureUpdate)
      return
    }

    this.applyPlaybackCursor()
    this.applySelectedRange()
  }

  private clearWheelInteractionTimeout() {
    if (this.wheelInteractionTimeout === null) return
    clearTimeout(this.wheelInteractionTimeout)
    this.wheelInteractionTimeout = null
  }

  private updateScale() {
    if (!this.layout || !this.domain) {
      this.x = null
      return
    }
    this.x = d3
      .scaleLinear()
      .domain([this.domain.from, this.domain.to])
      .range([this.layout.margin.left, this.width - this.layout.margin.right])
  }

  private setupSvg(layout: ChartLayout) {
    this.brushGroup?.on('.brush', null)
    this.navigationRect?.on('.drag', null).on('wheel', null)
    this.brushGroup = null
    this.navigationRect = null

    this.svg.selectAll('*').remove()
    this.svg
      .attr('width', this.width)
      .attr('height', layout.renderedHeight)
      .attr('viewBox', `0 0 ${this.width} ${layout.renderedHeight}`)

    this.baseLayer = this.svg.append('g').attr('class', 'chart-base')
    this.overlayLayer = this.svg.append('g').attr('class', 'chart-overlay')
    this.setupDefinitions(layout)
  }

  private base(): Group {
    if (!this.baseLayer) this.baseLayer = this.svg.insert('g', ':first-child').attr('class', 'chart-base')
    return this.baseLayer
  }

  private overlay(): Group {
    if (!this.overlayLayer) this.overlayLayer = this.svg.append('g').attr('class', 'chart-overlay')
    return this.overlayLayer
  }

  private setupDefinitions(layout: ChartLayout) {
    const requestTop = layout.margin.top
    const requestBottom = Math.max(requestTop, layout.metricTop - 14)
    const plotLeft = layout.margin.left
    const plotRight = this.width - layout.margin.right
    const plotWidth = Math.max(1, plotRight - plotLeft)
    const requestHeight = Math.max(1, requestBottom - requestTop)
    const fadeWidth = Math.min(18, plotWidth / 4)

    const defs = this.base().append('defs')
    defs
      .append('clipPath')
      .attr('id', this.requestClipId())
      .append('rect')
      .attr('x', plotLeft)
      .attr('y', requestTop)
      .attr('width', plotWidth)
      .attr('height', requestHeight)

    const mask = defs
      .append('mask')
      .attr('id', this.requestFadeMaskId())
      .attr('maskUnits', 'userSpaceOnUse')
      .attr('x', plotLeft)
      .attr('y', requestTop)
      .attr('width', plotWidth)
      .attr('height', requestHeight)

    mask
      .append('rect')
      .attr('x', plotLeft)
      .attr('y', requestTop)
      .attr('width', plotWidth)
      .attr('height', requestHeight)
      .attr('fill', 'white')

    if (!this.options.zoom || fadeWidth <= 0) return

    const leftGradientId = `${this.requestFadeMaskId()}-left`
    const rightGradientId = `${this.requestFadeMaskId()}-right`

    const leftGradient = defs
      .append('linearGradient')
      .attr('id', leftGradientId)
      .attr('gradientUnits', 'userSpaceOnUse')
      .attr('x1', plotLeft)
      .attr('x2', plotLeft + fadeWidth)
      .attr('y1', 0)
      .attr('y2', 0)
    leftGradient.append('stop').attr('offset', '0%').attr('stop-color', 'black')
    leftGradient.append('stop').attr('offset', '100%').attr('stop-color', 'white')

    const rightGradient = defs
      .append('linearGradient')
      .attr('id', rightGradientId)
      .attr('gradientUnits', 'userSpaceOnUse')
      .attr('x1', plotRight - fadeWidth)
      .attr('x2', plotRight)
      .attr('y1', 0)
      .attr('y2', 0)
    rightGradient.append('stop').attr('offset', '0%').attr('stop-color', 'white')
    rightGradient.append('stop').attr('offset', '100%').attr('stop-color', 'black')

    mask
      .append('rect')
      .attr('x', plotLeft)
      .attr('y', requestTop)
      .attr('width', fadeWidth)
      .attr('height', requestHeight)
      .attr('fill', `url(#${leftGradientId})`)
    mask
      .append('rect')
      .attr('x', plotRight - fadeWidth)
      .attr('y', requestTop)
      .attr('width', fadeWidth)
      .attr('height', requestHeight)
      .attr('fill', `url(#${rightGradientId})`)
  }

  private requestClipId(): string {
    return `timeline-request-clip-${this.id}`
  }

  private requestFadeMaskId(): string {
    return `timeline-request-fade-${this.id}`
  }

  private drawTimeAxis(layout: ChartLayout) {
    if (!this.x) return
    const base = this.base()
    base.selectAll('g.time-axis').remove()
    const xAxis = d3
      .axisBottom(this.x)
      .ticks(layout.axisTicks)
      .tickFormat((value: d3.NumberValue) => formatProcessTime(Number(value)))

    base
      .append('g')
      .attr('transform', `translate(0,${layout.renderedHeight - layout.margin.bottom + 12})`)
      .attr('class', 'axis time-axis')
      .call(xAxis)
  }

  private drawMetricDivider(layout: ChartLayout) {
    this.base()
      .append('line')
      .attr('class', 'chart-grid-line')
      .attr('x1', layout.margin.left)
      .attr('x2', this.width - layout.margin.right)
      .attr('y1', layout.metricTop - 14)
      .attr('y2', layout.metricTop - 14)
  }

  private drawEmptyState(layout: ChartLayout, chartData: TimelineChartData) {
    const base = this.base()
    base.selectAll('text.timeline-empty-state').remove()
    if (chartData.lanes.length !== 0) return
    base
      .append('text')
      .attr('class', 'chart-muted-text timeline-empty-state')
      .attr('x', layout.margin.left)
      .attr('y', layout.margin.top + 28)
      .attr('font-size', 13)
      .text('No requests in this range')
  }

  private drawRequestBars(layout: ChartLayout, chartData: TimelineChartData) {
    if (!this.x) return
    const base = this.base()
    base.selectAll('g.request-lanes').remove()
    base
      .append('g')
      .attr('class', 'request-lanes')
      .attr('clip-path', this.options.zoom ? `url(#${this.requestClipId()})` : null)
      .attr('mask', this.options.zoom ? `url(#${this.requestFadeMaskId()})` : null)
      .selectAll('rect.request')
      .data(chartData.requestMarks)
      .join('rect')
      .attr('class', (mark) => `request request-color-${mark.colorIndex}`)
      .attr('x', (mark) => this.x?.(mark.startNs) ?? 0)
      .attr('y', (mark) => layout.margin.top + mark.laneIndex * (layout.laneHeight + layout.laneGap) + 2)
      .attr('width', (mark) => mark.request.end && this.x ? Math.max(3, this.x(mark.endNs) - this.x(mark.startNs)) : this.options.openRequestWidth)
      .attr('height', layout.laneHeight - 6)
      .attr('rx', 4)
      .attr('stroke-width', 1)
      .attr('stroke-dasharray', (mark) => mark.request.end ? null : '4 3')
      .attr('tabindex', 0)
      .attr('role', 'button')
      .attr('aria-label', (mark) => `Select request ${mark.requestId}`)
      .on('click', (_event: MouseEvent, mark: RequestMark) => this.callbacks.selectRequest(mark.request))
      .append('title')
      .text((mark) => mark.title)
  }

  private drawEventMarks(layout: ChartLayout, chartData: TimelineChartData) {
    if (!this.x) return
    const base = this.base()
    base.selectAll('g.marker,g.marker-labels,g.glitch').remove()
    const visibleMarkers = chartData.markerMarks.filter((marker) => this.isInScaleDomain(marker.ns))
    const markerLabels = this.layoutMarkerLabels(visibleMarkers, layout)
    const markerLabelById = this.markerLabelGroupsByMarkerId(markerLabels)

    for (const marker of visibleMarkers) {
      const labelGroup = markerLabelById[marker.id]
      const line = base.append('g').attr('class', 'marker')
      line
        .append('line')
        .attr('class', 'marker-line')
        .attr('x1', this.x(marker.ns))
        .attr('x2', this.x(marker.ns))
        .attr('y1', labelGroup ? this.markerLabelLineY(labelGroup, layout) : layout.markerY1)
        .attr('y2', layout.markerY2)
        .attr('stroke-width', 1.5)
        .attr('stroke-dasharray', '4 4')
    }

    base
      .append('g')
      .attr('class', 'marker-labels')
      .selectAll<SVGGElement, MarkerLabelGroup>('g.marker-label-group')
      .data(markerLabels, (group) => group.id)
      .join('g')
      .attr('class', 'marker-label-group')
      .each(function (group) {
        const label = d3.select(this)
        const textY = layout.markerY1 + group.slotIndex * (layout.laneHeight + layout.laneGap) + 12
        label
          .append('rect')
          .attr('class', 'marker-label-background')
          .attr('x', group.startX)
          .attr('y', textY - 13)
          .attr('width', group.labelWidth)
          .attr('height', markerLabelBackgroundHeight)
          .attr('rx', 4)
        label
          .append('text')
          .attr('class', 'marker-label')
          .attr('x', group.labelX)
          .attr('y', textY)
          .attr('font-size', markerLabelFontSize)
          .text(group.label)
        label.append('title').text(group.title)
      })

    for (const glitch of chartData.glitchMarks) {
      if (!this.isInScaleDomain(glitch.ns)) continue
      const group = base.append('g').attr('class', 'glitch')
      group
        .append('line')
        .attr('class', 'glitch-line')
        .attr('x1', this.x(glitch.ns))
        .attr('x2', this.x(glitch.ns))
        .attr('y1', layout.markerY1)
        .attr('y2', layout.markerY2)
        .attr('stroke-width', 2)
      group
        .append('path')
        .attr('class', 'glitch-marker')
        .attr('d', `M ${this.x(glitch.ns)} ${layout.markerY1 - 2} l 7 12 h -14 z`)
      group.append('title').text(glitch.title)
    }
  }

  private layoutMarkerLabels(markers: MarkerMark[], layout: ChartLayout): MarkerLabelGroup[] {
    if (!this.x || markers.length === 0) return []

    const slots = Array.from({ length: this.markerLabelSlotCount(layout) }, () => [] as MarkerLabelGroup[])
    const sources = markers
      .map((marker) => ({
        marker,
        label: marker.label,
        width: this.estimateMarkerLabelWidth(marker.label),
        markerX: this.x?.(marker.ns) ?? 0,
      }))
      .sort((a, b) => a.marker.ns - b.marker.ns || a.marker.id.localeCompare(b.marker.id))

    for (const source of sources) {
      const group = this.createMarkerLabelGroup([source], 0)
      const slotIndex = this.firstOpenMarkerLabelSlot(group, slots)

      if (slotIndex !== -1) {
        group.slotIndex = slotIndex
        slots[slotIndex].push(group)
        continue
      }

      const mergeTarget = this.closestMarkerLabelGroup(source, slots)
      if (mergeTarget) {
        this.updateMarkerLabelGroup(mergeTarget, [...mergeTarget.markers, source])
      } else {
        slots[0].push(group)
      }
    }

    return slots.flat()
  }

  private markerLabelSlotCount(layout: ChartLayout): number {
    return Math.max(1, layout.markerLabelSlotCount)
  }

  private firstOpenMarkerLabelSlot(
    group: MarkerLabelGroup,
    slots: MarkerLabelGroup[][],
  ): number {
    return slots.findIndex((slot) =>
      slot.every((existingGroup) => !this.markerLabelGroupsOverlap(group, existingGroup)),
    )
  }

  private markerLabelGroupsOverlap(a: MarkerLabelGroup, b: MarkerLabelGroup): boolean {
    return a.startX < b.endX + markerLabelGap && a.endX + markerLabelGap > b.startX
  }

  private closestMarkerLabelGroup(
    source: MarkerLabelSource,
    slots: MarkerLabelGroup[][],
  ): MarkerLabelGroup | null {
    return slots
      .flat()
      .reduce<MarkerLabelGroup | null>((closest, group) => {
        if (!closest) return group
        return Math.abs(source.markerX - this.markerLabelGroupCenter(group))
          < Math.abs(source.markerX - this.markerLabelGroupCenter(closest))
          ? group
          : closest
      }, null)
  }

  private createMarkerLabelGroup(markers: MarkerLabelSource[], slotIndex: number): MarkerLabelGroup {
    return this.updateMarkerLabelGroup({
      id: '',
      slotIndex,
      markers: [],
      label: '',
      labelX: 0,
      labelWidth: 0,
      startX: 0,
      endX: 0,
      title: '',
    }, markers)
  }

  private updateMarkerLabelGroup(group: MarkerLabelGroup, markers: MarkerLabelSource[]): MarkerLabelGroup {
    const sortedMarkers = [...markers].sort((a, b) => a.marker.ns - b.marker.ns || a.marker.id.localeCompare(b.marker.id))
    const label = sortedMarkers.length === 1 ? sortedMarkers[0].label : `${sortedMarkers.length} markers`
    const textWidth = sortedMarkers.length === 1
      ? sortedMarkers[0].width
      : Math.max(minMergedMarkerLabelWidth, this.estimateMarkerLabelWidth(label))
    const labelWidth = textWidth + markerLabelPaddingX * 2
    const centerX = sortedMarkers.length === 1
      ? sortedMarkers[0].markerX + markerLabelOffsetX + textWidth / 2
      : d3.mean(sortedMarkers, (marker) => marker.markerX) ?? sortedMarkers[0].markerX
    const backgroundX = this.clampMarkerLabelX(centerX - labelWidth / 2, labelWidth)

    group.id = sortedMarkers.map((marker) => marker.marker.id).join(':')
    group.markers = sortedMarkers
    group.label = label
    group.labelX = backgroundX + markerLabelPaddingX
    group.labelWidth = labelWidth
    group.startX = backgroundX
    group.endX = backgroundX + labelWidth
    group.title = sortedMarkers
      .map((marker) => `${formatProcessTime(marker.marker.ns)} ${marker.label}`)
      .join('\n')
    for (const marker of sortedMarkers) {
      marker.labelGroup = group
    }

    return group
  }

  private markerLabelGroupsByMarkerId(groups: MarkerLabelGroup[]): Record<string, MarkerLabelGroup> {
    const index: Record<string, MarkerLabelGroup> = {}
    for (const group of groups) {
      for (const marker of group.markers) {
        index[marker.marker.id] = group
      }
    }
    return index
  }

  private markerLabelLineY(group: MarkerLabelGroup, layout: ChartLayout): number {
    return layout.markerY1 + group.slotIndex * (layout.laneHeight + layout.laneGap) + layout.laneHeight - 4
  }

  private estimateMarkerLabelWidth(label: string): number {
    return Math.max(24, label.length * markerLabelAverageCharWidth)
  }

  private markerLabelGroupCenter(group: MarkerLabelGroup): number {
    return group.startX + (group.endX - group.startX) / 2
  }

  private clampMarkerLabelX(labelX: number, width: number): number {
    const minX = this.layout?.margin.left ?? 0
    const maxX = Math.max(minX, this.width - (this.layout?.margin.right ?? 0) - width)
    return Math.min(maxX, Math.max(minX, labelX))
  }

  private drawMetricBand(layout: ChartLayout, chartData: TimelineChartData) {
    if (!this.x) return
    const base = this.base()
    base.selectAll('text.metric-label,g.metric-axis,g.metric-grid,g.metric-lines,g.sleep-lines,text.sleep-label').remove()
    const y = d3.scaleLinear().domain([0, chartData.maxMbps]).range([layout.metricTop + layout.metricHeight, layout.metricTop])

    base
      .append('text')
      .attr('class', 'chart-muted-text metric-label')
      .attr('x', 12)
      .attr('y', layout.metricTop + 16)
      .attr('font-size', 12)
      .attr('font-weight', 650)
      .text('Mbps')

    base
      .append('g')
      .attr('transform', `translate(${layout.margin.left},0)`)
      .attr('class', 'axis metric-axis')
      .call(d3.axisLeft(y).ticks(3))

    base
      .append('g')
      .attr('class', 'metric-grid')
      .selectAll('line')
      .data(y.ticks(3))
      .join('line')
      .attr('class', 'chart-grid-line')
      .attr('x1', layout.margin.left)
      .attr('x2', this.width - layout.margin.right)
      .attr('y1', (tick) => y(tick))
      .attr('y2', (tick) => y(tick))
      .attr('stroke-dasharray', '2 5')

    const metricLine = d3
      .line<MetricPoint>()
      .x((point) => this.x?.(point.xNs) ?? 0)
      .y((point) => y(point.effectiveMbps))

    base
      .append('g')
      .attr('class', 'metric-lines')
      .selectAll('path')
      .data(chartData.metricSeries)
      .join('path')
      .attr('class', (series) => `metric-line metric-color-${series.colorIndex}`)
      .attr('fill', 'none')
      .attr('stroke-opacity', 0.95)
      .attr('stroke-width', 1.8)
      .attr('d', (series) => metricLine(series.points))
      .append('title')
      .text((series) => series.title)

    if (this.options.zoom) {
      const sleepY = d3.scaleLinear().domain([0, chartData.maxSleepActualNs]).range([layout.metricTop + layout.metricHeight, layout.metricTop])
      const sleepLine = d3
        .line<MetricPoint>()
        .x((point) => this.x?.(point.xNs) ?? 0)
        .y((point) => sleepY(point.maxSleepActualNs))

      base
        .append('g')
        .attr('class', 'sleep-lines')
        .selectAll('path')
        .data(chartData.metricSeries)
        .join('path')
        .attr('class', 'sleep-line')
        .attr('fill', 'none')
        .attr('stroke-opacity', 0.8)
        .attr('stroke-width', 1.4)
        .attr('stroke-dasharray', '5 4')
        .attr('d', (series) => sleepLine(series.points))

      base
        .append('text')
        .attr('class', 'sleep-label')
        .attr('x', this.width - layout.margin.right - 132)
        .attr('y', layout.metricTop + 16)
        .attr('font-size', 11)
        .text('sleep actual')
    }
  }

  private drawSelectionLayer(layout: ChartLayout) {
    const brushHeight = 30
    const brushY = layout.renderedHeight - layout.margin.bottom + 6
    const extent: [[number, number], [number, number]] = [
      [layout.margin.left, brushY],
      [this.width - layout.margin.right, brushY + brushHeight],
    ]

    if (!this.brush) {
      this.brush = d3
        .brushX()
        .on('start brush end', (event: d3.D3BrushEvent<unknown>) => this.handleBrushEvent(event))
    }

    this.brush.extent(extent)
    this.brushGroup = this.overlay().append('g').attr('class', 'timeline-brush')
    this.brushGroup.call(this.brush)
  }

  private drawZoomNavigationLayer(layout: ChartLayout) {
    const brushHeight = 30
    const brushY = layout.renderedHeight - layout.margin.bottom + 6

    this.navigationRect = this.overlay()
      .append('rect')
      .attr('x', layout.margin.left)
      .attr('y', brushY)
      .attr('width', Math.max(1, this.width - layout.margin.left - layout.margin.right))
      .attr('height', brushHeight)
      .attr('class', 'timeline-zoom-navigation')

    if (!this.drag) {
      const manager = this
      this.drag = d3
        .drag<SVGRectElement, unknown>()
        .on('start', function (event: d3.D3DragEvent<SVGRectElement, unknown, unknown>) {
          manager.navigationDragStarted(event)
          d3.select(this).attr("data-state", "active")
        })
        .on('drag', (event: d3.D3DragEvent<SVGRectElement, unknown, unknown>) => this.handleNavigationDrag(event))
        .on('end', function (_event: d3.D3DragEvent<SVGRectElement, unknown, unknown>) {
          manager.navigationDragEnded()
          d3.select(this).attr("data-state", "inactive")
        })
    }

    this.navigationRect
      .call(this.drag)
      .on('wheel', (event: WheelEvent) => this.handleNavigationWheel(event))
  }

  private handleBrushEvent(event: d3.D3BrushEvent<unknown>) {
    if (this.programmaticBrushMove || !event.sourceEvent) return

    this.userBrushActive = event.type !== 'end'
    if (event.type === 'start' || !this.domain || !this.x) return

    if (!event.selection) {
      this.callbacks.setRange(null)
      if (event.type === 'end') this.flushTransientUpdates()
      return
    }

    const [x0, x1] = event.selection as [number, number]
    this.callbacks.setRange({
      from: Math.max(this.domain.from, this.x.invert(x0)),
      to: Math.min(this.domain.to, this.x.invert(x1)),
    }, {
      loadZoom: event.type === 'end',
    })
    if (event.type === 'end') this.flushTransientUpdates()
  }

  private handleNavigationDrag(event: d3.D3DragEvent<SVGRectElement, unknown, unknown>) {
    if (
      this.navigationDragStartX === null
      || !this.navigationDragStartRange
      || !this.navigationDragStartDomain
      || !this.layout
    ) return

    const deltaX = event.x - this.navigationDragStartX
    this.callbacks.setRange(
      this.panRange(this.navigationDragStartRange, deltaX, this.layout, this.navigationDragStartDomain),
      { loadZoom: false },
    )
  }

  private handleNavigationWheel(event: WheelEvent) {
    event.preventDefault()
    this.wheelInteractionStarted()
    if (!this.selectedRange || !this.navigationDomain || !this.x) return
    const [pointerX] = d3.pointer(event, this.svg.node())
    this.callbacks.setRange(
      this.zoomRangeAt(this.selectedRange, this.x.invert(pointerX), event.deltaY, this.navigationDomain),
      { deferLoad: true },
    )
  }

  private navigationDragStarted(event: d3.D3DragEvent<SVGRectElement, unknown, unknown>) {
    this.navigationDragActive = true
    this.navigationDragStartX = event.x
    this.navigationDragStartRange = this.selectedRange
      ? { from: this.selectedRange.from, to: this.selectedRange.to }
      : null
    this.navigationDragStartDomain = this.navigationDomain
      ? { from: this.navigationDomain.from, to: this.navigationDomain.to }
      : null
  }

  private navigationDragEnded() {
    this.navigationDragActive = false
    this.navigationDragStartX = null
    this.navigationDragStartRange = null
    this.navigationDragStartDomain = null
    this.flushTransientUpdates()
  }

  private wheelInteractionStarted() {
    this.wheelInteractionActive = true
    this.clearWheelInteractionTimeout()
    this.wheelInteractionTimeout = setTimeout(() => {
      this.wheelInteractionTimeout = null
      this.wheelInteractionActive = false
      this.flushTransientUpdates()
    }, 120)
  }

  private panRange(range: RangeNs, deltaX: number, layout: ChartLayout, bounds: RangeNs): RangeNs {
    const plotWidth = Math.max(1, this.width - layout.margin.left - layout.margin.right)
    const deltaNs = deltaX * ((range.to - range.from) / plotWidth)
    return this.clampRange({
      from: range.from - deltaNs,
      to: range.to - deltaNs,
    }, bounds)
  }

  private zoomRangeAt(range: RangeNs, anchorNs: number, deltaY: number, bounds: RangeNs): RangeNs {
    const rangeWidth = range.to - range.from
    const boundsWidth = bounds.to - bounds.from
    const minWidth = Math.max(1, boundsWidth / 1000)
    const targetWidth = Math.min(boundsWidth, Math.max(minWidth, rangeWidth * (deltaY > 0 ? 1.16 : 1 / 1.16)))
    const anchorRatio = Math.min(1, Math.max(0, (anchorNs - range.from) / rangeWidth))

    return this.clampRange({
      from: anchorNs - targetWidth * anchorRatio,
      to: anchorNs + targetWidth * (1 - anchorRatio),
    }, bounds)
  }

  private clampRange(range: RangeNs, bounds: RangeNs): RangeNs {
    const boundsWidth = bounds.to - bounds.from
    const rangeWidth = Math.min(boundsWidth, range.to - range.from)
    let from = range.from
    let to = range.to

    if (from < bounds.from) {
      from = bounds.from
      to = from + rangeWidth
    }
    if (to > bounds.to) {
      to = bounds.to
      from = to - rangeWidth
    }

    return {
      from: Math.max(bounds.from, from),
      to: Math.min(bounds.to, to),
    }
  }

  private isInScaleDomain(ns: number): boolean {
    if (!this.x) return false
    const [from, to] = this.x.domain()
    return ns >= from && ns <= to
  }
}
