<script lang="ts">
  import { requestDurationMs, resourceKey } from "../timelineData"
  import { formatBytes } from "../utils/format"
  import { getSessionContext } from "../data/session-state.svelte"

  const { selectedRequest } = getSessionContext()
  const request = $derived(selectedRequest())
</script>
<article class="compact-panel">
  <h3>Selected request</h3>
  {#if request}
    <dl class="request-detail">
      <div>
        <dt>ID</dt>
        <dd>{request.request_id}</dd>
      </div>
      <div>
        <dt>Resource</dt>
        <dd>{resourceKey(request)}</dd>
      </div>
      <div>
        <dt>Path</dt>
        <dd>{request.start.url_path}</dd>
      </div>
      <div>
        <dt>Protocol</dt>
        <dd>{request.start.http_proto} {request.start.alpn_protocol || ''}</dd>
      </div>
      <div>
        <dt>TLS</dt>
        <dd>{request.start.tls_enabled ? request.start.tls_version || 'enabled' : 'off'}</dd>
      </div>
      <div>
        <dt>Content</dt>
        <dd>{formatBytes(request.start.content_length)}</dd>
      </div>
      <div>
        <dt>Profile</dt>
        <dd>{request.start.pacing_profile_name || 'unset'}</dd>
      </div>
      <div>
        <dt>Duration</dt>
        <dd>{requestDurationMs(request).toFixed(1)} ms</dd>
      </div>
      <div>
        <dt>Range</dt>
        <dd>{request.start.range_header || 'none'}</dd>
      </div>
      <div>
        <dt>Status</dt>
        <dd>{request.start.response_status}</dd>
      </div>
    </dl>
  {:else}
    <div class="empty-state">Select a request bar</div>
  {/if}
</article>
