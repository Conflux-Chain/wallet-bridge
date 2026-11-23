import { expect, test, type Page } from '@playwright/test';
import { randomBytes } from 'node:crypto';
import { readFile } from 'node:fs/promises';
import { join } from 'node:path';
import { authorize, origin } from './bridge';

async function readFromPage(page: Page, path: string) {
  return page.evaluate(async (path) => {
    const response = await fetch(path, { credentials: 'same-origin', mode: 'same-origin', cache: 'no-store' });
    return { status: response.status, body: await response.json() };
  }, path);
}

test('native fetch reviews a connection and commits a test binding', async ({ page }) => {
  const id = randomBytes(16).toString('hex');
  const execution = randomBytes(16).toString('hex');
  const key = await readFile(join(process.env.WALLET_BRIDGE_TEST_DATA!, 'client.key'), 'utf8');
  const created = await fetch(origin + '/api/v1/connections', {
    method: 'POST', redirect: 'error',
    headers: { Authorization: 'Bearer ' + key.trim(), 'Content-Type': 'application/json' },
    body: JSON.stringify({ id, clientName: 'Test CLI', chainId: '71' }),
  });
  expect(created.status).toBe(200);
  const path = '/api/v1/browser/connections/' + id;
  await page.goto('/');
  for (const suffix of ['', '/execution']) {
    expect((await readFromPage(page, path + suffix)).status).toBe(401);
  }
  await authorize(page);
  await expect(page.getByRole('status')).toHaveText('浏览器已授权');
  expect(await readFromPage(page, path)).toMatchObject({ status: 200, body: { id, state: 'pending' } });
  expect(await readFromPage(page, path + '/execution')).toMatchObject({ status: 404, body: { code: 'execution_not_found' } });
  const claim = await page.evaluate(async ({ path, execution }) => {
    const response = await fetch(path + '/execution/claim', {
      method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ id: execution }),
    });
    return response.status;
  }, { path, execution });
  expect(claim).toBe(200);
  expect(await readFromPage(page, path + '/execution')).toMatchObject({ status: 200, body: { id: execution, phase: 'ready' } });

  // Exercise the browser transport with a synthetic binding. No provider is installed.
  const account = '0x' + 'a'.repeat(40);
  const result = await page.evaluate(async ({ path, execution, account }) => {
    async function post(action: string, body: object) {
      const response = await fetch(path + '/execution/' + action, {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ id: execution, ...body }),
      });
      if (!response.ok) throw new Error(action + ' returned ' + response.status);
      return response.json();
    }
    const callId = crypto.randomUUID().replaceAll('-', '');
    const permit = await post('begin', { callId });
    const retry = await post('begin', { callId });
    const returned = await post('return', { callId });
    const approved = await post('complete', { wallet: { kind: 'legacy', name: 'Test wallet' }, account, chainId: '0x47' });
    return { invoke: permit.invoke, retry: retry.invoke, phase: returned.phase, approved };
  }, { path, execution, account });
  expect(result).toMatchObject({ invoke: true, retry: false, phase: 'ready', approved: { id, state: 'succeeded' } });
  const session = await fetch(origin + '/api/v1/sessions/' + result.approved.sessionId, {
    redirect: 'error', headers: { Authorization: 'Bearer ' + key.trim() },
  });
  expect(session.status).toBe(200);
  expect(await session.json()).toMatchObject({ account, chainId: '0x47', wallet: { name: 'Test wallet' } });
});
