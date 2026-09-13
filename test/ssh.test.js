import test from 'node:test';
import assert from 'node:assert/strict';
import http from 'node:http';
import net from 'node:net';
import { once } from 'node:events';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { attachSsh, mintSshTicket, revokeSsh } from '../src/ssh.js';

async function setup(t) {
  const echo = net.createServer(socket => socket.pipe(socket));
  echo.listen(0, '127.0.0.1'); await once(echo, 'listening');
  const server = http.createServer(); attachSsh(server);
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  t.after(() => { revokeSsh('computer'); echo.close(); server.close(); });
  return { echo, server, ticket: validate => mintSshTicket('computer', { ssh_host: '127.0.0.1', ssh_port: echo.address().port }, { validate }) };
}
async function dial(server, ticket) {
  const socket = net.connect(server.address().port, '127.0.0.1');
  socket.write(`CONNECT /internal/ssh/${ticket} HTTP/1.1\r\nHost: localhost\r\n\r\n`);
  return { socket, response: (await once(socket, 'data'))[0].toString() };
}
test('SSH CONNECT ticket is one-use, streams bytes and is revoked on lifecycle change', async t => {
  const h = await setup(t); const ticket = h.ticket(async () => true);
  const first = await dial(h.server, ticket); assert.match(first.response, /200 Connection/);
  first.socket.write('hello'); assert.equal((await once(first.socket, 'data'))[0].toString(), 'hello');
  const replay = await dial(h.server, ticket); assert.match(replay.response, /404/); replay.socket.destroy();
  const closed = once(first.socket, 'close'); revokeSsh('computer'); await closed;
});
test('SSH CONNECT refuses stale generation before dialing', async t => {
  const h = await setup(t); const result = await dial(h.server,h.ticket(async () => false));
  assert.match(result.response,/404/); result.socket.destroy();
});
test('SSH refuses arbitrary target hosts', () => {
  assert.throws(() => mintSshTicket('computer', {ssh_host:'169.254.169.254',ssh_port:80},{}), /loopback/);
});

test('SSH delivery preserves engine and customer keys while rotating the gateway key', async () => {
  const { handleSsh } = await import('../src/ssh.js');
  const key = `ssh-ed25519 ${Buffer.concat([Buffer.from('0000000b7373682d6564323535313900000020','hex'),Buffer.alloc(32,7)]).toString('base64')}`;
  const record = { cloud: {generation:2,ssh_gateway_key:'old-gateway'}, authorized_keys:['engine-key','customer-key','old-gateway'], boot_id:'current', desired_state:'running' };
  let delivered;
  const api = { record: () => record, publicKey:'engine-key', registry:{flush(){}}, runtime:{describe:async()=>({status:'running',ssh_host:'127.0.0.1',ssh_port:22})}, action:async(target,action)=>{ delivered=action; return {exit_code:0}; }, validateDesktop:async()=>true };
  const result = await handleSsh(api,'computer',{gateway_public_key:key});
  assert.equal(result.status,201); assert.match(result.body.data.ssh_ticket,/^[A-Za-z0-9_-]{43}$/);
  assert.deepEqual(record.authorized_keys,['engine-key','customer-key',key]);
  assert.equal(delivered.action, 'exec');
  assert.equal(delivered.type, undefined);
  // Exercise the real Python transport dispatcher, mocking only its network
  // subprocess so a mismatched JS/Python action discriminator cannot pass.
  const python = spawnSync('python3', ['-c', `
import runpy,sys,json,types
from unittest.mock import patch
module=runpy.run_path(sys.argv[1])
action=json.load(sys.stdin)
target=dict(id='computer',ssh_host='127.0.0.1',ssh_port=22,known_hosts='/unused',ssh_key='/unused')
with patch('subprocess.run',return_value=types.SimpleNamespace(returncode=0,stdout='{"exit_code":0}')) as transport:
    assert module['run'](dict(target=target,action=action))['exit_code']==0
    assert transport.call_count==1
`, fileURLToPath(new URL('../runtime/automation.py', import.meta.url))], { input: JSON.stringify(delivered), encoding: 'utf8' });
  assert.equal(python.status, 0, python.stderr);
  assert.ok(delivered.command.includes(Buffer.from(`engine-key\ncustomer-key\n${key}\n`).toString('base64')));
  revokeSsh('computer');
});
test('SSH delivery fails closed for fenced machines, malformed keys and failed delivery', async () => {
  const { handleSsh } = await import('../src/ssh.js');
  await assert.rejects(handleSsh({record:()=>({cloud:{fenced:true}})},'computer',{}),/fenced/);
  await assert.rejects(handleSsh({record:()=>({cloud:{}})},'computer',{gateway_public_key:'ssh-ed25519 invalid'}),/valid Ed25519/);
  const key = `ssh-ed25519 ${Buffer.concat([Buffer.from('0000000b7373682d6564323535313900000020','hex'),Buffer.alloc(32,7)]).toString('base64')}`;
  const api = { record:()=>({cloud:{generation:2},boot_id:'boot',desired_state:'running'}),publicKey:'engine-key',registry:{flush(){}},runtime:{describe:async()=>({status:'running',ssh_host:'127.0.0.1',ssh_port:22})},action:async()=>({exit_code:1}) };
  await assert.rejects(handleSsh(api,'computer',{gateway_public_key:key}),/delivery failed/);
});
