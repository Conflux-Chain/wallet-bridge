import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdtempSync, readdirSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { withMCP } from '../web/tests/e2e/mcp-stdio.ts';

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
  await withMCP({ binary, env, clientInfo: { name: 'smoke', version: '1' } }, async (init, request) => {
    assert.equal(init.serverInfo.name, 'wallet-bridge');
    assert.equal(init.serverInfo.version, info.version);
    assert.ok(init.capabilities.tools);
    const { tools } = await request('tools/list', {});
    assert.deepEqual(tools.map(tool => tool.name).sort(), ['cancel_connection', 'close_session', 'create_connection', 'create_transaction', 'get_connection', 'get_session']);
    const create = tools.find(tool => tool.name === 'create_connection');
    assert.deepEqual([...create.inputSchema.required].sort(), ['chainId', 'requestId']);
    assert.equal(create.inputSchema.additionalProperties, false);
    const txSchema = tools.find(tool => tool.name === 'create_transaction').inputSchema;
    assert.deepEqual([...txSchema.required].sort(), ['requestId', 'sessionId', 'transaction']);
    assert.deepEqual([...txSchema.properties.transaction.required].sort(), ['chainId', 'from', 'to']);
    assert.equal(txSchema.properties.transaction.additionalProperties, false);
  });
  assert.deepEqual(readdirSync(profile), [], 'Discovery created local service data');
} finally {
  rmSync(profile, { recursive: true, force: true });
}
console.log('MCP discovery passed');
console.log(`Smoke passed: ${info.platform}, embedded UI, empty PATH`);
