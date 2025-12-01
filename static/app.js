const state = {
  page: 1,
  pageSize: 25,
  items: [],
  selected: null,
  theme: localStorage.getItem('theme') || 'dark',
};

document.addEventListener('DOMContentLoaded', () => {
  applyTheme();
  document.getElementById('theme-toggle').addEventListener('click', toggleTheme);
  document.getElementById('search').addEventListener('input', debounce(loadList, 250));
  document.getElementById('status-filter').addEventListener('change', loadList);
  document.getElementById('sort-by').addEventListener('change', loadList);
  document.getElementById('prev-page').addEventListener('click', () => changePage(-1));
  document.getElementById('next-page').addEventListener('click', () => changePage(1));
  document.getElementById('download-txt').addEventListener('click', () => download('txt'));
  document.getElementById('download-json').addEventListener('click', () => download('json'));
  document.getElementById('download-srt').addEventListener('click', () => download('srt'));
  document.getElementById('retranscribe').addEventListener('click', retranscribe);
  setupTabs();
  loadList();
});

function applyTheme() {
  const root = document.documentElement;
  if (state.theme === 'light') root.classList.add('light');
  else root.classList.remove('light');
}

function toggleTheme() {
  state.theme = state.theme === 'light' ? 'dark' : 'light';
  localStorage.setItem('theme', state.theme);
  applyTheme();
}

async function loadList() {
  const q = document.getElementById('search').value.trim();
  const status = document.getElementById('status-filter').value;
  const sort = document.getElementById('sort-by').value;
  const url = new URL('/api/transcriptions', window.location.origin);
  url.searchParams.set('page', state.page);
  url.searchParams.set('page_size', state.pageSize);
  if (q) url.searchParams.set('q', q);
  if (status) url.searchParams.set('status', status);
  if (sort) url.searchParams.set('sort', sort === 'time' ? '' : sort);
  const res = await fetch(url);
  if (!res.ok) return;
  const items = await res.json();
  state.items = items;
  renderList();
}

function renderList() {
  const container = document.getElementById('items');
  container.innerHTML = '';
  state.items.forEach((item) => {
    const div = document.createElement('div');
    div.className = 'item';
    div.innerHTML = `
      <div>${item.filename}</div>
      <div><span class="status ${item.status}">${item.status}</span></div>
      <div>${new Date(item.updated_at).toLocaleString()}</div>
    `;
    div.addEventListener('click', () => selectItem(item));
    container.appendChild(div);
  });
  document.getElementById('page-info').textContent = `Page ${state.page}`;
}

function changePage(delta) {
  if (state.page + delta < 1) return;
  state.page += delta;
  loadList();
}

async function selectItem(item) {
  const res = await fetch(`/api/transcription/${encodeURIComponent(item.filename)}`);
  const data = await res.json();
  state.selected = data;
  document.getElementById('detail-name').textContent = data.filename;
  const meta = [];
  if (data.size_bytes) meta.push(`${(data.size_bytes/1024/1024).toFixed(2)} MB`);
  if (data.duration_seconds) meta.push(`${data.duration_seconds.toFixed(1)}s`);
  if (data.hash) meta.push(`hash ${data.hash.slice(0,10)}`);
  if (data.duplicate_of) meta.push(`duplicate of ${data.duplicate_of}`);
  document.getElementById('detail-meta').textContent = meta.join(' • ');
  updateTranscript('clean');
  setActiveTab('clean');
  enableDownloads(true);
  document.getElementById('retranscribe').disabled = false;
  const player = document.getElementById('player');
  player.src = `/${encodeURIComponent(item.filename)}`;
  renderSimilar(item.filename);
  drawWaveform(player);
}

function enableDownloads(on) {
  ['download-txt','download-json','download-srt'].forEach(id => {
    document.getElementById(id).disabled = !on;
  });
}

function setupTabs() {
  document.querySelectorAll('.transcript-tabs button').forEach(btn => {
    btn.addEventListener('click', () => {
      setActiveTab(btn.dataset.tab);
      updateTranscript(btn.dataset.tab);
    });
  });
}

function setActiveTab(tab) {
  document.querySelectorAll('.transcript-tabs button').forEach(btn => {
    btn.classList.toggle('active', btn.dataset.tab === tab);
  });
}

function updateTranscript(tab) {
  const data = state.selected;
  const pre = document.getElementById('transcript-text');
  if (!data) { pre.textContent = 'Select a call to view transcript'; return; }
  let text = '';
  if (tab === 'raw') text = data.raw_transcript_text || data.transcript_text || '';
  else if (tab === 'translation') text = data.translation_text || 'No translation available yet.';
  else text = data.clean_transcript_text || data.transcript_text || '';
  if (!text) text = `Status: ${data.status}`;
  pre.textContent = text;
}

function download(format) {
  if (!state.selected) return;
  window.location = `/api/transcription/${encodeURIComponent(state.selected.filename)}/download?format=${format}`;
}

async function retranscribe() {
  if (!state.selected) return;
  await fetch(`/api/transcription/${encodeURIComponent(state.selected.filename)}/retranscribe`, { method: 'POST' });
  alert('Retranscription queued (no GroupMe alert will be sent).');
}

async function renderSimilar(filename) {
  const res = await fetch(`/api/transcription/${encodeURIComponent(filename)}/similar`);
  if (!res.ok) return;
  const sims = await res.json();
  const container = document.getElementById('similar');
  container.innerHTML = '';
  if (!sims.length) return;
  const h = document.createElement('h3');
  h.textContent = 'Similar calls';
  container.appendChild(h);
  sims.forEach(s => {
    const div = document.createElement('div');
    div.textContent = `${s.filename} (${s.score.toFixed(2)})`;
    container.appendChild(div);
  });
}

function debounce(fn, wait) {
  let t;
  return (...args) => {
    clearTimeout(t);
    t = setTimeout(() => fn.apply(this, args), wait);
  };
}

function drawWaveform(audioEl) {
  const canvas = document.getElementById('waveform');
  const ctx = canvas.getContext('2d');
  ctx.clearRect(0,0,canvas.width,canvas.height);
  audioEl.addEventListener('play', () => renderWave(ctx, audioEl));
}

async function renderWave(ctx, audioEl) {
  const audioCtx = new AudioContext();
  const source = audioCtx.createMediaElementSource(audioEl);
  const analyser = audioCtx.createAnalyser();
  analyser.fftSize = 2048;
  const bufferLength = analyser.frequencyBinCount;
  const dataArray = new Uint8Array(bufferLength);
  source.connect(analyser);
  analyser.connect(audioCtx.destination);
  function draw() {
    if (audioEl.paused) return;
    requestAnimationFrame(draw);
    analyser.getByteTimeDomainData(dataArray);
    ctx.fillStyle = 'rgba(0,0,0,0)';
    ctx.clearRect(0,0,ctx.canvas.width, ctx.canvas.height);
    ctx.lineWidth = 2;
    ctx.strokeStyle = '#60a5fa';
    ctx.beginPath();
    const sliceWidth = ctx.canvas.width * 1.0 / bufferLength;
    let x = 0;
    for(let i = 0; i < bufferLength; i++) {
      const v = dataArray[i] / 128.0;
      const y = v * ctx.canvas.height/2;
      if(i === 0) ctx.moveTo(x, y);
      else ctx.lineTo(x, y);
      x += sliceWidth;
    }
    ctx.lineTo(ctx.canvas.width, ctx.canvas.height/2);
    ctx.stroke();
  }
  draw();
}
