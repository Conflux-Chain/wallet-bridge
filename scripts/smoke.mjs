import assert from 'node:assert/strict';
import { spawn, spawnSync } from 'node:child_process';
import { once } from 'node:events';
import { mkdtempSync, readdirSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { createInterface } from 'node:readline';
import { fileURLToPath } from 'node:url';

const root = dirname(dirname(fileURLToPath(import.meta.url)));
const goos = process.platform === 'win32' ? 'windows' : process.platform;
const goarch = { x64: 'amd64', arm64: 'arm64' }[process.arch];
assert.ok(goarch, `Unsupported host architecture: ${process.arch}`);
const binary = join(root, 'dist', `wallet-bridge-${goos}-${goarch}${goos === 'windows' ? '.exe' : ''}`);
const isolatedEnv = Object.fromEntries(Object.entries(process.env).filter(([key]) => key.toLowerCase() !== 'path'));
isolatedEnv.PATH = '';

function run(args, status = 0) {
  const result = spawnSync(binary, args, { cwd: root, env: isolatedEnv, encoding: 'utf8' });
  if (result.error) throw result.error;
  assert.equal(result.status, status, result.stderr);
  if (status === 0) assert.equal(result.stderr, '');
  else assert.equal(result.stdout, '');
  return result;
}

const info = JSON.parse(run(['version', '--json']).stdout);
assert.ok(info.version.length > 0);
assert.ok(info.revision.length > 0);
assert.equal(typeof info.modified, 'boolean');
assert.match(info.goVersion, /^go\d+\.\d+/);
assert.equal(info.platform, `${goos}/${goarch}`);
assert.equal(info.uiEmbedded, true, 'Full builds must contain the browser UI');
assert.match(run(['version']).stdout, /^wallet-bridge /);
assert.match(run([]).stdout, /Usage:/);
assert.match(run(['--help']).stdout, /Usage:/);
assert.match(run(['version', '--help']).stdout, /Usage:/);
run(['unknown-command'], 2);
run(['version', '--unknown-flag'], 2);
run(['version', 'unexpected-argument'], 2);
assert.match(run(['mcp', '--help']).stdout, /wallet-bridge mcp/);
run(['mcp', 'unexpected-argument'], 2);

// Exercise the packaged STDIO endpoint without using the user's Bridge data.
const profile = mkdtempSync(join(tmpdir(), 'wallet-bridge-mcp-'));
try {
  const env = { ...isolatedEnv, HOME: profile, LOCALAPPDATA: profile };
  const child = spawn(binary, ['mcp'], { env, stdio: ['pipe', 'pipe', 'inherit'], timeout: 15000, killSignal: 'SIGKILL' });
  const closed = once(child, 'close');
  const lines = createInterface({ input: child.stdout })[Symbol.asyncIterator]();
  const send = message => child.stdin.write(JSON.stringify({ jsonrpc: '2.0', ...message }) + '\n');
  async function resultFor(id) {
    const line = await lines.next();
    assert.equal(line.done, false, 'MCP server closed before replying');
    const response = JSON.parse(line.value);
    assert.equal(response.jsonrpc, '2.0');
    assert.equal(response.id, id);
    assert.ok(response.result, line.value);
    return response.result;
  }
  try {
    const protocolVersion = '2025-11-25';
    send({ id: 1, method: 'initialize', params: {
      protocolVersion, capabilities: {}, clientInfo: { name: 'smoke', version: '1' },
    } });
    const init = await resultFor(1);
    assert.equal(init.protocolVersion, protocolVersion);
    assert.equal(init.serverInfo.name, 'wallet-bridge');
    assert.equal(init.serverInfo.version, info.version);
    assert.ok(init.capabilities.tools);
    send({ method: 'notifications/initialized' });
    send({ id: 2, method: 'tools/list', params: {} });
    assert.deepEqual((await resultFor(2)).tools, []);
  } finally {
    child.stdin.end();
    assert.deepEqual(await closed, [0, null], 'MCP server must exit cleanly on EOF');
  }
  assert.deepEqual(readdirSync(profile), [], 'Discovery created local service data');
} finally {
  rmSync(profile, { recursive: true, force: true });
}
console.log('MCP discovery passed');
console.log(`Smoke passed: ${info.platform}, embedded UI, empty PATH`);
