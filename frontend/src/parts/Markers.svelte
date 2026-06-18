<script lang="ts">
  import { getSessionContext } from "../data/session-state.svelte"

  const { video: { addCursorMarker } } = getSessionContext()

  const markerLabels = ['glitch_seen', 'avatar_switch', 'mirror_opened', 'test_start', 'test_end', 'note']
</script>

<article class="compact-panel">
  <h3>Markers</h3>
  <div class="marker-buttons">
    {#each markerLabels as label (label)}
      <button type="button" onclick={() => addCursorMarker(label)}>{label}</button>
    {/each}
  </div>
</article>
