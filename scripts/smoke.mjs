import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { copyFileSync, mkdtempSync, readdirSync, rmSync } from 'node:fs';
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

function run(args, status = 0, program = binary) {
  const result = spawnSync(program, args, { cwd: root, env: isolatedEnv, encoding: 'utf8' });
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
const missingID = run(['transaction', 'get'], 2);
assert.match(missingID.stderr, /<request-id> is required.*wallet-bridge transaction send/);
const invalidID = JSON.parse(run(['session', 'get', 'aaa', '--json'], 2).stderr);
assert.equal(invalidID.code, 'bad_request');
assert.match(invalidID.message, /<session-id> has an invalid format.*32 lowercase hexadecimal.*approving wallet-bridge connect/);
assert.match(run(['connection', 'cancel', 'first', 'second'], 2).stderr, /Too many IDs.*wallet-bridge connect/);
assert.match(run(['mcp', '--help']).stdout, /wallet-bridge mcp/);
run(['mcp', 'unexpected-argument'], 2);
const argumentCases = [
  [['install', '--data-dir', 'relative'], /--data-dir.*absolute path/i],
  [['connect', '--chain-id', '71', '--client-name= '], /--client-name.*display name/i],
  [['transaction', 'send', '--session-id', 'a'.repeat(32), '--client-name='], /--client-name.*display name/i],
  [['service', 'status', '--unknown=private-value'], /Unknown option.*--unknown.*service status --help/],
  [['version', '--json=private-value'], /--json requires true or false/],
  [['upgrade', 'extra'], /upgrade does not accept positional arguments/],
  [['session', 'unknown'], /Unknown command.*session --help/],
  [['browser', 'list', '--limit', 'private-value'], /--limit.*1 to 50/],
  [['browser', 'list', '--limit', '51'], /--limit.*1 to 50/],
  [['browser', 'list', '--after', 'private-value'], /--after.*previous browser list page/],
  [['browser', 'revoke'], /requires a grant ID.*--all/],
  [['browser', 'revoke', 'a'.repeat(32), '--all'], /either one grant ID or --all/],
  [['browser', 'revoke', 'private-value'], /Invalid grant ID.*browser list/],
  [['update', 'download'], /requires a release tag.*v1.2.3/],
  [['update', 'download', 'private-value'], /Invalid release tag.*v1.2.3/],
  [['update', 'download', 'v1.2.3', 'extra'], /exactly one release tag/],
];
for (const [args, hint] of argumentCases) {
  const message = run(args, 2).stderr;
  assert.match(message, hint);
  assert.doesNotMatch(message, /private-value|Usage:/);
  const failure = JSON.parse(run([...args, '--json'], 2).stderr);
  assert.equal(failure.code, 'bad_request');
  assert.match(failure.message, hint);
  assert.doesNotMatch(failure.message, /private-value|Usage:/);
}

// Exercise the packaged STDIO endpoint without using the user's Bridge data.
const profile = mkdtempSync(join(tmpdir(), 'wallet-bridge-mcp-'));
try {
  const dir = join(profile, 'Bridge data');
  const download = join(profile, goos === 'windows' ? 'download.exe' : 'download');
  copyFileSync(binary, download);
  const installed = run(['install', '--data-dir', dir], 0, download).stdout.trim();
  assert.equal(installed, join(dir, 'bin', goos === 'windows' ? 'wallet-bridge.exe' : 'wallet-bridge'));
  rmSync(download);
  assert.deepEqual(JSON.parse(run(['version', '--json'], 0, installed).stdout), info);
  assert.deepEqual(JSON.parse(run(['install', '--data-dir', dir, '--json'], 0, installed).stdout), { path: installed });
  const env = { ...isolatedEnv, HOME: profile, LOCALAPPDATA: profile };
  await withMCP({ binary: installed, env, clientInfo: { name: 'smoke', version: '1' } }, async (init, request) => {
    assert.equal(init.serverInfo.name, 'wallet-bridge');
    assert.equal(init.serverInfo.version, info.version);
    assert.ok(init.capabilities.tools);
    const { tools } = await request('tools/list', {});
    assert.deepEqual(tools.map(tool => tool.name).sort(), [
      'cancel_connection', 'cancel_transaction', 'close_session', 'create_connection',
      'create_transaction', 'get_connection', 'get_session', 'get_transaction',
    ]);
    const create = tools.find(tool => tool.name === 'create_connection');
    assert.deepEqual([...create.inputSchema.required].sort(), ['chainId', 'requestId']);
    assert.equal(create.inputSchema.additionalProperties, false);
    const txSchema = tools.find(tool => tool.name === 'create_transaction').inputSchema;
    assert.deepEqual([...txSchema.required].sort(), ['requestId', 'sessionId', 'transaction']);
    assert.deepEqual([...txSchema.properties.transaction.required].sort(), ['chainId', 'from', 'to']);
    assert.equal(txSchema.properties.transaction.additionalProperties, false);
  });
  assert.deepEqual(readdirSync(profile), ['Bridge data'], 'Discovery created local service data');
} finally {
  rmSync(profile, { recursive: true, force: true });
}
console.log('Installed CLI and MCP discovery passed');
console.log(`Smoke passed: ${info.platform}, embedded UI, empty PATH`);
