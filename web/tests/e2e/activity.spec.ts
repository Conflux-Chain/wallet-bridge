import { expect, test } from '@playwright/test';
import { authorize, connectionClient } from './bridge';
import { account, createSession, newID, to } from './transaction-fixture';
import { install } from './review-wallet-fixture';

test('an authorized user can find requests, refresh their outcomes and open the original transaction', async ({ page }) => {
  const sessionId = await createSession(page, '0x47');
  const id = newID();
  await connectionClient('connections', { id, clientName: 'Codex', chainId: '0x47' });
  await connectionClient('transactions', { id, sessionId, clientName: 'WorkBuddy', transaction: { from: account, to, chainId: '0x47' } });
  await install(page);
  await page.goto('/');
  await page.getByRole('link', { name: '查看活动记录' }).click();
  const rows = page.getByRole('listitem').filter({ hasText: id });
  await expect(rows).toHaveCount(2);
  const transaction = rows.filter({ has: page.getByRole('link', { name: '交易 · WorkBuddy' }) });
  const connection = rows.filter({ has: page.getByRole('link', { name: '连接 · Codex' }) });
  await expect(transaction).toContainText('待审批');
  await expect(transaction).toContainText(sessionId);
  await expect(connection.getByRole('link')).toHaveAttribute('href', '/?connection=' + id);
  expect(await page.evaluate(() => window.reviewWallet.calls)).toEqual([]);
  await connectionClient('connections/' + id + '/cancel', {});
  await connectionClient('sessions/' + sessionId + '/close', {});
  await page.getByRole('button', { name: '刷新记录' }).click();
  await expect(transaction).toContainText('已取消');
  await expect(connection).toContainText('已取消');
  await transaction.getByRole('link').click();
  await expect(page).toHaveURL(/\?transaction=/);
  await expect(page.getByText('已取消', { exact: true })).toBeVisible();
});

test('older activity keeps its page on refresh and shows a late hash on the original row', async ({ page }) => {
  const id = newID();
  const hash = '0x' + 'ab'.repeat(32);
  const cursor = '1000.' + id + '.transaction';
  const record = { kind: 'transaction', state: 'uncertain', id, clientName: 'CLI', chainId: '0x47', createdAt: '2026-01-01T00:00:00Z' };
  let reported = false;
  await page.route('**/browser/activity?*', route => {
    const after = new URL(route.request().url()).searchParams.get('after');
    if (after === null) return route.fulfill({ json: { activities: [{ ...record, id: '1'.repeat(32), state: 'pending' }], nextAfter: cursor } });
    expect(after).toBe(cursor);
    return route.fulfill({ json: { activities: [reported ? { ...record, state: 'sent', txHash: hash } : record] } });
  });
  await authorize(page);
  await page.getByRole('link', { name: '查看活动记录' }).click();
  await page.getByRole('link', { name: '查看更早记录' }).click();
  const row = page.getByRole('listitem');
  await expect(row).toContainText('结果未确定');
  reported = true;
  await page.getByRole('button', { name: '刷新记录' }).click();
  await expect(row).toContainText('已发送');
  await expect(row).toContainText(hash);
  await page.getByRole('link', { name: '返回最新记录' }).click();
  await expect(row).toContainText('待审批');
});

test('empty history, failed reads, loss of authorization and older services have distinct views', async ({ page }) => {
  let status = 200, reads = 0;
  await page.route('**/browser/activity?*', route => { reads++; return route.fulfill({ status, json: { activities: [] } }); });
  await authorize(page);
  await page.getByRole('link', { name: '查看活动记录' }).click();
  await expect(page.getByText('此页没有活动记录。')).toBeVisible();
  status = 500;
  await page.getByRole('button', { name: '刷新记录' }).click();
  await expect(page.getByRole('alert')).toContainText('暂时无法读取');
  status = 401;
  await page.getByRole('button', { name: '刷新记录' }).click();
  await expect(page.getByRole('status')).toHaveText('需要浏览器授权');
  await expect(page.getByRole('region', { name: '请求活动' })).toHaveCount(0);
  const before = reads;
  await page.route('**/api/v1/status', async route => {
    const response = await route.fetch();
    const body = await response.json();
    body.info.capabilities = body.info.capabilities.filter((capability: string) => capability !== 'browser.activity');
    await route.fulfill({ response, json: body });
  });
  await page.reload();
  await expect(page.getByRole('heading', { name: '当前服务不支持活动记录' })).toBeVisible();
  await page.getByRole('link', { name: '返回管理首页' }).click();
  await expect(page.getByRole('status')).toHaveText('浏览器已授权');
  await expect(page.getByRole('link', { name: '查看活动记录' })).toHaveCount(0);
  expect(reads).toBe(before);
});
