# Svelte Coding Style

This guide summarizes the frontend coding style used by the diagnostics
dashboard after the `b7e1feb` refactor. Use it as a reusable instruction file for
Svelte 5 + TypeScript applications that should stay modular, data-oriented, and
easy to extend.

## Architecture

- Keep the root component thin. It should initialize global data, own only the
  smallest amount of top-level state, and compose feature-level components.
- Split feature work into three layers:
  - `data/*.svelte.ts` for state factories, derived values, async loading, and
    feature actions.
  - Feature or workspace components, such as `Session.svelte`, for composing a
    complete screen area.
  - `parts/*.svelte` for focused UI panels, forms, lists, and details.
- Prefer feature-specific modules over one large component. Move code out when a
  section has its own state, actions, or rendering concerns.
- Use context for shared feature state. Create the state once near the feature
  boundary, call `setContext`, and let child parts read it with a typed
  `get...Context()` helper.
- Keep pure data transforms outside components. Put formatting, grouping,
  timeline math, summaries, and unit constants in plain TypeScript modules.

## State And Data

- Use Svelte 5 runes for local and feature state:
  - `$state` for mutable feature state objects and local form fields.
  - `$derived` for computed values.
  - `$effect` for reacting to prop or state changes.
- Wrap feature state in factory functions named `create...State`.
- Return a clear object from state factories:
  - `state` first.
  - child state modules next.
  - derived value accessors after state.
  - actions last.
- Do not return `$derived` values directly from a state factory. Returning the
  value snapshots the current value into the returned object. Wrap derived reads
  in closures instead:

  ```ts
  const total = $derived(items.reduce((sum, item) => sum + item.value, 0))

  return {
    state,
    total() {
      return total
    },
  }
  ```

- When consuming derived accessors from a state object, call the accessor inside
  markup or another `$derived`, such as `const total = $derived(state.total())`.
- Export state types with `ReturnType<typeof create...State>`.
- Pass dependencies into state factories as functions when the dependency can
  change, such as `sessionId: () => string`.
- Keep load actions idempotent and guarded. If required IDs are missing, return
  early.
- Track `loading` and `error` inside the relevant state module, not in a distant
  component.
- Use `try/catch/finally` around async data loading. Set loading before the
  request, record errors, show a toast when useful, and clear loading in
  `finally`.
- When a child data source can be derived from a parent response, hydrate it from
  the parent instead of making a redundant API request.
- Provide `clear()` functions for feature state so session switches can reset all
  related data in one place.

## Components

- Use `<script lang="ts">` and define a local `Props` interface when a component
  accepts props.
- Destructure props from `$props()` near the top of the script.
- Destructure context state and actions at the top of small parts so markup stays
  readable.
- Keep components focused on presentation and direct user interaction. Complex
  calculations belong in `data` modules or utility modules.
- Prefer explicit event handler functions for non-trivial interactions.
- Use native event attributes such as `onclick`, `onsubmit`, and `onchange`.
- Key every `{#each}` block by a stable ID or stable string.
- Use semantic elements that match behavior and document structure, such as
  `section`, `article`, `aside`, `dl`, and `button`.
- Include practical accessibility attributes on major regions and interactive
  graphics, such as `aria-label`, `aria-labelledby`, `aria-live`, `role`, and
  `tabindex`.
- Keep empty states close to the UI they replace.

## Data Visualization

- Use D3 for timeline and chart rendering, but isolate imperative D3 drawing in a
  small set of render functions.
- Bind container width from Svelte and redraw only when the SVG element, data, and
  minimum width are ready.
- Clear and redraw the SVG on each render instead of mixing long-lived D3 state
  with Svelte-managed DOM.
- Keep chart helper functions pure where possible. Use typed domain objects such
  as ranges, lanes, summaries, and segments.
- Use explicit chart constants for margins, lane sizes, and sizing thresholds.
- Add SVG titles and ARIA labels for interactive marks.

## Forms And Feedback

- Keep form state local to the form component unless another feature needs it.
- Convert empty string form values to `undefined` before sending API payloads.
- Reset form fields after successful submission.
- Use a small toast helper module for success, warning, and failure messages.
- Keep API-specific payload construction in the submitting component when it is
  simple; move it to a helper only when it becomes shared or complex.

## Naming

- Name state factories by domain: `createSessionState`, `createTimelineState`,
  `createVideoState`.
- Name context helpers as `set...Context` and `get...Context`.
- Use domain nouns for data modules and parts: `sessions`, `requests`, `timeline`,
  `video`, `Markers`, `Requests`, `RequestDetail`, `Zoomed`.
- Use explicit domain terms in state fields: `selectedRequestId`,
  `selectedRange`, `zoomTimeline`, `recordingSegments`, `playbackCursorNs`.
- Keep utility names direct: `formatBytes`, `formatDuration`,
  `formatProcessTime`, `timelineDomain`, `summarizeRange`.

## Import And File Style

- Prefer TypeScript type imports with `import type`.
- Import from nearby domain modules directly; avoid barrel files until they remove
  real repetition.
- Keep comments rare and only for non-obvious behavior.
- Use ASCII text in source and docs unless the project already requires Unicode.
- Keep semicolons out of Svelte and TypeScript files unless the surrounding code
  has a different established convention.

## What To Avoid

- Do not let `App.svelte` become the dumping ground for feature state,
  transformations, API calls, and markup.
- Do not duplicate API loading if a richer parent response already contains the
  child data.
- Do not hide domain calculations inside markup.
- Do not guess icon or component APIs in projects that use documented component
  libraries; consult the project tooling first.
