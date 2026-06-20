<script lang="ts">
  import { onMount } from "svelte"
  import { Pause, Play, Upload, Video as VideoIcon } from "@lucide/svelte"

  import { formatProcessTime } from "../utils/format"
  import { getSessionContext } from "../data/session-state.svelte"

  const {
    video: {
      state: videoState,
      recordingSegments,
      recordingStartMarkers,
      playbackCursorNs,

      loadVideo,
      unloadVideo,
    }
  } = getSessionContext()

  const url = $derived(videoState.url)
  const paused = $derived(videoState.paused)
  const segments = $derived(recordingSegments())
  const startMarkers = $derived(recordingStartMarkers())
  const playbackCursor = $derived(playbackCursorNs())

  const boundRecordingStartMarker = $derived(
    startMarkers.find(
      (marker) => marker.marker_id === videoState.startMarkerID
    ) ?? null,
  )

  onMount(() => {
    return () => {
      unloadVideo()
    }
  })

  function handleVideoFileChange(event: Event) {
    const input = event.currentTarget as HTMLInputElement
    const file = input.files?.[0]
    loadVideo(file)
  }
</script>

<article class="panel video-panel">
  <div class="panel-title-row compact-title-row">
    <div>
      <h3 class="inline-flex items-center gap-2">
        <VideoIcon aria-hidden="true" class="size-4"/>
        OBS video
      </h3>
      <p>{playbackCursor === null ? 'No cursor' : formatProcessTime(playbackCursor)}</p>
    </div>
    <span class="status-pill inline-flex items-center gap-1.5">
      {#if paused}
        <Pause aria-hidden="true" class="size-3.5"/>
        Paused
      {:else}
        <Play aria-hidden="true" class="size-3.5"/>
        Playing
      {/if}
    </span>
  </div>

  <label>
    <span class="inline-flex items-center gap-1.5">
      <Upload aria-hidden="true" class="size-3.5"/>
      Video file
    </span>
    <input class="file:mr-3 file:rounded-md file:border-0 file:bg-blue-600 file:px-3 file:py-1.5 file:text-sm file:font-bold file:text-white" accept="video/*" onchange={handleVideoFileChange} type="file"/>
  </label>

  <label>
    <span>Bound start</span>
    <select bind:value={videoState.startMarkerID} disabled={startMarkers.length === 0}>
      <option value="">unbound</option>
      {#each startMarkers as marker (marker.marker_id)}
        <option value={marker.marker_id}>
          {formatProcessTime(marker.marker.time?.process_uptime_ns ?? 0)}
          {marker.marker.note ? ` {marker.marker.note}` : ''}
        </option>
      {/each}
    </select>
  </label>

  {#if url}
    <video
        class="obs-video"
        controls
        src={url}
        bind:currentTime={videoState.currentTime}
        bind:duration={videoState.duration}
        bind:paused={videoState.paused}
    >
      <track kind="captions" src=""/>
    </video>
  {:else}
    <div class="video-empty">No video loaded</div>
  {/if}

  <dl class="video-facts">
    <div>
      <dt>Video</dt>
      <dd>
        {videoState.currentTime.toFixed(3)}s
        /
        {Number.isFinite(videoState.duration) ? videoState.duration.toFixed(3) : 'n/a'}s
      </dd>
    </div>
    <div>
      <dt>Start</dt>
      <dd>{boundRecordingStartMarker ? formatProcessTime(boundRecordingStartMarker.marker.time?.process_uptime_ns ?? 0) : 'unbound'}</dd>
    </div>
    <div>
      <dt>Segments</dt>
      <dd>{segments.length}</dd>
    </div>
  </dl>
</article>
