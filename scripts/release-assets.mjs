import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { binaryName, releaseTargets } from './build.mjs';

export function releaseChannel(version) {
  const tag = /^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-([\da-zA-Z-]+(?:\.[\da-zA-Z-]+)*))?(?:\+[\da-zA-Z-]+(?:\.[\da-zA-Z-]+)*)?$/;
  const match = tag.exec(version);
  if (!match || match[4]?.split('.').some(id => /^0\d+$/.test(id))) {
    throw new Error('Expected a release version such as v1.0.0 or v1.0.0-rc.1');
  }
  return match[4] ? 'prerelease' : 'stable';
}

export function sha256(bytes) {
  return createHash('sha256').update(bytes).digest('hex');
}

export function programAssets(dir) {
  return releaseTargets.map(target => {
    const name = binaryName(target);
    return { name, sha256: sha256(readFileSync(join(dir, name))) };
  });
}

export function checksumList(assets) {
  return assets.map(asset => `${asset.sha256}  ${asset.name}\n`).join('');
}
