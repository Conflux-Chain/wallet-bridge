import { expect, test } from '@playwright/test';
import { withMCP } from './mcp-stdio';
import { authorize, cli, origin } from './bridge';
import { account, newID } from './transaction-fixture';
import { install } from './review-wallet-fixture';

function mcpOptions() {
  return {
    binary: process.env.WALLET_BRIDGE_TEST_EXECUTABLE!,
    args: ['--data-dir', process.env.WALLET_BRIDGE_TEST_DATA!],
    clientInfo: { name: 'bridge-test-host', title: 'MCP 测试客户端', version: '1' },
  };
}

test('MCP reconnects by the original ID and reads the browser consent result', async ({page}) => {
  test.setTimeout(90_000);
  const options = mcpOptions();
  const requestId = newID();
  const input = { requestId, chainId: '71', intent: '验证连接' };
  await cli(['service', 'stop', '--json']);
  await withMCP(options, async (_init, request) => {
    // Discard the creation result as if the caller had lost its response.
    await request('tools/call', { name: 'create_connection', arguments: input });
  });
  const original = JSON.parse(await cli(['connection', 'get', requestId, '--json']));
  expect(original).toMatchObject({ state: 'pending', clientName: options.clientInfo.title });
  const fallback = { name: options.clientInfo.title, version: '1' };
  await withMCP({ ...options, clientInfo: fallback }, async (_init, request) => {
    const call = (name: string, args: object) => request('tools/call', { name, arguments: args });
    const replay = await call('create_connection', input);
    const url = origin + '/?connection=' + requestId;
    expect(replay.structuredContent).toEqual({ ...original, kind: 'connection', url });
    expect(replay.content[0].text).toContain('](' + url + ')');
    expect(replay.content[0].text).toContain('在系统默认浏览器继续');
    const changed = await call('create_connection', { ...input, chainId: '1' });
    expect(changed.isError).toBe(true);
    expect(changed.content[0].text).toContain('connection_conflict');
    await authorize(page);
    await expect(page.getByRole('status')).toHaveText('浏览器已授权');
    await install(page);
    await page.goto(url);
    await expect(page.getByText(options.clientInfo.title, {exact: true})).toBeVisible();
    await page.getByRole('radio', {name: /test\.wallet/}).check();
    await page.getByRole('button', {name: '读取账户和网络'}).click();
    await page.getByRole('combobox', {name: '本次连接账户'}).selectOption(account);
    await page.getByRole('button', {name: '同意连接'}).click();
    await expect(page.getByRole('heading', {name: '已同意连接'})).toBeVisible();
    const approved = (await call('get_connection', {requestId})).structuredContent;
    expect(approved).toMatchObject({ state: 'succeeded', id: requestId, chainId: '0x47', url, sessionId: expect.any(String) });
    const session = { sessionId: approved.sessionId };
    const active = (await call('get_session', session)).structuredContent;
    expect(active).toMatchObject({ state: 'active', id: session.sessionId, connectionId: requestId, account, chainId: '0x47',
      wallet: { name: '测试钱包', rdns: 'test.wallet' } });
    expect((await call('cancel_connection', {requestId})).structuredContent).toEqual(approved);
    const closed = (await call('close_session', session)).structuredContent;
    expect(closed).toEqual({ ...active, state: 'closed', closedAt: expect.any(String) });
    expect((await call('close_session', session)).structuredContent).toEqual(closed);
    expect((await call('get_session', session)).structuredContent).toEqual(closed);
    expect((await call('get_connection', {requestId})).structuredContent).toEqual(approved);
  });
});

test('MCP cancels a pending connection after service restart and preserves the result', async () => {
  const requestId = newID();
  await withMCP(mcpOptions(), async (_init, request) => {
    const call = (name: string, args: object) => request('tools/call', {name, arguments: args});
    const original = (await call('create_connection', {requestId, chainId: '71'})).structuredContent;
    expect(original.state).toBe('pending');
    await cli(['service', 'stop', '--json']);
    const canceled = (await call('cancel_connection', {requestId})).structuredContent;
    expect(canceled).toEqual({ ...original, state: 'canceled', finishedAt: expect.any(String) });
    expect((await call('cancel_connection', {requestId})).structuredContent).toEqual(canceled);
    expect((await call('get_connection', {requestId})).structuredContent).toEqual(canceled);
    const missing = await call('get_session', {sessionId: newID()});
    expect(missing.isError).toBe(true);
    expect(missing.content[0].text).toContain('session_not_found');
  });
});
