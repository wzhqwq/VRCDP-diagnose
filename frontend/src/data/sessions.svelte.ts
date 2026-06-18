import { derived, writable } from "svelte/store"
import { getSessions, type SessionSummary } from "../api"
import { failure } from "../themedToast"

export const loading = writable(false)
export const sessions = writable<SessionSummary[]>([])
export const sessionFilter = writable('')

export const filteredSessions = derived(
  [sessions, sessionFilter],
  ([sessions, sessionFilter]) => sessions.filter((session) => {
    const query = sessionFilter.trim().toLowerCase()
    if (!query) return true
    return [session.session_id, session.session_label ?? '', session.start_wall_time ?? '']
      .join(' ')
      .toLowerCase()
      .includes(query)
  }),
)

export async function loadSessions(currentId: string) {
  try {
    loading.set(true)
    const nextSessions = await getSessions()
    sessions.set(nextSessions)
    return nextSessions.some((session) => session.session_id === currentId)
      ? currentId
      : nextSessions[0]?.session_id || ''
  } catch (error) {
    failure(error instanceof Error ? error.message : 'Unable to refresh diagnostics data')
  } finally {
    loading.set(false)
  }
  return currentId
}

