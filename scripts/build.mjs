import { spawnSync } from 'node:child_process';
import { mkdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

export const root = dirname(dirname(fileURLToPath(import.meta.url)));
export const releaseTargets = [
  { goos: 'windows', goarch: 'amd64' },
  { goos: 'darwin', goarch: 'amd64' },
  { goos: 'darwin', goarch: 'arm64' },
];
export const native = {
  goos: process.platform === 'win32' ? 'windows' : process.platform,
  goarch: { x64: 'amd64', arm64: 'arm64' }[process.arch],
};

function run(command, commandArgs, env = process.env) {
  const result = spawnSync(command, commandArgs, { cwd: root, env, stdio: 'inherit' });
  if (result.error) throw result.error;
  if (result.status !== 0) throw new Error(`${command} failed: ${result.signal ?? result.status}`);
}

export function binaryName({ goos, goarch }) {
  return `wallet-bridge-${goos}-${goarch}${goos === 'windows' ? '.exe' : ''}`;
}

export function buildBundles(output, targets, version) {
  if (!process.env.npm_execpath) throw new Error('Run the build via npm');
  run(process.execPath, [process.env.npm_execpath, '--prefix', join(root, 'web'), 'run', 'build']);
  mkdirSync(output, { recursive: true });
  for (const target of targets) {
    const file = join(output, binaryName(target));
    run('go', [
      'build', '-trimpath', '-mod=readonly',
      '-ldflags', `-s -w -X github.com/Conflux-Chain/wallet-bridge/internal/buildinfo.Version=${version}`,
      '-o', file, './cmd/wallet-bridge',
    ], { ...process.env, GOOS: target.goos, GOARCH: target.goarch, CGO_ENABLED: '0', GOFLAGS: '' });
  }
}

if (import.meta.main) {
  const args = process.argv.slice(2);
  if (args.length > 1 || (args.length === 1 && args[0] !== '--all')) {
    throw new Error('Usage: npm --prefix web run bundle [-- --all]');
  }
  if (!native.goarch) throw new Error(`Unsupported host architecture: ${process.arch}`);
  const version = process.env.WALLET_BRIDGE_VERSION ?? 'dev';
  if (!/^(?:dev|v?\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?)$/.test(version)) {
    throw new Error('WALLET_BRIDGE_VERSION must be dev or a semantic version');
  }
  const targets = args[0] === '--all' ? releaseTargets : [native];
  buildBundles(join(root, 'dist'), targets, version);
  for (const target of targets) console.log(`Built dist/${binaryName(target)}`);
}
