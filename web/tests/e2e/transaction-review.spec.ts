import { randomBytes } from 'node:crypto';
import { expect, test, type Page } from '@playwright/test';
import { authorize, connectionClient, origin } from './bridge';

const account = '0x' + 'ab'.repeat(20);
const to = '0x' + 'cd'.repeat(20);
const data = '0x00' + 'f0'.repeat(512);
const newID = () => randomBytes(16).toString('hex');

async function createReview(page: Page, chainId: string, transaction: Record<string, string>, intent?: string) {
  const connection = newID();
  await connectionClient('connections', { id: connection, clientName: 'Review fixture', chainId });
  await authorize(page);
  await expect(page.getByRole('status')).toHaveText('浏览器已授权');
  // Establish an isolated synthetic binding through the production browser API.
  const path = origin + '/api/v1/browser/connections/' + connection + '/execution/';
  const id = newID();
  const callId = newID();
  const post = async (action: string, extra: object = {}) => {
    const res = await page.request.post(path + action, { headers: { Origin: origin }, data: { id, ...extra } });
    expect(res.status()).toBe(200);
    return res.json();
  };
  await post('claim');
  await post('begin', { callId });
  await post('return', { callId });
  const { sessionId } = await post('complete', { wallet: { kind: 'eip6963', name: '测试钱包', rdns: 'test.wallet' }, account, chainId });
  const request = newID();
  await connectionClient('transactions', { id: request, sessionId, clientName: '示例 CLI', intent,
    transaction: { from: account, to, chainId, ...transaction } });
  return request;
}

test('review exact amount, addresses, fees and full data', async ({ page }) => {
  const id = await createReview(page, '0x47', { value: '1250000000000000001', data, gas: '75000', maxFeePerGas: '40', maxPriorityFeePerGas: '0' },
    '在测试网上调用合约，并附带原生币。');
  const writes: string[] = [];
  page.on('request', request => { if (request.method() !== 'GET') writes.push(request.url()); });
  await page.addInitScript(() => {
    Object.defineProperty(window, 'ethereum', { get() { throw new Error('Unexpected wallet access'); } });
  });
  const errors: string[] = [];
  page.on('pageerror', err => errors.push(err.message));
  await page.goto('/?transaction=' + id);
  await expect(page.getByRole('heading', { name: '交易审阅' })).toBeVisible();
  await expect(page.locator('.review-amount')).toHaveText('1.250000000000000001CFX');
  await expect(page.getByText('1250000000000000001 最小单位')).toBeVisible();
  await expect(page.getByText('chainId 71 · 0x47')).toBeVisible();
  await expect(page.getByText('测试钱包', { exact: true })).toBeVisible();
  await expect(page.getByText('示例 CLI', { exact: true })).toBeVisible();
  await expect(page.getByText('在测试网上调用合约，并附带原生币。')).toBeVisible();
  await expect(page.getByText(account, { exact: true })).toBeVisible();
  await expect(page.getByText(to, { exact: true })).toBeVisible();
  await expect(page.getByText('75000 gas', { exact: false })).toBeVisible();
  await expect(page.getByText('最高优先费 · maxPriorityFeePerGas')).toBeVisible();
  await expect(page.getByText('调用方提供 · 0x0', { exact: true })).toBeVisible();
  // Capture clipboard writes without changing the developer's system clipboard.
  await page.evaluate(() => {
    Object.defineProperty(navigator, 'clipboard', { value: {
      writeText: async (text: string) => { document.body.dataset.lastCopy = text; },
    } });
  });
  await page.locator('summary').click();
  for (const [label, text] of [['发送地址', account], ['目标地址', to], ['完整 data', data]]) {
    await page.getByRole('button', { name: '复制' + label, exact: true }).click();
    await expect(page.locator('body')).toHaveAttribute('data-last-copy', text);
  }
  await page.setViewportSize({ width: 360, height: 780 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await connectionClient('transactions/' + id + '/cancel', {});
  await expect(page.getByText('已取消', { exact: true })).toBeVisible();
  await page.reload();
  await expect(page.getByText('已取消', { exact: true })).toBeVisible();
  expect(writes).toEqual([]);
  expect(errors).toEqual([]);
});

test('an uncertain transaction updates to its late hash on the original page', async ({ page }) => {
  const id = await createReview(page, '0x47', {});
  const path = origin + '/api/v1/browser/transactions/' + id;
  const execution = newID();
  const callId = newID();
  const report = async (suffix: string, data: object) => {
    const response = await page.request.post(path + suffix, { headers: { Origin: origin }, data });
    expect(response.status()).toBe(200);
    return response.json();
  };
  await report('/execution', { action: 'claim', id: execution });
  const permit = await report('/send', { id: execution, callId, account, chainId: '0x47',
    wallet: { kind: 'eip6963', name: '测试钱包', rdns: 'test.wallet' } });
  expect(permit.invoke).toBe(true);
  await report('/result', { id: execution, callId, errorCode: 4900 });
  await page.goto('/?transaction=' + id);
  await expect(page.getByRole('status')).toHaveText('结果未确定');
  const txHash = '0x' + '08'.repeat(32);
  await report('/result', { id: execution, callId, txHash });
  await expect(page.getByRole('status')).toHaveText('已发送');
  await expect(page.getByText(txHash, { exact: true })).toBeVisible();
});

test('unknown chain, omitted intent and wallet-estimated fees', async ({ page }) => {
  const id = await createReview(page, '0x123456', {});
  await page.goto('/?transaction=' + id + '&value=123&clientName=forged');
  await expect(page.getByText('调用方未提供说明')).toBeVisible();
  await expect(page.locator('.review-amount')).toHaveText('0最小单位');
  await expect(page.getByText(/chainId 1193046 · 0x123456/)).toBeVisible();
  await expect(page.getByText('由钱包估算', { exact: true })).toHaveCount(2);
  await expect(page.getByText('forged')).toHaveCount(0);
  await page.goto('/?transaction=' + id + '&connection=' + id);
  await expect(page.getByRole('heading', { name: '交易链接无效' })).toBeVisible();
});

test('ordinary link requires authorization and revoked access clears details', async ({ page }) => {
  const id = newID();
  const queries: string[] = [];
  page.on('request', r => { if (r.url().includes('/browser/transactions/')) queries.push(r.url()); });
  await page.goto('/?transaction=' + id);
  await expect(page.getByRole('status')).toHaveText('需要浏览器授权');
  expect(queries).toEqual([]);
  const real = await createReview(page, '0x1', { gasPrice: '1000000000' });
  await page.goto('/?transaction=' + real);
  await expect(page.getByText('Gas 单价 · gasPrice')).toBeVisible();
  await page.request.post(origin + '/api/v1/browser/logout', { headers: { Origin: origin }, data: {} });
  await page.getByRole('button', { name: '刷新状态' }).click();
  await expect(page.getByText(/浏览器授权已失效|需要浏览器授权/)).toBeVisible();
  await expect(page.getByText(account, { exact: true })).toHaveCount(0);
  await authorize(page);
  await page.goto('/?transaction=' + id);
  await expect(page.getByRole('alert')).toHaveText(/找不到这笔交易/);
});
