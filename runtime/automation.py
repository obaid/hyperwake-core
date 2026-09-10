#!/usr/bin/env python3
"""Private transport helper. Input arrives over stdin; nothing is a host shell command."""
import base64
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

# This program executes inside the guest, using the guest's Python installation.
GUEST_PROGRAM = r'''
import base64, json, os, pathlib, signal, stat, subprocess, sys, tempfile
p=json.load(sys.stdin)
a=p['action']
limit=1048576
if a=='exec':
    # SSH sessions do not inherit the graphical session's environment. Discover
    # this guest user's Wayland socket so commands can open desktop applications.
    runtime=pathlib.Path('/run/user')/str(os.getuid())
    for candidate in sorted(runtime.glob('wayland-*')):
        if stat.S_ISSOCK(candidate.stat().st_mode):
            os.environ.update(XDG_RUNTIME_DIR=str(runtime),WAYLAND_DISPLAY=candidate.name,
                              XDG_SESSION_TYPE='wayland',XDG_CURRENT_DESKTOP='Hyprland')
            if (runtime/'bus').exists():
                os.environ['DBUS_SESSION_BUS_ADDRESS']='unix:path='+str(runtime/'bus')
            break
    with tempfile.TemporaryFile() as out, tempfile.TemporaryFile() as err:
        child=subprocess.Popen(['/bin/bash','-lc',p['command']],stdout=out,stderr=err,start_new_session=True,cwd=os.path.expanduser('~'))
        timed_out=False
        try: child.wait(timeout=p.get('timeout',30))
        except subprocess.TimeoutExpired:
            timed_out=True
            os.killpg(child.pid,signal.SIGKILL)
            child.wait()
        out.seek(0); err.seek(0)
        stdout=out.read(limit+1); stderr=err.read(limit+1)
        print(json.dumps(dict(exit_code=child.returncode,stdout=stdout[:limit].decode('utf8','replace'),stderr=stderr[:limit].decode('utf8','replace'),timed_out=timed_out,truncated=len(stdout)>limit or len(stderr)>limit)))
elif a=='read_file':
    path=pathlib.Path(p['path']).expanduser()
    with path.open('rb') as f: data=f.read(limit+1)
    if len(data)>limit: raise ValueError('File exceeds 1 MiB; use exec to select a smaller portion.')
    print(json.dumps(dict(content_base64=base64.b64encode(data).decode(),size=len(data))))
elif a=='write_file':
    path=pathlib.Path(p['path']).expanduser()
    path.parent.mkdir(parents=True,exist_ok=True)
    data=p['content'].encode()
    path.write_bytes(data)
    print(json.dumps(dict(path=str(path),bytes_written=len(data))))
'''


def run(payload):
    target, action = payload['target'], payload['action']
    if action['action'] in ('exec', 'read_file', 'write_file'):
        # Host and port come from the compute driver, never request parameters.
        host = target['ssh_host']
        if not host or host.startswith('-') or not 0 < int(target['ssh_port']) < 65536:
            raise ValueError('Invalid connection target')
        encoded = base64.b64encode(GUEST_PROGRAM.encode()).decode()
        remote = "python3 -c \"import base64; exec(base64.b64decode('" + encoded + "'))\""
        args = ['ssh', '-T', '-o', 'BatchMode=yes', '-o', 'IdentitiesOnly=yes',
                '-o', 'ConnectTimeout=10', '-o', 'StrictHostKeyChecking=accept-new',
                '-o', 'HostKeyAlias=hyperwake-' + target['id'],
                '-o', 'UserKnownHostsFile=' + target['known_hosts'],
                '-i', target['ssh_key'], '-p', str(target['ssh_port']),
                'dev@' + host, remote]
        result = subprocess.run(args, input=json.dumps(action), capture_output=True, text=True,
                                timeout=action.get('timeout', 30) + 15)
        if result.returncode:
            raise RuntimeError('SSH command failed; check the guest and its authorized key.')
        return json.loads(result.stdout)
    from vncdotool import api
    from vncdotool.client import VNCDoToolFactory
    class DesktopFactory(VNCDoToolFactory):
        # WayVNC can answer the first update with this extension alone. The
        # client then waits forever for pixels. Standard RFB key events suffice.
        qemu_extended_key = False
    host, port = target['display_host'], target['display_port']
    if not host or not port:
        raise RuntimeError('Desktop is not connected')
    with api.connect(f'{host}::{int(port)}', factory_class=DesktopFactory, timeout=15) as client:
        op = action['action']
        if op == 'screenshot':
            with tempfile.TemporaryDirectory(prefix='hyperwake-screen-') as temp:
                path = Path(temp) / 'screen.png'
                # The first WayVNC update may be its placeholder framebuffer.
                # Request the next full update after capture has started.
                client.refreshScreen()
                client.pause(0.2)
                client.captureScreen(str(path))
                return {'mime_type': 'image/png', 'image_base64': base64.b64encode(path.read_bytes()).decode()}
        if op in ('click', 'move'):
            client.mouseMove(action['x'], action['y'])
            if op == 'click': client.mousePress(action.get('button', 1))
        elif op == 'scroll':
            for _ in range(action.get('amount', 3)):
                client.mousePress(4 if action['direction'] == 'up' else 5)
        elif op == 'type':
            for character in action['text']:
                client.keyPress({'\n': 'enter', '\t': 'tab'}.get(character, character))
        elif op == 'key':
            aliases = {'escape': 'esc', 'backspace': 'bsp', 'pageup': 'pgup', 'pagedown': 'pgdn', 'insert': 'ins'}
            client.keyPress('-'.join(aliases.get(key, key) for key in action['key'].split('-')))
        else: raise ValueError('Unsupported action')
        return {'ok': True}

if __name__ == '__main__':
    os.umask(0o077)
    try:
        print(json.dumps(run(json.load(sys.stdin))))
    except Exception:
        # Neither credentials nor command/file contents belong in process logs.
        print('Automation transport failed.', file=sys.stderr)
        sys.exit(1)
    finally:
        vnc_api = sys.modules.get('vncdotool.api')
        if vnc_api is not None:
            vnc_api.shutdown()
