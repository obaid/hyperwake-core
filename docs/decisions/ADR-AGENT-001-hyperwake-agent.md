# ADR-AGENT-001 — hyperwake-agent, a chat app that drives a machine

**Date:** 2026-09-11
**Status:** Proposed, not started
**Home:** this file moves to `obaid/hyperwake-agent` once that repository exists

## What we are building

One command starts a local web app that shows an agent using an Omarchy
computer:

```sh
npx hyperwake          # the engine, already shipped
npx hyperwake-agent    # this
```

The app opens a browser. A short setup asks which model provider to use and
which model. After that it looks like a chat app: threads on the left,
conversation in the middle, and a button that slides the live desktop in on the
right. You watch the agent work, take the mouse whenever you want, and hand it
back.

The point is not to build a chat app. Hyperwake is an API, and an API demos
badly. This is the artifact that shows what the API is for.

## Why the agent runs in the app, not inside the machine

[ADR-ENGINE-001](ADR-ENGINE-001-agents.md) works through a different question:
installing Codex or pi *inside* a guest and giving it a key through a gateway.
That is a real design and it stays on the table for the hosted product.

This is the opposite arrangement. The agent loop runs in the app's own Node
process. The machine is a tool it calls, the same way a coding agent calls a
shell. Three reasons to prefer it here:

- **Nothing to install in the guest.** A machine reaches `ready` in about seven
  seconds and the agent can use it immediately. The in-guest route spends one to
  three minutes installing a runtime first.
- **The key never leaves your laptop.** No gateway, no proxy, no credential
  inside a machine that a hostile tenant is root in. ADR-ENGINE-001's Decision 1
  is a problem that this shape does not have.
- **Vision.** The agent sees the screen through `screenshot` and acts through
  `click` and `type`. An agent living inside the guest sees a terminal.

The two designs also disagree about who the customer is. In-guest agents suit
someone who wants a machine that thinks for itself. This suits someone who wants
to watch an agent use a computer, which is the thing people do not currently
believe works.

## Measured constraints

Checked against the running engine and the published API, not assumed.

| | |
|---|---|
| Engine sends no CORS headers | the browser cannot call the engine; every call goes through the app's server |
| Desktop viewer sends no `X-Frame-Options` or `frame-ancestors` | it can be embedded in an iframe from another origin |
| Desktop ticket is single use, 60 second TTL, redeemed when the socket connects | mint when the panel opens, not when a tool runs |
| `exec` caps at 120 seconds | `pacman -S` and `npm install` exceed it |
| Screen is 1280x800 | above the ~1024 px width vision models are tuned for |
| Default machine is 4 vCPU, 4 GB, 40 GB | four idle threads is 16 GB of a 16 GB Mac |
| Nine actions exist: `exec`, `read_file`, `write_file`, `screenshot`, `click`, `move`, `scroll`, `type`, `key` | enough; no engine change needed |

**The app needs no changes to hyperwake-core.** Create, stop, delete, the nine
actions and the desktop URL are all already public API. That is worth protecting
as the project goes on, because it keeps the engine small and it proves the API
is good enough for someone else to build on.

## Architecture

```
browser  ──────►  hyperwake-agent server  ──────►  engine :4141  ──►  QEMU
  chat UI          agent loop, tool calls           REST API
  iframe ─────────────────────────────────────────► /desktop (direct)
```

The server exists for three reasons, and each is load bearing:

1. The model provider key must never reach the browser.
2. The engine has no CORS, so the browser cannot reach it anyway.
3. An agent run takes minutes. It has to survive a tab reload.

The iframe is the one thing that talks to the engine directly, because it loads
a document rather than making a fetch, so CORS does not apply.

### Framework

**Next.js, App Router, `output: 'standalone'`.** The AI SDK's streaming and tool
calling are least surprising there, and this is code people are meant to read and
fork. The 1.1 GB guest image makes any argument about tarball size irrelevant.

The risk is packaging, not building: `output: 'standalone'` expects `.next/static`
and `public` copied next to it, and shipping a pruned `node_modules` inside an npm
tarball is not a well trodden path. **Milestone 0 is a packaging spike**, before
any feature work. If it fights back, Vite plus a small Hono server is the
fallback and nothing else in this plan changes, because the AI SDK core does not
care what framework is around it.

### Dependencies, at versions that exist today

| Package | Version | Note |
|---|---|---|
| `ai` | 7.0.97 | `ToolLoopAgent`, tools, streaming, tool approvals |
| `@ai-sdk/anthropic` | 4.0.52 | |
| `@ai-sdk/openai` | 4.0.65 | |
| `@openrouter/ai-sdk-provider` | 3.0.0 | **not** `@ai-sdk/openrouter`, which does not exist |

## The agent loop

`ToolLoopAgent` from `ai` runs the loop. It is worth being specific about what
this removes from the plan, because it is most of a milestone:

```ts
import { ToolLoopAgent, isStepCount } from 'ai';

const agent = new ToolLoopAgent({
  model,
  instructions: systemPrompt,
  tools,
  stopWhen: isStepCount(150),
  toolApproval: { delete_machine: 'user-approval' },
  onToolExecutionStart: ({ toolCall }) => push(threadId, toolCall),
  onToolExecutionEnd:   ({ result })  => push(threadId, result),
});
```

| What it gives us | What we would have written |
|---|---|
| `toolApproval` with per-tool and function forms | the whole approval mechanism |
| `state: 'approval-requested'` in `useChat`, plus `addToolApprovalResponse` | the approval UI and its round trip |
| `experimental_toolApprovalSecret` | HMAC binding so the browser cannot forge an approval |
| `onToolExecutionStart` / `onToolExecutionEnd` and the other lifecycle hooks | the event stream that draws tool cards and opens the desktop panel |
| `createAgentUIStreamResponse`, `InferAgentUIMessage` | typed streaming from route to component |
| `runtimeContext` | passing the thread's machine id into every tool |
| `prepareStep` | per-step model or setting changes |

**`stopWhen` needs attention.** The default is `isStepCount(20)`. Driving a
desktop costs a step per screenshot and a step per click, so twenty steps is
about four useful actions and the agent will stop mid-task looking broken. Set it
far higher, and put a real budget next to it: a step cap, a token cap accumulated
in `onStepEnd`, and a stop button in the UI. An agent that runs away is a worse
failure than one that stops early, so the cap has to exist; it just has to be
chosen for this workload rather than inherited.

**We do not use `experimental_sandbox`.** The SDK offers a sandbox for running
code the model writes. Our sandbox is the Omarchy machine, which is the entire
point of the project. Mentioning it here so nobody adds a second one later.

## Setup

Three steps, and the first one is not what it looks like.

**1. The engine.** Do not ask the user to paste a token. The server reads
`~/.hyperwake/token` itself and calls `GET /v1` to confirm the engine is up. The
step becomes a verification: a green check, the host's platform and accelerator
from the same response, and the number of machines already running. If the engine
is not answering, say so and print `npx hyperwake`. A paste box stays available
for a non-default `HYPERWAKE_HOME` or a remote engine.

**2. Provider.** Anthropic, OpenAI, or OpenRouter. The key is posted once to the
server, written to `~/.hyperwake-agent/config.json` at mode 0600, and never sent
back to the browser. The UI shows the last four characters afterwards.

**3. Model.** Fetched live with the user's key rather than hardcoded, because a
hardcoded list is wrong within a month.

| Provider | Endpoint | Capability filtering |
|---|---|---|
| OpenRouter | `GET /api/v1/models` | real: keep models whose `supported_parameters` contains `tools` and whose `architecture.input_modalities` contains `image` |
| Anthropic | `GET /v1/models` | none exposed; use a small allowlist |
| OpenAI | `GET /v1/models` | none exposed; use a small allowlist |

This filtering is not a nicety. OpenRouter lists 443 models and most of them
cannot call a tool or look at a screenshot, so an unfiltered list is a menu of
ways for the demo to fail.

## The tools

One set of `tool()` definitions, every provider, one code path. Anthropic's
native `computer_use` tool is better at coordinates and worth adopting later, but
taking it now means maintaining two agent loops from day one.

| Tool | Engine call |
|---|---|
| `create_machine` | `POST /v1/machines`, then poll `GET /v1/machines/{id}` until `ready` |
| `run_command` | `exec` |
| `start_task` / `check_task` | `exec` with backgrounding, see below |
| `read_file`, `write_file` | the matching actions |
| `screenshot` | `screenshot`, downscaled, returned to the model as an image |
| `click`, `move`, `scroll`, `type`, `key` | the matching actions |
| `open_desktop` | tells the UI to open the panel; the URL is minted there |
| `stop_machine`, `delete_machine` | `POST /stop`, `DELETE` |

### Two details that decide whether this works

**Long commands.** The 120 second cap kills the first interesting demo, because
installing anything takes longer. `start_task` wraps the command in `nohup`,
redirects to a log under `/tmp`, and returns a handle. `check_task` tails the log
and reports whether the process is still alive. Backgrounding lives in the app,
so the engine keeps its simple synchronous `exec` and nothing new has to be
designed into the API.

**Screenshot scaling.** Send 1280x800 to a vision model every turn and you pay
for pixels the model was not tuned for. Downscale to 1024 wide before sending,
then scale coordinates in the model's replies back up by the same factor before
they reach `click`. Get the second half wrong and every click lands about 20% off,
which looks like a broken agent rather than a broken conversion.

## Human in the loop

Three mechanisms, in increasing order of how much control the person takes.

**Ask.** An `ask_human` tool. The run pauses, the question renders in the chat as
a prompt, and the answer resumes the run. This is for the agent's own
uncertainty: which of these two files, what should I call it.

**Take over.** The user opens the desktop panel and clicks "take control". The
agent pauses after its current tool call. The desktop is already interactive, so
there is nothing to switch on. When they click "hand back", the app takes a fresh
screenshot and injects it with a note saying the human used the machine, so the
model does not act on a stale picture of the screen.

**Approve.** This is `toolApproval`, so there is little to build:

```ts
toolApproval: {
  delete_machine: 'user-approval',
  run_command: askEveryCommand ? 'user-approval' : undefined,
}
```

The request arrives in `useChat` as a tool part with
`state: 'approval-requested'`, and the answer goes back through
`addToolApprovalResponse({ id, approved })`. Set
`experimental_toolApprovalSecret` as well: it HMAC-signs each request against the
tool name, call id and arguments, so a tampered-with browser cannot approve a
call the server never offered. The secret is one `openssl rand -base64 32`
written to the config file on first run.

`delete_machine` always asks. `run_command` does not, because the whole premise
is a disposable machine and a confirmation on every command makes the demo
unwatchable. One setting flips it. Matching command text against
dangerous-looking patterns is deliberately not in the plan: it is easy to fool,
it teaches false confidence, and the blast radius is a VM you can recreate in one
second.

The function form of `toolApproval` is there if a rule ever needs the arguments,
which keeps the door open without opening it now.

## Machines and memory

One machine per thread, created lazily when a tool first needs it. Four idle
threads at the 4 GB default is 16 GB, which is the whole machine on a base Mac.

So, as defaults rather than settings:

- Stop a thread's machine after 15 minutes with no activity. Disks persist across
  stop and start, so the thread resumes where it was.
- Start it again on the next message, which costs the seven seconds it always did.
- Warn on the create path when machines are already running, with the count.
- A machines view lists what the engine has, flags any not owned by a thread, and
  offers to delete them. Leaked VMs are the most likely way this app annoys
  someone.

## Storage

The AI SDK's memory API is about giving a model long-term recall, not about
storing threads, so it does not answer this. We still need our own store.

`~/.hyperwake-agent/`, with threads, messages, tool calls and the thread's machine
id. Two candidates:

- **`node:sqlite`.** Built into Node 22, no native build, verified working on this
  machine. It prints an experimental warning on every start, which the bin can
  silence, and Next may need it in `serverExternalPackages` so the bundler leaves
  it alone.
- **One JSON file per thread**, written atomically. Zero dependencies and obvious
  to anyone reading the repo.

**Recommendation: JSON files.** This is a reference implementation whose job is to
be read and copied. A single user's chat threads do not need a database, and the
absence of one is a feature when someone opens the repo to work out how it fits
together.

Runs are server side and stream to the UI over SSE. Closing the tab does not stop
a run; reopening replays it from the store. That is what makes the app feel like
a tool rather than a demo.

## Security

- Bind to `127.0.0.1`. Never `0.0.0.0`.
- Check the `Origin` header on every mutating route, so a page the user happens to
  open cannot drive their machines.
- The provider key and the Hyperwake token stay server side, always.
- Say plainly in the README that the agent runs arbitrary commands in the guest.
  That is the product, and people should read it before their first run rather
  than discover it.

## Milestones

**Sequencing.** [ADR-MCP-001](ADR-MCP-001-mcp-server.md) goes first. An MCP
server is a few hundred lines against a whole application, and it tests the tool
names, descriptions and argument shapes against real models before a UI is built
around them. This app inherits whatever that teaches.

**M0 — packaging spike.** `next build`, `npm pack`, `npx ./the-tarball.tgz`,
browser opens, health check passes. No features. This is the only unknown in the
plan that could change the framework, so it goes first.

**M1 — watch an agent use a computer.** Setup, one thread, no persistence. Tools:
`create_machine`, `run_command`, `screenshot`, `click`, `type`, `key`, and the
desktop panel. The deliverable is a recording of the agent opening a terminal in
Omarchy and doing something, with the screen visible next to the chat. An
exec-only first milestone would be a chatbot with ssh, and would not show the
thing that makes this worth building.

**M2 — a real app.** Threads, persistence, resumable runs, machine lifecycle with
idle stop, the machines view.

**M3 — human in the loop.** `ask_human`, take over and hand back. Approvals are
mostly configuration by this point, so the work here is the takeover flow and the
fresh screenshot that follows it.

**M4 — ship it.** README, a recorded demo, the docs page on hyperwake.ai, publish
to npm.

## Licence and naming

`hyperwake-agent` is free on npm.

**FSL-1.1-ALv2**, matching hyperwake-core. A chat app that drives Hyperwake is
close to what the control panel is going to be, so the non-compete is protecting
exactly the thing it exists to protect. If maximum forkability matters more,
MIT is the alternative, and the argument for it is that a reference
implementation gets copied more when copying is unambiguous.

## What I would verify before building

1. That the packaging spike works. Everything else is ordinary web work; this is
   the only step that could send us to a different framework.
2. That a downscaled screenshot plus scaled-back coordinates actually lands a
   click on a target in Omarchy. One script, no UI, before any agent loop exists.
3. That the desktop iframe survives the single-use ticket in practice, including
   what happens when the socket drops and the viewer wants to reconnect with a
   ticket that has already been redeemed.
