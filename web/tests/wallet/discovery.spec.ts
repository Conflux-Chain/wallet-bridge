import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';

interface MockWallet {
  key: string;
  name: string;
  uuid?: string;
  rdns?: string;
  icon?: string;
  legacy?: boolean;
  silent?: boolean;
}

declare global {
  interface Window {
    walletTest: {
      add(wallet: MockWallet): void;
      announce(key: string): void;
      setLegacy(key: string): void;
      calls: string[];
    };
  }
}

async function install(page: Page, initial: MockWallet[] = []) {
  await page.addInitScript((initial) => {
    const calls: string[] = [];
    const wallets = new Map<string, { info: object; provider: object; silent?: boolean }>();
    const announce = (key: string) => window.dispatchEvent(new CustomEvent('eip6963:announceProvider', { detail: wallets.get(key) }));
    const setLegacy = (key: string) => Object.defineProperty(window, 'ethereum', { configurable: true, value: wallets.get(key)?.provider });
    const add = (wallet: MockWallet) => {
      const provider = {
        request: async ({ method }: { method: string }) => { calls.push(method); throw new Error('Unexpected wallet request'); },
        on: () => { calls.push('on'); },
        removeListener: () => { calls.push('removeListener'); },
      };
      wallets.set(wallet.key, {
        info: {
          name: wallet.name, uuid: wallet.uuid ?? crypto.randomUUID(), rdns: wallet.rdns ?? `org.example.${wallet.key}`,
          icon: wallet.icon ?? 'data:image/svg+xml,<svg xmlns="http://www.w3.org/2000/svg" width="96" height="96"><rect width="96" height="96" fill="green"/></svg>',
        },
        provider, silent: wallet.silent,
      });
      if (wallet.legacy) setLegacy(wallet.key);
      if (!wallet.silent) announce(wallet.key);
    };
    window.walletTest = { add, announce, setLegacy, calls };
    window.addEventListener('eip6963:requestProvider', () => {
      wallets.forEach((wallet, key) => { if (!wallet.silent) announce(key); });
    });
    initial.forEach(add);
  }, initial);
}

test('empty discovery explains extension access without calling the service', async ({ page }) => {
  const requests: string[] = [];
  page.on('request', (request) => { if (request.url().includes('/api/v1/')) requests.push(request.url()); });
  await install(page);
  await page.goto('/');
  await expect(page.getByRole('heading', { name: '选择浏览器钱包' })).toBeVisible();
  await expect(page.getByText(/尚未发现钱包/)).toBeVisible();
  await page.getByRole('button', { name: '重新检测钱包' }).click();
  await expect(page.getByRole('radio')).toHaveCount(0);
  expect(requests).toEqual([]);
  expect(await page.evaluate(() => window.walletTest.calls)).toEqual([]);
});

test('synchronous announcements deduplicate through StrictMode and require explicit selection', async ({ page }) => {
  await install(page, [
    { key: 'metamask', name: 'MetaMask', legacy: true },
    { key: 'fluent', name: 'Fluent' },
    { key: 'other', name: 'Another Wallet' },
  ]);
  await page.goto('/');
  await expect(page.getByRole('radio')).toHaveCount(3);
  await expect(page.locator('input:checked')).toHaveCount(0);
  const fluent = page.getByRole('radio', { name: /Fluent/ });
  await fluent.check();
  await page.getByRole('button', { name: '重新检测钱包' }).click();
  await expect(page.getByRole('radio')).toHaveCount(3);
  await expect(fluent).toBeChecked();
  await expect(page.getByText('当前选择：Fluent', { exact: true })).toBeVisible();
  expect(await page.evaluate(() => window.walletTest.calls)).toEqual([]);
  await page.reload();
  await expect(page.getByRole('radio')).toHaveCount(3);
  await expect(page.locator('input:checked')).toHaveCount(0);
});

test('late announcements and a replaced legacy entry do not change the selection', async ({ page }) => {
  await install(page, [{ key: 'first', name: 'First Wallet', legacy: true }]);
  await page.goto('/');
  await page.getByRole('radio', { name: /First Wallet/ }).check();
  await page.evaluate(() => {
    window.walletTest.add({ key: 'late', name: 'Late Wallet' });
    window.walletTest.add({ key: 'legacy', name: 'Legacy', silent: true, legacy: true });
  });
  await expect(page.getByRole('radio', { name: /Late Wallet/ })).toBeVisible();
  await page.getByRole('button', { name: '重新检测钱包' }).click();
  await expect(page.getByRole('radio')).toHaveCount(3);
  await expect(page.getByRole('radio', { name: /First Wallet/ })).toBeChecked();
  expect(await page.evaluate(() => window.walletTest.calls)).toEqual([]);
});

test('a legacy provider upgrades in place when its standard announcement arrives', async ({ page }) => {
  await install(page, [{ key: 'legacy', name: 'Fluent', silent: true, legacy: true }]);
  await page.goto('/');
  await page.getByRole('radio', { name: /兼容入口/ }).check();
  await page.evaluate(() => window.walletTest.announce('legacy'));
  await expect(page.getByRole('radio')).toHaveCount(1);
  await expect(page.getByRole('radio', { name: /Fluent/ })).toBeChecked();
  expect(await page.evaluate(() => window.walletTest.calls)).toEqual([]);
});

test('colliding UUIDs and rdns retain separate providers without replacing a selection', async ({ page }) => {
  const uuid = '350670db-19fa-4704-a166-e52e178b59d2';
  await install(page, [{ key: 'one', name: 'Wallet One', uuid, rdns: 'org.example.same' }]);
  await page.goto('/');
  await page.getByRole('radio', { name: /Wallet One/ }).check();
  await page.evaluate((uuid) => window.walletTest.add({ key: 'two', name: 'Wallet Two', uuid, rdns: 'org.example.same' }), uuid);
  await expect(page.getByRole('radio')).toHaveCount(2);
  await expect(page.getByText('此钱包的标识与其他钱包重复，请核对名称与来源。', { exact: true })).toHaveCount(2);
  await expect(page.getByRole('radio', { name: /Wallet One/ })).toBeChecked();
  await page.getByRole('radio', { name: /Wallet Two/ }).check();
  await page.getByRole('button', { name: '重新检测钱包' }).click();
  await expect(page.getByRole('radio', { name: /Wallet Two/ })).toBeChecked();
  expect(await page.evaluate(() => window.walletTest.calls)).toEqual([]);
});

test('malformed events and throwing extension properties do not break valid discovery', async ({ page }) => {
  await install(page);
  await page.goto('/');
  await page.evaluate(() => {
    for (const detail of [null, {}, { info: {}, provider: {} }]) window.dispatchEvent(new CustomEvent('eip6963:announceProvider', { detail }));
    const info = { uuid: crypto.randomUUID(), name: 'Invalid Provider', rdns: 'org.example.invalid', icon: 'data:image/png,' };
    for (const provider of [null, {}, { request: async () => [], on() {} }]) {
      window.dispatchEvent(new CustomEvent('eip6963:announceProvider', { detail: { info, provider } }));
    }
    window.dispatchEvent(new Event('eip6963:announceProvider'));
    window.dispatchEvent(new CustomEvent('eip6963:announceProvider', { detail: Object.defineProperty({}, 'info', { get() { throw new Error('bad info'); } }) }));
    Object.defineProperty(window, 'ethereum', { configurable: true, get() { throw new Error('bad legacy getter'); } });
    window.walletTest.add({ key: 'bad', name: 'Invalid UUID', uuid: 'not-uuid-v4' });
  });
  await page.getByRole('button', { name: '重新检测钱包' }).click();
  await expect(page.getByRole('radio')).toHaveCount(0);
  await page.evaluate(() => window.walletTest.add({ key: 'valid', name: 'Valid Wallet' }));
  await expect(page.getByRole('radio', { name: /Valid Wallet/ })).toBeVisible();
});

test('extension text is escaped and icons cannot load remote URLs or execute SVG scripts', async ({ page }) => {
  const remoteRequests: string[] = [];
  page.on('request', (request) => { if (request.url().startsWith('https://example.com/')) remoteRequests.push(request.url()); });
  const name = '<img src=x onerror="window.walletInjected=true">';
  await install(page, [
    { key: 'remote', name, icon: 'https://example.com/wallet.png' },
    { key: 'svg', name: 'SVG Wallet', icon: 'data:image/svg+xml,' + encodeURIComponent('<svg xmlns="http://www.w3.org/2000/svg" onload="parent.walletInjected=true"><script>parent.walletInjected=true</script></svg>') },
  ]);
  await page.goto('/');
  await expect(page.getByText(name, { exact: true })).toBeVisible();
  await expect(page.locator('.wallet-option img')).toHaveCount(1);
  await expect(page.locator('.wallet-option img')).toHaveJSProperty('complete', true);
  expect(await page.evaluate(() => 'walletInjected' in window)).toBe(false);
  expect(remoteRequests).toEqual([]);
});

test('discovery start and cleanup retain a fixed provider instance and remove listeners', async ({ page }) => {
  await install(page);
  await page.goto('/');
  const result = await page.evaluate(async () => {
    const path = '/src/wallet/discovery.ts';
    const { WalletDiscovery } = await import(path) as typeof import('../../src/wallet/discovery');
    const discovery = new WalletDiscovery(window);
    let notifications = 0;
    const unsubscribe = discovery.subscribe(() => notifications++);
    discovery.start(); discovery.start();
    window.walletTest.add({ key: 'one', name: 'One' });
    const first = discovery.getSnapshot().wallets[0];
    discovery.select(first.id);
    discovery.stop();
    window.walletTest.add({ key: 'two', name: 'Two' });
    const stoppedCount = discovery.getSnapshot().wallets.length;
    discovery.start();
    const resumed = discovery.getSnapshot();
    unsubscribe();
    discovery.stop();
    return { notifications, stoppedCount, count: resumed.wallets.length, same: resumed.wallets[0].provider === first.provider, selected: resumed.selectedId === first.id, calls: window.walletTest.calls };
  });
  expect(result).toEqual({ notifications: 3, stoppedCount: 1, count: 2, same: true, selected: true, calls: [] });
});

test('wallet selection remains usable on a narrow screen', async ({ page }) => {
  await page.setViewportSize({ width: 360, height: 800 });
  await install(page, [{ key: 'long', name: 'A Wallet With A Very Long Display Name', rdns: 'org.example.averylongwalletidentifier' }]);
  await page.goto('/');
  await page.getByRole('radio').check();
  await expect(page.getByRole('radio')).toBeChecked();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});
