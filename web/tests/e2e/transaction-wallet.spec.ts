import { expect, test, type Page } from '@playwright/test';
import { createReview } from './transaction-fixture';
import { connectionClient, origin } from './bridge';

import { install } from './review-wallet-fixture';

test('fixed wallet reads all authorized accounts and reacts to network, disconnect and late reads', async ({ page }) => {
  const id = await createReview(page, '0x47', {});
  await install(page);
  await page.goto('/?transaction=' + id);
  const panel = page.getByRole('region', { name: '钱包状态核对' });
  await expect(panel.getByText('固定发送账户可用')).toBeVisible();
  await page.reload();
  await expect(panel.getByText('固定发送账户可用')).toBeVisible();
  await expect(panel.getByText('网络匹配', { exact: true })).toBeVisible();
  await page.evaluate(() => {
    const w = window.reviewWallet;
    w.delayAccountRead();
    w.event('accountsChanged');
  });
  await expect(panel.getByText('正在读取钱包状态…')).toBeVisible();
  await page.evaluate(() => {
    const w = window.reviewWallet;
    w.accounts = [];
    w.chain = '0x1';
    w.event('chainChanged');
  });
  await expect(panel.getByText('尚未提供账户', { exact: false })).toBeVisible();
  await page.evaluate(() => window.reviewWallet.finishAccountRead());
  await expect(panel.getByText(/网络不匹配/)).toBeVisible();
  await expect(panel.getByText('固定发送账户可用')).toHaveCount(0);
  await page.evaluate(() => window.reviewWallet.event('disconnect'));
  await expect(panel.getByText(/钱包已断开/)).toBeVisible();
  await expect(panel.getByText(/网络不匹配/)).toHaveCount(0);
  await page.evaluate(() => window.reviewWallet.event('connect'));
  await expect(panel.getByText(/网络不匹配/)).toBeVisible();
  const calls = await page.evaluate(() => [...new Set(window.reviewWallet.calls)].sort());
  expect(calls).toEqual(['fixed/eth_accounts', 'fixed/eth_chainId']);
});

test('a late identical candidate invalidates the match and the original transaction can still be rejected', async ({ page }) => {
  const id = await createReview(page, '0x47', {});
  await install(page);
  await page.goto('/?transaction=' + id);
  await expect(page.getByText('固定发送账户可用')).toBeVisible();
  await page.evaluate(() => window.reviewWallet.duplicate());
  await expect(page.getByText(/多个钱包使用相同/)).toBeVisible();
  await expect(page.getByText('固定发送账户可用')).toHaveCount(0);
  await page.getByRole('button', { name: '重新检测钱包' }).click();
  expect(await page.evaluate(() => window.reviewWallet.calls.some(call => call.startsWith('other/')))).toBe(false);
  await page.getByRole('button', { name: '拒绝这笔交易' }).click();
  await expect(page.getByText('用户已拒绝', { exact: true })).toBeVisible();
  await expect(page.getByRole('region', { name: '钱包状态核对' })).toHaveCount(0);
});

test('legacy injection requires confirmation on reopening and replacement', async ({ page }) => {
  const id = await createReview(page, '0x47', {}, undefined, { kind: 'legacy', name: '浏览器注入钱包' });
  await install(page, true);
  await page.goto('/?transaction=' + id);
  const confirm = page.getByRole('button', { name: '使用当前注入钱包' });
  await expect(confirm).toBeVisible();
  expect(await page.evaluate(() => window.reviewWallet.calls)).toEqual([]);
  await confirm.click();
  await expect(page.getByText('固定发送账户可用')).toBeVisible();
  await page.evaluate(() => { window.reviewWallet.replaceLegacy(); window.dispatchEvent(new Event('focus')); });
  await expect(confirm).toBeVisible();
  await expect(page.getByText('固定发送账户可用')).toHaveCount(0);
  expect(await page.evaluate(() => window.reviewWallet.calls.some(call => call.startsWith('other/')))).toBe(false);
  await page.reload();
  await expect(confirm).toBeVisible();
  expect(await page.evaluate(() => window.reviewWallet.calls)).toEqual([]);
});

async function openMismatched(page: Page, id: string) {
  await install(page);
  await page.goto('/?transaction=' + id);
  await expect(page.getByText('固定发送账户可用')).toBeVisible();
  await page.evaluate(() => {
    window.reviewWallet.chain = '0x1';
    window.reviewWallet.event('chainChanged');
  });
  return page.getByRole('button', { name: /切换到 Conflux eSpace Testnet/ });
}

test('the first wallet action claims the review, a refusal can be retried, and another tab can resume after closure', async ({ page, context }) => {
  const id = await createReview(page, '0x47', {});
  const switchNetwork = await openMismatched(page, id);
  const other = await context.newPage();
  const otherSwitch = await openMismatched(other, id);
  const path = origin + '/api/v1/browser/transactions/' + id + '/execution';
  await expect(switchNetwork).toBeEnabled();
  await expect(otherSwitch).toBeEnabled();
  expect((await (await page.request.get(path)).json()).phase).toBe('unclaimed');
  await page.evaluate(() => { window.reviewWallet.switchMode = 'reject'; });
  await switchNetwork.click();
  await expect(page.getByText(/交易仍可在原期限内重试/)).toBeVisible();
  expect((await connectionClient('transactions/' + id)).state).toBe('pending');
  await expect(otherSwitch).toBeDisabled();
  await expect(other.getByText(/此交易正在其他页面审阅/)).toBeVisible();
  await page.evaluate(() => { window.reviewWallet.switchMode = 'approve'; });
  await switchNetwork.click();
  await expect(page.getByText('网络匹配', { exact: true })).toBeVisible();
  expect((await (await page.request.get(path)).json()).phase).toBe('ready');
  expect(await page.evaluate(() => window.reviewWallet.calls.filter(call => call.endsWith('/wallet_switchEthereumChain')).length)).toBe(2);
  await page.close();
  await other.getByRole('button', { name: '继续审阅', exact: true }).click();
  await expect(otherSwitch).toBeEnabled();
  await other.evaluate(() => { window.reviewWallet.accounts = []; window.reviewWallet.event('accountsChanged'); });
  await other.getByRole('button', { name: '授权访问账户' }).click();
  await expect(other.getByText('固定发送账户可用')).toBeVisible();
  expect(await other.evaluate(() => window.reviewWallet.calls.filter(call => call.endsWith('/eth_requestAccounts')).length)).toBe(1);
  await other.getByRole('button', { name: '拒绝这笔交易' }).click();
  await expect(other.getByText('用户已拒绝', { exact: true })).toBeVisible();
});

test('a lost preparation permit never opens the wallet and a lost return does not repeat it', async ({ page }) => {
  const id = await createReview(page, '0x47', {});
  const switchNetwork = await openMismatched(page, id);
  let lose = 'begin_preparation';
  const receipts: Record<string, string[]> = {};
  await page.route('**/transactions/' + id + '/execution', async route => {
    if (route.request().method() === 'POST') {
      const { action, callId } = route.request().postDataJSON();
      (receipts[action] ??= []).push(callId);
      if (action === lose) {
        lose = '';
        expect((await route.fetch()).status()).toBe(200);
        await route.abort();
        return;
      }
    }
    await route.continue();
  });
  await switchNetwork.click();
  await expect(page.getByText(/钱包操作未执行/)).toBeVisible();
  expect(receipts.begin_preparation).toHaveLength(2);
  expect(new Set(receipts.begin_preparation).size).toBe(1);
  expect(await page.evaluate(() => window.reviewWallet.calls.some(call => call.endsWith('/wallet_switchEthereumChain')))).toBe(false);
  lose = 'return_preparation';
  await switchNetwork.click();
  await expect(page.getByText('网络匹配', { exact: true })).toBeVisible();
  await expect(page.getByText(/钱包操作已返回/)).toBeVisible();
  expect(receipts.return_preparation.slice(-2)).toEqual([receipts.begin_preparation[2], receipts.begin_preparation[2]]);
  expect(await page.evaluate(() => window.reviewWallet.calls.filter(call => call.endsWith('/wallet_switchEthereumChain')).length)).toBe(1);
  await connectionClient('transactions/' + id + '/cancel', {});
});

test('canceling a transaction during switching keeps the popup occupied until a confirmed release', async ({ page, context }) => {
  const id = await createReview(page, '0x47', {});
  const switchNetwork = await openMismatched(page, id);
  await page.evaluate(() => { window.reviewWallet.switchMode = 'hold'; });
  await switchNetwork.click();
  await expect.poll(() => page.evaluate(() => window.reviewWallet.calls.includes('fixed/wallet_switchEthereumChain'))).toBe(true);
  await connectionClient('transactions/' + id + '/cancel', {});
  const other = await context.newPage();
  await other.goto('/?transaction=' + id);
  await expect(other.getByText('已取消', { exact: true })).toBeVisible();
  const status = async () => (await (await other.request.get(origin + '/api/v1/status')).json()).activeWalletRequest;
  expect(await status()).toMatchObject({ kind: 'transaction', id, phase: 'interrupted_calling' });
  await expect(other.getByRole('button', { name: '已处理钱包弹窗，解除占用' })).toBeEnabled();
  await page.getByRole('button', { name: '已处理钱包弹窗，解除占用' }).click();
  await expect.poll(status).toBeUndefined();
  const returned = page.waitForResponse(r => r.request().method() === 'POST' && r.url().endsWith('/execution') &&
    r.request().postDataJSON()?.action === 'return_preparation');
  await page.evaluate(() => window.reviewWallet.finishSwitch());
  await returned;
  expect((await connectionClient('transactions/' + id)).state).toBe('canceled');
});
