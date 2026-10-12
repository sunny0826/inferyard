import { createServer } from 'node:http';
import { expect } from '@playwright/test';
import assert from 'node:assert/strict';

export async function verifyRunsWire(browser, vite, errors) {
  const requests = [];
  const connections = new Set();
  let seq = 0; let detailReads = 0; let historyReads = 0; let taskReads = 0;
  let hold = false; let heldResponse;
  const task = { task_id: 'wire', kind: 'run', state: 'running', queued: false, config_path: '/config/core', resume_from: null, bundle: 'zh-core', run_id: 'wire-run', run_path: '/results/wire-run', created_at: '2026-10-12T00:00:00Z', started_at: '2026-10-12T00:00:01Z', finished_at: null, exit: null, error: null };
  const point = { utc: '2026-10-12T00:00:01Z', mem_available_bytes: 1073741824, service_rss_bytes: 1024, service_cpu_percent: null, temperature_celsius: null };
  let detail = { task, progress: { phase: 'formal', done: 0, total: 2, current_case_id: null, last_seq: 1, cases: [] }, resources: [point] };
  const source = { run_id: 'source', path: '/results/batch/runs/source', batch_id: 'batch', status: 'partial', sealed: false, completeness: 'incomplete', stop_reason: null, model_name: null, model_path: null, engine: null, bundle: 'zh-core', score: null, relation: null, parent_run_id: null, started_at: null, finished_at: null, resumable: true };
  const history = { results_root: '/results', scanned_at: 'fixture', batches: [{ batch_id: 'batch', path: '/results/batch', runs: [source] }], standalone_runs: [] };
  const settings = { settings: { inferyard_path: null, results_root: null, models_root: null }, effective: { inferyard_path: null, inferyard_version: null, inferyard_status: 'not_found', results_root: null, models_root: null, reports_root: null } };
  function emit(name, data) { for (const res of connections) res.write(`id: ${++seq}\nevent: ${name}\ndata: ${JSON.stringify(data)}\n\n`); }
  const server = createServer(async (req, res) => {
    const path = req.url.split('?')[0];
    if (path === '/api/events') {
      res.writeHead(200, { 'Content-Type': 'text/event-stream' }); res.write(': connected\n\n'); connections.add(res); req.on('close', () => connections.delete(res)); return;
    }
    if (!path.startsWith('/api/')) { vite.middlewares(req, res); return; }
    res.setHeader('Content-Type', 'application/json');
    if (req.method === 'POST') {
      let raw = ''; for await (const chunk of req) raw += chunk;
      const body = raw ? JSON.parse(raw) : null; requests.push({ path, body, authorization: req.headers.authorization });
      if (path === '/api/runs') {
        if (body.config_paths[0] === '/invalid') { res.statusCode = 400; res.end(JSON.stringify({ error: { code: 'invalid_request', message: 'Invalid config fixture', limitations: ['invalid_input'], details: null } })); }
        else { res.statusCode = 202; res.end(JSON.stringify({ tasks: body.config_paths.map((config, index) => ({ ...task, task_id: `created-${requests.length}-${index}`, config_path: config, state: 'starting', queued: true })) })); }
      } else if (path === '/api/runs/resume') {
        if (body.endpoint_url) { res.statusCode = 202; res.end(JSON.stringify({ task: { ...task, kind: 'resume', resume_from: body.from_run } })); }
        else { res.statusCode = 409; res.end(JSON.stringify({ error: { code: 'conflict', message: 'Resume denied fixture', limitations: ['resume_requires_latest_unfinished_run'], details: { reason: 'not_resumable' } } })); }
      } else { res.statusCode = 409; res.end(JSON.stringify({ error: { code: 'conflict', message: 'Already finished fixture', limitations: [], details: { reason: 'invalid_state' } } })); }
    } else if (path === '/api/device') res.end(JSON.stringify({ snapshot: null, captured_at: null, refreshing: false, last_error: null }));
    else if (path === '/api/settings') res.end(JSON.stringify(settings));
    else if (path === '/api/runs') { historyReads++; res.end(JSON.stringify(history)); }
    else if (path === '/api/reports') res.end(JSON.stringify({ reports_root: null, reports: [] }));
    else if (path === '/api/downloads') res.end(JSON.stringify({ tasks: [] }));
    else if (path === '/api/run-tasks') { taskReads++; res.end(JSON.stringify({ tasks: [detail.task] })); }
    else if (path === '/api/run-tasks/wire') { detailReads++; if (hold) heldResponse = { res, body: JSON.stringify(detail) }; else res.end(JSON.stringify(detail)); }
    else { res.statusCode = 404; res.end(JSON.stringify({ error: { code: 'not_found', message: 'Task not found fixture', limitations: [], details: null } })); }
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const page = await browser.newPage(); page.on('pageerror', error => errors.push(error.message));
  try {
    await page.goto(`http://127.0.0.1:${server.address().port}/?token=wire-token#/runs/wire`);
    await expect(page.getByTestId('sample-count')).toHaveText('采样点：1');
    await expect.poll(() => connections.size).toBe(1);
    await page.getByRole('button', { name: '取消任务', exact: true }).click();
    await expect(page.getByRole('alert')).toContainText('invalid_state');
    await expect(page.getByTestId('task-state')).toContainText('running');
    const update = { ...detail.progress, done: 1, last_seq: 2 };
    emit('run.progress', { task_id: 'wire', run_id: 'wire-run', progress: update });
    emit('run.resources', { task_id: 'wire', points: [{ ...point, utc: '2026-10-12T00:00:02Z', service_cpu_percent: 30 }] });
    await expect(page.getByTestId('progress')).toContainText('1 / 2');
    await expect(page.getByTestId('sample-count')).toHaveText('采样点：2');
    hold = true;
    await page.getByRole('button', { name: '刷新详情', exact: true }).click();
    await expect.poll(() => Boolean(heldResponse)).toBe(true);
    emit('run.progress', { task_id: 'wire', run_id: 'wire-run', progress: { ...update, done: 2, last_seq: 3 } });
    emit('run.resources', { task_id: 'wire', points: [{ ...point, utc: '2026-10-12T00:00:03Z', mem_available_bytes: null }] });
    // The connection status is a DOM-observable barrier after the ordered SSE frames.
    emit('device.updated', { snapshot: null, captured_at: 'barrier', error: null });
    await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
    heldResponse.res.end(heldResponse.body); hold = false;
    await expect(page.getByTestId('progress')).toContainText('2 / 2');
    await expect(page.getByTestId('sample-count')).toHaveText('采样点：2');
    detail = { task: { ...task, state: 'done', finished_at: '2026-10-12T00:00:09Z', exit: { exit_code: 3, cli_status: 'duration_requires_new_window', completeness: 'incomplete', stop_reason: 'duration_requires_new_window', limitations: ['raw_limit'] } }, progress: { ...update, done: 1, total: 1, last_seq: 10 }, resources: [{ ...point, service_cpu_percent: 88 }] };
    const reads = { detailReads, historyReads, taskReads };
    emit('sync.reset', { reason: 'buffer_gap' });
    await expect(page.getByTestId('task-state')).toContainText('运行不完整');
    await expect(page.getByTestId('progress')).toContainText('1 / 1');
    await expect(page.getByTestId('sample-count')).toHaveText('采样点：1');
    assert.ok(detailReads > reads.detailReads && historyReads > reads.historyReads && taskReads > reads.taskReads);
    emit('run.state', { task: { ...detail.task, state: 'failed', exit: { ...detail.task.exit, exit_code: 4 }, error: { code: 'cli_error', message: 'SSE evidence error', limitations: ['raw_evidence_limit'], details: null } } });
    await expect(page.getByTestId('task-state')).toContainText('工具 / 证据错误');
    await expect(page.getByRole('alert').last()).toContainText('raw_evidence_limit');
    await page.getByRole('link', { name: '← 返回 Runs' }).click();
    await page.getByLabel('zh-core 已绑定配置路径').fill('/config/core');
    await page.getByLabel('zh-svg-pelican 已绑定配置路径').fill('/config/svg');
    await page.getByRole('button', { name: '启动运行', exact: true }).click();
    await expect(page.getByTestId('task-group').last()).toContainText('2 个任务');
    assert.deepEqual(requests.at(-1).body, { config_paths: ['/config/core', '/config/svg'] });
    await page.getByLabel('仅运行单 bundle').check();
    const before = requests.length;
    await expect(page.getByRole('button', { name: '启动运行', exact: true })).toBeDisabled();
    assert.equal(requests.length, before);
    await page.getByLabel('我确认只运行一个 bundle').check();
    await page.getByRole('button', { name: '启动运行', exact: true }).click();
    await expect(page.getByTestId('task-group').last()).toContainText('1 个任务');
    assert.deepEqual(requests.at(-1).body, { config_paths: ['/config/core'], allow_single_bundle: true });
    await page.getByLabel('zh-core 已绑定配置路径').fill('/invalid');
    await page.getByRole('button', { name: '启动运行', exact: true }).click();
    await expect(page.getByRole('alert')).toContainText('Invalid config fixture');
    await expect(page.getByRole('alert')).toContainText('invalid_input');
    await page.getByRole('button', { name: '续测 source', exact: true }).click();
    await page.getByRole('button', { name: '确认续测', exact: true }).click();
    await expect(page.getByRole('region', { name: '续测设置' }).getByRole('alert')).toContainText('not_resumable');
    assert.deepEqual(requests.at(-1).body, { from_run: source.path });
    await expect(page.getByRole('button', { name: '续测 source', exact: true })).toBeDisabled();
    await page.reload();
    await page.getByRole('button', { name: '续测 source', exact: true }).click();
    await page.getByLabel('endpoint_url（可选）').fill('http://127.0.0.1:8080');
    await page.getByLabel('server_pid（与 endpoint 同给同省）').fill('42');
    await page.getByRole('button', { name: '确认续测', exact: true }).click();
    await expect(page.getByRole('heading', { name: 'RunDetail', exact: true })).toBeVisible();
    assert.deepEqual(requests.at(-1).body, { from_run: source.path, endpoint_url: 'http://127.0.0.1:8080', server_pid: 42 });
    assert.ok(requests.every(request => request.authorization === 'Bearer wire-token'));
    await page.goto(`http://127.0.0.1:${server.address().port}/?token=wire-token#/runs/missing`);
    await expect(page.getByRole('alert')).toContainText('Task not found fixture');
    console.log('PASS T5 wire fixture: POST exact bodies; 400/409 errors; native run SSE; in-flight REST/event merge; sync.reset history/tasks/detail/resources rebuild; unknown task');
  } finally {
    await page.close(); for (const connection of connections) connection.end();
    server.closeAllConnections(); await new Promise(resolve => server.close(resolve));
  }
}
