import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { statePath } from './paths.js';

/**
 * The engine's own key for reaching guests.
 *
 * Generated here and never shared: a key baked into an image would be the same
 * key on every machine anyone ever built from it. This one is created on first
 * run, kept outside the package, and handed to each guest as an authorized key
 * at creation time.
 */
export function guestKey() {
  const dir = statePath('keys');
  mkdirSync(dir, { recursive: true, mode: 0o700 });
  const priv = join(dir, 'guest');
  const pub = `${priv}.pub`;

  if (!existsSync(priv)) {
    execFileSync('ssh-keygen', ['-t', 'ed25519', '-N', '', '-C', 'hyperwake-engine', '-f', priv], { stdio: 'ignore' });
  }
  return { privateKey: priv, publicKey: readFileSync(pub, 'utf8').trim(), knownHosts: join(dir, 'known_hosts') };
}
