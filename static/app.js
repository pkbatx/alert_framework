const state = {
  items: [],
  filtered: [],
  selected: null,
  activeTab: 'clean',
  tagFilter: null,
  dateRange: '24h',
  callTypes: new Set(),
  durationBucket: 'all',
  sort: 'time_desc',
  page: 1,
  pageSize: 25,
  segments: [],
  reviewed: new Set(),
};

const flags = {
  devMode: document.body.dataset.devMode === 'true',
  defaultCleanup: document.body.dataset.defaultCleanup || '',
};

let waveInstance = null;
let waveDuration = 0;
let lastHighlightTs = 0;

const formatTime = (value) => {
  if (!value) return '';
  const d = new Date(value);
  return d.toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' });
};

const debounce = (fn, wait = 250) => {
  let t;
  return (...args) => {
    clearTimeout(t);
    t = setTimeout(() => fn(...args), wait);
  };
};

const formatDuration = (seconds) => {
  if (seconds === undefined || seconds === null || Number.isNaN(Number(seconds))) return '--';
  const total = Math.max(0, Math.round(Number(seconds)));
  const mins = Math.floor(total / 60)
    .toString()
    .padStart(2, '0');
  const secs = (total % 60).toString().padStart(2, '0');
  return `${mins}:${secs}`;
};

const formatTimestamp = (seconds) => {
  const mins = Math.floor(seconds / 60)
    .toString()
    .padStart(2, '0');
  const secs = Math.floor(seconds % 60)
    .toString()
    .padStart(2, '0');
  return `${mins}:${secs}`;
};

const renderTags = (tags = []) => {
  if (!tags.length) return '';
  return `<div class="tag-row">${tags.map((t) => `<button class="badge tag" data-tag="${t}" type="button">${t}</button>`).join('')}</div>`;
};

const buildLocationLine = (item) => {
  const pieces = [item.town, item.agency].filter(Boolean);
  return pieces.join(' • ');
};

window.addEventListener('DOMContentLoaded', () => {
  document.getElementById('search').addEventListener('input', debounce(handleFilterChange, 200));
  document.getElementById('status-filter').addEventListener('change', handleFilterChange);
  document.getElementById('source-filter').addEventListener('change', handleFilterChange);
  document.getElementById('sort-order').addEventListener('change', (e) => { state.sort = e.target.value; renderList(); });
  document.getElementById('refresh-btn').addEventListener('click', () => { loadList(); loadStatus(); });
  document.getElementById('export-btn').addEventListener('click', exportCurrentView);
  setupChipGroup('date-filter', (value) => { state.dateRange = value; handleFilterChange(); });
  setupChipGroup('duration-filter', (value) => { state.durationBucket = value; handleFilterChange(); });
  setupChipGroup('calltype-filter', toggleCallType);
  document.getElementById('retry-status').addEventListener('click', loadStatus);
  document.getElementById('prev-page').addEventListener('click', () => changePage(-1));
  document.getElementById('next-page').addEventListener('click', () => changePage(1));
  document.getElementById('copy-transcript').addEventListener('click', copyTranscriptToClipboard);
  document.getElementById('download-transcript').addEventListener('click', downloadTranscriptText);
  document.getElementById('mark-reviewed').addEventListener('click', toggleReviewed);
  document.getElementById('settings-form').addEventListener('submit', saveSettings);
  document.getElementById('reset-cleanup').addEventListener('click', resetCleanupPrompt);
  document.getElementById('trigger-backfill').addEventListener('click', runBackfill);
  setupTabs();
  setupAdminVisibility();
  setupCollapsibles();
  renderTagFilterPill();
  loadList();
  loadStatus();
  loadSettings();
});

function setupChipGroup(id, handler) {
  const row = document.getElementById(id);
  if (!row) return;
  row.querySelectorAll('button').forEach((btn) => {
    btn.addEventListener('click', () => {
      if (btn.dataset.type) {
        btn.classList.toggle('active');
        handler(btn.dataset.type, btn.classList.contains('active'));
      } else {
        row.querySelectorAll('button').forEach((b) => b.classList.toggle('active', b === btn));
        handler(btn.dataset.range || btn.dataset.duration || btn.dataset.type || btn.value);
      }
    });
  });
}

function toggleCallType(type, isActive) {
  if (isActive) state.callTypes.add(type);
  else state.callTypes.delete(type);
  handleFilterChange();
}

function handleFilterChange() {
  state.page = 1;
  renderList();
}

async function loadList() {
  const search = document.getElementById('search').value.trim();
  const status = document.getElementById('status-filter').value;
  const url = new URL('/api/transcriptions', window.location.origin);
  if (search) url.searchParams.set('q', search);
  if (status) url.searchParams.set('status', status);
  url.searchParams.set('sort', 'time');
  url.searchParams.set('page', '1');
  url.searchParams.set('page_size', '100');
  toggleError(false);
  try {
    const res = await fetch(url);
    if (!res.ok) throw new Error('request failed');
    state.items = await res.json();
    renderList();
  } catch (e) {
    toggleError(true);
  }
}

function renderList() {
  const listEl = document.getElementById('list');
  listEl.innerHTML = '';
  applyFilters();
  const totalPages = Math.max(1, Math.ceil(state.filtered.length / state.pageSize));
  state.page = Math.min(state.page, totalPages);

  const durations = state.filtered.map((item) => Number(item.duration_seconds) || 0).filter((v) => v > 0);
  const avgDuration = durations.length ? durations.reduce((a, b) => a + b, 0) / durations.length : 0;
  document.getElementById('list-caption').textContent = `${state.filtered.length} calls • Avg duration ${formatDuration(avgDuration)}`;

  if (!state.filtered.length) {
    document.getElementById('empty-state').classList.remove('hidden');
    return;
  }
  document.getElementById('empty-state').classList.add('hidden');

  const start = (state.page - 1) * state.pageSize;
  const pageItems = state.filtered.slice(start, start + state.pageSize);
  pageItems.forEach((item) => {
    const card = document.createElement('div');
    card.className = 'call-card';
    card.role = 'listitem';
    const snippet = buildSnippet(item);
    const title = item.pretty_title || item.filename;
    const location = buildLocationLine(item);
    const duration = item.duration_seconds ? `<span class="badge duration">${formatDuration(item.duration_seconds)}</span>` : '';
    card.innerHTML = `
      <div class="card-header">
        <div>
          <div class="filename">${title}</div>
          ${location ? `<p class="muted tight">${location}</p>` : ''}
          ${renderTags(item.tags)}
        </div>
        <div class="badge-row">
          ${renderStatusBadge(item.status)}
          ${renderSourceBadge(item.source)}
          ${duration}
        </div>
      </div>
      <p class="snippet">${snippet}</p>
      <div class="meta">
        <span>${formatTime(item.updated_at)}</span>
        ${item.call_type ? `<span>${item.call_type}</span>` : ''}
      </div>
    `;
    card.addEventListener('click', () => selectItem(item));
    card.querySelectorAll('.badge.tag').forEach((tagBtn) => {
      tagBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        toggleTagFilter(tagBtn.dataset.tag);
      });
    });
    if (state.selected && state.selected.filename === item.filename) {
      card.classList.add('active');
    }
    listEl.appendChild(card);
  });

  const pageLabel = document.getElementById('page-label');
  pageLabel.textContent = `Page ${state.page} of ${totalPages}`;
  document.getElementById('prev-page').disabled = state.page <= 1;
  document.getElementById('next-page').disabled = state.page >= totalPages;
}

function applyFilters() {
  const sourceFilter = document.getElementById('source-filter').value;
  const statusFilter = document.getElementById('status-filter').value;
  const search = document.getElementById('search').value.trim().toLowerCase();
  const now = Date.now();
  const startOfToday = new Date();
  startOfToday.setHours(0, 0, 0, 0);
  const rangeStart = (() => {
    switch (state.dateRange) {
      case 'today':
        return startOfToday.getTime();
      case '7d':
        return now - 7 * 24 * 60 * 60 * 1000;
      case '24h':
        return now - 24 * 60 * 60 * 1000;
      default:
        return null;
    }
  })();

  state.filtered = state.items
    .filter((item) => {
      if (sourceFilter && item.source !== sourceFilter) return false;
      if (statusFilter && item.status !== statusFilter) return false;
      if (state.tagFilter) {
        const tags = Array.isArray(item.tags) ? item.tags : [];
        if (!tags.includes(state.tagFilter)) return false;
      }
      if (search) {
        const blob = [item.filename, item.pretty_title, item.clean_transcript_text, item.transcript_text]
          .join(' ')
          .toLowerCase();
        if (!blob.includes(search)) return false;
      }
      if (state.callTypes.size > 0) {
        const type = (item.call_type || '').toLowerCase();
        if (!state.callTypes.has(type)) return false;
      }
      if (state.durationBucket !== 'all') {
        const dur = Number(item.duration_seconds || 0);
        if (state.durationBucket === 'short' && !(dur > 0 && dur < 60)) return false;
        if (state.durationBucket === 'medium' && !(dur >= 60 && dur <= 300)) return false;
        if (state.durationBucket === 'long' && !(dur > 300)) return false;
      }
      if (rangeStart) {
        const t = new Date(item.updated_at).getTime();
        if (Number.isFinite(t) && t < rangeStart) return false;
      }
      return true;
    })
    .sort(sortComparer(state.sort));
}

function sortComparer(key) {
  return (a, b) => {
    switch (key) {
      case 'time_asc':
        return new Date(a.updated_at) - new Date(b.updated_at);
      case 'duration_desc':
        return (b.duration_seconds || 0) - (a.duration_seconds || 0);
      case 'duration_asc':
        return (a.duration_seconds || 0) - (b.duration_seconds || 0);
      case 'status':
        return (a.status || '').localeCompare(b.status || '');
      case 'source':
        return (a.source || '').localeCompare(b.source || '');
      default:
        return new Date(b.updated_at) - new Date(a.updated_at);
    }
  };
}

function changePage(delta) {
  const totalPages = Math.max(1, Math.ceil(state.filtered.length / state.pageSize));
  state.page = Math.min(totalPages, Math.max(1, state.page + delta));
  renderList();
}

function buildSnippet(item) {
  const text = item.clean_transcript_text || item.transcript_text || item.raw_transcript_text;
  if (!text) return 'Transcript not available yet.';
  const trimmed = text.replace(/\s+/g, ' ').trim();
  return trimmed.length > 140 ? `${trimmed.slice(0, 140)}…` : trimmed;
}

function renderStatusBadge(status) {
  const cls = `badge status-${status || 'queued'}`;
  const label = status ? status.charAt(0).toUpperCase() + status.slice(1) : 'Queued';
  return `<span class="${cls}">${label}</span>`;
}

function renderSourceBadge(source) {
  if (!source) return '';
  return `<span class="badge source-${source}">${source}</span>`;
}

async function selectItem(item) {
  stopWaveform();
  try {
    const res = await fetch(`/api/transcription/${encodeURIComponent(item.filename)}`);
    if (!res.ok) throw new Error('failed');
    const data = await res.json();
    state.selected = data;
    document.getElementById('detail-title').textContent = data.pretty_title || data.filename;
    const metaParts = [];
    if (data.duration_seconds) metaParts.push(`${data.duration_seconds.toFixed(1)}s`);
    if (data.size_bytes) metaParts.push(`${(data.size_bytes / 1024 / 1024).toFixed(2)} MB`);
    metaParts.push(formatTime(data.updated_at));
    document.getElementById('detail-meta').textContent = metaParts.filter(Boolean).join(' • ');
    const location = buildLocationLine(data);
    document.getElementById('detail-location').textContent = location || data.filename;
    renderBadges(data);
    renderAudioPlayer(data);
    updateTranscript(state.activeTab);
    const reviewedBtn = document.getElementById('mark-reviewed');
    const isReviewed = state.reviewed.has(data.filename);
    reviewedBtn.classList.toggle('active', isReviewed);
    reviewedBtn.setAttribute('aria-pressed', isReviewed.toString());
  } catch (e) {
    document.getElementById('detail-meta').textContent = 'Unable to load transcription details.';
  }
}

function renderBadges(data) {
  const wrap = document.getElementById('detail-badges');
  wrap.innerHTML = '';
  wrap.insertAdjacentHTML('beforeend', renderStatusBadge(data.status));
  wrap.insertAdjacentHTML('beforeend', renderSourceBadge(data.source));
  if (data.call_type) wrap.insertAdjacentHTML('beforeend', `<span class="badge">${data.call_type}</span>`);
  const recognized = Array.isArray(data.recognized_towns)
    ? data.recognized_towns
    : data.recognized_towns
      ? [data.recognized_towns]
      : [];
  const tags = Array.isArray(data.tags) ? data.tags : recognized;
  const tagHtml = renderTags(tags);
  if (tagHtml) {
    wrap.insertAdjacentHTML('beforeend', tagHtml);
  }
}

function setupTabs() {
  document.querySelectorAll('.transcript-tabs button').forEach((btn) => {
    btn.addEventListener('click', () => {
      state.activeTab = btn.dataset.tab;
      setActiveTab(btn.dataset.tab);
      updateTranscript(btn.dataset.tab);
    });
  });
}

function setActiveTab(tab) {
  document.querySelectorAll('.transcript-tabs button').forEach((btn) => {
    btn.classList.toggle('active', btn.dataset.tab === tab);
  });
}

function updateTranscript(tab) {
  const pre = document.getElementById('transcript-text');
  const list = document.getElementById('transcript-list');
  const data = state.selected;
  if (!data) {
    pre.textContent = 'No call selected.';
    pre.classList.remove('hidden');
    list.classList.add('hidden');
    return;
  }
  let text = '';
  if (tab === 'raw') text = data.raw_transcript_text || data.transcript_text || '';
  else if (tab === 'translation') text = data.translation_text || 'No translation available yet.';
  else text = data.clean_transcript_text || data.transcript_text || '';
  if (tab === 'clean') {
    state.segments = buildSegments(text, data.duration_seconds);
    renderSegments(state.segments);
    list.classList.toggle('hidden', !state.segments.length);
    pre.classList.toggle('hidden', !!state.segments.length);
    pre.textContent = !state.segments.length ? text || `Status: ${data.status}` : '';
  } else {
    pre.classList.remove('hidden');
    list.classList.add('hidden');
    pre.textContent = text || `Status: ${data.status}`;
  }
}

function toggleError(show) {
  document.getElementById('list-error').classList.toggle('hidden', !show);
  document.getElementById('list').classList.toggle('hidden', show);
  document.getElementById('empty-state').classList.toggle('hidden', show);
}

async function loadStatus() {
  try {
    const res = await fetch('/debug/queue');
    if (!res.ok) throw new Error('status failed');
    const data = await res.json();
    document.getElementById('queue-level').textContent = data.length ?? '--';
    document.getElementById('queue-capacity').textContent = `of ${data.capacity ?? '--'}`;
    document.getElementById('worker-count').textContent = data.workers ?? '--';
    document.getElementById('stat-queue').textContent = `${data.length ?? 0}`;
    document.getElementById('stat-capacity').textContent = data.capacity ?? '--';
    document.getElementById('stat-workers').textContent = data.workers ?? '--';
    document.getElementById('stat-processed').textContent = data.processed_jobs ?? '--';
    document.getElementById('stat-failed').textContent = data.failed_jobs ?? '--';
    renderViz(data);
    renderBackfill(data.has_backfill ? data.last_backfill : null, data.backfill_running);
  } catch (e) {
    document.getElementById('backfill-caption').textContent = 'Unable to load status right now.';
  }
}

function renderViz(data) {
  const capacity = Number(data.capacity) || 0;
  const length = Number(data.length) || 0;
  const processed = Number(data.processed_jobs) || 0;
  const failed = Number(data.failed_jobs) || 0;
  const queuePct = capacity > 0 ? Math.min(100, Math.round((length / capacity) * 100)) : 0;
  const successDenom = processed + failed;
  const successPct = successDenom > 0 ? Math.round(((processed - failed) / successDenom) * 100) : 0;

  document.getElementById('viz-queue-label').textContent = capacity ? `${queuePct}% full` : '--';
  document.getElementById('viz-success-label').textContent = successDenom ? `${successPct}% success` : '--';
  document.getElementById('viz-queue').style.width = `${queuePct}%`;
  document.getElementById('viz-success').style.width = `${Math.max(0, Math.min(100, successPct))}%`;
}

function renderBackfill(summary, busy = false) {
  const btn = document.getElementById('trigger-backfill');
  btn.disabled = !!busy;
  document.getElementById('backfill-hint').textContent = busy ? 'Backfill running…' : 'Runs up to the configured backfill limit.';
  if (!summary) {
    document.getElementById('backfill-caption').textContent = 'No runs yet.';
    return;
  }
  document.getElementById('backfill-caption').textContent = `${summary.selected || 0} selected • ${summary.enqueued || 0} enqueued`;
  document.getElementById('bf-selected').textContent = summary.selected ?? '--';
  document.getElementById('bf-enqueued').textContent = summary.enqueued ?? '--';
  document.getElementById('bf-dropped').textContent = summary.dropped_full ?? '--';
  document.getElementById('bf-errors').textContent = summary.other_errors ?? '--';
  document.getElementById('bf-processed').textContent = summary.already_processed ?? '--';
  document.getElementById('bf-unprocessed').textContent = summary.unprocessed ?? '--';
}

async function runBackfill() {
  const btn = document.getElementById('trigger-backfill');
  btn.disabled = true;
  document.getElementById('backfill-hint').textContent = 'Starting backfill…';
  try {
    const res = await fetch('/api/backfill', { method: 'POST' });
    if (!res.ok) throw new Error('failed');
    const data = await res.json();
    if (data.status === 'busy') {
      document.getElementById('backfill-hint').textContent = 'Backfill already running.';
    } else {
      document.getElementById('backfill-hint').textContent = 'Backfill started.';
      setTimeout(loadStatus, 500);
    }
  } catch (e) {
    document.getElementById('backfill-hint').textContent = 'Unable to start backfill right now.';
  }
}

async function loadSettings() {
  try {
    const res = await fetch('/api/settings');
    if (!res.ok) return;
    const data = await res.json();
    document.getElementById('setting-model').value = data.DefaultModel || 'gpt-4o-transcribe';
    document.getElementById('setting-mode').value = data.DefaultMode || 'transcribe';
    document.getElementById('setting-format').value = data.DefaultFormat || 'json';
    document.getElementById('setting-auto').checked = !!data.AutoTranslate;
    document.getElementById('setting-webhooks').value = (data.WebhookEndpoints || []).join('\n');
    document.getElementById('setting-cleanup').value = data.CleanupPrompt || flags.defaultCleanup;
  } catch (e) {
    document.getElementById('setting-cleanup').value = flags.defaultCleanup;
  }
}

function resetCleanupPrompt() {
  document.getElementById('setting-cleanup').value = flags.defaultCleanup;
}

async function saveSettings(e) {
  e.preventDefault();
  const payload = {
    DefaultModel: document.getElementById('setting-model').value,
    DefaultMode: document.getElementById('setting-mode').value,
    DefaultFormat: document.getElementById('setting-format').value,
    AutoTranslate: document.getElementById('setting-auto').checked,
    WebhookEndpoints: document.getElementById('setting-webhooks').value.split('\n').map((v) => v.trim()).filter(Boolean),
    CleanupPrompt: document.getElementById('setting-cleanup').value || flags.defaultCleanup,
  };
  await fetch('/api/settings', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload) });
}

function toggleTagFilter(tag) {
  state.tagFilter = state.tagFilter === tag ? null : tag;
  renderTagFilterPill();
  renderList();
}

function renderTagFilterPill() {
  const pill = document.getElementById('active-tag-filter');
  const hint = document.getElementById('active-tag-hint');
  if (state.tagFilter) {
    pill.textContent = `Filtering by tag: ${state.tagFilter}`;
    pill.className = 'badge tag';
    pill.onclick = () => toggleTagFilter(state.tagFilter);
    hint.classList.remove('hidden');
  } else {
    pill.textContent = '';
    pill.className = 'hidden';
    pill.onclick = null;
    hint.classList.add('hidden');
  }
}

function setupAdminVisibility() {
  if (flags.devMode) return;
  document.querySelectorAll('.admin-only').forEach((el) => {
    el.classList.add('hidden');
    el.setAttribute('aria-hidden', 'true');
  });
}

function buildSegments(text, durationSeconds) {
  if (!text) return [];
  const sentences = text
    .split(/(?<=[\.\!\?])\s+|\n+/)
    .map((s) => s.trim())
    .filter(Boolean);
  if (!sentences.length) return [];
  const duration = Number(durationSeconds) || sentences.length * 3;
  const approx = duration / sentences.length;
  let cursor = 0;
  return sentences.map((sentence, idx) => {
    const start = cursor;
    cursor += approx;
    return { id: `${idx}`, start, end: idx === sentences.length - 1 ? duration : cursor, text: sentence };
  });
}

function renderSegments(segments) {
  const list = document.getElementById('transcript-list');
  list.innerHTML = '';
  if (!segments.length) {
    list.innerHTML = '<p class="muted">No time-coded transcript available yet.</p>';
    return;
  }
  segments.forEach((seg) => {
    const btn = document.createElement('button');
    btn.className = 'segment';
    btn.type = 'button';
    btn.dataset.start = seg.start;
    btn.dataset.end = seg.end;
    btn.setAttribute('role', 'listitem');
    btn.innerHTML = `<span class="timestamp">[${formatTimestamp(seg.start)}]</span><span class="segment-text">${seg.text}</span>`;
    btn.addEventListener('click', () => {
      if (waveInstance && waveDuration) {
        waveInstance.seekTo(seg.start / waveDuration);
        waveInstance.play();
      }
    });
    btn.addEventListener('keydown', (e) => {
      if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault();
        btn.click();
      }
    });
    list.appendChild(btn);
  });
}

function renderAudioPlayer(data) {
  const container = document.getElementById('audio-player');
  container.innerHTML = '';
  if (!data.audio_url) {
    container.innerHTML = '<p class="muted">No audio URL available for this call.</p>';
    return;
  }
  const markup = `
    <div class="audio-shell" role="group" aria-label="Audio player for ${data.filename}">
      <div class="wave-row">
        <div id="waveform" class="waveform" aria-label="Waveform visualization"></div>
        <div id="waveform-loading" class="wave-loading">Loading audio…</div>
      </div>
      <div class="player-controls">
        <button id="play-toggle" class="primary" aria-label="Play">Play</button>
        <div class="time-display" aria-live="polite"><span id="current-time">00:00</span> / <span id="total-time">--:--</span></div>
        <label class="inline">
          Volume
          <input id="volume" type="range" min="0" max="1" step="0.05" value="0.8" aria-label="Volume" />
        </label>
        <label class="inline">
          Speed
          <select id="playback-rate" aria-label="Playback speed">
            <option value="0.75">0.75x</option>
            <option value="1" selected>1x</option>
            <option value="1.25">1.25x</option>
            <option value="1.5">1.5x</option>
          </select>
        </label>
        <a class="audio-link" href="${data.audio_url}" target="_blank" rel="noopener">Open audio file</a>
      </div>
      <div id="player-error" class="error hidden" role="alert">Player unavailable. Use the download link above.</div>
    </div>
  `;
  container.insertAdjacentHTML('afterbegin', markup);

  import('https://cdn.jsdelivr.net/npm/wavesurfer.js@7/dist/wavesurfer.esm.js')
    .then((mod) => {
      const WaveSurfer = mod.default;
      stopWaveform();
      waveInstance = WaveSurfer.create({
        container: '#waveform',
        url: data.audio_url,
        waveColor: '#374785',
        progressColor: '#2693ff',
        cursorColor: '#ffffff',
        height: 96,
        dragToSeek: true,
      });
      bindPlayerControls();
      waveInstance.on('ready', () => {
        waveDuration = waveInstance.getDuration();
        document.getElementById('waveform-loading').classList.add('hidden');
        document.getElementById('total-time').textContent = formatTimestamp(waveDuration);
        document.getElementById('current-time').textContent = '00:00';
      });
      waveInstance.on('audioprocess', handleWaveProgress);
      waveInstance.on('seek', handleWaveProgress);
      waveInstance.on('finish', () => {
        document.getElementById('play-toggle').textContent = 'Play';
        document.getElementById('play-toggle').setAttribute('aria-label', 'Play');
      });
      waveInstance.on('error', showWaveError);
    })
    .catch(() => {
      showWaveError();
    });
}

function handleWaveProgress() {
  if (!waveInstance) return;
  const t = waveInstance.getCurrentTime();
  document.getElementById('current-time').textContent = formatTimestamp(t);
  const now = performance.now();
  if (now - lastHighlightTs > 150) {
    highlightSegment(t);
    lastHighlightTs = now;
  }
}

function highlightSegment(time) {
  if (!state.segments.length) return;
  const active = state.segments.find((seg) => time >= seg.start && time < seg.end) || state.segments[state.segments.length - 1];
  document.querySelectorAll('.segment').forEach((btn) => {
    const match = Number(btn.dataset.start) === active.start;
    btn.classList.toggle('active', match);
    if (match) {
      btn.scrollIntoView({ block: 'nearest' });
    }
  });
}

function bindPlayerControls() {
  const playToggle = document.getElementById('play-toggle');
  const volume = document.getElementById('volume');
  const playbackRate = document.getElementById('playback-rate');
  playToggle.addEventListener('click', () => {
    if (!waveInstance) return;
    waveInstance.playPause();
    const playing = waveInstance.isPlaying();
    playToggle.textContent = playing ? 'Pause' : 'Play';
    playToggle.setAttribute('aria-label', playing ? 'Pause' : 'Play');
  });
  volume.addEventListener('input', (e) => {
    if (waveInstance) waveInstance.setVolume(Number(e.target.value));
  });
  playbackRate.addEventListener('change', (e) => {
    if (waveInstance) waveInstance.setPlaybackRate(Number(e.target.value));
  });
}

function stopWaveform() {
  if (waveInstance) {
    waveInstance.destroy();
    waveInstance = null;
  }
  waveDuration = 0;
}

function showWaveError() {
  document.getElementById('waveform-loading')?.classList.add('hidden');
  document.getElementById('player-error')?.classList.remove('hidden');
}

async function copyTranscriptToClipboard() {
  if (!state.selected) return;
  const text = state.selected.clean_transcript_text || state.selected.transcript_text || '';
  if (!text) return;
  await navigator.clipboard.writeText(text);
}

function downloadTranscriptText() {
  if (!state.selected) return;
  const text = state.selected.clean_transcript_text || state.selected.transcript_text || '';
  const blob = new Blob([text], { type: 'text/plain' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = `${state.selected.filename || 'transcript'}.txt`;
  a.click();
  URL.revokeObjectURL(url);
}

function toggleReviewed() {
  if (!state.selected) return;
  const reviewedBtn = document.getElementById('mark-reviewed');
  if (state.reviewed.has(state.selected.filename)) {
    state.reviewed.delete(state.selected.filename);
    reviewedBtn.classList.remove('active');
    reviewedBtn.setAttribute('aria-pressed', 'false');
  } else {
    state.reviewed.add(state.selected.filename);
    reviewedBtn.classList.add('active');
    reviewedBtn.setAttribute('aria-pressed', 'true');
  }
}

function exportCurrentView() {
  applyFilters();
  const rows = [
    ['filename', 'timestamp', 'source', 'type', 'tags', 'duration', 'status', 'preview'],
    ...state.filtered.map((item) => [
      item.filename,
      item.updated_at,
      item.source,
      item.call_type || '',
      (item.tags || []).join('|'),
      item.duration_seconds || '',
      item.status,
      buildSnippet(item).replace(/\n/g, ' '),
    ]),
  ];
  const csv = rows.map((r) => r.map((v) => `"${String(v || '').replace(/"/g, '""')}"`).join(',')).join('\n');
  const blob = new Blob([csv], { type: 'text/csv' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = 'transcriptions.csv';
  a.click();
  URL.revokeObjectURL(url);
}

function setupCollapsibles() {
  document.querySelectorAll('.panel .toggle-panel').forEach((btn) => {
    const panel = btn.closest('.panel');
    const body = panel.querySelector('.collapsible-body');
    const collapsed = panel.dataset.collapsed === 'true';
    body.classList.toggle('hidden', collapsed);
    btn.textContent = collapsed ? 'Expand' : 'Collapse';
    btn.setAttribute('aria-expanded', (!collapsed).toString());
    btn.addEventListener('click', () => {
      const isHidden = body.classList.toggle('hidden');
      panel.dataset.collapsed = isHidden.toString();
      btn.textContent = isHidden ? 'Expand' : 'Collapse';
      btn.setAttribute('aria-expanded', (!isHidden).toString());
    });
  });
}
