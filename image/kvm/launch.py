#!/usr/bin/env python3
"""Launch a KVM guest. Docker owns this supervisor; customer code runs inside QEMU."""
import json
import os
from pathlib import Path
import shlex
import signal
import socket
import subprocess
import tempfile
import time

os.umask(0o077)
if not os.access('/dev/kvm', os.R_OK | os.W_OK):
    raise SystemExit('Hyperwake requires a Linux x86_64 host with usable /dev/kvm. No emulation fallback.')
base = Path('/opt/hyperwake')
data = Path('/data')
data.mkdir(exist_ok=True)
disk = data / 'root.ext4'
if not disk.exists():
    temporary = data / 'root.ext4.new'
    subprocess.run(['zstd', '-d', '--sparse', '-f', str(base / 'root.ext4.zst'), '-o', str(temporary)], check=True)
    size = max(16, int(os.environ.get('HYPERWAKE_VM_DISK_GB', '40')))
    subprocess.run(['truncate', '-s', f'{size}G', str(temporary)], check=True)
    subprocess.run(['resize2fs', str(temporary)], check=True)
    temporary.rename(disk)

with tempfile.TemporaryDirectory(prefix='hyperwake-identity-') as temp:
    seed = Path(temp)
    fields = ('HYPERWAKE_ENDPOINT', 'HYPERWAKE_REGISTRATION_TOKEN', 'HYPERWAKE_COMPUTER_ID', 'HYPERWAKE_AUTHORIZED_KEYS', 'HYPERWAKE_MACHINE_NAME')
    (seed / 'identity.env').write_text(''.join(f'{key}={shlex.quote(os.environ.get(key, ""))}\n' for key in fields))
    identity = Path('/run/identity.ext4')
    subprocess.run(['truncate', '-s', '8M', str(identity)], check=True)
    subprocess.run(['mkfs.ext4', '-q', '-F', '-d', str(seed), str(identity)], check=True)

qmp_path = '/run/hyperwake-qmp.sock'
Path(qmp_path).unlink(missing_ok=True)
# Xvfb is only the host-side OpenGL surface. The customer desktop is Hyprland
# inside the VM and sees a virtio GPU. CPU rendering avoids requiring a host GPU.
args = ['xvfb-run', '-a', '-s', '-screen 0 1440x900x24', 'qemu-system-x86_64',
        '-enable-kvm', '-machine', 'q35', '-cpu', 'host',
        '-smp', os.environ.get('HYPERWAKE_VM_CPUS', '2'),
        '-m', os.environ.get('HYPERWAKE_VM_MEMORY_MB', '4096'),
        '-kernel', str(base / 'vmlinuz-linux'), '-initrd', str(base / 'initramfs-linux.img'),
        '-append', 'root=/dev/vda rw console=ttyS0 systemd.unit=multi-user.target',
        '-drive', f'file={disk},if=virtio,format=raw',
        '-drive', f'file={identity},if=virtio,format=raw,readonly=on',
        '-device', 'virtio-vga-gl', '-display', 'sdl,gl=on',
        '-netdev', 'user,id=net,hostfwd=tcp:0.0.0.0:22-:22,hostfwd=tcp:0.0.0.0:5900-:5900',
        '-device', 'virtio-net-pci,netdev=net', '-serial', 'stdio', '-monitor', 'none',
        '-qmp', f'unix:{qmp_path},server=on,wait=off']
child = subprocess.Popen(args)


def powerdown(signum, frame):
    try:
        with socket.socket(socket.AF_UNIX) as sock:
            sock.settimeout(3)
            sock.connect(qmp_path)
            wire = sock.makefile('rwb', buffering=0)
            wire.readline()
            wire.write(b'{"execute":"qmp_capabilities"}\n')
            wire.readline()
            wire.write(b'{"execute":"system_powerdown"}\n')
    except OSError:
        # Docker's stop grace period remains the final bound; don't fake a flush.
        pass

signal.signal(signal.SIGTERM, powerdown)
signal.signal(signal.SIGINT, powerdown)
raise SystemExit(child.wait())
