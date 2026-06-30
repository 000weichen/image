/* ═══════════════════════════════════════════════════
   IMGHOST — SPA Application
   ═══════════════════════════════════════════════════ */

// ─── API Module ───
const API = {
  base: '/api',
  token() { return localStorage.getItem('imgbed_token') || ''; },
  headers() { return { 'X-API-Token': this.token() }; },
  async request(method, path, body) {
    const opts = { method, headers: this.headers() };
    if (body !== undefined) {
      opts.headers['Content-Type'] = 'application/json';
      opts.body = JSON.stringify(body);
    }
    const res = await fetch(this.base + path, opts);
    if (res.status === 401) { showTokenModal(); throw new Error('Token 无效'); }
    if (!res.ok) {
      let msg = '请求失败';
      try { msg = (await res.json()).message || msg; } catch {}
      throw new Error(msg);
    }
    return res.json();
  },
  get(p) { return this.request('GET', p); },
  post(p, b) { return this.request('POST', p, b); },
  patch(p, b) { return this.request('PATCH', p, b); },
  put(p, b) { return this.request('PUT', p, b); },
  del(p) { return this.request('DELETE', p); },
  upload(formData, onProgress) {
    return new Promise((resolve, reject) => {
      const xhr = new XMLHttpRequest();
      xhr.open('POST', this.base + '/upload');
      xhr.setRequestHeader('X-API-Token', this.token());
      xhr.upload.addEventListener('progress', (e) => {
        if (e.lengthComputable && onProgress) onProgress(Math.round((e.loaded / e.total) * 100));
      });
      xhr.onload = () => {
        if (xhr.status === 401) { showTokenModal(); return reject(new Error('Token 无效')); }
        if (xhr.status >= 200 && xhr.status < 300) {
          try { resolve(JSON.parse(xhr.responseText)); } catch { reject(new Error('解析失败')); }
        } else {
          let msg = '上传失败';
          try { msg = JSON.parse(xhr.responseText).message || msg; } catch {}
          reject(new Error(msg));
        }
      };
      xhr.onerror = () => reject(new Error('网络错误'));
      xhr.send(formData);
    });
  },
};

// ─── State ───
const state = {
  view: 'dashboard',
  stats: null,
  albums: [],
  page: 1,
  albumFilter: null,
  searchQuery: '',
  preserveManageFilter: false,
  detailImage: null,
  maxSize: 20,
  uploadResults: [],
};

// ─── Utilities ───
const $ = (id) => document.getElementById(id);
const esc = (s) => String(s ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));

function formatSize(bytes) {
  if (!bytes) return '0 B';
  const u = ['B', 'KB', 'MB', 'GB', 'TB'];
  let i = 0;
  while (bytes >= 1024 && i < u.length - 1) { bytes /= 1024; i++; }
  return `${bytes.toFixed(i === 0 ? 0 : 1)} ${u[i]}`;
}

const imgSrc = (fn) => `/preview/${fn}`;
const shareURL = (img) => img.alias_url || img.url;

function toast(msg, type = '') {
  const el = $('toast');
  el.textContent = msg;
  el.className = 'toast show ' + type;
  setTimeout(() => el.classList.remove('show'), 2500);
}

async function copyText(text, successMsg) {
  const value = String(text ?? '');
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(value);
    } else {
      fallbackCopyText(value);
    }
    toast(successMsg, 'success');
  } catch {
    try {
      fallbackCopyText(value);
      toast(successMsg, 'success');
    } catch {
      toast('复制失败，请手动复制', 'error');
    }
  }
}

function fallbackCopyText(text) {
  const ta = document.createElement('textarea');
  ta.value = text;
  ta.setAttribute('readonly', '');
  ta.style.position = 'fixed';
  ta.style.left = '-9999px';
  ta.style.top = '0';
  document.body.appendChild(ta);
  ta.focus();
  ta.select();
  ta.setSelectionRange(0, ta.value.length);
  const ok = document.execCommand('copy');
  document.body.removeChild(ta);
  if (!ok) throw new Error('copy failed');
}

function showTokenModal() {
  $('token-modal').classList.add('show');
  localStorage.removeItem('imgbed_token');
}
function hideTokenModal() { $('token-modal').classList.remove('show'); }

// ─── Data Loading ───
async function loadStats() {
  try {
    state.stats = await API.get('/stats');
    updateSidebarStorage();
    return state.stats;
  } catch { return null; }
}

async function loadAlbums() {
  try {
    const list = await API.get('/albums');
    state.albums = Array.isArray(list) ? list : [];
    return state.albums;
  } catch { state.albums = []; return []; }
}

function updateSidebarStorage() {
  const s = state.stats;
  if (!s) return;
  $('sidebar-size').textContent = formatSize(s.total_size);
  $('sidebar-count').textContent = `${s.total_images} 张图片`;
  const pct = Math.min(100, (s.total_size / (10 * 1024 * 1024 * 1024)) * 100);
  $('storage-fill').style.width = pct + '%';
}

// ─── View Router ───
const views = {};
const titles = { dashboard: '仪表盘', upload: '上传图片', manage: '图片管理', albums: '相册', settings: '设置' };

function navigate(view) {
  state.view = view;
  $('page-title').textContent = titles[view] || 'IMGHOST';
  document.querySelectorAll('.nav-item').forEach(el => {
    el.classList.toggle('active', el.dataset.view === view);
  });
  $('view').innerHTML = '';
  $('topbar-actions').innerHTML = '';
  if (views[view]) views[view]();
}

window.addEventListener('hashchange', () => {
  const v = location.hash.slice(1) || 'dashboard';
  if (v === 'manage') {
    if (!state.preserveManageFilter) {
      state.albumFilter = null;
      state.searchQuery = '';
      state.page = 1;
    }
    state.preserveManageFilter = false;
  }
  navigate(v);
});

// ═══════════════════════════════════════════════════
// VIEW: Dashboard
// ═══════════════════════════════════════════════════
views.dashboard = async function () {
  const view = $('view');
  view.innerHTML = '<div class="loading"><div class="spinner"></div>加载中…</div>';

  const [stats] = await Promise.all([loadStats(), loadAlbums()]);
  if (!stats) {
    view.innerHTML = '<div class="empty-state"><p>无法加载数据，请检查 Token 设置。</p></div>';
    return;
  }

  let recent = [];
  try {
    const data = await API.get('/images?page=1');
    recent = (data.images || []).slice(0, 6);
  } catch {}

  const albumDist = (stats.albums || []).slice(0, 6);
  const maxCount = albumDist.length ? Math.max(...albumDist.map(a => a.image_count)) : 1;
  const palette = ['#4f7cff', '#6b5cff', '#34d399', '#fbbf24', '#a78bfa', '#f97316'];

  view.innerHTML = `
    <div class="stat-grid">
      <div class="stat-card">
        <div class="stat-icon blue"><svg width="20" height="20"><use href="#ic-images"/></svg></div>
        <div class="stat-value">${stats.total_images}</div>
        <div class="stat-label">总图片</div>
      </div>
      <div class="stat-card">
        <div class="stat-icon green"><svg width="20" height="20"><use href="#ic-database"/></svg></div>
        <div class="stat-value">${formatSize(stats.total_size)}</div>
        <div class="stat-label">占用空间</div>
      </div>
      <div class="stat-card">
        <div class="stat-icon amber"><svg width="20" height="20"><use href="#ic-trending"/></svg></div>
        <div class="stat-value">${stats.total_views}</div>
        <div class="stat-label">总访问量</div>
      </div>
      <div class="stat-card">
        <div class="stat-icon purple"><svg width="20" height="20"><use href="#ic-folder"/></svg></div>
        <div class="stat-value">${(stats.albums || []).length}</div>
        <div class="stat-label">相册数</div>
      </div>
    </div>

    <div class="dashboard-row">
      <div>
        <div class="section-header">
          <h2 class="section-title">最近上传</h2>
          <a href="#upload" class="btn btn-ghost btn-sm">去上传</a>
        </div>
        <div class="recent-grid" id="recent-grid">
          ${recent.length === 0
            ? '<div class="empty-state" style="grid-column:1/-1;padding:30px"><p>还没有图片</p></div>'
            : recent.map(img => `
              <div class="recent-item" data-id="${img.id}">
                <img src="${imgSrc(img.filename)}" loading="lazy" alt="" />
                <div class="recent-info">
                  <div class="recent-name">${esc(img.original_name)}</div>
                  <div class="recent-meta">${formatSize(img.size)}</div>
                </div>
              </div>`).join('')}
        </div>
      </div>

      <div>
        <div class="section-header">
          <h2 class="section-title">相册分布</h2>
          <a href="#albums" class="btn btn-ghost btn-sm">管理</a>
        </div>
        <div class="album-dist">
          ${albumDist.length === 0
            ? '<p style="color:var(--text-muted);font-size:13px;">暂无相册</p>'
            : albumDist.map((a, i) => {
              const pct = (a.image_count / maxCount) * 100;
              return `<div class="dist-item">
                <span class="dist-name" title="${esc(a.name)}">${esc(a.name)}</span>
                <div class="dist-bar-bg"><div class="dist-bar" style="width:${pct}%;background:${palette[i % palette.length]};"></div></div>
                <span class="dist-count">${a.image_count}</span>
              </div>`;
            }).join('')}
          <div class="dist-item">
            <span class="dist-name">未分类</span>
            <div class="dist-bar-bg"><div class="dist-bar" style="width:${maxCount ? (stats.unassigned / maxCount) * 100 : 0}%;background:var(--text-muted);"></div></div>
            <span class="dist-count">${stats.unassigned || 0}</span>
          </div>
        </div>
      </div>
    </div>`;

  view.querySelectorAll('.recent-item').forEach(el => {
    el.addEventListener('click', () => {
      const img = recent.find(i => String(i.id) === el.dataset.id);
      if (img) openDetail(img);
    });
  });
};

// ═══════════════════════════════════════════════════
// VIEW: Upload
// ═══════════════════════════════════════════════════
views.upload = async function () {
  await loadAlbums();
  const view = $('view');
  const albumOpts = state.albums.map(a => `<option value="${a.id}">${esc(a.name)}</option>`).join('');

  view.innerHTML = `
    <div class="upload-view">
      <div class="drop-zone" id="drop-zone">
        <input type="file" id="file-input" multiple accept="image/*" hidden />
        <div class="drop-icon"><svg width="30" height="30"><use href="#ic-cloud-up"/></svg></div>
        <div class="drop-title">拖拽图片到这里，或点击浏览</div>
        <div class="drop-hint">支持 JPG / PNG / GIF / WEBP / BMP，单文件最大 ${state.maxSize} MB</div>
      </div>
      <div class="upload-options">
        <select id="upload-album">
          <option value="">默认（未分类）</option>
          ${albumOpts}
        </select>
      </div>
      <div class="upload-results" id="upload-results"></div>
    </div>`;

  initUploadZone();
};

function initUploadZone() {
  const dz = $('drop-zone');
  const fi = $('file-input');
  ['dragenter', 'dragover', 'dragleave', 'drop'].forEach(e => dz.addEventListener(e, (ev) => { ev.preventDefault(); ev.stopPropagation(); }));
  ['dragenter', 'dragover'].forEach(e => dz.addEventListener(e, () => dz.classList.add('dragover')));
  ['dragleave', 'drop'].forEach(e => dz.addEventListener(e, () => dz.classList.remove('dragover')));
  dz.addEventListener('drop', (e) => handleFiles(e.dataTransfer.files));
  dz.addEventListener('click', () => fi.click());
  fi.addEventListener('change', (e) => { handleFiles(e.target.files); e.target.value = ''; });
}

// Clipboard paste (global, only in upload view)
document.addEventListener('paste', (e) => {
  if (state.view !== 'upload') return;
  const items = e.clipboardData?.items;
  if (!items) return;
  const files = [];
  for (const item of items) {
    if (item.type.startsWith('image/')) {
      const f = item.getAsFile();
      if (f) files.push(f);
    }
  }
  if (files.length) { e.preventDefault(); handleFiles(files); }
});

async function handleFiles(files) {
  if (!files.length) return;
  const album = $('upload-album')?.value || null;
  for (const file of files) {
    if (file.size > state.maxSize * 1024 * 1024) {
      toast(`跳过 ${file.name}，超过 ${state.maxSize} MB`, 'error');
      continue;
    }
    const form = new FormData();
    form.append('file', file);
    if (album) form.append('album', album);
    try {
      toast(`上传 ${file.name}: 0%`);
      const img = await API.upload(form, (p) => toast(`上传 ${file.name}: ${p}%`));
      state.uploadResults.unshift(img);
      renderUploadResults();
      toast(`${file.name} 上传成功`, 'success');
    } catch (err) {
      toast(err.message, 'error');
    }
  }
  loadStats();
}

function renderUploadResults() {
  const el = $('upload-results');
  if (!el) return;
  if (!state.uploadResults.length) { el.innerHTML = ''; return; }
  el.innerHTML = `
    <div class="section-header"><h2 class="section-title">上传结果</h2></div>
    ${state.uploadResults.slice(0, 10).map(img => `
      <div class="result-item">
        <img src="${imgSrc(img.filename)}" loading="lazy" alt="" />
        <div class="result-info">
          <div class="result-name">${esc(img.original_name)}</div>
          <div class="result-meta">${formatSize(img.size)} · ${img.width || '?'}×${img.height || '?'}</div>
        </div>
        <button class="btn btn-ghost btn-sm" data-cu="${shareURL(img)}"><svg width="14" height="14"><use href="#ic-copy"/></svg></button>
        <button class="btn btn-ghost btn-sm" data-cmd="${shareURL(img)}" data-cmn="${esc(img.original_name)}">MD</button>
      </div>`).join('')}`;
  el.querySelectorAll('[data-cu]').forEach(b => {
    b.addEventListener('click', () => copyText(b.dataset.cu, 'URL 已复制'));
  });
  el.querySelectorAll('[data-cmd]').forEach(b => {
    b.addEventListener('click', () => copyText(`![${b.dataset.cmn}](${b.dataset.cmd})`, 'Markdown 已复制'));
  });
}

// ═══════════════════════════════════════════════════
// VIEW: Manage
// ═══════════════════════════════════════════════════
views.manage = async function () {
  await loadAlbums();
  const view = $('view');
  const albumOpts = state.albums.map(a => {
    const sel = state.albumFilter === a.id ? 'selected' : '';
    return `<option value="${a.id}" ${sel}>${esc(a.name)}</option>`;
  }).join('');

  view.innerHTML = `
    <div class="manage-toolbar">
      <div class="search-bar">
        <svg class="icon"><use href="#ic-search"/></svg>
        <input type="text" id="search-input" placeholder="搜索文件名 / Hash" value="${esc(state.searchQuery)}" />
      </div>
      <select id="album-filter">
        <option value="" ${state.albumFilter === null ? 'selected' : ''}>全部图片</option>
        <option value="0" ${state.albumFilter === 0 ? 'selected' : ''}>未分类</option>
        ${albumOpts}
      </select>
      <button class="btn btn-ghost btn-sm" id="refresh-btn"><svg width="14" height="14"><use href="#ic-refresh"/></svg></button>
    </div>
    <div id="gallery" class="gallery"><div class="loading"><div class="spinner"></div>加载中…</div></div>
    <div class="pagination" id="pagination"></div>`;

  let timer;
  $('search-input').addEventListener('input', (e) => {
    clearTimeout(timer);
    const val = e.target.value.trim();
    timer = setTimeout(() => { state.searchQuery = val; state.page = 1; loadImages(); }, 300);
  });
  $('album-filter').addEventListener('change', (e) => {
    const v = e.target.value;
    state.albumFilter = v === '' ? null : parseInt(v);
    state.page = 1;
    loadImages();
  });
  $('refresh-btn').addEventListener('click', () => { loadStats(); loadImages(); });
  loadImages();
};

async function loadImages() {
  const gallery = $('gallery');
  if (!gallery) return;
  const params = new URLSearchParams({ page: state.page });
  if (state.albumFilter !== null) params.set('album', state.albumFilter);
  if (state.searchQuery) params.set('q', state.searchQuery);
  gallery.innerHTML = '<div class="loading"><div class="spinner"></div>加载中…</div>';
  try {
    const data = await API.get(`/images?${params}`);
    renderGallery(data);
    renderPagination(data.total, data.page, data.page_size);
  } catch (err) {
    if (err.message !== 'Token 无效')
      gallery.innerHTML = `<div class="empty-state"><p>${esc(err.message)}</p></div>`;
  }
}

function renderGallery(data) {
  const g = $('gallery');
  const images = data.images || [];
  if (!images.length) {
    g.innerHTML = `<div class="empty-state">
      <div class="empty-icon"><svg width="28" height="28"><use href="#ic-images"/></svg></div>
      <p>暂无图片，<a href="#upload">去上传</a></p>
    </div>`;
    return;
  }

  g.innerHTML = images.map(img => `
    <div class="gallery-card" data-id="${img.id}">
      <img class="gallery-thumb" src="${imgSrc(img.filename)}" loading="lazy" alt="" data-detail="${img.id}" />
      <div class="gallery-info">
        <div class="gallery-name" title="${esc(img.original_name)}">${esc(img.original_name)}</div>
        <div class="gallery-meta">${formatSize(img.size)} · ${img.width || '?'}×${img.height || '?'} <svg><use href="#ic-eye"/></svg>${img.views}</div>
      </div>
      <select class="move-select" data-move="${img.id}">
        <option value="">${img.album_id ? '移出相册' : '移入相册'}</option>
        ${state.albums.map(a => `<option value="${a.id}" ${img.album_id === a.id ? 'selected' : ''}>${esc(a.name)}</option>`).join('')}
      </select>
      <div class="gallery-actions">
        <button class="btn btn-ghost" data-cu="${shareURL(img)}"><svg width="12" height="12"><use href="#ic-copy"/></svg></button>
        <button class="btn btn-ghost" data-cmd="${shareURL(img)}" data-cmn="${esc(img.original_name)}">MD</button>
        <button class="btn btn-ghost" data-detail="${img.id}"><svg width="12" height="12"><use href="#ic-eye"/></svg></button>
        <button class="btn btn-danger" data-del="${img.id}"><svg width="12" height="12"><use href="#ic-trash"/></svg></button>
      </div>
    </div>`).join('');

  g.querySelectorAll('[data-detail]').forEach(el => {
    el.addEventListener('click', () => {
      const img = images.find(i => String(i.id) === el.dataset.detail);
      if (img) openDetail(img);
    });
  });
  g.querySelectorAll('[data-cu]').forEach(b => {
    b.addEventListener('click', () => copyText(b.dataset.cu, 'URL 已复制'));
  });
  g.querySelectorAll('[data-cmd]').forEach(b => {
    b.addEventListener('click', () => copyText(`![${b.dataset.cmn}](${b.dataset.cmd})`, 'Markdown 已复制'));
  });
  g.querySelectorAll('[data-del]').forEach(b => {
    b.addEventListener('click', async () => {
      if (!confirm('确定删除这张图片？')) return;
      try { await API.del(`/images/${b.dataset.del}`); toast('已删除', 'success'); loadStats(); loadImages(); }
      catch (err) { toast(err.message, 'error'); }
    });
  });
  g.querySelectorAll('[data-move]').forEach(sel => {
    sel.addEventListener('change', async () => {
      try { await API.patch(`/images/${sel.dataset.move}`, { album: sel.value }); toast(sel.value ? '已移动到相册' : '已移出相册', 'success'); loadStats(); loadImages(); }
      catch (err) { toast(err.message, 'error'); }
    });
  });
}

function renderPagination(total, page, pageSize) {
  const p = $('pagination');
  if (!p) return;
  const totalPages = Math.max(1, Math.ceil(total / pageSize));
  p.innerHTML = '';
  if (totalPages <= 1) return;

  const mkBtn = (txt, disabled, active, onClick) => {
    const b = document.createElement('button');
    b.textContent = txt;
    b.disabled = disabled;
    if (active) b.classList.add('active');
    b.addEventListener('click', onClick);
    return b;
  };

  p.appendChild(mkBtn('‹', page <= 1, false, () => { state.page = page - 1; loadImages(); }));

  const maxShow = 7;
  let start = Math.max(1, page - 3);
  let end = Math.min(totalPages, start + maxShow - 1);
  if (end - start < maxShow - 1) start = Math.max(1, end - maxShow + 1);

  for (let i = start; i <= end; i++)
    p.appendChild(mkBtn(i, false, i === page, () => { state.page = i; loadImages(); }));

  p.appendChild(mkBtn('›', page >= totalPages, false, () => { state.page = page + 1; loadImages(); }));
}

// ═══════════════════════════════════════════════════
// VIEW: Albums
// ═══════════════════════════════════════════════════
views.albums = async function () {
  await Promise.all([loadStats(), loadAlbums()]);
  const view = $('view');

  view.innerHTML = `
    <div class="inline-form">
      <div class="section-header" style="margin-bottom:0">
        <h2 class="section-title">创建相册</h2>
      </div>
      <div class="inline-form-row">
        <input type="text" id="new-album-name" placeholder="相册名称" />
        <input type="text" id="new-album-desc" placeholder="描述（可选）" />
        <button class="btn btn-primary" id="create-album-btn">创建</button>
      </div>
    </div>
    <div class="album-grid" id="album-grid"><div class="loading"><div class="spinner"></div>加载中…</div></div>`;

  $('create-album-btn').addEventListener('click', createAlbum);
  $('new-album-name').addEventListener('keydown', (e) => { if (e.key === 'Enter') createAlbum(); });
  renderAlbumGrid();
};

async function createAlbum() {
  const name = $('new-album-name').value.trim();
  const desc = $('new-album-desc').value.trim();
  if (!name) { toast('请输入相册名称', 'error'); return; }
  try {
    await API.post('/albums', { name, description: desc });
    toast('相册创建成功', 'success');
    $('new-album-name').value = '';
    $('new-album-desc').value = '';
    await loadAlbums();
    renderAlbumGrid();
  } catch (err) { toast(err.message, 'error'); }
}

async function renderAlbumGrid() {
  const grid = $('album-grid');
  if (!grid) return;
  const albums = state.albums;

  if (!albums.length) {
    grid.innerHTML = `<div class="empty-state" style="grid-column:1/-1">
      <div class="empty-icon"><svg width="28" height="28"><use href="#ic-folder"/></svg></div>
      <p>还没有相册，在上方创建一个吧</p>
    </div>`;
    return;
  }

  // Fetch cover images
  const covers = await Promise.all(albums.map(async (a) => {
    try { const d = await API.get(`/images?page=1&album=${a.id}`); return d.images?.[0]?.filename || null; }
    catch { return null; }
  }));

  grid.innerHTML = albums.map((a, i) => `
    <div class="album-card" data-id="${a.id}">
      <div class="album-cover">
        ${covers[i] ? `<img src="${imgSrc(covers[i])}" loading="lazy" alt="" style="width:100%;height:100%;object-fit:cover" />` : `<svg width="32" height="32"><use href="#ic-folder"/></svg>`}
      </div>
      <div class="album-card-body">
        <div class="album-card-name">${esc(a.name)}</div>
        ${a.description ? `<div class="album-card-desc">${esc(a.description)}</div>` : ''}
        <div class="album-card-meta"><svg width="14" height="14"><use href="#ic-images"/></svg> ${a.image_count || 0} 张图片</div>
      </div>
      <div class="album-card-actions">
        <button class="btn btn-ghost btn-sm" data-av="${a.id}">查看</button>
        <button class="btn btn-ghost btn-sm" data-ae="${a.id}"><svg width="12" height="12"><use href="#ic-edit"/></svg></button>
        <button class="btn btn-danger btn-sm" data-ad="${a.id}"><svg width="12" height="12"><use href="#ic-trash"/></svg></button>
      </div>
    </div>`).join('');

  grid.querySelectorAll('.album-card').forEach(card => {
    card.addEventListener('click', (e) => {
      if (e.target.closest('button')) return;
      gotoManageWithAlbum(parseInt(card.dataset.id));
    });
  });
  grid.querySelectorAll('[data-av]').forEach(b => {
    b.addEventListener('click', () => gotoManageWithAlbum(parseInt(b.dataset.av)));
  });
  grid.querySelectorAll('[data-ae]').forEach(b => {
    b.addEventListener('click', () => {
      const album = state.albums.find(a => a.id === parseInt(b.dataset.ae));
      if (album) openAlbumEdit(album);
    });
  });
  grid.querySelectorAll('[data-ad]').forEach(b => {
    b.addEventListener('click', async () => {
      const id = parseInt(b.dataset.ad);
      const album = state.albums.find(a => a.id === id);
      if (!confirm(`确定删除相册「${album.name}」？图片不会被删除，仅变为未分类。`)) return;
      try { await API.del(`/albums/${id}`); toast('相册已删除', 'success'); await loadAlbums(); await loadStats(); renderAlbumGrid(); }
      catch (err) { toast(err.message, 'error'); }
    });
  });
}

function gotoManageWithAlbum(albumId) {
  state.albumFilter = albumId;
  state.page = 1;
  state.searchQuery = '';
  state.preserveManageFilter = true;
  if (location.hash === '#manage') {
    state.preserveManageFilter = false;
    navigate('manage');
    return;
  }
  location.hash = '#manage';
}

// ═══════════════════════════════════════════════════
// VIEW: Settings
// ═══════════════════════════════════════════════════
views.settings = function () {
  const view = $('view');
  const token = API.token();
  const masked = token ? token.slice(0, 8) + '•'.repeat(16) + token.slice(-4) : '未设置';

  view.innerHTML = `
    <div class="settings-view">
      <div class="settings-section">
        <div class="settings-section-header">认证</div>
        <div class="settings-section-body">
          <div class="setting-row">
            <div class="setting-label">API Token<small>用于保护上传和管理接口</small></div>
            <div class="setting-value"><span class="token-display">${esc(masked)}</span></div>
          </div>
          <div class="setting-row">
            <div class="setting-label">更新 Token<small>输入新 Token 后回车生效</small></div>
            <div class="setting-value">
              <input type="password" id="new-token-input" placeholder="新 Token" style="width:200px" />
              <button class="btn btn-primary btn-sm" id="update-token-btn">更新</button>
            </div>
          </div>
          <div class="setting-row">
            <div class="setting-label">清除登录<small>清除本地保存的 Token</small></div>
            <button class="btn btn-danger btn-sm" id="clear-token-btn">清除并退出</button>
          </div>
        </div>
      </div>
      <div class="settings-section">
        <div class="settings-section-header">上传限制</div>
        <div class="settings-section-body">
          <div class="setting-row">
            <div class="setting-label">单文件大小上限</div>
            <div class="setting-value"><span class="badge badge-blue">${state.maxSize} MB</span></div>
          </div>
          <div class="setting-row">
            <div class="setting-label">支持的图片格式</div>
            <div class="setting-value" style="flex-wrap:wrap;justify-content:flex-end;gap:4px">
              <span class="badge badge-green">JPEG</span>
              <span class="badge badge-green">PNG</span>
              <span class="badge badge-green">GIF</span>
              <span class="badge badge-green">WebP</span>
              <span class="badge badge-green">BMP</span>
            </div>
          </div>
          <div class="setting-row">
            <div class="setting-label">去重方式</div>
            <div class="setting-value"><span class="badge badge-muted">SHA-256</span></div>
          </div>
        </div>
      </div>
      <div class="settings-section">
        <div class="settings-section-header">关于</div>
        <div class="settings-section-body">
          <div class="setting-row"><div class="setting-label">应用名称</div><div class="setting-value">IMGHOST 图床</div></div>
          <div class="setting-row"><div class="setting-label">技术栈</div><div class="setting-value">Go · Gin · SQLite · Vanilla JS</div></div>
          <div class="setting-row"><div class="setting-label">存储后端</div><div class="setting-value"><span class="badge badge-blue">本地磁盘</span></div></div>
        </div>
      </div>
    </div>`;

  const doUpdate = () => {
    const val = $('new-token-input').value.trim();
    if (!val) { toast('Token 不能为空', 'error'); return; }
    localStorage.setItem('imgbed_token', val);
    toast('Token 已更新', 'success');
    loadStats();
    loadAlbums();
  };
  $('update-token-btn').addEventListener('click', doUpdate);
  $('new-token-input').addEventListener('keydown', (e) => { if (e.key === 'Enter') doUpdate(); });
  $('clear-token-btn').addEventListener('click', () => { localStorage.removeItem('imgbed_token'); showTokenModal(); });
};

// ═══════════════════════════════════════════════════
// Modals
// ═══════════════════════════════════════════════════
function openDetail(img) {
  state.detailImage = img;
  $('detail-img').src = imgSrc(img.filename);
  $('detail-name').textContent = img.original_name;
  $('detail-meta').innerHTML = `
    ${formatSize(img.size)} · ${img.width || '?'}×${img.height || '?'} · <svg width="12" height="12" style="display:inline;vertical-align:middle"><use href="#ic-eye"/></svg> ${img.views} 次访问<br>
    MIME: ${esc(img.mime)}<br>
    Hash: <span class="hash">${esc(img.hash)}</span><br>
    创建: ${esc(img.created_at)}`;
  $('detail-url').value = img.url;
  $('detail-alias').value = img.alias || '';
  $('detail-alias-url').value = img.alias_url || '';
  $('detail-modal').classList.add('show');
}

function openAlbumEdit(album) {
  $('album-edit-id').value = album.id;
  $('album-edit-name').value = album.name;
  $('album-edit-desc').value = album.description || '';
  $('album-edit-modal').classList.add('show');
}

// Modal close
$('detail-close').addEventListener('click', () => $('detail-modal').classList.remove('show'));
$('album-edit-close').addEventListener('click', () => $('album-edit-modal').classList.remove('show'));
$('album-edit-cancel').addEventListener('click', () => $('album-edit-modal').classList.remove('show'));
$('detail-modal').addEventListener('click', (e) => { if (e.target === e.currentTarget) e.currentTarget.classList.remove('show'); });
$('album-edit-modal').addEventListener('click', (e) => { if (e.target === e.currentTarget) e.currentTarget.classList.remove('show'); });
document.addEventListener('keydown', (e) => {
  if (e.key === 'Escape') { $('detail-modal').classList.remove('show'); $('album-edit-modal').classList.remove('show'); }
});

// Detail copy buttons
$('detail-copy-url').addEventListener('click', () => copyText($('detail-url').value, 'URL 已复制'));
$('detail-copy-url2').addEventListener('click', () => copyText($('detail-url').value, 'URL 已复制'));
$('detail-copy-md').addEventListener('click', () => copyText(`![${$('detail-name').textContent}](${$('detail-url').value})`, 'Markdown 已复制'));
$('detail-copy-alias').addEventListener('click', () => {
  const url = $('detail-alias-url').value.trim();
  if (!url) { toast('请先保存别名', 'error'); return; }
  copyText(url, '别名链接已复制');
});
$('detail-save-alias').addEventListener('click', async () => {
  const img = state.detailImage;
  if (!img) return;
  try {
    const updated = await API.patch(`/images/${img.id}/alias`, { alias: $('detail-alias').value.trim() });
    Object.assign(img, updated);
    state.detailImage = img;
    $('detail-alias').value = img.alias || '';
    $('detail-alias-url').value = img.alias_url || '';
    $('detail-url').value = img.url;
    toast(img.alias ? '别名已保存' : '别名已清除', 'success');
    if (state.view === 'manage') loadImages();
  } catch (err) {
    toast(err.message, 'error');
  }
});
$('detail-alias').addEventListener('keydown', (e) => { if (e.key === 'Enter') $('detail-save-alias').click(); });

// Album edit submit
$('album-edit-form').addEventListener('submit', async (e) => {
  e.preventDefault();
  const id = $('album-edit-id').value;
  const name = $('album-edit-name').value.trim();
  const desc = $('album-edit-desc').value.trim();
  if (!name) { toast('相册名称不能为空', 'error'); return; }
  try {
    await API.put(`/albums/${id}`, { name, description: desc });
    toast('相册已更新', 'success');
    $('album-edit-modal').classList.remove('show');
    await loadAlbums();
    if (state.view === 'albums') renderAlbumGrid();
    if (state.view === 'manage') loadImages();
  } catch (err) { toast(err.message, 'error'); }
});

// Token modal
$('token-save').addEventListener('click', () => {
  const val = $('token-input').value.trim();
  if (!val) { $('token-error').textContent = 'Token 不能为空'; return; }
  localStorage.setItem('imgbed_token', val);
  $('token-error').textContent = '';
  hideTokenModal();
  init();
});
$('token-input').addEventListener('keydown', (e) => { if (e.key === 'Enter') $('token-save').click(); });

// Sidebar clear token
$('clear-token').addEventListener('click', () => { localStorage.removeItem('imgbed_token'); showTokenModal(); });

// ─── Init ───
async function init() {
  if (!API.token()) { showTokenModal(); return; }
  await Promise.all([loadStats(), loadAlbums()]);
  const view = location.hash.slice(1) || 'dashboard';
  navigate(view);
}

init();
