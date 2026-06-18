import type { RequestSummary } from "../api"
import { getRequests } from '../api'
import type { TimelineState } from "./timeline.svelte"
import { failure } from "../themedToast"

export function createRequestsState(sessionId: () => string) {
  const state = $state({
    data: [] as RequestSummary[],
    loading: false,
    error: null as unknown,
  })

  const totalBytesServed = $derived(state.data.reduce((total, request) => total + (request.end?.total_bytes_sent ?? 0), 0))

  async function load(timelineState?: TimelineState) {
    if (timelineState) {
      state.data = timelineState.state.data?.requests ?? []
      state.error = null
      return
    }

    state.loading = true
    state.error = null

    try {
      state.data = await getRequests(sessionId())
    } catch (e) {
      state.error = e
      failure(e instanceof Error ? e.message : 'Unable to load requests')
    } finally {
      state.loading = false
    }
  }

  function clear() {
    state.data = []
    state.error = null
  }

  return {
    state,

    totalBytesServed() {
      return totalBytesServed
    },

    load,
    clear,
  }
}

export type RequestState = ReturnType<typeof createRequestsState>;
