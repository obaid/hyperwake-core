#!/usr/bin/env node
import { inspectHost } from '../src/preflight.js';
import { createServer } from '../src/server.js';
import { stateDir } from '../src/paths.js';
import { ensurePython } from '../src/python.js';

const command = process.argv[2] ?? 'start';
const port = Number(
  process.env.HYPERWAKE_PORT
  || process.argv.find((argument) => argument.startsWith('--port='))?.split('=')[1]
  || 4141,
);

const ESC = '[';
const bold = (s) => `${ESC}1m${s}${ESC}0m`;
const dim = (s) => `${ESC}2m${s}${ESC}0m`;
const green = (s) => `${ESC}32m${s}${ESC}0m`;
const red = (s) => `${ESC}31m${s}${ESC}0m`;

function reportHost(host) {
  const mark = (ok) => (ok ? green('yes') : red('no'));
  console.log(`  platform      ${host.platform}/${host.arch}`);
  console.log(`  python3       ${mark(host.python)}`);
  console.log(`  ssh           ${mark(host.ssh)}`);
  console.log(`  docker        ${mark(host.docker)} ${dim('(prepares images; does not run the guest)')}`);
  console.log(`  accelerator   ${host.accelerator ? green(host.accelerator) : red('none')}`);
  if (host.qemu) console.log(`  qemu          ${dim(host.qemu)}`);
  if (host.qemu) {
    console.log(`  graphics      ${host.acceleratedGraphics ? green('accelerated (virgl)') : 'software (llvmpipe)'}`);
  }
  console.log(`  guest image   ${host.image ? green('yes') : red('none')} ${dim(host.imagePath)}`);
}

/**
 * A missing image is not the same failure as a missing hypervisor.
 * The host is fine; there is simply nothing to clone yet, so say how to fix it.
 */
function explainMissingImage(host) {
  const home = host.imagePath.replace(/\/image$/, '');
  console.log(`\n${red('No guest image.')} Machines are cloned from one, and you build it once.\n`);
  console.log('  That step needs Docker running, and on Apple Silicon a copy of Try');
  console.log('  Omarchy in /Applications to take the base filesystem from. Neither is');
  console.log('  used again once the image exists.\n');
  console.log(dim('    git clone https://github.com/obaid/hyperwake-core'));
  console.log(dim('    cd hyperwake-core && npm install'));
  console.log(dim(`    python3 bin/native-prepare --output ${home}`));
  console.log(`\n  ${dim('https://obaid.github.io/hyperwake-core/#image')}\n`);
}

if (command === 'doctor') {
  const host = inspectHost();
  console.log(bold('\nHyperwake engine — host check\n'));
  reportHost(host);
  if (!host.hostReady) {
    console.log(`\n${red('Not ready.')} ${host.reason}\n`);
    process.exit(1);
  }
  if (!host.image) {
    explainMissingImage(host);
    process.exit(1);
  }
  console.log(`\n${green('Ready.')}\n`);
  process.exit(0);
}

if (command !== 'start') {
  console.log('usage: hyperwake [start|doctor] [--port=4141]');
  process.exit(1);
}

const host = inspectHost();
if (!host.hostReady) {
  console.log(bold('\nHyperwake engine\n'));
  reportHost(host);
  console.log(`\n${red('Cannot start.')} ${host.reason}\n`);
  process.exit(1);
}
if (!host.image) {
  console.log(bold('\nHyperwake engine\n'));
  reportHost(host);
  explainMissingImage(host);
  process.exit(1);
}

process.env.HYPERWAKE_PORT = String(port);
ensurePython();
const { server, runtime, token } = await createServer({ host, port });

server.listen(port, '127.0.0.1', () => {
  const base = `http://127.0.0.1:${port}`;
  console.log(`
${bold('  Hyperwake')} ${dim('· Omarchy computers on this machine')}

  ${bold('API')}    ${green(`${base}/v1`)}
  ${bold('Token')}  ${token}
  ${dim(`State  ${stateDir()}`)}

  ${dim('Give your agent these:')}

    curl ${base}/v1 -H "Authorization: Bearer $TOKEN"

    POST   /v1/machines                  create a fresh Omarchy computer
    GET    /v1/machines                  list them
    GET    /v1/machines/{id}             describe one
    POST   /v1/machines/{id}/start       start it
    POST   /v1/machines/{id}/stop        shut down   {"force": true} cuts power
    POST   /v1/machines/{id}/actions     exec | read_file | write_file | screenshot
                                         click | move | scroll | type | key
    POST   /v1/machines/{id}/desktop     a browser URL for the desktop
    DELETE /v1/machines/{id}             destroy it and its disk

  ${dim('Ctrl-C to stop. Machines keep their disks; the engine does not.')}
`);
});

const shutdown = () => {
  console.log('\n  stopping...');
  server.close();
  runtime.stop();
  process.exit(0);
};
process.on('SIGINT', shutdown);
process.on('SIGTERM', shutdown);
