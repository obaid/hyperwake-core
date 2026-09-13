import { test } from 'node:test';
import assert from 'node:assert/strict';
import http from 'node:http';
import net from 'node:net';
import { once } from 'node:events';
import { WebSocket } from 'ws';
import { mintTicket, revokeDesktop, attachDesktop } from '../src/desktop.js';

async function harness(t) {
  let connections = 0;
  const upstreamSockets = new Set();
  const upstream = net.createServer(socket => {
    connections++;
    upstreamSockets.add(socket);
    socket.on('close', () => upstreamSockets.delete(socket));
    socket.write('RFB 003.008\n');
  });
  upstream.listen(0, '127.0.0.1'); await once(upstream, 'listening');
  const server = http.createServer(); attachDesktop(server);
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  const clients = new Set();
  t.after(async () => {
    for (const client of clients) client.terminate();
    for (const socket of upstreamSockets) socket.destroy();
    await Promise.all([new Promise(resolve => server.close(resolve)), new Promise(resolve => upstream.close(resolve))]);
  });
  function client(ticket) {
    const ws = new WebSocket(`ws://127.0.0.1:${server.address().port}/desktop/socket?t=${ticket}`);
    clients.add(ws);
    ws.on('error', () => {});
    return ws;
  }
  async function refusal(ticket) {
    const ws = client(ticket);
    const result = await once(ws, 'unexpected-response');
    result[1].resume(); ws.terminate();
    return result[1].statusCode;
  }
  return { described: { display_host: '127.0.0.1', display_port: upstream.address().port }, client, refusal, get connections() { return connections; } };
}

test('a cloud ticket for an earlier boot is refused before dialing the reused display port', async t => {
  const h = await harness(t);
  let currentBoot = 'boot-one';
  const ticket = mintTicket('machine-one', h.described, { boot_id: currentBoot, generation: 1, validate: binding => binding.boot_id === currentBoot });
  currentBoot = 'boot-two';
  assert.equal(await h.refusal(ticket), 401);
  assert.equal(h.connections, 0);
});

test('mutation revokes a redeemed ticket while its runtime validation is still pending', async t => {
  const h = await harness(t);
  let release, validating;
  const entered = new Promise(resolve => { validating = resolve; });
  const ticket = mintTicket('machine-race', h.described, {
    boot_id: 'boot-one', generation: 1,
    validate: async () => { validating(); await new Promise(resolve => { release = resolve; }); return true; },
  });
  const refused = h.refusal(ticket);
  await entered;
  revokeDesktop('machine-race');
  release();
  assert.equal(await refused, 401);
  assert.equal(h.connections, 0);
});

test('revocation closes an established transport and local single-use tickets still work', async t => {
  const h = await harness(t);
  const ticket = mintTicket('machine-local', h.described);
  const ws = h.client(ticket);
  const [message] = await once(ws, 'message');
  assert.equal(message.toString(), 'RFB 003.008\n');
  assert.equal(h.connections, 1);
  const closed = once(ws, 'close');
  revokeDesktop('machine-local');
  await closed;
  assert.equal(await h.refusal(ticket), 401);
  assert.equal(h.connections, 1);
});
