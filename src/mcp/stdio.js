/**
 * JSON-RPC 2.0 over stdin and stdout, which is all MCP's stdio transport is.
 *
 * Frames are newline delimited JSON. One object per line, no length prefix, no
 * batching. stdin arrives in arbitrary chunks that split lines wherever they
 * like, so we buffer and only act on complete lines.
 *
 * The one rule that matters: stdout carries the protocol and nothing else. A
 * stray console.log corrupts the stream and the failure looks like the client
 * hanging, with no error anywhere. Everything diagnostic goes to stderr.
 */

export const log = (...parts) => process.stderr.write(`${parts.join(' ')}\n`);

// JSON-RPC reserves -32768 to -32000. These are the three we can actually cause.
export const PARSE_ERROR = -32700;
export const METHOD_NOT_FOUND = -32601;
export const INVALID_PARAMS = -32602;
export const INTERNAL_ERROR = -32603;

/**
 * An error that should reach the client as a protocol-level failure rather than
 * as a tool result. Unknown tool, malformed arguments: things the model cannot
 * fix by trying again with different inputs.
 */
export class RpcError extends Error {
  constructor(code, message) {
    super(message);
    this.code = code;
  }
}

/**
 * Read newline-delimited JSON from a stream, yielding parsed values.
 *
 * Lines that do not parse are reported and skipped rather than thrown, because
 * one bad frame should not take down a session that is otherwise working.
 */
export async function* frames(input) {
  let buffer = '';
  for await (const chunk of input) {
    buffer += chunk;
    let cut;
    while ((cut = buffer.indexOf('\n')) !== -1) {
      const line = buffer.slice(0, cut).trim();
      buffer = buffer.slice(cut + 1);
      if (!line) continue;
      try {
        yield JSON.parse(line);
      } catch {
        yield { parseError: true, line };
      }
    }
  }
}

/**
 * Serve a set of methods over one stdio pair.
 *
 * Requests carry an id and expect a reply. Notifications have no id and must not
 * be answered, which is why the id is checked before anything is written: a
 * reply to a notification is a protocol violation that some clients treat as
 * fatal.
 */
export async function serve({ input = process.stdin, output = process.stdout, methods }) {
  const send = (message) => output.write(`${JSON.stringify(message)}\n`);

  const reply = (id, result) => send({ jsonrpc: '2.0', id, result });
  const fail = (id, code, message) => send({ jsonrpc: '2.0', id, error: { code, message } });

  for await (const message of frames(input)) {
    if (message.parseError) {
      fail(null, PARSE_ERROR, 'Could not parse that as JSON.');
      continue;
    }

    const { id, method, params } = message;
    const isNotification = id === undefined || id === null;

    const handler = methods[method];
    if (!handler) {
      // Unknown notifications are ignored on purpose. Clients send several we do
      // not implement, and answering them is worse than staying quiet.
      if (!isNotification) fail(id, METHOD_NOT_FOUND, `No such method: ${method}`);
      continue;
    }

    try {
      const result = await handler(params ?? {});
      if (!isNotification) reply(id, result ?? {});
    } catch (error) {
      if (isNotification) {
        log(`error in notification ${method}:`, error.message);
        continue;
      }
      const code = error instanceof RpcError ? error.code : INTERNAL_ERROR;
      fail(id, code, error.message);
    }
  }
}
