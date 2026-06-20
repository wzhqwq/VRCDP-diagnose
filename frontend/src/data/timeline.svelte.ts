import { createMarker, getTimeline, type TimelineSummary } from "../api"
import { failure, success } from "../themedToast"
import {
  glitchesInRange,
  markersInRange,
  requestEndNs,
  requestsInRange,
  requestStartNs,
  type RangeNs,
  timelineDomain,
  windowsInRange,
} from "../timelineData"
import { NS_PER_MS, NS_PER_SEC } from "../constants/units"

export type TimelineWindowMs = 50 | 100
type RefreshMode = 'auto' | 'manual'

interface WindowPage {
  from: number
  to: number
  loaded: boolean
}

interface WindowCache {
  pages: WindowPage[]
}

const WINDOW_SIZES: TimelineWindowMs[] = [50, 100]
const WINDOW_PAGE_SAMPLE_COUNT = 240
const DETAIL_WINDOW_THRESHOLD_NS = NS_PER_SEC * 45

function emptyTimeline(sessionId: string): TimelineSummary {
  return {
    session_id: sessionId,
    requests: [],
    windows: [],
    markers: [],
    glitches: [],
  }
}

function sampleWindowForRange(range: RangeNs): TimelineWindowMs {
  return range.to - range.from <= DETAIL_WINDOW_THRESHOLD_NS ? 50 : 100
}

function windowPageSizeNs(windowMs: TimelineWindowMs): number {
  return windowMs * NS_PER_MS * WINDOW_PAGE_SAMPLE_COUNT
}

function pageRangeFor(range: RangeNs, windowMs: TimelineWindowMs): RangeNs {
  const pageSize = windowPageSizeNs(windowMs)
  return {
    from: Math.floor(Math.max(0, range.from) / pageSize) * pageSize,
    to: Math.ceil(Math.max(0, range.to) / pageSize) * pageSize,
  }
}

function coversRange(page: WindowPage, range: RangeNs): boolean {
  return page.loaded && page.from <= range.from && page.to >= range.to
}

function mergePages(pages: WindowPage[]): WindowPage[] {
  const sorted = pages
    .filter((page) => page.to > page.from)
    .sort((a, b) => a.from - b.from || a.to - b.to)
  const merged: WindowPage[] = []
  for (const page of sorted) {
    const last = merged.at(-1)
    if (last && last.to >= page.from) {
      last.to = Math.max(last.to, page.to)
    } else {
      merged.push({ ...page })
    }
  }
  return merged
}

function addLoadedPage(cache: WindowCache, range: RangeNs) {
  cache.pages = mergePages([...cache.pages, { ...range, loaded: true }])
}

function missingWindowPages(cache: WindowCache, range: RangeNs, windowMs: TimelineWindowMs): RangeNs[] {
  const snapped = pageRangeFor(range, windowMs)
  const pageSize = windowPageSizeNs(windowMs)
  const missing: RangeNs[] = []
  for (let from = snapped.from; from < snapped.to; from += pageSize) {
    const page = { from, to: from + pageSize }
    if (!cache.pages.some((loadedPage) => coversRange(loadedPage, page))) {
      missing.push(page)
    }
  }
  return missing
}

function mergeRanges(ranges: RangeNs[]): RangeNs[] {
  const sorted = ranges
    .filter((range) => range.to > range.from)
    .sort((a, b) => a.from - b.from || a.to - b.to)
  const merged: RangeNs[] = []
  for (const range of sorted) {
    const last = merged.at(-1)
    if (last && last.to >= range.from) {
      last.to = Math.max(last.to, range.to)
    } else {
      merged.push({ ...range })
    }
  }
  return merged
}

function mergeTimeline(base: TimelineSummary, next: TimelineSummary): TimelineSummary {
  return {
    session_id: next.session_id || base.session_id,
    requests: mergeByKey(base.requests ?? [], next.requests ?? [], (request) => request.request_id),
    windows: mergeByKey(
      base.windows ?? [],
      next.windows ?? [],
      (window) => `${window.request_id}:${window.window_ms}:${window.window_start_ns}:${window.window_end_ns}`,
    ),
    markers: mergeByKey(base.markers ?? [], next.markers ?? [], (marker) => marker.marker_id),
    glitches: mergeByKey(base.glitches ?? [], next.glitches ?? [], (glitch) => glitch.glitch_id),
  }
}

function mergeByKey<T>(base: T[], next: T[], keyOf: (item: T) => string): T[] {
  const merged = new Map<string, T>()
  for (const item of base) merged.set(keyOf(item), item)
  for (const item of next) merged.set(keyOf(item), item)
  return [...merged.values()]
}

function timelineMaxNs(timeline: TimelineSummary | null): number {
  if (!timeline) return 0
  const values = [
    ...(timeline.requests ?? []).flatMap((request) => [requestStartNs(request), requestEndNs(request)]),
    ...(timeline.windows ?? []).map((window) => window.window_end_ns),
    ...(timeline.markers ?? []).map((marker) => marker.marker.time?.process_uptime_ns ?? 0),
    ...(timeline.glitches ?? []).map((glitch) => glitch.glitch.time?.process_uptime_ns ?? 0),
  ]
  return Math.max(0, ...values.filter(Number.isFinite))
}

function composeTimeline(
  timeline: TimelineSummary | null,
  range: RangeNs | null,
  windowMs: TimelineWindowMs,
): TimelineSummary | null {
  if (!timeline) return null
  return {
    session_id: timeline.session_id,
    requests: range ? requestsInRange(timeline.requests ?? [], range) : timeline.requests ?? [],
    windows: (range ? windowsInRange(timeline.windows ?? [], range) : timeline.windows ?? [])
      .filter((window) => window.window_ms === windowMs),
    markers: range ? markersInRange(timeline.markers ?? [], range) : timeline.markers ?? [],
    glitches: range ? glitchesInRange(timeline.glitches ?? [], range) : timeline.glitches ?? [],
  }
}

function hasPageCovering(cache: WindowCache | undefined, range: RangeNs): boolean {
  return Boolean(cache?.pages.some((page) => coversRange(page, range)))
}

function hasTimelineData(timeline: TimelineSummary): boolean {
  return Boolean(
    timeline.requests?.length ||
    timeline.windows?.length ||
    timeline.markers?.length ||
    timeline.glitches?.length,
  )
}

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
  let cachedTimeline: TimelineSummary | null = null
  let windowCaches = new Map<TimelineWindowMs, WindowCache>(WINDOW_SIZES.map((windowMs) => [windowMs, { pages: [] }]))

  function clearRangeLoadTimeout() {
    if (rangeLoadTimeout === null) return
    clearTimeout(rangeLoadTimeout)
    rangeLoadTimeout = null
  }

  function queueRangeLoad(range: RangeNs) {
    clearRangeLoadTimeout()
    rangeLoadTimeout = setTimeout(() => {
      rangeLoadTimeout = null
      void loadRange(range)
    }, 180)
  }

  function renderedWindowForRange(range: RangeNs): TimelineWindowMs {
    const preferredWindowMs = sampleWindowForRange(range)
    if (preferredWindowMs === 100) return 100

    const preferredPages = pageRangeFor(range, preferredWindowMs)
    return hasPageCovering(windowCaches.get(preferredWindowMs), preferredPages) ? preferredWindowMs : 100
  }

  function updateVisibleTimelines(options: { updateZoom?: boolean } = {}) {
    const { updateZoom = true } = options
    state.data = composeTimeline(cachedTimeline, null, 100)
    if (!updateZoom) return
    if (state.selectedRange) {
      state.zoomTimeline = composeTimeline(cachedTimeline, state.selectedRange, renderedWindowForRange(state.selectedRange))
    } else {
      state.zoomTimeline = null
    }
  }

  function resetCache(sId: string) {
    cachedTimeline = emptyTimeline(sId)
    windowCaches = new Map<TimelineWindowMs, WindowCache>(WINDOW_SIZES.map((windowMs) => [windowMs, { pages: [] }]))
  }

  async function fetchAndMerge(
    range: RangeNs | null,
    windowMs: TimelineWindowMs,
    options: { updateZoom?: boolean } = {},
  ) {
    const sId = sessionId()
    if (!sId) {
      return
    }
    if (!cachedTimeline || cachedTimeline.session_id !== sId) resetCache(sId)

    const query = range
      ? { fromNs: range.from, toNs: range.to, windowMs }
      : { windowMs }
    const next = await getTimeline(sId, query)
    cachedTimeline = mergeTimeline(cachedTimeline ?? emptyTimeline(sId), next)

    const cache = windowCaches.get(windowMs)
    if (cache && range && range.to < Number.MAX_SAFE_INTEGER) {
      addLoadedPage(cache, pageRangeFor(range, windowMs))
    } else if (cache && hasTimelineData(next)) {
      addLoadedPage(cache, timelineDomain(next, null))
    }
    updateVisibleTimelines(options)
  }

  async function load(range?: RangeNs | null) {
    if (range) {
      return loadRange(range)
    }
    return loadFull('manual')
  }

  async function loadFull(mode: RefreshMode = 'manual') {
    const sId = sessionId()
    if (!sId) return
    try {
      state.loading = true
      if (mode === 'manual' || !cachedTimeline || cachedTimeline.session_id !== sId || !state.data) {
        resetCache(sId)
        await fetchAndMerge(null, 100)
        if (state.selectedRange) {
          await loadRange(state.selectedRange)
        }
        return
      }
      const from = timelineMaxNs(cachedTimeline) + 1
      await fetchAndMerge(
        { from, to: Number.MAX_SAFE_INTEGER },
        100,
        { updateZoom: false },
      )
    } catch (error) {
      state.error = error
      failure(error instanceof Error ? error.message : 'Unable to load session timeline')
    } finally {
      state.loading = false
    }
  }

  async function loadRange(range: RangeNs) {
    const sId = sessionId()
    if (!sId) return
    const windowMs = sampleWindowForRange(range)
    const cache = windowCaches.get(windowMs)
    const missing = cache ? mergeRanges(missingWindowPages(cache, range, windowMs)) : [range]
    if (missing.length === 0) {
      updateVisibleTimelines()
      return
    }
    try {
      state.loading = true
      for (const missingRange of missing) {
        await fetchAndMerge(missingRange, windowMs)
      }
    } catch (error) {
      state.error = error
      failure(error instanceof Error ? error.message : 'Unable to load session timeline')
    } finally {
      state.loading = false
    }
  }

  function loadSelectedRange() {
    if (!state.selectedRange) return Promise.resolve()
    return loadRange(state.selectedRange)
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
      await loadFull('manual')
    } catch (error) {
      failure(error instanceof Error ? error.message : 'Unable to create marker')
    }
  }

  function clear() {
    clearRangeLoadTimeout()
    cachedTimeline = null
    windowCaches = new Map<TimelineWindowMs, WindowCache>(WINDOW_SIZES.map((windowMs) => [windowMs, { pages: [] }]))
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
        updateVisibleTimelines()
        queueRangeLoad({ ...state.selectedRange })
      } else {
        clearRangeLoadTimeout()
        void loadRange(state.selectedRange)
      }
    } else if (!state.selectedRange) {
      clearRangeLoadTimeout()
      updateVisibleTimelines()
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
    loadFull,
    loadSelectedRange,
    clear,
    addMarker,
    hasWindowRange(range: RangeNs, windowMs: TimelineWindowMs) {
      return hasPageCovering(windowCaches.get(windowMs), pageRangeFor(range, windowMs))
    },
    preferredWindowMs(range: RangeNs) {
      return sampleWindowForRange(range)
    },
    setRange,
  }
}

export type TimelineState = ReturnType<typeof createTimelineState>
