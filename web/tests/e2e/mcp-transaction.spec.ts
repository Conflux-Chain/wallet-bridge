import { expect, test } from '@playwright/test';
import { withMCP } from './mcp-stdio';
import { cli, origin } from './bridge';
import { account, createSession, newID, to } from './transaction-fixture';
import { install } from './review-wallet-fixture';

test('MCP creates one immutable review for raw calldata and replays it after Session closure', async ({page}) => {
  const sessionId = await createSession(page, '0x47');
  const requestId = newID();
  const transaction = { from: account, to, chainId: '71', data: '0x00deadc0de' };
  const args = { requestId, sessionId, transaction };
  const options = {
    binary: process.env.WALLET_BRIDGE_TEST_EXECUTABLE!,
    args: ['--data-dir', process.env.WALLET_BRIDGE_TEST_DATA!],
    clientInfo: { name: 'test-host', title: 'MCP 交易客户端', version: '1' },
  };
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
