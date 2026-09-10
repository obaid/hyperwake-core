# Hyperwake

**Omarchy computers on your own machine, driven over a REST API.**

```sh
npx hyperwake
```

That prints a URL and a token. Hand them to your agent. It can now create a
computer, run commands in it, read and write its files, see its screen, move its
mouse, and throw it away.

```
  API    http://127.0.0.1:4141/v1
  Token  7_NmOSph4PWtcZP5QpVW4rJJ-9VIlIx3
```

Every machine is a **fresh Omarchy instance**, cloned from a base image in under
a second. Nothing you do to one affects another.

## What you need

| | |
|---|---|
| **Apple Silicon Mac** | `brew install qemu`, Python 3.10+, an SSH client. |
| **Linux x86_64** | `qemu-system-x86_64`, `/dev/kvm`, Python 3.10+, SSH. |

No Apple Developer ID, no signing, no bundled runtime. QEMU signs itself with
the `com.apple.security.hypervisor` entitlement at build time — an *ad-hoc*
signature (`codesign -s -`) — so any QEMU you install can use the Hypervisor
framework.

If [Try Omarchy](https://github.com/omacom/try-omarchy) happens to be installed,
the engine will use its QEMU instead, because that build carries
`virglrenderer` and gives the guest accelerated graphics. It is an optimisation,
not a requirement: `hyperwake doctor` reports which one it found and whether
graphics are accelerated or software-rendered.

Run `npx hyperwake doctor` to see what your machine can do. It measures rather
than guesses, and tells you exactly what is missing.

**A note on Docker.** Docker is useful here for *preparing images*, not for
running the guest. Omarchy is a Wayland desktop and its compositor needs a real
accelerator and a display device; Docker on macOS provides neither KVM nor a DRM
node, so the guest runs under native QEMU instead. The engine will say so rather
than fail four minutes into a boot.

## The API

Everything below needs `Authorization: Bearer <token>`.

```sh
curl $API/v1 -H "Authorization: Bearer $TOKEN"
```

| | |
|---|---|
| `POST /v1/machines` | create a fresh Omarchy computer and start it |
| `GET /v1/machines` | list them |
| `GET /v1/machines/{id}` | describe one |
| `POST /v1/machines/{id}/start` | start it |
| `POST /v1/machines/{id}/stop` | shut it down — `{"force": true}` cuts power |
| `POST /v1/machines/{id}/actions` | do something inside it |
| `POST /v1/machines/{id}/desktop` | get a browser URL for the screen |
| `DELETE /v1/machines/{id}` | destroy it and its disk |

### Creating one

```sh
curl -X POST $API/v1/machines -H "Authorization: Bearer $TOKEN" \
  -H 'content-type: application/json' \
  -d '{"name": "research", "vcpus": 4, "memory_mb": 4096, "disk_gb": 40}'
```

Reaches `ready` in about ten seconds. `ready` means the guest has reported in
for *this* boot and its shell works — not merely that a process started.

### Doing things inside it

`POST /v1/machines/{id}/actions`, one verb per call:

```jsonc
{"action": "exec",       "command": "ls ~"}
{"action": "read_file",  "path": "~/notes.md"}          // returns content_base64
{"action": "write_file", "path": "~/notes.md", "content": "hello\n"}
{"action": "screenshot"}                                 // returns image_base64
{"action": "click",  "x": 640, "y": 400}
{"action": "move",   "x": 640, "y": 400}
{"action": "scroll", "direction": "down", "amount": 3}
{"action": "type",   "text": "hello"}
{"action": "key",    "key": "super"}
```

### Seeing the screen

```sh
curl -X POST $API/v1/machines/$ID/desktop -H "Authorization: Bearer $TOKEN"
# {"data": {"desktop_url": "http://127.0.0.1:4141/desktop#t=...", "expires_in": 60}}
```

Open it in a browser. The ticket lives in the URL **fragment**, which browsers
never send to a server — so it stays out of access logs and `Referer` headers —
and it is single-use with a sixty-second life.

## Where things live

State goes in `~/.hyperwake` (override with `HYPERWAKE_HOME`), deliberately
outside the package: `npx` installs into a cache that gets wiped, and a
machine's disk must outlive the tool that made it.

```
~/.hyperwake/
  token           the operator token
  keys/           the engine's own SSH key, generated on first run
  machines.json   what exists
  runtime/        QEMU supervision and per-machine disks
  image/          the Omarchy base image
  python/         a private venv for screen capture
```

## Design notes worth knowing

**The guest is a hostile tenant.** Whoever uses a machine is root inside it.
Nothing it reports is trusted as fact about the platform — capabilities are a
claim that the machine believes it is usable, and readiness is the engine's
conclusion.

**`unknown` is a real status.** A runtime that cannot see a machine has *not*
told us the machine stopped. Collapsing those two is how a platform loses track
of a computer it is still running.

**Nothing is forgotten while it still exists.** A delete that cannot tear a
machine down leaves it registered and says so, rather than removing the record
and stranding a VM with no name. On start the engine adopts anything the runtime
holds that it does not recognise.

## What is in this repository

| | |
|---|---|
| `src/`, `bin/` | the engine: REST API, guest protocol, desktop viewer |
| `runtime/` | the Python supervisor that drives QEMU, and the automation transport |
| `guest/` | the Go daemon that runs inside a machine and reports to the engine |
| `image/` | the Omarchy image build |
| `postman/` | a collection that exercises the whole API |
| `bin/build-images` | builds the Omarchy container images and the guest daemon |
| `bin/native-prepare` | prepares a guest image for the native QEMU runtime |

The published npm package is `bin/`, `src/`, `runtime/` and `postman/`. `guest/`
and `image/` are build inputs.

## Development

```sh
npm test                                    # unit tests
node bin/hyperwake.js doctor                # what this host can do
HYPERWAKE_HOME=/tmp/hw test/acceptance.sh   # end to end, against a running engine
```

Useful environment variables: `HYPERWAKE_HOME` (state directory),
`HYPERWAKE_PORT`, `HYPERWAKE_QEMU` (use a specific QEMU),
`HYPERWAKE_MAX_RUNNING`.

## Licence

**AGPL-3.0-or-later.** Running a modified version as a network service means
publishing your changes; self-hosting an unmodified copy carries no such
obligation. Omarchy and the bundled dependencies keep their own licences.
Hyperwake is independent and is not affiliated with or endorsed by Omarchy, DHH,
37signals, Basecamp, or the Omacom Foundation.
