import { randomBytes } from 'node:crypto';
import { expect, test, type BrowserContext, type Page } from '@playwright/test';
import { authorize, connectionClient } from './bridge';

declare global {
  interface Window { countWalletPrompt(): Promise<void>; }
}

async function createRequest(name: string): Promise<string> {
  const id = randomBytes(16).toString('hex');
  await connectionClient('connections', { id, clientName: name, chainId: '71' });
  return id;
}

async function waitInWallet(context: BrowserContext, owner: Page, id: string) {
  let prompts = 0;
  await context.exposeFunction('countWalletPrompt', () => { prompts++; });
  await context.addInitScript(() => {
    Object.defineProperty(window, 'ethereum', { value: {
      on() {}, removeListener() {},
      async request({ method }: { method: string }) {
        if (method === 'eth_accounts') return [];
        if (method === 'eth_chainId') return '0x47';
        if (method !== 'eth_requestAccounts') throw Error('Unexpected wallet method');
        await window.countWalletPrompt();
        return new Promise((resolve) => window.addEventListener('settle-wallet', () => resolve([]), { once: true }));
      },
    } });
  });
  await authorize(owner);
  await expect(owner.getByRole('status')).toHaveText('浏览器已授权');
  await owner.goto('/?connection=' + id);
  await owner.getByRole('radio').check();
  await owner.getByRole('button', { name: '读取账户和网络' }).click();
  const begin = owner.waitForRequest(r => r.url().endsWith('/execution/begin'));
  await owner.getByRole('button', { name: '授权访问账户' }).click();
  await expect.poll(() => prompts).toBe(1);
  return { handoff: (await begin).postDataJSON(), count: () => prompts };
}

async function findOwner(page: Page, id: string) {
  const link = page.getByRole('link', { name: '在新标签页查看占用钱包的请求' });
  await expect(link).toHaveAttribute('href', '/?connection=' + id);
  const opening = page.context().waitForEvent('page');
  await link.click();
  const found = await opening;
  await expect(found.getByText('原请求 A', { exact: true })).toBeVisible();
  return found;
}

test('waiting tab finds the closed owner and explicitly resumes the pending request', async ({ context, page }) => {
  const a = await createRequest('原请求 A');
  const b = await createRequest('等待请求 B');
  const wallet = await waitInWallet(context, page, a);
  const waiting = await context.newPage();
  await waiting.goto('/?connection=' + b);
  const observer = await findOwner(waiting, a);
  await expect(observer.getByText('正在等待钱包操作结束。')).toBeVisible();
  await expect(observer.getByRole('button', {name:/继续连接/})).toHaveCount(0);
  await observer.close();
  await page.close({ runBeforeUnload: true });
  const recovered = await findOwner(waiting, a);
  await recovered.getByRole('button', { name: '已处理原钱包弹窗，继续连接' }).click();
  await expect(recovered.getByRole('radio')).not.toBeChecked();
  expect(wallet.count()).toBe(1);
  const late = await recovered.evaluate(async ({ a, handoff }) => {
    const response = await fetch('/api/v1/browser/connections/' + a + '/execution/return', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ id: handoff.id, callId: handoff.callId }),
    });
    return response.status;
  }, { a, handoff: wallet.handoff });
  expect(late).toBe(409);
  expect((await connectionClient('connections/' + a)).state).toBe('pending');
  await recovered.getByRole('button', { name: '拒绝本次连接' }).click();
  await expect(recovered.getByRole('heading', { name: '已拒绝本次连接' })).toBeVisible();
  await waiting.getByRole('button', { name: '重新检查请求' }).click();
  await expect(waiting.getByRole('button', { name: '拒绝本次连接' })).toBeEnabled();
  expect((await connectionClient('connections/' + b)).state).toBe('pending');
});

test('management page finds a canceled request and the late wallet result leaves the slot free', async ({ context, page }) => {
  const a = await createRequest('原请求 A');
  const wallet = await waitInWallet(context, page, a);
  await connectionClient('connections/' + a + '/cancel', {});
  const home = await context.newPage();
  await home.goto('/');
  const found = await findOwner(home, a);
  await expect(found.getByRole('heading', { name: '调用方已取消连接' })).toBeVisible();
  await found.getByRole('button', { name: '已处理原钱包弹窗，解除等待' }).click();
  await expect(found.getByRole('button', { name: /解除等待/ })).toHaveCount(0);
  const callback = page.waitForResponse(r => r.url().endsWith('/execution/return'));
  await page.evaluate(() => window.dispatchEvent(new Event('settle-wallet')));
  await callback;
  await home.getByRole('button', { name: '重新检查', exact: true }).click();
  await expect(home.getByRole('link', { name: /查看占用钱包的请求/ })).toHaveCount(0);
  expect(await connectionClient('connections/' + a)).toMatchObject({ state: 'canceled' });
  expect(wallet.count()).toBe(1);
});
