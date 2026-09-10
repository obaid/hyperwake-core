# hyperwake-guest

The daemon that runs inside every Hyperwake machine.

## What it does

- **Registers once.** Exchanges its injected single-use registration token for
  a rotating machine credential, then persists that credential on the machine's
  disk.
- **Heartbeats.** Reports liveness, guest version, and SSH/TTY session counts.
- **Obeys desired state.** When the control plane says `stopped`, it
  acknowledges and shuts the machine down cleanly.
- **Rotates its credential** whenever the control plane hands back a new one.

## What it must never do

- listen on a network port
- hold hypervisor, billing or platform credentials
- learn anything about another tenant
- be trusted for billing — usage is metered from host-side allocation state

## Build and test

```bash
make            # fmt, vet, test -race, then cross-compile
make test
make build      # bin/hyperwake-guest-linux-{arm64,amd64}
```

## Identity injection: two paths

Nothing is baked into the image — an image carrying a shared secret would let
any tenant impersonate any other. Identity is injected per machine at creation
time, by one of two routes:

| Path | Mechanism |
|---|---|
| Docker / development | environment variables set at container creation |
| Proxmox / production | `smbios1: serial=<base64 JSON>`, read back from `/sys/class/dmi/id/product_serial` |

Proxmox cannot use cloud-init snippets here: those need filesystem access to
Proxmox host storage, and the control plane's API token is deliberately scoped
to VM lifecycle only, so a compromised control plane cannot write arbitrary
files to a hypervisor. The SMBIOS serial is the one per-VM field the lifecycle
API can set.

The decoded payload is:

```json
{
  "computer_id": "01a0844a-...",
  "endpoint": "https://hyperwake.ai",
  "registration_token": "<single use>"
}
```

**The environment wins field by field**, with SMBIOS as the fallback. Per-field
precedence means one value can be overridden for debugging without discarding
the rest of an injected payload. `HYPERWAKE_SMBIOS_PATH` overrides the DMI path
and exists so the decode is testable without hardware.

The payload carries no shared secret: the registration token is single-use and
unique per machine, so reading another machine's DMI gains an attacker nothing
replayable.

## Configuration

| Variable | Default | Purpose |
|---|---|---|
| `HYPERWAKE_ENDPOINT` | — | control plane base URL. Required unless supplied via SMBIOS |
| `HYPERWAKE_REGISTRATION_TOKEN` | — | single-use; only needed on first boot |
| `HYPERWAKE_COMPUTER_ID` | from SMBIOS | the control plane's id for this machine |
| `HYPERWAKE_MACHINE_ID` | generated | per-machine identity |
| `HYPERWAKE_SMBIOS_PATH` | `/sys/class/dmi/id/product_serial` | override for testing |
| `HYPERWAKE_STATE_DIR` | `/var/lib/hyperwake` | must be on the persistent disk |
| `HYPERWAKE_HEARTBEAT_INTERVAL` | `15` | seconds; the control plane may override |
| `HYPERWAKE_HTTP_TIMEOUT` | `15s` | bounds every call home |
| `HYPERWAKE_SHUTDOWN_COMMAND` | `/sbin/shutdown -h now` | how to stop the guest |

### The state directory must persist

The registration token is single-use. A machine that lost its stored credential
could never re-register and would be orphaned. The image therefore points
`HYPERWAKE_STATE_DIR` at a path on the persistent volume.

## Design notes

**Backoff is bounded** (2s → 2min ceiling). A control plane outage must not
turn into a fleet that never reports again.

**Heartbeats are jittered** by up to 20%, so a fleet that boots together does
not heartbeat in lockstep.

**A rejected credential is terminal.** The daemon clears its local token and
exits rather than hammering the endpoint with a dead credential.

**A replayed registration token is fatal, not retried.** It can never succeed,
and retrying looks like an attack from the control plane's side.

**Session counting walks `/proc`** rather than parsing `utmp`, which would mean
cgo or a hand-rolled binary layout that differs by libc. The per-connection
`sshd: user@pts/N` processes are counted; the listener is not, or every idle
machine would look busy and never auto-stop.

## Deployment

`packaging/hyperwake-guest.service` is the systemd unit for the real image. The
development container starts the binary from its entrypoint instead.
