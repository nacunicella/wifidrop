/* WiFiDrop UI. Vanilla JS, no build step. Talks to the local API with the
   PIN as a header; the PIN arrives either embedded in the QR URL (?k=) or is
   typed by the user once and kept in localStorage. */
(() => {
  'use strict';

  const $ = (id) => document.getElementById(id);
  const els = {
    deviceName: $('deviceName'), installBtn: $('installBtn'), quitBtn: $('quitBtn'),
    pinCard: $('pinCard'), pinForm: $('pinForm'), pinInput: $('pinInput'), pinError: $('pinError'), pinDeviceName: $('pinDeviceName'),
    app: $('app'), target: $('target'), drop: $('drop'), fileInput: $('fileInput'), transfers: $('transfers'),
    pairCard: $('pairCard'), togglePair: $('togglePair'), pairBody: $('pairBody'), qr: $('qr'), urlSelect: $('urlSelect'), pinValue: $('pinValue'),
    peers: $('peers'), manualPeer: $('manualPeer'), files: $('files'), dirLabel: $('dirLabel'), toasts: $('toasts'),
    peerPinDialog: $('peerPinDialog'), peerPinName: $('peerPinName'), peerPinInput: $('peerPinInput'),
    manualDialog: $('manualDialog'), manualAddr: $('manualAddr'),
  };

  const state = {
    info: null,
    key: '',
    peers: [],
    manualPeers: load('manualPeers', []),
    peerPins: load('peerPins', {}),
    files: [],
  };

  function load(k, def) { try { const v = localStorage.getItem('wifidrop.' + k); return v ? JSON.parse(v) : def; } catch { return def; } }
  function save(k, v) { try { localStorage.setItem('wifidrop.' + k, JSON.stringify(v)); } catch {} }

  // ---- key handling ------------------------------------------------------
  const params = new URLSearchParams(location.search);
  if (params.get('k')) {
    state.key = params.get('k');
    save('key', state.key);
    history.replaceState(null, '', location.pathname);
  } else {
    state.key = load('key', '');
  }
  if (params.get('shared')) {
    history.replaceState(null, '', location.pathname);
    setTimeout(() => toast(`Recibido${params.get('shared') === '1' ? '' : 's'} desde Compartir`), 300);
  }

  function headers(extra = {}) {
    const h = { ...extra };
    if (state.key) h['X-WifiDrop-Key'] = state.key;
    return h;
  }
  async function api(path, opts = {}) {
    const res = await fetch(path, { ...opts, headers: headers(opts.headers || {}) });
    if (res.status === 401) throw Object.assign(new Error('unauthorized'), { status: 401 });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(data.error || res.statusText);
    return data;
  }
  function shareKeyWithSW() {
    if (navigator.serviceWorker && navigator.serviceWorker.controller) {
      navigator.serviceWorker.controller.postMessage({ type: 'key', key: state.key });
    }
  }

  // ---- boot --------------------------------------------------------------
  async function boot() {
    let info;
    try {
      info = await api('/api/info');
    } catch (e) {
      toast('No se puede contactar con el servidor: ' + e.message);
      return;
    }
    state.info = info;
    els.deviceName.textContent = info.name;
    els.pinDeviceName.textContent = info.name;
    document.title = 'WiFiDrop · ' + info.name;

    if (info.local && info.pin) {
      state.key = info.pin; // the page on the host machine uses the PIN directly
      shareKeyWithSW();
    }
    if (!info.authed && !(info.local && info.pin)) {
      showPin();
      return;
    }
    showApp(info);
  }

  function showPin() {
    els.pinCard.hidden = false;
    els.app.hidden = true;
    els.pinInput.focus();
  }

  els.pinForm.addEventListener('submit', async (e) => {
    e.preventDefault();
    const pin = els.pinInput.value.trim();
    els.pinError.hidden = true;
    try {
      const res = await fetch('/api/auth', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ pin }) });
      if (!res.ok) throw new Error((await res.json()).error || 'PIN incorrecto');
      state.key = pin; save('key', pin); shareKeyWithSW();
      els.pinCard.hidden = true;
      boot();
    } catch (err) {
      els.pinError.textContent = err.message; els.pinError.hidden = false;
      els.pinInput.select();
    }
  });

  function showApp(info) {
    els.pinCard.hidden = true;
    els.app.hidden = false;
    els.dirLabel.textContent = info.dir || '';
    els.pairCard.hidden = !info.local;
    els.quitBtn.hidden = !info.local;
    if (info.local) renderPairing(info);
    refreshPeers(); refreshFiles();
    setInterval(refreshPeers, 3000);
    setInterval(refreshFiles, 5000);
    connectEvents();
  }

  // ---- pairing (host machine only) --------------------------------------
  function renderPairing(info) {
    els.pinValue.textContent = info.pin || '—';
    els.urlSelect.innerHTML = '';
    (info.urls || []).forEach((u) => {
      const o = document.createElement('option'); o.value = u; o.textContent = u; els.urlSelect.appendChild(o);
    });
    if (!info.urls || !info.urls.length) {
      const o = document.createElement('option'); o.textContent = 'Sin conexión Wi‑Fi detectada'; els.urlSelect.appendChild(o);
    }
    updateQR();
    const wasOpen = load('pairOpen', true);
    els.pairBody.hidden = !wasOpen;
    els.togglePair.textContent = wasOpen ? 'Ocultar' : 'Mostrar';
  }
  function updateQR() {
    const u = els.urlSelect.value;
    if (!u || !u.startsWith('http')) { els.qr.removeAttribute('src'); return; }
    els.qr.src = '/api/qr?pin=1&url=' + encodeURIComponent(u) + '&t=' + Date.now();
  }
  els.urlSelect.addEventListener('change', updateQR);
  els.togglePair.addEventListener('click', () => {
    const open = els.pairBody.hidden;
    els.pairBody.hidden = !open; save('pairOpen', open);
    els.togglePair.textContent = open ? 'Ocultar' : 'Mostrar';
  });

  // ---- peers -------------------------------------------------------------
  async function refreshPeers() {
    try {
      const { peers } = await api('/api/peers');
      state.peers = peers;
    } catch (e) {
      if (e.status === 401) { showPin(); return; }
    }
    renderPeers();
  }
  function allTargets() {
    const list = [{ id: 'self', name: state.info.name + (state.info.local ? ' (este equipo)' : ''), os: state.info.os, self: true }];
    state.peers.forEach((p) => list.push({ id: p.id, name: p.name, os: p.os, addr: p.addr }));
    state.manualPeers.forEach((addr) => {
      if (!state.peers.some((p) => p.addr === addr)) list.push({ id: 'addr:' + addr, name: addr, os: '', addr, manual: true });
    });
    return list;
  }
  const osIcon = (os) => ({ windows: '🖥️', darwin: '💻', linux: '🐧', android: '📱', ios: '📱' }[os] || '📡');
  function renderPeers() {
    const prev = els.target.value;
    els.target.innerHTML = '';
    const targets = allTargets();
    targets.forEach((t) => {
      const o = document.createElement('option'); o.value = t.id; o.textContent = t.name; els.target.appendChild(o);
    });
    if ([...els.target.options].some((o) => o.value === prev)) els.target.value = prev;

    els.peers.innerHTML = '';
    const others = targets.filter((t) => !t.self);
    if (!others.length) {
      els.peers.innerHTML = '<li class="muted">Ningún otro equipo con WiFiDrop en esta red. Ábrelo en otro ordenador o añádelo por IP.</li>';
      return;
    }
    others.forEach((t) => {
      const li = document.createElement('li');
      li.innerHTML = `<span class="peer-os">${osIcon(t.os)}</span><span class="peer-name"></span>`;
      li.querySelector('.peer-name').innerHTML = `${escapeHtml(t.name)}<span class="peer-addr">${escapeHtml(t.addr)}${t.manual ? ' · manual' : ''}</span>`;
      const btn = document.createElement('button'); btn.className = 'btn-sm'; btn.textContent = 'Enviar…';
      btn.onclick = () => { els.target.value = t.id; els.fileInput.click(); };
      li.appendChild(btn);
      if (t.manual) {
        const del = document.createElement('button'); del.className = 'btn-sm danger'; del.textContent = '✕'; del.title = 'Quitar';
        del.onclick = () => { state.manualPeers = state.manualPeers.filter((a) => a !== t.addr); save('manualPeers', state.manualPeers); renderPeers(); };
        li.appendChild(del);
      }
      els.peers.appendChild(li);
    });
  }
  els.manualPeer.addEventListener('click', () => {
    els.manualAddr.value = '';
    els.manualDialog.showModal();
  });
  els.manualDialog.addEventListener('close', () => {
    if (els.manualDialog.returnValue !== 'ok') return;
    const addr = els.manualAddr.value.trim().replace(/^https?:\/\//, '').replace(/\/$/, '');
    if (!addr) return;
    if (!state.manualPeers.includes(addr)) { state.manualPeers.push(addr); save('manualPeers', state.manualPeers); }
    renderPeers();
  });

  // ---- files -------------------------------------------------------------
  async function refreshFiles() {
    try {
      const { files } = await api('/api/files');
      state.files = files;
      renderFiles();
    } catch (e) {
      if (e.status === 401) showPin();
    }
  }
  function renderFiles() {
    els.files.innerHTML = '';
    if (!state.files.length) { els.files.innerHTML = '<li class="muted">Aún no hay archivos.</li>'; return; }
    state.files.forEach((f) => {
      const li = document.createElement('li');
      const href = '/api/files/' + encodeURIComponent(f.name) + (state.key && !state.info.local ? '?key=' + encodeURIComponent(state.key) : '');
      li.innerHTML = `<span class="peer-os">${fileIcon(f.name)}</span>
        <span class="file-name"></span>
        <a class="btn-sm" download>Guardar</a>
        <button class="btn-sm danger" title="Eliminar">✕</button>`;
      li.querySelector('.file-name').innerHTML = `${escapeHtml(f.name)}<span class="file-meta">${humanSize(f.size)} · ${timeAgo(f.modified)}</span>`;
      const a = li.querySelector('a'); a.href = href; a.setAttribute('download', f.name);
      li.querySelector('button').onclick = async () => {
        if (!confirm(`¿Eliminar "${f.name}" de ${state.info.name}?`)) return;
        try { await api('/api/files/' + encodeURIComponent(f.name), { method: 'DELETE' }); refreshFiles(); }
        catch (e) { toast('No se pudo eliminar: ' + e.message); }
      };
      els.files.appendChild(li);
    });
  }
  function fileIcon(name) {
    const ext = (name.split('.').pop() || '').toLowerCase();
    if (/^(jpe?g|png|gif|webp|heic|svg|bmp)$/.test(ext)) return '🖼️';
    if (/^(mp4|mov|mkv|webm|avi)$/.test(ext)) return '🎬';
    if (/^(mp3|m4a|wav|flac|ogg|aac)$/.test(ext)) return '🎵';
    if (/^(pdf)$/.test(ext)) return '📕';
    if (/^(zip|rar|7z|tar|gz)$/.test(ext)) return '🗜️';
    if (/^(doc|docx|txt|md|odt|rtf)$/.test(ext)) return '📄';
    if (/^(xls|xlsx|csv|ods)$/.test(ext)) return '📊';
    if (/^(apk|exe|dmg|msi|deb)$/.test(ext)) return '📦';
    return '📎';
  }

  // ---- sending -----------------------------------------------------------
  els.drop.addEventListener('click', () => els.fileInput.click());
  els.fileInput.addEventListener('change', () => { sendFiles([...els.fileInput.files]); els.fileInput.value = ''; });
  ['dragenter', 'dragover'].forEach((ev) => els.drop.addEventListener(ev, (e) => { e.preventDefault(); els.drop.classList.add('over'); }));
  ['dragleave', 'drop'].forEach((ev) => els.drop.addEventListener(ev, (e) => { e.preventDefault(); els.drop.classList.remove('over'); }));
  els.drop.addEventListener('drop', (e) => sendFiles([...e.dataTransfer.files]));
  document.addEventListener('paste', (e) => {
    const files = [...(e.clipboardData?.files || [])];
    if (files.length) sendFiles(files);
  });

  async function sendFiles(files) {
    files = files.filter((f) => f.size > 0 || f.type);
    if (!files.length) return;
    const targetId = els.target.value;
    const target = allTargets().find((t) => t.id === targetId) || allTargets()[0];
    let url, pin = '';
    if (target.self) {
      url = '/api/upload';
    } else {
      pin = state.peerPins[target.addr] || '';
      if (!pin && !state.info.open) {
        pin = await askPeerPin(target.name);
        if (pin === null) return;
      }
      url = '/api/send?' + (target.manual ? 'addr=' + encodeURIComponent(target.addr) : 'peer=' + encodeURIComponent(target.id)) + '&pin=' + encodeURIComponent(pin);
    }
    // Send files in parallel, a few at a time, so a big file never blocks small ones.
    const queue = files.slice();
    const workers = Array.from({ length: Math.min(3, queue.length) }, async () => {
      while (queue.length) {
        const f = queue.shift();
        const ok = await uploadOne(f, url, target);
        if (ok === 'unauthorized') {
          delete state.peerPins[target.addr]; save('peerPins', state.peerPins);
          const p = await askPeerPin(target.name);
          if (p === null) { queue.length = 0; return; }
          url = url.replace(/&pin=[^&]*/, '&pin=' + encodeURIComponent(p));
          queue.unshift(f);
        }
      }
    });
    await Promise.all(workers);
  }

  function uploadOne(file, url, target) {
    return new Promise((resolve) => {
      const li = document.createElement('li'); li.className = 'transfer';
      li.innerHTML = `<span class="name"></span><span class="status">0%</span><div class="bar"><i></i></div>`;
      li.querySelector('.name').textContent = `${file.name} → ${target.name}`;
      els.transfers.prepend(li);
      const status = li.querySelector('.status'), bar = li.querySelector('.bar i');
      const started = Date.now();

      const form = new FormData();
      form.append('file', file, file.name);
      const xhr = new XMLHttpRequest();
      xhr.open('POST', url);
      Object.entries(headers()).forEach(([k, v]) => xhr.setRequestHeader(k, v));
      xhr.upload.onprogress = (e) => {
        if (!e.lengthComputable) return;
        const pct = Math.round((e.loaded / e.total) * 100);
        const secs = (Date.now() - started) / 1000;
        const speed = secs > 0.5 ? ` · ${humanSize(e.loaded / secs)}/s` : '';
        status.textContent = `${pct}%${speed}`;
        bar.style.width = pct + '%';
      };
      xhr.onload = () => {
        if (xhr.status === 200) {
          const secs = Math.max((Date.now() - started) / 1000, 0.1);
          li.classList.add('done'); status.textContent = `✓ ${humanSize(file.size)} · ${humanSize(file.size / secs)}/s`; bar.style.width = '100%';
          if (target.self) refreshFiles();
          setTimeout(() => li.remove(), 15000);
          resolve(true);
        } else if (xhr.status === 401 && !target.self) {
          li.remove(); resolve('unauthorized');
        } else {
          let msg = xhr.statusText;
          try { msg = JSON.parse(xhr.responseText).error || msg; } catch {}
          if (xhr.status === 401) { showPin(); }
          li.classList.add('failed'); status.textContent = 'Error: ' + msg;
          resolve(false);
        }
      };
      xhr.onerror = () => { li.classList.add('failed'); status.textContent = 'Error de red'; resolve(false); };
      xhr.send(form);
    });
  }

  function askPeerPin(name) {
    return new Promise((resolve) => {
      els.peerPinName.textContent = name;
      els.peerPinInput.value = '';
      els.peerPinDialog.showModal();
      els.peerPinDialog.addEventListener('close', function onClose() {
        els.peerPinDialog.removeEventListener('close', onClose);
        if (els.peerPinDialog.returnValue !== 'ok') return resolve(null);
        const pin = els.peerPinInput.value.trim();
        const target = allTargets().find((t) => t.name === name);
        if (target && target.addr) { state.peerPins[target.addr] = pin; save('peerPins', state.peerPins); }
        resolve(pin);
      });
    });
  }

  // ---- live events -------------------------------------------------------
  function connectEvents() {
    if (!window.EventSource) return;
    const es = new EventSource('/api/events' + (state.key && !state.info.local ? '?key=' + encodeURIComponent(state.key) : ''));
    es.addEventListener('received', (e) => {
      const d = JSON.parse(e.data);
      toast(`📥 ${d.name} (${humanSize(d.size)}) de ${d.from}`);
      refreshFiles();
      if (navigator.vibrate) navigator.vibrate(30);
    });
    es.addEventListener('deleted', refreshFiles);
    es.onerror = () => { es.close(); setTimeout(connectEvents, 5000); };
  }

  // ---- quit (host machine only) -----------------------------------------
  els.quitBtn.addEventListener('click', async () => {
    if (!confirm('¿Cerrar WiFiDrop? El móvil y los demás equipos dejarán de ver este ordenador.')) return;
    try {
      await api('/api/quit', { method: 'POST' });
      document.body.innerHTML = '<main><section class="card"><h2>WiFiDrop se ha cerrado</h2><p class="muted">Puedes cerrar esta pestaña. Para volver a usarlo, abre WiFiDrop desde el acceso directo.</p></section></main>';
    } catch (e) {
      toast('No se pudo cerrar: ' + e.message);
    }
  });

  // ---- install prompt ----------------------------------------------------
  let deferredPrompt = null;
  window.addEventListener('beforeinstallprompt', (e) => {
    e.preventDefault(); deferredPrompt = e; els.installBtn.hidden = false;
  });
  els.installBtn.addEventListener('click', async () => {
    if (!deferredPrompt) return;
    deferredPrompt.prompt();
    await deferredPrompt.userChoice;
    deferredPrompt = null; els.installBtn.hidden = true;
  });
  if (navigator.serviceWorker) {
    navigator.serviceWorker.register('/sw.js').then(() => shareKeyWithSW()).catch(() => {});
    navigator.serviceWorker.addEventListener('message', (e) => { if (e.data?.type === 'needKey') shareKeyWithSW(); });
  }

  // ---- utils -------------------------------------------------------------
  function humanSize(n) {
    if (!n && n !== 0) return '';
    const u = ['B', 'KB', 'MB', 'GB', 'TB']; let i = 0;
    while (n >= 1024 && i < u.length - 1) { n /= 1024; i++; }
    return (i === 0 ? n : n.toFixed(1)) + ' ' + u[i];
  }
  function timeAgo(iso) {
    const s = (Date.now() - new Date(iso).getTime()) / 1000;
    if (s < 60) return 'ahora mismo';
    if (s < 3600) return `hace ${Math.floor(s / 60)} min`;
    if (s < 86400) return `hace ${Math.floor(s / 3600)} h`;
    return new Date(iso).toLocaleDateString();
  }
  function escapeHtml(s) { return String(s ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c])); }
  function toast(msg) {
    const t = document.createElement('div'); t.className = 'toast'; t.textContent = msg;
    els.toasts.appendChild(t); setTimeout(() => t.remove(), 4500);
  }

  boot();
})();
