<script lang="ts">
  import { requestDurationMs, resourceKey } from "../timelineData"
  import { formatBytes } from "../utils/format"
  import { getSessionContext } from "../data/session-state.svelte"

  const { selectedRequest } = getSessionContext()
</script>
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
        <dd>{selectedRequest.start.response_status}</dd>
      </div>
    </dl>
  {:else}
    <div class="empty-state">Select a request bar</div>
  {/if}
</article>
