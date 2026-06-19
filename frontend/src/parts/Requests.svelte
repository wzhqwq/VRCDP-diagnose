<script lang="ts">
  import { getSessionContext } from "../data/session-state.svelte"
  import { requestEndNs, requestStartNs, resourceKey } from "../timelineData"
  import { formatBytes, formatDuration, formatProcessTime } from "../utils/format"

  const {
    state: sessionState,
    requests: {
      state: requestsState,
    },
    selectRequest
  } = getSessionContext()

  const requests = $derived(requestsState.data)
  const selectedRequestId = $derived(sessionState.selectedRequestId)

</script>
<section aria-labelledby="request-list-title" class="panel">
  <h3 id="request-list-title">Requests</h3>
  <div class="request-table">
    <div class="request-row header">
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
