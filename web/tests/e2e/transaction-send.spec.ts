import { expect, test, type Page } from '@playwright/test';
import { authorize, connectionClient, origin } from './bridge';
import { createReview } from './transaction-fixture';
import { install } from './review-wallet-fixture';

const txHash = '0x' + '72'.repeat(32);
const sendButton = (page: Page) => page.getByRole('button', { name: '在钱包中确认并发送' });
const countSends = (page: Page) => page.evaluate(() => window.reviewWallet.sends.length);

async function open(page: Page) {
  const id = await createReview(page, '0x47', { value: '13', data: '0x0001', gas: '75000' });
  await install(page);
  await page.goto('/?transaction=' + id);
  await expect(sendButton(page)).toBeEnabled();
  return id;
}

test('a double click sends one frozen payload and a lost hash response only replays the result', async ({ page }) => {
  const id = await open(page);
  const tx = (await connectionClient('transactions/' + id)).transaction;
  const reports: unknown[] = [];
  await page.route('**/' + id + '/result', async route => {
    reports.push(route.request().postDataJSON());
    if (reports.length === 1) { expect((await route.fetch()).ok()).toBe(true); await route.abort(); }
    else await route.continue();
  });
  await sendButton(page).evaluate(button => { (button as HTMLButtonElement).click(); (button as HTMLButtonElement).click(); });
  await expect(page.getByText('等待钱包', { exact: true })).toBeVisible();
  expect(await page.evaluate(() => window.reviewWallet.sends)).toEqual([[tx]]);
  expect(await page.evaluate(() => window.reviewWallet.calls.filter(c => c.endsWith('/eth_sendTransaction')))).toEqual(['fixed/eth_sendTransaction']);
  await page.evaluate(hash => window.reviewWallet.finishSend({ hash }), txHash);
  await expect(page.getByText(txHash, { exact: true })).toBeVisible();
  expect(reports).toHaveLength(2);
  expect(reports[1]).toEqual(reports[0]);
  await page.reload();
  await expect(page.getByText('已发送', { exact: true })).toBeVisible();
  expect(await countSends(page)).toBe(0);
});

test('an account change before clicking and a chain change during handoff both block the wallet send', async ({ page }) => {
  const id = await open(page);
  await page.evaluate(() => { window.reviewWallet.accounts = []; });
  await sendButton(page).click();
  await expect(sendButton(page)).toBeDisabled();
  expect((await connectionClient('transactions/' + id)).state).toBe('pending');
  await page.getByRole('button', { name: '授权访问账户' }).click();
  await expect(sendButton(page)).toBeEnabled();
  await page.route('**/' + id + '/send', async route => {
    const response = await route.fetch();
    expect((await response.json()).invoke).toBe(true);
    await page.evaluate(() => { window.reviewWallet.chain = '0x1'; });
    await route.fulfill({ response });
  });
  await sendButton(page).click();
  await expect(page.getByText('结果未确定', { exact: true })).toBeVisible();
  expect(await countSends(page)).toBe(0);
});

test('lost send permission is retried with original IDs and never invokes the wallet', async ({ page }) => {
  const id = await open(page);
  const permits: unknown[] = [];
  await page.route('**/' + id + '/send', async route => {
    permits.push(route.request().postDataJSON());
    const response = await route.fetch();
    expect((await response.json()).invoke).toBe(permits.length === 1);
    if (permits.length === 1) await route.abort();
    else await route.fulfill({ response });
  });
  await sendButton(page).click();
  await expect(page.getByText('结果未确定', { exact: true })).toBeVisible();
  expect(permits).toHaveLength(2);
  expect(permits[1]).toEqual(permits[0]);
  expect(await countSends(page)).toBe(0);
});

for (const [outcome, state] of [[{ code: 4001 }, '用户已拒绝'], [{ code: 4900 }, '结果未确定'], [{ hash: '0x01' }, '结果未确定']] as const) {
  test('wallet outcome ' + JSON.stringify(outcome) + ' is recorded on the original request', async ({ page }) => {
    await open(page);
    await sendButton(page).click();
    await expect.poll(() => countSends(page)).toBe(1);
    await page.evaluate(value => window.reviewWallet.finishSend(value), outcome);
    await expect(page.getByText(state, { exact: true })).toBeVisible();
    await expect(sendButton(page)).toHaveCount(0);
  });
}

test('another profile releases the interrupted popup and the reauthorized original tab submits its late hash', async ({ page, browser, context }) => {
  const id = await open(page);
  const original = (await (await page.request.get(origin + '/api/v1/status')).json()).browser.id;
  await sendButton(page).click();
  await expect.poll(() => countSends(page)).toBe(1);
  const logout = await page.request.post(origin + '/api/v1/browser/logout', { headers: { Origin: origin }, data: {} });
  expect(logout.ok()).toBe(true);
  await page.evaluate(() => window.dispatchEvent(new Event('focus')));
  await expect(page.getByText('需要浏览器授权', { exact: true })).toBeVisible();

  const other = await browser.newPage();
  try {
    await authorize(other);
    await expect(other.getByRole('status')).toHaveText('浏览器已授权');
    await other.goto(origin + '/?transaction=' + id);
    await other.getByRole('button', { name: '已处理钱包弹窗，解除占用' }).click();
    await expect(other.getByRole('button', { name: '已处理钱包弹窗，解除占用' })).toHaveCount(0);
    expect((await connectionClient('transactions/' + id)).state).toBe('uncertain');
    await page.evaluate(hash => window.reviewWallet.finishSend({ hash }), txHash);
    await expect.poll(() => page.evaluate(id => !!sessionStorage.getItem('wallet-bridge.result.' + id), id)).toBe(true);
    const auth = await context.newPage();
    await authorize(auth);
    await expect(auth.getByRole('status')).toHaveText('浏览器已授权');
    expect((await (await auth.request.get(origin + '/api/v1/status')).json()).browser.id).not.toBe(original);
    await page.getByRole('button', { name: '重新检查' }).click();
    await expect(page.getByText(txHash, { exact: true })).toBeVisible();
    await expect(other.getByText(txHash, { exact: true })).toBeVisible();
    expect(await countSends(page)).toBe(1);
    expect(await page.evaluate(id => sessionStorage.getItem('wallet-bridge.result.' + id), id)).toBeNull();
  } finally { await other.close(); }
});
