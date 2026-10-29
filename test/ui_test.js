const { chromium } = require('playwright');
const path = require('path');
(async () => {
  const browser = await chromium.launch();
  const errors = [];
  // Desktop (loopback) view
  const ctx = await browser.newContext({ viewport: { width: 1100, height: 900 } });
  const page = await ctx.newPage();
  page.on('pageerror', (e) => errors.push('desktop: ' + e.message));
  page.on('console', (m) => { if (m.type() === 'error') errors.push('desktop console: ' + m.text()); });
  await page.goto('http://localhost:8750/');
  await page.waitForSelector('#app:not([hidden])');
  await page.waitForFunction(() => document.querySelectorAll('#peers li').length > 0 && !document.querySelector('#peers li').classList.contains('muted'));
  await page.waitForTimeout(800);
  await page.screenshot({ path: 'ui_desktop.png', fullPage: true });
  // Upload a file to self through the UI
  await page.setInputFiles('#fileInput', path.join(__dirname, 'fixtures', 'nota.txt'));
  await page.waitForSelector('.transfer.done');
  await page.waitForFunction(() => [...document.querySelectorAll('#files li')].some(li => li.textContent.includes('nota.txt')));
  // Send to the peer through the UI with the PIN dialog
  await page.selectOption('#target', { index: 1 });
  const dlg = page.locator('#peerPinDialog');
  await page.setInputFiles('#fileInput', path.join(__dirname, 'fixtures', 'big.bin'));
  await dlg.waitFor({ state: 'visible' });
  await page.fill('#peerPinInput', '222222');
  await page.click('#peerPinDialog button[value=ok]');
  await page.waitForSelector('.transfer.done', { timeout: 15000 });
  const transferText = await page.locator('.transfer.done .status').first().textContent();
  console.log('desktop transfer status:', transferText);
  await page.screenshot({ path: 'ui_desktop_after.png', fullPage: true });

  // Phone view: non-loopback address, no PIN -> PIN screen
  const phone = await browser.newContext({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true, deviceScaleFactor: 2 });
  const p = await phone.newPage();
  p.on('pageerror', (e) => errors.push('phone: ' + e.message));
  p.on('console', (m) => { if (m.type() === 'error') errors.push('phone console: ' + m.text()); });
  await p.goto('http://192.0.2.2:8750/');
  await p.waitForSelector('#pinCard:not([hidden])');
  await p.screenshot({ path: 'ui_phone_pin.png' });
  await p.fill('#pinInput', '123456');
  await p.click('#pinForm button');
  await p.waitForSelector('#pinError:not([hidden])');
  console.log('wrong pin message:', await p.locator('#pinError').textContent());
  await p.fill('#pinInput', '111111');
  await p.click('#pinForm button');
  await p.waitForSelector('#app:not([hidden])');
  await p.waitForFunction(() => [...document.querySelectorAll('#files li')].some(li => li.textContent.includes('nota.txt')));
  await p.waitForTimeout(500);
  await p.screenshot({ path: 'ui_phone_app.png' });
  // Phone with QR link (?k=) in a fresh context
  const phone2 = await browser.newContext({ viewport: { width: 390, height: 844 }, isMobile: true });
  const p2 = await phone2.newPage();
  p2.on('pageerror', (e) => errors.push('phone2: ' + e.message));
  await p2.goto('http://192.0.2.2:8750/?k=111111');
  await p2.waitForSelector('#app:not([hidden])');
  console.log('phone2 url after key:', p2.url());
  // Download link works with key
  const href = await p2.locator('#files a').first().getAttribute('href');
  const res = await p2.request.get('http://192.0.2.2:8750' + href);
  console.log('phone download status:', res.status(), 'bytes', (await res.body()).length);
  const sw = await p2.evaluate(async () => navigator.serviceWorker ? ((await navigator.serviceWorker.getRegistration()) ? 'registered' : 'none') : 'unavailable (insecure context)');
  const swLocal = await page.evaluate(async () => (await navigator.serviceWorker.getRegistration()) ? 'registered' : 'none');
  console.log('service worker on localhost:', swLocal);
  console.log('service worker:', sw);
  const manifest = await p2.request.get('http://192.0.2.2:8750/manifest.webmanifest');
  console.log('manifest:', manifest.status(), manifest.headers()['content-type']);
  console.log('errors:', errors);
  await browser.close();
})().catch((e) => { console.error('FAILED', e); process.exit(1); });
