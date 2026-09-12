# Mola desktop image (development)

`hyperwake/desktop:dev` — the machine image the **docker** compute driver
clones for every computer created on a developer laptop.

## This is not Omarchy

This image is a development stand-in. It exists so the entire control plane —
provisioning, the desktop gateway, SSH, whole-disk persistence, the usage
ledger — can be exercised end to end without a Proxmox host.

It is Debian with i3, not Arch with Hyprland. The `Image` record that points at
it is deliberately slugged `hyperwake-dev-desktop`, the welcome screen says so
in the first paragraph, and the wallpaper says so too. Nothing in the product
should ever present it as Omarchy.

Building the real Omarchy image is a separate pipeline (Packer, a pinned
Omarchy ISO, `cidata` unattended install) and belongs to `image/packer/`.

Arch with Hyprland was tried and abandoned for the local image: under Docker
Desktop on Apple Silicon it is a long yak-shave for something that is only ever
a stand-in. The abstraction that matters is `ComputeDriver` + `Image`, and this
image proves it.

## The contract an image must satisfy

The control plane does not care what distribution an image runs. It requires:

| Requirement | Why |
|---|---|
| VNC display on `:5900` inside the machine | the desktop gateway proxies to it |
| `sshd` on `:22`, public-key auth only | the SSH path in the PRD |
| authorized keys read from `HYPERWAKE_AUTHORIZED_KEYS` | per-machine key injection |
| the guest daemon running and calling home | health, auto-stop hints, shutdown |
| `/home/dev` as the only directory that must survive a stop | the persistence promise |
| no shared secret baked into the image | two clones must never share an identity |
| identity read from env **or** the SMBIOS serial | Docker and Proxmox inject it differently |

Anything meeting that contract is a valid Mola image.

## Build

```bash
# The daemon binary must exist first; the Dockerfile copies it in.
cd guest/hyperwake-guest && make build
cp bin/hyperwake-guest-linux-arm64 ../../image/docker/bin/hyperwake-guest

cd image/docker && docker build -t hyperwake/desktop:dev .
```

Use `hyperwake-guest-linux-amd64` on an x86 host.

## Environment

| Variable | Purpose |
|---|---|
| `HYPERWAKE_ENDPOINT` | control plane base URL. May instead arrive via SMBIOS |
| `HYPERWAKE_REGISTRATION_TOKEN` | single-use token, injected per machine |
| `HYPERWAKE_SMBIOS_PATH` | override the DMI path identity is read from |
| `HYPERWAKE_AUTHORIZED_KEYS` | newline-separated SSH public keys |
| `HYPERWAKE_MACHINE_NAME` | shown in the status bar and welcome screen |
| `HYPERWAKE_DISPLAY_GEOMETRY` | default `1440x900` |
| `HYPERWAKE_HEARTBEAT_INTERVAL` | seconds; the control plane can override it |

## Persistence

`/home/dev` is the volume. Three things live under `~/.hyperwake` and therefore
persist with the disk:

- `state/` — the machine credential. **This must persist.** The registration
  token is single-use, so a machine that lost its credential could never
  re-register and would be orphaned forever.
- `ssh/` — the machine's SSH host keys. If these changed on every wake, every
  customer would get a host-key-changed warning and the "same computer" promise
  would visibly break.
- `.seeded` — the marker that stops the skeleton being re-copied.

Seeding is strictly additive (`cp -rn`). Nothing under `/home/dev` is ever
overwritten by a boot, because silently destroying customer work is the one
failure this product cannot have.

`authorized_keys` is the deliberate exception: it is rewritten on every boot,
because it is control-plane state. A key the customer revoked in the dashboard
must actually stop working.

## Why the display has no VNC password

The X server runs with `-SecurityTypes None`. That is safe **only** because of
where the port lives:

- the container's `5900` is published to `127.0.0.1` on the host, never to a
  routable address;
- the only thing that connects to it is the Mola desktop gateway;
- the gateway requires a single-use, 60-second token bound to the owning user,
  which it redeems against the control plane before it opens any TCP socket.

Publishing `5900` on a public interface would turn this into an unauthenticated
remote desktop. Do not do it. In production the equivalent rule is: the guest
display is reachable only on the private management path.

Note also the absence of `-localhost` on `Xtigervnc`: Docker's port forwarding
makes connections appear to arrive from the bridge gateway rather than
`127.0.0.1`, so `-localhost` would reject the gateway itself.

## SSH host keys

The Dockerfile deletes the host keys apt generates (`RUN rm -f /etc/ssh/ssh_host_*`).
Baking host keys into a reusable template would give every clone the same SSH
identity — precisely the "no shared secret in a reusable template" rule. They
are generated per machine on first boot and then persisted with the disk.

## Verified behaviour

Checked against a running container:

- VNC listening on 5900, sshd on 22
- i3, i3bar, i3status and a terminal all running, framebuffer captured and
  confirmed to be drawing (1192 distinct colours — not the classic
  connected-but-black canvas)
- SSH accepts the injected key; password auth is refused with
  `Permission denied (publickey)`
- files written to `/home/dev` survive `docker stop` + `docker start`
- the SSH host key fingerprint is identical before and after a wake
- the second boot logs "existing home directory found, leaving it untouched"
- the guest daemon registers once and reuses its stored credential on wake
- a graceful stop returns in ~0.15s with exit code 0 — `tini` is PID 1, the
  entrypoint traps `TERM`, and the daemon uses `signal.NotifyContext`. Nothing
  here waits out the `shutdown_grace` timeout
- identity works from either source: `identity_source: environment` on the
  Docker path, `identity_source: smbios` with no identity env vars at all
