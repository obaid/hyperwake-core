# Hyperwake

**Throwaway Omarchy computers for your agents.**

A real Arch and Hyprland desktop in its own virtual machine, on hardware you
already own. One second to make one. Seven to use it. Delete it and make
another.

```sh
npx hyperwake
```

The engine starts and prints an address and a token:

```
  API    http://127.0.0.1:4141/v1
  Token  7_NmOSph4PWtcZP5QpVW4rJJ-9VIlIx3
```

Hand those to your agent. It can create a computer, run shell commands, read and
write files, take screenshots, move the mouse and type, open the desktop in a
browser, and delete the whole thing when it is done.

Each computer is a full [Omarchy](https://omarchy.org) desktop: Arch Linux with
Hyprland, running in its own virtual machine. Creating one takes about a second
and it becomes usable in under ten.

## What you need

Node 20 or newer, plus:

| | |
|---|---|
| Apple Silicon Mac | `brew install qemu`, Python 3.10 or newer, an SSH client |
| Linux x86_64 | `qemu-system-x86_64`, a usable `/dev/kvm`, Python 3.10 or newer, SSH |

Check your machine before you start:

```sh
npx hyperwake doctor
```

It reports what it found rather than what it assumes, and names anything that is
missing.

Machines are cloned from a base image. The first run downloads one, verifies its
checksums, and puts it in place, so you learn no second command.

Set `HYPERWAKE_IMAGE_URL` to use a different image, or build one with
`bin/native-prepare`. The script checks its own prerequisites and names anything
missing. [Getting started](docs/getting-started.md) covers both.

## Your first computer

Save the token from the startup banner, then:

```sh
export TOKEN=...          # from the banner
export API=http://127.0.0.1:4141/v1

ID=$(curl -s -X POST $API/machines \
  -H "Authorization: Bearer $TOKEN" \
  -H 'content-type: application/json' \
  -d '{"name": "first"}' | python3 -c 'import sys,json;print(json.load(sys.stdin)["data"]["id"])')
```

Wait for it to come up. A new computer starts in `booting` and reaches `ready`
once the guest has reported in:

```sh
curl -s $API/machines/$ID -H "Authorization: Bearer $TOKEN"
```

Then run something on it:

```sh
curl -s -X POST $API/machines/$ID/actions \
  -H "Authorization: Bearer $TOKEN" -H 'content-type: application/json' \
  -d '{"action": "exec", "command": "uname -a; ls ~"}'
```

And look at the screen:

```sh
curl -s -X POST $API/machines/$ID/desktop -H "Authorization: Bearer $TOKEN"
```

That returns a URL. Open it in a browser and you are looking at the live
desktop, mouse and keyboard included.

[Getting started](docs/getting-started.md) walks through the same path in more
detail, including cleaning up afterwards.

## The API

Every request needs `Authorization: Bearer <token>`. `GET /v1` lists the
endpoints and reports what the host can do.

| | |
|---|---|
| `POST /v1/machines` | create a computer and start it |
| `GET /v1/machines` | list them |
| `GET /v1/machines/{id}` | describe one |
| `POST /v1/machines/{id}/start` | start a stopped computer |
| `POST /v1/machines/{id}/stop` | shut it down |
| `POST /v1/machines/{id}/actions` | do something inside it |
| `POST /v1/machines/{id}/desktop` | get a browser URL for the screen |
| `DELETE /v1/machines/{id}` | destroy it and its disk |

Nine actions run inside a machine: `exec`, `read_file`, `write_file`,
`screenshot`, `click`, `move`, `scroll`, `type`, and `key`.

Full details in the [API reference](docs/api.md). There is also a
[Postman collection](postman/) that exercises the whole thing in sixteen
assertions.

## Documentation

- [Getting started](docs/getting-started.md), from install to a working computer
- [API reference](docs/api.md), every endpoint and action
- [Concepts](docs/concepts.md), what a machine is and what its states mean
- [Troubleshooting](docs/troubleshooting.md), failures you are likely to hit
- [Development](docs/development.md), working on the engine itself

## What is in this repository

| | |
|---|---|
| `src/`, `bin/` | the engine: HTTP API, guest protocol, desktop viewer |
| `runtime/` | the Python supervisor that drives QEMU, and the automation transport |
| `guest/` | the Go daemon that runs inside a machine and reports back |
| `image/` | guest image builds |
| `postman/` | a collection covering the whole API |

The published npm package contains `bin/`, `src/`, `runtime/` and `postman/`.
The rest are build inputs.

## Where state lives

Everything the engine owns sits in `~/.hyperwake`, which you can move with
`HYPERWAKE_HOME`. It is deliberately outside the package, because `npx` installs
into a cache that gets cleared, and a computer's disk has to outlive the tool
that made it.

```
~/.hyperwake/
  token           the operator token
  keys/           the engine's SSH key, generated on first run
  machines.json   what exists
  runtime/        QEMU supervision and per-machine disks
  image/          the guest base image
  python/         a private virtualenv for screen capture
```

## Licence

[Functional Source License 1.1](LICENSE.md). Use Hyperwake for anything, at home
or at work, including inside a product you sell. The one thing you may not do is
sell Hyperwake itself, or a service that does what it does.

Every release becomes Apache 2.0 two years after it ships, and that grant is
irrevocable. Until then the source is published and you can read, change, and run
it, but the terms are not open source by the OSI definition.

Omarchy and the bundled dependencies keep their own licences. See
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md). Hyperwake is independent and
is not affiliated with or endorsed by Omarchy, DHH, 37signals, Basecamp, or the
Omacom Foundation.
