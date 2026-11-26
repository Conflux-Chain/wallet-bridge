import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { expect, test } from '@playwright/test';
import { cli } from '../e2e/bridge';
import { withMCP } from '../e2e/mcp-stdio';
import { install } from '../e2e/review-wallet-fixture';
import { account, createSession, to } from '../e2e/transaction-fixture';

test('an open review and old clients recover one late wallet hash across an upgrade', async ({ page, context }) => {
  test.setTimeout(90_000);
  const originalVersion = JSON.parse(await cli(['service', 'status', '--json'])).info.version;
  const sessionId = await createSession(page, '0x47');
  const receipt = JSON.parse(await cli(['transaction', 'send', '--session-id', sessionId, '--chain-id', '71',
    '--from', account, '--to', to, '--value', '13', '--no-open', '--no-wait', '--json']));
  const cliGet = async () => JSON.parse(await cli(['transaction', 'get', receipt.id, '--json']));
  const initial = await cliGet();
  expect(initial).toMatchObject({ id: receipt.id, sessionId, state: 'pending', expiresAt: expect.any(String) });
  const identity = { id: receipt.id, sessionId, createdAt: initial.createdAt, expiresAt: initial.expiresAt,
    transaction: initial.transaction };

  await install(page);
  await page.goto(receipt.url);
  await withMCP({
    binary: process.env.WALLET_BRIDGE_TEST_EXECUTABLE!, args: ['--data-dir', process.env.WALLET_BRIDGE_TEST_DATA!],
    clientInfo: { name: 'upgrade-test', version: '1' },
  }, async (_init, request) => {
    const mcpGet = async () => (await request('tools/call', { name: 'get_transaction', arguments: { requestId: receipt.id } })).structuredContent;
    expect(await mcpGet()).toMatchObject({ ...identity, state: 'pending' });
    await page.getByRole('button', { name: '在钱包中确认并发送' }).click();
    const sends = () => page.evaluate(() => window.reviewWallet.sends.length);
    await expect.poll(sends).toBe(1);
    expect(await mcpGet()).toMatchObject({ ...identity, state: 'awaiting_wallet' });

    // Interrupt the old service, then use the normal recovery control while
    // the original tab still holds the wallet promise.
    await cli(['service', 'stop', '--json']);
    expect(await mcpGet()).toMatchObject({ ...identity, state: 'uncertain' });
    const recovery = await context.newPage();
    await recovery.goto(receipt.url);
    const release = recovery.getByRole('button', { name: '已处理钱包弹窗，解除占用' });
    await release.click();
    await expect(release).toHaveCount(0);
    await recovery.close();

    await promisify(execFile)(process.env.WALLET_BRIDGE_TEST_PREPARE!,
      ['-test.run=^TestPrepareBrowserSwitch$'], { windowsHide: true, timeout: 30_000 });
    const switched = JSON.parse(await cli(['upgrade', '--json']));
    expect(switched).toMatchObject({ current: originalVersion, latest: expect.any(String), state: 'upgraded' });
    expect(switched.latest).not.toBe(originalVersion);
    expect(JSON.parse(await cli(['service', 'status', '--json'])).info.version).toBe(switched.latest);
    expect(await mcpGet()).toMatchObject({ ...identity, state: 'uncertain' });

    // Recheck status in the same document so the original provider and its
    // pending send survive. Observe the response the page itself consumes.
    await page.bringToFront();
    const status = page.waitForResponse(r => r.url().endsWith('/api/v1/status') && r.ok());
    await page.evaluate(() => window.dispatchEvent(new Event('focus')));
    const recheck = page.getByRole('button', { name: '重新检查', exact: true });
    if (await recheck.isVisible()) await recheck.click();
    expect((await (await status).json()).info.version).toBe(switched.latest);
    const hash = '0x' + '93'.repeat(32);
    await page.evaluate(hash => window.reviewWallet.finishSend({ hash }), hash);
    await expect(page.getByText(hash, { exact: true })).toBeVisible();
    const sent = { ...identity, state: 'sent', txHash: hash };
    expect(await cliGet()).toMatchObject(sent);
    expect(await mcpGet()).toMatchObject(sent);
    await expect(page.getByText(receipt.id, { exact: true })).toBeVisible();
    expect(await sends()).toBe(1);
  });
});
