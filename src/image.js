import { createHash } from 'node:crypto';
import { createWriteStream, existsSync, mkdirSync, renameSync, rmSync, statSync, truncateSync } from 'node:fs';
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
const DEFAULT_MANIFEST = 'https://github.com/obaid/hyperwake-core/releases/download/image-latest/manifest.json';

export function manifestUrl() {
  return process.env.HYPERWAKE_IMAGE_URL || DEFAULT_MANIFEST;
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
 * Fetch one artifact, verifying as the bytes arrive.
 *
 * The digest is computed on the compressed stream, which is what the manifest
 * records, so a truncated or tampered download fails before anything is moved
 * into place rather than after.
 */
async function fetchArtifact(artifact, into) {
  const response = await fetch(artifact.url, { redirect: 'follow' });
  if (!response.ok) throw new Error(`${artifact.name}: HTTP ${response.status}`);

  const total = Number(response.headers.get('content-length')) || artifact.bytes || 0;
  const digest = createHash('sha256');
  let seen = 0;

  const counted = Readable.fromWeb(response.body).map((chunk) => {
    digest.update(chunk);
    seen += chunk.length;
    progress(artifact.name, seen, total);
    return chunk;
  });

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
      await fetchArtifact(artifact, staging);
    }
    rmSync(final, { recursive: true, force: true });
    renameSync(staging, final);
  } catch (error) {
    rmSync(staging, { recursive: true, force: true });
    throw error;
  }

  log('  guest image ready');
  return final;
}
