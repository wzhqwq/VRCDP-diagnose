import { createMarker, getTimeline, type TimelineSummary } from "../api"
import { failure, success } from "../themedToast"
import { type RangeNs, timelineDomain } from "../timelineData"

export function createTimelineState(sessionId: () => string) {
  const state = $state({
    data: null as TimelineSummary | null,
    loading: false,
    error: null as unknown,

    zoomTimeline: null as TimelineSummary | null,
    selectedRange: null as RangeNs | null,
  })

  const maxMbps = $derived(Math.max(0, ...(state.data?.windows ?? []).map((window) => window.effective_mbps)))
  const currentDomain = $derived(timelineDomain(state.data, state.selectedRange))
  let rangeLoadTimeout: ReturnType<typeof setTimeout> | null = null

  function clearRangeLoadTimeout() {
    if (rangeLoadTimeout === null) return
    clearTimeout(rangeLoadTimeout)
    rangeLoadTimeout = null
  }

  function queueRangeLoad(range: RangeNs) {
    clearRangeLoadTimeout()
    rangeLoadTimeout = setTimeout(() => {
      rangeLoadTimeout = null
      void load(range)
    }, 180)
  }

  async function load(range?: RangeNs | null) {
    const sId = sessionId()
    if (!sId) {
      return
    }
    try {
      state.loading = true
      if (range) {
        state.zoomTimeline = await getTimeline(sId, {
          fromNs: range.from,
          toNs: range.to,
          windowMs: 50,
        })
      } else {
        state.data = await getTimeline(sId, { windowMs: 100 })
      }
    } catch (error) {
      state.error = error
      failure(error instanceof Error ? error.message : 'Unable to load session timeline')
    } finally {
      state.loading = false
    }
  }

  function loadSelectedRange() {
    return load(state.selectedRange)
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
      success(`Marker recorded: ${label}`)
      await loadSelectedRange()
    } catch (error) {
      failure(error instanceof Error ? error.message : 'Unable to create marker')
    }
  }

  function clear() {
    clearRangeLoadTimeout()
    state.data = null
    state.error = null
    state.zoomTimeline = null
    state.selectedRange = null
  }

  function setRange(range: RangeNs | null, options: { loadZoom?: boolean; deferLoad?: boolean } = {}) {
    const { loadZoom = true, deferLoad = false } = options
    state.selectedRange = range && range.to > range.from ? range : null
    if (state.selectedRange && loadZoom) {
      if (deferLoad) {
        queueRangeLoad({ ...state.selectedRange })
      } else {
        clearRangeLoadTimeout()
        void load(state.selectedRange)
      }
    } else if (!state.selectedRange) {
      clearRangeLoadTimeout()
      state.zoomTimeline = null
    } else if (!deferLoad) {
      clearRangeLoadTimeout()
    }
  }

  return {
    state,

    maxMbps() {
      return maxMbps
    },
    currentDomain() {
      return currentDomain
    },

    load,
    loadSelectedRange,
    clear,
    addMarker,
    setRange,
  }
}

export type TimelineState = ReturnType<typeof createTimelineState>
