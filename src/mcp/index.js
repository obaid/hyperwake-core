import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { serve, log, RpcError, METHOD_NOT_FOUND, INVALID_PARAMS } from './stdio.js';
import { createEngine, EngineDown, EngineError } from './engine.js';
import { buildTools } from './tools.js';
import { packageRoot } from '../paths.js';

/**
 * The MCP server: three methods, a table of tools, and careful error handling.
 *
 * The protocol version is pinned rather than echoed back. Claiming to speak
 * whatever the client asked for is how you end up half implementing a revision
 * you have never read.
 */
const PROTOCOL_VERSION = '2025-06-18';

function version() {
  try {
    return JSON.parse(readFileSync(join(packageRoot, 'package.json'), 'utf8')).version;
  } catch {
    return '0.0.0';
  }
}

/**
 * Turn a thrown error into something the model can act on.
 *
 * The distinction that matters: a tool *result* with `isError` goes back to the
 * model, which can read it and try something else. A protocol error goes to the
 * client and usually surfaces as a broken tool. Anything the model could
 * plausibly recover from belongs in the first category, including the engine
 * being down, because the model can relay that to the person.
 */
function asToolError(error) {
  if (error instanceof EngineDown) {
    return {
      content: [{
        type: 'text',
        text:
          `${error.message}\n\n`
          + 'The Hyperwake engine is not running. Ask the person to run `npx hyperwake` '
          + 'in a terminal and leave it running, then try again.',
      }],
      isError: true,
    };
  }
  if (error instanceof EngineError) {
    const hint = error.status === 409
      ? ' The machine is not in a state that allows this; check list_machines.'
      : error.status === 404
        ? ' No machine with that id. Check list_machines, or create one.'
        : '';
    return { content: [{ type: 'text', text: `${error.message}${hint}` }], isError: true };
  }
  return { content: [{ type: 'text', text: error.message }], isError: true };
}

export function createMcpServer({ engine = createEngine() } = {}) {
  const tools = buildTools(engine);
  const byName = new Map(tools.map((t) => [t.spec.name, t]));

  return {
    tools,
    methods: {
      initialize() {
        // Succeeds even with no engine running, so the server appears in the
        // client's list and its tools are discoverable. A failure here reads as
        // "this server is broken" rather than "start the engine".
        return {
          protocolVersion: PROTOCOL_VERSION,
          capabilities: { tools: { listChanged: false } },
          serverInfo: { name: 'hyperwake', version: version() },
          instructions:
            'Hyperwake gives you throwaway Linux computers. Each one is a full Arch Linux '
            + 'desktop running Hyprland in its own virtual machine.\n\n'
            + 'Start with create_machine, then reuse its id. Prefer run_command for anything '
            + 'a shell can do, because it is faster and more reliable than driving the screen. '
            + 'Use start_task for installs and builds, which exceed the command timeout. '
            + 'Reach for screenshot and click only for genuinely graphical work.\n\n'
            + 'Machines are disposable and nothing carries between them. Delete one when the '
            + 'work is done, because each running machine holds several GB of the host.',
        };
      },

      'notifications/initialized': () => undefined,
      ping: () => ({}),

      'tools/list': () => ({
        tools: tools.map((t) => t.spec),
      }),

      async 'tools/call'(params) {
        const { name, arguments: args } = params;
        const tool = byName.get(name);
        if (!tool) throw new RpcError(METHOD_NOT_FOUND, `No such tool: ${name}`);

        for (const required of tool.spec.inputSchema.required ?? []) {
          if (args?.[required] === undefined) {
            throw new RpcError(INVALID_PARAMS, `${name} needs "${required}".`);
          }
        }

        try {
          return await tool.run(args ?? {});
        } catch (error) {
          log(`tool ${name} failed:`, error.message);
          return asToolError(error);
        }
      },
    },
  };
}

export async function runMcpServer(options = {}) {
  const { methods, tools } = createMcpServer(options);
  log(`hyperwake mcp: ${tools.length} tools, protocol ${PROTOCOL_VERSION}`);
  await serve({ methods, ...options });
}
