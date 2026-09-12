import { execFileSync } from 'node:child_process';
import { existsSync, readdirSync, readFileSync, rmSync, statSync } from 'node:fs';
import { homedir } from 'node:os';
import { createInterface } from 'node:readline/promises';
import { join } from 'node:path';

/**
 * Where the state lives, WITHOUT creating it.
 *
 * paths.js's stateDir() makes the directory as a side effect, which is right
 * for every other caller and exactly wrong here: asking "is there anything to
 * remove?" would answer yes by having just created it.
 */
const stateDir = () => process.env.MOLA_HOME || join(homedir(), '.mola');
const statePath = (...parts) => join(stateDir(), ...parts);

/**
 * Removing everything Mola put on this computer.
 *
 * Worth having because the engine deliberately keeps its state outside the
 * package: `npx` clears its own cache, but a machine's disk has to outlive the
 * tool that made it, so nothing else will ever reclaim it. A 16 GB image and a
 * pile of per-machine disks sit there until somebody deletes them by hand, and
 * most people will not know where to look.
 *
 * The rule throughout: touch only what Mola created. A QEMU process is ours
 * only if it was started with `-name mola-<id>`; a directory is ours only if it
 * is the state directory. Anything else is somebody else's and stays.
 */

/** The old name's state directory, orphaned by the rename to Mola. */
const LEGACY = join(homedir(), '.hyperwake');

const mb = (bytes) => `${(bytes / 1024 / 1024).toFixed(0)} MB`;
const gb = (bytes) => (bytes >= 1024 ** 3 ? `${(bytes / 1024 ** 3).toFixed(1)} GB` : mb(bytes));

/**
 * Measure a directory as the disk sees it.
 *
 * Sparse files matter here: a machine disk claims 40 GB and occupies a fraction
 * of that, so reporting the apparent size would promise space that was never
 * taken. `blocks` is in 512-byte units and counts what is really allocated.
 */
function sizeOf(path) {
  let total = 0;
  const walk = (dir) => {
    let entries;
    try {
      entries = readdirSync(dir, { withFileTypes: true });
    } catch {
      return;
    }
    for (const entry of entries) {
      const full = join(dir, entry.name);
      try {
        if (entry.isDirectory() && !entry.isSymbolicLink()) walk(full);
        else if (entry.isFile()) total += statSync(full).blocks * 512;
      } catch {
        // A file that vanished mid-walk is not worth failing over.
      }
    }
  };
  walk(path);
  return total;
}

/**
 * QEMU processes this engine started.
 *
 * Matched on the `-name mola-<id>` the runtime gives every machine, so another
 * project's QEMU, or a virtual machine somebody started by hand, is invisible
 * to this and stays running.
 */
export function runningMachines() {
  let output = '';
  try {
    output = execFileSync('ps', ['-axo', 'pid=,command='], { encoding: 'utf8', maxBuffer: 8 << 20 });
  } catch {
    return [];
  }
  return output
    .split('\n')
    .map((line) => line.match(/^\s*(\d+)\s+(.*)$/))
    .filter(Boolean)
    .filter(([, , command]) => /qemu-system/.test(command) && /\s-name\s+mola-/.test(command))
    .map(([, pid, command]) => ({
      pid: Number(pid),
      id: command.match(/\s-name\s+mola-(\S+)/)?.[1] ?? 'unknown',
    }));
}

/** Everything that would be removed, measured before anything is touched. */
export function survey() {
  const root = stateDir();
  const machinesFile = statePath('machines.json');

  let recorded = [];
  try {
    const parsed = JSON.parse(readFileSync(machinesFile, 'utf8'));
    recorded = Object.values(parsed ?? {}).map((m) => ({ id: m.id, name: m.name }));
  } catch {
    // No file, or an unreadable one. Either way there is nothing to list.
  }

  const parts = [
    { label: 'guest image', path: statePath('image') },
    { label: 'machine disks', path: statePath('runtime') },
    { label: 'python runtime', path: statePath('python') },
    { label: 'keys and token', path: statePath('keys') },
  ].filter((p) => existsSync(p.path))
    .map((p) => ({ ...p, bytes: sizeOf(p.path) }));

  const sockets = existsSync('/tmp')
    ? readdirSync('/tmp')
      .filter((n) => n.startsWith(`mola-${process.getuid?.() ?? 0}-`))
      .map((n) => join('/tmp', n))
    : [];

  return {
    root,
    exists: existsSync(root),
    parts,
    total: parts.reduce((sum, p) => sum + p.bytes, 0),
    recorded,
    running: runningMachines(),
    sockets,
    legacy: existsSync(LEGACY) ? { path: LEGACY, bytes: sizeOf(LEGACY) } : null,
  };
}

/** Ask the engine to tear its machines down properly, if it is listening. */
async function deleteThroughEngine(log) {
  const base = process.env.MOLA_API || `http://127.0.0.1:${process.env.MOLA_PORT || 4141}`;
  const tokenFile = statePath('token');
  if (!existsSync(tokenFile)) return false;
  const token = readFileSync(tokenFile, 'utf8').trim();

  let machines;
  try {
    const response = await fetch(`${base}/v1/machines`, {
      headers: { authorization: `Bearer ${token}` },
      signal: AbortSignal.timeout(4000),
    });
    if (!response.ok) return false;
    machines = (await response.json()).data ?? [];
  } catch {
    return false; // Not running. The caller falls back to stopping QEMU itself.
  }

  log(`  engine is running; asking it to delete ${machines.length} machine${machines.length === 1 ? '' : 's'}`);
  for (const machine of machines) {
    try {
      await fetch(`${base}/v1/machines/${encodeURIComponent(machine.id)}`, {
        method: 'DELETE',
        headers: { authorization: `Bearer ${token}` },
        signal: AbortSignal.timeout(120_000),
      });
      log(`    deleted ${machine.id.slice(0, 8)}`);
    } catch {
      log(`    could not delete ${machine.id.slice(0, 8)}; its disk goes with the state directory`);
    }
  }
  return true;
}

/** Stop QEMU ourselves, politely first. */
async function stopQemu(running, log) {
  if (running.length === 0) return;
  log(`  stopping ${running.length} virtual machine${running.length === 1 ? '' : 's'}`);
  for (const { pid, id } of running) {
    try {
      process.kill(pid, 'SIGTERM');
      log(`    signalled ${id.slice(0, 8)} (pid ${pid})`);
    } catch { /* already gone */ }
  }

  // Give them a moment to exit before insisting.
  for (let waited = 0; waited < 10_000; waited += 500) {
    if (runningMachines().length === 0) return;
    await new Promise((r) => setTimeout(r, 500));
  }
  for (const { pid } of runningMachines()) {
    try { process.kill(pid, 'SIGKILL'); } catch { /* gone */ }
  }
}

async function confirm(question) {
  if (!process.stdin.isTTY) return false;
  const rl = createInterface({ input: process.stdin, output: process.stdout });
  const answer = (await rl.question(question)).trim().toLowerCase();
  rl.close();
  return answer === 'y' || answer === 'yes';
}

/**
 * Remove Mola from this computer.
 *
 * `keepImage` exists because the image is the slow part: someone clearing out
 * machines should not have to re-download a gigabyte to make the next one.
 */
export async function uninstall({ yes = false, keepImage = false, log = console.log, colour } = {}) {
  const { bold = (s) => s, dim = (s) => s, red = (s) => s, green = (s) => s } = colour ?? {};
  const found = survey();

  log(bold('\n  Mola — uninstall\n'));

  if (!found.exists && found.running.length === 0 && !found.legacy) {
    log(`  Nothing to remove. No state directory at ${dim(found.root)}.\n`);
    return { removed: false, reclaimed: 0 };
  }

  if (found.exists) {
    log(`  ${dim(found.root)}`);
    for (const part of found.parts) {
      log(`    ${part.label.padEnd(16)} ${gb(part.bytes).padStart(9)}`);
    }
    log(`    ${'total'.padEnd(16)} ${gb(found.total).padStart(9)}\n`);
  }

  if (found.recorded.length > 0) {
    log(`  ${found.recorded.length} machine${found.recorded.length === 1 ? '' : 's'} recorded, ${found.running.length} running now.`);
    log(`  ${red('Their disks go with them.')}\n`);
  } else if (found.running.length > 0) {
    log(`  ${found.running.length} virtual machine${found.running.length === 1 ? '' : 's'} still running.\n`);
  }

  if (keepImage) log(`  ${dim('Keeping the guest image, so the next machine does not re-download it.')}\n`);

  if (!yes) {
    const ok = await confirm(`  Remove all of this? ${dim('[y/N] ')}`);
    if (!ok) {
      log('\n  Left alone.\n');
      return { removed: false, reclaimed: 0 };
    }
    log('');
  }

  // Prefer a clean teardown so the runtime releases its own resources; fall
  // back to signalling QEMU when the engine is not there to ask.
  const viaEngine = await deleteThroughEngine(log);
  if (!viaEngine) await stopQemu(found.running, log);

  let reclaimed = 0;
  if (keepImage) {
    for (const part of found.parts.filter((p) => p.label !== 'guest image')) {
      rmSync(part.path, { recursive: true, force: true });
      reclaimed += part.bytes;
    }
    for (const file of ['machines.json', 'token']) {
      rmSync(statePath(file), { force: true });
    }
  } else {
    reclaimed = found.total;
    rmSync(found.root, { recursive: true, force: true });
  }

  for (const socket of found.sockets) rmSync(socket, { recursive: true, force: true });

  log(`\n  ${green('Removed.')} ${gb(reclaimed)} reclaimed.\n`);

  if (found.legacy) {
    log(`  There is also ${dim(found.legacy.path)} (${gb(found.legacy.bytes)}),`);
    log('  left by the old name. Nothing can reach it now.\n');
    const ok = yes || await confirm(`  Remove that too? ${dim('[y/N] ')}`);
    if (ok) {
      rmSync(found.legacy.path, { recursive: true, force: true });
      reclaimed += found.legacy.bytes;
      log(`\n  ${green('Removed.')} ${gb(found.legacy.bytes)} more reclaimed.\n`);
    } else {
      log('  Left alone.\n');
    }
  }

  return { removed: true, reclaimed };
}
