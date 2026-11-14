import { chromium, expect } from '@playwright/test';
import assert from 'node:assert/strict';

const browser = await chromium.launch({ headless: true, executablePath: process.env.HOSHILET_CHROMIUM || undefined });
const getAfter = (page, suffix) =>
  page.waitForResponse((r) => r.url().endsWith(suffix) && r.request().method() === 'GET' && r.ok());
const dialog = (page) => page.getByRole('dialog');

try {
  const context = await browser.newContext({
    viewport: { width: 1400, height: 1000 },
    extraHTTPHeaders: { Authorization: 'Bearer session1' },
  });
  const page = await context.newPage();
  const errors = [];
  page.on('pageerror', (e) => errors.push(e.message));
  page.on(
    'console',
    (m) => m.type() === 'error' && !m.text().includes('Failed to load resource') && errors.push(m.text()),
  );

  // Sites: create, upload, stage, activate, roll back
  await page.goto(process.env.HOSHILET_BROWSER_URL);
  await page.getByRole('heading', { name: '站点', exact: true }).waitFor();
  // Administrator tools start off; ordinary namespace membership still applies.
  const adminMode = page.getByRole('switch', { name: '管理员功能', exact: true });
  assert.equal(await adminMode.isChecked(), false);
  assert.equal(await page.getByRole('link', { name: '概览与策略', exact: true }).count(), 0);
  await page.getByRole('button', { name: '切换空间' }).click();
  await page.getByRole('textbox', { name: '搜索空间', exact: true }).fill('~JOINED');
  await page.getByRole('option', { name: /~joined-team/ }).waitFor();
  assert.equal(await page.getByRole('option').count(), 1);
  await page.getByRole('textbox', { name: '搜索空间', exact: true }).fill('foreign');
  await page.getByText('没有匹配的空间', { exact: true }).waitFor();
  await page.getByRole('textbox', { name: '搜索空间', exact: true }).fill('joined');
  await page
    .getByRole('option', { name: /~joined-team/ })
    .getByRole('button')
    .click();
  await page.getByRole('heading', { name: '站点', exact: true }).waitFor();
  await page.waitForURL('**/#/n/joined-team/sites');
  await expect(page.getByRole('button', { name: '新建站点', exact: true })).toHaveCount(0);
  await adminMode.click();
  await expect(adminMode).toHaveAttribute('aria-checked', 'true');
  await page.getByRole('link', { name: '概览与策略', exact: true }).waitFor();
  await page.getByRole('button', { name: '新建站点', exact: true }).first().waitFor();
  await page.getByRole('button', { name: '切换空间' }).click();
  await page.getByRole('textbox', { name: '搜索空间', exact: true }).fill('foreign');
  await page
    .getByRole('option', { name: /~foreign-team/ })
    .getByRole('button')
    .click();
  await page.waitForURL('**/#/n/foreign-team/sites');
  await page.getByRole('heading', { name: '站点', exact: true }).waitFor();
  await adminMode.click();
  await expect(adminMode).toHaveAttribute('aria-checked', 'false');
  await page.waitForURL('**/#/n/alice/sites');
  assert.equal(await page.getByRole('link', { name: '概览与策略', exact: true }).count(), 0);
  await adminMode.click();
  await expect(adminMode).toHaveAttribute('aria-checked', 'true');
  await page.getByRole('link', { name: '概览与策略', exact: true }).click();
  await page.getByRole('heading', { name: '平台策略', exact: true }).waitFor();
  await adminMode.click();
  await expect(adminMode).toHaveAttribute('aria-checked', 'false');
  await page.waitForURL('**/#/n/alice/sites');
  await adminMode.click();
  await expect(adminMode).toHaveAttribute('aria-checked', 'true');
  await page.getByRole('link', { name: '概览与策略', exact: true }).waitFor();
  await page.reload();
  await page.getByRole('heading', { name: '站点', exact: true }).waitFor();
  assert.equal(await adminMode.isChecked(), true);
  await page.getByRole('button', { name: '新建站点' }).first().click();
  await dialog(page).getByLabel('站点名称').fill('browser-site');
  await dialog(page).getByRole('button', { name: '创建站点' }).click();
  await page.getByRole('heading', { name: /browser-site/ }).waitFor();

  await page.getByLabel('选择 ZIP 部署文件').setInputFiles(process.env.HOSHILET_BROWSER_ZIP);
  await page.getByText('新版本已上线', { exact: true }).waitFor();
  await page.getByRole('switch', { name: '上传后立即上线' }).uncheck();
  await page.getByLabel('选择 ZIP 部署文件').setInputFiles(process.env.HOSHILET_BROWSER_ZIP);
  await page.getByText('版本已保存，尚未上线', { exact: true }).waitFor();
  const timeline = page.getByRole('list', { name: '上线记录' }).getByRole('listitem');
  await page.getByRole('button', { name: '上线', exact: true }).click();
  await dialog(page).getByRole('button', { name: '确认上线' }).click();
  await timeline.nth(1).waitFor();
  await page.getByRole('button', { name: '回滚', exact: true }).click();
  await dialog(page).getByRole('button', { name: '确认回滚' }).click();
  await timeline.nth(2).waitFor();
  await page.evaluate(() => window.scrollTo(0, 0));
  await page.screenshot({ path: process.env.HOSHILET_BROWSER_SCREENSHOT || '/tmp/hoshilet-ui.png', fullPage: true });

  // CI trust with protected ref and literal "all" branch
  await page.getByRole('link', { name: 'CI 发布' }).click();
  await page.getByRole('button', { name: '添加信任' }).click();
  await dialog(page).getByLabel('GitLab 项目 ID').fill('42');
  await dialog(page).getByLabel('允许的分支').fill('all');
  await dialog(page).getByRole('switch', { name: '仅限受保护分支' }).check();
  const mrSwitch = dialog(page).getByRole('switch', { name: '允许同仓库 MR 流水线发布' });
  assert.equal(await mrSwitch.isChecked(), false);
  await mrSwitch.check();
  const [trustResponse] = await Promise.all([
    getAfter(page, '/trusts'),
    dialog(page).getByRole('button', { name: '添加信任' }).click(),
  ]);
  const trusts = await trustResponse.json();
  assert.equal(trusts.length, 1);
  assert.equal(trusts[0].branches, '["all"]');
  assert.equal(trusts[0].require_ref_protected, 1);
  assert.equal(trusts[0].allow_mr_pipelines, 1);

  // API key: create project-scoped key, then revoke it
  await page.getByRole('link', { name: 'API Keys' }).click();
  await page.getByRole('button', { name: '创建 Key' }).first().click();
  await dialog(page).getByLabel('名称').fill('browser key');
  await dialog(page).getByRole('combobox', { name: '站点' }).filter({ hasText: '/~alice/browser-site/' }).waitFor();
  await dialog(page).getByRole('button', { name: '创建 Key' }).click();
  const token = await dialog(page).getByLabel('API Key').textContent();
  assert.match(token, /^hoshilet_key_/);
  await dialog(page).getByRole('button', { name: '我已保存' }).click();
  const keyRow = page.getByRole('row').filter({ hasText: 'browser key' });
  await keyRow.getByText('/~alice/browser-site/').waitFor();
  await keyRow.getByRole('button', { name: '撤销' }).click();
  await dialog(page).getByRole('button', { name: '撤销 Key' }).click();
  await page.getByText('Key 已撤销', { exact: true }).waitFor();

  // Namespace members
  await page.getByRole('link', { name: '空间成员' }).click();
  await page.getByRole('heading', { name: '空间成员' }).waitFor();
  await page.getByRole('combobox', { name: 'user3 的角色' }).waitFor();

  // Namespace request and approval
  await page.getByRole('link', { name: '空间申请' }).click();
  await page.getByLabel('空间名称').fill('team-x');
  await page.getByRole('button', { name: '提交申请' }).click();
  await page.getByText('申请已提交，等待平台管理员审核').waitFor();
  await page.getByRole('link', { name: /空间审批/ }).click();
  await page.locator('[data-request="team-x"]').getByRole('button', { name: '批准' }).click();
  await page.getByText('空间 ~team-x 已创建').waitFor();
  await page.getByRole('button', { name: '切换空间' }).click();
  await page.getByRole('option', { name: /~team-x/ }).waitFor();
  await page.keyboard.press('Escape');

  // Administrator namespace usage, lazy expansion and empty namespace.
  await page.getByRole('link', { name: '空间用量', exact: true }).click();
  await page.getByRole('heading', { name: '空间用量', exact: true }).waitFor();
  await page.getByRole('button', { name: /~alice/ }).click();
  await page.getByRole('heading', { name: '项目占用', exact: true }).waitFor();
  await page.locator('.namespace-usage-detail').getByRole('link', { name: 'browser-site', exact: true }).waitFor();
  await page.locator('.namespace-usage-detail').getByText('user2', { exact: true }).waitFor();
  await page.getByLabel('搜索 namespace').fill('team-x');
  await page.getByRole('button', { name: /~team-x/ }).click();
  await page.getByText('此空间暂无项目。', { exact: true }).waitFor();
  await page.getByLabel('搜索 namespace').fill('');

  // Platform settings
  await page.getByRole('link', { name: '概览与策略' }).click();
  await page.getByRole('heading', { name: '平台策略' }).waitFor();
  assert.equal(await page.getByRole('switch', { name: '允许同仓库 MR 流水线发布' }).count(), 0);
  await page.getByLabel('每个站点保留版本数').fill('9');
  await Promise.all([getAfter(page, '/admin/metrics'), page.getByRole('button', { name: '保存策略' }).click()]);
  const settings = await page.evaluate(async () => (await fetch('/api/v1/admin/settings')).json());
  assert.equal(settings.max_versions, 9);
  assert.ok(!('allow_mr_pipelines' in settings));

  // Whitelist approval and revocation
  const waiting = await browser.newContext({ extraHTTPHeaders: { Authorization: 'Bearer pending-ui-session' } });
  const waitingPage = await waiting.newPage();
  await waitingPage.goto(process.env.HOSHILET_BROWSER_URL);
  await waitingPage.getByRole('heading', { name: '等待管理员授权' }).waitFor();
  await page.getByRole('link', { name: /用户授权/ }).click();
  const userRow = page.locator('[data-user="waiting-user"]');
  await Promise.all([getAfter(page, '/admin/users'), userRow.getByRole('button', { name: '批准授权' }).click()]);
  await waitingPage.reload();
  await waitingPage.getByRole('heading', { name: '站点', exact: true }).waitFor();
  await userRow.getByRole('button', { name: '撤销授权' }).click();
  await Promise.all([getAfter(page, '/admin/users'), dialog(page).getByRole('button', { name: '撤销授权' }).click()]);
  await waitingPage.reload();
  await waitingPage.getByRole('heading', { name: '账户已停用' }).waitFor();
  await waiting.close();
  await page.getByRole('button', { name: '预先授权' }).click();
  await dialog(page).getByLabel('GitLab 用户 ID').fill('2000');
  await Promise.all([getAfter(page, '/admin/users'), dialog(page).getByRole('button', { name: '加入白名单' }).click()]);
  const preRow = page.locator('[data-user="2000"]');
  await preRow.getByText('尚未登录').waitFor();
  await preRow.getByText('已授权').waitFor();

  // Mobile layout: no horizontal overflow, drawer navigation works
  await page.setViewportSize({ width: 390, height: 844 });
  for (const hash of ['#/admin/users', '#/admin/namespaces', '#/n/alice/sites', '#/keys']) {
    await page.goto(process.env.HOSHILET_BROWSER_URL + '/' + hash);
    await page.locator('.page-head h1').waitFor();
    assert.ok(
      await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth),
      'mobile overflow on ' + hash,
    );
  }
  await page.getByRole('button', { name: '打开菜单' }).click();
  await page.getByRole('link', { name: '站点', exact: true }).click();
  await page.getByRole('heading', { name: '站点', exact: true }).waitFor();

  // Non-administrators never receive the switch, even with a forged UI preference.
  const regular = await browser.newContext({ extraHTTPHeaders: { Authorization: 'Bearer session3' } });
  await regular.addInitScript(() => sessionStorage.setItem('hoshilet:admin-mode-user', '3'));
  const regularPage = await regular.newPage();
  await regularPage.goto(process.env.HOSHILET_BROWSER_URL);
  await regularPage.getByRole('heading', { name: '站点', exact: true }).waitFor();
  assert.equal(await regularPage.getByRole('switch', { name: '管理员功能', exact: true }).count(), 0);
  assert.equal(await regularPage.getByRole('link', { name: '概览与策略', exact: true }).count(), 0);
  await regular.close();
  await page.setViewportSize({ width: 1400, height: 1000 });
  await page.getByRole('button', { name: '退出登录', exact: true }).click();
  await page.getByRole('link', { name: /使用 GitLab/ }).waitFor();
  assert.equal(await page.evaluate(() => sessionStorage.getItem('hoshilet:admin-mode-user')), null);
  assert.deepEqual(errors, []);
  console.log(
    'Browser create/upload/stage/rollback/trust/API key/members/namespace request/MR setting/whitelist approval and revocation/mobile: PASS',
  );
  await context.close();
} finally {
  await browser.close();
}
