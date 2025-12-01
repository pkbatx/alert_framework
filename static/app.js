const state = {
  items: [],
  filtered: [],
  selected: null,
  activeTab: 'clean',
  tagFilter: null,
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

const debounce = (fn, wait = 250) => {
  let t;
  return (...args) => {
    clearTimeout(t);
    t = setTimeout(() => fn(...args), wait);
  };
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
  document.getElementById('search').addEventListener('input', debounce(loadList, 200));
  document.getElementById('status-filter').addEventListener('change', loadList);
  document.getElementById('source-filter').addEventListener('change', renderList);
  document.getElementById('refresh-btn').addEventListener('click', () => { loadList(); loadStatus(); });
  document.getElementById('retry-status').addEventListener('click', loadStatus);
  document.getElementById('settings-form').addEventListener('submit', saveSettings);
  document.getElementById('reset-cleanup').addEventListener('click', resetCleanupPrompt);
  document.getElementById('trigger-backfill').addEventListener('click', runBackfill);
  setupTabs();
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
  const sourceFilter = document.getElementById('source-filter').value;
  state.filtered = state.items.filter((item) => {
    if (sourceFilter && item.source !== sourceFilter) return false;
    if (state.tagFilter) {
      const tags = Array.isArray(item.tags) ? item.tags : [];
      if (!tags.includes(state.tagFilter)) return false;
    }
    return true;
  });

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
          ${renderSourceBadge(item.source)}
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
    listEl.appendChild(card);
  });
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
    updateTranscript(state.activeTab);
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
  const data = state.selected;
  if (!data) {
    pre.textContent = 'No call selected.';
    return;
  }
  let text = '';
  if (tab === 'raw') text = data.raw_transcript_text || data.transcript_text || '';
  else if (tab === 'translation') text = data.translation_text || 'No translation available yet.';
  else text = data.clean_transcript_text || data.transcript_text || '';
  pre.textContent = text || `Status: ${data.status}`;
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
