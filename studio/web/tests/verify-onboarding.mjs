import { expect } from '@playwright/test';
import assert from 'node:assert/strict';

export async function verifyOnboarding(browser, base, errors) {
  const page = await browser.newPage();
  page.on('pageerror', error => errors.push(error.message));
  await page.goto(`${base}/#/dashboard`);
  const wizard = page.getByRole('region', { name: '首次上手向导' });
  await expect(wizard.getByRole('heading', { name: '欢迎使用 InferYard' })).toBeVisible();
  await wizard.getByRole('button', { name: '收起引导' }).click();
  assert.equal(await page.evaluate(() => localStorage.getItem('inferyard.readiness.dismissed')), 'true');
  await expect(wizard.getByRole('heading', { name: '环境准备' })).toBeVisible();
  await wizard.getByText('1 · 下载模型', { exact: true }).click();
  await wizard.getByLabel('模型 URL', { exact: true }).fill('https://huggingface.co/demo/model/blob/' + 'a'.repeat(40) + '/model.gguf');
  await wizard.locator('#model-name').fill('model-1');
  await wizard.getByLabel('凭据环境变量名（可选）').fill('HF_TOKEN');
  await wizard.getByRole('button', { name: '下载模型', exact: true }).click();
  await expect(wizard.getByRole('status')).toContainText('complete');
  await wizard.getByText('2 · 下载引擎（macOS）', { exact: true }).click();
  await wizard.getByLabel('引擎 URL').fill('https://example.org/engine.tar.gz');
  await wizard.locator('#engine-name').fill('engine-1');
  await wizard.getByRole('button', { name: '下载引擎', exact: true }).click();
  await expect(wizard.getByRole('status')).toContainText('complete');
  await wizard.getByRole('button', { name: '初始化工作区', exact: true }).click();
  await wizard.getByRole('button', { name: '3 · 检查设备', exact: true }).click();
  for (const bundle of ['zh-core', 'zh-svg-pelican']) {
    if (bundle === 'zh-svg-pelican') await expect(wizard.getByRole('heading', { name: '8 · 第二题包' })).toBeVisible();
    await wizard.getByLabel('引擎文件绝对路径').fill('/mock/downloaded/llama-server');
    await wizard.getByLabel('引擎文件绝对路径').blur();
    await expect(wizard.getByLabel('下一步命令（参数可编辑）')).toHaveValue(/\/mock\/downloaded\/llama-server/);
    await wizard.getByRole('button', { name: '4 · 创建候选', exact: true }).click();
    await expect(wizard.getByRole('heading', { name: '5 · 在外部终端启动服务' })).toBeVisible();
    await expect(wizard.getByText(/Studio 不代启或代停/)).toBeVisible();
    await wizard.getByLabel('本机进程').selectOption('123');
    await wizard.getByLabel('endpoint', { exact: true }).fill('http://127.0.0.1:48858');
    await wizard.getByLabel('endpoint', { exact: true }).blur();
    assert.ok((await wizard.getByLabel('下一步命令（参数可编辑）').inputValue()).includes('48858'));
    await wizard.getByRole('button', { name: '6 · 绑定进程', exact: true }).click();
    await wizard.getByRole('button', { name: '7 · 运行题包', exact: true }).click();
    await expect(wizard.getByRole('button', { name: '取消运行', exact: true })).toBeVisible();
    await expect(wizard.getByRole('button', { name: '取消运行', exact: true })).toHaveCount(0, { timeout: 15000 });
  }
  await wizard.getByRole('button', { name: '9 · 生成报告并验证', exact: true }).click();
  await expect(wizard.getByRole('heading', { name: '两题包报告已验证' })).toBeVisible();
  await page.setViewportSize({ width: 390, height: 844 });
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
  await page.close();
  console.log('PASS: mock onboarding acquire, init, preflight, two candidates, frozen startup, PID/endpoint, EventBus runs, report+verify, local dismissal, mobile layout');
}
