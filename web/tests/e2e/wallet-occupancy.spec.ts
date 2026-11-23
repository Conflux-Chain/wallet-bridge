import { randomBytes } from 'node:crypto';
import { expect, test } from '@playwright/test';
import { authorize, connectionClient } from './bridge';

test('transaction occupancy blocks a same-ID connection until the wallet is free', async ({ page }) => {
  const id = randomBytes(16).toString('hex');
  await connectionClient('connections', { id, clientName: '等待钱包的 CLI', chainId: '71' });
  await authorize(page);
  await expect(page.getByRole('status')).toHaveText('浏览器已授权');

  let phase: string | undefined = 'ready';
  // The transaction claim endpoint arrives later. All other reads use the running service.
  await page.route('**/api/v1/status', async route => {
    const response = await route.fetch();
    const status = await response.json();
    await route.fulfill({ response, json: { ...status, activeWalletRequest: phase && { kind: 'transaction', id, phase } } });
  });
  await page.getByRole('button', { name: '重新检查', exact: true }).click();
  const notice = page.getByRole('region', { name: '钱包占用情况' });
  await expect(notice.getByRole('heading')).toHaveText('钱包正在处理交易');
  await expect(notice.getByText(/等待本次审阅结束/)).toBeVisible();
  await expect(notice.getByText(id, { exact: true })).toBeVisible();

  phase = 'calling';
  await page.goto('/?connection=' + id);
  await expect(notice.getByText('请等待钱包返回操作结果。')).toBeVisible();
  await expect(page.getByRole('button', { name: '拒绝本次连接' })).toHaveCount(0);
  await expect(page.getByRole('heading', { name: '选择浏览器钱包' })).toHaveCount(0);
  await expect(notice.getByRole('link')).toHaveCount(0);
  phase = 'interrupted_calling';
  await page.getByRole('button', { name: '重新检查请求' }).click();
  await expect(notice.getByText(/检查钱包中是否还有待处理的弹窗/)).toBeVisible();
  await expect(page.getByRole('button', { name: /继续连接|解除等待/ })).toHaveCount(0);

  phase = undefined;
  await page.getByRole('button', { name: '重新检查请求' }).click();
  await expect(notice).toHaveCount(0);
  await expect(page.getByRole('button', { name: '拒绝本次连接' })).toBeEnabled();
});

test('API 1.6 connection occupancy still links to its original request', async ({ page }) => {
  const id = randomBytes(16).toString('hex');
  await authorize(page);
  await expect(page.getByRole('status')).toHaveText('浏览器已授权');
  await page.route('**/api/v1/status', async route => {
    const response = await route.fetch();
    const status = await response.json();
    await route.fulfill({ response, json: { ...status, walletOperation: { connectionId: id, phase: 'ready' } } });
  });
  await page.getByRole('button', { name: '重新检查', exact: true }).click();
  await expect(page.getByRole('link', { name: /查看占用钱包的请求/ })).toHaveAttribute('href', '/?connection=' + id);
});
