import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, rmSync, readFileSync, statSync, mkdirSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Readable } from 'node:stream';
import { pipeline } from 'node:stream/promises';
import { createZstdCompress } from 'node:zlib';

/**
 * A downloaded guest image must not store its own free space.
 *
 * The root filesystem is 16 GiB of which about 5 GiB is data. Written
 * literally, every installation gives up 11 GiB it never uses. Skipping the
 * zero-filled blocks leaves holes instead.
 *
 * This needs its own test because nothing else can catch a mistake in it: the
 * manifest's digest covers the compressed stream, so it is verified before
 * decompression and would not notice a decompressed file written wrongly.
 */

function scratch(t) {
  const dir = mkdtempSync(join(tmpdir(), 'mola-sparse-'));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  return dir;
}

/** Data, a long run of zeroes, more data, and a trailing hole. */
function mixed() {
  const parts = [
    Buffer.alloc(3 << 20, 0x41),
    Buffer.alloc(8 << 20, 0),
    Buffer.alloc(1 << 20, 0x42),
    // Deliberately not block-aligned, so the tail path is exercised.
    Buffer.alloc(777, 0x43),
    Buffer.alloc(4 << 20, 0),
  ];
  return Buffer.concat(parts);
}

async function fetchThrough(dir, body, { compress = false } = {}) {
  const { downloadImage } = await import(`../src/image.js?${Math.random()}`);
  const served = join(dir, 'served');
  mkdirSync(served, { recursive: true });

  let payload = body;
  if (compress) {
    const chunks = [];
    await pipeline(Readable.from([body]), createZstdCompress(), async function* (source) {
      for await (const chunk of source) chunks.push(chunk);
    });
    payload = Buffer.concat(chunks);
  }

  const manifest = {
    version: 'test',
    artifacts: [{
      name: 'root.ext4',
      url: 'root.ext4.zst',
      bytes: payload.length,
      ...(compress ? { compression: 'zstd' } : {}),
    }],
  };

  const realFetch = global.fetch;
  global.fetch = async (url) => {
    const name = String(url).split('/').pop();
    if (name === 'manifest.json') return new Response(JSON.stringify(manifest), { status: 200 });
    return new Response(payload, { status: 200 });
  };
  try {
    process.env.MOLA_HOME = dir;
    await downloadImage({ url: 'http://example.invalid/manifest.json', log: () => {} });
  } finally {
    global.fetch = realFetch;
    delete process.env.MOLA_HOME;
  }
  return join(dir, 'image', 'root.ext4');
}

test('the bytes written are exactly the bytes decompressed', async (t) => {
  const dir = scratch(t);
  const body = mixed();

  const written = readFileSync(await fetchThrough(dir, body, { compress: true }));

  assert.equal(written.length, body.length, 'length must survive the holes');
  assert.ok(written.equals(body), 'content must be byte-identical');
});

test('a filesystem\'s free space is not stored', async (t) => {
  const dir = scratch(t);
  // The shape of a real guest image: data at the front, then the free space
  // inside the filesystem, which is zeroes all the way to the end.
  //
  // 64 MiB rather than something smaller because APFS allocates small files
  // whole regardless of holes — the threshold is somewhere between 16 and
  // 32 MiB — so a tidier 16 MiB fixture would fail here and pass on Linux.
  const body = Buffer.concat([Buffer.alloc(8 << 20, 0x41), Buffer.alloc(56 << 20, 0)]);

  const path = await fetchThrough(dir, body, { compress: true });
  const { size, blocks } = statSync(path);
  const allocated = blocks * 512;

  assert.equal(size, body.length, 'the image is still its full length');
  assert.ok(allocated < size * 0.25,
    `allocated ${allocated} of ${size} bytes; the free space was written out`);
});

/**
 * How much is saved depends on the filesystem, and macOS is the weaker case.
 *
 * APFS creates a hole when a file is extended past its last byte, but not for a
 * gap between two writes: writing at 0 and again at 11 MiB allocates all 16.
 * ext4 punches the interior hole as well. So on macOS the saving is everything
 * after the last used block of the guest filesystem, which for a 16 GiB image
 * holding about 5 GiB is still the large majority of it.
 *
 * The property asserted here is the one that holds on both: whatever the
 * filesystem does with the holes, the bytes read back are the bytes written.
 */
test('interior holes stay correct even where they cannot be sparse', async (t) => {
  const dir = scratch(t);
  const body = mixed();

  const written = readFileSync(await fetchThrough(dir, body, { compress: true }));

  assert.ok(written.equals(body));
});

test('a body with no zeroes at all is written whole', async (t) => {
  const dir = scratch(t);
  const body = Buffer.alloc(2 << 20, 0x5a);

  const written = readFileSync(await fetchThrough(dir, body, { compress: true }));

  assert.ok(written.equals(body));
});

test('an entirely empty body still ends up the right length', async (t) => {
  const dir = scratch(t);
  const body = Buffer.alloc(5 << 20, 0);

  const path = await fetchThrough(dir, body, { compress: true });
  const { size, blocks } = statSync(path);

  assert.equal(size, body.length, 'a file that is all holes still has a length');
  assert.ok(blocks * 512 < size * 0.1, 'and should occupy almost nothing');
  assert.ok(readFileSync(path).equals(body));
});
