<script lang="ts">
  import { getSessionContext } from "../data/session-state.svelte"

  const { video: { addCursorMarker } } = getSessionContext()

  const markerLabels = ['glitch_seen', 'avatar_switch', 'mirror_opened', 'test_start', 'test_end', 'note']
</script>

<article class="rounded-lg border border-gray-200 bg-white/70 p-2">
  <div class="marker-buttons flex flex-wrap gap-2">
    {#each markerLabels as label (label)}
      <button type="button" onclick={() => addCursorMarker(label)}>{label}</button>
    {/each}
  </div>
</article>
