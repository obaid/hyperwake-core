import { test } from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { mkdtempSync, mkdirSync, writeFileSync, rmSync, lstatSync, readlinkSync, statSync, existsSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

/**
 * The session script must not let anything fill the runtime tmpfs.
 *
 * This is the bug that made machines lose the ability to launch any graphical
 * application after about a day: Hyprland's own log grew into a 392 MB tmpfs
 * until systemd could no longer write transient scope units. Nothing caught it
 * because the shell path kept working perfectly, so a broken machine looked
 * healthy from every direction an automated check was looking.
 *
 * The compositor itself needs a KVM guest and cannot run here. The containment
 * around it can, and it is the half that failed.
 */

const SESSION = join(import.meta.dirname, '../image/omarchy/rootfs/usr/local/bin/mola-session');

/** Run one of the script's maintenance subcommands against a throwaway tree. */
function run(mode, { runtime, logs, cap }) {
  return execFileSync('bash', [SESSION, mode], {
    encoding: 'utf8',
    env: {
      ...process.env,
      XDG_RUNTIME_DIR: runtime,
      MOLA_LOG_DIR: logs,
      ...(cap ? { MOLA_LOG_CAP_BYTES: String(cap) } : {}),
    },
  });
}

function tree(t) {
  const root = mkdtempSync(join(tmpdir(), 'mola-session-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const runtime = join(root, 'run');
  const logs = join(root, 'log');
  mkdirSync(runtime, { recursive: true });
  return { root, runtime, logs };
}

test("Hyprland's instance directory is moved off the runtime tmpfs", (t) => {
  const { runtime, logs } = tree(t);

  run('--contain-logs', { runtime, logs });

  const link = join(runtime, 'hypr');
  assert.ok(lstatSync(link).isSymbolicLink(), 'runtime hypr/ should be a link, not a directory');
  assert.equal(readlinkSync(link), join(logs, 'hypr'));
});

test('a directory left by a previous boot is replaced, not nested inside', (t) => {
  const { runtime, logs } = tree(t);

  // What a machine that booted before this fix looks like.
  mkdirSync(join(runtime, 'hypr', 'abc123'), { recursive: true });
  writeFileSync(join(runtime, 'hypr', 'abc123', 'hyprland.log'), 'x'.repeat(1024));

  run('--contain-logs', { runtime, logs });

  assert.ok(lstatSync(join(runtime, 'hypr')).isSymbolicLink());
  assert.equal(existsSync(join(logs, 'hypr', 'abc123')), false,
    'the old instance directory must not be carried onto the disk copy');
});

test('instance directories do not accumulate across restarts', (t) => {
  const { runtime, logs } = tree(t);

  run('--contain-logs', { runtime, logs });
  mkdirSync(join(logs, 'hypr', 'first-boot'), { recursive: true });
  writeFileSync(join(logs, 'hypr', 'first-boot', 'hyprland.log'), 'x'.repeat(4096));

  run('--contain-logs', { runtime, logs });

  assert.equal(existsSync(join(logs, 'hypr', 'first-boot')), false,
    'each start should clear the previous start, or growth just moves to the disk');
});

test('a log past the cap is truncated, and one under it is left alone', (t) => {
  const { runtime, logs } = tree(t);
  const cap = 64 * 1024;

  run('--contain-logs', { runtime, logs });
  mkdirSync(join(logs, 'hypr', 'sig'), { recursive: true });
  const big = join(logs, 'hypr', 'sig', 'hyprland.log');
  const small = join(logs, 'seatd.log');
  writeFileSync(big, 'x'.repeat(cap * 4));
  writeFileSync(small, 'y'.repeat(128));

  run('--truncate-logs', { runtime, logs, cap });

  assert.equal(statSync(big).size, 0, 'the oversized log should be emptied');
  assert.equal(statSync(small).size, 128, 'a small log should be untouched');
});

test('an unwritable log directory is survivable, not fatal', (t) => {
  const { runtime, root } = tree(t);
  // A path that cannot be created, standing in for /var/log on a guest where
  // the entrypoint did not run. The session must still start the desktop.
  const logs = join(root, 'not-a-dir', 'log');
  writeFileSync(join(root, 'not-a-dir'), '');

  const result = execFileSync('bash', [SESSION, '--contain-logs'], {
    encoding: 'utf8',
    env: { ...process.env, HOME: root, XDG_RUNTIME_DIR: runtime, MOLA_LOG_DIR: logs },
  });

  // It falls back to the home directory rather than giving up.
  assert.ok(existsSync(join(root, '.mola/log/hypr')), `fell back; said: ${result}`);
});
