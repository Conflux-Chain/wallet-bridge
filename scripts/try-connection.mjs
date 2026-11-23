// Manual two-wallet acceptance. Only the isolated test service uses this directory.
import { spawn, execFileSync } from 'node:child_process';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { dirname, join, relative, isAbsolute } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = dirname(dirname(fileURLToPath(import.meta.url)));
const os = process.platform === 'win32' ? 'windows' : process.platform;
const arch = process.arch === 'x64' ? 'amd64' : process.arch;
const binary = join(root, 'dist', `wallet-bridge-${os}-${arch}${os === 'windows' ? '.exe' : ''}`);
const temporary = await mkdtemp(join(tmpdir(), 'wallet-bridge-connect-'));
const data = join(temporary, 'data');
const cli = (...args) => execFileSync(binary, [...args, '--data-dir', data], { encoding: 'utf8', windowsHide: true });
let started = false;
let stopped = false;
function stop() {
  if (started) {
    const result = JSON.parse(cli('service', 'stop', '--json'));
    if (result.state !== 'stopped') throw new Error('测试服务停止状态未确认');
    started = false;
    stopped = true;
  }
}
function interrupt() {
  process.exitCode = 130;
  stop();
}
try {
  const result = JSON.parse(cli('service', 'ensure', '--json'));
  if (result.state !== 'running') throw new Error('测试服务未就绪');
  started = true;
  process.on('SIGINT', interrupt);
  console.log('在装有钱包的 Chrome 测试配置中打开以下一次性授权链接：');
  console.log(cli('ui', '--print-url').trim());
  console.log('在同一配置中依次打开终端给出的两个连接链接，分别选择 MetaMask 和 Fluent（EVM）。');
  for (const wallet of ['MetaMask', 'Fluent']) {
    console.log(`请使用 ${wallet} 完成本次连接。`);
    const command = spawn(binary, ['connect', '--no-open', '--chain-id', '71', '--chain-name', 'Conflux eSpace Testnet',
      '--client-name', 'Wallet Bridge 验收', '--intent', '核对选定的钱包、账户与网络', '--data-dir', data],
      { cwd: root, stdio: 'inherit', windowsHide: true });
    const code = await new Promise((resolve, reject) => { command.once('error', reject); command.once('exit', resolve); });
    if (code !== 0 || !started) {
      process.exitCode ||= code || 1;
      break;
    }
  }
} finally {
  process.off('SIGINT', interrupt);
  stop();
  const child = relative(tmpdir(), temporary);
  if (stopped && !isAbsolute(child) && child.startsWith('wallet-bridge-connect-')) {
    await rm(temporary, { recursive: true, force: true });
  } else {
    console.error('请检查端口 18765 是否空闲。测试目录保留在', temporary);
  }
}
