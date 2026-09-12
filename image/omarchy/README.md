# `mola/omarchy:dev` — the flagship image

Genuine upstream **Omarchy v4.0.3** on Arch Linux x86_64. This is the image the
product is named after. `../docker` is the Debian + i3 development stand-in and
is not Omarchy; it is kept because it is the only one whose desktop runs on a
laptop.

## What makes it genuinely Omarchy

- **Omarchy's own package repository** (`https://pkgs.omarchy.org/stable/$arch`),
  which is where `aether`, `omacut`, `omawrite`, `omarchy-nvim` and the rest of
  the project's own software comes from. 125 of the 147 base packages resolve
  from Arch; the remainder come from here. Without this repo you have an Arch
  box with similar packages, which is not the same thing.
- **The upstream package list verbatim** — `install/omarchy-base.packages` from
  the pinned tag, not a hand-curated approximation.
- **Upstream configuration** cloned at tag `v4.0.3`, commit `0534987`.

Verified installed: `hyprland 0.56.2`, `quickshell 0.3.1`, `aether 4.29.8`,
`omacut 0.4.0`, `omawrite 0.5.0`, `omarchy-nvim 2026.8.13`, `chromium 152`,
`foot 1.28`, `uwsm 0.26.7`, `wayvnc 0.10.1`.

### Deliberate exclusions

Seven packages address physical hardware a VM does not have, and are recorded in
`/etc/mola/image.json` rather than silently dropped: `asdcontrol` (Apple
display brightness over USB), `ddcutil` (DDC/CI over I2C), `plymouth` (boot
splash), `bolt` (Thunderbolt authorisation), and the three `cups` printing
packages.

`wayvnc` is a Mola **addition** — Omarchy assumes a physical screen and
ships no remote display.

## The desktop does not run in Docker

Read `docs/implementation/decisions/ADR-IMAGE-001-omarchy-in-docker.md` for the
measured evidence. In short: Hyprland renders through aquamarine, aquamarine
needs a DRM render node on every backend path, and Docker Desktop's LinuxKit
kernel has no `/dev/dri` and no `vkms` module to make one.

The session script **refuses to substitute another desktop** and exits 78. An
image that quietly starts i3 when Hyprland fails is an image that claims to be
Omarchy and is not.

On a KVM guest with `virtio-gpu` the render node exists and the same script is
the production path.

## Build

```bash
# The guest daemon must be built for amd64 first.
(cd ../../guest/mola-guest && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
   go build -o ../../image/omarchy/bin/mola-guest-amd64 ./cmd/mola-guest)

docker build --platform linux/amd64 -t mola/omarchy:dev .
```

~9 GB and about 15 minutes on a warm cache; the package install alone is 6.5 GB.

## What is proven locally, and what is not

| | State |
|---|---|
| Omarchy package set installs from pinned sources | verified |
| Immutable image identity in `/etc/mola/image.json` | verified |
| `/home/dev` survives container recreation | verified |
| First-boot seeding is additive; second boot leaves home alone | verified |
| Per-machine SSH host keys, stable across wake | verified |
| `docker stop` returns in 0s, exit 0 | verified |
| `mola-doctor` fails honestly on missing capability | verified |
| **Hyprland session** | blocked — needs a GPU |
| **wayvnc remote display** | blocked — needs the session |
| **SSH login** | blocked — Rosetta cannot run sshd's seccomp sandbox |
| **Input latency** | not started — needs a desktop |

## Known local-only artifacts

Both are properties of running x86_64 under emulation on macOS, not of the image:

- **sshd closes every connection pre-auth** with
  `ssh_sandbox_child: prctl(PR_SET_SECCOMP): Invalid argument`. Rosetta does not
  implement seccomp filters. `seccomp=unconfined` does not help. The native
  arm64 Debian image's sshd works on this same machine, which is the control.
- **pacman's download sandbox must be disabled at build time** (Landlock and the
  `alpm` user both fail in the Docker VM).

## Arch, not Arch Linux ARM

ALARM is a different distribution and its repo is currently unsatisfiable for
this set: `hyprland 0.56.1-3` wants `libaquamarine.so=13-64`, `aquamarine
0.15.0-2` provides `14-64`. Arch proper is consistent and is Omarchy's actual
target, so this image is amd64 and runs under emulation here.
