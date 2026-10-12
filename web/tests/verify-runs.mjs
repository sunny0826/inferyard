import { expect } from '@playwright/test';
import assert from 'node:assert/strict';

export async function verifyRuns(browser, origin, errors) {
  const page = await browser.newPage();
  page.on('pageerror', error => errors.push(error.message));
  await page.goto(`${origin}/#/dashboard`);
  await expect(page.getByRole('heading', { name: 'Dashboard', exact: true })).toBeVisible();
  await expect(page.getByTestId('batch-batch-priority').locator(':scope > .badge')).toHaveText('已取消 · cancelled');
  await expect(page.getByRole('button', { name: '续测 standalone-partial', exact: true })).toBeDisabled();
  await expect(page.getByTestId('history-old-dirty')).toContainText('未排空 · dirty');
  await expect(page.getByTestId('history-old-partial')).toContainText('不完整 · partial');
  await expect(page.getByTestId('history-old-complete')).toContainText('完整 · complete');
  await expect(page.getByLabel('仅运行单 bundle')).not.toBeChecked();
  await page.getByRole('button', { name: '启动运行', exact: true }).click();
  await expect(page.getByTestId('task-group').first()).toContainText('2 个任务');
  await expect(page.getByTestId('task-mock-2')).toContainText('排队中');
  await page.getByRole('button', { name: '启动运行', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('queue_duplicate');
  await page.getByTestId('task-mock-2').getByRole('link').click();
  await expect(page.getByTestId('task-state')).toContainText('排队中');
  await page.getByRole('button', { name: '取消任务', exact: true }).click();
  await expect(page.getByTestId('task-state')).toContainText('cancelled');
  await expect(page.getByText('退出码', { exact: true })).toHaveCount(0);
  await page.getByRole('link', { name: '← 返回 Runs' }).click();
  await page.getByTestId('task-mock-1').getByRole('link').click();
  await expect(page.getByTestId('sample-count')).not.toHaveText('采样点：0');
  await expect(page.locator('.uplot canvas')).toBeVisible();
  await expect(page.getByTestId('task-state')).toContainText('成功', { timeout: 10000 });
  await expect(page.getByTestId('progress')).toContainText('2 / 2');
  await expect(page.getByTestId('sample-count')).toHaveText('采样点：8');
  await page.getByText('采样数值', { exact: true }).click();
  await expect(page.getByRole('region', { name: '资源曲线' }).getByRole('row').filter({ hasText: '缺测' })).toHaveCount(2);
  const data = await page.evaluate(async () => {
    const { chartData, aggregateStatus, appendResources } = await import('/src/runs/model.ts');
    const points = [
      { utc: '2026-10-12T00:00:00Z', mem_available_bytes: 1073741824, service_rss_bytes: 2147483648, service_cpu_percent: 20, temperature_celsius: 42 },
      { utc: '2026-10-12T00:00:01Z', mem_available_bytes: null, service_rss_bytes: null, service_cpu_percent: null, temperature_celsius: null },
      { utc: '2026-10-12T00:00:02Z', mem_available_bytes: 0, service_rss_bytes: 0, service_cpu_percent: 0, temperature_celsius: 0 },
    ];
    return { data: chartData(points), priority: [aggregateStatus(['complete', 'partial']), aggregateStatus(['partial', 'dirty']), aggregateStatus(['dirty', 'cancelled'])], merged: appendResources(points, [points[1]]).length };
  });
  assert.deepEqual(data.data.slice(1), [[1, null, 0], [2, null, 0], [20, null, 0], [42, null, 0]]);
  assert.deepEqual(data.priority, ['partial', 'dirty', 'cancelled']); assert.equal(data.merged, 3);
  await page.setViewportSize({ width: 390, height: 844 });
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
  await page.screenshot({ path: '/tmp/inferyard-t5-detail-mobile.png', fullPage: true });
  await page.getByRole('link', { name: '← 返回 Runs' }).click();
  await page.getByRole('button', { name: '续测 stale-source', exact: true }).click();
  await page.getByRole('button', { name: '确认续测', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('not_resumable');
  await expect(page.getByRole('button', { name: '续测 stale-source', exact: true })).toBeDisabled();
  await expect(page.getByRole('button', { name: '确认续测', exact: true })).toBeDisabled();
  await page.getByRole('button', { name: '关闭续测设置' }).click();
  await page.getByRole('button', { name: '续测 resume-source', exact: true }).click();
  await page.getByLabel('endpoint_url（可选）').fill('http://127.0.0.1:8080');
  await page.getByRole('button', { name: '确认续测', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('必须同给同省');
  await page.getByLabel('server_pid（与 endpoint 同给同省）').fill('12');
  await page.getByRole('button', { name: '确认续测', exact: true }).click();
  await expect(page.getByTestId('progress')).toContainText('/ 1');
  await expect(page.getByText(/分母为冻结续测子集/)).toBeVisible();
  await expect(page.getByTestId('task-state')).toContainText('成功', { timeout: 10000 });
  await page.getByRole('link', { name: '← 返回 Runs' }).click();
  await expect(page.getByTestId('history-run-mock-3')).toContainText('resume');
  await page.getByLabel('仅运行单 bundle').check();
  await expect(page.getByLabel('我确认只运行一个 bundle')).not.toBeChecked();
  await expect(page.getByRole('button', { name: '启动运行', exact: true })).toBeDisabled();
  await page.getByLabel('我确认只运行一个 bundle').check();
  await page.getByLabel('zh-core 已绑定配置路径').fill('/mock/invalid');
  await page.getByRole('button', { name: '启动运行', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('invalid_request');
  await page.getByLabel('zh-core 已绑定配置路径').fill('/mock/outside');
  await page.getByRole('button', { name: '启动运行', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('output_root_outside_results');
  for (const [scenario, id, label, limitation, historyStatus] of [
    ['slow', 4, 'cancelled', 'interrupted', 'cancelled'],
    ['blocked', 5, '输入 / 预检阻断', 'device_preflight_blocked', 'partial'],
    ['error', 6, '工具 / 证据错误', 'evidence_invalid', 'partial'],
    ['partial', 7, '运行不完整', 'duration_requires_new_window', 'partial'],
    ['dirty', 8, '运行不完整', 'engine_not_drained', 'dirty'],
  ]) {
    await page.getByLabel('仅运行单 bundle').check();
    await page.getByLabel('我确认只运行一个 bundle').check();
    await page.getByLabel('zh-core 已绑定配置路径').fill(`/mock/${scenario}`);
    await page.getByRole('button', { name: '启动运行', exact: true }).click();
    await expect(page.getByTestId('task-group').last()).toContainText('1 个任务');
    await page.getByTestId(`task-mock-${id}`).getByRole('link').click();
    if (scenario === 'slow') {
      await expect(page.getByTestId('task-state')).toContainText('运行中');
      await page.getByRole('button', { name: '取消任务', exact: true }).click();
      await expect(page.getByTestId('task-state')).toContainText('取消中');
    }
    await expect(page.getByTestId('task-state')).toContainText(label, { timeout: 10000 });
    await expect(page.locator('main')).toContainText(limitation);
    if (scenario === 'partial' || scenario === 'dirty' || scenario === 'slow') await expect(page.getByRole('alert')).toHaveCount(0);
    if (scenario === 'blocked') await expect(page.getByRole('cell', { name: '无效 · invalid', exact: true })).toBeVisible();
    if (scenario === 'error') await expect(page.getByRole('cell', { name: '失败 · failed', exact: true })).toBeVisible();
    await page.getByRole('link', { name: '← 返回 Runs' }).click();
    await expect(page.getByTestId(`history-run-mock-${id}`)).toContainText(`· ${historyStatus}`);
    await expect(page.getByRole('button', { name: `续测 run-mock-${id}`, exact: true })).toBeDisabled();
  }
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
  await page.screenshot({ path: '/tmp/inferyard-t5-runs-mobile.png', fullPage: true });
  await page.close();
  console.log('PASS T5: history hierarchy/priority; grouped double and confirmed single launch; 400/409 errors; queued/running cancel; resume subset/stale refusal; exits 0/2/3/4/130; SSE resources/null gaps; 390px layout');
}
