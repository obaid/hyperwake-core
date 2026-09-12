import { createHash } from 'node:crypto';
import { createWriteStream, existsSync, mkdirSync, readFileSync, renameSync, rmSync, statSync, truncateSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { pipeline } from 'node:stream/promises';
import { Readable } from 'node:stream';
import { createZstdDecompress } from 'node:zlib';
import { statePath } from './paths.js';

/**
 * Where a prebuilt guest image comes from.
 *
 * Overridable so an operator can host their own, or point at a file:// copy on
 * a machine with no internet. The manifest names each artifact, its digest and
 * whether it arrives compressed, so the engine never has to guess.
 */
const ROOTFS_NAME = 'root.ext4';
const INSTALLED = 'installed.json';

const DEFAULT_MANIFEST = 'https://github.com/obaid/mola-core/releases/download/image-latest/manifest.json';

export function manifestUrl() {
  return process.env.MOLA_IMAGE_URL || DEFAULT_MANIFEST;
}

export function imageDir() {
  return statePath('image');
}

export function hasImage() {
  return existsSync(join(imageDir(), 'root.ext4'));
}

const mb = (bytes) => (bytes / 1024 / 1024).toFixed(0);

function progress(label, done, total) {
  if (!process.stdout.isTTY) return;
  const pct = total ? ` ${Math.floor((done / total) * 100)}%` : '';
  const size = total ? `${mb(done)} / ${mb(total)} MB` : `${mb(done)} MB`;
  process.stdout.write(`\r  ${label} ${size}${pct}   `);
}

/**
 * Stream one artifact's bytes, in order, from however many URLs hold them.
 *
 * Large artifacts are split because a single multi-gigabyte upload is the
 * least reliable thing in the chain. Splitting also means a failed download
 * retries one part rather than the whole image.
 */
async function* artifactBytes(artifact, onChunk, base) {
  const parts = artifact.parts ?? [{ url: artifact.url, bytes: artifact.bytes }];
  for (const [index, part] of parts.entries()) {
    // Resolved against the manifest's own URL, so a bundle works from wherever
    // it is served. A published manifest names absolute release URLs and this
    // returns them unchanged; a manifest that names bare filenames resolves
    // them beside itself, which is what makes a local mirror or an offline
    // copy work without rewriting anything.
    const url = new URL(part.url, base).toString();
    let response;
    for (let attempt = 1; ; attempt += 1) {
      try {
        response = await fetch(url, { redirect: 'follow' });
        if (response.ok) break;
        throw new Error(`HTTP ${response.status}`);
      } catch (error) {
        if (attempt >= 3) throw new Error(`${artifact.name} part ${index + 1}: ${error.message}`);
        await new Promise((r) => setTimeout(r, attempt * 2000));
      }
    }
    for await (const chunk of Readable.fromWeb(response.body)) {
      onChunk(chunk);
      yield chunk;
    }
  }
}

/**
 * Fetch one artifact, verifying as the bytes arrive.
 *
 * The digest covers the whole compressed stream across every part, so a
 * truncated or tampered download fails before anything moves into place.
 */
async function fetchArtifact(artifact, into, base) {
  const total = artifact.bytes
    ?? (artifact.parts ?? []).reduce((sum, part) => sum + (part.bytes ?? 0), 0);
  const digest = createHash('sha256');
  let seen = 0;

  const counted = Readable.from(artifactBytes(artifact, (chunk) => {
    digest.update(chunk);
    seen += chunk.length;
    progress(artifact.name, seen, total);
  }, base));

  const target = join(into, artifact.name);
  const stages = artifact.compression === 'zstd'
    ? [counted, createZstdDecompress(), createWriteStream(target)]
    : [counted, createWriteStream(target)];

  await pipeline(...stages);
  if (process.stdout.isTTY) process.stdout.write('\n');

  const got = digest.digest('hex');
  if (artifact.sha256 && got !== artifact.sha256) {
    throw new Error(`${artifact.name}: checksum mismatch (expected ${artifact.sha256}, got ${got})`);
  }

  // A root filesystem ships at its used size and runs at its full size. Growing
  // it here costs nothing on a sparse filesystem and saves every machine from
  // discovering a disk smaller than its manifest claims.
  if (artifact.sparse_bytes && statSync(target).size < artifact.sparse_bytes) {
    truncateSync(target, artifact.sparse_bytes);
  }
}

/** Identity of an image: what it calls itself, and what it actually is. */
function identify(manifest) {
  const rootfs = (manifest?.artifacts ?? []).find((a) => a.name === ROOTFS_NAME);
  return { version: manifest?.version ?? null, digest: rootfs?.sha256 ?? null };
}

/** What is on disk, or null when nothing recorded it. */
export function installedImage() {
  try {
    return identify(JSON.parse(readFileSync(join(imageDir(), INSTALLED), 'utf8')));
  } catch {
    return null;
  }
}

/**
 * Whether the installed image is behind the published one.
 *
 * The version string names the Omarchy release and the architecture, so two
 * different builds of the same release share it. The comparison is therefore on
 * the root filesystem's digest, which is the only thing that actually changes
 * when the image is rebuilt.
 *
 * An image with nothing recorded beside it was installed by an engine that did
 * not write one, which is every engine before this check existed. That is not
 * ambiguity: it is old, and it is worth saying so.
 *
 * Never throws and never blocks. An engine that cannot start because a version
 * check could not reach the network would be a worse bug than the one this is
 * here to surface.
 */
export async function imageStatus({ url = manifestUrl(), timeout = 4000 } = {}) {
  const installed = installedImage();
  if (!installed) return { state: 'unrecorded' };

  try {
    const response = await fetch(url, { redirect: 'follow', signal: AbortSignal.timeout(timeout) });
    if (!response.ok) return { state: 'unknown' };
    const published = identify(await response.json());
    if (!published.digest || !installed.digest) return { state: 'unknown' };
    return published.digest === installed.digest
      ? { state: 'current', version: installed.version }
      : { state: 'stale', installed: installed.version, published: published.version };
  } catch {
    return { state: 'unknown' };
  }
}

/**
 * Download a guest image into place, or say why it could not.
 *
 * Builds into a sibling directory and renames on success, so an interrupted
 * download never leaves a half-image that looks complete to the next run.
 */
export async function downloadImage({ url = manifestUrl(), log = console.log } = {}) {
  const final = imageDir();
  const staging = `${final}.incoming`;

  const response = await fetch(url, { redirect: 'follow' });
  if (!response.ok) {
    const error = new Error(`no image manifest at ${url} (HTTP ${response.status})`);
    error.code = 'NO_MANIFEST';
    throw error;
  }
  const manifest = await response.json();

  log(`  fetching guest image ${manifest.version ?? ''}`.trimEnd());

  rmSync(staging, { recursive: true, force: true });
  mkdirSync(staging, { recursive: true, mode: 0o700 });

  try {
    for (const artifact of manifest.artifacts) {
      await fetchArtifact(artifact, staging, url);
    }
    // Record what was installed. Without it there is no way to tell a cached
    // image apart from the current one, and a cached image is never fetched
    // again — so a fix to the guest would reach new installations only.
    writeFileSync(join(staging, INSTALLED), JSON.stringify(manifest, null, 2) + '\n');
    rmSync(final, { recursive: true, force: true });
    renameSync(staging, final);
  } catch (error) {
    rmSync(staging, { recursive: true, force: true });
    throw error;
  }

  log('  guest image ready');
  return final;
}
