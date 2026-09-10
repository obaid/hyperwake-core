# Run Hyperwake on your computer

Your agent runs on your usual machine. Hyperwake gives it separate Omarchy
computers through one API. The service runs in Docker; a native helper runs
hardware-accelerated virtual machines where Docker cannot provide that access.

This is a source preview. Mac has a measured local runtime test; Linux and
Windows need community hardware testing. A successful CI run does not establish
that a desktop boots on those platforms.

| Host                    | Computer runtime                                 | Verification                                                                       |
| ----------------------- | ------------------------------------------------ | ---------------------------------------------------------------------------------- |
| Apple Silicon Mac       | ARM64 Omarchy, QEMU + Apple Hypervisor Framework | Local boot, API, SSH, browser, screenshots, stop/start and file persistence tested |
| Linux x86_64            | x86 Omarchy, Docker-managed QEMU + KVM           | Builds and automated tests; real hardware acceptance needed                        |
| Windows 10/11 x86_64    | x86 Omarchy, native QEMU + WHPX                  | Experimental adapter and image preparation; real hardware acceptance needed        |
| Intel Mac / Windows ARM | No packaged runtime yet                          | Unsupported in this preview                                                        |

## Apple Silicon Mac

Install Docker Desktop, Python 3.10 or newer, Git, and
[Try Omarchy](https://github.com/omacom/try-omarchy/releases) in `/Applications`.
The preview uses its signed QEMU/VirGL runtime and verifies its factory-image
checksums. The official v0.3.0 release download reports app bundle version 0.4.0;
that is the runtime used for the Mac tests. Keep that app installed;
Hyperwake references its runtime. It creates **separate Hyperwake disks** and
does not use or reset your Try Omarchy personal VM. Host/guest clipboard synchronization is disabled in this preview;
copy/paste inside the guest and the file API remain available.

Allow at least 16 GiB system RAM and 40 GiB free disk. The initial image and
application builds take longer than five minutes.

```sh
git clone https://github.com/obaid/hyperwake.git
cd hyperwake
python3 bin/setup --build
```

Setup imports the ARM image, builds the service, starts the local VM helper,
and prints your URL and credentials-file location. If port 8080 is occupied it
chooses a free port through 8090. For an explicit port, pass
`--url http://localhost:8088` on your first setup.

```sh
python3 bin/hyperwake login --credentials .hyperwake/credentials/customer.json
python3 bin/hyperwake doctor
```

Give your agent the [quickstart prompt](quickstart.md). Its commands are the
same on every host. No model subscription is included or required by Hyperwake;
you use your own agent and model access.

## Linux x86_64

Use Docker Engine with Compose v2, Python 3.10+, Git and OpenSSH. Your user
must be able to read/write `/dev/kvm`. A VPS needs nested virtualization exposed
by its provider. Start with 16 GiB RAM and 80 GiB free disk for the source build.

```sh
python3 bin/setup --build
```

For a remote server, use `--url https://computers.example.com --email you@example.com`
and follow [self-hosting](self-hosting.md). The installer selects the existing
Docker/KVM runtime automatically on Linux. No Mac helper is needed.

## Windows x86_64 — community test preview

Use Windows 10/11, Docker Desktop in **Linux containers** mode, Python 3.10+,
Git, OpenSSH Client, and enabled Windows Hypervisor Platform (WHPX). Use an NTFS
local folder under your user profile. Allow 16 GiB RAM and 80 GiB free disk.

The desktop needs a QEMU build with working VirGL graphics and WHPX CPU support,
such as the runtime used by
[Try Omarchy for Windows](https://github.com/omacom/try-omarchy-windows).
A stock QEMU executable without those features is insufficient. Installing or
enabling the Windows hypervisor may require a restart; Hyperwake does not change
Windows optional features automatically.

In PowerShell, replace the QEMU path with your installed GPU-capable runtime:

```powershell
git clone https://github.com/obaid/hyperwake.git
cd hyperwake
python bin/native-prepare --qemu 'C:\WINQ-EMU\qemu-system-x86_64.exe'
python bin/setup --runtime native --build
python bin/hyperwake login --credentials .hyperwake/credentials/customer.json
python bin/hyperwake doctor
python bin/acceptance
```

Image preparation builds Hyperwake's x86 guest through Docker and exports its
kernel, initramfs and full disk for the native runner. It does not import a
personal Windows Try Omarchy VM. This flow is implemented but has **not been
executed on Windows hardware**. WHPX boot flags, the graphics backend, file
permissions and Docker-to-host networking are explicit test gates.

## Runtime operation

Setup starts the native helper for the current login session. After a host
restart, run `python3 bin/setup` again (`python` on Windows), or start it in a
terminal for troubleshooting:

```sh
python3 bin/native-host --config .hyperwake/native/config.json
```

The helper listens on `127.0.0.1:19380` and requires the random token in
`.hyperwake/native/host.token`. Docker reaches it through `host.docker.internal`.
Guest SSH/VNC ports bind loopback. Keep host tokens and customer credentials out
of prompts and reports. Do not expose the helper directly to your network.

Native disks and logs live under `.hyperwake/native/machines/COMPUTER_ID`.
Stopping retains the whole disk, not running application memory. Chromium may
show a restore-pages prompt after restart; close applications before stopping
when you need them to save their session cleanly. Preserving deletion moves the disk into
`.hyperwake/native/retained`; reattaching an orphaned retained disk is currently
an operator recovery task. Snapshot support is not implemented. A native QEMU
window may appear for each running Mac/Windows computer, even when the agent
uses the API.

The control plane initially budgets 4 vCPUs, 8 GiB RAM and 100 GiB of disks.
At the native profile's 4 vCPUs/40 GiB per computer, that permits one running
computer and two persistent disks by default. Retained disks also consume storage.
The native helper has a separate ceiling of two running computers and 8 GiB RAM.
The lower of the control-plane budget and helper limits applies. Adjust the
host capacity in the admin dashboard before increasing concurrency.
Adjust `max_running` and `max_memory_mb` in its configuration to fit your host,
and restart the helper. Leave memory for Docker and your own apps. Stop computers
through the public API before shutting down the service, removing the checkout,
or backing up their disks. Docker Compose does not stop native VMs.

After a Windows helper restart, an unreachable QMP endpoint with a running marker
is reported as unknown, not stopped. Inspect the process and disk before recovery;
do not remove `running.marker` while QEMU may still be using the disk.

## Help test a release

Run `python3 bin/acceptance` (`python` on Windows). It creates a computer, checks
the real desktop and browser, exercises input and files, then tests stop/start
persistence. It leaves the computer stopped for inspection. To repeat without allocating
another disk, use `python3 bin/acceptance --computer COMPUTER_ID`. Run one trial
at a time, and wait for its cleanup to finish before repeating. Review the generated
screenshot yourself and attach the redacted report to a **Platform test** issue.
Then repeat the natural-language prompt in your agent and describe the result.

Report the commit, host OS/CPU/RAM, Docker version, QEMU runtime/version, agent,
setup duration, first-task duration, and the first failing step. Never attach
`server.env`, `customer.json`, `host.token`, SSH keys, a full VM disk, or an
unreviewed agent transcript. We need Linux/KVM and Windows/WHPX results before
calling those paths verified. You can invite testers to a public preview without
claiming production readiness.
