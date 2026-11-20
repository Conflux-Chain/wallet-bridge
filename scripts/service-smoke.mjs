import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { createHmac, randomBytes } from 'node:crypto';
import { mkdtemp, readFile, rm, stat } from 'node:fs/promises';
import { Agent, request } from 'node:http';
import { tmpdir } from 'node:os';
import { dirname, isAbsolute, join, relative } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';
import { fileURLToPath } from 'node:url';

const root = dirname(dirname(fileURLToPath(import.meta.url)));
const goos = process.platform === 'win32' ? 'windows' : process.platform;
const goarch = { x64: 'amd64', arm64: 'arm64' }[process.arch];
assert.ok(goarch, `Unsupported host architecture: ${process.arch}`);
const binary = join(root, 'dist', `wallet-bridge-${goos}-${goarch}${goos === 'windows' ? '.exe' : ''}`);
const env = Object.fromEntries(Object.entries(process.env).filter(([key]) => key.toLowerCase() !== 'path'));
env.PATH = '';
const temporary = await mkdtemp(join(tmpdir(), 'wallet-bridge-service-'));
const data = join(temporary, 'data');
const children = new Set();
// A private agent connects directly; environment proxy settings never receive tokens.
const agent = new Agent({ keepAlive: false });

function bounded(promise, message, milliseconds = 10_000) {
  let timer;
  return Promise.race([
    promise,
    new Promise((_, reject) => { timer = setTimeout(() => reject(new Error(message)), milliseconds); }),
  ]).finally(() => clearTimeout(timer));
}

function launch(directory) {
  const child = spawn(binary, ['service', 'run', '--data-dir', directory], {
    cwd: root, env, windowsHide: true, stdio: ['ignore', 'pipe', 'pipe'],
  });
  children.add(child);
  const process = { child, stdout: '', stderr: '', result: undefined };
  child.stdout.setEncoding('utf8').on('data', (chunk) => { process.stdout += chunk; });
  child.stderr.setEncoding('utf8').on('data', (chunk) => { process.stderr += chunk; });
  process.finished = new Promise((resolve) => {
    child.once('error', (error) => { process.result = { error }; });
    child.once('close', (code, signal) => {
      children.delete(child);
      process.result ??= { code, signal };
      resolve(process.result);
    });
  });
  return process;
}

async function start(directory) {
  const process = launch(directory);
  await bounded((async () => {
    while (!process.stdout.includes('wallet-bridge ready at http://127.0.0.1:18765\n')) {
      if (process.result) throw new Error(`Service startup failed: ${process.stderr}`);
      await delay(20);
    }
  })(), 'Service readiness timed out');
  return process;
}

function http(path, { method = 'GET', token, headers = {}, body } = {}) {
  return new Promise((resolve, reject) => {
    const req = request({
      hostname: '127.0.0.1', port: 18765, path, method, agent,
      headers: { ...(token ? { Authorization: `Bearer ${token}` } : {}), ...(body === undefined ? {} : { 'Content-Type': 'application/json' }), ...headers },
    }, (response) => {
      let body = '';
      response.setEncoding('utf8');
      response.on('data', (chunk) => { body += chunk; });
      response.on('error', reject);
      response.on('end', () => resolve({ status: response.statusCode, headers: response.headers, body }));
    });
    req.setTimeout(3000, () => req.destroy(new Error('HTTP request timed out')));
    req.on('error', reject);
    req.end(body === undefined ? undefined : JSON.stringify(body));
  });
}

async function credential(directory, name) {
  const contents = await readFile(join(directory, `${name}.key`), 'utf8');
  assert.ok(/^[0-9a-f]{64}\n$/.test(contents), 'Invalid credential format');
  return contents.trim();
}

async function identity(client) {
  const challenge = randomBytes(32).toString('hex');
  const response = await http(`/api/v1/info?challenge=${challenge}`);
  assert.equal(response.status, 200);
  const info = JSON.parse(response.body);
  assert.equal(info.name, 'wallet-bridge');
  assert.equal(info.challenge, challenge);
  assert.equal(info.ready, true);
  const proof = createHmac('sha256', Buffer.from(client, 'hex'))
    .update('wallet-bridge.info.v1\n')
    .update(JSON.stringify({
      name: info.name, version: info.version, apiVersion: info.apiVersion,
      capabilities: info.capabilities, ready: info.ready, challenge,
    })).digest('hex');
  assert.ok(info.proof === proof, 'Service identity proof failed');
}

async function exited(process, code = 0) {
  const result = await bounded(process.finished, 'Service did not exit');
  if (result.error) throw result.error;
  assert.equal(result.code, code, process.stderr);
  assert.equal(result.signal, null);
  if (code === 0) assert.equal(process.stderr, '');
}

async function stop(process, token) {
  const response = await http('/api/v1/control/stop', { method: 'POST', token });
  assert.equal(response.status, 202);
  assert.equal(JSON.parse(response.body).state, 'stopping');
  await exited(process);
}

try {
  const first = await start(data);
  const token = await credential(data, 'client');
  await assert.rejects(stat(join(data, 'control.key')), { code: 'ENOENT' });
  await identity(token);
  assert.equal((await http('/api/v1/status')).status, 401);
  const status = await http('/api/v1/status', { token });
  assert.equal(status.status, 200);
  assert.equal(JSON.parse(status.body).state, 'running');
  assert.equal((await http('/api/v1/control/stop', { method: 'POST' })).status, 401);
  assert.equal((await http('/api/v1/info', { headers: { Host: 'example.com:18765' } })).status, 403);
  assert.equal((await http('/api/v1/info', { headers: { Origin: 'https://example.com' } })).status, 403);

  const link = await http('/api/v1/control/browser-grants', { method: 'POST', token, body: {} });
  assert.equal(link.status, 201);
  const authorizationURL = new URL(JSON.parse(link.body).url);
  assert.ok(authorizationURL.origin === 'http://127.0.0.1:18765' && /^#authorize=[0-9a-f]{64}$/.test(authorizationURL.hash), 'Invalid one-time authorization URL');
  const ticket = authorizationURL.hash.slice('#authorize='.length);
  assert.equal((await http('/api/v1/browser/authorize', { method: 'POST', body: { ticket } })).status, 403);
  const browserOrigin = { Origin: 'http://127.0.0.1:18765' };
  const authorized = await http('/api/v1/browser/authorize', { method: 'POST', headers: browserOrigin, body: { ticket } });
  assert.equal(authorized.status, 200);
  const cookieHeaders = authorized.headers['set-cookie'];
  assert.ok(cookieHeaders?.length === 1 && cookieHeaders[0].includes('HttpOnly') && cookieHeaders[0].includes('SameSite=Strict') && cookieHeaders[0].includes('Path=/api/v1'), 'Missing protected browser cookie');
  const cookie = cookieHeaders[0].split(';')[0];
  const browser = { ...browserOrigin, Cookie: cookie };
  assert.ok(!authorized.body.includes(cookie.split('=')[1]) && !authorized.body.includes(ticket), 'Exchange JSON disclosed a secret');
  const browserStatus = await http('/api/v1/status', { headers: browser });
  assert.equal(browserStatus.status, 200);
  assert.ok(browserStatus.headers['set-cookie'] === undefined, 'Status renewed the browser cookie');
  const grant = JSON.parse(authorized.body);
  assert.equal(JSON.parse(browserStatus.body).browser.id, grant.id);
  assert.equal((await http('/api/v1/control/stop', { method: 'POST', headers: browser })).status, 401);
  assert.equal((await http('/api/v1/control/browser-grants', { headers: browser })).status, 401);
  assert.equal((await http('/api/v1/browser/authorize', { method: 'POST', headers: browserOrigin, body: { ticket } })).status, 401);
  const pendingLink = await http('/api/v1/control/browser-grants', { method: 'POST', token });
  assert.equal(pendingLink.status, 201);
  const pendingTicket = new URL(JSON.parse(pendingLink.body).url).hash.slice('#authorize='.length);

  const page = await http('/');
  assert.equal(page.status, 200);
  assert.match(page.headers['content-type'], /^text\/html/);
  const script = page.body.match(/<script[^>]+src="([^"]+\.js)"/);
  assert.ok(script, 'Embedded HTML must reference JavaScript');
  const js = await http(script[1]);
  assert.equal(js.status, 200);
  assert.match(js.headers['content-type'], /^(text|application)\/javascript/);
  assert.match(js.body, /Wallet Bridge/);

  const contender = launch(data);
  await exited(contender, 1);
  assert.match(contender.stderr, /^already_running:/);
  const otherData = join(temporary, 'other-data');
  const occupied = launch(otherData);
  await exited(occupied, 1);
  assert.match(occupied.stderr, /^port_in_use:/);
  await assert.rejects(stat(join(otherData, 'client.key')), { code: 'ENOENT' });

  await stop(first, token);
  const database = await readFile(join(data, 'bridge.db'));
  assert.equal(database.subarray(0, 16).toString(), 'SQLite format 3\0', 'Production service did not initialize SQLite');
  assert.ok((await stat(join(data, 'bridge.db-journal'))).isFile(), 'Protected rollback journal is missing');
  const restarted = await start(data);
  assert.ok(await credential(data, 'client') === token, 'Restart replaced the service credential');
  await assert.rejects(stat(join(data, 'control.key')), { code: 'ENOENT' });
  await identity(token);
  const restored = await http('/api/v1/status', { headers: browser });
  assert.equal(restored.status, 200);
  assert.equal(JSON.parse(restored.body).browser.expiresAt, grant.expiresAt, 'Restart extended browser authorization');
  assert.equal((await http('/api/v1/browser/authorize', { method: 'POST', headers: browserOrigin, body: { ticket: pendingTicket } })).status, 401);
  const logout = await http('/api/v1/browser/logout', { method: 'POST', headers: browser, body: {} });
  assert.equal(logout.status, 200);
  assert.ok(logout.headers['set-cookie']?.some((value) => value.includes('Max-Age=0')), 'Logout did not expire its cookie');
  assert.equal((await http('/api/v1/status', { headers: browser })).status, 401);
  const grantList = await http('/api/v1/control/browser-grants', { token });
  assert.equal(grantList.status, 200);
  assert.ok(JSON.parse(grantList.body).grants.some((entry) => entry.id === grant.id && entry.state === 'revoked'), 'Logout did not persist revocation');
  await stop(restarted, token);
  assert.ok((await stat(join(data, 'service.lock'))).isFile(), 'The released lock file must remain');

  if (goos === 'darwin') {
    const signaled = await start(data);
    signaled.child.kill('SIGTERM');
    await exited(signaled);
  }
  console.log(`Service smoke passed: ${goos}/${goarch}, HTTP boundaries, browser authorization, replay rejection, persistent cookie, logout, contention, restart, shutdown, empty PATH`);
} finally {
  agent.destroy();
  for (const child of children) child.kill();
  await bounded(Promise.all([...children].map((child) => new Promise((resolve) => child.once('close', resolve)))), 'Owned process cleanup timed out');
  const target = relative(tmpdir(), temporary);
  assert.ok(!isAbsolute(target) && !target.startsWith('..') && target.startsWith('wallet-bridge-service-'), 'Unsafe temporary cleanup path');
  await rm(temporary, { recursive: true, force: true });
}
