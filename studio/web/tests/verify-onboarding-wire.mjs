import { createServer } from 'node:http';
import { expect } from '@playwright/test';
import assert from 'node:assert/strict';

export async function verifyOnboardingWire(browser, vite, errors) {
  let cli = false;
  let bodies = [];
  let progressReads = 0;
  const baseDirectory = { path: '/fixture/work', configured: true, exists: true, initialized: true };
  const progress = { kind: 'progress', progress_version: 1, workspace: '/fixture/work', device_preflight: { status: 'ok', path: '/fixture/work/preflight/device/device-preflight.json' },
    bundles: [{ bundle: 'zh-core', phase: 'bound_missing', candidate_path: '/fixture/work/candidates/core', bound_path: null, runs: [], next_action: 'bind', next_command: 'inferyard config bind --candidate /fixture/work/candidates/core/candidate.toml --pid "${PID:?Choose a process}" --endpoint "${ENDPOINT:?Set endpoint}" --out /fixture/work/bound/core', blocked_reason: null }],
    report: { status: 'missing', path: null, runs: [] }, next_action: 'bind', next_command: null, default_scope_complete: false };
  progress.next_command = progress.bundles[0].next_command;
  const server = createServer(async (req, res) => {
    if (req.url.startsWith('/api/')) {
      if (!req.url.startsWith('/api/events')) assert.equal(req.headers.authorization, 'Bearer wizard-token');
      res.setHeader('Content-Type', 'application/json');
      if (req.url.startsWith('/api/events')) { res.writeHead(200, { 'Content-Type': 'text/event-stream' }); res.write(': connected\n\n'); return; }
      if (req.url === '/api/readiness') return res.end(JSON.stringify({ cli: { path: cli ? '/fixture/inferyard' : null, version: cli ? 'fixture' : null, status: cli ? 'ok' : 'not_found' }, workspace: baseDirectory, results: baseDirectory, models: { ...baseDirectory, path: '/fixture/models' }, first_run: false }));
      if (req.url === '/api/progress') { progressReads++; return res.end(JSON.stringify({ status: 'complete', exit_code: 0, progress })); }
      if (req.url.startsWith('/api/onboarding/startup?')) {
        assert.equal(new URL(req.url, 'http://localhost').searchParams.get('candidate'), '/fixture/work/candidates/core');
        return res.end(JSON.stringify({ path: '/fixture/llama-server', startup_args: ['-m', '/fixture/model.gguf', '--host', '127.0.0.1', '--port', '48857'] }));
      }
      if (req.url === '/api/onboarding/processes') return res.end(JSON.stringify([{ pid: 4242, name: 'llama-server' }]));
      if (req.url === '/api/onboarding/execute') {
        let data = ''; for await (const chunk of req) data += chunk;
        bodies.push(JSON.parse(data));
        return res.end(JSON.stringify({ status: 'blocked', exit_code: 2, tasks: [] }));
      }
      if (req.url === '/api/device') return res.end(JSON.stringify({ snapshot: null, captured_at: null, refreshing: false, last_error: null }));
      if (req.url === '/api/settings') return res.end(JSON.stringify({ settings: { workspace_root: '/fixture/work', inferyard_path: null, results_root: null, models_root: null }, effective: { workspace_root: '/fixture/work', inferyard_path: null, inferyard_version: null, inferyard_status: 'not_found', results_root: null, models_root: null, reports_root: null } }));
      if (req.url === '/api/runs') return res.end(JSON.stringify({ results_root: null, scanned_at: 'fixture', batches: [], standalone_runs: [] }));
      if (req.url === '/api/reports') return res.end(JSON.stringify({ reports_root: null, reports: [] }));
      if (req.url === '/api/run-tasks' || req.url === '/api/downloads') return res.end(JSON.stringify({ tasks: [] }));
      res.statusCode = 404; return res.end('{}');
    }
    vite.middlewares(req, res);
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const page = await browser.newPage(); page.on('pageerror', error => errors.push(error.message));
  try {
    await page.goto(`http://127.0.0.1:${server.address().port}/?token=wizard-token#/dashboard`);
    const wizard = page.getByRole('region', { name: '首次上手向导' });
    await expect(wizard.getByText('CLI：请安装 CLI 或在 Settings 配置路径')).toBeAttached();
    assert.equal(progressReads, 0);
    await page.getByRole('link', { name: 'Device', exact: true }).click();
    await expect(page.getByText('尚无设备快照', { exact: true })).toBeVisible();
    cli = true;
    await page.getByRole('link', { name: 'Dashboard', exact: true }).click();
    await expect(wizard.getByRole('heading', { name: '5 · 在外部终端启动服务' })).toBeVisible();
    await wizard.getByLabel('本机进程').selectOption('4242');
    await wizard.getByLabel('endpoint', { exact: true }).fill('http://127.0.0.1:48858');
    await wizard.getByRole('button', { name: '6 · 绑定进程', exact: true }).click();
    await expect(wizard.getByRole('status')).toContainText('blocked（退出码 2）');
    assert.deepEqual(bodies, [{ command: "inferyard config bind --candidate /fixture/work/candidates/core/candidate.toml --pid '4242' --endpoint 'http://127.0.0.1:48858' --out /fixture/work/bound/core", report_verify: false }]);
    assert.ok(progressReads > 0);
    assert.equal(await page.evaluate(() => localStorage.length), 0);
    console.log('PASS: onboarding wire auth, CLI-missing soft gate, progress.v1 parsing, frozen startup, PID/endpoint command edits and blocked result');
  } finally { await page.close(); server.closeAllConnections(); await new Promise(resolve => server.close(resolve)); }
}
