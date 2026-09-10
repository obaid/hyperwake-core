import { execFileSync } from 'node:child_process';
import { existsSync } from 'node:fs';
import { join } from 'node:path';
import { statePath, runtimeScript } from './paths.js';

/**
 * A private virtual environment for the automation dependencies.
 *
 * Screen capture and input replay go through `vncdotool`, which is not in any
 * standard install. Rather than asking the operator to pip-install into a
 * system Python — which is both rude and increasingly forbidden by distros —
 * the engine keeps its own environment beside its other state.
 */
export function pythonBin() {
  const venv = statePath('python');
  const binary = join(venv, 'bin', 'python3');
  if (existsSync(binary)) return binary;
  return 'python3';
}

export function ensurePython({ quiet = false } = {}) {
  const venv = statePath('python');
  const binary = join(venv, 'bin', 'python3');

  if (!existsSync(binary)) {
    if (!quiet) console.log('  preparing the automation environment (first run only)...');
    execFileSync('python3', ['-m', 'venv', venv], { stdio: quiet ? 'ignore' : 'inherit' });
  }

  try {
    execFileSync(binary, ['-c', 'import vncdotool'], { stdio: 'ignore' });
  } catch {
    if (!quiet) console.log('  installing vncdotool...');
    execFileSync(binary, ['-m', 'pip', 'install', '--quiet', '--disable-pip-version-check', '-r', runtimeScript('requirements.txt')], {
      stdio: quiet ? 'ignore' : 'inherit',
      timeout: 600_000,
    });
  }
  return binary;
}
