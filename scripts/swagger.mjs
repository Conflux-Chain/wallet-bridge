import { spawnSync } from 'node:child_process';
import { dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = dirname(dirname(fileURLToPath(import.meta.url)));
const result = spawnSync('go', [
  'run', 'github.com/swaggo/swag/cmd/swag@v1.16.6', 'init',
  '--dir', 'internal/api', '--generalInfo', 'docs.go',
  '--output', 'docs/swagger', '--outputTypes', 'json,yaml', '--quiet',
], { cwd: root, stdio: 'inherit' });
if (result.error) throw result.error;
process.exit(result.status ?? 1);
