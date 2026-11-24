import { expect, test } from '@playwright/test';
import { authorize, connectionClient } from './bridge';
import { account, createSession, newID, to } from './transaction-fixture';
import { install } from './review-wallet-fixture';

test('the homepage opens the later transaction without reserving the wallet for the earlier connection', async ({ page }) => {
  const id = newID();
  const sessionId = await createSession(page, '0x47');
  const connection = await connectionClient('connections', { id, clientName: 'Codex', chainId: '0x1' });
  await connectionClient('transactions', { id, sessionId, clientName: 'WorkBuddy', transaction: { from: account, to, chainId: '0x47' } });
  await install(page);
  await page.goto('/');
  const list = page.getByRole('region', { name: '待审批请求' });
  const rows = list.getByRole('listitem').filter({ hasText: id });
  await expect(rows).toHaveCount(2);
  await expect(rows.nth(0).getByRole('link', { name: '审阅连接 · Codex' })).toHaveAttribute('href', '/?connection=' + id);
  await expect(rows.nth(0)).toContainText('Ethereum Mainnet · 0x1');
  const later = rows.nth(1).getByRole('link', { name: '审阅交易 · WorkBuddy' });
  await expect(later).toHaveAttribute('href', '/?transaction=' + id);
  await expect(rows.nth(1)).toContainText('Conflux eSpace Testnet · 0x47');
  expect(await page.evaluate(() => window.reviewWallet.calls)).toEqual([]);
  expect((await (await page.request.get('/api/v1/status')).json()).activeWalletRequest).toBeUndefined();

  await later.click();
  await expect(page.getByText('固定发送账户可用')).toBeVisible();
  await page.evaluate(() => { window.reviewWallet.chain = '0x1'; window.reviewWallet.event('chainChanged'); });
  await page.getByRole('button', { name: /切换到 Conflux eSpace Testnet/ }).click();
  await expect(page.getByText('网络匹配', { exact: true })).toBeVisible();
  expect((await (await page.request.get('/api/v1/status')).json()).activeWalletRequest).toMatchObject({ kind: 'transaction', id });
  await page.getByRole('button', { name: '拒绝这笔交易' }).click();
  await expect(page.getByText('用户已拒绝', { exact: true })).toBeVisible();
  expect(await connectionClient('connections/' + id)).toEqual(connection);
  await page.goto('/');
  await expect(rows).toHaveCount(1);
  await connectionClient('connections/' + id + '/cancel', {});
  await page.getByRole('button', { name: '重新检查', exact: true }).click();
  await expect(rows).toHaveCount(0);
});

test('the homepage distinguishes empty and failed reads and drops management data on a list 401', async ({ page }) => {
  let status = 503;
  await page.route('**/browser/pending-requests', route => route.fulfill({ status, json: { requests: [] } }));
  await authorize(page);
  const list = page.getByRole('region', { name: '待审批请求' });
  await expect(list).toContainText('暂时无法读取列表');
  status = 200;
  await page.getByRole('button', { name: '重新检查', exact: true }).click();
  await expect(list).toContainText('当前没有待审批请求');
  status = 401;
  await page.evaluate(() => window.dispatchEvent(new Event('focus')));
  await expect(page.getByRole('status')).toHaveText('需要浏览器授权');
  await expect(list).toHaveCount(0);
  await expect(page.getByText('当前授权 ID')).toHaveCount(0);
});

test('services without the pending-list capability still show the management homepage', async ({ page }) => {
  let reads = 0;
  await page.route('**/browser/pending-requests', async route => { reads++; await route.abort(); });
  await page.route('**/api/v1/status', async route => {
    const response = await route.fetch();
    const status = await response.json();
    status.info.capabilities = status.info.capabilities.filter((name: string) => name !== 'browser.pending-requests');
    await route.fulfill({ response, json: status });
  });
  await authorize(page);
  await expect(page.getByRole('status')).toHaveText('浏览器已授权');
  await expect(page.getByRole('heading', { name: '选择浏览器钱包' })).toBeVisible();
  await expect(page.getByRole('region', { name: '待审批请求' })).toHaveCount(0);
  expect(reads).toBe(0);
});
