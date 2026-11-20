import { expect, test } from '@playwright/test';
import { readFile } from 'node:fs/promises';
import { createServer } from 'node:http';
import type { AddressInfo } from 'node:net';
import { join } from 'node:path';
import { authorize, cli, link, openLink, origin, root } from './bridge';

test.beforeEach(async () => { await cli(['browser', 'revoke', '--all', '--json']); });

test('unauthorized browser sees instructions without management data', async ({ page }) => {
  await page.goto('/');
  await expect(page.getByRole('status')).toHaveText('需要浏览器授权');
  await expect(page.getByText('wallet-bridge ui', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: '退出当前浏览器' })).toHaveCount(0);
  await expect(page.getByText('当前授权 ID')).toHaveCount(0);
  await expect(page.getByRole('heading', { name: '选择浏览器钱包' })).toHaveCount(0);
});

test('one exchange clears the fragment before sending and sets an isolated HttpOnly cookie', async ({ page, context }) => {
  let exchanges = 0;
  const requestURLs: string[] = [];
  page.on('request', (request) => {
    requestURLs.push(request.url());
    if (request.url().endsWith('/browser/authorize')) exchanges++;
  });
  await page.addInitScript(() => {
    const original = window.fetch;
    window.fetch = (...args) => {
      if (String(args[0]).endsWith('/browser/authorize')) document.documentElement.dataset.fragmentAtExchange = location.hash || 'empty';
      return original(...args);
    };
  });
  const url = await link();
  await openLink(page, url);
  await expect(page.getByRole('status')).toHaveText('浏览器已授权');
  await expect(page.getByRole('heading', { name: '选择浏览器钱包' })).toBeVisible();
  expect(page.url() === origin + '/').toBe(true);
  expect(await page.locator('html').getAttribute('data-fragment-at-exchange')).toBe('empty');
  expect(exchanges).toBe(1);
  expect(requestURLs.every((value) => !value.includes('#authorize=') && !value.includes(url.split('#authorize=')[1]))).toBe(true);
  expect((await page.locator('body').innerText()).includes(url.split('#authorize=')[1])).toBe(false);
  const cookies = await context.cookies(origin + '/api/v1/status');
  const cookie = cookies.find((value) => value.name === 'wallet_bridge_browser');
  expect(Boolean(cookie?.httpOnly && cookie.sameSite === 'Strict' && cookie.path === '/api/v1' && cookie.domain === '127.0.0.1')).toBe(true);
  expect((cookie?.expires ?? 0) - Date.now() / 1000).toBeGreaterThan(29 * 24 * 60 * 60);
  expect(await page.evaluate(() => document.cookie.includes('wallet_bridge_browser'))).toBe(false);
  const controlAccess = await context.request.get(origin + '/api/v1/control/browser-grants');
  expect(controlAccess.status()).toBe(401);
  await page.reload();
  await expect(page.getByRole('status')).toHaveText('浏览器已授权');
  expect(exchanges).toBe(1);
  const reopened = await context.newPage();
  await reopened.goto('/');
  await expect(reopened.getByRole('status')).toHaveText('浏览器已授权');
  await openLink(page, url);
  await expect(page.getByRole('status')).toHaveText('授权链接已失效');
  await page.getByRole('button', { name: '重新检查' }).click();
  await expect(page.getByRole('status')).toHaveText('浏览器已授权');
});

test('logout revokes only this browser and duplicate clicks do not replay', async ({ page, browser }) => {
  await authorize(page);
  await expect(page.getByRole('status')).toHaveText('浏览器已授权');
  const other = await browser.newContext();
  try {
    const second = await other.newPage();
    await authorize(second);
    await expect(second.getByRole('status')).toHaveText('浏览器已授权');
    let logouts = 0;
    await page.route('**/api/v1/browser/logout', async (route) => {
      logouts++;
      await new Promise((resolve) => setTimeout(resolve, 150));
      await route.continue();
    });
    const button = page.getByRole('button', { name: '退出当前浏览器' });
    await button.evaluate((element: HTMLButtonElement) => { element.click(); element.click(); });
    await expect(page.getByRole('status')).toHaveText('已退出当前浏览器');
    expect(logouts).toBe(1);
    await page.reload();
    await expect(page.getByRole('status')).toHaveText('需要浏览器授权');
    await second.reload();
    await expect(second.getByRole('status')).toHaveText('浏览器已授权');
    const records = JSON.parse(await cli(['browser', 'list', '--json'])).grants;
    expect(records.some((grant: { state: string }) => grant.state === 'revoked')).toBe(true);
    expect(records.some((grant: { state: string }) => grant.state === 'active')).toBe(true);
  } finally { await other.close(); }
});

test('invalidated and malformed links fail without leaving secrets in the address', async ({ page }) => {
  const stale = await link();
  await cli(['browser', 'revoke', '--all', '--json']);
  await openLink(page, stale);
  await expect(page.getByRole('status')).toHaveText('授权链接已失效');
  expect(page.url() === origin + '/').toBe(true);
  let exchanges = 0;
  page.on('request', (request) => { if (request.url().endsWith('/browser/authorize')) exchanges++; });
  await page.goto('/#authorize=malformed');
  await expect(page.getByRole('status')).toHaveText('授权链接已失效');
  expect(page.url() === origin + '/').toBe(true);
  expect(exchanges).toBe(0);
});

test('service restart preserves authorization and CLI revocation updates the page', async ({ page, context }) => {
  await authorize(page);
  await expect(page.getByRole('status')).toHaveText('浏览器已授权');
  const before = (await context.cookies(origin + '/api/v1/status')).find((cookie) => cookie.name === 'wallet_bridge_browser')!;
  await cli(['service', 'stop', '--json']);
  await page.getByRole('button', { name: '重新检查' }).click();
  await expect(page.getByRole('status')).toHaveText('暂时无法确认服务状态');
  await expect(page.getByText('当前授权 ID')).toHaveCount(0);
  await cli(['service', 'ensure', '--json']);
  await page.getByRole('button', { name: '重新检查' }).click();
  await expect(page.getByRole('status')).toHaveText('浏览器已授权');
  const after = (await context.cookies(origin + '/api/v1/status')).find((cookie) => cookie.name === 'wallet_bridge_browser')!;
  expect(before.expires === after.expires).toBe(true);
  const status = await context.request.get(origin + '/api/v1/status');
  const id = (await status.json()).browser.id;
  await cli(['browser', 'revoke', id, '--json']);
  await expect(page.getByRole('status')).toHaveText('需要浏览器授权', { timeout: 15_000 });
});

test('a lost exchange response is not replayed and can be checked safely', async ({ page }) => {
  let exchanges = 0;
  await page.route('**/api/v1/browser/authorize', async (route) => {
    exchanges++;
    await route.fetch();
    await route.abort('failed');
  });
  await authorize(page);
  await expect(page.getByRole('status')).toHaveText('暂时无法确认授权结果');
  expect(page.url() === origin + '/').toBe(true);
  await page.getByRole('button', { name: '重新检查' }).click();
  await expect(page.getByRole('status')).not.toHaveText('暂时无法确认授权结果');
  expect(exchanges).toBe(1);
});

test('a lost logout response hides stale data and checks committed revocation', async ({ page }) => {
  await authorize(page);
  await expect(page.getByRole('status')).toHaveText('浏览器已授权');
  let logouts = 0;
  await page.route('**/api/v1/browser/logout', async (route) => {
    logouts++;
    await route.fetch();
    await route.abort('failed');
  });
  await page.getByRole('button', { name: '退出当前浏览器' }).click();
  await expect(page.getByRole('status')).toHaveText('暂时无法确认退出结果');
  await expect(page.getByText('当前授权 ID')).toHaveCount(0);
  await page.getByRole('button', { name: '重新检查' }).click();
  await expect(page.getByRole('status')).toHaveText('需要浏览器授权');
  expect(logouts).toBe(1);
});

test('a browser with an expired cookie is asked to authorize again', async ({ page, context }) => {
  await authorize(page);
  await expect(page.getByRole('status')).toHaveText('浏览器已授权');
  const cookies = await context.cookies(origin + '/api/v1/status');
  const cookie = cookies.find((value) => value.name === 'wallet_bridge_browser')!;
  await context.addCookies([{ ...cookie, expires: Math.floor(Date.now() / 1000) - 1 }]);
  await page.reload();
  await expect(page.getByRole('status')).toHaveText('需要浏览器授权');
});

test('an exchange without a saved cookie explains the failure', async ({ page }) => {
  // Inject a successful exchange without a cookie to exercise browser policy failures.
  await page.route('**/api/v1/browser/authorize', (route) => route.fulfill({ status: 200, contentType: 'application/json', body: '{}' }));
  await authorize(page);
  await expect(page.getByRole('status')).toHaveText('无法保存浏览器授权');
});

test('a preview origin clears fragments and never calls service APIs', async ({ page }) => {
  const preview = createServer(async (request, response) => {
    const path = request.url === '/' ? 'index.html' : request.url?.slice(1);
    if (path !== 'index.html' && !/^assets\/[a-zA-Z0-9_-]+\.(js|css)$/.test(path ?? '')) { response.writeHead(404).end(); return; }
    try {
      const body = await readFile(join(root, 'web/dist', path!));
      response.setHeader('Content-Type', path!.endsWith('.js') ? 'text/javascript' : path!.endsWith('.css') ? 'text/css' : 'text/html');
      response.end(body);
    } catch { response.writeHead(404).end(); }
  });
  await new Promise<void>((resolve) => preview.listen(0, '127.0.0.1', resolve));
  try {
    let requests = 0;
    page.on('request', (request) => { if (request.url().includes('/api/v1/')) requests++; });
    const url = `http://127.0.0.1:${(preview.address() as AddressInfo).port}/`;
    await page.goto(url + '#authorize=' + 'a'.repeat(64));
    await expect(page.getByRole('status')).toHaveText('开发预览');
    expect(page.url() === url).toBe(true);
    expect(requests).toBe(0);
    await expect(page.getByRole('button', { name: '重新检查', exact: true })).toHaveCount(0);
    await expect(page.getByRole('button', { name: '重新检测钱包' })).toBeVisible();
  } finally { await new Promise<void>((resolve, reject) => preview.close((error) => error ? reject(error) : resolve())); }
});
