# Your first computer

Hyperwake runs on a server you control. Your agent connects to that server from
its usual machine. You do not need to move Claude Code, Codex, OpenCode, or Pi
into the computer it operates.

**Source preview:** the Mac runtime has been exercised locally. Linux and
Windows need community hardware tests. Initial source builds exceed five minutes.

## 1. Set up Hyperwake once

Follow the [platform guide](platforms.md) for Mac, Linux, or Windows prerequisites.
On Mac and Linux, from a fresh clone:

```sh
python3 bin/setup --build
```

Setup selects the host runtime and prints the service URL and credentials path.
For a local agent on the same computer:

```sh
python3 bin/hyperwake login --credentials .hyperwake/credentials/customer.json
python3 bin/hyperwake doctor
```

Skip to step 3 once `doctor` succeeds. On Windows use `python` instead of
`python3` and complete the native image preparation step in the platform guide.

For a remote Linux server, pass `--url https://computers.example.com` to setup;
see [self-hosting](self-hosting.md). Your agent can stay on your laptop.

## 2. Connect your agent machine

Copy `bin/hyperwake` to the machine running your agent and make it executable.
It needs Python 3 only. Setup wrote the token and dashboard login to the private
file `.hyperwake/credentials/customer.json` **on the server**. Transfer the API
token securely; do not put credentials in prompts, shell history, or Git.

```sh
./hyperwake login --endpoint https://computers.example.com/api/v1 --token-stdin
./hyperwake doctor
```

`login` reads the token from standard input. It saves credentials in
`~/.config/hyperwake/config.json` with owner-only permissions. When running the
client on the server itself, use:

```sh
./bin/hyperwake login --credentials .hyperwake/credentials/customer.json
```

## 3. Ask your agent to work

Give it the path to `hyperwake`, and this prompt:

> Read the Hyperwake agent guide. Create an Omarchy computer named research and
> wait until it is ready. Use its browser to open example.com. Save a short
> summary to ~/research.md. Capture a screenshot for me, stop the computer, and
> tell me its ID. Keep its disk so we can resume later.

The [agent guide](agents.md) has the exact commands. An agent can execute them
through its existing terminal tool. A model that can view images can inspect
screenshots; command/file tasks also work with a text-only agent.

## Resume later

```sh
./hyperwake list
./hyperwake start COMPUTER_ID --wait
./hyperwake read COMPUTER_ID '~/research.md'
./hyperwake stop COMPUTER_ID --wait
```

Quote guest paths containing `~` to prevent your local shell expanding them:
`./hyperwake read COMPUTER_ID '~/research.md'`.

Stopping shuts down processes and retains the guest disk. Waking boots the same
disk; it does not restore running processes or in-memory state. Cookies may
remain, but websites can still expire sessions or require you to log in again.

## Inspect the desktop yourself

Open your server URL, log in using the email and generated dashboard password,
and open the computer's desktop. The password lives in the server credentials
file. Your agent needs only its API token.

If a computer cannot boot, consult [troubleshooting](troubleshooting.md).
