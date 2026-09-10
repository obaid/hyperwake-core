import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

process.env.HYPERWAKE_HOME = mkdtempSync(join(tmpdir(), 'hyperwake-test-'));

const { presentStatus, validateSpec, validateAction } = await import('../src/api.js');
const { Registry } = await import('../src/state.js');
const { GuestService } = await import('../src/guest.js');

test('a running machine is not ready until the guest reports in', () => {
  assert.equal(presentStatus('running', {}), 'booting');
  assert.equal(presentStatus('running', { last_heartbeat_at: new Date().toISOString(), capabilities: { shell: true } }), 'ready');
});

test('a stale heartbeat does not keep a machine ready', () => {
  const old = new Date(Date.now() - 120_000).toISOString();
  assert.equal(presentStatus('running', { last_heartbeat_at: old, capabilities: { shell: true } }), 'booting');
});

test('unknown is reported, never flattened into stopped', () => {
  // A provider that cannot see a machine has not said the machine stopped.
  // Collapsing the two is how a platform loses one it is still running.
  assert.equal(presentStatus('unknown', {}), 'unknown');
  assert.equal(presentStatus('stopped', {}), 'stopped');
});

test('specs are bounded', () => {
  assert.deepEqual(validateSpec({}).vcpus, 4);
  assert.equal(validateSpec({ name: '  box  ' }).name, 'box');
  assert.throws(() => validateSpec({ vcpus: 99 }), /vcpus/);
  assert.throws(() => validateSpec({ memory_mb: 128 }), /memory_mb/);
  assert.throws(() => validateSpec({ disk_gb: 4 }), /disk_gb/);
});

test('only known actions are accepted', () => {
  assert.throws(() => validateAction({ action: 'rm -rf' }), /action must be one of/);
  assert.throws(() => validateAction({ action: 'exec' }), /command/);
  assert.throws(() => validateAction({ action: 'write_file', path: '~/a' }), /content/);
  assert.doesNotThrow(() => validateAction({ action: 'exec', command: 'ls' }));
  assert.doesNotThrow(() => validateAction({ action: 'screenshot' }));
});

test('a registration token cannot be redeemed twice', () => {
  const registry = new Registry(join(process.env.HYPERWAKE_HOME, 'a.json'));
  const guests = new GuestService(registry);
  const record = registry.create({ name: 'x', vcpus: 1, memory_mb: 1024, disk_gb: 16 });

  const first = guests.register({ registration_token: record.registration_token });
  assert.equal(first.status, 200);
  assert.ok(first.body.machine_token);

  // A replayed token must not mint a second credential.
  const replay = guests.register({ registration_token: record.registration_token });
  assert.equal(replay.status, 401);
});

test('an enrolment key lets a lost response be recovered', () => {
  const registry = new Registry(join(process.env.HYPERWAKE_HOME, 'b.json'));
  const guests = new GuestService(registry);
  const record = registry.create({ name: 'x', vcpus: 1, memory_mb: 1024, disk_gb: 16 });

  guests.register({ registration_token: record.registration_token, enrollment_public_key: 'k' });
  const recovered = guests.register({ registration_token: record.registration_token, enrollment_public_key: 'k' });
  assert.equal(recovered.status, 200);
  assert.equal(recovered.body.recovered, true);
});

test('an unknown registration token is refused', () => {
  const registry = new Registry(join(process.env.HYPERWAKE_HOME, 'c.json'));
  const guests = new GuestService(registry);
  assert.equal(guests.register({ registration_token: 'nope' }).status, 401);
  assert.equal(guests.register({}).status, 401);
});

test('capabilities are recorded as a claim, and the challenge rotates', () => {
  const registry = new Registry(join(process.env.HYPERWAKE_HOME, 'd.json'));
  const guests = new GuestService(registry);
  const record = registry.create({ name: 'x', vcpus: 1, memory_mb: 1024, disk_gb: 16 });
  guests.register({ registration_token: record.registration_token });

  const first = guests.heartbeat(registry.get(record.id), { capabilities: { shell: true }, boot_id: 'b1' });
  assert.equal(first.body.challenge_verified, false, 'nothing to verify on the first beat');
  assert.ok(first.body.challenge);

  const second = guests.heartbeat(registry.get(record.id), { challenge_response: first.body.challenge });
  assert.equal(second.body.challenge_verified, true);
  assert.notEqual(second.body.challenge, first.body.challenge, 'the nonce must not repeat');
});

test('the registry survives a reload', () => {
  const file = join(process.env.HYPERWAKE_HOME, 'e.json');
  const first = new Registry(file);
  const record = first.create({ name: 'keeper', vcpus: 2, memory_mb: 2048, disk_gb: 20 });

  const reopened = new Registry(file);
  assert.equal(reopened.get(record.id).name, 'keeper');

  reopened.remove(record.id);
  assert.equal(new Registry(file).get(record.id), null);
});
