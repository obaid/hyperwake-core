# Using Hyperwake from Claude Code, Claude Desktop and other MCP clients

Hyperwake ships an [MCP](https://modelcontextprotocol.io) server, so an agent you
already use can create a Linux computer and work on it. You write a config file
rather than any code.

## Setup

Start the engine and leave it running:

```sh
npx hyperwake
```

Then add the server to your client.

### Claude Code

```sh
claude mcp add hyperwake -- npx -y hyperwake mcp
```

### Claude Desktop

Edit `~/Library/Application Support/Claude/claude_desktop_config.json` on macOS,
or `%APPDATA%\Claude\claude_desktop_config.json` on Windows:

```json
{
  "mcpServers": {
    "hyperwake": {
      "command": "npx",
      "args": ["-y", "hyperwake", "mcp"]
    }
  }
}
```

Restart the app afterwards.

### Cursor, Windsurf and others

Any client that speaks MCP over stdio takes the same command and arguments:
`npx -y hyperwake mcp`.

If your engine is not on the default port, or its state is somewhere else, pass
the environment through:

```json
{
  "mcpServers": {
    "hyperwake": {
      "command": "npx",
      "args": ["-y", "hyperwake", "mcp"],
      "env": { "HYPERWAKE_PORT": "4242", "HYPERWAKE_HOME": "/path/to/state" }
    }
  }
}
```

## Try it

Ask for something that needs a computer:

> Make me a Linux machine, install neovim on it, and show me the desktop.

The agent creates a machine, waits for it, installs in the background while
reporting progress, and hands you a link to watch the screen.

## What the agent can do

Seventeen tools, in four groups.

| | |
|---|---|
| Machines | `create_machine`, `list_machines`, `start_machine`, `stop_machine`, `delete_machine` |
| Shell | `run_command`, `start_task`, `check_task` |
| Files | `read_file`, `write_file` |
| Screen | `screenshot`, `click`, `move_mouse`, `scroll`, `type_text`, `press_key`, `open_desktop` |

Three of these deserve explanation.

**`run_command` is the one to prefer.** Anything a shell can do is faster and
more reliable through a command than by driving the desktop. The tool
descriptions tell the model this, and good models listen.

**`start_task` exists because `run_command` stops at 120 seconds.** Installs,
builds and large downloads go through `start_task`, which detaches the process
and returns a handle. `check_task` reports whether it is still running and
returns the end of its output. This is also how the agent gives you progress on
something slow rather than going quiet for four minutes.

**`screenshot` is how the agent sees.** It returns the screen as an image, which
MCP passes to the model. This is what makes a model that has never heard of
Hyperwake able to use a desktop: it looks, clicks, looks again.

`open_desktop` returns a link you open in a browser to watch and control the
machine yourself. It works once and expires after sixty seconds, so use it
promptly, and ask for another whenever you want one.

## Things worth knowing

**The engine has to be running.** If it is not, the tools say so rather than
failing quietly. Run `npx hyperwake` in a terminal and leave it there. The MCP
server does not start the engine itself, because a server that dies when you
close your editor should not own machines that outlive it.

**Machines are disposable and they cost memory.** Each one holds 4 GB of your
host by default. Ask the agent to delete machines when it is done, or run
`list_machines` yourself to see what is still around. Stopping a machine keeps
its disk; deleting removes it.

**The agent can run anything on the machine.** That is the point of it, and it
is why the machine is a throwaway VM rather than your laptop. Nothing in the
guest can reach your filesystem. Your MCP client will ask before running tools;
that is the moment to look at what it is about to do.

## When something goes wrong

| What you see | What it means |
|---|---|
| Tools listed, every call says the engine is not running | Start `npx hyperwake`, or the port does not match |
| The server does not appear at all | The client cannot run `npx`; try an absolute path to node |
| `No machine with that id` | It was deleted, or the agent invented an id. Ask it to list machines |
| A command returns nothing for a long time | It probably needed `start_task`; ask the agent to use that instead |

To see the traffic, run the server by hand. It logs to stderr and speaks
JSON-RPC on stdout:

```sh
echo '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' | npx hyperwake mcp
```
