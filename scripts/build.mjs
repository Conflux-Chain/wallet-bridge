import { spawnSync } from 'node:child_process';
import { mkdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = dirname(dirname(fileURLToPath(import.meta.url)));
const args = process.argv.slice(2);
if (args.length > 1 || (args.length === 1 && args[0] !== '--all')) {
  throw new Error('Usage: npm --prefix web run bundle [-- --all]');
}
if (!process.env.npm_execpath) {
  throw new Error('Run this script with npm --prefix web run bundle');
}
const version = process.env.WALLET_BRIDGE_VERSION ?? 'dev';
if (!/^(?:dev|v?\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?)$/.test(version)) {
  throw new Error('WALLET_BRIDGE_VERSION must be dev or a semantic version');
}

function run(command, commandArgs, env = process.env) {
  const result = spawnSync(command, commandArgs, { cwd: root, env, stdio: 'inherit' });
  if (result.error) throw result.error;
  if (result.status !== 0) process.exit(result.status ?? 1);
}

run(process.execPath, [process.env.npm_execpath, '--prefix', join(root, 'web'), 'run', 'build']);
const native = {
  goos: process.platform === 'win32' ? 'windows' : process.platform,
  goarch: { x64: 'amd64', arm64: 'arm64' }[process.arch],
};
if (!native.goarch) throw new Error(`Unsupported host architecture: ${process.arch}`);
const targets = args[0] === '--all'
  ? [
      { goos: 'windows', goarch: 'amd64' },
      { goos: 'darwin', goarch: 'amd64' },
      { goos: 'darwin', goarch: 'arm64' },
    ]
  : [native];

mkdirSync(join(root, 'dist'), { recursive: true });
for (const { goos, goarch } of targets) {
  const name = `wallet-bridge-${goos}-${goarch}${goos === 'windows' ? '.exe' : ''}`;
  run('go', [
    'build', '-trimpath', '-mod=readonly',
    '-ldflags', `-s -w -X github.com/Conflux-Chain/wallet-bridge/internal/buildinfo.Version=${version}`,
    '-o', join(root, 'dist', name), './cmd/wallet-bridge',
  ], { ...process.env, GOOS: goos, GOARCH: goarch, CGO_ENABLED: '0', GOFLAGS: '' });
  console.log(`Built dist/${name}`);
}
