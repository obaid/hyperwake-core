# ADR-MCP-001 — An MCP server, hand written, in the engine

**Date:** 2026-09-11
**Status:** Accepted, building

## The question that produced this

While planning [hyperwake-agent](ADR-AGENT-001-hyperwake-agent.md), the obvious
question came up: should the engine expose MCP so the agent app can consume it?

**No.** The agent app's server already calls the REST API in process. Putting MCP
between two components we both own turns

```
agent loop → HTTP → engine
```

into

```
agent loop → MCP client → spawn a server process → HTTP → engine
```

and pays for it with a process and a hop. It also loses the things the app
depends on: `runtimeContext` carrying the machine id into every tool, screenshot
downscaling with coordinates scaled back on the way out, and typed results the UI
renders as cards rather than opaque JSON.

**But MCP is worth building for a different reason**, so the answer to the
question as asked is no and the answer to the better question is yes.

## Why build it at all

Every MCP client becomes a Hyperwake client: Claude Code, Claude Desktop, Cursor,
Windsurf. Not by writing code, by writing config.

```json
{ "mcpServers": { "hyperwake": { "command": "npx", "args": ["-y", "hyperwake", "mcp"] } } }
```

Then a person says "make me a Linux desktop and install neovim" inside a tool they
already use, and watches it happen. That is distribution, and it reaches people
who will never install a demo app.

Two things make the fit unusually clean:

**The screenshot action is already MCP image content.** The engine returns
`mime_type` and `image_base64`. MCP wants `{"type": "image", "data", "mimeType"}`.
It is a rename. So an agent in Claude Code genuinely sees the Omarchy screen, and
computer use arrives through a config file.

**Approval is already solved.** MCP clients confirm tool calls, and the
specification says they should. The approval flow hyperwake-agent has to build,
these clients have had all along.

## Why not the official SDK

Measured, not assumed:

| | Direct dependencies | Installed |
|---|---|---|
| `hyperwake` today | 2 | **984 KB** |
| `@modelcontextprotocol/sdk` 1.30.0 | 17 | **26 MB** |

The SDK's tree includes Express *and* Hono, plus `jose`, `ajv`, `cors` and
`pkce-challenge`. All of that serves HTTP transport and OAuth. A stdio server
touches none of it.

The engine holds a token that controls virtual machines on the operator's
computer. Adding an OAuth library and two web frameworks to that process, for a
feature many users will not switch on, is a bad trade on supply chain grounds
before the twenty-six-fold size increase is even considered.

A tools-only stdio server needs `initialize`, `tools/list` and `tools/call`,
carried as newline-delimited JSON-RPC 2.0 over stdin and stdout. That is a few
hundred lines and no new dependencies. The engine already hand writes its HTTP
server, so this is consistent rather than eccentric.

Revisit the SDK if we ever want HTTP transport to a remote engine. The
dependencies would then be paying for something.

## Shape

`npx hyperwake mcp` runs a stdio server. It is a **client of the REST API**, not a
second engine.

That distinction is load bearing. Two processes writing `~/.hyperwake/machines.json`
would corrupt it. The MCP server owns no state, supervises no QEMU, and holds no
lock. It reads the token, calls `http://127.0.0.1:4141`, and translates.

It also means the MCP server is a genuine test of whether the public API is
sufficient, which is worth something on its own.

### When the engine is not running

`initialize` still succeeds, so the client does not hard fail at startup and the
user can see the server listed. Individual tool calls return `isError: true` with
the sentence that fixes it: start the engine with `npx hyperwake`.

Auto-starting the engine was considered and rejected. A stdio server dies when its
client disconnects, so an engine started underneath it would either die with it,
killing the user's machines, or outlive it invisibly. Neither is a good surprise.

### stdout belongs to the protocol

Nothing may write to stdout except JSON-RPC frames. Every log, warning and error
goes to stderr. This is the single easiest way to break a stdio MCP server and it
breaks silently, so it is worth stating as a rule rather than discovering.

## Tools

The nine actions plus lifecycle, named for a model rather than for the API.

| Tool | Engine call |
|---|---|
| `create_machine` | `POST /v1/machines`, then poll until `ready` |
| `list_machines` | `GET /v1/machines` |
| `run_command` | `exec` |
| `start_task`, `check_task` | `exec`, with backgrounding, see below |
| `read_file`, `write_file` | the matching actions |
| `screenshot` | `screenshot`, returned as MCP image content |
| `click`, `move`, `scroll`, `type_text`, `press_key` | the matching actions |
| `open_desktop` | `POST /desktop`, returns a URL for the user to click |
| `stop_machine`, `delete_machine` | `POST /stop`, `DELETE` |

`type` and `key` are renamed to `type_text` and `press_key`, because a tool called
`type` in a list of tools reads like a category.

**Long commands.** `exec` caps at 120 seconds and the first `pacman -S` exceeds it.
`start_task` wraps the command with `nohup`, redirects to a log under `/tmp`, and
returns a handle. `check_task` reports whether the process is alive and returns
the tail of its log. Backgrounding lives here, so the engine keeps one simple
synchronous `exec`.

**The desktop is a link, not a panel.** MCP clients cannot embed a VNC viewer, so
`open_desktop` returns a URL. The ticket lives sixty seconds, so the tool
description has to say "open this now", and calling it again is cheap. The
side-by-side view stays exclusive to hyperwake-agent, which is a decent argument
for that app continuing to exist.

## What this changes about the agent app

MCP goes first. It is a few hundred lines against a whole Next.js application, and
it exercises the tool design against real models before any UI is built around
those same names and descriptions. Whatever we learn about what makes a model use
`run_command` instead of clicking around, the web app inherits.

The web app is still worth building. It owns the whole experience, it can show the
screen beside the conversation, and it is the thing you can record.

## Risks

- **Hand written protocol.** The spec moves. Mitigated by implementing only the
  tools subset, pinning the protocol version we answer with, and keeping the
  transport in one small file.
- **A tool list long enough to crowd a client's context.** Sixteen tools is on the
  high side. If it becomes a problem, the input actions collapse into one
  `control_screen` tool with an action argument.
- **People will point it at a machine and run anything.** That is the product. The
  README says so plainly.
