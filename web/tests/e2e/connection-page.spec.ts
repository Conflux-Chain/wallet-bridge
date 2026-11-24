import { expect, test, type Page } from '@playwright/test';
import { authorize, cli, connectionClient as client, waitingCLI } from './bridge';

const first = '0x' + '1'.repeat(40);
const second = '0x' + '2'.repeat(40);

const connectArgs = ['connect', '--chain-id', '71', '--client-name', 'Test client', '--intent', 'Review the test account', '--no-open', '--json'];

async function waitingConnect() {
  const { receipt, done } = await waitingCLI(connectArgs);
  expect(receipt).toMatchObject({state: 'pending', url: expect.stringContaining('?connection=' + receipt.id)});
  return { receipt, done };
}

async function review(page: Page, receipt: { id: string; url: string }, supportsSwitch = true) {
  await page.addInitScript(({ first, second, supportsSwitch }) => {
    let denied = false;
    let accounts: string[] = [];
    let network = '0x1';
    const events = new EventTarget();
    window.addEventListener('test-network-change', event => { network = (event as CustomEvent<string>).detail; });
    window.addEventListener('test-accounts-change', event => { accounts = (event as CustomEvent<string[]>).detail; });
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
          if (!supportsSwitch) throw { code: 4902 };
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
  }, { first, second, supportsSwitch });
  const created = await client('connections/' + receipt.id);
  await authorize(page);
  await expect(page.getByRole('status')).toHaveText('浏览器已授权');
  await page.goto(receipt.url);
  await expect(page.getByText('Test client', {exact:true})).toBeVisible();
  return created;
}

test('wallet refusal can be retried before selecting and confirming the second account', async ({page}) => {
  const {receipt, done} = await waitingConnect();
  const original = await review(page, receipt);
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
  await page.evaluate(() => window.dispatchEvent(new CustomEvent('test-network-change', {detail: '0x1'})));
  await consent.click();
  await expect(consent).toBeDisabled();
  expect(await client('connections/' + original.id)).toEqual(original);
  await page.getByRole('button', { name: '切换到 0x47' }).click();
  await account.selectOption(second);
  await consent.click();
  await expect(page.getByRole('heading', {name:'已同意连接'})).toBeVisible();
  const completed = await done;
  expect(completed.code).toBe(0);
  expect(JSON.parse(completed.stdout)).toMatchObject({id: original.id, state:'succeeded', session: {connectionId: original.id, account: second, chainId:'0x47', wallet: { name:'Simulated Wallet' }}});
  const result = JSON.parse(await cli(['connection', 'get', original.id, '--json']));
  expect(result).toMatchObject({ id: original.id, state: 'succeeded' });
  const active = JSON.parse(await cli(['session', 'get', result.sessionId, '--json']));
  expect(active).toMatchObject({ state: 'active', connectionId: original.id, account: second, chainId:'0x47', wallet: { name:'Simulated Wallet' } });
  const closed = JSON.parse(await cli(['session', 'close', result.sessionId, '--json']));
  expect(closed).toMatchObject({ ...active, state: 'closed', closedAt: expect.any(String) });
  expect(JSON.parse(await cli(['session', 'get', result.sessionId, '--json']))).toEqual(closed);
  expect(JSON.parse(await cli(['connection', 'get', original.id, '--json']))).toEqual(result);
});

test('target network uses the client name, a known network name, or the chain ID alone', async ({page}) => {
  await authorize(page);
  await expect(page.getByRole('status')).toHaveText('浏览器已授权');
  const cases = [
    {chain: '1030', expected: 'Conflux eSpace · 0x406'},
    {chain: '71', expected: 'Conflux eSpace Testnet · 0x47'},
    {chain: '1', expected: 'Ethereum Mainnet · 0x1'},
    {chain: '71', name: 'Client test network', expected: 'Client test network · 0x47'},
    {chain: '0x10000000000000047', expected: '0x10000000000000047'},
  ];
  for (const {chain, name, expected} of cases) {
    await test.step(expected, async () => {
      const args = ['connect', '--chain-id', chain, '--no-open', '--no-wait', '--json'];
      if (name) args.push('--chain-name', name);
      const {url} = JSON.parse(await cli(args));
      await page.goto(url);
      await expect(page.getByRole('region', {name: '连接请求'}).getByRole('definition')
        .filter({hasText: expected})).toHaveText(expected);
    });
  }
});

test('unsupported switch and a silently removed account keep the original request pending', async ({page}) => {
  const receipt = JSON.parse(await cli([...connectArgs, '--no-wait']));
  const original = await review(page, receipt, false);
  await page.evaluate(accounts => window.dispatchEvent(new CustomEvent('test-accounts-change', {detail: accounts})), [first, second]);
  await page.getByRole('radio', {name: /Simulated Wallet/}).check();
  const read = page.getByRole('button', {name: '读取账户和网络'});
  const account = page.getByRole('combobox', {name: '本次连接账户'});
  const consent = page.getByRole('button', {name: '同意连接'});
  await read.click();
  await account.selectOption(second);
  await page.getByRole('button', {name: '切换到 0x47'}).click();
  await expect(page.getByRole('status')).toContainText('钱包无法自动切换');
  await expect(consent).toBeDisabled();
  expect(await client('connections/' + original.id)).toEqual(original);

  await page.evaluate(() => window.dispatchEvent(new CustomEvent('test-network-change', {detail: '0x47'})));
  await read.click();
  await expect(consent).toBeEnabled();
  await page.evaluate(remaining => window.dispatchEvent(new CustomEvent('test-accounts-change', {detail: [remaining]})), first);
  await consent.click();
  await expect(page.getByRole('status')).toContainText('账户或网络已改变');
  await expect(consent).toBeDisabled();
  await expect(account).toHaveValue('');
  expect(await client('connections/' + original.id)).toEqual(original);

  await account.selectOption(first);
  await consent.click();
  await expect(page.getByRole('heading', {name: '已同意连接'})).toBeVisible();
  const result = await client('connections/' + original.id);
  expect(await client('sessions/' + result.sessionId)).toMatchObject({
    connectionId: original.id, account: first, chainId: '0x47', wallet: {name: 'Simulated Wallet'},
  });
});

test('CLI recovers the original request after a service stop and explicitly cancels it', async () => {
  const {id} = JSON.parse(await cli([...connectArgs, '--no-wait']));
  const original = await client('connections/' + id);
  expect(JSON.parse(await cli(['service', 'stop', '--json'])).state).toBe('stopped');
  expect(JSON.parse(await cli(['connection', 'get', id, '--json']))).toEqual(original);
  const canceled = JSON.parse(await cli(['connection', 'cancel', id, '--json']));
  expect(canceled).toMatchObject({ ...original, state: 'canceled', finishedAt: expect.any(String) });
  expect(JSON.parse(await cli(['connection', 'cancel', id, '--json']))).toEqual(canceled);
});

test('explicit rejection ends the pending client request', async ({page}) => {
  const {receipt, done} = await waitingConnect();
  const original = await review(page, receipt);
  await page.getByRole('button', {name:'拒绝本次连接'}).click();
  await expect(page.getByRole('heading', {name:'已拒绝本次连接'})).toBeVisible();
  expect(await client('connections/' + original.id)).toMatchObject({state:'rejected'});
  expect(await done).toEqual({code: 1, stdout: ''});
});
