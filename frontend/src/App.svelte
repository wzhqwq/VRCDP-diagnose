<script lang="ts">
  import { onMount } from 'svelte'
  import {
    createGlitch,
    createMarker,
    getSessions,
    getStats,
    getTimeline,
    type GlitchEvent,
    type RequestSummary,
    type RuntimeStats,
    type SessionSummary,
    type TimelineSummary,
  } from './api'
  import TimelineChart from './TimelineChart.svelte'
  import {
    formatBytes,
    formatDuration,
    formatProcessTime,
    buildRecordingSegments,
    obsRecordingStartMarkers,
    requestDurationMs,
    requestEndNs,
    requestStartNs,
    resourceKey,
    summarizeRange,
    timelineNsFromVideoTime,
    timelineDomain,
    type RangeNs,
  } from './timelineData'

  const markerLabels = ['glitch_seen', 'avatar_switch', 'mirror_opened', 'test_start', 'test_end', 'note']
  const severities = ['', 'low', 'medium', 'high']
  const corruptionTypes = [
    '',
    'block_artifacts',
    'green_or_purple_blocks',
    'full_frame_corruption',
    'partial_texture_corruption',
    'frame_freeze',
    'black_frame',
    'unknown',
  ]

  let sessions = $state<SessionSummary[]>([])
  let stats = $state<RuntimeStats | null>(null)
  let selectedSessionId = $state('')
  let timeline = $state<TimelineSummary | null>(null)
  let zoomTimeline = $state<TimelineSummary | null>(null)
  let selectedRange = $state<RangeNs | null>(null)
  let selectedRequestId = $state<string | null>(null)
  let sessionFilter = $state('')
  let loading = $state(false)
  let timelineLoading = $state(false)
  let actionMessage = $state('')
  let errorMessage = $state('')
  let videoURL = $state('')
  let videoCurrentTime = $state(0)
  let videoDuration = $state(0)
  let videoPaused = $state(true)
  let boundRecordingStartMarkerID = $state('')
  let cursorMarkerLabel = $state('note')

  let glitchSeverity = $state('')
  let glitchType = $state('')
  let recordingFilename = $state('')
  let recordingFrameIndex = $state<number | undefined>(undefined)
  let recordingTimeSec = $state<number | undefined>(undefined)
  let durationFrames = $state<number | undefined>(undefined)
  let durationMs = $state<number | undefined>(undefined)
  let glitchNotes = $state('')

  const filteredSessions = $derived(
    sessions.filter((session) => {
      const query = sessionFilter.trim().toLowerCase()
      if (!query) return true
      return [session.session_id, session.session_label ?? '', session.start_wall_time ?? '']
        .join(' ')
        .toLowerCase()
        .includes(query)
    }),
  )
  const selectedSession = $derived(sessions.find((session) => session.session_id === selectedSessionId) ?? null)
  const requests = $derived(timeline?.requests ?? [])
  const selectedRequest = $derived(requests.find((request) => request.request_id === selectedRequestId) ?? null)
  const detailTimeline = $derived(zoomTimeline ?? timeline)
  const currentDomain = $derived(timelineDomain(timeline, selectedRange))
  const rangeSummary = $derived(summarizeRange(detailTimeline, selectedRange, (selectedSession?.chunk_events_dropped ?? 0) > 0))
  const totalBytesServed = $derived(requests.reduce((total, request) => total + (request.end?.total_bytes_sent ?? 0), 0))
  const maxTimelineMbps = $derived(Math.max(0, ...(timeline?.windows ?? []).map((window) => window.effective_mbps)))
  const recordingStartMarkers = $derived(obsRecordingStartMarkers(timeline))
  const boundRecordingStartMarker = $derived(
    recordingStartMarkers.find((marker) => marker.marker_id === boundRecordingStartMarkerID) ?? null,
  )
  const recordingSegments = $derived(buildRecordingSegments(timeline, boundRecordingStartMarkerID, videoDuration))
  const playbackCursorNs = $derived(timelineNsFromVideoTime(recordingSegments, videoCurrentTime))

  onMount(() => {
    void refreshShell(true)
    const interval = window.setInterval(() => {
      void refreshShell(false)
      if (selectedSessionId) void loadTimeline(selectedSessionId, selectedRange)
    }, 5000)
    return () => {
      window.clearInterval(interval)
      revokeVideoURL()
    }
  })

  async function refreshShell(loadDefaultTimeline: boolean) {
    try {
      loading = true
      const [nextStats, nextSessions] = await Promise.all([getStats(), getSessions()])
      stats = nextStats
      sessions = nextSessions
      const nextSelected = nextSessions.some((session) => session.session_id === selectedSessionId)
        ? selectedSessionId
        : nextSessions[0]?.session_id || ''
      if (nextSelected && nextSelected !== selectedSessionId) {
        selectedSessionId = nextSelected
        selectedRange = null
        zoomTimeline = null
        selectedRequestId = null
        if (loadDefaultTimeline) await loadTimeline(nextSelected, null)
      }
      errorMessage = ''
    } catch (error) {
      errorMessage = error instanceof Error ? error.message : 'Unable to refresh diagnostics data'
    } finally {
      loading = false
    }
  }

  async function loadTimeline(sessionID: string, range: RangeNs | null) {
    try {
      timelineLoading = true
      if (range) {
        zoomTimeline = await getTimeline(sessionID, {
          fromNs: range.from,
          toNs: range.to,
          windowMs: 50,
        })
      } else {
        timeline = await getTimeline(sessionID, { windowMs: 100 })
      }
      errorMessage = ''
    } catch (error) {
      errorMessage = error instanceof Error ? error.message : 'Unable to load session timeline'
    } finally {
      timelineLoading = false
    }
  }

  function selectSession(sessionID: string) {
    if (sessionID === selectedSessionId) return
    selectedSessionId = sessionID
    selectedRange = null
    zoomTimeline = null
    selectedRequestId = null
    boundRecordingStartMarkerID = ''
    void loadTimeline(sessionID, null)
  }

  function handleRangeChange(range: RangeNs | null) {
    selectedRange = range && range.to > range.from ? range : null
    if (selectedSessionId && selectedRange) {
      void loadTimeline(selectedSessionId, selectedRange)
    } else {
      zoomTimeline = null
    }
  }

  async function addMarker(label: string, processUptimeNs?: number) {
    try {
      await createMarker({
        label,
        source: 'frontend',
        time:
          processUptimeNs === undefined
            ? undefined
            : {
                process_uptime_ns: Math.round(processUptimeNs),
                wall_unix_nano: 0,
                wall_rfc3339_nano: '',
              },
      })
      actionMessage = `Marker recorded: ${label}`
      if (selectedSessionId) await loadTimeline(selectedSessionId, selectedRange)
    } catch (error) {
      errorMessage = error instanceof Error ? error.message : 'Unable to create marker'
    }
  }

  async function addCursorMarker() {
    if (playbackCursorNs === null) return
    await addMarker(cursorMarkerLabel || 'note', playbackCursorNs)
  }

  async function submitGlitch(event: SubmitEvent) {
    event.preventDefault()
    const glitch: GlitchEvent = {
      severity: glitchSeverity || undefined,
      corruption_type: glitchType || undefined,
      recording_filename: recordingFilename || undefined,
      recording_frame_index: recordingFrameIndex,
      recording_time_sec: recordingTimeSec,
      duration_frames: durationFrames,
      duration_ms: durationMs,
      notes: glitchNotes || undefined,
      source: 'frontend',
    }
    if (selectedRange) {
      glitch.time = {
        process_uptime_ns: Math.round((selectedRange.from + selectedRange.to) / 2),
        wall_unix_nano: 0,
        wall_rfc3339_nano: '',
      }
    }

    try {
      const id = await createGlitch(glitch)
      actionMessage = `Glitch recorded: ${id}`
      resetGlitchForm()
      if (selectedSessionId) await loadTimeline(selectedSessionId, selectedRange)
    } catch (error) {
      errorMessage = error instanceof Error ? error.message : 'Unable to create glitch'
    }
  }

  function resetGlitchForm() {
    glitchSeverity = ''
    glitchType = ''
    recordingFilename = ''
    recordingFrameIndex = undefined
    recordingTimeSec = undefined
    durationFrames = undefined
    durationMs = undefined
    glitchNotes = ''
  }

  function selectRequest(request: RequestSummary) {
    selectedRequestId = request.request_id
  }

  function refreshAll() {
    void refreshShell(false)
    if (selectedSessionId) void loadTimeline(selectedSessionId, selectedRange)
  }

  function handleVideoFileChange(event: Event) {
    const input = event.currentTarget as HTMLInputElement
    const file = input.files?.[0]
    revokeVideoURL()
    videoCurrentTime = 0
    videoDuration = 0
    videoPaused = true
    if (!file) return
    videoURL = URL.createObjectURL(file)
  }

  function revokeVideoURL() {
    if (!videoURL) return
    URL.revokeObjectURL(videoURL)
    videoURL = ''
  }
</script>

<svelte:head>
  <title>VRCDP Diagnostics</title>
</svelte:head>

<main class="diagnostics-shell">
  <aside class="session-sidebar" aria-label="Diagnostic sessions">
    <div class="sidebar-header">
      <div>
        <p class="eyebrow">VRCDP</p>
        <h1>Diagnostics</h1>
      </div>
      <button class="icon-button" type="button" aria-label="Refresh diagnostics" onclick={refreshAll}>↻</button>
    </div>

    <label class="search-field">
      <span>Search sessions</span>
      <input bind:value={sessionFilter} placeholder="label, id, time" />
    </label>

    <div class="session-list" aria-live="polite">
      {#each filteredSessions as session (session.session_id)}
        <button
          class:selected={session.session_id === selectedSessionId}
          class:has-drops={session.chunk_events_dropped > 0}
          type="button"
          onclick={() => selectSession(session.session_id)}
        >
          <span class="session-title">{session.session_label || session.session_id}</span>
          <span class="session-time">{session.start_wall_time || 'start time unavailable'}</span>
          <span class="session-metrics">
            <span>{session.request_count} req</span>
            <span>{session.marker_count} markers</span>
            <span>{session.glitch_count} glitches</span>
          </span>
          <span class="session-metrics">
            <span>{session.chunk_events_recorded} chunks</span>
            <span>{session.chunk_events_dropped} dropped</span>
          </span>
        </button>
      {:else}
        <div class="empty-state">No sessions found</div>
      {/each}
    </div>
  </aside>

  <section class="session-workspace" aria-label="Current diagnostic session">
    <header class="workspace-top">
      <div>
        <p class="eyebrow">Current session</p>
        <h2>{selectedSession?.session_label || selectedSession?.session_id || 'No session selected'}</h2>
      </div>
      <div class="workspace-actions">
        {#if loading || timelineLoading}
          <span class="status-pill">Refreshing</span>
        {/if}
        <button type="button" onclick={refreshAll}>Refresh</button>
      </div>
    </header>

    {#if errorMessage}
      <div class="notice error">{errorMessage}</div>
    {:else if actionMessage}
      <div class="notice">{actionMessage}</div>
    {/if}

    {#if selectedSession}
      <section class="stats-grid" aria-label="Session and runtime summary">
        <article>
          <span>Runtime</span>
          <strong>{stats?.enabled ? 'enabled' : 'disabled'}</strong>
        </article>
        <article>
          <span>Requests</span>
          <strong>{selectedSession.request_count}</strong>
        </article>
        <article>
          <span>Total served</span>
          <strong>{formatBytes(totalBytesServed)}</strong>
        </article>
        <article>
          <span>Max Mbps</span>
          <strong>{maxTimelineMbps.toFixed(1)}</strong>
        </article>
        <article class:warning={selectedSession.chunk_events_dropped > 0}>
          <span>Dropped chunks</span>
          <strong>{selectedSession.chunk_events_dropped}</strong>
        </article>
        <article>
          <span>Queue</span>
          <strong>{stats?.queue_length ?? 0}</strong>
        </article>
      </section>

      <section class="timeline-panel" aria-labelledby="main-timeline-title">
        <div class="panel-title-row">
          <div>
            <h3 id="main-timeline-title">Session timeline</h3>
            <p>{formatProcessTime(currentDomain.from)} to {formatProcessTime(currentDomain.to)}</p>
          </div>
          <button type="button" onclick={() => handleRangeChange(null)} disabled={!selectedRange}>Clear range</button>
        </div>
        <TimelineChart
          {timeline}
          {selectedRange}
          selectedRequestId={selectedRequestId}
          playbackCursorNs={playbackCursorNs}
          onRangeChange={handleRangeChange}
          onSelectRequest={selectRequest}
        />
      </section>

      <section class="bottom-grid">
        <article class="zoom-panel" aria-labelledby="zoom-title">
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
          </div>
          <TimelineChart
            timeline={detailTimeline}
            selectedRange={selectedRange}
            zoom
            height={300}
            selectedRequestId={selectedRequestId}
            onSelectRequest={selectRequest}
          />
          <dl class="range-facts">
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
        </article>

        <aside class="detail-stack">
          <article class="compact-panel video-panel">
            <div class="panel-title-row compact-title-row">
              <div>
                <h3>OBS video</h3>
                <p>{playbackCursorNs === null ? 'No cursor' : formatProcessTime(playbackCursorNs)}</p>
              </div>
              <span class="status-pill">{videoPaused ? 'Paused' : 'Playing'}</span>
            </div>

            <label>
              <span>Video file</span>
              <input accept="video/*" type="file" onchange={handleVideoFileChange} />
            </label>

            <label>
              <span>Bound start</span>
              <select bind:value={boundRecordingStartMarkerID} disabled={recordingStartMarkers.length === 0}>
                <option value="">unbound</option>
                {#each recordingStartMarkers as marker (marker.marker_id)}
                  <option value={marker.marker_id}>
                    {formatProcessTime(marker.marker.time?.process_uptime_ns ?? 0)}
                    {marker.marker.note ? ` ${marker.marker.note}` : ''}
                  </option>
                {/each}
              </select>
            </label>

            {#if videoURL}
              <video
                class="obs-video"
                controls
                src={videoURL}
                bind:currentTime={videoCurrentTime}
                bind:duration={videoDuration}
                bind:paused={videoPaused}
              >
                <track kind="captions" />
              </video>
            {:else}
              <div class="video-empty">No video loaded</div>
            {/if}

            <dl class="video-facts">
              <div>
                <dt>Video</dt>
                <dd>{videoCurrentTime.toFixed(3)}s / {Number.isFinite(videoDuration) ? videoDuration.toFixed(3) : 'n/a'}s</dd>
              </div>
              <div>
                <dt>Start</dt>
                <dd>{boundRecordingStartMarker ? formatProcessTime(boundRecordingStartMarker.marker.time?.process_uptime_ns ?? 0) : 'unbound'}</dd>
              </div>
              <div>
                <dt>Segments</dt>
                <dd>{recordingSegments.length}</dd>
              </div>
            </dl>

            <div class="cursor-marker-row">
              <label>
                <span>Cursor label</span>
                <input bind:value={cursorMarkerLabel} placeholder="label" />
              </label>
              <button type="button" onclick={addCursorMarker} disabled={playbackCursorNs === null}>Add label</button>
            </div>
          </article>

          <article class="compact-panel">
            <h3>Markers</h3>
            <div class="marker-buttons">
              {#each markerLabels as label (label)}
                <button type="button" onclick={() => addMarker(label)}>{label}</button>
              {/each}
            </div>
          </article>

          <article class="compact-panel">
            <h3>Glitch annotation</h3>
            <form class="glitch-form" onsubmit={submitGlitch}>
              <div class="form-row">
                <label>
                  <span>Severity</span>
                  <select bind:value={glitchSeverity}>
                    {#each severities as severity (severity)}
                      <option value={severity}>{severity || 'unset'}</option>
                    {/each}
                  </select>
                </label>
                <label>
                  <span>Type</span>
                  <select bind:value={glitchType}>
                    {#each corruptionTypes as type (type)}
                      <option value={type}>{type || 'unset'}</option>
                    {/each}
                  </select>
                </label>
              </div>
              <label>
                <span>Recording file</span>
                <input bind:value={recordingFilename} />
              </label>
              <div class="form-row">
                <label>
                  <span>Frame</span>
                  <input type="number" bind:value={recordingFrameIndex} min="0" />
                </label>
                <label>
                  <span>Time sec</span>
                  <input type="number" bind:value={recordingTimeSec} min="0" step="0.001" />
                </label>
              </div>
              <div class="form-row">
                <label>
                  <span>Frames</span>
                  <input type="number" bind:value={durationFrames} min="0" />
                </label>
                <label>
                  <span>Duration ms</span>
                  <input type="number" bind:value={durationMs} min="0" step="0.1" />
                </label>
              </div>
              <label>
                <span>Notes</span>
                <textarea bind:value={glitchNotes} rows="3"></textarea>
              </label>
              <button type="submit">Record glitch</button>
            </form>
          </article>

          <article class="compact-panel">
            <h3>Selected request</h3>
            {#if selectedRequest}
              <dl class="request-detail">
                <div>
                  <dt>ID</dt>
                  <dd>{selectedRequest.request_id}</dd>
                </div>
                <div>
                  <dt>Resource</dt>
                  <dd>{resourceKey(selectedRequest)}</dd>
                </div>
                <div>
                  <dt>Path</dt>
                  <dd>{selectedRequest.start.url_path}</dd>
                </div>
                <div>
                  <dt>Protocol</dt>
                  <dd>{selectedRequest.start.http_proto} {selectedRequest.start.alpn_protocol || ''}</dd>
                </div>
                <div>
                  <dt>TLS</dt>
                  <dd>{selectedRequest.start.tls_enabled ? selectedRequest.start.tls_version || 'enabled' : 'off'}</dd>
                </div>
                <div>
                  <dt>Content</dt>
                  <dd>{formatBytes(selectedRequest.start.content_length)}</dd>
                </div>
                <div>
                  <dt>Profile</dt>
                  <dd>{selectedRequest.start.pacing_profile_name || 'unset'}</dd>
                </div>
                <div>
                  <dt>Duration</dt>
                  <dd>{requestDurationMs(selectedRequest).toFixed(1)} ms</dd>
                </div>
                <div>
                  <dt>Range</dt>
                  <dd>{selectedRequest.start.range_header || 'none'}</dd>
                </div>
                <div>
                  <dt>Status</dt>
                  <dd>{selectedRequest.end?.response_status ?? selectedRequest.start.response_status}</dd>
                </div>
              </dl>
            {:else}
              <div class="empty-state">Select a request bar</div>
            {/if}
          </article>
        </aside>
      </section>

      <section class="request-table-panel" aria-labelledby="request-list-title">
        <h3 id="request-list-title">Requests</h3>
        <div class="request-table">
          <div class="request-row header">
            <span>Request</span>
            <span>Resource</span>
            <span>Profile</span>
            <span>Start</span>
            <span>Duration</span>
            <span>Bytes</span>
          </div>
          {#each requests as request (request.request_id)}
            <button
              class="request-row"
              class:selected={request.request_id === selectedRequestId}
              type="button"
              onclick={() => selectRequest(request)}
            >
              <span>{request.request_id}</span>
              <span>{resourceKey(request)}</span>
              <span>{request.start.pacing_profile_name || 'unset'}</span>
              <span>{formatProcessTime(requestStartNs(request))}</span>
              <span>{formatDuration(Math.max(0, requestEndNs(request) - requestStartNs(request)))}</span>
              <span>{formatBytes(request.end?.total_bytes_sent ?? 0)}</span>
            </button>
          {:else}
            <div class="empty-state">No requests recorded</div>
          {/each}
        </div>
      </section>
    {:else}
      <div class="empty-workspace">No diagnostic session is available yet.</div>
    {/if}
  </section>
</main>
