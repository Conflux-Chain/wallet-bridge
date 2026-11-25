import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { binaryName, native, root } from './build.mjs';
import { checksumList, programAssets, releaseChannel, sha256 } from './release-assets.mjs';

const repo = 'Conflux-Chain/wallet-bridge';

export function verifyDraft(candidate, draft, tagRevision) {
  assert.equal(draft.isDraft, true, 'Expected an unpublished draft');
  assert.equal(draft.tagName, candidate.version, 'Draft tag differs');
  assert.equal(tagRevision, candidate.revision, 'Tag points to a different commit');
  assert.equal(draft.isPrerelease, candidate.channel === 'prerelease', 'Draft channel differs');
  assert.equal(draft.assets.length, candidate.assets.length, 'Draft asset count differs');
  for (const expected of candidate.assets) {
    const actual = draft.assets.find(asset => asset.name === expected.name);
    assert.ok(actual, `Missing draft asset: ${expected.name}`);
    assert.equal(actual.state, 'uploaded', `Upload incomplete: ${expected.name}`);
    assert.equal(actual.digest, `sha256:${expected.sha256}`, `Draft digest differs: ${expected.name}`);
  }
}

if (import.meta.main) {
  const [version, ...options] = process.argv.slice(2);
  const checkDraft = options[0] === '--draft' && options.length === 2 && /^[0-9a-f]{64}$/.test(options[1]);
  if (!version || (options.length && !checkDraft)) {
    throw new Error('Usage: npm --prefix web run release:review -- <version> [--draft <accepted SHA256SUMS digest>]');
  }
  const channel = releaseChannel(version);
  const dir = join(root, 'dist', 'releases', version);
  const manifest = readFileSync(join(dir, 'SHA256SUMS'));
  const manifestSha256 = sha256(manifest);
  if (checkDraft) assert.equal(manifestSha256, options[1], 'Manifest differs from the accepted candidate');
  const assets = programAssets(dir);
  assert.ok(manifest.equals(Buffer.from(checksumList(assets))), 'Program checksums differ from SHA256SUMS');
  assets.push({ name: 'SHA256SUMS', sha256: manifestSha256 });
  const program = join(dir, binaryName(native));
  const info = JSON.parse(execFileSync(program, ['version', '--json'], { encoding: 'utf8' }));
  assert.equal(info.version, version, 'Embedded version differs');
  assert.equal(info.platform, `${native.goos}/${native.goarch}`, 'Native platform differs');
  assert.equal(info.uiEmbedded, true, 'Missing approval UI');
  const candidate = { version, channel, revision: info.revision, modified: info.modified, manifestSha256, assets };
  if (checkDraft) {
    assert.equal(info.modified, false, 'Draft assets must correspond to the tagged source');
    const draft = JSON.parse(execFileSync('gh', [
      'release', 'view', version, '--repo', repo, '--json', 'tagName,isDraft,isPrerelease,assets',
    ], { encoding: 'utf8' }));
    const tagRevision = execFileSync('gh', [
      'api', `repos/${repo}/commits/refs/tags/${version}`, '--jq', '.sha',
    ], { encoding: 'utf8' }).trim();
    verifyDraft(candidate, draft, tagRevision);
  }
  console.log(JSON.stringify(candidate, null, 2));
}
