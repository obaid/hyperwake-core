import { test } from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

test('native snapshots preserve sparse disk data, reject corrupt restore, resume transfers and fence source', () => {
  const fixture = fileURLToPath(new URL('./native-storage-fixture.py', import.meta.url));
  const module = fileURLToPath(new URL('../runtime/native/host.py', import.meta.url));
  const result = spawnSync('python3', [fixture, module], { encoding: 'utf8' });
  assert.equal(result.status, 0, result.stderr || result.stdout);
});
