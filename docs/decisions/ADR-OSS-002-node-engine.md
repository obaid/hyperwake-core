# ADR-OSS-002 — The open-source engine is a Node service, and Laravel keeps the fleet

**Date:** 2026-09-10
**Status:** Proposed
**Supersedes part of:** ADR-OSS-001 (which assumed the open build was extracted
from the Laravel app)

## Decision

Two programs, split at an HTTP boundary that already exists in the type system.

| | **Hyperwake Engine** (open source) | **Hyperwake Panel** (closed) |
|---|---|---|
| Language | Node | Laravel (unchanged) |
| Scope | **One host** | **A fleet of hosts** |
| Owns | QEMU/KVM/Docker lifecycle, disks, automation, desktop URL | Users, orgs, plans, Stripe, usage, capacity placement |
| State | SQLite or a JSON file | Postgres |
| Talks to | its own machines | N engines, over their REST API |

## Why this works: `ComputeDriver` is already the API

The interface we have been building against for weeks is, line for line, a REST
surface:

| `ComputeDriver` method | Engine endpoint |
|---|---|
| `createMachine(MachineSpec)` | `POST /machines` |
| `start(id)` | `POST /machines/{id}/start` |
| `requestShutdown(id)` | `POST /machines/{id}/shutdown` |
| `forceStop(id)` | `POST /machines/{id}/force-stop` |
| `destroy(id, deleteDisk)` | `DELETE /machines/{id}` |
| `status(id)` / `describe(id)` | `GET /machines/{id}/status` / `GET /machines/{id}` |
| `createSnapshot(id, name)` | `POST /machines/{id}/snapshots` |
| `listManaged()` | `GET /machines` |
| `supportsPreservingDisk()` | capability flag on `GET /` |

So the panel keeps every line of hardened logic and gains one new driver:

```php
final class HyperwakeEngineDriver implements ComputeDriver { /* HTTP client */ }
```

**Nothing we hardened gets thrown away.** The capacity ledgers, generation
fencing, idempotency records, zero-drift accounting, resumable deletes and the
fourteen P0 fixes all live in the panel, because every one of them is about
*fleet* concerns — many tenants, many hosts, money. A single-operator engine has
none of those problems.

## Why Node is the right call for the engine, and why it is not a rewrite

The instinct to fear a rewrite was based on the wrong number. The Laravel app is
~7,000 lines in the relevant modules, but most of that mass exists *because* it
is multi-tenant: plans, quotas, usage intervals, capacity scheduling across
hosts, billing reconciliation. **The engine inherits none of it.**

What the engine actually does: keep a list of machines on one box, start and stop
QEMU, hand back status honestly, proxy automation calls to the guest daemon, mint
a desktop URL. That is a small program — low thousands of lines, not seven.

It also inherits the parts that were never PHP in the first place:

| Component | Lines | Language |
|---|---|---|
| Guest daemon | 3,318 | Go |
| SSH gateway | 2,077 | Go |
| Desktop gateway | 285 | Node |
| Native/KVM runtimes | 568 | Python |
| Omarchy image | 373 | Docker/shell |

**6,621 lines carry over untouched regardless of what language the engine is.**
The runtimes are already separate processes; the engine supervises them.

Node earns its place on distribution. The open-source user is a developer
pointing agents at their own hardware, and the difference between
`npx hyperwake` and "clone the repo, run Docker Compose with seven services,
wait for a Postgres migration" is the difference between trying it and not. The
desktop gateway is already Node, and ADR-OSS-001 moves the noVNC viewer into it —
so the engine and the gateway can be one process.

## What the engine must not get wrong

A new program does not inherit the bugs we already paid for, but it does inherit
the *lessons*. These are the ones that transfer:

1. **The guest is hostile.** Never trust guest-reported state for anything that
   matters. Possession-bound enrollment; the control plane decides readiness.
2. **A provider outage must not look like success.** P0-03. `status()` must be
   able to say "I do not know", and the panel already handles that answer.
3. **Operations must be idempotent and fenced.** Two `start` calls for the same
   machine must not produce two VMs.
4. **Never use clamped subtraction to release capacity** (`CONTRACTS.md`). Even
   with no ledger, releasing a disk or a port twice must be detectable.
5. **Do not bake a shared secret into an image.**
6. **Ship the guest image before the stricter control plane**, always.

The HTTP boundary is not a new risk. The panel has always treated the driver as
an unreliable remote system, because that is exactly what Proxmox is — that is
what generation fencing and the evidence model are *for*.

## Consequences

- `HyperwakeEngineDriver` joins Fake/Docker/Proxmox/Native. The existing driver
  conformance tests apply to it unchanged, which is how we know the boundary is
  honest.
- The engine needs its own test suite. It is new code; it gets no credit for the
  panel's 430 tests.
- The engine's API is a **public contract** the moment it ships. Version it
  (`/v1`) from the first commit.
- The panel's `Native`/`Docker` drivers can eventually be retired in favour of
  talking to a local engine, so there is one implementation of "run a VM on a
  host" rather than two.
- Licence: the engine ships under FSL-1.1-ALv2. Every use is permitted except
  selling something that competes with Hyperwake, and each release converts to
  Apache 2.0 two years after it ships. ADR-OSS-001 proposed AGPL, which lets a
  competitor host the engine unmodified and satisfy the licence by pointing at
  this repository. A noncommercial licence was tried and rejected for the
  opposite reason: it bans the engineer running `npx hyperwake` on a work
  laptop, who is the whole audience. The split makes a non-compete workable,
  because the panel talks to the engine over a documented network protocol
  rather than linking against it.

## Sequence

1. Move noVNC into the desktop gateway; API returns `desktop_url` (ADR-OSS-001).
2. Define and freeze the engine's `/v1` REST contract, taken from `ComputeDriver`
   plus the automation verbs.
3. Build the Node engine against the Apple Silicon native runtime first — it is
   the one path proven to boot a real Omarchy desktop today.
4. Write `HyperwakeEngineDriver` in the panel; run the existing driver
   conformance suite against a live engine.
5. Only then consider retiring the panel's direct runtime drivers.
