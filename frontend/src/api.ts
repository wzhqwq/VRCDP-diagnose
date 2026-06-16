export interface TimePoint {
  wall_unix_nano: number
  wall_rfc3339_nano: string
  process_uptime_ns: number
}

export interface RuntimeStats {
  session_id: string
  enabled: boolean
  requests_started: number
  requests_ended: number
  chunk_events_recorded: number
  chunk_events_dropped: number
  markers_recorded: number
  glitches_recorded: number
  queue_length?: number
}

export interface SessionSummary {
  session_id: string
  session_label?: string
  start_wall_time?: string
  start_unix_nano?: number
  request_count: number
  marker_count: number
  glitch_count: number
  chunk_events_recorded: number
  chunk_events_dropped: number
}

export interface RequestStart {
  time: TimePoint
  request_id?: string
  resource_id?: string
  connection_id?: string
  client_ip: string
  method: string
  host: string
  url_path: string
  raw_query: string
  user_agent: string
  http_proto: string
  tls_enabled: boolean
  tls_version?: string
  tls_cipher_suite?: string
  alpn_protocol?: string
  range_header?: string
  response_status: number
  content_type: string
  content_length: number
  pacing_profile_name: string
  target_mbps: number
  tick: number
  bytes_per_tick: number
  burst_policy: string
}

export interface RequestEnd {
  time: TimePoint
  response_status: number
  total_bytes_sent: number
  duration_ns: number
  error?: string
}

export interface RequestSummary {
  session_id: string
  request_id: string
  start: RequestStart
  end?: RequestEnd
  incomplete: boolean
}

export interface WindowMetric {
  session_id: string
  request_id: string
  window_ms: number
  window_start_ns: number
  window_end_ns: number
  bytes_sent: number
  effective_mbps: number
  write_count: number
  max_read_duration_ns: number
  max_flush_duration_ns: number
  max_sleep_actual_ns: number
  min_allowance: number
  max_allowance: number
}

export interface MarkerEvent {
  time?: TimePoint
  label: string
  note?: string
  source?: string
}

export interface MarkerSummary {
  marker_id: string
  marker: MarkerEvent
}

export interface GlitchEvent {
  time?: TimePoint
  recording_filename?: string
  recording_frame_index?: number
  recording_time_sec?: number
  duration_frames?: number
  duration_ms?: number
  severity?: string
  corruption_type?: string
  notes?: string
  source?: string
}

export interface GlitchSummary {
  glitch_id: string
  glitch: GlitchEvent
}

export interface TimelineSummary {
  session_id: string
  requests: RequestSummary[]
  windows: WindowMetric[]
  markers: MarkerSummary[]
  glitches: GlitchSummary[]
}

export interface TimelineQuery {
  fromNs?: number
  toNs?: number
  windowMs?: number
}

async function fetchJSON<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    headers: { 'Content-Type': 'application/json', ...(init?.headers ?? {}) },
    ...init,
  })
  if (!response.ok) {
    let message = `${response.status} ${response.statusText}`
    try {
      const body = await response.json()
      if (typeof body.error === 'string') message = body.error
    } catch {
      // Keep the status text when the backend does not return JSON.
    }
    throw new Error(message)
  }
  return (await response.json()) as T
}

export function getStats(): Promise<RuntimeStats> {
  return fetchJSON<RuntimeStats>('api/stats')
}

export async function getSessions(): Promise<SessionSummary[]> {
  const body = await fetchJSON<{ sessions: SessionSummary[] }>('api/sessions')
  return body.sessions ?? []
}

export function getTimeline(sessionID: string, query: TimelineQuery = {}): Promise<TimelineSummary> {
  const params = new URLSearchParams()
  if (query.fromNs !== undefined) params.set('from_ns', String(Math.max(0, Math.floor(query.fromNs))))
  if (query.toNs !== undefined) params.set('to_ns', String(Math.max(0, Math.floor(query.toNs))))
  if (query.windowMs !== undefined) params.set('window_ms', String(Math.max(1, Math.floor(query.windowMs))))
  const suffix = params.size > 0 ? `?${params}` : ''
  return fetchJSON<TimelineSummary>(`api/sessions/${encodeURIComponent(sessionID)}/timeline${suffix}`)
}

export async function getRequests(sessionID: string): Promise<RequestSummary[]> {
  const body = await fetchJSON<{ requests: RequestSummary[] }>(`api/sessions/${encodeURIComponent(sessionID)}/requests`)
  return body.requests ?? []
}

export async function createMarker(marker: MarkerEvent): Promise<string> {
  const body = await fetchJSON<{ marker_id: string }>('api/markers', {
    method: 'POST',
    body: JSON.stringify(marker),
  })
  return body.marker_id
}

export async function createGlitch(glitch: GlitchEvent): Promise<string> {
  const body = await fetchJSON<{ glitch_id: string }>('api/glitches', {
    method: 'POST',
    body: JSON.stringify(glitch),
  })
  return body.glitch_id
}
