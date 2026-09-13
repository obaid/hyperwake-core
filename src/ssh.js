import { randomBytes, createHash } from 'node:crypto';
import { connect } from 'node:net';

const tickets = new Map();
const active = new Set();
const hash = value => createHash('sha256').update(value).digest('hex');
const fail = (status, message) => { throw Object.assign(new Error(message), { status }); };

export async function handleSsh(api, id, body) {
  const record = api.record(id);
  if (record.cloud.fenced) fail(409, 'Machine is fenced.');
  if (Object.keys(body).some(key => key !== 'gateway_public_key')) fail(400, 'Unknown SSH field.');
  const key = typeof body.gateway_public_key === 'string' ? body.gateway_public_key.trim() : '';
  const match = /^ssh-ed25519 ([A-Za-z0-9+/]+={0,2})$/.exec(key);
  const blob = match ? Buffer.from(match[1], 'base64') : Buffer.alloc(0);
  if (blob.length !== 51 || blob.readUInt32BE(0) !== 11 || blob.subarray(4, 15).toString() !== 'ssh-ed25519' || blob.readUInt32BE(15) !== 32) fail(400, 'A valid Ed25519 gateway key is required.');
  const described = await api.runtime.describe(id);
  const binding = { generation: record.cloud.generation, boot_id: record.boot_id };
  // Caller already holds the lifecycle lock: do not recursively acquire it.
  if (described.status !== 'running' || !record.boot_id || record.desired_state !== 'running') fail(409, 'Machine is not ready.');
  const previous = record.cloud.ssh_gateway_key;
  const keys = [...new Set([api.publicKey, ...(record.authorized_keys || []).filter(value => value !== previous), key])];
  record.cloud.ssh_gateway_key = key;
  record.authorized_keys = keys;
  api.registry.flush();
  // Deliver immediately using the engine's existing private guest connection.
  // The heartbeat continues to reconcile the same authoritative set afterward.
  const encoded = Buffer.from(`${keys.join('\n')}\n`).toString('base64');
  const result = await api.action({ id, ...described }, { action: 'exec', command: `umask 077; mkdir -p "$HOME/.ssh" && printf '%s' '${encoded}' | base64 -d > "$HOME/.ssh/authorized_keys.mola-new" && chmod 600 "$HOME/.ssh/authorized_keys.mola-new" && mv "$HOME/.ssh/authorized_keys.mola-new" "$HOME/.ssh/authorized_keys"` });
  if (result.exit_code !== 0) fail(502, 'SSH key delivery failed.');
  const ticket = mintSshTicket(id, described, { ...binding, validate: value => api.validateDesktop(id, value) });
  return { status: 201, body: { data: { ssh_ticket: ticket, expires_in: 60 } } };
}

export function mintSshTicket(id, described, binding) {
  const host = described.ssh_host === 'host.docker.internal' ? '127.0.0.1' : described.ssh_host;
  if (!['127.0.0.1', '::1', 'localhost'].includes(host) || !Number.isInteger(described.ssh_port) || described.ssh_port < 1 || described.ssh_port > 65535) fail(409, 'SSH requires a private host-loopback target.');
  for (const [key, entry] of tickets) if (entry.expires <= Date.now()) tickets.delete(key);
  const ticket = randomBytes(32).toString('base64url');
  tickets.set(hash(ticket), { id, host, port: described.ssh_port, binding, expires: Date.now() + 60_000, revoked: false });
  return ticket;
}

export function revokeSsh(id) {
  for (const [key, entry] of tickets) if (entry.id === id) { entry.revoked = true; tickets.delete(key); }
  for (const entry of active) if (entry.id === id) { entry.revoked = true; entry.client.destroy(); entry.upstream?.destroy(); }
}

/** Single-use, private HTTP CONNECT tunnel. The target is never client supplied. */
export function attachSsh(server) {
  server.on('connect', async (request, socket, head) => {
    const match = /^\/internal\/ssh\/([A-Za-z0-9_-]{43})$/.exec(request.url || '');
    const key = match ? hash(match[1]) : '';
    const target = tickets.get(key);
    tickets.delete(key);
    if (!target || request.headers.origin || target.expires <= Date.now()) {
      socket.end('HTTP/1.1 404 Not Found\r\nConnection: close\r\n\r\n'); return;
    }
    target.client = socket;
    active.add(target);
    socket.on('error', () => target.upstream?.destroy());
    socket.on('close', () => { active.delete(target); target.upstream?.destroy(); });
    let valid = false;
    try { valid = await target.binding.validate(target.binding); } catch { /* deny uncertainty */ }
    if (!valid || target.revoked || socket.destroyed || target.expires <= Date.now()) { socket.end('HTTP/1.1 404 Not Found\r\nConnection: close\r\n\r\n'); return; }
    const upstream = connect(target.port, target.host);
    target.upstream = upstream;
    upstream.setTimeout(10_000, () => upstream.destroy());
    upstream.on('error', () => socket.destroy());
    upstream.on('close', () => socket.destroy());
    upstream.once('connect', () => {
      upstream.setTimeout(0);
      if (target.revoked || socket.destroyed) { upstream.destroy(); return; }
      socket.write('HTTP/1.1 200 Connection Established\r\n\r\n');
      if (head.length) upstream.write(head);
      socket.pipe(upstream); upstream.pipe(socket);
    });
  });
}
