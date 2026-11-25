import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, renameSync, rmSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { binaryName, buildBundles, native, releaseTargets, root } from './build.mjs';
import { checksumList, programAssets, releaseChannel } from './release-assets.mjs';

const [version, ...extra] = process.argv.slice(2);
if (extra.length || !version) {
  throw new Error('Usage: npm --prefix web run release:build -- <vMAJOR.MINOR.PATCH[-prerelease][+metadata]>');
}
releaseChannel(version);

const releases = join(root, 'dist', 'releases');
const output = join(releases, version);
if (existsSync(output)) throw new Error(`Release directory already exists: ${output}`);
mkdirSync(releases, { recursive: true });
const staging = mkdtempSync(join(releases, '.build-'));
try {
  buildBundles(staging, releaseTargets, version);
  if (releaseTargets.some(t => t.goos === native.goos && t.goarch === native.goarch)) {
    const file = join(staging, binaryName(native));
    const info = JSON.parse(execFileSync(file, ['version', '--json'], { encoding: 'utf8' }));
    assert.equal(info.version, version);
    assert.equal(info.platform, `${native.goos}/${native.goarch}`);
    assert.equal(info.uiEmbedded, true);
  }
  writeFileSync(join(staging, 'SHA256SUMS'), checksumList(programAssets(staging)));
  renameSync(staging, output);
  console.log(`Built ${output}`);
} finally {
  rmSync(staging, { recursive: true, force: true });
}
