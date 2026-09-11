import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Readable, Writable } from 'node:stream';
import { serve, frames } from '../src/mcp/stdio.js';
import { createMcpServer } from '../src/mcp/index.js';
import { createEngine, waitForReady, EngineDown, EngineError } from '../src/mcp/engine.js';
import { shellQuote } from '../src/mcp/tools.js';

/** Drive the server over a fake stdio pair and collect what it writes back. */
async function exchange(messages, options = {}) {
  const input = Readable.from(messages.map((m) => `${JSON.stringify(m)}\n`));
  const written = [];
  const output = new Writable({
    write(chunk, _enc, done) {
      written.push(chunk.toString());
      done();
    },
  });
  const { methods } = createMcpServer(options);
  await serve({ input, output, methods });
  return written.join('').trim().split('\n').filter(Boolean).map((l) => JSON.parse(l));
}

const hello = {
  jsonrpc: '2.0', id: 1, method: 'initialize',
  params: { protocolVersion: '2025-06-18', capabilities: {}, clientInfo: { name: 't', version: '1' } },
};

test('initialize succeeds even with no engine, so the server still appears', async () => {
  const engine = createEngine({ token: null });
  const [reply] = await exchange([hello], { engine });
  assert.equal(reply.result.serverInfo.name, 'hyperwake');
  assert.equal(reply.result.protocolVersion, '2025-06-18');
  assert.ok(reply.result.capabilities.tools);
});

test('notifications are never answered', async () => {
  const replies = await exchange([
    hello,
    { jsonrpc: '2.0', method: 'notifications/initialized' },
    { jsonrpc: '2.0', method: 'notifications/cancelled', params: { requestId: 1 } },
  ]);
  assert.equal(replies.length, 1, 'only initialize should produce a reply');
});

test('every tool declares a name, description and object schema', async () => {
  const [, listed] = await exchange([hello, { jsonrpc: '2.0', id: 2, method: 'tools/list' }]);
  const tools = listed.result.tools;
  assert.ok(tools.length >= 15);
  for (const tool of tools) {
    assert.match(tool.name, /^[a-z][a-z0-9_]*$/, `${tool.name} should be snake_case`);
    assert.ok(tool.description.length > 30, `${tool.name} needs a real description`);
    assert.equal(tool.inputSchema.type, 'object');
  }
  // The model has to be steered towards the shell, or it drives the GUI.
  const run = tools.find((t) => t.name === 'run_command');
  assert.match(run.description, /first/i);
});

test('an unknown tool is a protocol error, not a tool result', async () => {
  const [, reply] = await exchange([
    hello,
    { jsonrpc: '2.0', id: 2, method: 'tools/call', params: { name: 'nope', arguments: {} } },
  ]);
  assert.equal(reply.error.code, -32601);
});

test('a missing required argument is rejected before the engine is touched', async () => {
  let called = false;
  const engine = createEngine({ token: 't', fetchImpl: async () => { called = true; } });
  const [, reply] = await exchange([
    hello,
    { jsonrpc: '2.0', id: 2, method: 'tools/call', params: { name: 'run_command', arguments: { machine_id: 'm' } } },
  ], { engine });
  assert.equal(reply.error.code, -32602);
  assert.match(reply.error.message, /command/);
  assert.equal(called, false);
});

test('a down engine reaches the model as a readable tool error', async () => {
  const engine = createEngine({ token: null });
  const [, reply] = await exchange([
    hello,
    { jsonrpc: '2.0', id: 2, method: 'tools/call', params: { name: 'list_machines', arguments: {} } },
  ], { engine });
  assert.equal(reply.result.isError, true);
  assert.match(reply.result.content[0].text, /npx hyperwake/);
});

test('malformed input is reported without killing the session', async () => {
  const input = Readable.from(['not json\n', `${JSON.stringify(hello)}\n`]);
  const written = [];
  const output = new Writable({ write(c, _e, d) { written.push(c.toString()); d(); } });
  const { methods } = createMcpServer({ engine: createEngine({ token: null }) });
  await serve({ input, output, methods });
  const replies = written.join('').trim().split('\n').map((l) => JSON.parse(l));
  assert.equal(replies[0].error.code, -32700);
  assert.ok(replies[1].result.serverInfo, 'the session survives a bad frame');
});

test('a screenshot becomes MCP image content', async () => {
  const engine = createEngine({
    token: 't',
    fetchImpl: async () => new Response(
      JSON.stringify({ data: { mime_type: 'image/png', image_base64: 'AAAA' } }),
      { status: 200, headers: { 'content-type': 'application/json' } },
    ),
  });
  const [, reply] = await exchange([
    hello,
    { jsonrpc: '2.0', id: 2, method: 'tools/call', params: { name: 'screenshot', arguments: { machine_id: 'm' } } },
  ], { engine });
  assert.deepEqual(reply.result.content[0], { type: 'image', data: 'AAAA', mimeType: 'image/png' });
});

test('a command that times out says to use start_task instead', async () => {
  const engine = createEngine({
    token: 't',
    fetchImpl: async () => new Response(
      JSON.stringify({ data: { exit_code: 124, stdout: 'partial', stderr: '', timed_out: true } }),
      { status: 200 },
    ),
  });
  const [, reply] = await exchange([
    hello,
    {
      jsonrpc: '2.0', id: 2, method: 'tools/call',
      params: { name: 'run_command', arguments: { machine_id: 'm', command: 'sleep 999' } },
    },
  ], { engine });
  assert.equal(reply.result.isError, true);
  assert.match(reply.result.content[0].text, /start_task/);
});

test('shell quoting survives a command containing single quotes', () => {
  assert.equal(shellQuote("echo 'hi'"), `'echo '\\''hi'\\'''`);
});

test('frames splits on newlines regardless of how chunks arrive', async () => {
  const chunks = ['{"a":1}\n{"b', '":2}\n', '\n{"c":3}\n'];
  const seen = [];
  for await (const value of frames(Readable.from(chunks))) seen.push(value);
  assert.deepEqual(seen, [{ a: 1 }, { b: 2 }, { c: 3 }]);
});

test('waitForReady stops on a terminal status rather than polling to the deadline', async () => {
  let polls = 0;
  const engine = {
    getMachine: async () => {
      polls += 1;
      return { id: 'm', status: polls < 3 ? 'booting' : 'failed' };
    },
  };
  await assert.rejects(
    () => waitForReady(engine, 'm', { sleep: async () => {} }),
    (error) => error instanceof EngineError && /failed/.test(error.message),
  );
  assert.equal(polls, 3);
});

test('waitForReady returns as soon as the guest has checked in', async () => {
  let polls = 0;
  const engine = {
    getMachine: async () => ({ id: 'm', status: (polls += 1) < 2 ? 'booting' : 'ready' }),
  };
  const machine = await waitForReady(engine, 'm', { sleep: async () => {} });
  assert.equal(machine.status, 'ready');
});

test('a refused connection is EngineDown, not a generic failure', async () => {
  const engine = createEngine({
    token: 't',
    fetchImpl: async () => { throw Object.assign(new Error('connect ECONNREFUSED'), { name: 'TypeError' }); },
  });
  await assert.rejects(() => engine.listMachines(), (e) => e instanceof EngineDown);
});
