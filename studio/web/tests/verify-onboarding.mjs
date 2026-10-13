import { expect } from '@playwright/test';
import assert from 'node:assert/strict';

const simulatedOpen = '模拟模式：已模拟打开向导，不会打开原生窗口。';

async function entryOnly(wizard) {
  await expect(wizard.getByRole('button', { name: '打开向导', exact: true })).toBeEnabled();
  await expect(wizard.locator('textarea, select, input')).toHaveCount(0);
  await expect(wizard.getByRole('button', { name: /下载模型|下载引擎|初始化工作区|创建候选|绑定进程|运行题包|生成报告并验证/ })).toHaveCount(0);
}

export async function verifyOnboardingEntry(browser, base, errors) {
  const page = await browser.newPage();
  const nativeImports = [];
  page.on('pageerror', error => errors.push(error.message));
  page.on('request', request => {
    if (/\/mygo\.ts(?:\?|$)|mygo-runtime/.test(request.url())) nativeImports.push(request.url());
  });
  try {
    await page.goto(`${base}/#/dashboard`);
    const wizard = page.getByRole('region', { name: '首次上手向导' });
    await expect(wizard.getByRole('heading', { name: '欢迎使用 InferYard' })).toBeVisible();
    await expect(wizard.getByText('打开向导，初始化工作区并完成首次测评。', { exact: true })).toBeVisible();
    await entryOnly(wizard);
    await wizard.getByRole('button', { name: '打开向导', exact: true }).click();
    await expect(wizard.getByRole('status')).toContainText(simulatedOpen);
    await expect(wizard.getByText('打开向导，初始化工作区并完成首次测评。', { exact: true })).toBeVisible();
    await wizard.getByRole('button', { name: '收起引导' }).click();
    assert.equal(await page.evaluate(() => localStorage.getItem('inferyard.readiness.dismissed')), 'true');
    await expect(wizard.getByRole('heading', { name: '环境准备' })).toBeVisible();
    await wizard.getByText('有待准备的项目', { exact: true }).click();
    await wizard.getByRole('button', { name: '准备目录', exact: true }).click();
    await expect(wizard.getByText(/工作区：.*已配置，待初始化/)).toBeAttached();

    await page.goto(`${base}/?onboarding=progress#/dashboard`);
    await expect(wizard.getByRole('listitem').filter({ hasText: 'zh-core' })).toContainText('中文核心题包');
    await expect(wizard.getByRole('listitem').filter({ hasText: 'zh-core' })).toContainText('运行已完成');
    await expect(wizard.getByRole('listitem').filter({ hasText: 'zh-svg-pelican' })).toContainText('SVG 鹈鹕题包');
    await expect(wizard.getByRole('listitem').filter({ hasText: 'zh-svg-pelican' })).toContainText('待连接模型服务');
    await expect(wizard.getByText('在向导中连接已启动的模型服务。', { exact: true })).toBeVisible();
    await expect(wizard).not.toContainText(/run_complete|bound_missing|config bind/);
    await entryOnly(wizard);
    await wizard.getByRole('button', { name: '打开向导', exact: true }).click();
    await expect(wizard.getByRole('status')).toContainText(simulatedOpen);
    await expect(wizard.getByRole('listitem').filter({ hasText: 'zh-svg-pelican' })).toContainText('待连接模型服务');
    await page.setViewportSize({ width: 390, height: 844 });
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);

    await page.goto(`${base}/?onboarding=complete#/dashboard`);
    await expect(wizard.getByRole('heading', { name: '两题包报告已验证', exact: true })).toBeVisible();
    await expect(wizard.getByRole('link', { name: '查看报告', exact: true })).toHaveAttribute('href', '#/reports');
    for (const bundle of ['zh-core', 'zh-svg-pelican']) {
      await expect(wizard.getByRole('listitem').filter({ hasText: bundle })).toContainText('运行已完成');
    }
    await entryOnly(wizard);
    await wizard.getByRole('button', { name: '打开向导', exact: true }).click();
    await expect(wizard.getByRole('status')).toContainText(simulatedOpen);
    await expect(wizard.getByRole('heading', { name: '两题包报告已验证', exact: true })).toBeVisible();

    await page.goto(`${base}/?onboarding=cli-missing#/dashboard`);
    await expect(wizard.getByText('CLI：请安装 CLI 或在 Settings 配置路径')).toBeAttached();
    await entryOnly(wizard);
    await page.getByRole('link', { name: 'Device', exact: true }).click();
    await expect(page.getByText('尚无设备快照', { exact: true })).toBeVisible();
    assert.deepEqual(nativeImports, []);
    console.log('PASS T14 mock: first-run entry, readiness dismissal/provision, human progress and next step, simulated open without state changes or native imports, verified completion, CLI soft gate, 390px layout');
  } finally {
    await page.close();
  }
}
