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

export interface RecordingSegment {
  timelineStartNs: number
  timelineEndNs: number
  videoStartSec: number
  videoEndSec: number
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

  const values = [
      ...(timeline.requests ?? []).flatMap(request => [requestStartNs(request), requestEndNs(request)]),
      ...(timeline.windows ?? []).flatMap(win => [win.window_start_ns, win.window_end_ns]),
      ...(timeline.markers ?? []).map(marker => marker.marker.time?.process_uptime_ns).filter(n => typeof n === 'number'),
      ...(timeline.glitches ?? []).map(glitch => glitch.glitch.time?.process_uptime_ns).filter(n => typeof n === 'number'),
  ]

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

export function markerTimeNs(marker: MarkerSummary): number {
  return marker.marker.time?.process_uptime_ns ?? 0
}

export function obsRecordingStartMarkers(timeline: TimelineSummary | null): MarkerSummary[] {
  return (timeline?.markers ?? [])
    .filter((marker) => marker.marker.label === 'obs_recording_started' && markerTimeNs(marker) > 0)
    .sort((a, b) => markerTimeNs(a) - markerTimeNs(b))
}

export function buildRecordingSegments(
  timeline: TimelineSummary | null,
  startMarkerID: string,
  videoDurationSec: number,
): RecordingSegment[] {
  if (!timeline || !startMarkerID) return []
  const startMarker = timeline.markers?.find((marker) => marker.marker_id === startMarkerID)
  const startNs = startMarker ? markerTimeNs(startMarker) : 0
  if (startNs <= 0) return []

  const events = (timeline.markers ?? [])
    .filter((marker) => {
      const ns = markerTimeNs(marker)
      return ns >= startNs && marker.marker.source === 'obs-websocket'
    })
    .sort((a, b) => markerTimeNs(a) - markerTimeNs(b))

  const segments: RecordingSegment[] = []
  let recording = false
  let segmentStartNs = startNs
  let videoStartSec = 0
  const maxVideoSec = Number.isFinite(videoDurationSec) && videoDurationSec > 0 ? videoDurationSec : Number.POSITIVE_INFINITY

  for (const marker of events) {
    const ns = markerTimeNs(marker)
    switch (marker.marker.label) {
      case 'obs_recording_started':
        if (ns === startNs) {
          recording = true
          segmentStartNs = ns
        } else if (recording && ns > segmentStartNs) {
          const durationSec = Math.min((ns - segmentStartNs) / NS_PER_SEC, maxVideoSec - videoStartSec)
          if (durationSec > 0) {
            segments.push({
              timelineStartNs: segmentStartNs,
              timelineEndNs: segmentStartNs + durationSec * NS_PER_SEC,
              videoStartSec,
              videoEndSec: videoStartSec + durationSec,
            })
            videoStartSec += durationSec
          }
          break
        }
        break
      case 'obs_recording_paused':
      case 'obs_recording_stopped':
        if (recording && ns > segmentStartNs) {
          const durationSec = Math.min((ns - segmentStartNs) / NS_PER_SEC, maxVideoSec - videoStartSec)
          if (durationSec > 0) {
            segments.push({
              timelineStartNs: segmentStartNs,
              timelineEndNs: segmentStartNs + durationSec * NS_PER_SEC,
              videoStartSec,
              videoEndSec: videoStartSec + durationSec,
            })
            videoStartSec += durationSec
          }
        }
        recording = false
        if (marker.marker.label === 'obs_recording_stopped') return segments
        break
      case 'obs_recording_resumed':
        if (!recording) {
          recording = true
          segmentStartNs = ns
        }
        break
    }
    if (videoStartSec >= maxVideoSec) return segments
  }

  if (recording && videoStartSec < maxVideoSec) {
    const domain = timelineDomain(timeline)
    const fallbackEndNs =
      Number.isFinite(maxVideoSec) && maxVideoSec > videoStartSec
        ? segmentStartNs + (maxVideoSec - videoStartSec) * NS_PER_SEC
        : Math.max(segmentStartNs, domain.to)
    if (fallbackEndNs > segmentStartNs) {
      const durationSec = Math.min((fallbackEndNs - segmentStartNs) / NS_PER_SEC, maxVideoSec - videoStartSec)
      if (durationSec > 0) {
        segments.push({
          timelineStartNs: segmentStartNs,
          timelineEndNs: segmentStartNs + durationSec * NS_PER_SEC,
          videoStartSec,
          videoEndSec: videoStartSec + durationSec,
        })
      }
    }
  }

  return segments
}

export function timelineNsFromVideoTime(segments: RecordingSegment[], currentTimeSec: number): number | null {
  if (segments.length === 0 || !Number.isFinite(currentTimeSec)) return null
  for (const segment of segments) {
    if (currentTimeSec >= segment.videoStartSec && currentTimeSec <= segment.videoEndSec) {
      return segment.timelineStartNs + (currentTimeSec - segment.videoStartSec) * NS_PER_SEC
    }
  }
  const first = segments[0]
  const last = segments[segments.length - 1]
  if (currentTimeSec < first.videoStartSec) return first.timelineStartNs
  if (currentTimeSec > last.videoEndSec) return last.timelineEndNs
  return null
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

  const windows = windowsInRange(timeline.windows ?? [], range)
  const requests = requestsInRange(timeline.requests ?? [], range)
  const allowances = windows.flatMap((window) => [window.min_allowance, window.max_allowance])

  return {
    durationMs: (range.to - range.from) / NS_PER_MS,
    requestCount: requests.length,
    restartCount: requests.filter((request) => {
      const start = requestStartNs(request)
      return start >= range.from && start <= range.to
    }).length,
    markerCount: markersInRange(timeline.markers ?? [], range).length,
    glitchCount: glitchesInRange(timeline.glitches ?? [], range).length,
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
