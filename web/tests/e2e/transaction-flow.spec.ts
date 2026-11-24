import { expect, test } from '@playwright/test';
import { authorize, cli, waitingCLI } from './bridge';
import { install } from './review-wallet-fixture';
import { account } from './transaction-fixture';

test('CLI connects the selected wallet, sends from its Session and reads the hash after closure', async ({ page }) => {
  await install(page);
  await authorize(page);
  await expect(page.getByRole('status')).toHaveText('浏览器已授权');

  const connection = await waitingCLI(['connect', '--chain-id', '71', '--no-open', '--json']);
  await page.goto(connection.receipt.url);
  await page.getByRole('radio', { name: /test\.wallet/ }).check();
  await page.getByRole('button', { name: '读取账户和网络' }).click();
  await page.getByRole('combobox', { name: '本次连接账户' }).selectOption(account);
  await page.getByRole('button', { name: '同意连接' }).click();
  const connected = await connection.done;
  expect(connected.code).toBe(0);
  const { session } = JSON.parse(connected.stdout);
  expect(session).toMatchObject({ account, chainId: '0x47', wallet: { rdns: 'test.wallet' } });

  const send = await waitingCLI(['transaction', 'send', '--session-id', session.id, '--chain-id', session.chainId,
    '--from', session.account, '--to', session.account, '--intent', '测试账户零金额自转', '--no-open', '--json']);
  await page.goto(send.receipt.url);
  await expect(page.getByText('测试账户零金额自转', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: '在钱包中确认并发送' }).click();
  const transaction = { from: account, to: account, chainId: '0x47', value: '0x0', data: '0x' };
  await expect.poll(() => page.evaluate(() => window.reviewWallet.sends)).toEqual([[transaction]]);
  expect(await page.evaluate(() => window.reviewWallet.calls.filter(call => call.endsWith('/eth_sendTransaction'))))
    .toEqual(['fixed/eth_sendTransaction']);
  const txHash = '0x' + '61'.repeat(32);
  await page.evaluate(hash => window.reviewWallet.finishSend({ hash }), txHash);

  const sent = await send.done;
  expect(sent.code).toBe(0);
  const expected = { id: send.receipt.id, sessionId: session.id, state: 'sent', transaction, txHash };
  expect(JSON.parse(sent.stdout)).toMatchObject(expected);
  expect(JSON.parse(await cli(['session', 'close', session.id, '--json'])).state).toBe('closed');
  expect(JSON.parse(await cli(['transaction', 'get', send.receipt.id, '--json']))).toMatchObject(expected);
});
