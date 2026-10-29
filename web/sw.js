// Service worker: caches the app shell so the PWA opens instantly, and
// handles files shared to the app from the phone's "Share" menu.
const CACHE = 'wifidrop-v1';
const SHELL = ['/', '/index.html', '/style.css', '/app.js', '/icon.svg', '/icon-192.png', '/icon-512.png', '/manifest.webmanifest'];

self.addEventListener('install', (e) => {
  e.waitUntil(caches.open(CACHE).then((c) => c.addAll(SHELL)).then(() => self.skipWaiting()));
});

self.addEventListener('activate', (e) => {
  e.waitUntil(
    caches.keys().then((keys) => Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k))))
      .then(() => self.clients.claim())
  );
});

self.addEventListener('fetch', (e) => {
  const url = new URL(e.request.url);
  if (url.origin !== self.location.origin) return;

  // Files shared from the system "Share" sheet: forward to the API with the
  // stored PIN and show the app.
  if (e.request.method === 'POST' && url.pathname === '/share-target') {
    e.respondWith((async () => {
      const form = await e.request.formData();
      const files = form.getAll('file');
      const key = await getKey();
      if (files.length) {
        const body = new FormData();
        files.forEach((f) => body.append('file', f, f.name));
        await fetch('/api/upload', { method: 'POST', body, headers: key ? { 'X-WifiDrop-Key': key } : {} });
      }
      return Response.redirect('/?shared=' + files.length, 303);
    })());
    return;
  }

  if (e.request.method !== 'GET' || url.pathname.startsWith('/api/')) return;

  // Network first for the shell, cache as fallback (works when the server is
  // off, so the user sees the app instead of a browser error).
  e.respondWith(
    fetch(e.request).then((res) => {
      if (res.ok) caches.open(CACHE).then((c) => c.put(e.request, res.clone()));
      return res;
    }).catch(() => caches.match(e.request, { ignoreSearch: true }))
  );
});

// The PIN lives in the page's localStorage, which a worker cannot read; the
// page mirrors it here through a message.
let storedKey = '';
self.addEventListener('message', (e) => {
  if (e.data && e.data.type === 'key') storedKey = e.data.key || '';
});
async function getKey() {
  if (storedKey) return storedKey;
  const clients = await self.clients.matchAll({ type: 'window' });
  for (const c of clients) {
    c.postMessage({ type: 'needKey' });
  }
  await new Promise((r) => setTimeout(r, 300));
  return storedKey;
}
