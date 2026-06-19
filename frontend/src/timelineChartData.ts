import type { GlitchSummary, MarkerSummary, RequestSummary, TimelineSummary, WindowMetric } from './api'
import { requestEndNs, requestStartNs, resourceKey } from './timelineData'

export interface TimelineChartAssignments {
  requestLaneById: Record<string, number>
  resourceColorByKey: Record<string, number>
  nextResourceColorIndex: number
}

export interface RequestLane {
  index: number
  requests: RequestSummary[]
}

export interface RequestMark {
  request: RequestSummary
  requestId: string
  laneIndex: number
  startNs: number
  endNs: number
  activeFill: string
  mutedFill: string
  stroke: string
  title: string
}

export interface MetricSeries {
  requestId: string
  points: MetricPoint[]
  color: string
  title: string
}

export interface MetricPoint {
  xNs: number
  effectiveMbps: number
  maxSleepActualNs: number
}

export interface MarkerMark {
  id: string
  ns: number
  label: string
}

export interface GlitchMark {
  id: string
  ns: number
  title: string
}

export interface TimelineChartData {
  lanes: RequestLane[]
  requestMarks: RequestMark[]
  metricSeries: MetricSeries[]
  markerMarks: MarkerMark[]
  glitchMarks: GlitchMark[]
  maxMbps: number
  maxSleepActualNs: number
}

interface BuildTimelineChartDataOptions {
  timeline: TimelineSummary
  assignments: TimelineChartAssignments
  activeColors: string[]
  mutedColors: string[]
  fallbackMetricColor: string
}

interface RequestColorsById {
  active: Record<string, string>
  muted: Record<string, string>
}

export function createTimelineChartAssignments(): TimelineChartAssignments {
  return {
    requestLaneById: {},
    resourceColorByKey: {},
    nextResourceColorIndex: 0,
  }
}

export function buildTimelineChartData({
  timeline,
  assignments,
  activeColors,
  mutedColors,
  fallbackMetricColor,
}: BuildTimelineChartDataOptions): TimelineChartData {
  const requests = timeline.requests ?? []
  const metrics = timeline.windows ?? []
  const requestColors = requestColorsByResource(requests, assignments, activeColors, mutedColors)
  const lanes = buildRequestLanes(requests, assignments)
  const requestMarks = buildRequestMarks(lanes, requestColors, activeColors, mutedColors)
  const metricSeries = metricsByRequest(metrics).map((series) => ({
    requestId: series.requestId,
    points: series.metrics.map((metric) => ({
      xNs: metricTimeNs(metric),
      effectiveMbps: metric.effective_mbps,
      maxSleepActualNs: metric.max_sleep_actual_ns,
    })),
    color: requestColors.active[series.requestId] ?? fallbackMetricColor,
    title: `${series.requestId}\n${series.metrics.length} metric windows`,
  }))

  return {
    lanes,
    requestMarks,
    metricSeries,
    markerMarks: buildMarkerMarks(timeline.markers ?? []),
    glitchMarks: buildGlitchMarks(timeline.glitches ?? []),
    maxMbps: Math.max(1, ...metrics.map((metric) => metric.effective_mbps)),
    maxSleepActualNs: Math.max(1, ...metrics.map((metric) => metric.max_sleep_actual_ns)),
  }
}

function buildRequestMarks(
  lanes: RequestLane[],
  requestColors: RequestColorsById,
  activeColors: string[],
  mutedColors: string[],
): RequestMark[] {
  return lanes.flatMap((lane) =>
    lane.requests.map((request) => {
      const requestId = request.request_id
      return {
        request,
        requestId,
        laneIndex: lane.index,
        startNs: requestStartNs(request),
        endNs: requestEndNs(request),
        activeFill: requestColors.active[requestId] ?? activeColors[0],
        mutedFill: requestColors.muted[requestId] ?? mutedColors[0],
        stroke: requestColors.active[requestId] ?? activeColors[0],
        title: `${requestId}\n${request.start.url_path}\n${request.start.pacing_profile_name || 'profile unknown'}`,
      }
    }),
  )
}

function requestColorsByResource(
  requests: RequestSummary[],
  assignments: TimelineChartAssignments,
  activeColors: string[],
  mutedColors: string[],
): RequestColorsById {
  const active: Record<string, string> = {}
  const muted: Record<string, string> = {}

  for (const request of sortedRequests(requests)) {
    const key = resourceKey(request)
    assignments.resourceColorByKey[key] ??= assignments.nextResourceColorIndex++
    const colorIndex = assignments.resourceColorByKey[key] % activeColors.length
    active[request.request_id] = activeColors[colorIndex]
    muted[request.request_id] = mutedColors[colorIndex]
  }

  return { active, muted }
}

function buildRequestLanes(requests: RequestSummary[], assignments: TimelineChartAssignments): RequestLane[] {
  const lanes: RequestLane[] = []

  for (const request of sortedRequests(requests)) {
    const assignedLane = assignments.requestLaneById[request.request_id]
    if (assignedLane === undefined || requestIntersectsLane(request, lanes[assignedLane])) {
      assignments.requestLaneById[request.request_id] = firstOpenLaneIndex(request, lanes)
    }
    const laneIndex = assignments.requestLaneById[request.request_id]
    lanes[laneIndex] ??= { index: laneIndex, requests: [] }
    lanes[laneIndex].requests.push(request)
  }

  return lanes.filter(Boolean)
}

function firstOpenLaneIndex(request: RequestSummary, lanes: RequestLane[]): number {
  const laneIndex = lanes.findIndex((lane) => !requestIntersectsLane(request, lane))
  return laneIndex === -1 ? lanes.length : laneIndex
}

function requestIntersectsLane(request: RequestSummary, lane: RequestLane | undefined): boolean {
  return lane?.requests.some((laneRequest) => requestsIntersect(request, laneRequest)) ?? false
}

function requestsIntersect(a: RequestSummary, b: RequestSummary): boolean {
  return requestStartNs(a) < requestEndNs(b) && requestEndNs(a) > requestStartNs(b)
}

function sortedRequests(requests: RequestSummary[]): RequestSummary[] {
  return [...requests].sort((a, b) => requestStartNs(a) - requestStartNs(b) || a.request_id.localeCompare(b.request_id))
}

function metricTimeNs(metric: WindowMetric): number {
  return (metric.window_start_ns + metric.window_end_ns) / 2
}

function metricsByRequest(metrics: WindowMetric[]): Array<{ requestId: string; metrics: WindowMetric[] }> {
  const grouped: Record<string, WindowMetric[]> = {}
  for (const metric of metrics) {
    grouped[metric.request_id] = [...(grouped[metric.request_id] ?? []), metric]
  }
  return Object.entries(grouped)
    .map(([requestId, requestMetrics]) => ({
      requestId,
      metrics: requestMetrics.sort((a, b) => metricTimeNs(a) - metricTimeNs(b)),
    }))
    .sort((a, b) => metricTimeNs(a.metrics[0]) - metricTimeNs(b.metrics[0]) || a.requestId.localeCompare(b.requestId))
}

function buildMarkerMarks(markers: MarkerSummary[]): MarkerMark[] {
  return markers.map((marker) => ({
    id: marker.marker_id,
    ns: marker.marker.time?.process_uptime_ns ?? 0,
    label: marker.marker.label,
  }))
}

function buildGlitchMarks(glitches: GlitchSummary[]): GlitchMark[] {
  return glitches.map((glitch) => ({
    id: glitch.glitch_id,
    ns: glitch.glitch.time?.process_uptime_ns ?? 0,
    title: glitch.glitch.corruption_type || glitch.glitch.severity || 'glitch',
  }))
}
