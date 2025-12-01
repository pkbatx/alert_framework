# Transcription Monitor UI

This repository serves the Sussex County Alerts "Transcription Monitor" dashboard. The UI is a single-page experience served from `static/index.html` and powered by `static/app.js` and `static/style.css`.

## WaveSurfer.js integration
- The in-page audio player for the selected call uses [WaveSurfer.js v7](https://wavesurfer-js.org/) via an ESM CDN import inside `static/app.js`.
- When a new call is selected, any existing WaveSurfer instance is destroyed before a fresh one is created. Playback controls (play/pause, seek, volume, and speed) are wired through WaveSurfer APIs, and progress events synchronize the highlighted transcript segment.
- Dark theme colors are passed to WaveSurfer to keep waveform/progress/cursor contrast high. If the player fails to load, a fallback notice and direct audio link remain visible.

## Transcript segment format
- Interactive captions expect an array of segments shaped like:
  ```json
  [{ "start": 0.0, "end": 5.5, "text": "Segment text" }]
  ```
- If the backend does not provide time-coded data, the frontend derives coarse segments by splitting the cleaned transcript into sentences and distributing timings across the known duration (or a 3s-per-sentence fallback).
- Each segment is rendered as a keyboard-focusable button. Clicking or pressing Enter/Space seeks the player to the segment start and resumes playback.

## Filters, sorting, and export
- Filters for date range, source, status, call type, tags, and duration are applied client-side; search and status continue to be sent to `/api/transcriptions` for server-side narrowing.
- Sorting options include timestamp (asc/desc), duration (asc/desc), status, and source. Pagination is client-side at 25 items per page.
- The "Export current filter" control downloads a CSV containing the filtered set (filename, timestamp, source, type, tags, duration, status, and transcript preview).

## Accessibility notes
- All interactive controls in the player and transcript list are keyboard reachable with visible focus states. ARIA labels identify the player group, waveform, and play/pause toggle.
- Transcript content always remains available as plain text, even if the player fails.
