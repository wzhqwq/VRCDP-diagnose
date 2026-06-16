import type {
  GlitchSummary,
  MarkerSummary,
  RequestSummary,
  TimelineSummary,
  WindowMetric,
} from './api'

export interface RangeNs {
  from: number
  to: number
}

export interface ResourceLane {
  key: string
  label: string
  index: number
  colorIndex: number
  requests: RequestSummary[]
}

export interface RangeSummary {
  durationMs: number
  requestCount: number
  restartCount: number
  markerCount: number
  glitchCount: number
  maxMbps: number
  maxReadMs: number
  maxFlushMs: number
  maxSleepMs: number
  minAllowance: number | null
  maxAllowance: number | null
  bytesSent: number
  droppedChunks: boolean
}

export const NS_PER_MS = 1_000_000
export const NS_PER_SEC = 1_000_000_000

export function requestStartNs(request: RequestSummary): number {
  return request.start.time?.process_uptime_ns ?? 0
}

export function requestEndNs(request: RequestSummary): number {
  return request.end?.time?.process_uptime_ns || requestStartNs(request)
}

export function requestDurationMs(request: RequestSummary): number {
  const duration = request.end?.duration_ns ?? Math.max(0, requestEndNs(request) - requestStartNs(request))
  return duration / NS_PER_MS
}

export function resourceKey(request: RequestSummary): string {
  return request.start.resource_id || request.start.url_path || request.request_id
}

export function resourceLabel(key: string): string {
  if (key.length <= 42) return key
  return `${key.slice(0, 18)}...${key.slice(-18)}`
}

export function buildResourceLanes(requests: RequestSummary[]): ResourceLane[] {
  const grouped = new Map<string, RequestSummary[]>()
  for (const request of requests) {
    const key = resourceKey(request)
    grouped.set(key, [...(grouped.get(key) ?? []), request])
  }
  return [...grouped.entries()]
    .map(([key, laneRequests]) => ({
      key,
      label: resourceLabel(key),
      firstStart: Math.min(...laneRequests.map(requestStartNs)),
      requests: laneRequests.sort((a, b) => requestStartNs(a) - requestStartNs(b)),
    }))
    .sort((a, b) => a.firstStart - b.firstStart || a.key.localeCompare(b.key))
    .map((lane, index) => ({
      key: lane.key,
      label: lane.label,
      index,
      colorIndex: index % 2,
      requests: lane.requests,
    }))
}

export function timelineDomain(timeline: TimelineSummary | null, fallbackRange?: RangeNs | null): RangeNs {
  if (fallbackRange) return fallbackRange
  if (!timeline) return { from: 0, to: NS_PER_SEC }

  const values: number[] = []
  for (const request of timeline.requests) {
    values.push(requestStartNs(request), requestEndNs(request))
  }
  for (const window of timeline.windows) {
    values.push(window.window_start_ns, window.window_end_ns)
  }
  for (const marker of timeline.markers) {
    const ns = marker.marker.time?.process_uptime_ns
    if (ns) values.push(ns)
  }
  for (const glitch of timeline.glitches) {
    const ns = glitch.glitch.time?.process_uptime_ns
    if (ns) values.push(ns)
  }

  const positive = values.filter((value) => Number.isFinite(value) && value > 0)
  if (positive.length === 0) return { from: 0, to: NS_PER_SEC }

  const from = Math.min(...positive)
  const to = Math.max(...positive)
  if (from === to) return { from: Math.max(0, from - NS_PER_SEC), to: to + NS_PER_SEC }

  const padding = Math.max(NS_PER_MS * 250, (to - from) * 0.04)
  return { from: Math.max(0, from - padding), to: to + padding }
}

export function windowsInRange(windows: WindowMetric[], range: RangeNs): WindowMetric[] {
  return windows.filter((window) => window.window_end_ns >= range.from && window.window_start_ns <= range.to)
}

export function requestsInRange(requests: RequestSummary[], range: RangeNs): RequestSummary[] {
  return requests.filter((request) => requestStartNs(request) <= range.to && requestEndNs(request) >= range.from)
}

export function markersInRange(markers: MarkerSummary[], range: RangeNs): MarkerSummary[] {
  return markers.filter((marker) => {
    const ns = marker.marker.time?.process_uptime_ns ?? 0
    return ns >= range.from && ns <= range.to
  })
}

export function glitchesInRange(glitches: GlitchSummary[], range: RangeNs): GlitchSummary[] {
  return glitches.filter((glitch) => {
    const ns = glitch.glitch.time?.process_uptime_ns ?? 0
    return ns >= range.from && ns <= range.to
  })
}

export function summarizeRange(timeline: TimelineSummary | null, range: RangeNs | null, droppedChunks: boolean): RangeSummary {
  if (!timeline || !range) {
    return {
      durationMs: 0,
      requestCount: 0,
      restartCount: 0,
      markerCount: 0,
      glitchCount: 0,
      maxMbps: 0,
      maxReadMs: 0,
      maxFlushMs: 0,
      maxSleepMs: 0,
      minAllowance: null,
      maxAllowance: null,
      bytesSent: 0,
      droppedChunks,
    }
  }

  const windows = windowsInRange(timeline.windows, range)
  const requests = requestsInRange(timeline.requests, range)
  const allowances = windows.flatMap((window) => [window.min_allowance, window.max_allowance])

  return {
    durationMs: (range.to - range.from) / NS_PER_MS,
    requestCount: requests.length,
    restartCount: requests.filter((request) => {
      const start = requestStartNs(request)
      return start >= range.from && start <= range.to
    }).length,
    markerCount: markersInRange(timeline.markers, range).length,
    glitchCount: glitchesInRange(timeline.glitches, range).length,
    maxMbps: Math.max(0, ...windows.map((window) => window.effective_mbps)),
    maxReadMs: Math.max(0, ...windows.map((window) => window.max_read_duration_ns / NS_PER_MS)),
    maxFlushMs: Math.max(0, ...windows.map((window) => window.max_flush_duration_ns / NS_PER_MS)),
    maxSleepMs: Math.max(0, ...windows.map((window) => window.max_sleep_actual_ns / NS_PER_MS)),
    minAllowance: allowances.length > 0 ? Math.min(...allowances) : null,
    maxAllowance: allowances.length > 0 ? Math.max(...allowances) : null,
    bytesSent: windows.reduce((total, window) => total + window.bytes_sent, 0),
    droppedChunks,
  }
}

export function formatDuration(ns: number): string {
  const seconds = ns / NS_PER_SEC
  if (seconds < 1) return `${(ns / NS_PER_MS).toFixed(0)} ms`
  if (seconds < 60) return `${seconds.toFixed(2)} s`
  const minutes = Math.floor(seconds / 60)
  return `${minutes}m ${(seconds % 60).toFixed(0)}s`
}

export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`
  return `${(bytes / 1024 / 1024 / 1024).toFixed(2)} GB`
}

export function formatProcessTime(ns: number): string {
  return `${(ns / NS_PER_SEC).toFixed(3)}s`
}
