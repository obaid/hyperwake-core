# Security

The current self-hosted release candidate is intended for a single operator or
trusted team. Do not expose it as a public compute service for unrelated tenants
until isolation, abuse controls, backup/restore, and recovery have been verified.

## Deployment boundaries

- The agent acts inside an Omarchy VM. The control plane and supervisor
  are trusted infrastructure. Linux uses KVM; the Mac/Windows preview uses a
  native QEMU helper with HVF/WHPX. Only the Linux supervisor receives `/dev/kvm`.
- The Docker socket is host-root-equivalent and belongs only to the control
  plane/worker. It is never mounted into a customer guest.
- The database, Redis, gateway and guest VNC ports are not published publicly.
  The HTTPS proxy is the public entry point. Private trial mode binds loopback.
- Guest networking can reach the host's reachable networks. Private-address and
  cloud-metadata egress restrictions are **not implemented** in this candidate.
  Choose an isolated server/network and do not place sensitive services nearby.
- API actions resolve computers within the authenticated account and require
  write ability. A read-only token cannot execute commands or control the screen.
- Bootstrap generates random account credentials and API keys. Never run the
  development seeder in a deployment. Keep `.hyperwake` private and backed up.
- SSH guest keys are pinned on first use with a computer-specific alias. This
  is trust-on-first-use, not an out-of-band host-key attestation scheme.
- Users and their agents can read credentials stored inside their own guest.
  No customer home folder is shared with a guest by default.

## Native helper

The native helper is trusted host infrastructure for the same operator as Docker.
It listens on loopback and authenticates every request with a generated token;
it rejects browser Origin headers. The public API enforces account authorization.
The helper accepts lifecycle specifications, never arbitrary host paths or QEMU
arguments. Guest SSH/VNC binds loopback, and QMP verifies the computer identity
before mutations. Windows QMP also uses loopback; other local user processes are
outside the isolation boundary. Use a dedicated host account on shared machines.

Keep `.hyperwake` private. POSIX files are restricted to the operator; Windows
ACL behavior still requires hardware verification. An agent with access to the
host operator's credentials or Docker socket can control that host. Native VM
disks must be stopped before a consistent offline backup. Snapshots and automated
backup/restore are not provided by this preview.

## Reporting a vulnerability

Do not put credentials or exploit details in a public issue. Use GitHub's private
vulnerability reporting for this repository when enabled. If it is unavailable,
open a minimal issue requesting a private contact route without sensitive details.
Private vulnerability reporting must be enabled before a public launch.

Report the affected revision, deployment mode, impact, and minimal reproduction.
Release status and remaining verification are in
`docs/implementation/open-source-release.md`.
