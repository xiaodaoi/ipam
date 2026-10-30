#!/usr/bin/env node
/**
 * IPAM 主链路 E2E（M6-011）：真实浏览器 + 真实控制面。
 *
 * 环境变量：
 *   IPAM_E2E_BASE_URL      默认 https://127.0.0.1:8443
 *   IPAM_E2E_USER          默认 admin
 *   IPAM_E2E_PASSWORD      默认 admin123（仅开发环境；CI 用安装脚本生成）
 *   PLAYWRIGHT_CHROMIUM    自定义 Chromium 可执行文件（可选）
 *
 * 用法：node tests/e2e/run.mjs
 */
import { mkdir, writeFile } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

import { chromium } from '../../web/node_modules/playwright/index.mjs';

let base = (process.env.IPAM_E2E_BASE_URL || '').replace(/\/$/, '');
let hashMode = false;
function urlFor(path) {
  return hashMode ? `${base}/#${path}` : `${base}${path}`;
}
const user = process.env.IPAM_E2E_USER || 'admin';
const password = process.env.IPAM_E2E_PASSWORD || 'admin123';
const artifactDir = join(dirname(fileURLToPath(import.meta.url)), 'artifacts');

const results = [];

async function step(name, fn) {
  const started = Date.now();
  try {
    await fn();
    results.push({ name, ok: true, ms: Date.now() - started });
    console.log(`✓ ${name} (${Date.now() - started}ms)`);
  } catch (error) {
    results.push({ name, ok: false, ms: Date.now() - started, error: String(error?.message || error) });
    console.error(`✗ ${name}: ${error?.message || error}`);
    throw error;
  }
}

function assert(cond, message) {
  if (!cond) throw new Error(message);
}

async function waitText(page, text, timeout = 15_000) {
  await page.getByText(text, { exact: false }).first().waitFor({ state: 'visible', timeout });
}

async function login(page) {
  await page.goto(`${base}/auth/login`, { waitUntil: 'domcontentloaded', timeout: 30_000 });
  await page.waitForSelector('input', { timeout: 20_000 });
  await page.locator('input').nth(0).fill(user);
  await page.locator('input').nth(1).fill(password);
  const handle = page.locator('[name="captcha-action"]');
  await handle.waitFor({ state: 'visible', timeout: 20_000 });
  const hb = await handle.boundingBox();
  const pb = await handle.evaluate((el) => {
    const r = el.parentElement.getBoundingClientRect();
    return { height: r.height, width: r.width, x: r.x, y: r.y };
  });
  assert(hb, '滑块句柄不可见');
  await page.mouse.move(hb.x + hb.width / 2, hb.y + hb.height / 2);
  await page.mouse.down();
  for (let x = hb.x + hb.width / 2; x <= pb.x + pb.width - 4; x += 8) {
    await page.mouse.move(x, hb.y + hb.height / 2);
    await page.waitForTimeout(5);
  }
  await page.mouse.move(pb.x + pb.width - 2, hb.y + hb.height / 2);
  await page.mouse.up();
  await page.locator('button', { hasText: '登录' }).last().click();
  // 生产构建使用 hash 路由：URL 可能保留 /auth/login，以页面内容为准。
  await page.locator('.ipam-page').first().waitFor({ state: 'visible', timeout: 30_000 });
  await page.waitForTimeout(800);
  hashMode = page.url().includes('#/');
}

async function main() {
  await mkdir(artifactDir, { recursive: true });
  const browser = await chromium.launch({
    executablePath: process.env.PLAYWRIGHT_CHROMIUM || undefined,
    args: ['--no-sandbox'],
  });
  const context = await browser.newContext({
    ignoreHTTPSErrors: true,
    viewport: { height: 1000, width: 1600 },
  });
  if (!base) {
    for (const scheme of ['https', 'http']) {
      const candidate = `${scheme}://127.0.0.1:8443`;
      try {
        const r = await context.request.get(`${candidate}/healthz`);
        if (r.ok()) {
          base = candidate;
          break;
        }
      } catch {
        // 尝试下一个协议
      }
    }
    base ||= 'https://127.0.0.1:8443';
  }
  console.log(`目标: ${base}`);
  const page = await context.newPage();
  let failed = false;
  try {
    await step('healthz/readyz', async () => {
      const health = await context.request.get(`${base}/healthz`);
      assert(health.ok(), `/healthz ${health.status()}`);
      const ready = await context.request.get(`${base}/readyz`);
      assert(ready.ok(), `/readyz ${ready.status()}`);
    });

    await step('登录（滑块验证码）', async () => {
      await login(page);
      assert(await page.locator('.ipam-page').first().isVisible(), '登录后未进入管理页');
    });

    await step('仪表盘运行概览', async () => {
      await page.goto(urlFor('/overview'), { waitUntil: 'domcontentloaded' });
      await waitText(page, '运行概览');
    });

    await step('DHCP 子网管理', async () => {
      await page.goto(urlFor('/dhcp/subnets'), { waitUntil: 'domcontentloaded' });
      await waitText(page, '子网管理');
      await page.locator('.ant-table, .ipam-empty, [class*="empty"]').first().waitFor({ timeout: 15_000 });
    });

    await step('未登记终端发现', async () => {
      await page.goto(urlFor('/dhcp/ledger/discovered'), { waitUntil: 'domcontentloaded' });
      await waitText(page, '未登记终端');
      await page.locator('.ant-table, [class*="empty"]').first().waitFor({ timeout: 15_000 });
    });

    await step('租约管理 v4/v6 切换', async () => {
      await page.goto(urlFor('/dhcp/leases'), { waitUntil: 'domcontentloaded' });
      await waitText(page, '租约管理');
      const v6Tab = page.locator('.ipam-page .ant-radio-button-wrapper', { hasText: 'IPv6' }).first();
      await v6Tab.click();
      await waitText(page, 'DUID');
      const v4Tab = page.locator('.ipam-page .ant-radio-button-wrapper', { hasText: 'IPv4' }).first();
      await v4Tab.click();
      await waitText(page, 'MAC');
    });

    await step('利用率分析', async () => {
      await page.goto(urlFor('/dhcp/utilization'), { waitUntil: 'domcontentloaded' });
      await waitText(page, '利用率分析');
      await page.locator('.ant-table, .ant-progress, [class*="empty"]').first().waitFor({ timeout: 15_000 });
    });

    await step('DNS 解析记录', async () => {
      await page.goto(urlFor('/dns-service/records'), { waitUntil: 'domcontentloaded' });
      await waitText(page, '解析记录');
      await page.locator('.ant-select, .ipam-empty, [class*="empty"]').first().waitFor({ timeout: 15_000 });
    });

    await step('日志查询检索', async () => {
      await page.goto(urlFor('/logs-center/search'), { waitUntil: 'domcontentloaded' });
      await waitText(page, '查询检索');
    });

    await step('操作审计与完整性校验入口', async () => {
      await page.goto(urlFor('/logs-center/audit'), { waitUntil: 'domcontentloaded' });
      await waitText(page, '操作审计');
      const btn = page.locator('button', { hasText: /完整性校验|校验/ }).first();
      await btn.waitFor({ state: 'visible', timeout: 15_000 });
    });

    await step('系统账号管理', async () => {
      await page.goto(urlFor('/system/users'), { waitUntil: 'domcontentloaded' });
      await waitText(page, '账号管理');
      await page.locator('.ant-table').first().waitFor({ timeout: 15_000 });
    });
  } catch (error) {
    failed = true;
    const shot = join(artifactDir, `failure-${Date.now()}.png`);
    await page.screenshot({ path: shot, fullPage: true }).catch(() => {});
    console.error(`截图: ${shot}`);
  } finally {
    await browser.close();
  }

  const summary = {
    base,
    failed,
    finishedAt: new Date().toISOString(),
    results,
  };
  await writeFile(join(artifactDir, 'result.json'), JSON.stringify(summary, null, 2));
  const passed = results.filter((r) => r.ok).length;
  console.log(`\nE2E ${failed ? 'FAIL' : 'PASS'}: ${passed}/${results.length} 步通过`);
  process.exit(failed ? 1 : 0);
}

await main();
