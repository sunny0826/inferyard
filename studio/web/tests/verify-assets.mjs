import { expect } from '@playwright/test';
import assert from 'node:assert/strict';
export async function verifyAssets(browser, origin, errors) {
  const page = await browser.newPage(); page.on('pageerror', error => errors.push(error.message));
  try {
    await page.goto(`${origin}/#/reports`);
    await expect(page.getByText('removed-run · 来源不可达', { exact: true })).toBeVisible();
    await expect(page.getByText('目录修改时间：未知', { exact: true })).toBeVisible();
    const generate = page.getByRole('button', { name: '生成报告', exact: true });
    await expect(generate).toBeDisabled();
    await page.getByRole('checkbox').nth(0).check(); await page.getByRole('checkbox').nth(1).check();
    await generate.click(); await expect(page.getByRole('button', { name: '生成中…' })).toBeDisabled();
    await expect(page.getByRole('status')).toContainText('已生成');
    await expect(page.getByRole('region', { name: '报告列表' }).locator('article')).toHaveCount(2);
    await expect(page.getByRole('region', { name: '报告列表' }).locator('article').first().locator('li')).toHaveCount(2);
    await page.getByRole('checkbox').nth(0).uncheck(); await page.getByRole('checkbox').nth(1).uncheck();
    await page.getByLabel('old-dirty ·', { exact: false }).check(); await generate.click();
    await expect(page.getByRole('alert')).toContainText('unsupported_format');
    await expect(page.locator('iframe')).toHaveCount(0);
    await expect(page.getByRole('link', { name: '打开报告' }).first()).toHaveAttribute('target', '_blank');
    await page.setViewportSize({ width: 390, height: 844 });
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
    await page.screenshot({ path: '/tmp/inferyard-t6-reports-mobile.png', fullPage: true });
    await page.getByRole('link', { name: 'Models', exact: true }).click();
    await page.getByLabel('模型 URL').fill(`https://huggingface.co/demo/model/blob/${'a'.repeat(40)}/model.gguf`);
    for (const name of ['.', '..', '...', '../bad', 'bad name', '中文']) {
      await page.getByLabel('out_name', { exact: true }).fill(name);
      await expect(page.getByRole('button', { name: '创建下载', exact: true })).toBeDisabled();
      await expect(page.getByRole('alert')).toContainText('目录名无效');
    }
    await page.getByLabel('out_name', { exact: true }).fill('slow_model-1.gguf');
    await page.getByRole('button', { name: '创建下载', exact: true }).click();
    const first = page.getByRole('region', { name: '下载列表' }).locator('article').first();
    await expect(first).toContainText('2097152 bytes/s');
    await expect(first).toContainText('MiB'); await expect(first).not.toContainText('%');
    await expect(page.locator('progress,[role="progressbar"]')).toHaveCount(0);
    await first.getByRole('button', { name: '取消下载' }).click(); await expect(first).toContainText('已取消 · cancelled');
    await page.getByLabel('out_name', { exact: true }).fill('fail-model');
    await page.getByRole('button', { name: '创建下载', exact: true }).click();
    await expect(first).toContainText('失败 · failed'); await expect(first).toContainText('输出目录保留');
    await expect(first.getByRole('alert')).toContainText('mock_network_failure');
    await page.getByRole('button', { name: '创建下载', exact: true }).click();
    await expect(page.getByRole('region', { name: '创建下载' }).getByRole('alert')).toContainText('output_exists');
    await page.getByLabel('out_name', { exact: true }).fill('good-model'); await page.getByRole('button', { name: '创建下载', exact: true }).click();
    await expect(first).toContainText('成功 · done'); await expect(first).toContainText('SHA-256');
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
    await page.screenshot({ path: '/tmp/inferyard-t6-models-mobile.png', fullPage: true });
    console.log('PASS T6 mock: reports null source/time, multiselect, pending/error, top-level link; download validation, progress without total/percent, cancel, failure retained directory, 409, success; 390px layouts');
  } finally { await page.close(); }
}
