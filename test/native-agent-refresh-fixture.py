"""Real ext4 integration fixture; run in a disposable Linux tools container."""
import hashlib
import importlib.util
import importlib.machinery
import json
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

repo = Path(sys.argv[1]); binary = Path(sys.argv[2])
sys.path.insert(0, str(repo / 'runtime/native'))
import host as native
import refresh_guest_agent as patcher
loader = importlib.machinery.SourceFileLoader('stage_guest', str(repo / 'bin/stage-guest-agent-update'))
spec = importlib.util.spec_from_loader(loader.name, loader)
stager = importlib.util.module_from_spec(spec); loader.exec_module(stager)
ID = '11111111-1111-4111-8111-111111111111'
SNAPSHOT = '22222222-2222-4222-8222-222222222222'

def extracted(disk, path, output):
    output.unlink(missing_ok=True)
    patcher.debugfs(disk, 'dump ' + path + ' ' + patcher.quote(output))
    return output.read_bytes()

with tempfile.TemporaryDirectory(prefix='mola-agent-ext4-') as temporary:
    root = Path(temporary); factory = root / 'factory'; factory.mkdir()
    skeleton = root / 'skeleton'
    for directory in ['usr/local/bin', 'home/dev', 'etc/ssh']:
        (skeleton / directory).mkdir(parents=True)
    (skeleton / 'usr/local/bin/mola-guest').write_bytes(b'old-agent-before-reenrollment-fix')
    (skeleton / 'usr/local/bin/mola-guest').chmod(0o755)
    (skeleton / 'home/dev/retained-note').write_bytes(b'customer data from old snapshot')
    (skeleton / 'etc/machine-id').touch()
    disk = factory / 'root.ext4'
    with disk.open('wb') as stream: stream.truncate(64 * 1024 * 1024)
    subprocess.run(['mkfs.ext4', '-q', '-F', '-d', str(skeleton), str(disk)], check=True)
    (factory / 'vmlinuz-linux').write_bytes(b'unchanged fixture kernel')
    (factory / 'initramfs-linux.img').write_bytes(b'unchanged fixture initramfs')
    before = patcher.digest(disk)
    out = root / 'staged'
    receipt = stager.stage(factory, binary, out, 'fixture-restore401', patcher.digest(binary), 'x86_64')
    assert receipt['activated'] is False and receipt['source_unchanged'] is True
    assert patcher.digest(disk) == before
    assert extracted(out / 'root.ext4', '/usr/local/bin/mola-guest', root / 'agent') == binary.read_bytes()
    assert extracted(out / 'root.ext4', '/home/dev/retained-note', root / 'note') == b'customer data from old snapshot'
    assert (out / 'vmlinuz-linux').read_bytes() == (factory / 'vmlinuz-linux').read_bytes()
    # Restore a snapshot containing an old daemon; the platform binary is
    # refreshed before publication while the old snapshot's user data survives.
    runner = native.Runner.__new__(native.Runner)
    runner.root = root / 'runtime'; runner.root.mkdir()
    runner.machines = runner.root / 'machines'; runner.machines.mkdir()
    runner.sockets = runner.root / 'sockets'; runner.sockets.mkdir()
    runner.image = out; runner.arch = 'x86_64'; runner.accel = 'kvm'; runner.processes = {}
    runner.config = {'guest_agent_refresh': True}
    folder = runner.folder(ID); folder.mkdir()
    native.write_json(folder / 'machine.json', {'id': ID, 'managed_by': 'mola-native-v1'})
    shutil.copyfile(disk, folder / 'root.ext4')
    manifest = runner.snapshot(ID, {'snapshot_id': SNAPSHOT})
    assert manifest['sha256'] == before
    runner.restore_snapshot(ID, {'snapshot_id': SNAPSHOT})
    assert extracted(folder / 'root.ext4', '/usr/local/bin/mola-guest', root / 'restored-agent') == binary.read_bytes()
    assert extracted(folder / 'root.ext4', '/home/dev/retained-note', root / 'restored-note') == b'customer data from old snapshot'
    # A wrong operator digest makes injection fail; atomic restore must leave
    # the current disk intact, never publish the old daemon or a partial image.
    current = patcher.digest(folder / 'root.ext4')
    agent_manifest = json.loads((out / 'guest-agent.json').read_text())
    (out / 'guest-agent.json').write_text(json.dumps(dict(agent_manifest, sha256='0' * 64)))
    try: runner.restore_snapshot(ID, {'snapshot_id': SNAPSHOT})
    except subprocess.CalledProcessError: pass
    else: raise AssertionError('untrusted sidecar was installed')
    assert patcher.digest(folder / 'root.ext4') == current
    assert not (folder / 'root.restore').exists()
    (out / 'guest-agent.json').write_text(json.dumps(agent_manifest))
    # A guest-controlled symlink must never redirect the managed agent write.
    unsafe = root / 'unsafe.ext4'; shutil.copyfile(disk, unsafe)
    patcher.debugfs(unsafe, 'rm /usr/local/bin/mola-guest', writable=True)
    patcher.debugfs(unsafe, 'symlink /usr/local/bin/mola-guest /home/dev/retained-note', writable=True)
    try: patcher.refresh(out, unsafe, 'x86_64')
    except ValueError: pass
    else: raise AssertionError('guest-controlled destination symlink was followed')
    assert extracted(unsafe, '/home/dev/retained-note', root / 'unchanged-note') == b'customer data from old snapshot'
print('real ext4 staging, old-snapshot agent refresh, user data, failure atomicity and symlink confinement passed')
