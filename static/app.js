const state = {
  items: [],
  filtered: [],
  selected: null,
  activeTab: 'clean',
  tagFilter: '',
  window: '24h',
  wave: null,
  wordCount: 0,
};

const flags = {
  devMode: document.body.dataset.devMode === 'true',
};

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

const buildLocationLine = (item) => {
  const pieces = [item.town, item.agency].filter(Boolean);
  return pieces.join(' • ');
};

window.addEventListener('DOMContentLoaded', () => {
  document.getElementById('search').addEventListener('input', debounce(loadList, 200));
  document.getElementById('status-filter').addEventListener('change', loadList);
  document.getElementById('tag-filter').addEventListener('change', () => {
    state.tagFilter = document.getElementById('tag-filter').value;
    loadList();
  });
  document.querySelectorAll('.window-switch .chip').forEach((btn) => {
    btn.addEventListener('click', () => {
      document.querySelectorAll('.window-switch .chip').forEach((b) => b.classList.remove('active'));
      btn.classList.add('active');
      state.window = btn.dataset.window;
      updateWindowCaption();
      loadList();
    });
  });
  document.getElementById('refresh-btn').addEventListener('click', () => { loadList(); loadStatus(); });
  document.getElementById('retry-status').addEventListener('click', loadStatus);
  document.getElementById('play-toggle').addEventListener('click', togglePlayback);
  setupTabs();
  loadList();
  loadStatus();
});

function updateWindowCaption() {
  const copy = {
    '24h': 'Showing the latest 24h by default.',
    '7d': 'Showing the last 7 days.',
    '30d': 'Showing the last 30 days.',
  };
  document.getElementById('list-caption').textContent = copy[state.window] || copy['24h'];
}

async function loadList() {
  const search = document.getElementById('search').value.trim();
  const status = document.getElementById('status-filter').value;
  const url = new URL('/api/transcriptions', window.location.origin);
  if (search) url.searchParams.set('q', search);
  if (status) url.searchParams.set('status', status);
  if (state.tagFilter) url.searchParams.set('tag', state.tagFilter);
  url.searchParams.set('window', state.window || '24h');
  url.searchParams.set('sort', 'time');
  url.searchParams.set('page', '1');
  url.searchParams.set('page_size', '150');
  toggleError(false);
  try {
    const res = await fetch(url);
    if (!res.ok) throw new Error('request failed');
    state.items = await res.json();
    renderTagOptions();
    renderList();
    renderAnalytics();
  } catch (e) {
    toggleError(true);
  }
}

function renderTagOptions() {
  const select = document.getElementById('tag-filter');
  const tags = new Set();
  state.items.forEach((item) => (item.tags || []).forEach((t) => tags.add(t)));
  const current = state.tagFilter;
  select.innerHTML = '<option value="">All tags</option>';
  Array.from(tags)
    .sort()
    .forEach((tag) => {
      const opt = document.createElement('option');
      opt.value = tag;
      opt.textContent = tag;
      if (tag === current) opt.selected = true;
      select.appendChild(opt);
    });
}

function renderList() {
  const listEl = document.getElementById('list');
  listEl.innerHTML = '';
  state.filtered = state.items;
  if (!state.filtered.length) {
    document.getElementById('empty-state').classList.remove('hidden');
    return;
  }
  document.getElementById('empty-state').classList.add('hidden');

  state.filtered.forEach((item) => {
    const card = document.createElement('div');
    card.className = 'call-card';
    card.role = 'listitem';
    const snippet = buildSnippet(item);
    const title = item.pretty_title || item.filename;
    const location = buildLocationLine(item);
    card.innerHTML = `
      <div class="card-header">
        <div>
          <div class="filename">${title}</div>
          ${location ? `<p class="muted tight">${location}</p>` : ''}
          ${renderTags(item.tags)}
        </div>
        <div class="badge-row">
          ${renderStatusBadge(item.status)}
        </div>
      </div>
      <p class="snippet">${snippet}</p>
      <div class="meta">
        <span>${formatTime(item.updated_at)}</span>
        ${item.call_type ? `<span>${item.call_type}</span>` : ''}
      </div>
    `;
    card.addEventListener('click', () => selectItem(item));
    listEl.appendChild(card);
  });
}

function renderTags(tags = []) {
  if (!tags || !tags.length) return '';
  return `<div class="tag-row">${tags.map((t) => `<span class="badge tag">${t}</span>`).join('')}</div>`;
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

async function selectItem(item) {
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
    setupWaveform(data);
    updateTranscript(state.activeTab);
  } catch (e) {
    document.getElementById('detail-meta').textContent = 'Unable to load transcription details.';
  }
}

function renderBadges(data) {
  const wrap = document.getElementById('detail-badges');
  wrap.innerHTML = '';
  wrap.insertAdjacentHTML('beforeend', renderStatusBadge(data.status));
  if (data.call_type) wrap.insertAdjacentHTML('beforeend', `<span class="badge">${data.call_type}</span>`);
  const tagHtml = renderTags(data.tags);
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

function setActiveTab(name) {
  document.querySelectorAll('.transcript-tabs button').forEach((btn) => {
    btn.classList.toggle('active', btn.dataset.tab === name);
  });
}

function updateTranscript(tab) {
  if (!state.selected) return;
  let text = '';
  if (tab === 'raw') text = state.selected.raw_transcript_text || '';
  else if (tab === 'translation') text = state.selected.translation_text || '';
  else text = state.selected.clean_transcript_text || state.selected.transcript_text || '';
  const pre = document.getElementById('transcript-text');
  if (!text) {
    pre.textContent = 'Transcript not available yet.';
    state.wordCount = 0;
    return;
  }
  const words = text.split(/(\s+)/);
  state.wordCount = words.filter((w) => w.trim()).length;
  const wrapped = words
    .map((w, idx) => (w.trim() ? `<span class="word" data-idx="${idx}">${w}</span>` : w))
    .join('');
  pre.innerHTML = wrapped;
}

function setupWaveform(data) {
  const wrap = document.getElementById('wave-wrap');
  if (!data.audio_url) {
    wrap.classList.add('hidden');
    return;
  }
  wrap.classList.remove('hidden');
  const link = document.getElementById('detail-audio');
  link.href = data.audio_url;
  if (!state.wave) {
    state.wave = WaveSurfer.create({
      container: '#waveform',
      waveColor: '#5bd0ff',
      progressColor: '#3fb2e2',
      height: 72,
      normalize: true,
    });
    state.wave.on('audioprocess', syncHighlight);
    state.wave.on('seek', syncHighlight);
  }
  state.wave.load(data.audio_url);
}

function togglePlayback() {
  if (!state.wave) return;
  state.wave.playPause();
}

function syncHighlight() {
  if (!state.wave || !state.selected || state.wordCount === 0) return;
  const duration = state.wave.getDuration();
  const position = state.wave.getCurrentTime();
  const ratio = duration ? position / duration : 0;
  const words = document.querySelectorAll('#transcript-text .word');
  const activeCount = Math.floor(words.length * ratio);
  words.forEach((el, idx) => {
    el.classList.toggle('active', idx <= activeCount);
  });
}

function toggleError(show) {
  document.getElementById('list-error').classList.toggle('hidden', !show);
}

async function loadStatus() {
  try {
    const res = await fetch('/debug/queue');
    if (!res.ok) throw new Error('bad status');
    const data = await res.json();
    document.getElementById('queue-level').textContent = data.length;
    document.getElementById('queue-capacity').textContent = `/ ${data.capacity}`;
    document.getElementById('worker-count').textContent = data.workers;
    document.getElementById('stat-processed').textContent = data.processed_jobs;
  } catch (e) {
    document.getElementById('queue-level').textContent = '--';
  }
}

function renderAnalytics() {
  renderTagCloud();
  renderStatusPlot();
  renderTagPlot();
}

function renderTagCloud() {
  const container = document.getElementById('tag-cloud');
  container.innerHTML = '';
  const tags = new Map();
  state.filtered.forEach((item) => {
    (item.tags || []).forEach((tag) => {
      tags.set(tag, (tags.get(tag) || 0) + 1);
    });
  });
  Array.from(tags.entries())
    .sort((a, b) => b[1] - a[1])
    .slice(0, 12)
    .forEach(([tag, count]) => {
      const chip = document.createElement('button');
      chip.className = 'chip ghost';
      chip.textContent = `${tag} (${count})`;
      chip.addEventListener('click', () => {
        state.tagFilter = tag;
        document.getElementById('tag-filter').value = tag;
        loadList();
      });
      container.appendChild(chip);
    });
}

function renderStatusPlot() {
  if (typeof Plotly === 'undefined') return;
  const counts = {};
  state.filtered.forEach((item) => {
    counts[item.status || 'queued'] = (counts[item.status || 'queued'] || 0) + 1;
  });
  const labels = Object.keys(counts);
  const values = labels.map((l) => counts[l]);
  Plotly.newPlot('plot-status', [{ type: 'bar', x: labels, y: values, marker: { color: '#5bd0ff' } }], {
    paper_bgcolor: 'transparent',
    plot_bgcolor: 'transparent',
    margin: { t: 10, b: 30, l: 30, r: 10 },
    xaxis: { title: 'Status' },
    yaxis: { title: 'Count' },
    font: { color: '#e8edf7' },
  }, { displayModeBar: false, responsive: true });
}

function renderTagPlot() {
  if (typeof Plotly === 'undefined') return;
  const counts = {};
  state.filtered.forEach((item) => {
    (item.tags || []).forEach((tag) => {
      counts[tag] = (counts[tag] || 0) + 1;
    });
  });
  const entries = Object.entries(counts).sort((a, b) => b[1] - a[1]).slice(0, 6);
  if (!entries.length) {
    Plotly.purge('plot-tags');
    return;
  }
  const labels = entries.map((e) => e[0]);
  const values = entries.map((e) => e[1]);
  Plotly.newPlot('plot-tags', [{ type: 'pie', labels, values, hole: 0.55 }], {
    paper_bgcolor: 'transparent',
    plot_bgcolor: 'transparent',
    margin: { t: 0, b: 0, l: 0, r: 0 },
    font: { color: '#e8edf7' },
    showlegend: true,
  }, { displayModeBar: false, responsive: true });
}
