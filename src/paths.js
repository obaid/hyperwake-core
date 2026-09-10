import { homedir } from 'node:os';
import { join, dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { mkdirSync } from 'node:fs';

const here = dirname(fileURLToPath(import.meta.url));

/** The installed package root, so bundled runtime scripts are found after npx. */
export const packageRoot = resolve(here, '..');

/**
 * Where the engine keeps its own state.
 *
 * Deliberately outside the package: `npx` installs into a cache that is wiped
 * without warning, and a machine's disk must outlive the tool that made it.
 */
export function stateDir() {
  const base = process.env.HYPERWAKE_HOME || join(homedir(), '.hyperwake');
  mkdirSync(base, { recursive: true, mode: 0o700 });
  return base;
}

export function statePath(...parts) {
  return join(stateDir(), ...parts);
}

/**
 * The Python runtime that supervises QEMU.
 *
 * Shipped inside the package, but a checkout is preferred when present so
 * development does not require reinstalling.
 */
export function runtimeScript(name) {
  const override = process.env.HYPERWAKE_RUNTIME_DIR;
  if (override) return join(override, name);
  return join(packageRoot, 'runtime', name);
}
