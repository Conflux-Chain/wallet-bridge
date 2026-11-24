import { randomBytes } from 'node:crypto';
import { expect, type Page } from '@playwright/test';
import { authorize, connectionClient, origin, waitingCLI } from './bridge';

export const account = '0x' + 'ab'.repeat(20);
export const to = '0x' + 'cd'.repeat(20);
export const newID = () => randomBytes(16).toString('hex');

type Wallet = { kind: string; name: string; rdns?: string };
export async function createSession(page: Page, chainId: string,
  wallet: Wallet = { kind: 'eip6963', name: '测试钱包', rdns: 'test.wallet' }) {
  const connection = newID();
  await connectionClient('connections', { id: connection, clientName: 'Review fixture', chainId });
  await authorize(page);
  await expect(page.getByRole('status')).toHaveText('浏览器已授权');
  // Establish an isolated synthetic binding through the production browser API.
  const path = origin + '/api/v1/browser/connections/' + connection + '/execution/';
  const id = newID();
  const callId = newID();
  const post = async (action: string, extra: object = {}) => {
    const res = await page.request.post(path + action, { headers: { Origin: origin }, data: { id, ...extra } });
    expect(res.status()).toBe(200);
    return res.json();
  };
  await post('claim');
  await post('begin', { callId });
  await post('return', { callId });
  const { sessionId } = await post('complete', { wallet, account, chainId });
  return sessionId;
}

export async function waitingSend(page: Page) {
  const sessionId = await createSession(page, '0x47');
  const result = await waitingCLI(['transaction', 'send', '--session-id', sessionId, '--chain-id', '71',
    '--from', account, '--to', to, '--value', '13', '--data', '0x0001', '--gas', '75000', '--no-open', '--json']);
  expect(result.receipt).toMatchObject({state: 'pending', url: origin + '/?transaction=' + result.receipt.id});
  return result;
}

export async function createReview(page: Page, chainId: string, transaction: Record<string, string>, intent?: string, wallet?: Wallet) {
  const sessionId = await createSession(page, chainId, wallet);
  const request = newID();
  await connectionClient('transactions', { id: request, sessionId, clientName: '示例 CLI', intent,
    transaction: { from: account, to, chainId, ...transaction } });
  return request;
}
