# Troubleshooting

## Setup says KVM is unavailable

For the Linux runtime, run setup on a Linux x86_64 host. Mac and Windows use
the [native runtime](platforms.md), which does not need Docker to expose KVM. Check
`ls -l /dev/kvm`. Enable CPU virtualization in firmware, or choose a server
provider that exposes it. Docker Desktop alone does not satisfy this requirement.
Do not substitute the Debian development image and call it Omarchy.

## First build takes longer than five minutes

This checkout builds the OS and service from source. The five-minute customer
experience needs prebuilt release artifacts and a measured clean install; those
are still release gates. Watch `.hyperwake` build logs when testing locally, or
the live Docker build output from `bin/setup --build`.

## API connection fails

Run `hyperwake doctor`. Check the endpoint ends in `/api/v1`, your domain and
certificate, and that the token belongs to this deployment. The client refuses
unencrypted remote HTTP and redirects. For a private server use the SSH tunnel
shown in the quickstart, with the localhost endpoint.

## Computer remains provisioning or booting

Check `hyperwake show COMPUTER_ID`, then the worker logs:

```sh
docker compose --env-file .hyperwake/compose.env logs --tail=100 worker
```

The queue worker and scheduler are required. Check host capacity and available
disk space. A boot timeout requires inspecting the actual guest display and
SSH services; a running container alone does not mean the desktop is ready.

## Shell works but screenshot fails

The desktop and its VNC service may still be starting or may have failed.
Inspect the computer's status and gateway logs. A black screen is a failure to
investigate, not evidence of a working Omarchy computer. The desktop API supports
only known host-derived targets, not arbitrary host/port requests.

## SSH host key changed

The automation transport pins guest keys on first use under a computer-specific
alias. A key-change failure is intentional. Verify whether the guest was rebuilt
or compromised before removing its old entry from the application's
`storage/app/automation-known-hosts`. Do not disable host-key checking globally.

## File is on the wrong computer

Quote guest paths: `hyperwake read COMPUTER_ID '~/research.md'`. Unquoted `~`
is expanded by your local shell before Hyperwake receives it. An `exec` command
must also be quoted so local shell expansion cannot happen first.

## Hosted-access requests

The landing form saves requests in the deployment's `hosted_requests` table.
It sends no email and starts no subscription. The operator can inspect requests
using its database administration tools. Before exposing this form publicly,
set a retention policy and a contact route for deletion requests.
