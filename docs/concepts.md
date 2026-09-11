# Concepts

## A machine

A machine is a virtual machine running Omarchy: Arch Linux with the Hyprland
compositor, the same desktop you would install on a laptop. It runs under QEMU,
using your platform's hypervisor (HVF on macOS, KVM on Linux).

Each one is created from the same base image by copy-on-write clone, so making a
new machine costs about a second and a few megabytes rather than a full copy of
the disk. Machines do not share anything after that. What you install in one is
invisible to the others.

Inside every machine a small Go daemon runs at boot. It registers with the
engine, then sends a heartbeat every ten seconds carrying a boot id and what the
machine believes it can currently do.

## Status

`status` is the engine's conclusion about a machine. `runtime_status` is what
QEMU says. They are reported separately because they answer different questions.

| Status | Meaning |
|---|---|
| `booting` | QEMU is running, the guest has not reported in for this boot |
| `ready` | the guest has reported in and its shell works |
| `stopped` | the machine is off, its disk intact |
| `unknown` | the runtime cannot see the machine |

Two of those deserve explanation.

`ready` is not "the process started". A QEMU process can be running while the
guest is still bringing up its network, its SSH daemon, or its compositor. The
engine waits for a heartbeat from *this* boot, and for the guest to report a
working shell, before it calls a machine ready. Otherwise the first command you
send lands on a machine that cannot answer.

`unknown` is a real answer, and the engine will give it to you rather than
guessing. A runtime that cannot see a machine has not told you the machine
stopped. Collapsing those two is how a platform loses track of something it is
still running, or bills for something that died an hour ago.

## The guest is not trusted

Whoever uses a machine is root inside it. Anything it reports is therefore a
claim about itself, not a fact about the platform.

The `capabilities` field is the clearest example. The guest says whether it
believes its shell, display and SSH daemon are working. The engine records that
and uses it as one input, but readiness is the engine's conclusion. Nothing a
guest says can move it into a state the engine did not decide on.

The same rule holds for anything else a machine sends. Addresses it reports are
diagnostic. Its own view of how long it has been running is not the basis for
anything that matters.

## Nothing is forgotten while it still exists

The engine will not remove a record while the thing it describes is still
running somewhere.

A delete that cannot tear a machine down returns 502 and leaves the machine
registered, with the reason attached. A create that fails partway tears down
what it built, and if that cleanup also fails the record stays and is marked for
cleanup. The alternative, removing the row and moving on, leaves a virtual
machine consuming memory that nothing can name and nobody can stop.

On startup the engine asks the runtime what it is holding and adopts anything it
does not recognise. A hard kill leaves QEMU running and the record unwritten;
adopting on start means that machine gets a name again instead of quietly eating
the host.

## Where data lives

Everything the engine owns is in `~/.hyperwake`, or wherever `HYPERWAKE_HOME`
points.

```
~/.hyperwake/
  token           the operator token
  keys/           the engine's SSH key, generated on first run
  machines.json   the registry: names, sizes, guest credentials
  runtime/        QEMU supervision, and one directory per machine holding its disk
  image/          the base image every machine is cloned from
  python/         a private virtualenv for screen capture
```

This sits outside the installed package deliberately. `npx` installs into a
cache that is cleared without warning, and a machine's disk has to outlive the
tool that created it.

The registry is kept thin on purpose. It holds what the runtime has no opinion
about, such as the name you chose and when you made it. Whether a machine is
running is asked of the runtime rather than remembered, because two sources for
one answer eventually disagree.

## Keys and tokens

Three separate credentials, each doing one job.

The operator token authenticates you to the engine. It is created on first run
and lives in `~/.hyperwake/token`.

The engine's SSH key is generated on first run and its public half is installed
into each machine at creation. Nothing is baked into the base image, so no two
installations share a key. An image with a key in it would put the same key on
every machine anyone ever built from it.

Each machine gets its own credential for talking back to the engine. The
registration token is single use. If a guest loses the response it recovers by
proving it holds its enrolment key, rather than by presenting the token again.

## Graphics

Omarchy uses Hyprland, which needs a DRM render node to allocate buffers. The
machine gets one from virtio-gpu.

It does not need a GPU. With a plain virtio-gpu device the guest renders on the
CPU through llvmpipe, which is enough for a working desktop. If your QEMU was
built with virglrenderer, the engine uses `virtio-gpu-gl` instead and rendering
is accelerated. `hyperwake doctor` reports which one you have.

The desktop leaves the machine over its own VNC server, which the engine proxies
to your browser. The QEMU display is not involved.
