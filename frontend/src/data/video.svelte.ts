import { buildRecordingSegments, obsRecordingStartMarkers, timelineNsFromVideoTime } from "../timelineData"
import { type TimelineState } from "./timeline.svelte"

export function createVideoState(timelineState: TimelineState) {
  const state = $state({
    url: "",
    currentTime: 0,
    duration: 0,
    paused: false,
    startMarkerID: ""
  })

  const timeline = $derived(timelineState.state.data)

  const recordingSegments = $derived(
    buildRecordingSegments(timeline, state.startMarkerID, state.duration)
  )
  const playbackCursorNs = $derived(timelineNsFromVideoTime(recordingSegments, state.currentTime))

  const recordingStartMarkers = $derived(obsRecordingStartMarkers(timeline))

  function unloadVideo() {
    if (!state.url) return
    URL.revokeObjectURL(state.url)
    state.url = ''
  }

  function loadVideo(file?: File) {
    unloadVideo()
    state.currentTime = 0
    state.duration = 0
    state.paused = true
    if (!file) return
    state.url = URL.createObjectURL(file)
  }

  async function addCursorMarker(label: string) {
    const cursor = playbackCursorNs
    if (cursor === null) return
    await timelineState.addMarker(label, cursor)
  }

  function clear() {
    unloadVideo()
    state.startMarkerID = ""
  }

  return {
    state,
    recordingSegments() {
      return recordingSegments
    },
    playbackCursorNs() {
      return playbackCursorNs
    },
    recordingStartMarkers() {
      return recordingStartMarkers
    },

    unloadVideo,
    loadVideo,
    clear,
    addCursorMarker,
  }
}

export type VideoState = ReturnType<typeof createVideoState>
