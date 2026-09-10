# Self-hosting Hyperwake

This guide covers Linux server deployment. For a local Mac or Windows install,
start with the [platform guide](platforms.md). Deploy the complete service on
your own infrastructure and connect agents to its API. The service does not depend on a Hyperwake cloud account.

## Requirements

- Linux x86_64 with hardware virtualization exposed through `/dev/kvm`.
- Docker Engine and Compose v2; your deploy user must be able to use Docker.
- Python 3, Git, and OpenSSH tools on the server for bootstrap.
- Start with 16 GiB RAM and 80 GiB free disk for a source build and one computer.
- A domain pointing at the server and open ports 80/443 for remote HTTPS, or an
  SSH tunnel to the default localhost endpoint.

A cloud VPS must explicitly support nested virtualization. A bare-metal server
with VT-x/AMD-V is the least ambiguous starting point. No paid server is created
by setup. Mac and Windows use the native helper described in the platform guide.

## Deployment

Run `bin/setup --build --url https://computers.example.com --email you@example.com`
from the checkout. Source builds include the Omarchy package set and a bootable
VM disk and can take considerably longer than five minutes. Prebuilt distribution
and measured startup times remain release work.

Setup writes `.hyperwake/server.env`, `.hyperwake/compose.env`, and a private
credentials directory. Preserve these files along with database and guest disks.
Do not run the development database seeder: it contains development accounts.
Self-host bootstrap generates random credentials and a free self-hosted plan.
Your server opens its login/dashboard at the root URL. Hosted-access enquiries
belong to the separate hyperwake.ai marketing deployment and are disabled on
self-hosted installations.

The Compose deployment starts the web app, queue worker, scheduler, Postgres,
Redis, desktop gateway, and HTTPS proxy. Computer containers supervise KVM guests;
the customer's shell runs in the VM. The full guest filesystem is stored on a
named volume, so stopping or recreating the supervisor retains installed software
as well as home files.

The KVM supervisor uses a virtual graphics device and software rendering on its
host side. This path must pass the real-server acceptance test before the release
can claim supported Omarchy desktop operation.

## Operations

Always include the generated Compose environment file:

```sh
docker compose --env-file .hyperwake/compose.env ps
docker compose --env-file .hyperwake/compose.env logs --tail=100 app worker gateway
docker compose --env-file .hyperwake/compose.env restart worker scheduler
```

The CLI's `doctor` tests API access and catalog availability. It does not certify
KVM or desktop health; create a computer and complete the acceptance workflow.

Capacity defaults reserve 8 GiB guest RAM and four virtual CPUs on the host.
Budget additional RAM for the application and approximately 1 GiB overhead per
running VM. Adjust the host's capacity in the admin dashboard to match hardware.
Source builds need additional temporary disk space. Disk quotas are provisioned
per VM, while actual host free space must still be monitored.

## Upgrade

Stop computers through the API and back up their disks, application storage,
configuration, and Postgres. Review the release notes, check out the intended
release, rebuild with `python3 bin/build-images`, then run `python3 bin/setup` using the original
URL and email. Bootstrap preserves existing credentials; database migrations
are additive unless release notes explicitly state otherwise. Existing computers
retain their original disks and are not automatically rebased onto a new OS image.

Backups are operator-managed in the open-source release. Stop a VM before copying
its volume and test restoration; copying a running disk is not a reliable backup.
Do not advertise snapshots from the legacy Docker commit operation: it does not
capture the attached VM disk.

## Stop the service or uninstall

Stop every computer with the CLI first. `docker compose down` stops the control
plane but does not stop the dynamically created computer supervisors.

```sh
docker compose --env-file .hyperwake/compose.env down
```

This keeps volumes and credentials. To permanently delete a computer and disk,
use the explicit CLI deletion command before removing the control plane. Keep
backups and inspect any remaining Hyperwake-labelled resources. Avoid global
Docker prune commands: the server may run other applications.

## Hosting boundary

The initial release serves one operator or trusted team. It is not yet certified
for selling isolated capacity to unrelated tenants. Guest access to private
networks, metadata endpoints, capacity abuse, and recovery require additional
hardening for that use. See [security policy](../SECURITY.md).
