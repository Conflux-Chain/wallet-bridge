import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, renameSync, rmSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { binaryName, buildBundles, native, releaseTargets, root } from './build.mjs';

const [version, ...extra] = process.argv.slice(2);
const tag = /^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-([\da-zA-Z-]+(?:\.[\da-zA-Z-]+)*))?(?:\+[\da-zA-Z-]+(?:\.[\da-zA-Z-]+)*)?$/;
const match = tag.exec(version);
if (extra.length || !match || match[4]?.split('.').some(id => /^0\d+$/.test(id))) {
  throw new Error('Usage: npm --prefix web run release:build -- <vMAJOR.MINOR.PATCH[-prerelease][+metadata]>');
}

const releases = join(root, 'dist', 'releases');
const output = join(releases, version);
if (existsSync(output)) throw new Error(`Release directory already exists: ${output}`);
mkdirSync(releases, { recursive: true });
const staging = mkdtempSync(join(releases, '.build-'));
try {
  buildBundles(staging, releaseTargets, version);
  const checksums = [];
  for (const target of releaseTargets) {
    const name = binaryName(target);
    const file = join(staging, name);
    if (target.goos === native.goos && target.goarch === native.goarch) {
      const info = JSON.parse(execFileSync(file, ['version', '--json'], { encoding: 'utf8' }));
      assert.equal(info.version, version);
      assert.equal(info.platform, `${native.goos}/${native.goarch}`);
      assert.equal(info.uiEmbedded, true);
    }
    const digest = createHash('sha256').update(readFileSync(file)).digest('hex');
    checksums.push(`${digest}  ${name}\n`);
  }
  writeFileSync(join(staging, 'SHA256SUMS'), checksums.join(''));
  renameSync(staging, output);
  console.log(`Built ${output}`);
} finally {
  rmSync(staging, { recursive: true, force: true });
}
