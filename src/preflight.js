import { execFileSync } from 'node:child_process';
import { existsSync } from 'node:fs';
import { join } from 'node:path';
import { arch, platform } from 'node:os';
import { statePath } from './paths.js';

function has(command, args = ['--version']) {
  try {
    execFileSync(command, args, { stdio: 'ignore', timeout: 10_000 });
    return true;
  } catch {
    return false;
  }
}

/** Absolute path for a command on PATH, or the name unchanged if not found. */
function which(command) {
  try {
    return execFileSync('/usr/bin/which', [command], { encoding: 'utf8', timeout: 5000 }).trim() || command;
  } catch {
    return command;
  }
}

function qemuAccelerators(qemu) {
  try {
    return execFileSync(qemu, ['-accel', 'help'], { encoding: 'utf8', timeout: 10_000 })
      .split('\n').map((line) => line.trim()).filter(Boolean);
  } catch {
    return [];
  }
}

/**
 * What this host can actually do, measured rather than assumed.
 *
 * Omarchy is a Wayland desktop: its compositor needs a real accelerator and a
 * display device. Docker alone is not sufficient on macOS, where the Linux VM
 * exposes neither KVM nor a DRM node. Saying so here is far kinder than a
 * confusing failure four minutes into a boot.
 */
export function inspectHost() {
  const system = platform();
  const cpu = arch();
  const report = {
    platform: system,
    arch: cpu,
    python: has('python3', ['--version']),
    docker: has('docker', ['version', '--format', '{{.Server.Version}}']),
    ssh: has('ssh', ['-V']),
    kvm: existsSync('/dev/kvm'),
    qemu: null,
    accelerator: null,
    ready: false,
    reason: null,
    // A host that can run virtual machines still cannot make one without a
    // base image to clone. Reported separately because the remedy is
    // different: one is "install something", the other is "build an image".
    image: existsSync(join(statePath('image'), 'root.ext4')),
    imagePath: statePath('image'),
  };

  // An operator may point at any QEMU they trust. Useful for testing a stock
  // build, and the escape hatch when the packaged runtime is not installed.
  const override = process.env.HYPERWAKE_QEMU;
  if (override) {
    const accelerators = qemuAccelerators(override);
    const wanted = system === 'darwin' ? 'hvf' : 'kvm';
    if (!existsSync(override)) report.reason = `HYPERWAKE_QEMU points at ${override}, which does not exist.`;
    else if (!accelerators.includes(wanted)) report.reason = `${override} does not offer the ${wanted} accelerator.`;
    else { report.qemu = override; report.accelerator = wanted; }
  } else if (system === 'darwin' && cpu === 'arm64') {
    // Any QEMU will do. It signs itself with the com.apple.security.hypervisor
    // entitlement during its own build (ad-hoc, `codesign -s -`), so the
    // Hypervisor framework needs no Apple Developer ID and no bundled runtime.
    // Point HYPERWAKE_QEMU at a different build to use one.
    const candidates = [
      'qemu-system-aarch64',
      '/opt/homebrew/bin/qemu-system-aarch64',
      '/usr/local/bin/qemu-system-aarch64',
    ];
    const found = candidates.find((path) => qemuAccelerators(path).includes('hvf'));
    if (found) {
      // Store an absolute path: the runtime resolves this against its own
      // working directory, where a bare command name means nothing.
      report.qemu = found.includes('/') ? found : which(found);
      report.accelerator = 'hvf';
    } else {
      report.reason = 'No QEMU with the hvf accelerator. Try `brew install qemu`.';
    }
  } else if (system === 'linux' && cpu === 'x64') {
    const qemu = ['qemu-system-x86_64', '/usr/bin/qemu-system-x86_64'].find((path) => has(path));
    if (!qemu) report.reason = 'Install qemu-system-x86_64.';
    else if (!report.kvm) report.reason = 'No /dev/kvm. Enable virtualisation, or run on bare metal rather than inside a VM.';
    else { report.qemu = qemu.includes('/') ? qemu : which(qemu); report.accelerator = 'kvm'; }
  } else {
    report.reason = `No packaged runtime for ${system}/${cpu}. Supported: macOS arm64, Linux x86_64.`;
  }

  if (!report.python) report.reason ??= 'Install Python 3.10 or newer.';
  if (!report.ssh) report.reason ??= 'Install an OpenSSH client.';

  // virtio-gpu-gl needs virglrenderer built into QEMU. Without it the guest
  // still gets a DRM device and renders in software, which works but is
  // slower. Measured rather than assumed, because it decides the guest's feel.
  if (report.qemu) {
    try {
      const devices = execFileSync(report.qemu, ['-device', 'help'], { encoding: 'utf8', timeout: 10_000 });
      report.acceleratedGraphics = devices.includes('virtio-gpu-gl-pci');
    } catch {
      report.acceleratedGraphics = false;
    }
  }

  report.hostReady = Boolean(report.qemu) && report.python && report.ssh && !report.reason;
  report.ready = report.hostReady && report.image;
  if (report.hostReady && !report.image) {
    report.reason = `No guest image at ${report.imagePath}. Build one: see https://obaid.github.io/hyperwake-core/#image`;
  }
  return report;
}
