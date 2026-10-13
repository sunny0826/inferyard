import { createServer } from 'node:http';
import { expect } from '@playwright/test';
import assert from 'node:assert/strict';

export async function verifyOnboardingEntryWire(browser, vite, errors) {
  let cli = false;
  let initialized = false;
  let progressAvailable = true;
  let provisioned = false;
  let provisionCalls = 0;
  let cancelCalls = 0;
  let progressReads = 0;
  let sequence = 0;
  const removedRequests = [];
  const connections = new Set();
  const settings = {
    settings: { workspace_root: '/fixture/work', inferyard_path: null, results_root: '/fixture/results', models_root: '/fixture/models' },
    effective: { workspace_root: '/fixture/work', inferyard_path: null, inferyard_version: null, inferyard_status: 'not_found', results_root: '/fixture/results', models_root: '/fixture/models', reports_root: '/fixture/reports' },
  };
  const bundle = (name, phase, action) => ({
    bundle: name, phase, candidate_path: `/fixture/work/candidates/${name}`, bound_path: null,
    runs: [], next_action: action, next_command: `inferyard ${action} --workspace /fixture/work`, blocked_reason: null,
  });
  const progress = {
    kind: 'progress', progress_version: 1, workspace: '/fixture/work',
    device_preflight: { status: 'ok', path: '/fixture/work/preflight/device/device-preflight.json' },
    bundles: [bundle('zh-core', 'bound_missing', 'bind'), bundle('zh-svg-pelican', 'candidate_missing', 'prepare_candidate')],
    report: { status: 'missing', path: null, runs: [] },
    next_action: 'bind', next_command: 'inferyard config bind --candidate /fixture/work/candidates/zh-core', default_scope_complete: false,
  };
  let task = {
    task_id: 'wizard-task', kind: 'run', state: 'running', queued: false,
    config_path: '/fixture/work/bound/zh-core/config.toml', resume_from: null,
    bundle: 'zh-core', run_id: 'wizard-run', run_path: '/fixture/results/wizard-run',
    created_at: '2026-10-13T00:00:00Z', started_at: '2026-10-13T00:00:01Z',
    finished_at: null, exit: null, error: null,
  };
  const emit = (name, data) => {
    for (const response of connections) response.write(`id: ${++sequence}\nevent: ${name}\ndata: ${JSON.stringify(data)}\n\n`);
  };
  const server = createServer(async (req, res) => {
    const path = req.url.split('?')[0];
    if (!path.startsWith('/api/')) { vite.middlewares(req, res); return; }
    if (path === '/api/events') {
      assert.equal(new URL(req.url, 'http://localhost').searchParams.get('token'), 'wizard-token');
      res.writeHead(200, { 'Content-Type': 'text/event-stream' });
      res.write(': connected\n\n'); connections.add(res);
      req.on('close', () => connections.delete(res)); return;
    }
    assert.equal(req.headers.authorization, 'Bearer wizard-token');
    res.setHeader('Content-Type', 'application/json');
    if (path === '/api/readiness') {
      const directory = { configured: provisioned || initialized, exists: provisioned || initialized, initialized: false };
      return res.end(JSON.stringify({
        cli: { path: cli ? '/fixture/inferyard' : null, version: cli ? 'fixture' : null, status: cli ? 'ok' : 'not_found' },
        workspace: { ...directory, path: '/fixture/work', initialized },
        results: { ...directory, path: '/fixture/results' }, models: { ...directory, path: '/fixture/models' },
        first_run: !provisioned && !initialized,
      }));
    }
    if (path === '/api/settings/provision') {
      assert.equal(req.method, 'POST'); provisionCalls++; provisioned = true;
      return res.end(JSON.stringify(settings));
    }
    if (path === '/api/progress') {
      progressReads++;
      const available = initialized && progressAvailable;
      return res.end(JSON.stringify({ status: available ? 'complete' : 'blocked', exit_code: available ? 0 : 2, progress: available ? progress : null }));
    }
    if (path.startsWith('/api/onboarding/')) removedRequests.push(path);
    if (path === '/api/run-tasks/wizard-task/cancel') {
      assert.equal(req.method, 'POST'); cancelCalls++; task = { ...task, state: 'cancelling' };
      return res.end(JSON.stringify({ task }));
    }
    if (path === '/api/device') return res.end(JSON.stringify({ snapshot: null, captured_at: null, refreshing: false, last_error: null }));
    if (path === '/api/settings') return res.end(JSON.stringify(settings));
    if (path === '/api/runs') return res.end(JSON.stringify({ results_root: null, scanned_at: 'fixture', batches: [], standalone_runs: [] }));
    if (path === '/api/reports') return res.end(JSON.stringify({ reports_root: null, reports: [] }));
    if (path === '/api/run-tasks') return res.end(JSON.stringify({ tasks: [task] }));
    if (path === '/api/downloads') return res.end(JSON.stringify({ tasks: [] }));
    res.statusCode = 404; res.end('{}');
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const page = await browser.newPage();
  page.on('pageerror', error => errors.push(error.message));
  try {
    await page.goto(`http://127.0.0.1:${server.address().port}/?token=wizard-token#/dashboard`);
    const wizard = page.getByRole('region', { name: '首次上手向导' });
    await expect(wizard.getByText('CLI：请安装 CLI 或在 Settings 配置路径')).toBeVisible();
    await expect(wizard.getByRole('button', { name: '打开向导', exact: true })).toBeEnabled();
    assert.equal(progressReads, 0);
    await wizard.getByRole('button', { name: '准备目录', exact: true }).click();
    await expect.poll(() => provisionCalls).toBe(1);
    await expect(wizard.getByText(/工作区：.*已配置，待初始化/)).toBeAttached();
    await wizard.getByText('有待准备的项目', { exact: true }).click();
    await wizard.getByRole('button', { name: '准备目录', exact: true }).click();
    await expect.poll(() => provisionCalls).toBe(2);
    assert.equal(progressReads, 0);
    await page.getByRole('link', { name: 'Device', exact: true }).click();
    await expect(page.getByText('尚无设备快照', { exact: true })).toBeVisible();

    cli = true; initialized = true;
    await page.getByRole('link', { name: 'Dashboard', exact: true }).click();
    await expect(wizard.getByRole('listitem').filter({ hasText: 'zh-core' })).toContainText('待连接模型服务');
    await expect(wizard.getByRole('listitem').filter({ hasText: 'zh-svg-pelican' })).toContainText('待准备运行配置');
    await expect(wizard.getByText('在向导中连接已启动的模型服务。', { exact: true })).toBeVisible();
    await expect(wizard.locator('textarea, select, input')).toHaveCount(0);
    await expect(wizard).not.toContainText(/bound_missing|candidate_missing|inferyard config bind/);
    assert.ok(progressReads > 0);
    await expect.poll(() => connections.size).toBeGreaterThan(1);
    const activeLink = wizard.getByRole('link', { name: /zh-core.*查看进度/ });
    await expect(activeLink).toHaveAttribute('href', '#/runs/wizard-task');
    await wizard.getByRole('button', { name: '取消运行', exact: true }).click();
    await expect(wizard.getByRole('button', { name: '取消运行', exact: true })).toBeDisabled();
    await expect(activeLink).toContainText('取消中');
    assert.equal(cancelCalls, 1);
    task = { ...task, state: 'cancelled', finished_at: '2026-10-13T00:00:03Z', exit: { exit_code: 130, cli_status: 'interrupted', completeness: 'incomplete', stop_reason: 'user_cancelled', limitations: [] } };
    const beforeRun = progressReads;
    emit('run.state', { task });
    await expect(wizard.getByRole('button', { name: '取消运行', exact: true })).toHaveCount(0);
    await expect.poll(() => progressReads).toBeGreaterThan(beforeRun);

    for (const [phase, action, text] of [
      ['preflight_missing', 'device_check', '待检查设备'],
      ['candidate_missing', 'prepare_candidate', '待准备运行配置'],
      ['run_missing', 'run', '待开始运行'],
      ['run_incomplete', 'resume', '运行未完成'],
      ['run_complete', 'none', '运行已完成'],
    ]) {
      progress.bundles[0] = bundle('zh-core', phase, action);
      progress.next_action = action === 'resume' ? 'run' : action === 'none' ? 'prepare_candidate' : action;
      emit('sync.reset', { reason: 'workspace_changed' });
      await expect(wizard.getByRole('listitem').filter({ hasText: 'zh-core' })).toContainText(text);
      await expect(wizard).not.toContainText(phase);
    }
    progress.bundles[1] = bundle('zh-svg-pelican', 'run_missing', 'run'); progress.next_action = 'run';
    emit('device.updated', { snapshot: null, captured_at: 'fixture-device', error: null });
    await expect(wizard.getByRole('listitem').filter({ hasText: 'zh-svg-pelican' })).toContainText('待开始运行');

    progress.bundles[1] = bundle('zh-svg-pelican', 'run_complete', 'none');
    progress.next_action = 'done'; progress.next_command = null; progress.default_scope_complete = true;
    progress.report = { status: 'verified', path: '/fixture/reports/summary', runs: [] };
    emit('report.created', { report: { report_id: 'summary', path: '/fixture/reports/summary', created_at: 'fixture', run_ids: [], run_paths: [], html_url: '/reports/summary/report.html' } });
    await expect(wizard.getByRole('heading', { name: '两题包报告已验证', exact: true })).toBeVisible();
    await expect(wizard.getByRole('link', { name: '查看报告', exact: true })).toHaveAttribute('href', '#/reports');
    await expect(wizard.getByRole('button', { name: '打开向导', exact: true })).toBeEnabled();
    const beforeRefresh = progressReads;
    await wizard.getByRole('button', { name: '刷新向导', exact: true }).click();
    await expect.poll(() => progressReads).toBeGreaterThan(beforeRefresh);
    progressAvailable = false;
    emit('sync.reset', { reason: 'workspace_changed' });
    await expect(wizard.getByRole('heading', { name: '暂时无法读取工作区进度', exact: true })).toBeVisible();
    await expect(wizard.getByText('打开向导，初始化工作区并完成首次测评。', { exact: true })).toHaveCount(0);
    await expect(wizard.getByRole('button', { name: '打开向导', exact: true })).toBeEnabled();
    assert.deepEqual(removedRequests, []);
    assert.equal(await page.evaluate(() => localStorage.length), 0);
    console.log('PASS T14 wire: Bearer/SSE auth, CLI soft gate, repeatable provision, all human phases, next step, run link/cancel, sync.reset/device/report/run refresh, verified completion, unavailable progress, no removed onboarding API calls');
  } finally {
    await page.close();
    server.closeAllConnections();
    await new Promise(resolve => server.close(resolve));
  }
}
