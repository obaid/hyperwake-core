# ADR-IMAGE-001 — Real Omarchy builds in Docker; its desktop cannot run there

**Date:** 2026-09-09
**Status:** Historical finding; superseded for Mac support by the native runtime
**Context:** directive §1, §11; audit finding A17 / P0-14
**Evidence state:** `VERIFIED_LOCAL` for the image; `BLOCKED_EXTERNAL` for the display gate

## Updated scope — 2026-09-10

The failures below apply to the tested desktop running **inside Docker Desktop**.
They do not mean Omarchy cannot run on a Mac. Hyperwake now runs ARM Omarchy
through native QEMU/HVF with a virtual GPU, while Docker runs the API service.
See [platform support](../../platforms.md) and the release acceptance record.
The original measurements are retained below.

## Decision

`image/omarchy/` builds `hyperwake/omarchy:dev` from **genuine upstream Omarchy
v4.0.3** on Arch Linux x86_64, installed from Omarchy's own package repository.

Its **graphical session cannot start in Docker on macOS**, for a reason that is
a property of the host, not of the image. The image does not paper over this:
it refuses to start a substitute desktop, reports the cause, and exits non-zero.

`image/docker/` (Debian + i3) is retained and still works, explicitly labelled
as the local development stand-in.

## Why the desktop cannot run

Omarchy is Hyprland. Hyprland 0.56.2 renders through **aquamarine** 0.15.0,
which needs a DRM render node to allocate buffers. Every backend it has needs
one. Measured, in order:

**1. DRM backend — no seat, then no GPU.**

Initially libseat could not open a seat at all:

```
[libseat] Could not connect to socket /run/seatd.sock: No such file or directory
[libseat] Backend 'logind' failed to open seat, skipping
ERR from aquamarine ]: DRM Backend failed
```

That part is fixable — running `seatd -g seat` gives a real seat. It changes
nothing, because the actual blocker is underneath it:

```
[libseat] Seat opened with backend 'seatd'      <- seat now works
ERR from aquamarine ]: drm: No gpus in scanGPUs.
ERR from aquamarine ]: drm: Found no gpus to use, cannot continue
ERR from aquamarine ]: DRM Backend failed
CRIT from aquamarine ]: Cannot open backend: no allocator available
```

Docker Desktop's LinuxKit kernel exposes no DRM device and cannot make one:

```
# uname -r
6.12.76-linuxkit
# ls /dev/dri
ls: cannot access '/dev/dri': No such file or directory
# modprobe vkms
modprobe: FATAL: Module vkms not found in directory /lib/modules/6.12.76-linuxkit
```

`vkms` is the virtual KMS driver that would synthesise a DRM device. It is not
built for this kernel, so there is no software substitute available.

**2. Wayland-nested backend — parent compositor lacks the protocols.**

wlroots *does* have a headless backend, so `sway` starts fine headless and
publishes `wayland-1`. Nesting Hyprland inside it still fails:

```
DEBUG from aquamarine ]: Connected to a wayland compositor: unknown
ERR from aquamarine ]: Wayland backend cannot start: Missing protocols
ERR from aquamarine ]: Requested backend (wayland) could not start, enabling fallbacks
CRIT from aquamarine ]: Cannot open backend: no allocator available
```

Sway's headless output does not export the dmabuf/DRM protocols aquamarine
needs, because it has no GPU to export them from.

**3. X11 backend — does not exist any more.**

Confirmed with a working X server (`Xvnc`, verified at 1280x800 via `xdpyinfo`)
and `DISPLAY=:1` set: Hyprland fails identically. wlroots had an X11 backend;
aquamarine does not.

**4. There is no headless escape hatch.** The complete aquamarine environment
surface is `AQ_DRM_DEVICES`, `AQ_FORCE_LINEAR_BLIT`, `AQ_LIBINPUT_NO_PLUGINS`,
`AQ_MGPU_NO_EXPLICIT`, `AQ_NO_ATOMIC`, `AQ_NO_MODIFIERS`, `AQ_TRACEUH`. None
selects a headless backend. The wlroots-era `WLR_BACKENDS=headless` is ignored.

## The fix

A KVM guest has a virtual GPU. `virtio-gpu` presents a real DRM render node, so
`scanGPUs` finds a device and the DRM backend starts normally. **This is
Milestone 1 and it needs the Proxmox host.** The image is written for that
target and the session script is the production path, not a mock.

Two other paths were rejected:

- *Ship a GPU-less desktop by swapping in i3/X11.* This is what the directive
  explicitly forbids — it produces an image called Omarchy that is not Omarchy.
- *Use `vkms`.* Would need a custom Docker Desktop kernel. Not reproducible for
  anyone else and still not the production configuration.

## Also discovered

**Arch Linux ARM cannot be the base.** It is a different distribution from Arch,
and its repository is currently self-inconsistent for this package set:
`hyprland 0.56.1-3` requires `libaquamarine.so=13-64` while `aquamarine 0.15.0-2`
provides `libaquamarine.so=14-64`. Unsatisfiable. Arch proper x86_64 is
consistent (`hyprland 0.56.2-2` ↔ `.so=14-64`) and is Omarchy's actual target,
so the image is amd64 and runs here under Rosetta.

**pacman 7 needs its sandbox disabled to build in Docker Desktop.** Landlock and
the unprivileged `alpm` download user both fail in the VM, surfacing as a
misleading "failed to synchronize all databases". Handled in the Dockerfile.

**sshd cannot complete its privilege-separation sandbox under Rosetta:**

```
ssh_sandbox_child: prctl(PR_SET_SECCOMP): Invalid argument [preauth]
```

Every connection is closed pre-auth, and `--security-opt seccomp=unconfined`
does not help because the limitation is in the x86-on-ARM translation layer, not
Docker's profile. The control is the native arm64 Debian image, whose sshd works
on this same machine. SSH is therefore untested in the emulated Omarchy image
and expected to work on native x86 hardware. **This is an emulation artifact,
not an image defect — but it is untested until M1 either way.**

## What this means for the milestones

| Gate | State | Note |
|---|---|---|
| Genuine Omarchy package set installs | `VERIFIED_LOCAL` | 125/147 from Arch, remainder from `[omarchy]` |
| Pinned immutable image identity | `VERIFIED_LOCAL` | `/etc/hyperwake/image.json`, commit `0534987` |
| Persistent `/home/dev` across recreate | `VERIFIED_LOCAL` | file, config and SSH host key all survived |
| Additive first-boot seeding | `VERIFIED_LOCAL` | second boot leaves home untouched |
| Graceful shutdown | `VERIFIED_LOCAL` | `docker stop` in 0s, exit 0 |
| Doctor reports capability honestly | `VERIFIED_LOCAL` | exits 1 on missing DRM |
| **Hyprland/Wayland display** | **`BLOCKED_EXTERNAL`** | needs virtio-gpu; M1 |
| **wayvnc remote display** | **`BLOCKED_EXTERNAL`** | needs the compositor |
| **SSH into Omarchy image** | **`BLOCKED_EXTERNAL`** | Rosetta seccomp; works on native x86 |
| Input latency benchmark | `NOT_STARTED` | needs a running desktop |

The honest summary: **the image is real and most of the guest contract is
proven; the thing the product is actually selling — the desktop — is unproven
and stays unproven until there is hardware.**
