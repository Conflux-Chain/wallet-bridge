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

async function sampleSession() {
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
  return { sessionId, account, chainId };
}

async function selectedSession(signal) {
  const request = JSON.parse(cli('connect', '--no-open', '--no-wait', '--json', '--chain-id', '71', '--client-name', '钱包核对示例'));
  const link = new URL(cli('ui', '--print-url'));
  link.searchParams.set('connection', request.id);
  console.log('在装有钱包的 Chrome 测试配置中打开连接链接，选择钱包并同意连接。');
  console.log(link.href);
  for (;;) {
    await terminal.question('同意连接后回到此处按 Enter 查看交易。\n', { signal });
    const connection = JSON.parse(cli('connection', 'get', request.id, '--json'));
    if (connection.state === 'pending') continue;
    if (connection.state !== 'succeeded') throw new Error('连接已结束：' + connection.state);
    const session = JSON.parse(cli('session', 'get', connection.sessionId, '--json'));
    return { sessionId: session.id, account: session.account, chainId: session.chainId };
  }
}

async function previewURL(withWallet, signal) {
  const { sessionId, account, chainId } = withWallet ? await selectedSession(signal) : await sampleSession();
  const fields = withWallet
    ? ['--to', account, '--intent', '在 Conflux eSpace 测试网上向自己的账户发送零 CFX']
    : ['--to', '0x' + 'cd'.repeat(20), '--intent', '在测试网上调用合约，并附带原生币。',
      '--value', '1250000000000000001', '--data', '0x00' + 'f0'.repeat(128), '--gas', '75000',
      '--max-fee-per-gas', '40', '--max-priority-fee-per-gas', '0'];
  const request = JSON.parse(cli('transaction', 'send', '--session-id', sessionId, '--from', account,
    '--chain-id', chainId, '--client-name', '示例 CLI', ...fields, '--no-open', '--no-wait', '--json'));
  console.log('Session ID:', sessionId);
  const link = new URL(withWallet ? request.url : cli('ui', '--print-url'));
  link.searchParams.set('transaction', request.id);
  return link;
}

const terminal = createInterface({ input: process.stdin, output: process.stdout });
let started = false;
const stop = new AbortController();
const interrupt = () => stop.abort();
process.once('SIGINT', interrupt);
terminal.once('SIGINT', interrupt);
try {
  if (JSON.parse(cli('service', 'ensure', '--json')).state !== 'running') throw new Error('测试服务未就绪');
  started = true;
  const withWallet = process.argv.slice(2).includes('--wallet');
  const url = await previewURL(withWallet, stop.signal);
  console.log('交易请求 ID:', url.searchParams.get('transaction'));
  console.log('临时数据目录:', directory);
  console.log(withWallet ? '在刚完成连接的浏览器配置中打开交易链接。' : '在 Chrome 无痕窗口打开以下一次性链接。刷新或重开时使用地址栏中保留的普通链接。');
  if (withWallet) console.log('在钱包中最终确认会广播测试网交易并消耗测试币支付 gas。仅体验弹窗时，请在钱包内拒绝。');
  console.log(url.href);
  await terminal.question('查看结束后，回到终端按 Enter 停止服务。\n', { signal: stop.signal });
} catch (error) {
  if (!stop.signal.aborted) throw error;
} finally {
  process.off('SIGINT', interrupt);
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
