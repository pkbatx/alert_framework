# Alert Framework Transcription Monitor

This UI provides a dark-themed dashboard for reviewing recent call audio, transcripts, and worker health. The frontend is served from the `static/` directory and interacts with the `/api/transcriptions` and `/api/transcription/{filename}` endpoints exposed by the Go server.

## WaveSurfer integration
- The detail panel mounts a WaveSurfer.js v7 instance for the selected call (see `static/app.js`).
- When a new call is selected, the existing player is destroyed before creating a new instance.
- Playback controls include play/pause, seek via the waveform, volume, and playback speed adjustments.
- If the player fails to initialize or no audio URL is available, the UI surfaces a fallback message and keeps the download link visible.

## Transcript segmentation and sync
- Cleaned transcripts are converted into time-coded segments when the backend does not provide `segments` or `transcript_segments` data.
- Segments are derived by splitting text into sentences and estimating timing using call duration when available.
- As audio plays, the UI highlights the active segment and scrolls it into view. Clicking a segment seeks playback to its start time.
- Buttons beside the transcript let reviewers copy the cleaned transcript, download a `.txt` file, or mark the call as reviewed (client-side only).

## Filters and export
- The call list supports search, status, source, date range, call type, tag, duration, and sort controls. Pagination is handled client-side with adjustable page sizes.
- Tag chips on each call card can also toggle a focused tag filter.
- The "Export" action downloads the currently filtered set as CSV, including filename, timestamps, tags, duration, status, and a short preview.

## Adding new filters or fields
- UI controls live in `static/index.html` and the filtering/sorting logic is handled in `renderList` within `static/app.js`.
- To add a filter backed by the API, set the appropriate `searchParams` in `loadList`. For purely client-side filters, extend the predicates in `renderList` and add matching form controls.
- Additional metadata to display per call can be appended to the card template in `renderList` along with supporting styles in `static/style.css`.
