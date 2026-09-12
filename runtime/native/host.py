"""Authenticated, local QEMU supervisor. The control plane owns policy and billing.

Only an operator-selected image/runtime may run. Request data never selects a
host path, command, display backend, or QEMU argument. Guest ports bind loopback.
"""
import argparse
import hashlib
import hmac
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import platform
import re
import secrets
import shlex
import shutil
import socket
import struct
import subprocess
import threading
import time

ID = re.compile(r'[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}')


def write_json(path, value):
    temporary = path.with_suffix('.new')
    with temporary.open('w', encoding='utf-8') as stream:
        json.dump(value, stream, indent=2)
    os.chmod(temporary, 0o600)
    temporary.replace(path)


def seed_disk(path, text):
    """A FAT16 superfloppy readable by Linux; no host mkfs dependency."""
    data = text.encode('utf-8')
    if len(data) > 65536: raise ValueError('Identity payload is too large')
    sectors, fat_sectors, root_sectors, cluster_bytes = 32768, 32, 32, 2048
    boot = bytearray(512)
    boot[:11] = b'\xeb\x3c\x90HYPERWAK'
    struct.pack_into('<HBHBHHBHHHII', boot, 11, 512, 4, 1, 2, 512, sectors, 0xf8, fat_sectors, 32, 64, 0, 0)
    boot[36:39] = b'\x80\x00\x29'
    boot[43:54] = b'HYPERWAKE  '
    boot[54:62] = b'FAT16   '
    boot[510:] = b'\x55\xaa'
    fat = bytearray(fat_sectors * 512)
    struct.pack_into('<HH', fat, 0, 0xfff8, 0xffff)
    count = max(1, (len(data) + cluster_bytes - 1) // cluster_bytes)
    for index in range(count):
        struct.pack_into('<H', fat, (index + 2) * 2, 0xffff if index == count - 1 else index + 3)
    entry = bytearray(root_sectors * 512)
    entry[:11] = b'IDENTITYENV'
    entry[11] = 0x20
    struct.pack_into('<H', entry, 26, 2)
    struct.pack_into('<I', entry, 28, len(data))
    with path.open('wb') as disk:
        disk.write(boot); disk.write(fat); disk.write(fat); disk.write(entry); disk.write(data)
        disk.truncate(sectors * 512)
    os.chmod(path, 0o600)


def accelerator(system, host_arch, guest_arch):
    normalized = {'arm64': 'aarch64', 'AMD64': 'x86_64', 'amd64': 'x86_64'}.get(host_arch, host_arch)
    if normalized != guest_arch:
        raise ValueError('Guest and host CPU architecture must match; this runner does not silently emulate CPUs')
    if system == 'Darwin' and guest_arch == 'aarch64': return 'hvf'
    if system == 'Linux': return 'kvm'
    if system == 'Windows' and guest_arch == 'x86_64': return 'whpx'
    raise ValueError('Unsupported native host/guest combination')


def free_port():
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        return sock.getsockname()[1]


class Runner:
    def __init__(self, config_path):
        self.config_path = config_path.resolve()
        self.root = self.config_path.parent
        self.config = json.loads(config_path.read_text())
        self.arch = self.config['architecture']
        self.accel = accelerator(platform.system(), platform.machine(), self.arch)
        self.qemu = Path(self.config['qemu']).resolve()
        self.image = Path(self.config['image']).resolve()
        for path in [self.qemu, self.image, self.root]:
            if ',' in str(path) or '\n' in str(path): raise ValueError('QEMU paths cannot contain commas or newlines')
        for name in ['root.ext4', 'vmlinuz-linux', 'initramfs-linux.img']:
            if not (self.image / name).is_file(): raise ValueError('Missing prepared image artifact: ' + name)
        if self.accel == 'kvm' and not os.access('/dev/kvm', os.R_OK | os.W_OK):
            raise ValueError('Linux requires usable /dev/kvm')
        capabilities = subprocess.check_output([str(self.qemu), '-accel', 'help'], text=True)
        if self.accel not in capabilities.split(): raise ValueError('QEMU lacks ' + self.accel)
        self.machines = self.root / 'machines'
        self.machines.mkdir(parents=True, exist_ok=True, mode=0o700)
        self.sockets = Path('/tmp') / ('mola-' + str(os.getuid()) + '-' + hashlib.sha256(str(self.root).encode()).hexdigest()[:12]) if platform.system() != 'Windows' else self.root
        self.sockets.mkdir(mode=0o700, exist_ok=True)
        if self.sockets.is_symlink() or (platform.system() != 'Windows' and self.sockets.stat().st_uid != os.getuid()):
            raise ValueError('Unsafe socket directory')
        os.chmod(self.sockets, 0o700)
        self.lock = threading.RLock()
        self.processes = {}

    def folder(self, identifier):
        if not isinstance(identifier, str) or not ID.fullmatch(identifier): raise ValueError('Invalid computer id')
        folder = self.machines / identifier
        if folder.is_symlink(): raise ValueError('Machine directory must not be a symlink')
        return folder

    def metadata(self, identifier):
        data = json.loads((self.folder(identifier) / 'machine.json').read_text())
        if data.get('id') != identifier or data.get('managed_by') != 'mola-native-v1':
            raise ValueError('Refusing unmanaged machine')
        return data

    def qmp(self, data, command):
        if platform.system() == 'Windows':
            connection = socket.create_connection(('127.0.0.1', data['qmp_port']), timeout=3)
        else:
            connection = socket.socket(socket.AF_UNIX)
            connection.settimeout(3)
            connection.connect(str(self.sockets / (data['id'] + '.sock')))
        with connection, connection.makefile('rwb') as wire:
            def response():
                for _ in range(100):
                    line = wire.readline(65536)
                    if not line: raise OSError('QMP closed the connection')
                    item = json.loads(line)
                    if 'return' in item: return item['return']
                    if 'error' in item: raise ValueError('QMP rejected command: ' + str(item['error']))
                raise OSError('Too many QMP events')
            if 'QMP' not in json.loads(wire.readline(65536)): raise OSError('Not a QMP socket')
            wire.write(b'{"execute":"qmp_capabilities"}\n'); wire.flush(); response()
            wire.write(b'{"execute":"query-name"}\n'); wire.flush()
            if response().get('name') != 'mola-' + data['id']:
                raise ValueError('QMP machine identity mismatch')
            wire.write(json.dumps({'execute': command}).encode() + b'\n'); wire.flush()
            return response()

    def status(self, data):
        process = self.processes.get(data['id'])
        if process is not None and process.poll() is not None:
            self.processes.pop(data['id'], None)
            (self.folder(data['id']) / 'running.marker').unlink(missing_ok=True)
            return 'stopped'
        try:
            state = self.qmp(data, 'query-status')['status']
            return 'running' if state == 'running' else 'unknown'
        except (OSError, KeyError):
            # A live child without its management socket is never declared stopped.
            if process is not None: return 'starting'
            if (self.folder(data['id']) / 'running.marker').exists(): return 'unknown'
            if platform.system() != 'Windows' and (self.sockets / (data['id'] + '.sock')).exists(): return 'unknown'
            return 'stopped'

    def describe(self, identifier):
        data = self.metadata(identifier)
        status = self.status(data)
        return {'provider_vm_id': identifier, 'status': status, 'disk_id': identifier,
                'display_host': self.config.get('connect_host', 'host.docker.internal'),
                'display_port': data['vnc_port'], 'ssh_host': self.config.get('connect_host', 'host.docker.internal'),
                'ssh_port': data['ssh_port'], 'meta': {'runtime': self.accel, 'architecture': self.arch}}

    def create(self, spec):
        identifier = spec['computer_id']
        folder = self.folder(identifier)
        if (folder / 'machine.json').exists(): return self.describe(identifier)
        if folder.exists(): raise ValueError('Incomplete machine directory; inspect it before retrying')
        cpus, memory, disk_gb = spec['vcpus'], spec['memory_mb'], spec['disk_gb']
        if not all(type(n) is int for n in [cpus, memory, disk_gb]): raise ValueError('Resources must be integers')
        if not (1 <= cpus <= (8 if self.arch == 'aarch64' else 32) and 1024 <= memory <= self.config.get('max_memory_mb', 8192) and 16 <= disk_gb <= 1024):
            raise ValueError('Requested resources exceed native runner limits')
        keys = spec.get('authorized_keys', [])
        if not isinstance(keys, list) or any(not isinstance(key, str) or len(key) > 16384 for key in keys):
            raise ValueError('Invalid authorized keys')
        fields = {'HYPERWAKE_ENDPOINT': json.loads(self.config_path.read_text())['guest_endpoint'],
                  'HYPERWAKE_REGISTRATION_TOKEN': spec['registration_token'],
                  'HYPERWAKE_COMPUTER_ID': identifier, 'HYPERWAKE_MACHINE_NAME': spec['name'],
                  'HYPERWAKE_AUTHORIZED_KEYS': '\n'.join(keys)}
        if any(not isinstance(value, str) or '\x00' in value for value in fields.values()):
            raise ValueError('Invalid identity')
        payload = ''.join(key + '=' + shlex.quote(value) + '\n' for key, value in fields.items())
        if len(payload.encode()) > 65536: raise ValueError('Identity payload is too large')
        # Preserved disks live outside active machine metadata and are never overwritten.
        retained = self.root / 'retained' / (identifier + '.ext4')
        if retained.exists():
            raise ValueError('A retained disk exists; explicit recovery is required')
        folder.mkdir(mode=0o700)
        disk = folder / 'root.ext4'
        if platform.system() == 'Darwin':
            subprocess.run(['cp', '-c', str(self.image / 'root.ext4'), str(disk)], check=True)
        else:
            # Copy in sparse blocks to avoid allocating the image's free space.
            if platform.system() == 'Windows':
                disk.touch()
                subprocess.run(['fsutil', 'sparse', 'setflag', str(disk)], check=True, stdout=subprocess.DEVNULL)
            with (self.image / 'root.ext4').open('rb') as source, disk.open('wb') as target:
                for chunk in iter(lambda: source.read(1024 * 1024), b''):
                    if chunk.strip(b'\0'): target.write(chunk)
                    else: target.seek(len(chunk), 1)
                target.truncate()
        with disk.open('r+b') as stream: stream.truncate(max(disk.stat().st_size, disk_gb * 1024**3))
        seed_disk(folder / 'identity.img', payload)
        ports = set()
        while len(ports) < 3: ports.add(free_port())
        ssh_port, vnc_port, qmp_port = sorted(ports)
        data = {'managed_by': 'mola-native-v1', 'id': identifier, 'vcpus': cpus, 'memory_mb': memory,
                'ssh_port': ssh_port, 'vnc_port': vnc_port, 'qmp_port': qmp_port}
        write_json(folder / 'machine.json', data)
        return self.describe(identifier)

    def command(self, data):
        folder = self.folder(data['id'])
        if self.arch == 'aarch64':
            # GICv2 is what the packaged runtime supports; stock QEMU on HVF
            # requires GICv3. Configurable so either can drive the same image.
            machine = 'virt,accel=' + self.accel + ',gic-version=' + str(self.config.get('gic_version', 2))
            cpu = 'host,pmu=off'
        else:
            machine = 'q35,accel=' + self.accel
            cpu = self.config.get('cpu', 'host')
        qmp = ('tcp:127.0.0.1:' + str(data['qmp_port'])) if platform.system() == 'Windows' else 'unix:' + str(self.sockets / (data['id'] + '.sock'))
        args = [str(self.qemu), '-name', 'mola-' + data['id'], '-machine', machine, '-cpu', cpu,
                '-smp', str(data['vcpus']), '-m', str(data['memory_mb']), '-nodefaults',
                '-kernel', str(self.image / 'vmlinuz-linux'), '-initrd', str(self.image / 'initramfs-linux.img'),
                '-append', self.config['kernel_args'], '-qmp', qmp + ',server=on,wait=off',
                '-drive', f'file={folder / "root.ext4"},if=none,id=root,format=raw', '-device', 'virtio-blk-pci,drive=root',
                '-drive', f'file={folder / "identity.img"},if=none,id=identity,format=raw,readonly=on', '-device', 'virtio-blk-pci,drive=identity',
                '-netdev', f'user,id=net,hostfwd=tcp:127.0.0.1:{data["ssh_port"]}-:22,hostfwd=tcp:127.0.0.1:{data["vnc_port"]}-:5900',
                '-device', 'virtio-net-pci,netdev=net,romfile=', '-device', self.config['gpu'], '-display', self.config['display'],
                '-device', 'virtio-keyboard-pci', '-device', 'virtio-tablet-pci', '-device', 'virtio-serial-pci',
                '-chardev', f'file,id=console,path={folder / "console.log"}', '-device', 'virtconsole,chardev=console',
                '-serial', 'none', '-monitor', 'none']
        return args

    def start(self, identifier):
        data = self.metadata(identifier)
        current = self.status(data)
        if current in ('running', 'starting'): return self.describe(identifier)
        if current != 'stopped': raise ValueError('Machine state is uncertain; inspect it before starting')
        states = self.list()
        running = sum(value in ('running', 'starting', 'unknown') for value in states.values())
        memory = sum(self.metadata(key)['memory_mb'] for key, value in states.items() if value != 'stopped')
        if memory + data['memory_mb'] > self.config.get('max_memory_mb', 8192):
            raise ValueError('Native host memory limit reached')
        if running >= self.config.get('max_running', 2): raise ValueError('Native host running-computer limit reached')
        folder = self.folder(identifier)
        (self.sockets / (identifier + '.sock')).unlink(missing_ok=True)
        log = (folder / 'runtime.log').open('ab')
        options = {'creationflags': subprocess.CREATE_NEW_PROCESS_GROUP} if platform.system() == 'Windows' else {'start_new_session': True}
        try:
            if platform.system() == 'Windows': (folder / 'running.marker').touch(mode=0o600)
            self.processes[identifier] = subprocess.Popen(self.command(data), stdin=subprocess.DEVNULL, stdout=log, stderr=log, **options)
        finally: log.close()
        time.sleep(0.3)
        if self.processes[identifier].poll() is not None:
            raise ValueError('QEMU exited during launch; inspect the machine runtime.log')
        return self.describe(identifier)

    def stop(self, identifier, force=False):
        data = self.metadata(identifier)
        if self.status(data) == 'stopped': return
        self.qmp(data, 'quit' if force else 'system_powerdown')

    def destroy(self, identifier, delete_disk):
        folder = self.folder(identifier)
        retained = self.root / 'retained' / (identifier + '.ext4')
        if not folder.exists():
            if delete_disk: retained.unlink(missing_ok=True)
            return
        data = self.metadata(identifier)
        if self.status(data) != 'stopped': raise ValueError('Stop the computer before deleting it')
        if not delete_disk:
            retained.parent.mkdir(exist_ok=True, mode=0o700)
            if retained.exists(): raise ValueError('Retained disk already exists')
            (folder / 'root.ext4').rename(retained)
        shutil.rmtree(folder)

    def list(self):
        return {folder.name: self.status(self.metadata(folder.name)) for folder in self.machines.iterdir()
                if ID.fullmatch(folder.name) and (folder / 'machine.json').exists()}


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args): pass  # No request bodies or credentials in logs.

    def do_GET(self): self.dispatch()
    def do_POST(self): self.dispatch()
    def do_DELETE(self): self.dispatch()

    def dispatch(self):
        try:
            if not hmac.compare_digest(self.headers.get('Authorization', ''), 'Bearer ' + self.server.token):
                return self.reply(401, {'error': 'Unauthorized'})
            if self.headers.get('Origin'):
                return self.reply(403, {'error': 'Browser origins are not accepted'})
            size = int(self.headers.get('Content-Length', '0'))
            if size < 0 or size > 131072: return self.reply(413, {'error': 'Request too large'})
            self.connection.settimeout(10)
            payload = json.loads(self.rfile.read(size)) if size else {}
            parts = self.path.strip('/').split('/')
            runner = self.server.runner
            with runner.lock:
                if self.command == 'GET' and parts == ['health']:
                    result = {'ready': True, 'architecture': runner.arch, 'accelerator': runner.accel}
                elif parts == ['machines'] and self.command == 'GET': result = runner.list()
                elif parts == ['machines'] and self.command == 'POST': result = runner.create(payload)
                elif len(parts) == 2 and parts[0] == 'machines' and self.command == 'GET': result = runner.describe(parts[1])
                elif len(parts) == 2 and parts[0] == 'machines' and self.command == 'DELETE':
                    if type(payload.get('delete_disk')) is not bool: raise ValueError('delete_disk must be explicitly true or false')
                    runner.destroy(parts[1], payload['delete_disk']); result = {'ok': True}
                elif len(parts) == 3 and parts[0] == 'machines' and self.command == 'POST':
                    if parts[2] == 'start': result = runner.start(parts[1])
                    elif parts[2] in ('shutdown', 'force-stop'):
                        runner.stop(parts[1], parts[2] == 'force-stop'); result = {'ok': True}
                    else: return self.reply(404, {'error': 'Unknown operation'})
                else: return self.reply(404, {'error': 'Unknown operation'})
            self.reply(200, result)
        except FileNotFoundError: self.reply(404, {'error': 'Machine or runtime artifact not found'})
        except (ValueError, KeyError, TypeError) as error: self.reply(422, {'error': str(error)})
        except Exception as error:
            print(type(error).__name__ + ': ' + str(error), flush=True)
            self.reply(503, {'error': 'Native runtime failed; inspect host and machine logs'})

    def reply(self, status, payload):
        body = json.dumps(payload).encode()
        self.send_response(status)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(body)))
        self.end_headers()
        self.wfile.write(body)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--config', type=Path, required=True)
    parser.add_argument('--port', type=int, default=19380)
    args = parser.parse_args()
    os.umask(0o077)
    runner = Runner(args.config)
    token_path = runner.root / 'host.token'
    if not token_path.exists():
        with token_path.open('x') as stream: stream.write(secrets.token_hex(32))
    token = token_path.read_text().strip()
    if len(token) < 32: raise ValueError('Host token must contain at least 32 characters')
    server = ThreadingHTTPServer(('127.0.0.1', args.port), Handler)
    server.daemon_threads = True
    server.runner, server.token = runner, token
    print(f'Mola native host listening on 127.0.0.1:{args.port} ({runner.accel}/{runner.arch})', flush=True)
    try: server.serve_forever()
    except KeyboardInterrupt: pass
    finally: server.server_close()
    # Existing VMs retain their QMP sockets and disks. Restarting this service
    # reconnects to them; it never guesses ownership from an operating-system PID.


if __name__ == '__main__': main()
