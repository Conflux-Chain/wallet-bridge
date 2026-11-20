import assert from 'node:assert/strict';
import { chmod, copyFile, mkdir, mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { isAbsolute, join, relative } from 'node:path';
import { cli, root } from './bridge';

export default async function setup() {
  const temporary = await mkdtemp(join(tmpdir(), 'wallet-bridge-browser-'));
  const directory = join(temporary, 'data');
  const goos = process.platform === 'win32' ? 'windows' : process.platform;
  const goarch = process.arch === 'x64' ? 'amd64' : process.arch === 'arm64' ? 'arm64' : undefined;
  assert.ok(goarch, 'Unsupported browser test architecture');
  const name = `wallet-bridge-${goos}-${goarch}${goos === 'windows' ? '.exe' : ''}`;
  const programDirectory = join(temporary, 'Wallet Bridge 独立程序');
  const binary = join(programDirectory, name);
  await mkdir(programDirectory);
  // The runtime receives only one executable, without repository or web assets.
  await copyFile(join(root, 'dist', name), binary);
  await chmod(binary, 0o700);
  process.env.WALLET_BRIDGE_TEST_DATA = directory;
  process.env.WALLET_BRIDGE_TEST_EXECUTABLE = binary;
  const cleanup = async () => {
    // An unverifiable service is never stopped or followed by data deletion.
    const status = JSON.parse(await cli(['service', 'stop', '--json'], directory));
    assert.equal(status.state, 'stopped', 'Test service shutdown was not confirmed');
    const target = relative(tmpdir(), temporary);
    assert.ok(!isAbsolute(target) && !target.startsWith('..') && target.startsWith('wallet-bridge-browser-'), 'Unsafe browser test cleanup path');
    await rm(temporary, { recursive: true, force: true });
  };
  try {
    const status = JSON.parse(await cli(['service', 'ensure', '--json'], directory));
    assert.equal(status.state, 'running');
    console.log(`Standalone browser environment: ${goos}/${goarch}, one executable outside repository, empty PATH`);
  } catch {
    // Retain uncertain startup data for inspection instead of touching another service.
    throw new Error(`Browser test setup failed; ensure port 18765 is free. Test data retained at ${temporary}`);
  }
  return cleanup;
}
