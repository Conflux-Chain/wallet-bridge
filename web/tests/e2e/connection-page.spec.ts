import { expect, test, type Page } from '@playwright/test';
import { randomBytes } from 'node:crypto';
import { readFile } from 'node:fs/promises';
import { join } from 'node:path';
import { authorize, origin } from './bridge';

const first = '0x' + '1'.repeat(40);
const second = '0x' + '2'.repeat(40);

async function client(path: string, body?: object) {
  const key = await readFile(join(process.env.WALLET_BRIDGE_TEST_DATA!, 'client.key'), 'utf8');
  const response = await fetch(origin + '/api/v1/' + path, {
    method: body ? 'POST' : 'GET', redirect: 'error',
    headers: { Authorization: 'Bearer ' + key.trim(), 'Content-Type': 'application/json' },
    body: body && JSON.stringify(body),
  });
  expect(response.status).toBe(200);
  return response.json();
}

async function review(page: Page) {
  await page.addInitScript(({ first, second }) => {
    let denied = false;
    let accounts: string[] = [];
    let network = '0x1';
    const events = new EventTarget();
    window.addEventListener('test-network-change', () => { network = '0x1'; });
    const provider = {
      on: (name: string, cb: EventListener) => events.addEventListener(name, cb),
      removeListener: (name: string, cb: EventListener) => events.removeEventListener(name, cb),
      request: async ({ method, params }: {method: string; params?: {chainId:string}[]}) => {
        if (method === 'eth_accounts') return accounts;
        if (method === 'eth_chainId') return network;
        if (method === 'eth_requestAccounts') {
          if (!denied) { denied = true; throw { code: 4001 }; }
          accounts = [first, second];
          events.dispatchEvent(new Event('accountsChanged'));
          return accounts;
        }
        if (method === 'wallet_switchEthereumChain') {
          network = params![0].chainId;
          events.dispatchEvent(new Event('chainChanged'));
          return null;
        }
        throw new Error('Unexpected wallet method');
      },
    };
    const info = { name: 'Simulated Wallet', rdns: 'org.example.wallet', uuid: crypto.randomUUID(), icon: 'data:image/png;base64,' };
    window.addEventListener('eip6963:requestProvider', () => {
      window.dispatchEvent(new CustomEvent('eip6963:announceProvider', { detail: { info, provider } }));
    });
  }, { first, second });
  const id = randomBytes(16).toString('hex');
  const created = await client('connections', { id, clientName: 'Test client', intent: 'Review the test account', chainId:'71' });
  await authorize(page);
  await expect(page.getByRole('status')).toHaveText('浏览器已授权');
  await page.goto('/?connection=' + id);
  await expect(page.getByText('Test client', {exact:true})).toBeVisible();
  return created;
}

test('wallet refusal can be retried before selecting and confirming the second account', async ({page}) => {
  const original = await review(page);
  await page.getByRole('radio', {name:/Simulated Wallet/}).check();
  await page.getByRole('button', {name:'读取账户和网络'}).click();
  const allow = page.getByRole('button', {name:'授权访问账户'});
  await allow.click();
  await expect(page.getByRole('status')).toContainText('已在钱包中拒绝');
  expect(await client('connections/' + original.id)).toEqual(original);
  await allow.click();
  const account = page.getByRole('combobox', {name:'本次连接账户'});
  await expect(account).toHaveValue('');
  await account.selectOption(second);
  const consent = page.getByRole('button', {name:'同意连接'});
  await expect(consent).toBeDisabled();
  await page.getByRole('button', {name:'切换到 0x47'}).click();
  await account.selectOption(second);
  // The provider changed network without delivering an event. Final consent must reread it.
  await page.evaluate(() => window.dispatchEvent(new Event('test-network-change')));
  await consent.click();
  await expect(consent).toBeDisabled();
  expect(await client('connections/' + original.id)).toEqual(original);
  await page.getByRole('button', { name: '切换到 0x47' }).click();
  await account.selectOption(second);
  await consent.click();
  await expect(page.getByRole('heading', {name:'已同意连接'})).toBeVisible();
  const result = await client('connections/' + original.id);
  expect(await client('sessions/' + result.sessionId)).toMatchObject({ connectionId: original.id, account: second, chainId:'0x47', wallet: { name:'Simulated Wallet' } });
});

test('explicit rejection ends the pending client request', async ({page}) => {
  const original = await review(page);
  await page.getByRole('button', {name:'拒绝本次连接'}).click();
  await expect(page.getByRole('heading', {name:'已拒绝本次连接'})).toBeVisible();
  expect(await client('connections/' + original.id)).toMatchObject({state:'rejected'});
});
