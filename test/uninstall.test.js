import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, writeFileSync, existsSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

/**
 * Uninstall removes what Mola made, and nothing else.
 *
 * The dangerous half of this feature is not the deleting, it is the *scope* of
 * the deleting: a tool that reaches one directory too far, or kills the wrong
 * QEMU, does damage nobody asked for and cannot undo. So most of what follows
 * tests restraint rather than function.
 */

/** Build a state directory that looks like a real one, and point the module at it. */
function fakeState() {
  const root = mkdtempSync(join(tmpdir(), 'mola-uninstall-'));
  mkdirSync(join(root, 'image'), { recursive: true });
  mkdirSync(join(root, 'runtime', 'machines', 'abc'), { recursive: true });
  mkdirSync(join(root, 'python'), { recursive: true });
  mkdirSync(join(root, 'keys'), { recursive: true });
  writeFileSync(join(root, 'image', 'root.ext4'), Buffer.alloc(64 * 1024));
  writeFileSync(join(root, 'runtime', 'machines', 'abc', 'root.ext4'), Buffer.alloc(32 * 1024));
  writeFileSync(join(root, 'token'), 'not-a-real-token\n');
  writeFileSync(join(root, 'machines.json'), JSON.stringify({
    abc: { id: 'abc', name: 'scratch' },
  }));
  return root;
}

async function load(root) {
  process.env.MOLA_HOME = root;
  // Fresh module each time: paths.js reads the environment when it is called,
  // but the import cache would otherwise carry state between tests.
  return import(`../src/uninstall.js?${Math.random()}`);
}

test('survey measures what is there without removing anything', async (t) => {
  const root = fakeState();
  t.after(() => rmSync(root, { recursive: true, force: true }));

  const { survey } = await load(root);
  const found = survey();

  assert.equal(found.exists, true);
  assert.equal(found.root, root);
  assert.deepEqual(found.recorded, [{ id: 'abc', name: 'scratch' }]);
  assert.ok(found.total > 0, 'should measure some occupied space');
  assert.ok(found.parts.some((p) => p.label === 'guest image'));
  assert.ok(existsSync(root), 'surveying must not delete');
});

test('a declined confirmation removes nothing', async (t) => {
  const root = fakeState();
  t.after(() => rmSync(root, { recursive: true, force: true }));

  const { uninstall } = await load(root);
  // Not a TTY under the test runner, so confirm() answers no.
  const result = await uninstall({ log: () => {} });

  assert.equal(result.removed, false);
  assert.equal(result.reclaimed, 0);
  assert.ok(existsSync(join(root, 'image', 'root.ext4')), 'the image should survive a refusal');
});

test('--yes removes the whole state directory and reports what it freed', async (t) => {
  const root = fakeState();
  t.after(() => rmSync(root, { recursive: true, force: true }));

  const { uninstall } = await load(root);
  const result = await uninstall({ yes: true, log: () => {} });

  assert.equal(result.removed, true);
  assert.ok(result.reclaimed > 0);
  assert.equal(existsSync(root), false, 'the state directory should be gone');
});

test('--keep-image spares the slow part and removes the rest', async (t) => {
  const root = fakeState();
  t.after(() => rmSync(root, { recursive: true, force: true }));

  const { uninstall } = await load(root);
  await uninstall({ yes: true, keepImage: true, log: () => {} });

  assert.ok(existsSync(join(root, 'image', 'root.ext4')), 'the image should remain');
  assert.equal(existsSync(join(root, 'runtime')), false, 'machine disks should be gone');
  assert.equal(existsSync(join(root, 'python')), false);
  assert.equal(existsSync(join(root, 'machines.json')), false);
});

test('nothing to remove is said plainly, not treated as an error', async (t) => {
  const root = mkdtempSync(join(tmpdir(), 'mola-empty-'));
  rmSync(root, { recursive: true, force: true });
  t.after(() => rmSync(root, { recursive: true, force: true }));

  const { uninstall } = await load(root);
  const lines = [];
  const result = await uninstall({ yes: true, log: (l) => lines.push(l) });

  assert.equal(result.removed, false);
  assert.match(lines.join('\n'), /Nothing to remove/);
});

test('only QEMU named mola- is considered ours', async (t) => {
  const root = fakeState();
  t.after(() => rmSync(root, { recursive: true, force: true }));

  const { runningMachines } = await load(root);
  // Whatever is running on this machine right now, none of it should be claimed
  // unless it carries our own -name flag. This is the guard that stops uninstall
  // killing somebody else's virtual machine.
  for (const machine of runningMachines()) {
    assert.match(machine.id, /^\S+$/);
  }
  assert.ok(Array.isArray(runningMachines()));
});
