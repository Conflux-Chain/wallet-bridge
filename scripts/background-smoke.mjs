import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { mkdtemp, readFile, rm, stat, writeFile } from 'node:fs/promises';
import { createServer, request } from 'node:http';
import { tmpdir } from 'node:os';
import { dirname, isAbsolute, join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = dirname(dirname(fileURLToPath(import.meta.url)));
const goos = process.platform === 'win32' ? 'windows' : process.platform;
const goarch = { x64: 'amd64', arm64: 'arm64' }[process.arch];
assert.ok(goarch, `Unsupported host architecture: ${process.arch}`);
const binary = join(root, 'dist', `wallet-bridge-${goos}-${goarch}${goos === 'windows' ? '.exe' : ''}`);
const env = Object.fromEntries(Object.entries(process.env).filter(([key]) => key.toLowerCase() !== 'path'));
Object.assign(env, { PATH: '', HTTP_PROXY: 'http://127.0.0.1:1', ALL_PROXY: 'http://127.0.0.1:1', NO_PROXY: '' });
const temporary = await mkdtemp(join(tmpdir(), 'wallet-bridge-background-'));
const data = join(temporary, 'data');
let mayHaveService = false;
let foreign;

async function runCLI(args, expected = 0, json = true) {
  const child = spawn(binary, args, {
    cwd: root, env, windowsHide: true, stdio: ['ignore', 'pipe', 'pipe'],
  });
  let stdout = '', stderr = '';
  child.stdout.setEncoding('utf8').on('data', (chunk) => { stdout += chunk; });
  child.stderr.setEncoding('utf8').on('data', (chunk) => { stderr += chunk; });
  // Killing a stuck CLI never targets its independent service process.
  const timer = setTimeout(() => child.kill(), 20_000);
  try {
    const result = await new Promise((resolve, reject) => {
      child.once('error', reject);
      child.once('close', (code, signal) => resolve({ code, signal }));
    });
    assert.equal(result.signal, null, 'CLI exceeded its deadline');
    assert.equal(result.code, expected, stderr);
    assert.equal(expected === 0 ? stderr : stdout, '', 'CLI mixed success and error streams');
    const output = expected === 0 ? stdout : stderr;
    return json ? JSON.parse(output) : output;
  } finally { clearTimeout(timer); }
}

function run(action, directory = data, expected = 0) {
  return runCLI(['service', action, '--data-dir', directory, '--json'], expected);
}

function browserCommand(action, args = [], expected = 0) {
  return runCLI(['browser', action, ...args, '--data-dir', data, '--json'], expected);
}

async function authorizationLink() {
  const output = await runCLI(['ui', '--print-url', '--data-dir', data], 0, false);
  const url = new URL(output.trim());
  assert.ok(url.origin === 'http://127.0.0.1:18765' && url.pathname === '/' && url.search === '' && /^#authorize=[0-9a-f]{64}$/.test(url.hash), 'Invalid CLI authorization link');
  return url.hash.slice('#authorize='.length);
}

function browserHTTP(path, { ticket, cookie } = {}) {
  return new Promise((resolve, reject) => {
    const req = request({
      hostname: '127.0.0.1', port: 18765, path, method: ticket === undefined ? 'GET' : 'POST', agent: false,
      headers: { Origin: 'http://127.0.0.1:18765', ...(cookie ? { Cookie: cookie } : {}), ...(ticket === undefined ? {} : { 'Content-Type': 'application/json' }) },
    }, (res) => {
      let body = '';
      res.setEncoding('utf8').on('data', (chunk) => { body += chunk; });
      res.on('error', reject);
      res.on('end', () => resolve({ status: res.statusCode, headers: res.headers, body }));
    });
    req.setTimeout(3000, () => req.destroy(new Error('Browser HTTP test timed out')));
    req.on('error', reject);
    req.end(ticket === undefined ? undefined : JSON.stringify({ ticket }));
  });
}

try {
  assert.equal((await run('status')).state, 'stopped');
  assert.equal((await run('stop')).state, 'stopped');
  assert.equal((await browserCommand('list', [], 1)).code, 'service_not_running');
  assert.equal((await browserCommand('revoke', ['--all'], 1)).code, 'service_not_running');
  await assert.rejects(stat(data), { code: 'ENOENT' });

  mayHaveService = true;
  const results = await Promise.allSettled(Array.from({ length: 6 }, () => run('ensure')));
  const rejected = results.find((result) => result.status === 'rejected');
  if (rejected) throw rejected.reason;
  const statuses = results.map((result) => result.value);
  assert.ok(statuses.every((status) => status.state === 'running' && status.startedAt === statuses[0].startedAt), 'Concurrent callers did not reuse one service');
  assert.equal((await run('status')).startedAt, statuses[0].startedAt, 'Service did not survive CLI exit');
  assert.equal((await run('ensure')).startedAt, statuses[0].startedAt, 'Ensure replaced a running service');
  const credentials = await Promise.all(['client', 'control'].map((name) => readFile(join(data, `${name}.key`), 'utf8')));

  const other = join(temporary, 'other-data');
  assert.equal((await run('ensure', other, 1)).code, 'identity_unverified');
  await assert.rejects(stat(other), { code: 'ENOENT' });

  assert.equal((await run('stop')).state, 'stopped');
  mayHaveService = false;
  assert.equal((await run('status')).state, 'stopped');
  assert.equal((await run('stop')).state, 'stopped');
  mayHaveService = true;
  const firstTicket = await authorizationLink();
  const restarted = await run('ensure');
  assert.equal(restarted.state, 'running');
  assert.notEqual(restarted.startedAt, statuses[0].startedAt);
  for (const [index, name] of ['client', 'control'].entries()) {
    assert.ok(await readFile(join(data, `${name}.key`), 'utf8') === credentials[index], 'Restart replaced a credential');
  }

  const browsers = [];
  for (const ticket of [firstTicket, await authorizationLink()]) {
    const response = await browserHTTP('/api/v1/browser/authorize', { ticket });
    assert.equal(response.status, 200, 'CLI link could not be exchanged');
    const cookie = response.headers['set-cookie']?.[0]?.split(';')[0];
    assert.ok(cookie, 'Browser cookie was not set');
    browsers.push({ cookie, grant: JSON.parse(response.body) });
  }
  const page = await browserCommand('list', ['--limit', '1']);
  assert.equal(page.grants.length, 1);
  assert.ok(page.nextAfter, 'CLI pagination cursor missing');
  const next = await browserCommand('list', ['--limit', '1', '--after', page.nextAfter]);
  assert.equal(next.grants.length, 1);
  assert.notEqual(next.grants[0].id, page.grants[0].id);
  assert.equal(next.nextAfter, undefined);
  const table = await runCLI(['browser', 'list', '--data-dir', data], 0, false);
  assert.ok(table.includes('ISSUED AT') && table.includes('active'), 'Readable authorization list is missing');
  assert.ok(!table.includes(firstTicket) && !browsers.some(({ cookie }) => table.includes(cookie.split('=')[1])), 'CLI list disclosed credentials');
  await run('stop');
  await run('ensure');
  const restored = await browserHTTP('/api/v1/status', browsers[0]);
  assert.equal(restored.status, 200);
  assert.equal(JSON.parse(restored.body).browser.expiresAt, browsers[0].grant.expiresAt, 'Restart renewed authorization');
  assert.equal((await browserCommand('revoke', [browsers[0].grant.id])).state, 'revoked');
  assert.equal((await browserHTTP('/api/v1/status', browsers[0])).status, 401);
  assert.equal((await browserHTTP('/api/v1/status', browsers[1])).status, 200);
  const pendingTicket = await authorizationLink();
  assert.equal((await browserCommand('revoke', ['--all'])).state, 'revoked');
  assert.equal((await browserHTTP('/api/v1/status', browsers[1])).status, 401);
  assert.equal((await browserHTTP('/api/v1/browser/authorize', { ticket: pendingTicket })).status, 401);
  assert.ok((await browserCommand('list')).grants.every((grant) => grant.state === 'revoked'), 'CLI revocation history was not retained');
  assert.equal((await browserCommand('revoke', ['f'.repeat(32)], 1)).code, 'browser_grant_not_found');
  assert.equal((await run('stop')).state, 'stopped');
  mayHaveService = false;

  await writeFile(join(data, 'bridge.db'), 'damaged database retained for inspection');
  mayHaveService = true;
  assert.equal((await run('ensure', data, 1)).code, 'storage_error');
  mayHaveService = false;
  assert.equal(await readFile(join(data, 'bridge.db'), 'utf8'), 'damaged database retained for inspection');

  let requests = 0, credentialsSent = false;
  foreign = createServer((req, res) => {
    requests++;
    credentialsSent ||= req.headers.authorization !== undefined;
    const challenge = new URL(req.url, 'http://127.0.0.1:18765').searchParams.get('challenge');
    res.setHeader('Content-Type', 'application/json');
    res.end(JSON.stringify({ name: 'wallet-bridge', version: 'fake', apiVersion: { major: 1, minor: 0 }, capabilities: ['service.status', 'service.stop'], ready: true, challenge, proof: '00'.repeat(32) }));
  });
  await new Promise((resolve, reject) => { foreign.once('error', reject); foreign.listen(18765, '127.0.0.1', resolve); });
  for (const action of ['status', 'ensure', 'stop']) {
    assert.equal((await run(action, data, 1)).code, 'identity_unverified');
  }
  assert.equal((await browserCommand('list', [], 1)).code, 'identity_unverified');
  assert.equal((await browserCommand('revoke', ['--all'], 1)).code, 'identity_unverified');
  await runCLI(['ui', '--data-dir', data, '--print-url'], 1, false);
  assert.equal(requests, 6);
  assert.equal(credentialsSent, false, 'A forged service received a credential');
  console.log(`Background smoke passed: ${goos}/${goarch}, six callers, persistent service, reuse, stop, restart, CLI authorization links, pagination, revoke, corrupt database, forged service, empty PATH`);
} finally {
  if (foreign) await new Promise((resolve) => foreign.close(resolve));
  if (mayHaveService) {
    // Stop only a service authenticated with this test's private credentials.
    assert.equal((await run('stop')).state, 'stopped', `Test service cleanup failed; inspect ${temporary}`);
  }
  const target = relative(tmpdir(), temporary);
  assert.ok(!isAbsolute(target) && !target.startsWith('..') && target.startsWith('wallet-bridge-background-'), 'Unsafe temporary cleanup path');
  await rm(temporary, { recursive: true, force: true });
}
