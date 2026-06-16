# Svelte MCP Server Usage Guide

You are able to use the Svelte MCP server, where you have access to comprehensive Svelte 5 and SvelteKit documentation. Here's how to use the available tools effectively:

## Available Svelte MCP Tools:

### 1. list-sections

Use this FIRST to discover all available documentation sections. Returns a structured list with titles, use_cases, and paths.
When asked about Svelte or SvelteKit topics, ALWAYS use this tool at the start of the chat to find relevant sections.

### 2. get-documentation

Retrieves full documentation content for specific sections. Accepts single or multiple sections.
After calling the list-sections tool, you MUST analyze the returned documentation sections (especially the use_cases field) and then use the get-documentation tool to fetch ALL documentation sections that are relevant for the user's task.

### 3. svelte-autofixer

Analyzes Svelte code and returns issues and suggestions.
You MUST use this tool whenever writing Svelte code before sending it to the user. Keep calling it until no issues or suggestions are returned.

### 4. playground-link

Generates a Svelte Playground link with the provided code.
After completing the code, ask the user if they want a playground link. Only call this tool after user confirmation and NEVER if code was written to files in their project.

# shadcn-svelte MCP Server Usage Guide

You are able to use the shadcn-svelte MCP server for shadcn-svelte components, blocks, charts, documentation, Svelte Sonner docs, underlying Bits UI primitives, and Lucide icons. Use it whenever frontend work needs component examples, installation commands, lower-level primitive details, or icon discovery.

## Available shadcn-svelte MCP Tools:

### 1. shadcn-svelte-list

Lists available shadcn-svelte components, blocks, charts, docs, or Bits UI primitives.
Use this when discovering what exists or when a component/block name is uncertain.

### 2. shadcn-svelte-search

Searches shadcn-svelte documentation, components, blocks, charts, and examples by keyword.
Use this for discovery and fuzzy matching. Keep queries short and component-oriented, such as `button`, `dialog`, `dashboard`, or `chart`.

### 3. shadcn-svelte-get

Retrieves detailed usage information for a shadcn-svelte component, block, chart, documentation section, or Svelte Sonner docs.
Use this FIRST for normal shadcn-svelte usage, installation, and page composition. Do not use React-only shadcn patterns such as `asChild`; follow Svelte examples and APIs from this tool.

### 4. bits-ui-get

Retrieves lower-level Bits UI primitive documentation and API details, including the component llms.txt source.
Use this when shadcn-svelte docs point to an underlying primitive, when primitive props/events are unclear, or when directly using Bits UI. Prefer shadcn-svelte-get for normal component work, then use bits-ui-get only for deeper primitive behavior.

### 5. shadcn-svelte-icons

Searches Lucide icons available through `@lucide/svelte`.
Use this before adding or changing icon imports. Search by concrete names or intent, such as `refresh`, `bell`, `server`, `check`, or `x`, and import only icons that the tool confirms exist.

## Usage Rules

- For shadcn-svelte component work, start with `shadcn-svelte-get` if you know the component name; otherwise use `shadcn-svelte-search` or `shadcn-svelte-list`.
- Use `bits-ui-get` to fetch Bits UI primitive API details and llms.txt-backed docs when lower-level behavior matters.
- Use `shadcn-svelte-icons` to find Lucide icons instead of guessing icon names.
- Keep components consistent with this dashboard's quiet operational style; shadcn-svelte examples are references, not a reason to introduce marketing-style layouts.
- After writing Svelte code, still run the Svelte MCP `svelte-autofixer` before finishing frontend work.

# Current Frontend Direction

- Build a quiet operational diagnostics interface, not a marketing landing page.
- Prioritize dense, scannable views for sessions, requests, chunk events, pacing windows, markers, and glitches.
- Keep the initial visual system restrained: neutral surfaces, crisp borders, small status treatments, and clear data hierarchy.
- Use D3.js for stacking views, timelines, zoom views, and diagrams.
- Keep the session list fixed on the left and the selected/current session workspace on the right.
- The main timeline must show all visible selected-session telemetry, group displayed requests by resource id with URL/request fallback, and alternate request group colors.
- The main timeline must support range selection; the bottom of the selected session workspace shows the zoomed D3 view and computed range details.
- Do not start the dev server or perform browser checks for frontend changes unless the user explicitly requests it; prefer static checks, Svelte autofixer, type checks, and production builds for verification.
