const state = {
  items: [],
  filtered: [],
  selected: null,
  activeTab: 'clean',
  tagFilter: null,
  callTypeFilter: [],
  tagFilters: [],
  dateFilter: '24h',
  durationFilter: '',
  sort: 'time-desc',
  page: 1,
  pageSize: 25,
  wavesurfer: null,
  segments: [],
  highlightedSegment: null,
  duration: 0,
  reviewed: new Set(),
};

const flags = {
  devMode: document.body.dataset.devMode === 'true',
  defaultCleanup: document.body.dataset.defaultCleanup || '',
};

const formatTime = (value) => {
  if (!value) return '';
  const d = new Date(value);
  return d.toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' });
};

const formatClock = (seconds) => {
  if (Number.isNaN(seconds) || !Number.isFinite(seconds)) return '--:--';
  const mins = Math.floor(seconds / 60)
    .toString()
    .padStart(2, '0');
  const secs = Math.floor(seconds % 60)
    .toString()
    .padStart(2, '0');
  return `${mins}:${secs}`;
};

const debounce = (fn, wait = 250) => {
  let t;
  return (...args) => {
    clearTimeout(t);
    t = setTimeout(() => fn(...args), wait);
  };
};

const renderTags = (tags = []) => {
  if (!tags.length) return '';
  return `<div class="tag-row">${tags
    .map((t) => `<button class="badge tag" data-tag="${t}" type="button">${t}</button>`)
    .join('')}</div>`;
};

const buildLocationLine = (item) => {
  const pieces = [item.town, item.agency].filter(Boolean);
  return pieces.join(' • ');
};

window.addEventListener('DOMContentLoaded', () => {
  document.getElementById('search').addEventListener('input', debounce(loadList, 200));
  document.getElementById('status-filter').addEventListener('change', loadList);
  document.getElementById('source-filter').addEventListener('change', renderList);
  document.getElementById('date-filter').addEventListener('change', handleFilterChange);
  document.getElementById('type-filter').addEventListener('change', handleFilterChange);
  document.getElementById('tag-filter').addEventListener('change', handleFilterChange);
  document.getElementById('duration-filter').addEventListener('change', handleFilterChange);
  document.getElementById('sort-order').addEventListener('change', handleSortChange);
  document.getElementById('page-size').addEventListener('change', handlePageSizeChange);
  document.getElementById('refresh-btn').addEventListener('click', () => {
    loadList();
    loadStatus();
  });
  document.getElementById('retry-status').addEventListener('click', loadStatus);
  document.getElementById('prev-page').addEventListener('click', () => updatePage(-1));
  document.getElementById('next-page').addEventListener('click', () => updatePage(1));
  document.getElementById('export-btn').addEventListener('click', exportCsv);
  document.getElementById('settings-form').addEventListener('submit', saveSettings);
  document.getElementById('reset-cleanup').addEventListener('click', resetCleanupPrompt);
  document.getElementById('trigger-backfill').addEventListener('click', runBackfill);
  document.getElementById('play-btn').addEventListener('click', togglePlay);
  document.getElementById('volume-slider').addEventListener('input', handleVolume);
  document.getElementById('speed-select').addEventListener('change', handleSpeed);
  document.getElementById('copy-transcript').addEventListener('click', copyTranscript);
  document.getElementById('download-transcript').addEventListener('click', downloadTranscript);
  document.getElementById('mark-reviewed').addEventListener('click', toggleReviewed);
  setupTabs();
  setupCollapsibles();
  setupAdminVisibility();
  renderTagFilterPill();
  loadList();
  loadStatus();
  loadSettings();
});

async function loadList() {
  const search = document.getElementById('search').value.trim();
  const status = document.getElementById('status-filter').value;
  const url = new URL('/api/transcriptions', window.location.origin);
  if (search) url.searchParams.set('q', search);
  if (status) url.searchParams.set('status', status);
  url.searchParams.set('sort', 'time');
  url.searchParams.set('page', '1');
  url.searchParams.set('page_size', '250');
  state.page = 1;
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

function handleFilterChange() {
  const typeSelect = document.getElementById('type-filter');
  const tagSelect = document.getElementById('tag-filter');
  state.callTypeFilter = Array.from(typeSelect.selectedOptions).map((o) => o.value).filter(Boolean);
  state.tagFilters = Array.from(tagSelect.selectedOptions).map((o) => o.value).filter(Boolean);
  state.dateFilter = document.getElementById('date-filter').value;
  state.durationFilter = document.getElementById('duration-filter').value;
  state.page = 1;
  renderList();
}

function handleSortChange() {
  state.sort = document.getElementById('sort-order').value;
  renderList();
}

function handlePageSizeChange() {
  state.pageSize = Number(document.getElementById('page-size').value) || 25;
  state.page = 1;
  renderList();
}

function updatePage(delta) {
  const totalPages = Math.max(1, Math.ceil(state.filtered.length / state.pageSize));
  state.page = Math.min(totalPages, Math.max(1, state.page + delta));
  renderList();
}

function renderList() {
  const listEl = document.getElementById('list');
  listEl.innerHTML = '';
  const sourceFilter = document.getElementById('source-filter').value;
  const now = Date.now();
  const filtered = state.items.filter((item) => {
    if (sourceFilter && item.source !== sourceFilter) return false;
    if (state.tagFilter) {
      const tags = Array.isArray(item.tags) ? item.tags : [];
      if (!tags.includes(state.tagFilter)) return false;
    }
    if (state.callTypeFilter.length && !state.callTypeFilter.includes(item.call_type)) return false;
    if (state.tagFilters.length) {
      const tags = Array.isArray(item.tags) ? item.tags : [];
      if (!state.tagFilters.some((tag) => tags.includes(tag))) return false;
    }
    if (state.durationFilter) {
      const dur = Number(item.duration_seconds) || 0;
      if (state.durationFilter === 'short' && dur >= 60) return false;
      if (state.durationFilter === 'medium' && (dur < 60 || dur > 300)) return false;
      if (state.durationFilter === 'long' && dur <= 300) return false;
    }
    if (state.dateFilter !== 'all' && item.updated_at) {
      const updated = new Date(item.updated_at).getTime();
      const day = 24 * 60 * 60 * 1000;
      if (state.dateFilter === 'today') {
        const start = new Date();
        start.setHours(0, 0, 0, 0);
        if (updated < start.getTime()) return false;
      } else if (state.dateFilter === '24h' && updated < now - day) {
        return false;
      } else if (state.dateFilter === '7d' && updated < now - day * 7) {
        return false;
      }
    }
    return true;
  });

  filtered.sort((a, b) => {
    if (state.sort === 'duration') return (b.duration_seconds || 0) - (a.duration_seconds || 0);
    if (state.sort === 'status') return String(a.status || '').localeCompare(String(b.status || ''));
    if (state.sort === 'source') return String(a.source || '').localeCompare(String(b.source || ''));
    const aTime = new Date(a.updated_at || a.created_at || 0).getTime();
    const bTime = new Date(b.updated_at || b.created_at || 0).getTime();
    return state.sort === 'time-asc' ? aTime - bTime : bTime - aTime;
  });

  state.filtered = filtered;
  if (!state.filtered.length) {
    document.getElementById('empty-state').classList.remove('hidden');
    document.getElementById('pagination').classList.add('hidden');
    return;
  }
  document.getElementById('empty-state').classList.add('hidden');

  const totalPages = Math.max(1, Math.ceil(state.filtered.length / state.pageSize));
  state.page = Math.min(state.page, totalPages);
  const start = (state.page - 1) * state.pageSize;
  const currentPage = state.filtered.slice(start, start + state.pageSize);

  currentPage.forEach((item) => {
    const card = document.createElement('div');
    card.className = 'call-card';
    card.role = 'listitem';
    const snippet = buildSnippet(item);
    const title = item.pretty_title || item.filename;
    const location = buildLocationLine(item);
    const durationBadge = item.duration_seconds
      ? `<span class="badge duration" aria-label="Duration">${formatClock(item.duration_seconds)}</span>`
      : '';
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
          ${durationBadge}
        </div>
      </div>
      <p class="snippet">${snippet}</p>
      <div class="meta">
        <span>${formatTime(item.updated_at)}</span>
        ${item.call_type ? `<span>${item.call_type}</span>` : ''}
      </div>
    `;
    card.addEventListener('click', () => selectItem(item));
    card.classList.toggle('active', state.selected && state.selected.filename === item.filename);
    card.querySelectorAll('.badge.tag').forEach((tagBtn) => {
      tagBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        toggleTagFilter(tagBtn.dataset.tag);
      });
    });
    listEl.appendChild(card);
  });

  document.getElementById('pagination').classList.remove('hidden');
  document.getElementById('page-indicator').textContent = `Page ${state.page} of ${totalPages}`;
  document.getElementById('prev-page').disabled = state.page === 1;
  document.getElementById('next-page').disabled = state.page === totalPages;
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
  try {
    const res = await fetch(`/api/transcription/${encodeURIComponent(item.filename)}`);
    if (!res.ok) throw new Error('failed');
    const data = await res.json();
    state.selected = data;
    renderDetail(data);
    renderList();
  } catch (e) {
    document.getElementById('detail-meta').textContent = 'Unable to load transcription details.';
    teardownPlayer();
  }
}

function renderDetail(data) {
  document.getElementById('detail-title').textContent = data.pretty_title || data.filename;
  const metaParts = [];
  if (data.duration_seconds) metaParts.push(`${data.duration_seconds.toFixed(1)}s`);
  if (data.size_bytes) metaParts.push(`${(data.size_bytes / 1024 / 1024).toFixed(2)} MB`);
  metaParts.push(formatTime(data.updated_at));
  document.getElementById('detail-meta').textContent = metaParts.filter(Boolean).join(' • ');
  const location = buildLocationLine(data);
  document.getElementById('detail-location').textContent = location || data.filename;
  const audioLink = document.getElementById('detail-audio');
  if (data.audio_url) {
    audioLink.href = data.audio_url;
    audioLink.classList.remove('hidden');
  } else {
    audioLink.classList.add('hidden');
  }
  renderBadges(data);
  setActiveTab(state.activeTab);
  renderTranscript(state.activeTab);
  initPlayer(data);
  syncReviewedButton();
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
      renderTranscript(btn.dataset.tab);
    });
  });
}

function setActiveTab(tab) {
  document.querySelectorAll('.transcript-tabs button').forEach((btn) => {
    btn.classList.toggle('active', btn.dataset.tab === tab);
  });
}

function buildSegmentsFromText(text, durationSeconds = 0) {
  if (!text) return [];
  const sentences = text
    .split(/(?<=[.!?])\s+/)
    .map((s) => s.trim())
    .filter(Boolean);
  const approxDuration = durationSeconds || Math.max(sentences.length * 3, 5);
  const segmentDuration = approxDuration / Math.max(sentences.length, 1);
  let cursor = 0;
  return sentences.map((s) => {
    const seg = { start: cursor, end: cursor + segmentDuration, text: s };
    cursor += segmentDuration;
    return seg;
  });
}

function getSegmentsForTab(data, tab) {
  if (!data) return [];
  const text = tab === 'raw'
    ? data.raw_transcript_text || data.transcript_text
    : tab === 'translation'
      ? data.translation_text
      : data.clean_transcript_text || data.transcript_text;
  if (Array.isArray(data.segments) && data.segments.length) return data.segments;
  if (Array.isArray(data.transcript_segments) && data.transcript_segments.length) return data.transcript_segments;
  return buildSegmentsFromText(text, data.duration_seconds || state.duration);
}

function renderTranscript(tab) {
  const pre = document.getElementById('transcript-text');
  const list = document.getElementById('transcript-list');
  const emptyHint = document.getElementById('transcript-empty');
  const data = state.selected;
  if (!data) {
    pre.textContent = 'No call selected.';
    list.innerHTML = '';
    emptyHint.classList.remove('hidden');
    return;
  }
  if (tab === 'raw' || tab === 'translation') {
    state.segments = [];
    state.highlightedSegment = null;
    const text = tab === 'raw'
      ? data.raw_transcript_text || data.transcript_text || ''
      : data.translation_text || 'No translation available yet.';
    pre.textContent = text || `Status: ${data.status}`;
    pre.classList.remove('hidden');
    list.classList.add('hidden');
    emptyHint.classList.add('hidden');
    return;
  }

  state.segments = getSegmentsForTab(data, tab);
  state.highlightedSegment = null;
  list.innerHTML = '';
  if (!state.segments.length) {
    emptyHint.classList.remove('hidden');
    pre.classList.remove('hidden');
    pre.textContent = 'Transcript not available yet.';
    list.classList.add('hidden');
    return;
  }

  emptyHint.classList.add('hidden');
  pre.classList.add('hidden');
  list.classList.remove('hidden');
  state.segments.forEach((seg, idx) => {
    const item = document.createElement('button');
    item.type = 'button';
    item.className = 'segment';
    item.dataset.index = idx;
    item.dataset.start = seg.start;
    item.dataset.end = seg.end;
    item.setAttribute('role', 'listitem');
    item.innerHTML = `<span class="segment-time">[${formatClock(seg.start)}]</span><span class="segment-text">${seg.text}</span>`;
    item.addEventListener('click', () => seekToSegment(seg));
    list.appendChild(item);
  });
}

function seekToSegment(seg) {
  if (!state.wavesurfer || !seg) return;
  const duration = state.wavesurfer.getDuration();
  const target = duration ? seg.start / duration : 0;
  state.wavesurfer.seekTo(Math.min(1, target));
  state.wavesurfer.play();
}

async function initPlayer(data) {
  teardownPlayer();
  const statusEl = document.getElementById('player-status');
  const fallback = document.getElementById('audio-fallback');
  statusEl.textContent = data.audio_url ? 'Loading audio…' : 'Audio not available for this call.';
  fallback.classList.toggle('hidden', !!data.audio_url);
  if (!data.audio_url) return;

  const waveformEl = document.getElementById('waveform');
  waveformEl.innerHTML = '';
  try {
    const { default: WaveSurfer } = await import('https://cdn.jsdelivr.net/npm/wavesurfer.js@7/dist/wavesurfer.esm.js');
    state.wavesurfer = WaveSurfer.create({
      container: waveformEl,
      url: data.audio_url,
      waveColor: '#374785',
      progressColor: '#2693ff',
      cursorColor: '#ffffff',
      height: 96,
      barWidth: 2,
      normalize: true,
    });
    state.wavesurfer.on('ready', () => {
      state.duration = state.wavesurfer.getDuration();
      const vol = Number(document.getElementById('volume-slider').value) || 1;
      state.wavesurfer.setVolume(vol);
      document.getElementById('time-display').textContent = `${formatClock(0)} / ${formatClock(state.duration)}`;
      statusEl.textContent = 'Ready';
    });
    state.wavesurfer.on('audioprocess', handleTimeUpdate);
    state.wavesurfer.on('seek', () => handleTimeUpdate(state.wavesurfer.getCurrentTime()));
    state.wavesurfer.on('error', () => {
      statusEl.textContent = 'Player unavailable. Use download link below.';
      fallback.classList.remove('hidden');
    });
    state.wavesurfer.on('finish', () => {
      document.getElementById('play-btn').textContent = 'Play';
      document.getElementById('play-btn').setAttribute('aria-label', 'Play');
      handleTimeUpdate(state.duration);
    });
  } catch (err) {
    statusEl.textContent = 'Player unavailable. Use download link below.';
    fallback.classList.remove('hidden');
  }
}

function teardownPlayer() {
  if (state.wavesurfer) {
    state.wavesurfer.destroy();
    state.wavesurfer = null;
  }
  state.duration = 0;
  state.highlightedSegment = null;
  document.getElementById('time-display').textContent = `${formatClock(0)} / ${formatClock(0)}`;
  document.getElementById('player-status').textContent = 'Player idle';
  const btn = document.getElementById('play-btn');
  btn.textContent = 'Play';
  btn.setAttribute('aria-label', 'Play');
}

function togglePlay() {
  if (!state.wavesurfer) return;
  state.wavesurfer.playPause();
  const btn = document.getElementById('play-btn');
  const playing = state.wavesurfer.isPlaying();
  btn.textContent = playing ? 'Pause' : 'Play';
  btn.setAttribute('aria-label', playing ? 'Pause audio' : 'Play audio');
}

function handleVolume(e) {
  if (!state.wavesurfer) return;
  const volume = Number(e.target.value);
  state.wavesurfer.setVolume(volume);
}

function handleSpeed(e) {
  if (!state.wavesurfer) return;
  const rate = Number(e.target.value);
  state.wavesurfer.setPlaybackRate(rate);
}

function handleTimeUpdate(currentTime = 0) {
  document.getElementById('time-display').textContent = `${formatClock(currentTime)} / ${formatClock(state.duration)}`;
  highlightActiveSegment(currentTime);
}

function highlightActiveSegment(currentTime) {
  if (!state.segments.length || state.activeTab !== 'clean') return;
  const idx = state.segments.findIndex((seg, i) => {
    const isLast = i === state.segments.length - 1;
    return currentTime >= seg.start && (currentTime < seg.end || (isLast && currentTime >= seg.end));
  });
  if (idx === -1 || idx === state.highlightedSegment) return;
  state.highlightedSegment = idx;
  document.querySelectorAll('#transcript-list .segment').forEach((el, i) => {
    const active = i === idx;
    el.classList.toggle('active', active);
    if (active) {
      el.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
    }
  });
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

function setupCollapsibles() {
  document.querySelectorAll('.toggle-panel').forEach((btn) => {
    btn.addEventListener('click', () => {
      const target = document.getElementById(btn.dataset.target);
      if (!target) return;
      const nowCollapsed = target.classList.toggle('collapsed');
      btn.textContent = nowCollapsed ? 'Expand' : 'Collapse';
      btn.setAttribute('aria-expanded', nowCollapsed ? 'false' : 'true');
    });
  });
}

function copyTranscript() {
  if (!state.selected) return;
  const text = state.selected.clean_transcript_text || state.selected.transcript_text || '';
  if (!text) return;
  navigator.clipboard.writeText(text).then(() => {
    document.getElementById('copy-transcript').textContent = 'Copied!';
    setTimeout(() => (document.getElementById('copy-transcript').textContent = 'Copy transcript'), 1200);
  });
}

function downloadTranscript() {
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
  if (state.reviewed.has(state.selected.filename)) {
    state.reviewed.delete(state.selected.filename);
  } else {
    state.reviewed.add(state.selected.filename);
  }
  syncReviewedButton();
}

function syncReviewedButton() {
  const btn = document.getElementById('mark-reviewed');
  const isReviewed = state.selected && state.reviewed.has(state.selected.filename);
  btn.classList.toggle('active', !!isReviewed);
  btn.textContent = isReviewed ? 'Reviewed' : 'Mark reviewed';
  btn.setAttribute('aria-pressed', isReviewed ? 'true' : 'false');
}

function exportCsv() {
  const rows = state.filtered.length ? state.filtered : state.items;
  if (!rows.length) return;
  const headers = ['filename', 'timestamp', 'source', 'type', 'tags', 'duration', 'status', 'preview'];
  const csv = [headers.join(',')].concat(
    rows.map((r) => [
      r.filename,
      r.updated_at,
      r.source,
      r.call_type,
      (Array.isArray(r.tags) ? r.tags.join('|') : ''),
      r.duration_seconds || '',
      r.status,
      JSON.stringify(buildSnippet(r)),
    ].join(',')),
  );
  const blob = new Blob([csv.join('\n')], { type: 'text/csv' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = 'transcriptions.csv';
  a.click();
  URL.revokeObjectURL(url);
}
