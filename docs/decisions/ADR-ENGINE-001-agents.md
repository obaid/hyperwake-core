# ADR-ENGINE-001 — Agents on a machine, and how they get a key

**Date:** 2026-09-10
**Status:** Options, not yet decided

## What is actually there

Measured on a running Omarchy guest, not assumed:

| | |
|---|---|
| Present | `python3`, `git`, `curl`, working network |
| Absent | `node`, `npm`, `bun`, `uv` |
| Absent | `claude`, `codex`, `opencode`, `pi` |

**Omarchy does not preinstall any coding agent.** The image is 597 packages and
none of them is `nodejs`, `npm`, `claude`, `codex` or `opencode`. It is exe.dev's
`exeuntu` image that ships agents preinstalled, not ours. `mise` *is* present at
`/usr/bin/mise`, which is the clean no-sudo route to a node runtime.

But that turns out barely to matter, because **Codex ships a static
`aarch64-unknown-linux-musl` binary** and needs no node at all. Measured in a
running guest:

```
curl -fsSL codex-aarch64-unknown-linux-musl.zst   65 MB in 3.1s
zstd -d && install -m755 /usr/local/bin/codex
codex --version                                    codex-cli 0.154.0
```

About ten seconds, start to finish, on a machine with nothing installed. `zstd`,
`tar` and `curl` are already in the image.

`codex exec` is the non-interactive mode, and it has every flag this design
needs: `--json` for structured output, `-c key=value` to point `model_provider`
at our gateway, `--skip-git-repo-check` because a fresh machine has no repo, and
`--output-schema` for typed results.

`pi` and Claude Code are npm-only, so they cost a node install (via `mise`) that
Codex does not. **That is the argument for Codex first**, quite apart from
preference.

Relevant plumbing we already have: the guest reaches the engine at `10.0.2.2`
(that is how registration and heartbeats work today), so an engine-hosted
endpoint is reachable from inside a machine with no new networking.

## Decision 1 — where the provider key lives

**A. In the guest.** Write `OPENROUTER_API_KEY` into the machine at create time.
Simple, ten lines. But the tenant is root in their own machine: they can read the
key, take it, and spend it. For a single operator running the engine on their own
laptop with their own key that is *honest* — it is your key on your computer.
For anything hosted it is disqualifying.

**B. In the engine, behind a gateway.** The engine holds the key and exposes
`http://10.0.2.2:<port>/llm/...` to guests. The agent points its base URL there
with a placeholder key. **The key never enters the machine.**

This is exactly what exe.dev does, and their wording is the giveaway: *"The VM
can call the integration hostname, but cannot read the key."* Their agent config
is just

```
ANTHROPIC_BASE_URL=https://llm.int.exe.xyz  ANTHROPIC_API_KEY=implicit  claude
```

For OpenRouter it is simpler still, because OpenRouter is one OpenAI-compatible
base URL — the proxy is a request forwarder with a header swap, roughly a hundred
lines.

Three things B buys beyond secrecy:

- **Per-machine metering.** Every token a machine spends is attributable, because
  it all passes through us. That is a billing primitive the hosted product needs
  and cannot retrofit easily.
- **Revocation that works.** Kill a machine's gateway credential and it stops
  spending, without touching the upstream key.
- **It is the same code the panel needs.** Built once, in the open-source engine,
  used by both.

**Recommendation: B, with A as an explicit, documented opt-in** for people who
genuinely want their own key inside the machine (offline use, a provider we do
not proxy). Defaulting to A would contradict the invariant this whole codebase is
built on — *the guest is a hostile tenant*.

## Decision 2 — when the agent gets installed

**A. Always, at create.** Every machine gets node and pi. Costs one to three
minutes of downloads and destroys the six-to-ten-second ready time that is
currently the best thing about the engine.

**B. On request.** `POST /v1/machines` takes `agents: ["pi"]`, or a separate
`POST /v1/machines/{id}/agents` installs into a running machine. You pay the
install only when you want an agent.

**C. Baked into the image.** Instant, correct, and **blocked** — the guest image
is still Try Omarchy's factory image. It becomes available when we build our own
(the recipe is MIT and reproducible; see `ADR-OSS-002` follow-ups).

**Recommendation: B now, C as soon as we own the image.** B is a small addition
to the create payload and keeps the fast path fast.

## Decision 3 — how you drive the agent

**A. Nothing new — use `exec`.** `pi -p "prompt"` already works through the
existing actions endpoint. Zero code. The caller handles quoting, gets raw
stdout, and hits our 120-second exec cap.

**B. A dedicated endpoint.** `POST /v1/machines/{id}/agent` with
`{"agent": "pi", "prompt": "..."}`. pi has a **print/JSON mode**, so this can
return real structure rather than scraped text.

**C. pi's RPC mode.** Persistent session, streaming, multi-turn. The most
capable and the most work.

**Recommendation: B, and it must be asynchronous.** An agent run takes minutes;
our exec cap is two. So the endpoint should return a run id immediately and be
polled — `POST /agent` → `{"run_id": ...}`, `GET /agent/runs/{run_id}` →
status, output, token usage. Synchronous is a trap we would have to undo.

Worth noting: pi's four modes (interactive, print/JSON, RPC, SDK) mean **A, B and
C are the same binary**, so starting at B does not foreclose C.

## What this would look like

```jsonc
// create
POST /v1/machines
{ "name": "research", "agents": ["pi"] }

// run something
POST /v1/machines/{id}/agent
{ "agent": "pi", "prompt": "read ~/notes.md and summarise it into ~/summary.md" }
→ { "run_id": "..." }

GET /v1/machines/{id}/agent/runs/{run_id}
→ { "status": "running" | "done" | "failed", "output": ..., "usage": {...} }
```

The key is configured once on the engine, never per request:

```sh
hyperwake key set openrouter sk-or-...      # stored 0600 in ~/.hyperwake
```

## How exe.dev does it

Worth knowing before we invent something, because their answer is smaller than
ours:

1. **They bake the agents into the image.** `exeuntu` preinstalls `claude`,
   `codex` and `pi`, and runs their own agent **Shelley** as a systemd service on
   port 9999, reachable at `https://vmname.shelley.exe.xyz/`.
2. **The gateway removes the key problem.** *"By default, Shelley uses the LLM
   integration, backed by the LLM Gateway, so you don't need to configure any API
   keys."*
3. **At create time there is one affordance:** `new --prompt` hands an initial
   prompt to Shelley.
   `echo 'build me a web app' | ssh exe.dev new --prompt=/dev/stdin`
4. **After that there is no agent API at all.** You run the CLI through the same
   generic exec everything else uses: `POST /exec` with the body
   `ssh my-vm codex exec "..."`.

So their position is: **an agent is just a program, and running programs is what
the API already does.** The only agent-shaped thing they built is a prompt at
creation, and that hands off to a long-running *web* agent rather than being a
request/response call.

That is a real argument for our Decision 3A over 3B — with one caveat that does
not apply to them. Their `/exec` has a 30-second timeout and they expect you to
background the work yourself (`setsid nohup ... &`). Ours caps at 120 seconds.
Either way a synchronous agent call is wrong; the difference is only whether the
backgrounding is the caller's job or ours.

## The honest objection

For a **single operator on their own laptop**, Decision 1B is arguably
over-engineering: it is your key, your machine, your laptop. The reason to build
it anyway is that the gateway is the piece the hosted product cannot do without,
and building it in the open-source engine means it gets exercised by everyone
rather than being a closed-source afterthought bolted on later.

If that argument does not hold, 1A plus 2B plus 3A is perhaps forty lines and
ships this afternoon.
