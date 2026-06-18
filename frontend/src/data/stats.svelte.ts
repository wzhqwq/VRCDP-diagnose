import { getStats, type RuntimeStats } from "../api"
import { failure } from "../themedToast"
import { writable } from "svelte/store"

export let stats = writable<RuntimeStats | null>(null)
export let loadingStats = writable(false)

export async function loadStats() {
  loadingStats.set(true)
  // state.error = null

  try {
    stats.set(await getStats())
  } catch (e) {
    failure(e instanceof Error ? e.message : 'Unable to load stats')
    // state.error = e
  } finally {
    loadingStats.set(false)
  }
}


// export function createStatsState() {
//     const state = $state({
//         data: null as RuntimeStats | null,
//         loading: false,
//         error: null as unknown,
//     })
//
//     async function load() {
//         state.loading = true
//         state.error = null
//
//         try {
//             state.data = await getStats()
//         } catch (e) {
//             state.error = e
//         } finally {
//             state.loading = false
//         }
//     }
//
//     function clear() {
//         state.data = null
//         state.error = null
//     }
//
//     return {
//         state,
//
//         load,
//         clear,
//     }
// }
//
// export type StatsState = ReturnType<typeof createStatsState>;