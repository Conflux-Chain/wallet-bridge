import assert from 'node:assert/strict';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { isAbsolute, join, relative } from 'node:path';
import { cli } from './bridge';

export default async function setup() {
  const temporary = await mkdtemp(join(tmpdir(), 'wallet-bridge-browser-'));
  const directory = join(temporary, 'data');
  process.env.WALLET_BRIDGE_TEST_DATA = directory;
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
  } catch {
    // Retain uncertain startup data for inspection instead of touching another service.
    throw new Error(`Browser test setup failed; ensure port 18765 is free. Test data retained at ${temporary}`);
  }
  return cleanup;
}
