import { expect, test } from '@playwright/test';
import { cli, connectionClient } from './bridge';
import { account, createSession, to } from './transaction-fixture';
import { install } from './review-wallet-fixture';

async function createTransaction(sessionId: string, clientName: string) {
  const receipt = JSON.parse(await cli(['transaction', 'send', '--client-name', clientName,
    '--session-id', sessionId, '--chain-id', '71', '--from', account, '--to', to, '--no-wait', '--no-open', '--json']));
  expect(receipt.state).toBe('pending');
  return receipt as { id: string; url: string };
}

test('two clients recover across restart, release the wallet, and receive only their own late result', async ({ page, context }) => {
  test.setTimeout(90_000);
  const sessionA = await createSession(page, '0x47');
  const sessionB = await createSession(page, '0x47');
  const a = await createTransaction(sessionA, 'CLI A');
  const b = await createTransaction(sessionB, 'CLI B');
  const bOriginal = await connectionClient('transactions/' + b.id);
  await install(page);
  await page.goto(a.url);
  await page.getByRole('button', { name: '在钱包中确认并发送' }).click();
  await expect.poll(() => page.evaluate(() => window.reviewWallet.sends.length)).toBe(1);

  await cli(['service', 'stop', '--json']);
  await cli(['service', 'ensure', '--json']);
  const recovered = await connectionClient('transactions/' + a.id);
  expect(recovered).toMatchObject({ state: 'uncertain', sessionId: sessionA });
  expect(recovered.txHash).toBeUndefined();
  expect(await connectionClient('transactions/' + b.id)).toEqual(bOriginal);

  const waiting = await context.newPage();
  await install(waiting);
  await waiting.goto(b.url);
  const notice = waiting.getByRole('region', { name: '钱包占用情况' });
  const occupying = notice.getByRole('link');
  await expect(occupying).toHaveAttribute('href', '/?transaction=' + a.id);
  const popup = waiting.waitForEvent('popup');
  await occupying.click();
  const recovery = await popup;
  await recovery.getByRole('button', { name: '已处理钱包弹窗，解除占用' }).click();
  await expect(recovery.getByRole('button', { name: '已处理钱包弹窗，解除占用' })).toHaveCount(0);

  // B holds a new wallet operation when A's delayed response arrives.
  await waiting.reload();
  await expect(waiting.getByText('固定发送账户可用')).toBeVisible();
  await waiting.evaluate(() => {
    window.reviewWallet.chain = '0x1';
    window.reviewWallet.event('chainChanged');
  });
  await waiting.getByRole('button', { name: /切换到 Conflux eSpace Testnet/ }).click();
  await expect(waiting.getByText('网络匹配', { exact: true })).toBeVisible();
  const active = async () => (await (await waiting.request.get('/api/v1/status')).json()).activeWalletRequest;
  expect(await active()).toMatchObject({ kind: 'transaction', id: b.id, phase: 'ready' });

  const hash = '0x' + '81'.repeat(32);
  await page.evaluate(hash => window.reviewWallet.finishSend({ hash }), hash);
  // A may have observed the service outage while its wallet promise was pending.
  const recheck = page.getByRole('button', { name: '重新检查', exact: true });
  if (await recheck.isVisible()) await recheck.click();
  await expect.poll(async () => (await connectionClient('transactions/' + a.id)).txHash).toBe(hash);
  expect(await active()).toMatchObject({ kind: 'transaction', id: b.id, phase: 'ready' });
  expect(await connectionClient('transactions/' + b.id)).toEqual(bOriginal);
  expect(await page.evaluate(() => window.reviewWallet.sends.length)).toBe(1);
  expect(await waiting.evaluate(() => window.reviewWallet.sends.length)).toBe(0);
  await waiting.getByRole('button', { name: '拒绝这笔交易' }).click();
  await expect(waiting.getByText('用户已拒绝', { exact: true })).toBeVisible();

  await cli(['session', 'close', sessionA, '--json']);
  await recovery.goto('/?view=activity');
  const originalRow = recovery.getByRole('listitem').filter({ hasText: a.id });
  await expect(originalRow).toContainText(hash);
  await originalRow.getByRole('button', { name: '从活动列表移除' }).click();
  await expect(originalRow).toHaveCount(0);
  expect(JSON.parse(await cli(['transaction', 'get', a.id, '--json']))).toMatchObject({ state: 'sent', sessionId: sessionA, txHash: hash });
  expect(JSON.parse(await cli(['transaction', 'get', b.id, '--json']))).toMatchObject({ state: 'rejected', sessionId: sessionB });
});
