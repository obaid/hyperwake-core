# Hyperwake API v1

Programmatic access to computers: create one, wake it, hand it to an agent,
stop it, and read exactly what it cost.

Base URL: `https://hyperwake.ai/api/v1` (locally, `http://hyperwake.test/api/v1`)

Everything below is what the endpoints actually return — the examples are
lifted from the test suite in `tests/Feature/Api`.

---

## Authentication

Bearer tokens. Send them on every request:

```
Authorization: Bearer 3|kJq8...
Accept: application/json
```

Create your first token from the signed-in web app — the token endpoints also
accept the session cookie, so you are not stuck needing a token to get a token.
Requests from the app's own origin go through CSRF protection; requests from a
script with a bearer token stay stateless.

```bash
curl -X POST https://hyperwake.ai/api/v1/tokens \
  -H 'Accept: application/json' \
  -H 'Content-Type: application/json' \
  -b cookies.txt -H "X-XSRF-TOKEN: $(...)" \
  -d '{"name": "agent runner", "expires_in_days": 90}'
```

```json
{
  "data": {
    "id": 4,
    "name": "agent runner",
    "abilities": ["*"],
    "expires_at": "2026-12-08T04:11:02+00:00",
    "token": "4|Zt9xQm2pL0..."
  }
}
```

**The plaintext token is shown once.** Only a hash is stored; there is no
endpoint that can return it again. Lose it and you mint a new one.

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/tokens` | List your tokens (never the plaintext) |
| `POST` | `/tokens` | Create one. `name`, optional `abilities`, `expires_in_days` |
| `DELETE` | `/tokens/{id}` | Revoke immediately |

Abilities: `computers:read`, `computers:write`, or `*`.

---

## The Computer object

The only public representation of a machine. Note what is absent: nothing here
tells you which hypervisor, host, node or container backs the machine. That is
deliberate and permanent.

```json
{
  "id": "01a08457-284f-7129-afc6-43a2eaeb944b",
  "name": "agent box",
  "slug": "agent-box",
  "status": "ready",
  "status_label": "Ready",
  "is_transitional": false,
  "region": "local",
  "profile": { "slug": "small", "name": "Small", "vcpus": 2, "memory_mb": 4096, "disk_gb": 40 },
  "image":   { "slug": "hyperwake-dev-desktop", "name": "Hyperwake Desktop", "version": "0.1.0" },
  "auto_stop_minutes": 60,
  "ssh": {
    "command": "ssh -p 22001 dev@127.0.0.1",
    "host": "127.0.0.1",
    "port": 22001,
    "username": "dev"
  },
  "usage": { "current_session_seconds": 0, "month_to_date_seconds": 3600, "month_to_date_cents": 8 },
  "health": { "is_healthy": true, "last_heartbeat_at": "2026-09-09T04:10:55+00:00", "guest_version": "0.1.0" },
  "error": null,
  "created_at": "2026-09-09T04:05:54+00:00",
  "ready_at": "2026-09-09T04:06:41+00:00"
}
```

`ssh` is `null` until the machine has run at least once. **Ports change every
time a computer is woken** — always re-read `ssh.command` rather than caching it.

### Status

| Status | Meaning |
|---|---|
| `provisioning` | Being created for the first time |
| `booting` | Powering on, waiting for the guest to report healthy |
| `ready` | Usable. Desktop and SSH work |
| `stopping` | Shutting down |
| `stopped` | Off. Disk intact, nothing billing |
| `error` | Something failed. The disk is intact; wake it to retry |
| `deleting` | Being torn down |

`booting`, `ready` and `stopping` are the states in which compute is allocated
— exactly the window that accrues usage.

---

## Computers

### `GET /computers`

```json
{ "data": [ { "id": "...", "name": "agent box", "status": "ready", "...": "..." } ] }
```

### `POST /computers` → `201`

```bash
curl -X POST https://hyperwake.ai/api/v1/computers \
  -H "Authorization: Bearer $HYPERWAKE_TOKEN" \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: create-agent-box-2026-09-09' \
  -d '{"name": "agent box", "profile": "small", "auto_stop_minutes": 60}'
```

| Field | Required | Notes |
|---|---|---|
| `name` | yes | 1–48 chars, letters/numbers/spaces/hyphens/underscores |
| `profile` | yes | A slug from `GET /profiles` |
| `image` | no | A slug from `GET /images`. Defaults to the current default image |
| `auto_stop_minutes` | no | `30`, `60`, `240`, or `null` for never |

Returns the computer in `provisioning` plus the operation to poll:

```json
{
  "data": { "id": "01a08457-...", "status": "provisioning", "...": "..." },
  "operation": { "id": "01a08457-...", "type": "provision", "status": "pending", "step": "requested", "step_label": "Requested" }
}
```

Provisioning is asynchronous. Poll `GET /computers/{id}` until `status` is
`ready` — typically well under three minutes.

### `GET /computers/{id}`, `PATCH /computers/{id}`

`PATCH` accepts `auto_stop_minutes` only. Everything else is a lifecycle call.

### `DELETE /computers/{id}` → `202`

```bash
curl -X DELETE ".../computers/$ID" -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"delete_disk": true}'
```

**The disk survives by default.** Deleting the machine metadata and deleting
somebody's files are different decisions, so destroying storage requires
`delete_disk: true` explicitly.

---

## Power

| Method | Path | Result |
|---|---|---|
| `POST` | `/computers/{id}/start` | `202` — wakes a stopped computer |
| `POST` | `/computers/{id}/stop` | `202` — graceful shutdown, disk kept |
| `POST` | `/computers/{id}/restart` | `202` — reboot; stays inside one usage interval |

All three answer with the computer and an `operation`:

```json
{
  "data": { "id": "...", "status": "stopped", "...": "..." },
  "operation": { "id": "01a084...", "type": "start", "status": "pending" }
}
```

All three are idempotent. A second `start` while one is in flight returns the
operation already running rather than booting the machine twice — a duplicate
request is never a duplicate machine.

Waking an already-awake computer is accepted as a no-op, not an error.

---

## Desktop sessions

### `POST /computers/{id}/desktop-sessions` → `201`

```json
{ "ws_url": "ws://127.0.0.1:8788/desktop", "token": "hV3k…48 chars", "expires_in": 60 }
```

Open `{ws_url}?token={token}` as a websocket and speak VNC over it.

The token is **single use** and lives 60 seconds. It is stored only as a
SHA-256 hash, and the response deliberately does not contain the machine's
address or port — the gateway resolves those internally. Reconnecting means
minting a fresh token.

Requires `status: ready`; anything else is `409 invalid_state`.

---

## Usage

### `GET /computers/{id}/usage?from=2026-09-01&to=2026-09-30`

```json
{
  "data": {
    "computer_id": "01a08457-...",
    "from": "2026-09-01T00:00:00+00:00",
    "to": "2026-09-09T04:11:02+00:00",
    "total_seconds": 3600,
    "total_cents": 8,
    "open_interval": false,
    "intervals": [
      { "started_at": "2026-09-08T04:11:02+00:00", "ended_at": "2026-09-08T05:11:02+00:00",
        "seconds": 3600, "cents": 8, "billing_state": "closed" }
    ]
  }
}
```

Defaults to the current month. Usage is metered from **allocated running
time** on the host, never from anything the guest reports.

`billing_state` is one of `open`, `closed`, `quarantined`, `reported`. A
`quarantined` interval is one where the platform's evidence was ambiguous — it
is shown rather than hidden, and is not silently billed.

---

## SSH keys

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/computers/{id}/ssh-keys` | Keys on the account |
| `POST` | `/computers/{id}/ssh-keys` | Register a key. `name`, `public_key` |

Keys are account-wide and land in the guest on its next boot. Supported types:
`ssh-ed25519`, `ssh-rsa`, `ecdsa-sha2-nistp{256,384,521}` and the `sk-`
hardware variants.

The comment is stripped before storage, so a key cannot smuggle extra
`authorized_keys` options (`command=`, `environment=`) into a guest.

---

## Discovery

`GET /profiles` and `GET /images` — so a client never has to guess a valid
slug.

```json
{ "data": [ { "slug": "small", "name": "Small", "vcpus": 2, "memory_mb": 4096, "disk_gb": 40, "cents_per_hour": 8 } ] }
```

---

## Idempotency

Send `Idempotency-Key` on any POST. The first response for a key is replayed
for 24 hours:

```
Idempotency-Key: create-agent-box-2026-09-09
```

- A replayed response carries `Idempotent-Replay: true`.
- Keys are scoped to your account, the method and the path. Two clients cannot
  collide, and reusing a key on a different endpoint does not replay the wrong
  body.
- A request still in flight under the same key gets `409 idempotency_conflict`.
- **Failed attempts are not cached** — a retry after a `422` can still succeed.

An agent that times out and retries will not end up with two computers.

---

## Rate limits

Per authenticated user, per minute:

| Scope | Limit |
|---|---|
| Everything | 120 / min |
| `POST /computers` | 10 / min |
| `POST /computers/{id}/{start,stop,restart}` | 30 / min |

Exceeding a limit returns `429` with a `Retry-After` header. Creation is much
tighter than reading on purpose: a runaway agent should hit a wall long before
it hits a bill.

---

## Errors

Every failure has the same shape:

```json
{ "error": { "code": "plan_limit_reached", "message": "Your plan allows 1 computer(s). Delete one or upgrade to add another.", "details": { "limit": 1 } } }
```

Branch on `code`. `message` is for humans and may be reworded; `code` is stable.

| HTTP | `code` | When |
|---|---|---|
| 400 | `invalid_idempotency_key` | Key longer than 255 characters |
| 401 | `unauthenticated` | Missing, revoked or expired token |
| 402 | `spend_cap_exceeded` | The account is at its own spend ceiling. `details.cap_cents` / `details.spent_cents` |
| 403 | `insufficient_token_ability` | The token lacks the ability this endpoint needs. `details.required` / `details.granted` |
| 404 | `not_found` | No such resource — **also** what another account's resource returns |
| 409 | `plan_limit_reached` | At your plan's computer limit. `details.limit` |
| 409 | `account_not_provisionable` | Account suspended or past due |
| 409 | `illegal_state_transition` | `details.from` / `details.to` |
| 409 | `invalid_state` | The computer is not in a state that allows this |
| 409 | `idempotency_conflict` | Same key still in flight |
| 422 | `validation_failed` | `details` maps field → messages |
| 429 | `rate_limited` | Slow down |
| 502 | `clone_failed`, `start_failed`, … | Provider failed. `details.retryable` |
| 503 | `capacity_unavailable` | No host has room. Retryable; honour `Retry-After` |
| 500 | `internal_error` | Our fault. Logged with an operation id |

Another account's computer returns `404`, never `403`, so the API cannot be
used to discover which ids exist. The one `403` the API does return is
`insufficient_token_ability`, which is about your own token rather than about
somebody else's resource, so it leaks nothing.

### Token abilities

Every mutating endpoint requires `computers:write`; reads require nothing
beyond a valid token. A token minted with `["computers:read"]` can list and
inspect machines but cannot create, wake, stop, restart, delete, add a key, or
open a desktop session — those return `403 insufficient_token_ability`. A
token minted with `["*"]` may do everything. Give an autonomous agent the
narrowest token that lets it do its job.

---

## Worked example: give an agent a computer

```bash
#!/usr/bin/env bash
set -euo pipefail
API=https://hyperwake.ai/api/v1
AUTH="Authorization: Bearer $HYPERWAKE_TOKEN"
JSON='Content-Type: application/json'

# 1. Create it. The idempotency key makes this script safe to re-run.
ID=$(curl -sS -X POST "$API/computers" -H "$AUTH" -H "$JSON" \
      -H "Idempotency-Key: nightly-agent-$(date +%F)" \
      -d '{"name":"nightly agent","profile":"small","auto_stop_minutes":60}' \
    | jq -r '.data.id')

# 2. Register the key the agent will use.
curl -sS -X POST "$API/computers/$ID/ssh-keys" -H "$AUTH" -H "$JSON" \
  -d "{\"name\":\"agent\",\"public_key\":\"$(cat ~/.ssh/agent.pub)\"}" > /dev/null

# 3. Wait for it. Provisioning is asynchronous.
until [ "$(curl -sS "$API/computers/$ID" -H "$AUTH" | jq -r '.data.status')" = ready ]; do
  sleep 5
done

# 4. Hand it over. Re-read the command — the port changes on every wake.
SSH=$(curl -sS "$API/computers/$ID" -H "$AUTH" | jq -r '.data.ssh.command')
$SSH 'claude "summarise today’s open issues" > ~/report.md'

# 5. Stop it. The disk — and that report — survive.
curl -sS -X POST "$API/computers/$ID/stop" -H "$AUTH" > /dev/null

# Tomorrow: POST /start, and ~/report.md is still there.
```

That last line is the product. The computer persists; the agent can leave and
come back.

---

## Reliability tooling

Operator scripts in `bin/`, backing the PRD's launch gates:

```bash
bin/lifecycle-stress --cycles=100   # the launch gate: 100 clean cycles
bin/provision-benchmark --days=7    # time-to-ready percentiles
bin/capacity-report                 # host utilisation vs reservations
bin/failure-injection               # break the driver, prove cleanup works
```

`lifecycle-stress` simulates the guest heartbeat by default so it exercises the
control plane rather than the guest; pass `--no-simulate-guest` when a real
guest daemon is running.

## Computer automation

`POST /api/v1/computers/{id}/actions` operates a ready computer owned by the
caller. Requires a `computers:write` token. The request body selects an action:

| Action | Required fields | Result |
|---|---|---|
| `exec` | `command` | `stdout`, `stderr`, `exit_code`, `timed_out`, `truncated` |
| `read_file` | `path` | `content_base64`, `size` |
| `write_file` | `path`, `content` | `path`, `bytes_written` |
| `screenshot` | none | `image_base64`, `mime_type` |
| `click`, `move` | integer `x`, `y` | `ok` |
| `type` | `text` | `ok` |
| `key` | `key`, e.g. `ctrl-l` or `enter` | `ok` |
| `scroll` | `direction`: `up` or `down` | `ok` |

All results are wrapped in `{"data": ...}`. `exec.timeout` is 1–120 seconds
(default 30). Files and each output stream are limited to 1 MiB. `click.button`
is 1–3 (default 1); `scroll.amount` is 1–20 (default 3). Coordinates refer to
pixels in the current screenshot. Paths and shell commands refer to the guest.

A stopped computer or overlapping action returns 409. Invalid arguments return
422; another account's ID returns 404. Transport failures return 502 with
`error.code=automation_unavailable`. Failed guest commands return their nonzero
exit code in a successful HTTP response; callers must inspect it.

An action extends the activity lease. Only one action per computer executes at
a time. Reuse the same `Idempotency-Key` when retrying the same HTTP request,
and inspect side effects after a timeout before issuing a new action.

Self-hosted installs use their own URL, for example
`https://computers.example.com/api/v1`. The hosted API is not generally available.
