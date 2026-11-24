import { execFileSync } from 'node:child_process';
import { randomBytes } from 'node:crypto';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { dirname, isAbsolute, join, relative } from 'node:path';
import { createInterface } from 'node:readline/promises';
import { fileURLToPath } from 'node:url';

const root = dirname(dirname(fileURLToPath(import.meta.url)));
const os = process.platform === 'win32' ? 'windows' : process.platform;
const arch = process.arch === 'x64' ? 'amd64' : process.arch;
const binary = join(root, 'dist', `wallet-bridge-${os}-${arch}${os === 'windows' ? '.exe' : ''}`);
const temporary = await mkdtemp(join(tmpdir(), 'wallet-bridge-review-'));
const directory = join(temporary, 'data');
const cli = (...args) => execFileSync(binary, [...args, '--data-dir', directory], { encoding: 'utf8', windowsHide: true }).trim();
const origin = 'http://127.0.0.1:18765';
const newID = () => randomBytes(16).toString('hex');

async function post(path, body, headers) {
  const response = await fetch(origin + '/api/v1/' + path, {
    method: 'POST', redirect: 'error', headers: { 'Content-Type': 'application/json', ...headers }, body: JSON.stringify(body),
  });
  if (!response.ok) throw new Error(`测试接口 ${path} 返回 HTTP ${response.status}`);
  return response;
}

async function previewURL() {
  const token = (await readFile(join(directory, 'client.key'), 'utf8')).trim();
  const client = { Authorization: 'Bearer ' + token };
  const ticket = new URL(cli('ui', '--print-url')).hash.slice('#authorize='.length);
  const auth = await post('browser/authorize', { ticket }, { Origin: origin });
  const browser = { Origin: origin, Cookie: auth.headers.getSetCookie()[0].split(';')[0] };
  const connection = newID(), id = newID(), callId = newID();
  const chainId = '0x47', account = '0x' + 'ab'.repeat(20);
  await post('connections', { id: connection, clientName: '审阅页演示', chainId }, client);
  const path = 'browser/connections/' + connection + '/execution/';
  await post(path + 'claim', { id }, browser);
  await post(path + 'begin', { id, callId }, browser);
  await post(path + 'return', { id, callId }, browser);
  const consent = await post(path + 'complete', { id, account, chainId, wallet: { kind: 'legacy', name: '测试钱包' } }, browser);
  const { sessionId } = await consent.json();
  const request = newID();
  await post('transactions', { id: request, sessionId, clientName: '示例 CLI', intent: '在测试网上调用合约，并附带原生币。',
    transaction: { from: account, to: '0x' + 'cd'.repeat(20), chainId, value: '1250000000000000001', data: '0x00' + 'f0'.repeat(128),
      gas: '75000', maxFeePerGas: '40', maxPriorityFeePerGas: '0' } }, client);
  const link = new URL(cli('ui', '--print-url'));
  link.searchParams.set('transaction', request);
  return link;
}

const terminal = createInterface({ input: process.stdin, output: process.stdout });
let started = false;
try {
  if (JSON.parse(cli('service', 'ensure', '--json')).state !== 'running') throw new Error('测试服务未就绪');
  started = true;
  const url = await previewURL();
  console.log('在 Chrome 无痕窗口打开以下一次性链接。刷新或重开时使用地址栏中保留的普通链接。');
  console.log(url.href);
  const stop = new AbortController();
  const interrupt = () => stop.abort();
  process.once('SIGINT', interrupt);
  terminal.once('SIGINT', interrupt);
  try {
    await terminal.question('查看结束后，回到终端按 Enter 停止服务。\n', { signal: stop.signal });
  } catch (error) {
    if (!stop.signal.aborted) throw error;
  } finally {
    process.off('SIGINT', interrupt);
  }
} finally {
  terminal.close();
  if (started && JSON.parse(cli('service', 'stop', '--json')).state === 'stopped') {
    const child = relative(tmpdir(), temporary);
    if (isAbsolute(child) || !child.startsWith('wallet-bridge-review-')) throw new Error('测试目录路径异常');
    await rm(temporary, { recursive: true, force: true });
    console.log('测试服务已停止，临时数据已清理。');
  } else {
    console.error('请检查测试服务和端口 18765。测试目录保留在', temporary);
    process.exitCode = 1;
  }
}
