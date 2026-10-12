import { createServer } from 'node:http';
import { expect } from '@playwright/test';
import assert from 'node:assert/strict';
export async function verifyAssetsWire(browser, vite, errors) {
  const connections = new Set(); const requests = []; let seq = 0; let reportReads = 0; let downloadReads = 0;
  let heldReport; let holdDownloads = false; let heldDownloads;
  const run = { run_id: 'source', path: '/results/source', batch_id: null, status: 'complete', sealed: true, completeness: 'complete', stop_reason: null, model_name: null, model_path: null, engine: null, bundle: 'zh-core', score: null, relation: null, parent_run_id: null, started_at: null, finished_at: null, resumable: false };
  const history = { results_root: '/results', scanned_at: 'fixture', batches: [], standalone_runs: Array.from({ length: 101 }, (_, i) => ({ ...run, run_id: `source-${i}`, path: `/results/source-${i}` })) };
  const report = { report_id: 'report-20261012T100000Z', path: '/reports/report-20261012T100000Z', created_at: null, run_ids: ['source', 'removed'], run_paths: ['/results/source', null], html_url: '/reports/report-20261012T100000Z/report.html' };
  let reports = [report];
  const task = { task_id: 'wire-download', state: 'downloading', source_url: `https://huggingface.co/demo/model/blob/${'a'.repeat(40)}/model.gguf`, out_name: 'wire-model', out_dir: '/models/wire-model', file_name: null, downloaded_bytes: 0, bytes_per_second: null, started_at: '2026-10-12T10:00:00Z', finished_at: null, result: null, error: null };
  let downloads = [task];
  const settings = { settings: { inferyard_path: null, results_root: null, models_root: null }, effective: { inferyard_path: null, inferyard_version: null, inferyard_status: 'not_found', results_root: null, models_root: null, reports_root: null } };
  function emit(name, data) { for (const res of connections) res.write(`id: ${++seq}\nevent: ${name}\ndata: ${JSON.stringify(data)}\n\n`); }
  function fail(res, status, code, reason) { res.statusCode = status; res.end(JSON.stringify({ error: { code, message: 'Fixture operation rejected', limitations: ['wire_limit'], details: { reason } } })); }
  const server = createServer(async (req, res) => {
    const path = req.url.split('?')[0];
    if (path === '/api/events') { res.writeHead(200, { 'Content-Type': 'text/event-stream' }); res.write(': ready\n\n'); connections.add(res); req.on('close', () => connections.delete(res)); return; }
    if (path.startsWith('/reports/')) { res.setHeader('Content-Type', 'text/html'); res.end('<h1>Fixture report opened</h1>'); return; }
    if (!path.startsWith('/api/')) { vite.middlewares(req, res); return; }
    res.setHeader('Content-Type', 'application/json');
    if (req.method === 'POST') {
      let raw = ''; for await (const chunk of req) raw += chunk;
      const body = raw ? JSON.parse(raw) : null; requests.push({ path, body, authorization: req.headers.authorization });
      if (path === '/api/reports') {
        if (body.run_paths.includes('/results/source-2')) { fail(res, 422, 'cli_blocked', 'report_blocked'); return; }
        heldReport = res; return;
      }
      if (path === '/api/downloads') {
        if (body.out_name === 'occupied') { fail(res, 409, 'conflict', 'output_exists'); return; }
        const created = { ...task, task_id: 'created-download', out_name: body.out_name, source_url: body.source_url };
        downloads.push(created); emit('download.state', { task: { ...created, state: 'failed', error: { code: 'cli_error', message: 'Early SSE failure', limitations: ['early_failure'], details: null } } });
        res.statusCode = 202; res.end(JSON.stringify({ task: created })); return;
      }
      if (path.endsWith('/cancel')) { downloads = downloads.map(value => value.task_id === task.task_id ? { ...value, state: 'cancelled' } : value); res.end(JSON.stringify({ task: downloads[0] })); return; }
    }
    if (path === '/api/device') res.end(JSON.stringify({ snapshot: null, captured_at: null, refreshing: false, last_error: null }));
    else if (path === '/api/settings') res.end(JSON.stringify(settings));
    else if (path === '/api/runs') res.end(JSON.stringify(history));
    else if (path === '/api/run-tasks') res.end(JSON.stringify({ tasks: [] }));
    else if (path === '/api/reports') { reportReads++; res.end(JSON.stringify({ reports_root: '/reports', reports })); }
    else if (path === '/api/downloads') { downloadReads++; const body = JSON.stringify({ tasks: downloads }); if (holdDownloads) heldDownloads = { res, body }; else res.end(body); }
    else { res.statusCode = 404; res.end('{}'); }
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const page = await browser.newPage(); page.on('pageerror', error => errors.push(error.message));
  await page.addInitScript(() => { const timeout = AbortSignal.timeout.bind(AbortSignal); window.requestTimeouts = []; AbortSignal.timeout = ms => { window.requestTimeouts.push(ms); return timeout(ms); }; });
  try {
    await page.goto(`http://127.0.0.1:${server.address().port}/?token=wire-token#/reports`);
    await expect(page.getByText('removed · 来源不可达', { exact: true })).toBeVisible();
    await expect.poll(() => connections.size).toBe(1);
    const popupPromise = page.waitForEvent('popup'); await page.getByRole('link', { name: '打开报告' }).click(); const popup = await popupPromise;
    await expect(popup.getByRole('heading')).toHaveText('Fixture report opened'); assert.equal(new URL(popup.url()).searchParams.get('token'), 'wire-token'); await popup.close();
    await expect(page.locator('iframe')).toHaveCount(0);
    const boxes = page.getByRole('checkbox');
    for (let i = 0; i < 100; i++) await boxes.nth(i).check();
    await expect(boxes.nth(100)).toBeDisabled();
    for (let i = 2; i < 100; i++) await boxes.nth(i).uncheck();
    await page.getByRole('button', { name: '生成报告', exact: true }).click();
    await expect(page.getByRole('button', { name: '生成中…' })).toBeDisabled();
    await expect.poll(() => Boolean(heldReport)).toBe(true);
    assert.deepEqual(requests.at(-1).body, { run_paths: ['/results/source-0', '/results/source-1'] });
    assert.ok((await page.evaluate(() => window.requestTimeouts)).includes(180000));
    heldReport.statusCode = 201; heldReport.end(JSON.stringify({ report }));
    await expect(page.getByRole('status')).toContainText('已生成');
    await boxes.nth(2).check(); await page.getByRole('button', { name: '生成报告', exact: true }).click();
    await expect(page.getByRole('alert')).toContainText('cli_blocked'); await expect(page.getByRole('alert')).toContainText('wire_limit');
    const priorReads = reportReads;
    reports = [{ ...report, report_id: 'report-20261012T110000Z', created_at: '2026-10-12T11:00:00Z' }];
    emit('report.created', { report: reports[0] });
    await expect(page.getByRole('heading', { name: reports[0].report_id })).toBeVisible(); assert.ok(reportReads > priorReads);
    await page.getByRole('link', { name: 'Models', exact: true }).click();
    const row = page.getByTestId('download-wire-download');
    await expect(row).toContainText('未知');
    emit('download.progress', { task_id: task.task_id, downloaded_bytes: 2097152, bytes_per_second: 1024, utc: 'fixture' });
    await expect(row).toContainText('2.00 MiB（2097152 bytes）'); await expect(row).toContainText('1024 bytes/s'); await expect(row).not.toContainText('%');
    await expect(page.locator('progress,[role="progressbar"]')).toHaveCount(0);
    holdDownloads = true; await page.getByRole('button', { name: '刷新下载' }).click(); await expect.poll(() => Boolean(heldDownloads)).toBe(true);
    emit('download.progress', { task_id: task.task_id, downloaded_bytes: 3145728, bytes_per_second: 2048, utc: 'fixture-2' });
    await expect(row).toContainText('3.00 MiB'); heldDownloads.res.end(heldDownloads.body); holdDownloads = false;
    await expect(row).toContainText('3.00 MiB');
    await row.getByRole('button', { name: '取消下载' }).click(); await expect(row).toContainText('cancelled');
    assert.deepEqual(requests.at(-1), { path: '/api/downloads/wire-download/cancel', body: null, authorization: 'Bearer wire-token' });
    await page.getByLabel('模型 URL').fill(task.source_url); await page.getByLabel('out_name', { exact: true }).fill('occupied');
    await page.getByRole('button', { name: '创建下载', exact: true }).click();
    await expect(page.getByRole('alert')).toContainText('output_exists'); await expect(page.getByRole('alert')).toContainText('wire_limit');
    assert.deepEqual(requests.at(-1).body, { source_url: task.source_url, out_name: 'occupied' });
    await page.getByLabel('out_name', { exact: true }).fill('fresh'); await page.getByRole('button', { name: '创建下载', exact: true }).click();
    await expect(page.getByTestId('download-created-download')).toContainText('early_failure');
    assert.deepEqual(requests.at(-1).body, { source_url: task.source_url, out_name: 'fresh' });
    const counts = { reportReads, downloadReads };
    downloads = [{ ...task, state: 'done', downloaded_bytes: 4194304, result: { bytes: 4194304, sha256: 'b'.repeat(64) } }]; reports = [];
    emit('sync.reset', { reason: 'buffer_gap' });
    await expect(row).toContainText('成功 · done'); await expect(row).toContainText('SHA-256'); await expect(page.getByTestId('download-created-download')).toHaveCount(0);
    await page.getByRole('link', { name: 'Reports', exact: true }).click(); await expect(page.getByText('暂无报告。', { exact: true })).toBeVisible();
    assert.ok(reportReads > counts.reportReads && downloadReads > counts.downloadReads);
    assert.ok(requests.every(request => request.authorization === 'Bearer wire-token'));
    console.log('PASS T6 wire: report 1..100 and exact POST, 180s timeout, 201 pending, 422, SSE list refresh, authenticated top-level report; download POST/cancel, 409 body, native SSE and in-flight merge, POST/SSE race; sync.reset replaces reports/downloads');
  } finally {
    await page.close(); for (const res of connections) res.end(); server.closeAllConnections(); await new Promise(resolve => server.close(resolve));
  }
}
