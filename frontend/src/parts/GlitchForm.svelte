<script lang="ts">
  import { createGlitch, type GlitchEvent } from "../api"
  import { failure, success } from "../themedToast"
  import { getSessionContext } from "../data/session-state.svelte"

  const severities = ['', 'low', 'medium', 'high']
  const corruptionTypes = [
    '',
    'block_artifacts',
    'green_or_purple_blocks',
    'full_frame_corruption',
    'partial_texture_corruption',
    'frame_freeze',
    'black_frame',
    'unknown',
  ]

  const {
    timeline: {
      state: timelineState,
      loadSelectedRange
    }
  } = getSessionContext()

  const selectedRange = $derived(timelineState.selectedRange)

  let glitchSeverity = $state('')
  let glitchType = $state('')
  let recordingFilename = $state('')
  let recordingFrameIndex = $state<number | undefined>(undefined)
  let recordingTimeSec = $state<number | undefined>(undefined)
  let durationFrames = $state<number | undefined>(undefined)
  let durationMs = $state<number | undefined>(undefined)
  let glitchNotes = $state('')

  async function submitGlitch(event: SubmitEvent) {
    event.preventDefault()
    const glitch: GlitchEvent = {
      severity: glitchSeverity || undefined,
      corruption_type: glitchType || undefined,
      recording_filename: recordingFilename || undefined,
      recording_frame_index: recordingFrameIndex,
      recording_time_sec: recordingTimeSec,
      duration_frames: durationFrames,
      duration_ms: durationMs,
      notes: glitchNotes || undefined,
      source: 'frontend',
    }
    if (selectedRange) {
      glitch.time = {
        process_uptime_ns: Math.round((selectedRange.from + selectedRange.to) / 2),
        wall_unix_nano: 0,
        wall_rfc3339_nano: '',
      }
    }

    try {
      const id = await createGlitch(glitch)
      success(`Glitch recorded: ${id}`)
      resetGlitchForm()
      void loadSelectedRange()
    } catch (error) {
      failure(error instanceof Error ? error.message : 'Unable to create glitch')
    }
  }

  function resetGlitchForm() {
    glitchSeverity = ''
    glitchType = ''
    recordingFilename = ''
    recordingFrameIndex = undefined
    recordingTimeSec = undefined
    durationFrames = undefined
    durationMs = undefined
    glitchNotes = ''
  }

</script>
<article class="compact-panel">
  <h3>Glitch annotation</h3>
  <form class="glitch-form" onsubmit={submitGlitch}>
    <div class="form-row">
      <label>
        <span>Severity</span>
        <select bind:value={glitchSeverity}>
          {#each severities as severity (severity)}
            <option value={severity}>{severity || 'unset'}</option>
          {/each}
        </select>
      </label>
      <label>
        <span>Type</span>
        <select bind:value={glitchType}>
          {#each corruptionTypes as type (type)}
            <option value={type}>{type || 'unset'}</option>
          {/each}
        </select>
      </label>
    </div>
    <label>
      <span>Recording file</span>
      <input bind:value={recordingFilename}/>
    </label>
    <div class="form-row">
      <label>
        <span>Frame</span>
        <input bind:value={recordingFrameIndex} min="0" type="number"/>
      </label>
      <label>
        <span>Time sec</span>
        <input bind:value={recordingTimeSec} min="0" step="0.001" type="number"/>
      </label>
    </div>
    <div class="form-row">
      <label>
        <span>Frames</span>
        <input bind:value={durationFrames} min="0" type="number"/>
      </label>
      <label>
        <span>Duration ms</span>
        <input bind:value={durationMs} min="0" step="0.1" type="number"/>
      </label>
    </div>
    <label>
      <span>Notes</span>
      <textarea bind:value={glitchNotes} rows="3"></textarea>
    </label>
    <button type="submit">Record glitch</button>
  </form>
</article>
