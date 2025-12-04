import {
  buildIncidentViewModel,
  formatIncidentHeader,
  formatIncidentLocation,
  normalizeIncident,
} from './incidents.js';

(() => {
  const listEl = document.getElementById('call-list');
  const emptyState = document.getElementById('empty-state');
  const loadingState = document.getElementById('loading-state');
  const errorState = document.getElementById('error-state');
  const retryLoad = document.getElementById('retry-load');
  const totalCount = document.getElementById('total-count');
  const searchInput = document.getElementById('search');
  const statusSelect = document.getElementById('status');
  const callTypeSelect = document.getElementById('call-type');
  const agencySelect = document.getElementById('agency');
  const sortSelect = document.getElementById('sort');
  const refreshBtn = document.getElementById('refresh');
  const windowButtons = document.querySelectorAll('.window-switcher button');
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
  const detailLocation = document.getElementById('detail-location');
  const detailSummary = document.getElementById('detail-summary');
  const detailWarning = document.getElementById('detail-warning');
  const detailTags = document.getElementById('detail-tags');
  const mapChart = document.getElementById('map-chart');
  const updatedAtEl = document.getElementById('updated-at');
  const tagFilterEl = document.getElementById('tag-filter');
  const filterChips = document.getElementById('filter-chips');
  const toggleAdvancedBtn = document.getElementById('toggle-advanced');
  const advancedFilters = document.getElementById('advanced-filters');
  const filtersDrawer = document.getElementById('filters-drawer');
  const filtersOverlay = document.getElementById('filters-overlay');
  const closeFiltersBtn = document.getElementById('close-filters');
  const mapLayerDensity = document.getElementById('map-layer-density');
  const mapLayerPoints = document.getElementById('map-layer-points');
  const mapLayerHotspots = document.getElementById('map-layer-hotspots');
  const summaryTotal = document.getElementById('summary-total');
  const summaryTopType = document.getElementById('summary-top-type');
  const summaryAgency = document.getElementById('summary-agency');
  const summaryStatus = document.getElementById('summary-status');
  const summaryWindow = document.getElementById('summary-window');
  const summaryWindowHint = document.getElementById('summary-window-hint');
  const suggestionList = document.getElementById('search-suggestions');
  const hotspotListEl = document.getElementById('hotspot-list');
  const hotspotHint = document.getElementById('hotspot-hint');

  const state = {
    window: '6h',
    search: '',
    status: '',
    callType: '',
    agency: '',
    sort: 'recent',
    tagFilter: [],
    calls: [],
    stats: null,
    summaryStats: null,
    selected: null,
    wavesurfer: null,
    segments: [],
    mapboxToken: '',
    map: null,
    mapLayerVisibility: { density: true, points: false, hotspots: true },
    mapGeoJSON: { type: 'FeatureCollection', features: [] },
    hotspotGeoJSON: { type: 'FeatureCollection', features: [] },
    hotspots: [],
    pollTimer: null,
    autoRefreshTimer: null,
    summaryRefreshTimer: null,
    hotspotRefreshTimer: null,
    inlineAudio: null,
    mapResizeTimer: null,
    activePopup: null,
    loading: false,
    error: '',
    fetchingCalls: null,
  };

  const MAP_DEFAULT_CENTER = [-74.696, 41.05];
  const MAP_DEFAULT_ZOOM = 8;
  const MAP_STYLE = 'mapbox://styles/mapbox/dark-v11';
  const CALLS_REFRESH_INTERVAL = 5000;
  const SUMMARY_REFRESH_INTERVAL = 30000;
  const HOTSPOT_REFRESH_INTERVAL = 45000;

  function openFilters() {
    if (!filtersDrawer) return;
    filtersDrawer.classList.add('open');
    filtersDrawer.setAttribute('aria-hidden', 'false');
    if (toggleAdvancedBtn) toggleAdvancedBtn.textContent = 'Hide filters';
  }

  function closeFilters() {
    if (!filtersDrawer) return;
    filtersDrawer.classList.remove('open');
    filtersDrawer.setAttribute('aria-hidden', 'true');
    if (toggleAdvancedBtn) toggleAdvancedBtn.textContent = 'Filters';
  }

  function toggleAdvancedFilters() {
    if (!filtersDrawer) return;
    if (filtersDrawer.classList.contains('open')) {
      closeFilters();
    } else {
      openFilters();
    }
  }

  function setWindow(next) {
    state.window = next;
    state.summaryStats = null;
    windowButtons.forEach((btn) => btn.classList.toggle('active', btn.dataset.window === next));
    renderSummaryBar();
    refreshAll();
  }

  function windowHours() {
    switch (state.window) {
      case '6h':
        return 6;
      case '12h':
        return 12;
      case '72h':
        return 72;
      case 'all':
        return null;
      default:
        return 24;
    }
  }

  function windowLabel() {
    switch (state.window) {
      case '6h':
        return 'the last 6 hours';
      case '12h':
        return 'the last 12 hours';
      case '72h':
        return 'the last 72 hours';
      case 'all':
        return 'all time';
      default:
        return 'the last 24 hours';
    }
  }

  function windowChipLabel() {
    switch (state.window) {
      case 'all':
        return 'All';
      case '12h':
        return 'Last 12h';
      case '24h':
        return 'Last 24h';
      case '72h':
        return 'Last 72h';
      default:
        return 'Last 6h';
    }
  }

  function windowHintLabel() {
    if (state.window === 'all') {
      return 'All recorded incidents';
    }
    const label = windowLabel();
    if (label.startsWith('the last ')) {
      return `Past ${label.replace('the last ', '')}`;
    }
    return label;
  }

  function stopPolling() {
    if (state.pollTimer) {
      clearInterval(state.pollTimer);
      state.pollTimer = null;
    }
  }

  function statusIcon(status) {
    switch (status) {
      case 'done':
        return '✔️';
      case 'processing':
        return '⏳';
      case 'queued':
        return '🕒';
      case 'error':
        return '⚠️';
      default:
        return '•';
    }
  }

  function formatStatusLabel(status) {
    switch (status) {
      case 'done':
        return 'Completed';
      case 'processing':
        return 'Transcribing';
      case 'queued':
        return 'Queued';
      case 'error':
        return 'Failed';
      default:
        return 'Unknown';
    }
  }

  function callSummary(call) {
    const vm = buildIncidentViewModel(call);
    return vm.header;
  }

  function dedupeCalls(calls) {
    const seen = new Set();
    const unique = [];
    calls.forEach((call) => {
      if (seen.has(call.filename)) return;
      seen.add(call.filename);
      unique.push(call);
    });
    return unique;
  }

  function escapeHTML(text) {
    const div = document.createElement('div');
    div.textContent = text || '';
    return div.innerHTML;
  }

  function callLocationLabel(call) {
    return formatIncidentLocation(call);
  }

  function narrativeSnippet(call) {
    const vm = buildIncidentViewModel(call);
    return vm.summary || 'Transcript pending.';
  }

  function shortSummary(call, limit = 180) {
    const snippet = narrativeSnippet(call);
    if (snippet.length > limit) {
      return `${snippet.slice(0, limit - 1)}…`;
    }
    return snippet;
  }

  function audioUrlForCall(call) {
    return call.audioUrl || call.audio_url || call.audioPath || call.audio_path || (call.filename ? `/${call.filename}` : '');
  }

  function formatDate(value) {
    if (!value) return '—';
    const dt = new Date(value);
    return dt.toLocaleString();
  }

  function callTimestampValue(call) {
    return call.timestampLocal || call.call_timestamp || call.callTimestamp || call.updated_at || call.created_at || null;
  }

  function renderTags(container, tags, clickable = false) {
    container.innerHTML = '';
    tags.forEach((tag) => {
      const pill = document.createElement(clickable ? 'button' : 'span');
      pill.className = 'tag';
      pill.textContent = tag;
      if (clickable) {
        const active = state.tagFilter.includes(tag.toLowerCase());
        pill.classList.toggle('active', active);
        pill.setAttribute('aria-pressed', active);
        pill.type = 'button';
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

  function bindSuggestions() {
    if (!suggestionList) return;
    const buttons = suggestionList.querySelectorAll('button[data-value]');
    buttons.forEach((btn) => {
      btn.addEventListener('click', () => {
        const value = btn.dataset.value || btn.textContent || '';
        searchInput.value = value;
        state.search = value;
        fetchCalls();
      });
    });
  }

  function getVisibleCalls() {
    const agencyFilter = state.agency.trim().toLowerCase();
    const callTypeFilter = state.callType.trim().toLowerCase();
    let filtered = state.calls.filter((call) => {
      const agency = (call.agency || call.town || call.cityOrTown || '').toLowerCase();
      const callType = (call.callType || call.call_type || '').toLowerCase();
      if (agencyFilter && !agency.includes(agencyFilter)) return false;
      if (callTypeFilter && callType !== callTypeFilter) return false;
      return true;
    });

    switch (state.sort) {
      case 'status': {
        const order = ['processing', 'queued', 'done', 'error'];
        filtered = filtered.sort((a, b) => {
          const idxA = order.indexOf(a.status);
          const idxB = order.indexOf(b.status);
          if (idxA === idxB) {
            return new Date(callTimestampValue(b) || 0) - new Date(callTimestampValue(a) || 0);
          }
          return idxA - idxB;
        });
        break;
      }
      case 'oldest':
        filtered = filtered.sort((a, b) => new Date(callTimestampValue(a) || 0) - new Date(callTimestampValue(b) || 0));
        break;
      default:
        filtered = filtered.sort((a, b) => new Date(callTimestampValue(b) || 0) - new Date(callTimestampValue(a) || 0));
    }

    return filtered;
  }

  function buildCard(call) {
    const vm = buildIncidentViewModel(call);
    const audioUrl = audioUrlForCall(call);
    const hasMissing = Array.isArray(call.missingFields) && call.missingFields.length > 0;
    const card = document.createElement('article');
    card.className = `call-card ${vm.cardClass}`;
    card.tabIndex = 0;
    card.setAttribute('role', 'listitem');
    const title = callSummary(call);
    const locationLabel = callLocationLabel(call);
    const snippet = shortSummary(call);
    const snippetSafe = escapeHTML(snippet);
    const expanded = state.selected && state.selected.filename === call.filename;
    const fullText = escapeHTML(snippet);
    card.innerHTML = `
      <div class="card-top">
        <div>
          <div class="title">${escapeHTML(title)}</div>
          <div class="meta">${escapeHTML(vm.subtitle || '')}</div>
          <div class="meta location-line">${escapeHTML(locationLabel)}</div>
        </div>
        <div class="card-flags">
          <span class="status-icon ${call.status}" aria-label="${formatStatusLabel(call.status)}">${statusIcon(call.status)}</span>
          ${hasMissing ? '<span class="badge warning">Missing details</span>' : ''}
        </div>
      </div>
      <div class="call-snippet" title="${snippetSafe}">${snippetSafe}</div>
      <div class="card-footer">
        <span class="badge ${call.status}">${formatStatusLabel(call.status)}</span>
        <div class="quick-actions">
          ${
            audioUrl
              ? `<button type="button" class="mini inline-audio incident-audio ${vm.audioClass}">▶ Preview</button>`
              : '<span class="muted">Audio unavailable</span>'
          }
        </div>
      </div>
      ${expanded ? `<div class="call-expanded" title="${fullText}">${fullText}</div>` : ''}
    `;
    card.addEventListener('click', () => selectCall(call));
    card.addEventListener('keydown', (evt) => {
      if (evt.key === 'Enter' || evt.key === ' ') {
        evt.preventDefault();
        selectCall(call);
      }
    });
    const previewBtn = card.querySelector('.inline-audio');
    if (previewBtn) {
      previewBtn.addEventListener('click', (evt) => {
        evt.stopPropagation();
        playInlineAudio(call, previewBtn);
      });
    }
    if (state.selected && state.selected.filename === call.filename) {
      card.classList.add('active');
    }
    return card;
  }

  function stopInlineAudio() {
    if (state.inlineAudio) {
      state.inlineAudio.pause();
      state.inlineAudio = null;
    }
  }

  function playInlineAudio(call, button) {
    stopInlineAudio();
    const audioUrl = audioUrlForCall(call);
    if (!audioUrl) return;
    const audio = new Audio(audioUrl);
    audio.preload = 'none';
    button.textContent = '⏸ Pause';
    audio.addEventListener('ended', () => {
      button.textContent = '▶ Preview';
      stopInlineAudio();
    });
    audio.addEventListener('pause', () => {
      button.textContent = '▶ Preview';
    });
    audio.play();
    state.inlineAudio = audio;
  }

  function renderList() {
    listEl.innerHTML = '';
    if (loadingState) loadingState.classList.toggle('hidden', !state.loading);
    if (errorState) errorState.classList.add('hidden');
    if (emptyState) emptyState.classList.add('hidden');
    if (state.loading) {
      totalCount.textContent = '';
      return;
    }
    if (state.error) {
      if (errorState) errorState.classList.remove('hidden');
      totalCount.textContent = '';
      return;
    }
    const callsToRender = getVisibleCalls();
    if (!callsToRender.length) {
      if (emptyState) emptyState.classList.remove('hidden');
      totalCount.textContent = '';
      return;
    }
    callsToRender.forEach((call) => listEl.appendChild(buildCard(call)));
    totalCount.textContent = `${callsToRender.length} incidents`;
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

    const text = (
      call.cleanTranscript ||
      call.clean_transcript_text ||
      call.rawTranscript ||
      call.raw_transcript_text ||
      call.transcript_text ||
      call.translation ||
      call.translation_text ||
      ''
    ).trim();
    if (!text) return [];
    const duration = Number(call.duration_seconds || call.durationSeconds) || 0;
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
    const transcriptText =
      (
        call.cleanTranscript ||
        call.clean_transcript_text ||
        call.rawTranscript ||
        call.raw_transcript_text ||
        call.transcript_text ||
        call.translation ||
        call.translation_text ||
        ''
      ).trim();
    transcriptEl.innerHTML = '';
    transcriptEl.classList.toggle('playing', highlightToggle.checked && state.wavesurfer && state.wavesurfer.isPlaying());
    if (!state.segments.length) {
      const placeholder = document.createElement('div');
      placeholder.className = 'muted transcript-placeholder';
      if (call.status === 'done') {
        placeholder.textContent = transcriptText
          ? 'Transcript ready but could not be displayed yet.'
          : 'Transcript unavailable for this completed call.';
      } else if (call.status === 'error' && (call.last_error || call.lastError)) {
        const err = call.last_error || call.lastError;
        placeholder.textContent = `Transcription failed: ${err}`;
      } else {
        placeholder.innerHTML = `
          <div class="spinner" aria-hidden="true"></div>
          <div>
            <strong>Transcribing audio…</strong>
            <p class="muted">Hang tight — we will refresh this transcript automatically.</p>
          </div>`;
      }
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
    stopInlineAudio();
    transcriptEl.classList.remove('playing');
    const vm = buildIncidentViewModel(call);
    const audioUrl = audioUrlForCall(call);
    detailTitle.textContent = formatIncidentHeader(call) || call.pretty_title || call.filename;
    detailMeta.textContent = vm.subtitle || formatDate(callTimestampValue(call));
    if (detailLocation) detailLocation.textContent = formatIncidentLocation(call);
    const hasMissing = Array.isArray(call.missingFields) && call.missingFields.length > 0;
    if (detailWarning) {
      detailWarning.classList.toggle('hidden', !hasMissing);
    }
    if (detailSummary) {
      detailSummary.textContent = shortSummary(call, 320);
    }
    detailStatus.innerHTML = `${statusIcon(call.status)} ${formatStatusLabel(call.status)}`;
    detailStatus.className = `badge ${call.status}`;
    detailType.textContent = call.callType || call.call_type || '—';
    detailAgency.textContent = call.agency || call.cityOrTown || call.town || '—';
    updatedAtEl.textContent = `Updated ${formatDate(call.updated_at || call.updatedAt)}`;
    renderTags(detailTags, call.tags || []);
    renderTranscript(call);
    if (audioUrl) {
      playBtn.disabled = true;
      createWave(audioUrl);
      playBtn.disabled = false;
      downloadLink.href = audioUrl;
      downloadLink.textContent = 'Download audio';
    } else {
      destroyWave();
      playBtn.disabled = true;
      downloadLink.removeAttribute('href');
      downloadLink.textContent = 'Audio unavailable';
    }
    renderList();
    scheduleStatusPolling(call);
  }

  async function selectCall(call) {
    renderDetail(call);
    await refreshSelected(call.filename);
    focusMapOnCall(call);
  }

  function isRecentCompleted(call) {
    if (!call || call.status !== 'done') return false;
    if (call.duplicate_of || call.duplicateOf) return false;
    const tsValue = callTimestampValue(call);
    const ts = tsValue ? new Date(tsValue).getTime() : 0;
    if (!Number.isFinite(ts)) return false;
    const hours = windowHours();
    if (hours === null) return true;
    const cutoff = Date.now() - hours * 60 * 60 * 1000;
    return ts >= cutoff;
  }

  function getInsightCalls(source = state.calls) {
    return dedupeCalls(source).filter(isRecentCompleted);
  }

  function hasValidCoordinates(value) {
    return value && Number.isFinite(value.lat) && Number.isFinite(value.lon);
  }

  function extractCoordinates(call) {
    if (call.location && Number.isFinite(call.location.latitude) && Number.isFinite(call.location.longitude)) {
      return { lat: Number(call.location.latitude), lon: Number(call.location.longitude) };
    }
    return null;
  }

  function destroyMapInstance() {
    if (state.map) {
      state.map.remove();
      state.map = null;
    }
  }

  function showMapUnavailable(title, message) {
    destroyMapInstance();
    if (!mapChart) return;
    mapChart.innerHTML = `<div class="map-empty"><div><h4>${title}</h4><p class="muted">${message}</p></div></div>`;
  }

  function setMapOverlay(title, message) {
    if (!mapChart) return;
    let overlay = mapChart.querySelector('.map-empty');
    if (!overlay) {
      overlay = document.createElement('div');
      overlay.className = 'map-empty';
      mapChart.appendChild(overlay);
    }
    overlay.innerHTML = `<div><p class="eyebrow">Incident map</p><h4>${title}</h4><p class="muted">${message}</p></div>`;
  }

  function clearMapOverlay() {
    const overlay = mapChart?.querySelector('.map-empty');
    if (overlay) overlay.remove();
  }

  function scheduleMapResize() {
    if (!state.map) return;
    if (state.mapResizeTimer) clearTimeout(state.mapResizeTimer);
    state.mapResizeTimer = setTimeout(() => {
      if (state.map) {
        state.map.resize();
      }
    }, 180);
  }

  function resetMapView() {
    if (!state.map) return;
    state.map.setCenter(MAP_DEFAULT_CENTER);
    state.map.setZoom(MAP_DEFAULT_ZOOM);
  }

  function buildFeatureCollection(points) {
    return {
      type: 'FeatureCollection',
      features: points.map((point, idx) => ({
        type: 'Feature',
        id: point.call.filename || idx,
        geometry: { type: 'Point', coordinates: [point.lon, point.lat] },
        properties: {
          title: callSummary(point.call),
          subtitle: callLocationLabel(point.call),
          timestamp: formatDate(callTimestampValue(point.call)),
          weight: Math.max(0.3, Math.min(1, (point.call.duration_seconds || point.call.durationSeconds || 60) / 600)),
        },
      })),
    };
  }

  function buildHotspotCollection(spots) {
    return {
      type: 'FeatureCollection',
      features: spots
        .filter((spot) => Number.isFinite(spot.longitude) && Number.isFinite(spot.latitude))
        .map((spot, idx) => ({
          type: 'Feature',
          id: `hotspot-${idx}`,
          geometry: { type: 'Point', coordinates: [Number(spot.longitude), Number(spot.latitude)] },
          properties: {
            label: spot.label || 'Hotspot',
            count: Number(spot.count) || 1,
            intensity: Math.max(1, Math.min(Number(spot.count) || 1, 30)),
          },
        })),
    };
  }

  function formatHotspotWindowLabel(windowName) {
    switch (windowName) {
      case '6h':
        return 'Last 6 hours';
      case '12h':
        return 'Last 12 hours';
      case '24h':
        return 'Last 24 hours';
      case '72h':
        return 'Last 72 hours';
      case '7d':
        return 'Last 7 days';
      case '30d':
        return 'Last 30 days';
      case 'all':
        return 'All time';
      default:
        return 'Rolling 30 days';
    }
  }

  function renderHotspotList(windowName) {
    if (!hotspotListEl) return;
    hotspotListEl.innerHTML = '';
    if (hotspotHint) {
      hotspotHint.textContent = formatHotspotWindowLabel(windowName || state.window);
    }
    if (!state.hotspots.length) {
      const empty = document.createElement('li');
      empty.className = 'hotspot-empty muted';
      empty.textContent = 'No hotspots detected for this window.';
      hotspotListEl.appendChild(empty);
      return;
    }
    state.hotspots.slice(0, 6).forEach((spot) => {
      const li = document.createElement('li');
      const titleWrap = document.createElement('div');
      titleWrap.className = 'label';
      const title = document.createElement('strong');
      title.textContent = spot.label || 'Unnamed location';
      const meta = document.createElement('span');
      meta.className = 'meta';
      const lastSeen = spot.last_seen || spot.lastSeen;
      meta.textContent = lastSeen ? `Last seen ${formatDate(lastSeen)}` : 'Cluster activity';
      titleWrap.appendChild(title);
      titleWrap.appendChild(meta);
      const count = document.createElement('span');
      count.className = 'hotspot-count';
      count.textContent = `${Number(spot.count || 0).toLocaleString()}×`;
      li.appendChild(titleWrap);
      li.appendChild(count);
      hotspotListEl.appendChild(li);
    });
  }

  function ensureMapSource() {
    if (!state.map) return;
    if (!state.map.getSource('call-points')) {
      state.map.addSource('call-points', { type: 'geojson', data: state.mapGeoJSON });
    }
    if (!state.map.getLayer('call-heatmap')) {
      state.map.addLayer({
        id: 'call-heatmap',
        type: 'heatmap',
        source: 'call-points',
        maxzoom: 15,
        paint: {
          'heatmap-weight': ['get', 'weight'],
          'heatmap-intensity': 1,
          'heatmap-radius': 30,
          'heatmap-opacity': state.mapLayerVisibility.density ? 0.85 : 0,
          'heatmap-color': [
            'interpolate',
            ['linear'],
            ['heatmap-density'],
            0, 'rgba(124, 231, 255, 0)',
            0.2, 'rgba(124, 231, 255, 0.35)',
            0.5, 'rgba(167, 139, 250, 0.55)',
            0.8, 'rgba(94, 245, 164, 0.7)',
            1, 'rgba(255, 209, 102, 0.8)'
          ],
        },
      });
    }
    if (!state.map.getLayer('call-circles')) {
      state.map.addLayer({
        id: 'call-circles',
        type: 'circle',
        source: 'call-points',
        minzoom: 5,
        paint: {
          'circle-radius': 8,
          'circle-color': '#7ce7ff',
          'circle-stroke-color': '#0b1021',
          'circle-stroke-width': 1,
          'circle-opacity': state.mapLayerVisibility.points ? 0.95 : 0,
        },
      });
      state.map.on('click', 'call-circles', (evt) => {
        const feature = evt.features?.[0];
        if (!feature) return;
        const coordinates = feature.geometry.coordinates.slice();
        const html = `
          <div>
            <strong>${escapeHTML(feature.properties.title)}</strong>
            <div class="muted">${escapeHTML(feature.properties.timestamp)}</div>
            <div class="muted">${escapeHTML(feature.properties.subtitle)}</div>
          </div>`;
        new mapboxgl.Popup({ offset: 12 }).setLngLat(coordinates).setHTML(html).addTo(state.map);
      });
    }
    if (!state.map.getSource('hotspot-points')) {
      state.map.addSource('hotspot-points', { type: 'geojson', data: state.hotspotGeoJSON });
    }
    if (!state.map.getLayer('hotspot-glow')) {
      state.map.addLayer({
        id: 'hotspot-glow',
        type: 'circle',
        source: 'hotspot-points',
        minzoom: 6,
        paint: {
          'circle-radius': ['interpolate', ['linear'], ['get', 'intensity'], 1, 10, 30, 36],
          'circle-color': '#ffd166',
          'circle-opacity': 0,
          'circle-blur': 0.75,
        },
      });
    }
    if (!state.map.getLayer('hotspot-outline')) {
      state.map.addLayer({
        id: 'hotspot-outline',
        type: 'circle',
        source: 'hotspot-points',
        minzoom: 6,
        paint: {
          'circle-radius': ['interpolate', ['linear'], ['get', 'intensity'], 1, 5, 30, 18],
          'circle-color': 'rgba(11,16,33,0.8)',
          'circle-stroke-width': 2,
          'circle-stroke-color': '#ffd166',
          'circle-opacity': 0,
        },
      });
      state.map.on('click', 'hotspot-outline', (evt) => {
        const feature = evt.features?.[0];
        if (!feature) return;
        const coordinates = feature.geometry.coordinates.slice();
        const html = `
          <div>
            <strong>${escapeHTML(feature.properties.label || 'Hotspot')}</strong>
            <div class="muted">${Number(feature.properties.count || 0).toLocaleString()} incidents</div>
          </div>`;
        new mapboxgl.Popup({ offset: 12 }).setLngLat(coordinates).setHTML(html).addTo(state.map);
      });
    }
  }

  function refreshMapLayers(boundsData) {
    if (!state.map) return;
    ensureMapSource();
    const source = state.map.getSource('call-points');
    if (source) {
      source.setData(state.mapGeoJSON);
    }
    const hotspotSource = state.map.getSource('hotspot-points');
    if (hotspotSource) {
      hotspotSource.setData(state.hotspotGeoJSON);
    }
    if (state.map.getLayer('call-heatmap')) {
      state.map.setPaintProperty('call-heatmap', 'heatmap-opacity', state.mapLayerVisibility.density ? 0.85 : 0);
    }
    if (state.map.getLayer('call-circles')) {
      state.map.setPaintProperty('call-circles', 'circle-opacity', state.mapLayerVisibility.points ? 0.95 : 0);
    }
    const hotspotVisible = state.mapLayerVisibility.hotspots && state.hotspotGeoJSON.features.length > 0;
    if (state.map.getLayer('hotspot-glow')) {
      state.map.setPaintProperty('hotspot-glow', 'circle-opacity', hotspotVisible ? 0.65 : 0);
    }
    if (state.map.getLayer('hotspot-outline')) {
      state.map.setPaintProperty('hotspot-outline', 'circle-opacity', hotspotVisible ? 0.9 : 0);
    }
    if (boundsData && boundsData.isValid) {
      state.map.fitBounds(boundsData.bounds, { padding: 48, maxZoom: 13 });
    }
    scheduleMapResize();
  }

  function syncMapLayerButtons() {
    if (mapLayerDensity) mapLayerDensity.classList.toggle('active', state.mapLayerVisibility.density);
    if (mapLayerPoints) mapLayerPoints.classList.toggle('active', state.mapLayerVisibility.points);
    if (mapLayerHotspots) mapLayerHotspots.classList.toggle('active', state.mapLayerVisibility.hotspots);
  }

  function setMapLayerVisibility(layer, enabled) {
    state.mapLayerVisibility = { ...state.mapLayerVisibility, [layer]: enabled };
    syncMapLayerButtons();
    refreshMapLayers();
  }

  function focusMapOnCall(call) {
    if (!state.map) return;
    const coords = extractCoordinates(call);
    if (!hasValidCoordinates(coords)) return;
    state.map.flyTo({ center: [coords.lon, coords.lat], zoom: 12, essential: true });
    const html = `
      <div>
        <strong>${escapeHTML(callSummary(call))}</strong>
        <div class="muted">${formatDate(callTimestampValue(call))}</div>
        <div class="muted">${escapeHTML(callLocationLabel(call))}</div>
      </div>`;
    if (state.activePopup) state.activePopup.remove();
    state.activePopup = new mapboxgl.Popup({ offset: 12 }).setLngLat([coords.lon, coords.lat]).setHTML(html).addTo(state.map);
  }

  async function updateMapMarkers(calls) {
    if (!state.map) return;
    const points = [];
    for (const call of calls) {
      const coords = extractCoordinates(call);
      if (!hasValidCoordinates(coords)) continue;
      points.push({ call, ...coords });
    }

    if (!points.length) {
      state.mapGeoJSON = { type: 'FeatureCollection', features: [] };
      resetMapView();
      if (state.hotspotGeoJSON.features.length) {
        clearMapOverlay();
      } else {
        setMapOverlay('No mapped incidents', `No completed incidents with map coordinates in ${windowLabel()}.`);
      }
      refreshMapLayers();
      scheduleMapResize();
      return;
    }

    clearMapOverlay();
    state.mapGeoJSON = buildFeatureCollection(points);
    const bounds = new mapboxgl.LngLatBounds();
    points.forEach((point) => bounds.extend([point.lon, point.lat]));
    refreshMapLayers({ bounds, isValid: !bounds.isEmpty() });
  }

  async function renderMap() {
    if (typeof window === 'undefined' || !mapChart) return;
    const token = (state.mapboxToken || '').trim();
    if (!token) {
      showMapUnavailable('Map unavailable', 'Add a Mapbox access token to view incident geography.');
      return;
    }
    if (typeof mapboxgl === 'undefined') {
      showMapUnavailable('Map unavailable', 'Map library failed to load. Please refresh.');
      return;
    }

    mapboxgl.accessToken = token;
    const callsForMap = getInsightCalls(getVisibleCalls());

    if (!state.map) {
      clearMapOverlay();
      mapChart.innerHTML = '';
      state.map = new mapboxgl.Map({
        container: 'map-chart',
        style: MAP_STYLE,
        center: MAP_DEFAULT_CENTER,
        zoom: MAP_DEFAULT_ZOOM,
      });
      state.map.addControl(new mapboxgl.NavigationControl(), 'top-right');
      state.map.addControl(new mapboxgl.ScaleControl({ maxWidth: 120, unit: 'imperial' }), 'bottom-right');
      state.map.on('load', () => {
        ensureMapSource();
        updateMapMarkers(callsForMap);
        scheduleMapResize();
      });
    } else {
      updateMapMarkers(callsForMap);
    }
  }

  function applyStats() {
    renderMap();
    renderSummaryBar();
  }

  function formatTopLabel(value) {
    if (!value) return '';
    return value
      .replace(/[_-]+/g, ' ')
      .split(/\s+/)
      .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
      .join(' ');
  }

  function normalizeTopEntry(entry) {
    if (!entry) return null;
    return { label: entry.tag || entry.Tag || entry.label, count: entry.count || entry.Count || entry.value };
  }

  function topEntryFromMap(obj) {
    const pairs = Object.entries(obj || {}).sort((a, b) => Number(b[1]) - Number(a[1]));
    if (!pairs.length) return null;
    const [label, count] = pairs[0];
    return { label, count: Number(count) };
  }

  function renderSummaryBar() {
    if (!summaryTotal || !summaryTopType || !summaryAgency || !summaryStatus) return;
    const stats = state.summaryStats;
    const fallbackStats = state.stats;
    const total = stats?.total_incidents ?? fallbackStats?.total;

    if (summaryWindow) summaryWindow.textContent = windowChipLabel();
    if (summaryWindowHint) summaryWindowHint.textContent = windowHintLabel();

    summaryTotal.textContent = total !== undefined ? Number(total || 0).toLocaleString() : '—';

    const typeEntry =
      normalizeTopEntry((stats?.top_incident_types || [])[0]) ||
      topEntryFromMap(stats?.by_type) ||
      topEntryFromMap(fallbackStats?.call_type_counts);
    summaryTopType.textContent = typeEntry ? `${formatTopLabel(typeEntry.label)} (${typeEntry.count})` : '—';

    const agencyEntry =
      normalizeTopEntry((stats?.top_agencies || [])[0]) || topEntryFromMap(stats?.by_agency) || topEntryFromMap(fallbackStats?.agency_counts);
    summaryAgency.textContent = agencyEntry ? `${formatTopLabel(agencyEntry.label)} (${agencyEntry.count})` : '—';

    const statusCounts = stats?.by_status || fallbackStats?.status_counts || {};
    const queued = Number(statusCounts.queued || 0) + Number(statusCounts.processing || 0);
    const completed = Number(statusCounts.done || 0);
    summaryStatus.textContent = stats || fallbackStats ? `${queued} pending • ${completed} completed` : '—';
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
      pill.setAttribute('aria-pressed', state.tagFilter.includes(tag.toLowerCase()));
      pill.addEventListener('click', () => toggleTag(tag));
      tagFilterEl.appendChild(pill);
    });
  }

  function renderFilterOptions() {
    if (!state.stats) return;
    const callTypes = Object.keys(state.stats.call_type_counts || {}).sort((a, b) => state.stats.call_type_counts[b] - state.stats.call_type_counts[a]);
    const agencies = Object.keys(state.stats.agency_counts || {}).sort((a, b) => state.stats.agency_counts[b] - state.stats.agency_counts[a]);

    const setOptions = (select, options) => {
      if (!select) return;
      const current = select.value;
      select.innerHTML = '<option value="">Any</option>';
      options.forEach((value) => {
        const opt = document.createElement('option');
        opt.value = value;
        opt.textContent = value;
        select.appendChild(opt);
      });
      if (current && options.includes(current)) {
        select.value = current;
      }
    };

    setOptions(callTypeSelect, callTypes);
    setOptions(agencySelect, agencies);
  }

  function renderFilterChips() {
    if (!filterChips) return;
    filterChips.innerHTML = '';
    const chips = [];
    if (state.search) chips.push({ label: `Search: ${state.search}`, onRemove: () => { state.search = ''; searchInput.value = ''; fetchCalls(); } });
    if (state.status) {
      chips.push({
        label: `Status: ${formatStatusLabel(state.status)}`,
        onRemove: () => {
          state.status = '';
          statusSelect.value = '';
          fetchCalls();
        },
      });
    }
    if (state.callType)
      chips.push({
        label: `Incident type: ${state.callType}`,
        onRemove: () => {
          state.callType = '';
          callTypeSelect.value = '';
          fetchCalls();
        },
      });
    if (state.agency) chips.push({ label: `Agency: ${state.agency}`, onRemove: () => { state.agency = ''; agencySelect.value = ''; fetchCalls(); } });
    state.tagFilter.forEach((tag) => chips.push({ label: `Tag: ${tag}`, onRemove: () => toggleTag(tag) }));

    if (!chips.length) {
      filterChips.classList.add('hidden');
      return;
    }
    filterChips.classList.remove('hidden');
    chips.forEach((chip) => {
      const btn = document.createElement('button');
      btn.type = 'button';
      btn.className = 'chip';
      btn.textContent = chip.label;
      btn.addEventListener('click', chip.onRemove);
      filterChips.appendChild(btn);
    });
  }

  async function fetchCalls(options = {}) {
    const { silent = false } = options;
    if (silent && state.fetchingCalls) {
      return state.fetchingCalls;
    }
    if (!silent) {
      state.loading = true;
      state.error = '';
      renderList();
    }
    const params = new URLSearchParams();
    params.set('window', state.window);
    if (state.search) params.set('q', state.search);
    if (state.status) params.set('status', state.status);
    if (state.callType) params.set('call_type', state.callType);
    if (state.tagFilter.length) params.set('tags', state.tagFilter.join(','));
    const request = (async () => {
      try {
        const res = await fetch(`/api/transcriptions?${params.toString()}`);
        if (!res.ok) {
          throw new Error('Failed to load calls');
        }
        const payload = await res.json();
        state.mapboxToken = payload.mapbox_token || state.mapboxToken;
        state.calls = dedupeCalls((payload.calls || []).map(normalizeIncident));
        state.stats = payload.stats || {};
        if (state.error) {
          state.error = '';
        }
        renderList();
        renderTagFilterOptions();
        renderFilterChips();
        renderFilterOptions();
        applyStats();
        if (state.calls.length && (!state.selected || !state.calls.find((c) => c.filename === state.selected.filename))) {
          renderDetail(state.calls[0]);
        }
      } catch (err) {
        console.error(err);
        if (!silent) {
          state.error = 'Unable to load incidents.';
          renderList();
        }
      } finally {
        if (!silent) {
          state.loading = false;
          renderList();
        }
      }
    })();
    state.fetchingCalls = request;
    return request.finally(() => {
      state.fetchingCalls = null;
    });
  }

  async function fetchSummaryStats() {
    try {
      const res = await fetch(`/api/stats/last6h?window=${encodeURIComponent(state.window)}`);
      if (!res.ok) {
        throw new Error('failed to load summary stats');
      }
      const payload = await res.json();
      state.summaryStats = payload || null;
      state.mapboxToken = payload?.mapbox_token || state.mapboxToken;
      renderSummaryBar();
    } catch (err) {
      console.error(err);
    }
  }

  async function fetchHotspots() {
    try {
      const res = await fetch(`/api/hotspots?window=${encodeURIComponent(state.window)}`);
      if (!res.ok) {
        throw new Error('failed to load hotspots');
      }
      const payload = await res.json();
      state.hotspots = Array.isArray(payload.hotspots) ? payload.hotspots : [];
      state.hotspotGeoJSON = buildHotspotCollection(state.hotspots);
      renderHotspotList(payload.window || state.window);
      refreshMapLayers();
    } catch (err) {
      console.error(err);
      state.hotspots = [];
      state.hotspotGeoJSON = { type: 'FeatureCollection', features: [] };
      renderHotspotList(state.window);
      refreshMapLayers();
    }
  }

  async function refreshSelected(filename) {
    if (!filename) return;
    const res = await fetch(`/api/transcription/${encodeURIComponent(filename)}`);
    if (!res.ok) return;
    const data = normalizeIncident(await res.json());
    state.calls = dedupeCalls(
      state.calls.map((call) => (call.filename === data.filename ? { ...call, ...data } : call))
    );
    if (!state.calls.find((call) => call.filename === data.filename)) {
      state.calls.push(data);
    }
    if (state.selected && state.selected.filename === data.filename) {
      renderDetail({ ...state.selected, ...data });
    } else {
      renderList();
    }
    if (!['queued', 'processing'].includes(data.status)) {
      stopPolling();
    }
  }

  function scheduleStatusPolling(call) {
    stopPolling();
    if (!call || !['queued', 'processing'].includes(call.status)) return;
    state.pollTimer = setInterval(() => refreshSelected(call.filename), 5000);
  }

  function refreshAll(options = {}) {
    const { silentCalls = false } = options;
    fetchCalls({ silent: silentCalls });
    fetchSummaryStats();
    fetchHotspots();
  }

  function stopAutoRefreshLoops() {
    if (state.autoRefreshTimer) {
      clearInterval(state.autoRefreshTimer);
      state.autoRefreshTimer = null;
    }
    if (state.summaryRefreshTimer) {
      clearInterval(state.summaryRefreshTimer);
      state.summaryRefreshTimer = null;
    }
    if (state.hotspotRefreshTimer) {
      clearInterval(state.hotspotRefreshTimer);
      state.hotspotRefreshTimer = null;
    }
  }

  function startAutoRefreshLoops() {
    stopAutoRefreshLoops();
    state.autoRefreshTimer = setInterval(() => {
      if (!document.hidden) {
        fetchCalls({ silent: true });
      }
    }, CALLS_REFRESH_INTERVAL);
    state.summaryRefreshTimer = setInterval(() => {
      if (!document.hidden) {
        fetchSummaryStats();
      }
    }, SUMMARY_REFRESH_INTERVAL);
    state.hotspotRefreshTimer = setInterval(() => {
      if (!document.hidden) {
        fetchHotspots();
      }
    }, HOTSPOT_REFRESH_INTERVAL);
  }

  function handleVisibilityChange() {
    if (!document.hidden) {
      refreshAll({ silentCalls: true });
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
  callTypeSelect.addEventListener('change', (e) => {
    state.callType = e.target.value;
    fetchCalls();
  });
  agencySelect.addEventListener('change', (e) => {
    state.agency = e.target.value;
    renderFilterChips();
    renderList();
  });
  sortSelect.addEventListener('change', (e) => {
    state.sort = e.target.value;
    renderList();
  });
  if (retryLoad) {
    retryLoad.addEventListener('click', () => {
      state.error = '';
      fetchCalls();
    });
  }
  refreshBtn.addEventListener('click', () => {
    refreshAll();
  });
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

  if (toggleAdvancedBtn) {
    toggleAdvancedBtn.addEventListener('click', toggleAdvancedFilters);
  }

  if (filtersOverlay) {
    filtersOverlay.addEventListener('click', closeFilters);
  }

  if (closeFiltersBtn) {
    closeFiltersBtn.addEventListener('click', closeFilters);
  }

  document.addEventListener('keydown', (evt) => {
    if (evt.key === 'Escape') closeFilters();
  });
  document.addEventListener('visibilitychange', handleVisibilityChange);
  window.addEventListener('focus', () => refreshAll({ silentCalls: true }));
  window.addEventListener('beforeunload', stopAutoRefreshLoops);

  if (mapLayerDensity) {
    mapLayerDensity.addEventListener('click', () => setMapLayerVisibility('density', !state.mapLayerVisibility.density));
  }

  if (mapLayerPoints) {
    mapLayerPoints.addEventListener('click', () => setMapLayerVisibility('points', !state.mapLayerVisibility.points));
  }

  if (mapLayerHotspots) {
    mapLayerHotspots.addEventListener('click', () => setMapLayerVisibility('hotspots', !state.mapLayerVisibility.hotspots));
  }

  window.addEventListener('resize', scheduleMapResize);

  closeFilters();
  bindSuggestions();
  syncMapLayerButtons();
  renderHotspotList(state.window);
  refreshAll();
  startAutoRefreshLoops();
})();
