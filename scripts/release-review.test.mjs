import assert from 'node:assert/strict';
import test from 'node:test';
import { releaseChannel } from './release-assets.mjs';
import { verifyDraft } from './release-review.mjs';

test('channel follows the prerelease component, including versions with build metadata', () => {
  assert.equal(releaseChannel('v1.2.3+build-1'), 'stable');
  assert.equal(releaseChannel('v1.2.3-rc.1+build-1'), 'prerelease');
  assert.throws(() => releaseChannel('v1.2.3-rc.01'));
});

test('the draft must match the accepted files, tag, commit and channel', () => {
  const candidate = {
    version: 'v1.0.0-rc.1', revision: 'abc', channel: 'prerelease',
    assets: [{ name: 'program', sha256: '11' }, { name: 'SHA256SUMS', sha256: '22' }],
  };
  const files = candidate.assets.map(a => ({ name: a.name, state: 'uploaded', digest: `sha256:${a.sha256}` }));
  const draft = { tagName: candidate.version, isDraft: true, isPrerelease: true, assets: files.toReversed() };
  verifyDraft(candidate, draft, candidate.revision);
  assert.throws(() => verifyDraft(candidate, draft, 'other'));
  for (const change of [
    { isDraft: false }, { tagName: 'v2.0.0' }, { isPrerelease: false },
    { assets: files.slice(1) }, { assets: [...files, files[0]] }, { assets: [files[1], files[1]] },
    { assets: [{ ...files[0], digest: 'sha256:33' }, files[1]] },
    { assets: [{ ...files[0], state: 'new' }, files[1]] },
  ]) {
    assert.throws(() => verifyDraft(candidate, { ...draft, ...change }, candidate.revision));
  }
});
