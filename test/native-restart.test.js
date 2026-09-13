import test from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

test('Linux runtime survives PrivateTmp restart and requires process exit evidence before stopped', () => {
  const script = fileURLToPath(new URL('./native-restart-fixture.py', import.meta.url));
  const module = fileURLToPath(new URL('../runtime/native/host.py', import.meta.url));
  const result = spawnSync('python3', [script, module], { encoding: 'utf8', timeout: 10000 });
  assert.equal(result.status, 0, result.stderr || result.stdout || result.error?.message);
});
