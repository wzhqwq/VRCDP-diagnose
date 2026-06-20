import { getContext, setContext } from 'svelte'
import { createTimelineState } from "./timeline.svelte"
import { createVideoState } from "./video.svelte"
import type { RequestSummary, SessionSummary } from "../api"
import { createRequestsState } from "./requests.svelte"
import { sessions } from "./sessions.svelte"
import { get } from "svelte/store"

const SESSION_CONTEXT_KEY = Symbol('session')
export type SessionRefreshMode = 'auto' | 'manual'

export function createSessionState() {
  const state = $state({
    sessionId: "",
    session: null as SessionSummary | null,
    loading: false,

    selectedRequestId: ""
  })

  const sessionId = () => state.sessionId

  const timeline = createTimelineState(sessionId)
  const requests = createRequestsState(sessionId)
  const video = createVideoState(timeline)

  const selectedRequest = $derived(
    requests.state.data.find((request) => request.request_id === state.selectedRequestId) ?? null
  )

  async function loadAll() {
    if (!state.sessionId) return

    state.loading = true
    await Promise.all([
      timeline.loadFull('manual')
    ])
    await Promise.all([
      requests.load(timeline),
    ])
    state.loading = false
  }

  function clear() {
    state.selectedRequestId = ""
    timeline.clear()
    requests.clear()
    video.clear()
  }

  async function switchSession(sessionId: string, version: number, mode: SessionRefreshMode = 'manual') {
    if (state.sessionId === sessionId) {
      await timeline.loadFull(mode)
      await requests.load(timeline)
      return
    }

    state.sessionId = sessionId
    state.session = get(sessions).find((session) => session.session_id === sessionId) ?? null
    clear()
    await loadAll()
  }

  function selectRequest(request: RequestSummary) {
    state.selectedRequestId = request.request_id
  }

  return {
    state,

    timeline,
    requests,
    video,

    selectedRequest() {
      return selectedRequest
    },

    loadAll,
    switchSession,

    selectRequest,
  }
}

export type SessionState = ReturnType<typeof createSessionState>;

export function setSessionContext(session: SessionState) {
  setContext(SESSION_CONTEXT_KEY, session)
}

export function getSessionContext() {
  const session = getContext<SessionState>(SESSION_CONTEXT_KEY)

  if (!session) {
    throw new Error('Session context is not available.')
  }

  return session
}
