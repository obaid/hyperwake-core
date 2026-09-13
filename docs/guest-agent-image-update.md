# Staging a guest-agent image update

The restored-credential recovery fix must reach both future computers and older
snapshots. A snapshot can contain an old `/usr/local/bin/mola-guest`, so changing
only the cached factory image does not repair restores of that snapshot.

This update has two parts:

- Build and stage a private factory-image revision containing the fixed daemon
  and its verified sidecar binary/manifest.
- Refresh that managed daemon in a verified temporary restore disk before the
  native runtime atomically publishes the restored disk.

The platform-agent refresh is an explicit exception to byte-for-byte whole-disk
restore. Snapshot integrity is verified first; then only the platform-managed
`/usr/local/bin/mola-guest` is replaced. Customer files retain the snapshot's
contents. Kernel/initramfs artifacts remain unchanged. Restore still leaves the
computer stopped and reseeds its per-computer registration identity.

## Choose the correct source

Use a known unbooted factory image, never a customer VM disk or a running
computer export. The current pilot's `/var/lib/mola-cloud/image` originally
pointed to `/var/lib/mola/image`, shared with the original evaluation service.
Do not patch that shared directory in place. Create a private versioned image
for the cloud service and keep the original image as the rollback copy.

The x86 builder is `bin/native-prepare` → `runtime/native/prepare_x86.py` →
`bin/build-images`; it assembles the ext4 root using the Docker image's files.
The hotfix script reuses that ext4 format and the existing kernel/initramfs,
without rebuilding/upgrading Arch packages. A full rebuild can change the kernel
and create a mismatch when an older retained disk next wakes, so it is a separate
image release rather than a binary-only maintenance update.

## Build and verify the binary

Run the guest Go tests, then build a static Linux binary for the image's CPU
architecture from the reviewed source. For example:

```sh
cd guest/mola-guest
go test ./... -race
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath \
  -ldflags '-s -w -X github.com/obaid/mola-core/guest/mola-guest/internal/config.Version=0.1.0-restore401' \
  -o /private/staging/mola-guest-linux-amd64 ./cmd/mola-guest
sha256sum /private/staging/mola-guest-linux-amd64
```

Record the exact source revision, any uncommitted patch digest, build command,
version and resulting binary SHA-256 in the deployment record. A matching version
label alone is insufficient. Transfer the binary over the authenticated operator
connection and verify the same SHA-256 on the build host. Do not upload credentials
or build from a guest's executable.

## Stage without activating

On a Linux tools host with `python3`, GNU `cp`, `debugfs` and `e2fsck`:

```sh
python3 bin/stage-guest-agent-update \
  --image /var/lib/mola/image \
  --guest-binary /private/staging/mola-guest-linux-amd64 \
  --expected-binary-sha256 <verified-binary-sha256> \
  --architecture x86_64 \
  --version omarchy-agent-0.1.0-restore401-<short-digest> \
  --out /var/lib/mola-cloud/images/omarchy-agent-0.1.0-restore401-<short-digest>
```

The output must be a fresh directory, outside the source. The script refuses VM
metadata, known enrollment/SSH state and nonempty machine IDs; these checks do
not turn arbitrary user disks into safe factory images. It copies only the
factory artifacts, patches the staged daemon with `debugfs`, checks regular-file
placement and ownership/mode, verifies extracted daemon bytes against the
operator digest, and performs read-only filesystem consistency checks. It never
mounts a filesystem or changes active symlinks.

A successful stage includes:

- `root.ext4`, unchanged `vmlinuz-linux` and `initramfs-linux.img`.
- `mola-guest` and `guest-agent.json`, the trusted restore sidecar.
- `installed.json`, naming the new local revision and raw artifact SHA-256s.
- `agent-update.json`, recording old/new digests and `activation: not-activated`.

Existing provenance/license sidecars are retained when present. The installed
manifest describes local raw artifacts; it is not a published compressed release
manifest. `bin/publish-image` is a separate packaging/publishing tool. Do not reuse
an old compressed staging directory or publish a release as part of this hotfix.

The original source's digests are checked again after staging. It remains the
rollback image. `STAGING_INCOMPLETE` or `STAGING_FAILED` means the directory must
not be activated; the native runtime refuses those markers.

## Activate only after validation

Test a disposable VM against the staged image, including registration, SSH,
desktop, snapshot restore and an older snapshot containing the pre-fix daemon.
Confirm user-file recovery and current-daemon readiness. Verify available disk
space for the staged image and atomic restore workspace.

The pilot currently uses one configured `MOLA_IMAGE_REF` as a compatibility
family. Keep that semantic template alias aligned with CP image rows when making
this binary-only update; record the distinct hotfix revision in the image
manifest. Introducing a new template alias requires compatible host/image catalog
handling for existing computers and migration targets, and must not be silently
changed on one side alone.

Before activation, record the existing cloud image symlink target. With cloud
admissions paused, install ownership/access permissions on the new directory,
then atomically replace only `/var/lib/mola-cloud/image` with a symlink to the
validated version. Require the old cloud image entry to be a symlink before
replacing it. Do not modify `/var/lib/mola/image` or the original service.

Restart the cloud core so its native runner resolves the new image path. The
Linux runtime's durable QMP sockets preserve already running VMs across this warm
restart. Their disk files are independent clones and are not changed by image
activation. Existing running/retained computers need their own managed-daemon
update or a restore through the refreshed path; changing the factory image is
not an in-place guest updater.

## Restore enforcement

Hosted core (`MOLA_HOST_API=1`) enables `guest_agent_refresh` in native runtime
configuration by default. The helper reads only `<operator image>/guest-agent.json`
and the fixed `<operator image>/mola-guest` sidecar. It validates format,
architecture, static ELF form and SHA-256, and refuses symlinked guest destination
or parent directories. No API request chooses a host path, guest target or URL.

Refresh occurs after both snapshot checksums pass and before the temporary disk
replaces the current disk. Failure preserves the original disk and leaves the
operation pending for reconciliation. A missing sidecar also fails closed.
`MOLA_GUEST_AGENT_REFRESH=0` is an explicit operator compatibility escape, not the
supported hosted restore configuration; it reintroduces the old-snapshot agent
problem. Activate the sidecar-bearing image before deploying/enabling the new
hosted restore path.

The Go fix also permits a restored cached credential rejected with HTTP 401 to
re-enroll using its current bootstrap identity. Neither image staging nor the
refresh helper copies account keys, machine bearer tokens or enrollment keys
from a user's disk into the factory image.

## Verification

The real ext4 fixture at `test/native-agent-refresh-fixture.py` uses temporary
files only. On a disposable Linux tools host, invoke it with the repository path
and the verified static x86 daemon binary:

```sh
python3 test/native-agent-refresh-fixture.py /path/to/mola-core /private/staging/mola-guest-linux-amd64
```

It checks source immutability, staged kernel identity, old-snapshot daemon
replacement, preserved user bytes, checksum-failure atomicity and symlink
confinement. The normal core suite also checks that missing sidecars leave the
current disk intact. No real VM disks are needed for these tests.
