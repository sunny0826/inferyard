import { chromium, expect } from '@playwright/test';
import { createServer as createVite } from 'vite';
import { createServer } from 'node:http';
import assert from 'node:assert/strict';

const browser = await chromium.launch({ channel: 'chrome', headless: true });
const errors = [];
let vite;
let server;
try {
  process.env.VITE_API_MODE = 'mock';
  vite = await createVite({ server: { host: '127.0.0.1', port: 0 } });
  await vite.listen();
  const port = vite.httpServer.address().port;
  const page = await browser.newPage();
  page.on('pageerror', error => errors.push(error.message));
  await page.goto(`http://127.0.0.1:${port}/#/device`);
  await expect(page.getByText('尚无设备快照', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: '刷新设备' }).click();
  await expect(page.getByRole('button', { name: '刷新中…' })).toBeDisabled();
  await expect(page.getByRole('heading', { name: 'Demo GGUF', exact: true })).toBeVisible();
  await expect(page.getByRole('heading', { name: '缺测与来源' })).toBeVisible();
  await page.getByRole('link', { name: 'Settings', exact: true }).click();
  await page.getByLabel('results_root', { exact: true }).fill('/changed/results');
  await page.getByRole('button', { name: '保存设置' }).click();
  await expect(page.getByText('设置已保存', { exact: true })).toBeVisible();
  await expect(page.getByText('/changed/reports', { exact: true })).toBeVisible();
  await page.getByLabel('inferyard_path', { exact: true }).fill('/mock/blocked');
  await page.getByRole('button', { name: '保存设置' }).click();
  await expect(page.getByRole('alert')).toContainText('mock_probe_blocked');
  await page.getByLabel('inferyard_path', { exact: true }).fill('relative');
  await page.getByRole('button', { name: '保存设置' }).click();
  await expect(page.getByRole('alert')).toContainText('非空绝对路径');
  await page.getByLabel('inferyard_path', { exact: true }).fill('');
  await page.getByRole('button', { name: '保存设置' }).click();
  await expect(page.getByText('设置已保存', { exact: true })).toBeVisible();
  for (const name of ['Dashboard', 'Runs', 'Reports', 'Models']) {
    await page.getByRole('link', { name, exact: true }).click();
    await expect(page.getByText('此页面尚未实现。', { exact: true })).toBeVisible();
  }
  await page.getByRole('link', { name: 'Settings', exact: true }).click();
  await expect(page.getByLabel('results_root', { exact: true })).toHaveValue('/changed/results');
  assert.equal(await page.evaluate(() => localStorage.length), 0);
  await page.setViewportSize({ width: 390, height: 844 });
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
  console.log('PASS: mock empty state, refresh event, device sections, settings save/error, routes, memory persistence, mobile layout');
  await page.close();
  await vite.close();

  process.env.VITE_API_MODE = 'real';
  vite = await createVite({ server: { middlewareMode: true }, appType: 'spa' });
  let eventConnections = 0;
  let deviceReads = 0;
  let settingsReads = 0;
  let lastEventId;
  let authorization;
  let eventToken;
  const settings = { settings: { inferyard_path: null, results_root: null, models_root: null }, effective: { inferyard_path: null, inferyard_version: null, inferyard_status: 'not_found', results_root: null, models_root: null, reports_root: null } };
  server = createServer((req, res) => {
    if (req.url.startsWith('/api/events')) {
      eventToken = new URL(req.url, 'http://localhost').searchParams.get('token');
      res.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache' });
      eventConnections++;
      if (eventConnections === 1) {
        res.end('retry: 100\nid: 41\nevent: device.updated\ndata: {"snapshot":null,"captured_at":"fixture-event","error":null}\n\n');
      } else {
        lastEventId = req.headers['last-event-id'];
        res.write('id: 42\nevent: sync.reset\ndata: {"reason":"buffer_gap"}\n\n');
      }
    } else if (req.url.startsWith('/api/')) {
      authorization = req.headers.authorization;
      res.setHeader('Content-Type', 'application/json');
      if (req.url === '/api/device') { deviceReads++; res.end(JSON.stringify({ snapshot: null, captured_at: eventConnections > 1 ? 'REST rebuilt' : null, refreshing: false, last_error: null })); }
      else if (req.url === '/api/settings') { settingsReads++; res.end(JSON.stringify(settings)); }
      else { res.statusCode = 422; res.end(JSON.stringify({ error: { code: 'cli_blocked', message: 'Fixture refresh blocked', limitations: ['fixture_limit'], details: null } })); }
    } else vite.middlewares(req, res);
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const real = await browser.newPage();
  real.on('pageerror', error => errors.push(error.message));
  await real.goto(`http://127.0.0.1:${server.address().port}/?token=fixture-token#/device`);
  await expect(real.getByRole('status')).toContainText('REST rebuilt');
  assert.equal(lastEventId, '41');
  assert.equal(authorization, 'Bearer fixture-token');
  assert.equal(eventToken, 'fixture-token');
  assert.ok(deviceReads >= 2 && settingsReads >= 2);
  await real.getByRole('button', { name: '刷新设备' }).click();
  await expect(real.getByRole('alert')).toContainText('Fixture refresh blocked');
  await expect(real.getByRole('alert')).toContainText('fixture_limit');
  assert.equal(await real.evaluate(() => localStorage.length), 0);
  await real.close();
  assert.deepEqual(errors, []);
  console.log('PASS: REST Bearer, SSE query token, native Last-Event-ID reconnect, sync.reset REST rebuild, API error body, no console exceptions');
} finally {
  await browser.close();
  server?.closeAllConnections();
  if (server) await new Promise(resolve => server.close(resolve));
  await vite?.close();
}
