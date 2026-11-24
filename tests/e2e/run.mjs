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

async function switchLocale(page, label) {
  await page.locator('svg.lucide-languages').first().click();
  await page.waitForTimeout(500);
  const item = page
    .locator('[role="menuitem"], .ant-dropdown-menu-item')
    .filter({ hasText: label })
    .first();
  if (await item.count()) await item.click();
  else await page.getByText(label, { exact: true }).last().click();
  await page.waitForTimeout(1200);
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
  // 语言可能被上次运行留在英文，登录按钮文案随之变化。
  await page.locator('button').filter({ hasText: /登录|Login/ }).last().click();
  // 生产构建使用 hash 路由：URL 可能保留 /auth/login，以页面内容为准。
  await page.locator('.ipam-page').first().waitFor({ state: 'visible', timeout: 30_000 });
  await page.waitForTimeout(800);
  hashMode = page.url().includes('#/');
}

/**
 * 用浏览器上下文直接调用业务 API（复用登录态授权头）。
 * 说明：浏览器同源 fetch 可避免 Node 侧自签证书/协议差异。
 */
async function apiGet(page, path, authHeader) {
  assert(authHeader, '未捕获到 API 授权头，无法做接口断言');
  const res = await page.evaluate(
    async ({ auth, p }) => {
      const r = await fetch(`/api/v1${p}`, {
        headers: { Authorization: auth },
      });
      return { body: r.ok ? await r.json() : null, status: r.status };
    },
    { auth: authHeader, p: path },
  );
  assert(res.status === 200, `GET ${path} -> HTTP ${res.status}`);
  return res.body;
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
  // M6-045：捕获业务 API 的授权头，供“统计口径一致性”步骤直接断言接口数值。
  let authHeader = '';
  page.on('request', (req) => {
    if (!req.url().includes('/api/v1/')) return;
    const h = req.headers()['authorization'];
    if (h) authHeader = h;
  });
  // M6-018：监控任何指向外部图标 CDN 的请求（离线化后应为 0）。
  const externalIconRequests = [];
  page.on('request', (req) => {
    if (/iconify\.design|api\.simplesvg\.com|api\.unisvg\.com/.test(req.url())) {
      externalIconRequests.push(req.url());
    }
  });
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
      // 地址池利用率行显示所属部门（M6-020）。
      await page.locator('.ipam-rank__item--pool').first().waitFor({ timeout: 15_000 });
      await page.locator('.ipam-rank__dept').first().waitFor({ timeout: 15_000 });
      // M6-045：KPI 必须给出 IPv4/IPv6 拆分，趋势必须标注日志口径。
      await page.getByText('在线租约', { exact: true }).first().waitFor({ timeout: 15_000 });
      await page
        .getByText(/IPv4 \d+ · IPv6 \d+/)
        .first()
        .waitFor({ timeout: 15_000 });
      await page.getByText(/日志口径/).first().waitFor({ timeout: 15_000 });
    });

    await step('统计口径一致性（在线租约/池已用）', async () => {
      // M6-045 回归断言：仪表盘、台账、地址池三处数字必须同源同口径。
      // 容差仅用于吸收两次请求之间的租约到期/续约，远小于历史漂移（49 vs 103）。
      const TOLERANCE = 2;
      const dash = await apiGet(page, '/dashboard?poolTopN=100', authHeader);
      assert(
        dash.onlineNow === dash.onlineV4 + dash.onlineV6,
        `onlineNow=${dash.onlineNow} != onlineV4(${dash.onlineV4}) + onlineV6(${dash.onlineV6})`,
      );
      const poolSum = (dash.poolUtilTop ?? []).reduce((n, p) => n + p.used, 0);
      assert(
        poolSum === dash.onlineNow,
        `地址池已用合计 ${poolSum} != 在线租约 ${dash.onlineNow}`,
      );
      const v4 = await apiGet(page, '/ledger?family=4&state=online&pageSize=1', authHeader);
      const v6 = await apiGet(page, '/ledger?family=6&state=online&pageSize=1', authHeader);
      assert(
        Math.abs(dash.onlineV4 - v4.total) <= TOLERANCE,
        `仪表盘 IPv4 在线 ${dash.onlineV4} 与台账 ${v4.total} 不一致`,
      );
      assert(
        Math.abs(dash.onlineV6 - v6.total) <= TOLERANCE,
        `仪表盘 IPv6 在线 ${dash.onlineV6} 与台账 ${v6.total} 不一致`,
      );
      const top = (dash.poolUtilTop ?? []).find((p) => p.used > 0);
      if (top) {
        const sn = await apiGet(
          page,
          `/ledger?subnetId=${top.subnetId}&state=online&pageSize=1`,
          authHeader,
        );
        assert(
          Math.abs(top.used - sn.total) <= TOLERANCE,
          `地址池「${top.name || top.cidr}」已用 ${top.used} 与台账 ${sn.total} 不一致`,
        );
      }
    });

    await step('终端身份解析（DUID 与未登记口径）', async () => {
      // M6-046 回归断言：IPv6-only 终端以 DUID 存在，旧实现会整批丢弃（实测 108/278）。
      const dash = await apiGet(page, '/dashboard?poolTopN=100', authHeader);
      const disc = await apiGet(page, '/assets/discovered', authHeader);
      const items = disc.items ?? [];
      assert(items.length > 0, '未登记终端列表为空');
      for (const it of items) {
        assert(it.identity, `发现项缺少 identity: ${JSON.stringify(it)}`);
        assert(it.mac || it.duid, `发现项既无 MAC 也无 DUID: ${JSON.stringify(it)}`);
      }
      if (dash.onlineV6 > 0) {
        const duidItems = items.filter((i) => i.duid).length;
        const floor = Math.floor(dash.onlineV6 * 0.5);
        assert(
          duidItems >= floor,
          `DUID 终端仅 ${duidItems} 台（在线 IPv6 ${dash.onlineV6}，期望 ≥ ${floor}）——身份解析可能丢弃了 IPv6-only 终端`,
        );
        assert(
          items.some((i) => !i.mac && i.duid),
          '没有出现以 DUID 为身份键的 IPv6-only 终端',
        );
      }
      assert(
        items.some((i) => i.subnetCidr),
        '发现项缺少子网上下文（登记时无法带出组织）',
      );
    });

    await step('首次使用向导', async () => {
      // M6-024：仪表盘入口 → 5 步只读检测 → 稍后再说收起入口。
      await waitText(page, '首次使用向导');
      await page.locator('button', { hasText: '继续配置' }).first().click();
      await page.waitForURL(/onboarding/, { timeout: 15_000 });
      await waitText(page, '配置进度');
      const steps = page.locator('.ipam-onboarding-step');
      await steps.first().waitFor({ state: 'visible', timeout: 15_000 });
      assert((await steps.count()) === 5, `向导步骤数=${await steps.count()}`);
      await page.waitForTimeout(1500);
      assert(
        (await page.locator('.ipam-onboarding-step[data-done="true"]').count()) > 0,
        '向导未检测到任何已完成步骤',
      );
      await page.locator('button', { hasText: '稍后再说' }).first().click();
      await page.goto(urlFor('/overview'), { waitUntil: 'domcontentloaded' });
      await waitText(page, '运行概览');
      await page.waitForTimeout(600);
      // 顶部标签页仍保留「首次使用向导」标题，故以入口按钮判定而不是文本。
      assert(
        (await page.locator('button', { hasText: '继续配置' }).count()) === 0,
        '稍后再说后仪表盘仍显示向导入口',
      );
    });

    await step('DHCP 子网管理', async () => {
      await page.goto(urlFor('/dhcp/subnets'), { waitUntil: 'domcontentloaded' });
      await waitText(page, '子网管理');
      // 子网页已切换 vxe-table；:visible 避免命中默认隐藏的 empty 占位块。
      await page.locator('.vxe-table:visible, .ant-table:visible, .ipam-empty:visible').first().waitFor({ timeout: 15_000 });
    });

    await step('未登记终端发现', async () => {
      await page.goto(urlFor('/dhcp/ledger/discovered'), { waitUntil: 'domcontentloaded' });
      await waitText(page, '未登记终端');
      await page.locator('.vxe-table:visible, .ant-table:visible, .ipam-empty:visible').first().waitFor({ timeout: 15_000 });
      // M6-046：列表需带子网/组织上下文，且 DUID-only 终端可见（不再只显示 MAC 终端）。
      await page.locator('.ant-table-tbody tr').first().waitFor({ timeout: 15_000 });
      await page.getByText('所属组织').first().waitFor({ timeout: 15_000 });
      await page.getByText('DUID 终端').first().waitFor({ timeout: 15_000 });
    });

    await step('资产登记（MAC / DUID 身份键）', async () => {
      await page.goto(urlFor('/dhcp/ledger/assets'), { waitUntil: 'domcontentloaded' });
      await waitText(page, '资产登记');
      // M6-046：终端列明确区分 MAC / DUID，登记弹窗提供 DUID 字段。
      await page.getByText('终端（MAC / DUID）').first().waitFor({ timeout: 15_000 });
      await page.locator('button', { hasText: '登记资产' }).first().click();
      await page.getByPlaceholder(/IPv6-only/).first().waitFor({ timeout: 15_000 });
      await page.keyboard.press('Escape');
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
      // 热门域名支持分页浏览（M6-020）：切到该页签，有数据时分页控件可见。
      await page.getByText('热门域名', { exact: true }).first().click();
      const domPane = page.locator('.ant-tabs-tabpane-active').first();
      await domPane.locator('.ant-table').first().waitFor({ timeout: 15_000 });
      const rows = await domPane.locator('.ant-table-tbody tr.ant-table-row').count();
      if (rows > 0) {
        await domPane.locator('.ant-pagination').first().waitFor({ timeout: 10_000 });
      }
    });

    await step('日志外发与采样', async () => {
      await page.goto(urlFor('/logs-center/export'), { waitUntil: 'domcontentloaded' });
      await waitText(page, '外发与采样');
      await waitText(page, '采样策略');
      // 外发通道按功能分组排版（M6-020）：Syslog / Kafka / Webhook 三组独立分区。
      const channels = page.locator('.ipam-channel');
      await channels.first().waitFor({ timeout: 10_000 });
      assert((await channels.count()) === 3, '外发通道应为 3 个分组');
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

    await step('服务与运行历史趋势', async () => {
      await page.goto(urlFor('/system/runtime'), { waitUntil: 'domcontentloaded' });
      await waitText(page, '服务与运行');
      await waitText(page, '历史趋势');
      // 采样自启动后每分钟一次：数据未就绪时也必须渲染空态，而不是白屏或报错。
      const hasChart =
        (await page.locator('.ipam-page svg path').count()) > 0;
      const hasEmpty = (await page.getByText('暂无历史数据').count()) > 0;
      assert(hasChart || hasEmpty, '历史趋势卡片既无图表也无空态');
      const oneHour = page
        .locator('.ant-radio-button-wrapper', { hasText: '1 小时' })
        .first();
      if (await oneHour.count()) {
        await oneHour.click();
        await page.waitForTimeout(800);
      }
      await waitText(page, '历史趋势');
    });

    await step('日志中心主机资源', async () => {
      await page.goto(urlFor('/logs-center/host'), {
        waitUntil: 'domcontentloaded',
      });
      await waitText(page, '主机资源');
      await waitText(page, '运行趋势');
      await waitText(page, '磁盘使用分布');
      await waitText(page, '采样明细');
      // 曲线与饼图：数据未就绪时允许空态，但不能白屏。
      const hasChart = (await page.locator('.ipam-page svg path').count()) > 0;
      const hasEmpty = (await page.getByText('暂无历史数据').count()) > 0;
      assert(hasChart || hasEmpty, '运行趋势既无曲线也无空态');
      const hasPie = (await page.locator('.ipam-pie svg path').count()) > 0;
      const hasPieEmpty = (await page.getByText('暂无数据').count()) > 0;
      assert(hasPie || hasPieEmpty, '饼图既未渲染也无空态');
      await waitText(page, '查询');
      const oneHour = page
        .locator('.ipam-seg button', { hasText: '近 1 小时' })
        .first();
      if (await oneHour.count()) {
        await oneHour.click();
        await page.waitForTimeout(1000);
      }
      await waitText(page, '采样明细');
    });

    await step('目录服务设置', async () => {
      await page.goto(urlFor('/system/directory'), { waitUntil: 'domcontentloaded' });
      await waitText(page, '目录服务');
      await waitText(page, '连通性测试');
      const enabled = page.locator('.ant-switch').first();
      await enabled.waitFor({ state: 'visible', timeout: 15_000 });
    });

    await step('个人中心登录会话', async () => {
      await page.goto(urlFor('/profile'), { waitUntil: 'domcontentloaded' });
      await waitText(page, '个人中心');
      await page.getByText('安全设置', { exact: true }).first().click();
      await waitText(page, '登录会话');
      await waitText(page, '当前会话');
    });

    await step('英文界面切换（i18n）', async () => {
      await page.goto(urlFor('/overview'), { waitUntil: 'domcontentloaded' });
      await waitText(page, '运行概览');
      try {
        // 1. 即时切换：无需刷新
        await switchLocale(page, 'English');
        await waitText(page, 'Overview');
        await waitText(page, 'Active leases');
        // 2. 刷新后保持（偏好持久化）
        await page.reload({ waitUntil: 'domcontentloaded' });
        await waitText(page, 'Overview');
        assert(
          (await page.evaluate(() => document.documentElement.lang)) === 'en-US',
          'html lang 未切换为 en-US',
        );
        // 3. 业务页抽样：页头、表格列、表单均应为英文
        await page.goto(urlFor('/dns-service/records'), { waitUntil: 'domcontentloaded' });
        await waitText(page, 'DNS records');
        await page.goto(urlFor('/logs-center/search'), { waitUntil: 'domcontentloaded' });
        await waitText(page, 'Search');
        await page.goto(urlFor('/system/users'), { waitUntil: 'domcontentloaded' });
        await waitText(page, 'Account management');
      } finally {
        // 4. 恢复中文基线，保证重复执行与后续步骤稳定
        await page.goto(urlFor('/overview'), { waitUntil: 'domcontentloaded' });
        await page.waitForTimeout(600);
        await switchLocale(page, '简体中文');
        await waitText(page, '运行概览');
      }
    });

    await step('图标离线渲染与零外链', async () => {
      // 图标须来自本地离线集合：应有内联 <svg> 图标，且全程无外部 CDN 请求。
      await page.goto(urlFor('/overview'), { waitUntil: 'domcontentloaded' });
      await waitText(page, '运行概览');
      await page.locator('.iconify svg, svg.iconify').first().waitFor({ state: 'visible', timeout: 10_000 });
      assert(
        externalIconRequests.length === 0,
        `出现图标外部 CDN 请求: ${externalIconRequests.join(', ')}`,
      );
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
