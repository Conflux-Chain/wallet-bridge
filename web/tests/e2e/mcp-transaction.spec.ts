import { expect, test } from '@playwright/test';
import { withMCP } from './mcp-stdio';
import { cli, origin } from './bridge';
import { account, createReview, createSession, newID, to } from './transaction-fixture';
import { install } from './review-wallet-fixture';

function mcpOptions() {
  return {
    binary: process.env.WALLET_BRIDGE_TEST_EXECUTABLE!,
    args: ['--data-dir', process.env.WALLET_BRIDGE_TEST_DATA!],
    clientInfo: { name: 'test-host', title: 'MCP 交易客户端', version: '1' },
  };
}

test('MCP creates one immutable review for raw calldata and replays it after Session closure', async ({page}) => {
  const sessionId = await createSession(page, '0x47');
  const requestId = newID();
  const transaction = { from: account, to, chainId: '71', data: '0x00deadc0de' };
  const args = { requestId, sessionId, transaction };
  const options = mcpOptions();
  await withMCP(options, async (_init, request) => {
    const create = (input: object) => request('tools/call', { name: 'create_transaction', arguments: input });
    for (const invalid of [{chainId: '1'}, {from: to}, {gas: ''}]) {
      expect((await create({ ...args, transaction: { ...transaction, ...invalid } })).isError).toBe(true);
    }
    const result = await create(args);
    const receipt = result.structuredContent;
    const url = origin + '/?transaction=' + requestId;
    expect(receipt).toMatchObject({ id: requestId, sessionId, state: 'pending', intent: '', clientName: options.clientInfo.title, url,
      transaction: { ...transaction, chainId: '0x47', value: '0x0' } });
    expect(result.content[0].text).toContain('](' + url + ')');
    await install(page);
    await page.goto(url);
    await expect(page.getByText(options.clientInfo.title, {exact: true})).toBeVisible();
    await expect(page.getByText('调用方未提供说明')).toBeVisible();
    await page.getByText(/原始 data · 5 字节/).click();
    await expect(page.getByText(transaction.data, {exact: true})).toBeVisible();
    expect(await page.evaluate(() => window.reviewWallet.sends.length)).toBe(0);
    const replay = await create({ ...args, transaction: { ...transaction, chainId: '0x47', value: '0' } });
    expect(replay.structuredContent).toEqual(receipt);
    const conflict = await create({ ...args, intent: '不同说明' });
    expect(conflict.isError).toBe(true);
    expect(conflict.content[0].text).toContain('transaction_conflict');
    await cli(['session', 'close', sessionId, '--json']);
    const canceled = (await create(args)).structuredContent;
    expect(canceled).toMatchObject({ ...receipt, state: 'canceled', finishedAt: expect.any(String) });
    const fresh = await create({ ...args, requestId: newID() });
    expect(fresh.isError).toBe(true);
    expect(fresh.content[0].text).toContain('session_closed');
  });
});

test('MCP cancels pending transactions idempotently and preserves an earlier rejection', async ({page}) => {
  const id = await createReview(page, '0x47', {});
  await withMCP(mcpOptions(), async (_init, request) => {
    const call = (name: string, requestId: string) => request('tools/call', { name, arguments: { requestId } });
    const original = (await call('get_transaction', id)).structuredContent;
    expect(original).toMatchObject({ id, state: 'pending' });
    const canceled = (await call('cancel_transaction', id)).structuredContent;
    expect(canceled).toEqual({ ...original, state: 'canceled', finishedAt: expect.any(String) });
    expect((await call('cancel_transaction', id)).structuredContent).toEqual(canceled);
    expect((await call('get_transaction', id)).structuredContent).toEqual(canceled);
    const other = await createReview(page, '0x47', {});
    await page.goto('/?transaction=' + other);
    await page.getByRole('button', {name: '拒绝这笔交易'}).click();
    await expect(page.getByText('用户已拒绝', {exact: true})).toBeVisible();
    expect((await call('cancel_transaction', other)).structuredContent).toMatchObject({id: other, state: 'rejected'});
    const missing = await call('get_transaction', newID());
    expect(missing.isError).toBe(true);
    expect(missing.content[0].text).toContain('transaction_not_found');
  });
});

test('MCP follows the original wallet handoff through uncertainty to a late hash', async ({page}) => {
  test.setTimeout(90_000);
  const requestId = await createReview(page, '0x47', {});
  await install(page);
  await page.goto('/?transaction=' + requestId);
  await page.getByRole('button', {name: '在钱包中确认并发送'}).click();
  await expect.poll(() => page.evaluate(() => window.reviewWallet.sends.length)).toBe(1);
  await withMCP(mcpOptions(), async (_init, request) => {
    const call = (name: string) => request('tools/call', {name, arguments: {requestId}});
    const waiting = await call('cancel_transaction');
    expect(waiting.structuredContent).toMatchObject({id: requestId, state: 'awaiting_wallet'});
    await cli(['service', 'stop', '--json']);
    const uncertain = await call('get_transaction');
    expect(uncertain.structuredContent).toMatchObject({id: requestId, state: 'uncertain'});
    expect(uncertain.structuredContent.txHash).toBeUndefined();
    expect(uncertain.content[0].text).toContain('may have been broadcast');
    expect(uncertain.content[0].text).toContain('End automatic waiting');
    expect((await call('cancel_transaction')).structuredContent).toEqual(uncertain.structuredContent);
  });

  const hash = '0x' + '85'.repeat(32);
  await page.evaluate(hash => window.reviewWallet.finishSend({ hash }), hash);
  const recheck = page.getByRole('button', { name: '重新检查', exact: true });
  if (await recheck.isVisible()) await recheck.click();
  await withMCP(mcpOptions(), async (_init, request) => {
    const get = () => request('tools/call', {name: 'get_transaction', arguments: {requestId}});
    await expect.poll(async () => (await get()).structuredContent.txHash).toBe(hash);
    const sent = await get();
    expect(sent.structuredContent).toMatchObject({id: requestId, state: 'sent', txHash: hash});
    expect(sent.content[0].text).toContain(hash);
  });
  expect(await page.evaluate(() => window.reviewWallet.sends.length)).toBe(1);
});
