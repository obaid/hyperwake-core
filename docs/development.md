# Developing Hyperwake

Customers should start with the quickstart. This page is for contributors
changing the service itself.

## Development environment

Use PHP 8.4.1+, Composer, Node 22.19+ and Go 1.23+. The current local workspace
uses Laravel Herd at `http://hyperwake.test`; Postgres and Redis run in Docker.
The developer services in `docker/compose.yml` are separate from the customer
`compose.yaml` deployment.

```sh
composer install
npm ci
cp .env.example .env
php artisan key:generate
docker compose -f docker/compose.yml up -d
php artisan migrate --seed
npm run build
bin/dev
```

Only copy `.env.example` on a fresh checkout; do not overwrite an existing
configuration. The development seeder creates development accounts and is
refused in self-hosted mode. Never run `migrate:fresh` or `db:wipe` on a shared
database.

The Debian/i3 guest at `image/docker` is a development fixture. It can exercise
the lifecycle and automation transports on a laptop. It is not Omarchy and does
not satisfy the release acceptance test. The customer runtime is `image/kvm`,
which builds on `image/omarchy` and needs a Linux/KVM host to run.

## Code map

| Area | Location |
|---|---|
| API and application | `app`, `routes` |
| Machine lifecycle and compute drivers | `app/Domain`, `app/Infrastructure/Compute` |
| Website and dashboard | `resources/js`, `resources/css` |
| Customer CLI and setup | `bin/hyperwake`, `bin/setup` |
| Automation transport | `runtime/automation.py` |
| Desktop gateway | `gateway/desktop` |
| Guest heartbeat and identity | `guest/hyperwake-guest` |
| Customer deployment | `compose.yaml`, `docker/self-host`, `image/kvm` |

Public docs are Markdown files rendered by the docs controller. Add pages to its
explicit allowlist; do not accept arbitrary file paths from a URL. Every new
machine endpoint needs ownership and token-ability tests.

## Verify

Follow `CONTRIBUTING.md`. Use `bin/acceptance` against a configured test server
for the real machine workflow. It leaves its test computer stopped with its disk
retained, for review. Its timing excludes initial deployment and downloads.
Inspect the screenshot and run the natural-language prompt in each agent before
claiming end-to-end compatibility.
