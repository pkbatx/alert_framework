(() => {
  const listEl = document.getElementById('call-list');
  const emptyState = document.getElementById('empty-state');
  const totalCount = document.getElementById('total-count');
  const searchInput = document.getElementById('search');
  const statusSelect = document.getElementById('status');
  const refreshBtn = document.getElementById('refresh');
  const windowButtons = document.querySelectorAll('.window-switcher button');
  const regenBtn = document.getElementById('regen');
  const playBtn = document.getElementById('play');
  const downloadLink = document.getElementById('download');
  const waveformEl = document.getElementById('waveform');
  const transcriptEl = document.getElementById('transcript-text');
  const highlightToggle = document.getElementById('highlight-toggle');
  const detailTitle = document.getElementById('detail-title');
  const detailMeta = document.getElementById('detail-meta');
  const detailStatus = document.getElementById('detail-status');
  const detailType = document.getElementById('detail-type');
  const detailAgency = document.getElementById('detail-agency');
  const detailTags = document.getElementById('detail-tags');
  const mapChart = document.getElementById('map-chart');
  const updatedAtEl = document.getElementById('updated-at');
  const tagFilterEl = document.getElementById('tag-filter');

  const state = {
    window: '24h',
    search: '',
    status: '',
    tagFilter: [],
    calls: [],
    stats: null,
    selected: null,
    wavesurfer: null,
    segments: [],
    mapboxToken: '',
  };

  function setWindow(next) {
    state.window = next;
    windowButtons.forEach((btn) => btn.classList.toggle('active', btn.dataset.window === next));
    fetchCalls();
  }

  function formatDate(value) {
    if (!value) return '—';
    const dt = new Date(value);
    return dt.toLocaleString();
  }

  function renderTags(container, tags, clickable = false) {
    container.innerHTML = '';
    tags.forEach((tag) => {
      const pill = document.createElement('span');
      pill.className = 'tag';
      pill.textContent = tag;
      if (clickable) {
        const active = state.tagFilter.includes(tag.toLowerCase());
        pill.classList.toggle('active', active);
        pill.addEventListener('click', () => toggleTag(tag));
      }
      container.appendChild(pill);
    });
  }

  function toggleTag(tag) {
    const normalized = tag.toLowerCase();
    if (state.tagFilter.includes(normalized)) {
      state.tagFilter = state.tagFilter.filter((t) => t !== normalized);
    } else {
      state.tagFilter.push(normalized);
    }
    fetchCalls();
  }

  function buildCard(call) {
    const card = document.createElement('article');
    card.className = 'call-card';
    card.tabIndex = 0;
    card.setAttribute('role', 'listitem');
    card.innerHTML = `
      <div class="title">${call.pretty_title || call.filename}</div>
      <div class="meta">
        <span>${formatDate(call.call_timestamp)}</span>
        <span class="badge ${call.status}">${call.status}</span>
      </div>
    `;
    card.addEventListener('click', () => selectCall(call));
    card.addEventListener('keydown', (evt) => {
      if (evt.key === 'Enter' || evt.key === ' ') {
        evt.preventDefault();
        selectCall(call);
      }
    });
    if (state.selected && state.selected.filename === call.filename) {
      card.classList.add('active');
    }
    return card;
  }

  function renderList() {
    listEl.innerHTML = '';
    if (!state.calls.length) {
      emptyState.classList.remove('hidden');
      totalCount.textContent = '';
      return;
    }
    emptyState.classList.add('hidden');
    state.calls.forEach((call) => listEl.appendChild(buildCard(call)));
    totalCount.textContent = `${state.calls.length} calls`;
  }

  function formatTimecode(seconds = 0) {
    const total = Math.max(0, Math.floor(seconds));
    const mins = String(Math.floor(total / 60)).padStart(2, '0');
    const secs = String(total % 60).padStart(2, '0');
    return `${mins}:${secs}`;
  }

  function normalizeSegments(call) {
    const rawSegments = Array.isArray(call.segments) ? call.segments : [];
    const cleaned = rawSegments
      .map((seg) => ({
        start: Number(seg.start) || 0,
        end: Number(seg.end) || (Number(seg.start) || 0) + 0.5,
        text: (seg.text || '').trim(),
        speaker: seg.speaker || '',
      }))
      .filter((seg) => seg.text && seg.end > seg.start);
    if (cleaned.length) return cleaned;

    const text = (call.clean_transcript_text || call.raw_transcript_text || call.transcript_text || '').trim();
    if (!text) return [];
    const duration = Number(call.duration_seconds) || 0;
    return [
      {
        start: 0,
        end: duration > 0 ? duration : Math.max(1, text.split(/\s+/).length * 0.5),
        text,
      },
    ];
  }

  function renderTranscript(call) {
    state.segments = normalizeSegments(call);
    transcriptEl.innerHTML = '';
    transcriptEl.classList.toggle('playing', highlightToggle.checked && state.wavesurfer && state.wavesurfer.isPlaying());
    if (!state.segments.length) {
      const placeholder = document.createElement('p');
      placeholder.className = 'muted';
      placeholder.textContent = 'Transcript pending.';
      transcriptEl.appendChild(placeholder);
      return;
    }
    state.segments.forEach((segment) => {
      const row = document.createElement('div');
      row.className = 'transcript-segment';
      row.dataset.start = segment.start;
      row.dataset.end = segment.end;
      const ts = document.createElement('span');
      ts.className = 'timestamp';
      ts.textContent = formatTimecode(segment.start);
      const text = document.createElement('span');
      text.className = 'text';
      text.textContent = segment.text;
      row.appendChild(ts);
      row.appendChild(text);
      row.addEventListener('click', () => {
        if (state.wavesurfer) {
          state.wavesurfer.setTime(segment.start);
          state.wavesurfer.play();
          updateTranscriptHighlight(segment.start);
        }
      });
      transcriptEl.appendChild(row);
    });
    updateTranscriptHighlight(0);
  }

  function updateTranscriptHighlight(time) {
    const rows = transcriptEl.querySelectorAll('.transcript-segment');
    rows.forEach((row) => row.classList.remove('active'));
    if (!highlightToggle.checked || time === null || !rows.length) return;
    const active = Array.from(rows).find((row) => {
      const start = Number(row.dataset.start) || 0;
      const end = Number(row.dataset.end) || start;
      return time >= start && time < end;
    });
    if (active) {
      active.classList.add('active');
      active.scrollIntoView({ block: 'nearest' });
    }
  }

  function destroyWave() {
    if (state.wavesurfer) {
      state.wavesurfer.destroy();
      state.wavesurfer = null;
    }
    waveformEl.innerHTML = '';
  }

  function createWave(url) {
    destroyWave();
    const ws = WaveSurfer.create({
      container: waveformEl,
      waveColor: '#7ce7ff',
      progressColor: '#5ef5a4',
      height: 80,
      cursorWidth: 1,
      backend: 'MediaElement',
      mediaControls: false,
      removeMediaElementOnDestroy: true,
      normalize: true,
    });
    const handleTime = () => updateTranscriptHighlight(ws.getCurrentTime());
    ws.on('ready', () => {
      playBtn.removeAttribute('disabled');
      handleTime();
    });
    ws.on('audioprocess', handleTime);
    ws.on('timeupdate', handleTime);
    ws.on('seeking', handleTime);
    ws.on('interaction', handleTime);
    ws.on('play', () => transcriptEl.classList.toggle('playing', highlightToggle.checked));
    ws.on('pause', () => transcriptEl.classList.remove('playing'));
    ws.on('finish', () => {
      transcriptEl.classList.remove('playing');
      updateTranscriptHighlight(null);
    });
    ws.on('error', (e) => {
      console.error('WaveSurfer error', e);
      playBtn.disabled = true;
      downloadLink.textContent = 'Audio unavailable';
    });
    ws.load(url);
    state.wavesurfer = ws;
  }

  function renderDetail(call) {
    state.selected = call;
    transcriptEl.classList.remove('playing');
    detailTitle.textContent = call.pretty_title || call.filename;
    detailMeta.textContent = `${call.town || 'Unknown town'} • ${formatDate(call.call_timestamp)} • ${call.audio_url ? 'Audio ready' : 'No audio'}`;
    detailStatus.textContent = call.status;
    detailStatus.className = `badge ${call.status}`;
    detailType.textContent = call.call_type || '—';
    detailAgency.textContent = call.agency || call.town || '—';
    updatedAtEl.textContent = `Updated ${formatDate(call.updated_at)}`;
    renderTags(detailTags, call.tags || []);
    renderTranscript(call);
    if (call.audio_url) {
      playBtn.disabled = true;
      createWave(call.audio_url);
      playBtn.disabled = false;
      downloadLink.href = call.audio_url;
      downloadLink.textContent = 'Open audio';
    } else {
      destroyWave();
      playBtn.disabled = true;
      downloadLink.removeAttribute('href');
      downloadLink.textContent = 'Open audio';
    }
    regenBtn.disabled = false;
    renderList();
  }

  async function selectCall(call) {
    renderDetail(call);
  }

  const baseChartLayout = {
    margin: { t: 10, l: 30, r: 10, b: 30 },
    paper_bgcolor: 'transparent',
    plot_bgcolor: 'transparent',
    font: { color: '#e8eeff' },
  };

  function renderBarChart(targetId, labels, values, color, emptyLabel) {
    const el = document.getElementById(targetId);
    if (!labels.length || !values.length) {
      if (el) {
        el.innerHTML = `<p class="muted">${emptyLabel}</p>`;
      }
      Plotly.purge(targetId);
      return;
    }
    Plotly.newPlot(targetId, [{ type: 'bar', x: labels, y: values, marker: { color } }], baseChartLayout, { displayModeBar: false });
  }

  function renderMap() {
    if (!mapChart) return;
    const points = state.calls
      .filter((call) => call.location && Number.isFinite(call.location.latitude) && Number.isFinite(call.location.longitude))
      .map((call) => ({
        lat: call.location.latitude,
        lon: call.location.longitude,
        label: call.location.label || call.pretty_title || call.filename,
      }));

    if (!state.mapboxToken) {
      mapChart.innerHTML = '<p class="muted">Add MAPBOX_TOKEN to enable mapping.</p>';
      Plotly.purge('map-chart');
      return;
    }

    if (!points.length) {
      mapChart.innerHTML = '<p class="muted">No mappable calls in this window yet.</p>';
      Plotly.purge('map-chart');
      return;
    }

    Plotly.setPlotConfig({ mapboxAccessToken: state.mapboxToken });
    Plotly.newPlot('map-chart', [{
      type: 'scattermapbox',
      lat: points.map((p) => p.lat),
      lon: points.map((p) => p.lon),
      text: points.map((p) => p.label),
      marker: { size: 10, color: '#7ce7ff' },
      hovertemplate: '%{text}<extra></extra>',
    }], {
      mapbox: {
        style: 'mapbox/dark-v11',
        center: { lon: -74.696, lat: 41.05 },
        zoom: 8,
      },
      margin: { t: 0, l: 0, r: 0, b: 0 },
      paper_bgcolor: 'transparent',
      plot_bgcolor: 'transparent',
    }, { displayModeBar: false });
  }

  function applyStats() {
    if (!state.stats) return;
    const statusLabels = Object.keys(state.stats.status_counts || {});
    const statusValues = statusLabels.map((key) => state.stats.status_counts[key]);
    renderBarChart('status-chart', statusLabels, statusValues, '#7ce7ff', 'No calls yet.');

    const tagEntries = Object.entries(state.stats.tag_counts || {}).sort((a, b) => b[1] - a[1]).slice(0, 6);
    renderBarChart('tag-chart', tagEntries.map((e) => e[0]), tagEntries.map((e) => e[1]), '#5ef5a4', 'Tags will appear once transcripts are ready.');

    const agencyEntries = Object.entries(state.stats.agency_counts || {}).sort((a, b) => b[1] - a[1]).slice(0, 6);
    renderBarChart('agency-chart', agencyEntries.map((e) => e[0]), agencyEntries.map((e) => e[1]), '#a5b6ff', 'Agencies populate once calls arrive.');

    const townEntries = Object.entries(state.stats.town_counts || {}).sort((a, b) => b[1] - a[1]).slice(0, 6);
    renderBarChart('town-chart', townEntries.map((e) => e[0]), townEntries.map((e) => e[1]), '#ffd166', 'No towns recognized yet.');

    renderMap();
  }

  function renderTagFilterOptions() {
    tagFilterEl.innerHTML = '';
    if (!state.stats) return;
    const tags = Object.keys(state.stats.tag_counts || {}).sort((a, b) => state.stats.tag_counts[b] - state.stats.tag_counts[a]).slice(0, 12);
    if (!tags.length) {
      const hint = document.createElement('span');
      hint.className = 'muted';
      hint.textContent = 'Tags will appear once transcripts are ready.';
      tagFilterEl.appendChild(hint);
      return;
    }
    tags.forEach((tag) => {
      const pill = document.createElement('button');
      pill.type = 'button';
      pill.className = 'tag';
      pill.textContent = tag;
      if (state.tagFilter.includes(tag.toLowerCase())) pill.classList.add('active');
      pill.addEventListener('click', () => toggleTag(tag));
      tagFilterEl.appendChild(pill);
    });
  }

  async function fetchCalls() {
    const params = new URLSearchParams();
    params.set('window', state.window);
    if (state.search) params.set('q', state.search);
    if (state.status) params.set('status', state.status);
    if (state.tagFilter.length) params.set('tags', state.tagFilter.join(','));
    const res = await fetch(`/api/transcriptions?${params.toString()}`);
    if (!res.ok) {
      console.error('Failed to load calls');
      return;
    }
    const payload = await res.json();
    state.mapboxToken = payload.mapbox_token || state.mapboxToken;
    state.calls = (payload.calls || []).sort((a, b) => new Date(b.call_timestamp || 0) - new Date(a.call_timestamp || 0));
    state.stats = payload.stats || {};
    renderList();
    renderTagFilterOptions();
    applyStats();
    if (state.calls.length && (!state.selected || !state.calls.find((c) => c.filename === state.selected.filename))) {
      renderDetail(state.calls[0]);
    }
  }

  async function regenerate() {
    if (!state.selected) return;
    regenBtn.disabled = true;
    try {
      await fetch(`/api/transcription?filename=${encodeURIComponent(state.selected.filename)}`, { method: 'POST' });
    } finally {
      regenBtn.disabled = false;
      fetchCalls();
    }
  }

  searchInput.addEventListener('input', (e) => {
    state.search = e.target.value;
    fetchCalls();
  });
  statusSelect.addEventListener('change', (e) => {
    state.status = e.target.value;
    fetchCalls();
  });
  refreshBtn.addEventListener('click', fetchCalls);
  regenBtn.addEventListener('click', regenerate);
  playBtn.addEventListener('click', () => {
    if (state.wavesurfer) state.wavesurfer.playPause();
  });
  highlightToggle.addEventListener('change', () => {
    if (!highlightToggle.checked) {
      transcriptEl.classList.remove('playing');
      updateTranscriptHighlight(null);
      return;
    }
    const currentTime = state.wavesurfer ? state.wavesurfer.getCurrentTime() : 0;
    if (state.wavesurfer && state.wavesurfer.isPlaying()) {
      transcriptEl.classList.add('playing');
    }
    updateTranscriptHighlight(currentTime);
  });
  windowButtons.forEach((btn) => btn.addEventListener('click', () => setWindow(btn.dataset.window)));

  fetchCalls();
})();
