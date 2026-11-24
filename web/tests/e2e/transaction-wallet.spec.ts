import { expect, test, type Page } from '@playwright/test';
import { account, createReview, to } from './transaction-fixture';

interface WalletFixture {
  accounts: string[];
  chain: string;
  calls: string[];
  event(name: string): void;
  duplicate(): void;
  replaceLegacy(): void;
  delayAccountRead(): void;
  finishAccountRead(): void;
}
declare global { interface Window { reviewWallet: WalletFixture } }

async function install(page: Page, legacy = false) {
  await page.addInitScript(({ account, to, legacy }) => {
    const events = new EventTarget();
    let delayed = false;
    let finish = () => {};
    const fixture: WalletFixture = {
      accounts: [to, account], chain: '0x047', calls: [],
      event: name => events.dispatchEvent(new Event(name)),
      duplicate: () => announce('测试钱包', 'test.wallet', other),
      replaceLegacy: () => { Object.defineProperty(window, 'ethereum', { configurable: true, value: other }); },
      delayAccountRead: () => { delayed = true; },
      finishAccountRead: () => finish(),
    };
    function makeProvider(label: string) {
      return {
        on: (name: string, fn: EventListener) => events.addEventListener(name, fn),
        removeListener: (name: string, fn: EventListener) => events.removeEventListener(name, fn),
        request: async ({ method }: { method: string }) => {
          fixture.calls.push(label + '/' + method);
          if (method === 'eth_chainId') return fixture.chain;
          if (method !== 'eth_accounts') throw Error('Unexpected wallet method');
          const accounts = [...fixture.accounts];
          if (delayed) {
            delayed = false;
            await new Promise<void>(resolve => { finish = resolve; });
          }
          return accounts;
        },
      };
    }
    function announce(name: string, rdns: string, instance: ReturnType<typeof makeProvider>) {
      window.dispatchEvent(new CustomEvent('eip6963:announceProvider', { detail: {
        provider: instance, info: { name, rdns, uuid: crypto.randomUUID(), icon: 'data:image/png;base64,' },
      } }));
    }
    const provider = makeProvider('fixed'), other = makeProvider('other');
    const unrelated = makeProvider('unrelated');
    if (legacy) Object.defineProperty(window, 'ethereum', { configurable: true, value: provider });
    else window.addEventListener('eip6963:requestProvider', () => {
      announce('测试钱包', 'other.wallet', unrelated);
      announce('测试钱包', 'test.wallet', provider);
    });
    window.reviewWallet = fixture;
  }, { account, to, legacy });
}

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
