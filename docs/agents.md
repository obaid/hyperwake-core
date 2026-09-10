# Connect your agent

Hyperwake is a computer service. Your existing agent supplies the reasoning;
Hyperwake supplies the computer. The first integration uses a CLI so agents with
terminal access can share the same interface. No MCP configuration is required.

Claude Code, Codex, OpenCode, and Pi are intended clients through their terminal
tools. This is protocol compatibility, not a claim that every version of all
four agents has passed an end-to-end test. See the release checklist for evidence.

## Setup prompt

After following the quickstart and saving credentials, give your agent:

> Use Hyperwake at the configured endpoint. The CLI is /absolute/path/hyperwake.
> Read /absolute/path/to/hyperwake/docs/agents.md before acting. Create a computer,
> wait for it to be ready, and complete my task there. Keep the computer ID. Work
> only on that computer unless I explicitly request work on my local machine.
> Stop it when finished. Never delete its disk without my explicit request.

A copy of these instructions is also provided at `tools/agent/SKILL.md`.
The agent's normal permission controls still apply.

## Commands

In a source checkout, replace `hyperwake` below with
`python3 /absolute/path/to/hyperwake/bin/hyperwake`. On Windows use
`python C:\path\to\hyperwake\bin\hyperwake`. Quoting follows your local shell;
`exec` commands themselves run in the Linux guest's Bash.

Commands return JSON, except `read`, which writes the file bytes to stdout.
Use `--output` to save binary files or screenshots locally. Substitute the UUID
returned by `create` for `COMPUTER_ID`; do not use the display name as an ID.

```sh
hyperwake doctor
hyperwake profiles
hyperwake create research --wait
hyperwake list
hyperwake show COMPUTER_ID
hyperwake exec COMPUTER_ID 'mkdir -p ~/research && printf "hello\n" > ~/research/note.txt'
hyperwake read COMPUTER_ID '~/research/note.txt'
printf 'Updated findings\n' | hyperwake write COMPUTER_ID '~/research/note.txt'
hyperwake exec COMPUTER_ID 'xdg-open https://example.com >/tmp/browser.log 2>&1 &'
hyperwake screenshot COMPUTER_ID --output /tmp/hyperwake-screen.png
hyperwake click COMPUTER_ID 400 250
hyperwake key COMPUTER_ID ctrl-l
hyperwake type COMPUTER_ID https://example.com
hyperwake key COMPUTER_ID enter
hyperwake scroll COMPUTER_ID down --amount 3
hyperwake stop COMPUTER_ID --wait
hyperwake start COMPUTER_ID --wait
```

Observe a fresh screenshot before choosing coordinates. Coordinates refer to
pixels in that screenshot. `key` uses VNC key names such as `enter`, `tab`,
`escape`, and `ctrl-l`. `type` is appropriate for ordinary keyboard text; write
Unicode-rich or large documents through the file interface instead.

`exec` runs Bash inside the guest, starts in the guest home directory, and is
bounded to 30 seconds by default (`--timeout` accepts up to 120). Its JSON result
includes `exit_code`, `stdout`, `stderr`, `timed_out`, and `truncated`. A nonzero
guest exit code also makes the CLI exit nonzero. Large outputs are capped at
1 MiB per stream. Files are capped at 1 MiB per action. Use shell commands to
select a smaller range for larger files.

## Lifecycle and retries

`create` starts the computer. `--wait` polls until it is ready, with a default
300-second limit. A timeout means the operation may still be running: inspect
with `show` before attempting another create. Power operations are idempotent.
Repeated create commands intentionally create separate computers; reuse the ID.

Only one automation action runs per computer at a time. A concurrent action
returns HTTP 409. Wait for the earlier action to finish before retrying.

Avoid retrying shell or input actions blindly after a connection failure: the
operation may already have happened. Inspect the file or screen first. API
clients can reuse an `Idempotency-Key` for retries of the same request.

## Credentials and boundaries

Use `hyperwake login --token-stdin` or environment variables `HYPERWAKE_ENDPOINT`
and `HYPERWAKE_TOKEN`. Do not paste tokens into a model prompt or commit them.
Use `HYPERWAKE_CONFIG` to choose a separate config file for another server.

Guest files are separate from your local filesystem. `write` uploads text;
`read --output` and `screenshot --output` download to explicitly named paths.
Local home folders are not mounted into computers by default. Credentials
entered in a guest are available to agents that control that guest.

Stop retains the disk. `delete COMPUTER_ID` removes the computer metadata and
retains its disk, which then needs operator management. Permanent deletion is
`delete COMPUTER_ID --delete-disk --yes`; request explicit user authorization
before doing this.
