import { spawn } from 'node:child_process';
import { runtimeScript, statePath } from './paths.js';
import { pythonBin } from './python.js';
import { join } from 'node:path';

/**
 * Runs one automation verb against a guest.
 *
 * Shell and file actions travel over SSH; screen and input actions over the
 * guest's VNC socket. Both connection targets come from the runtime, never from
 * the request: a caller may say *what* to do, never *where* to do it.
 */
export function runAction(target, action, timeoutMs = 180_000) {
  const payload = {
    target: {
      id: target.id,
      ssh_host: target.ssh_host,
      ssh_port: target.ssh_port,
      display_host: target.display_host,
      display_port: target.display_port,
      ssh_key: join(statePath('keys'), 'guest'),
      known_hosts: join(statePath('keys'), 'known_hosts'),
    },
    action,
  };

  return new Promise((resolve, reject) => {
    const child = spawn(pythonBin(), [runtimeScript('automation.py')], { stdio: ['pipe', 'pipe', 'pipe'] });
    let out = '';
    let err = '';
    const timer = setTimeout(() => { child.kill('SIGKILL'); reject(new Error('Automation timed out.')); }, timeoutMs);

    child.stdout.on('data', (chunk) => { out += chunk; });
    child.stderr.on('data', (chunk) => { err += chunk; });
    child.on('error', (error) => { clearTimeout(timer); reject(error); });
    child.on('close', (code) => {
      clearTimeout(timer);
      if (code !== 0) return reject(new Error(err.trim() || 'Automation failed.'));
      try { resolve(JSON.parse(out)); } catch { reject(new Error('Automation returned malformed output.')); }
    });

    child.stdin.end(JSON.stringify(payload));
  });
}
