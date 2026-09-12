# Development

```sh
git clone https://github.com/obaid/mola-core
cd mola-core
npm install
```

## Running from a checkout

```sh
node bin/mola.js doctor
node bin/mola.js start
```

Use a separate state directory so you do not disturb an installed engine:

```sh
MOLA_HOME=/tmp/hw-dev MOLA_PORT=4242 node bin/mola.js start
```

## Tests

```sh
npm test
```

Ten unit tests covering status resolution, request validation, the guest
protocol and the registry. They run in well under a second and need no QEMU.

The end to end test needs a running engine and creates a real machine:

```sh
MOLA_HOME=/tmp/hw-dev MOLA_BASE=http://127.0.0.1:4242 test/acceptance.sh
```

It creates a machine, waits for ready, runs a command, round-trips a file, mints
a desktop ticket, checks the ticket cannot be replayed, then stops, deletes, and
verifies that machine is gone from both the registry and the disk. Sixteen
assertions, about forty seconds.

There is also a [Postman collection](../postman/) covering the same path, which
runs under newman.

## Layout

| | |
|---|---|
| `bin/mola.js` | CLI entry point: `start` and `doctor` |
| `src/preflight.js` | what this host can do, measured |
| `src/server.js` | HTTP routing and request handling |
| `src/api.js` | validation and response shaping |
| `src/runtime.js` | supervises the Python QEMU runner and talks to it |
| `src/guest.js` | the control plane half of the guest protocol |
| `src/automation.js` | runs one automation verb against a guest |
| `src/desktop.js` | desktop tickets, the noVNC page, the websocket proxy |
| `src/state.js` | the machine registry |
| `src/keys.js` | the engine's SSH key |
| `src/python.js` | the private virtualenv for screen capture |
| `runtime/` | the Python QEMU supervisor and automation transport |
| `guest/` | the Go daemon that runs inside a machine |
| `image/` | guest image builds |

The engine supervises the Python runner rather than reimplementing QEMU
management in JavaScript. That runner is the part that has actually booted
desktops; rewriting working supervision to save a process would trade
correctness for tidiness.

## Building the guest daemon

```sh
make -C guest/mola-guest
```

Binaries are not committed. A repository that ships its own build output invites
a stale binary running against a newer control plane, which strands every
machine at boot.

## Building images

```sh
python3 bin/build-images        # container images and the guest daemon
python3 bin/native-prepare      # a guest image for the native QEMU runtime
```

Both need Docker. Docker builds images; it does not run the machines.

## Conventions

Errors carry a status that means something and a message a person can act on.
409 means the machine is not in a state that allows this; 502 means teardown
failed and the machine is still registered.

New state a machine can report is a claim, not a fact. Record it, decide
separately.

Anything that can leave a machine running must not remove the record that names
it.
