# opencode-jobs

Run OpenCode agents on a schedule. One Go binary, no plugin.

`opencode-jobs` installs a native scheduler entry (launchd on macOS, systemd or
cron on Linux) that runs `opencode run` with a prompt you choose. Jobs survive
reboots, run from the project directory that owns them, and write searchable logs
and run history.

Works with **OpenCode V2**. The skill is installable with the
[`skills` CLI](https://skills.sh):

```bash
npx skills add Martins6/opencode-jobs --skill opencode-jobs -g -a opencode -y
```

## Install

### Homebrew (macOS and Linux)

```bash
brew tap Martins6/tap
brew install opencode-jobs
```

The tap installs a prebuilt binary for macOS and Linux on both arm64 and amd64.

### Install script

Prefer Homebrew or `go install`. The script is a fallback for machines without
them. Download it, review it, then run it from a file:

```bash
curl -fsSL https://raw.githubusercontent.com/Martins6/opencode-jobs/main/install.sh \
  -o /tmp/opencode-jobs-install.sh
less /tmp/opencode-jobs-install.sh
bash /tmp/opencode-jobs-install.sh
```

The script installs to `~/.local/bin` by default, verifies the release checksum,
and prints a PATH hint when needed. It does not verify itself. Options:

```bash
# Specific release
bash /tmp/opencode-jobs-install.sh --version v0.1.0

# Latest build from main
bash /tmp/opencode-jobs-install.sh --main

# Custom prefix
bash /tmp/opencode-jobs-install.sh --prefix /usr/local/bin
```

### Go

```bash
go install github.com/Martins6/opencode-jobs@latest
```

### The skill

Install globally so every OpenCode session can use it:

```bash
npx skills add Martins6/opencode-jobs --skill opencode-jobs -g -a opencode -y
```

Or install it into one project:

```bash
npx skills add Martins6/opencode-jobs --skill opencode-jobs -a opencode -y
```

The skill teaches OpenCode how to write, create, test, and troubleshoot jobs.
After installation, start a new session and ask:

```text
Schedule a daily job at 9am to search for standing desks under $300
and summarize the five best deals.
```

See [docs/install.md](docs/install.md) for upgrades, uninstalls, and
troubleshooting the installation.

## Quick start

```bash
# Create a job (defaults to the current working directory)
opencode-jobs add \
  --name "Daily deals" \
  --cron "0 9 * * *" \
  --prompt "Search for standing desks under \$300 and summarize the five best deals." \
  --timeout 30m

# List jobs in this project
opencode-jobs list

# Test it now, in the foreground
opencode-jobs run daily-deals --wait

# Read the log
opencode-jobs logs daily-deals -n 50
```

A minimal job is just a name, a cron schedule, and a prompt:

```bash
opencode-jobs add --name "Weekly report" --cron "0 8 * * 1" \
  --prompt "Summarize my GitHub notifications and write the report to reports/weekly.md."
```

## Commands

| Command | What it does |
| --- | --- |
| `opencode-jobs add` | Create a job and install its scheduler entry |
| `opencode-jobs list` | List jobs in the current workspace (`--all` for every workspace) |
| `opencode-jobs get <id>` | Show configuration, scheduler state, and recent runs |
| `opencode-jobs update <id>` | Change a job; only the flags you pass are changed |
| `opencode-jobs remove <id>` | Remove the scheduler entry, job file, log, and run history |
| `opencode-jobs run <id>` | Run now (detached by default; `--wait` for foreground) |
| `opencode-jobs logs <id>` | Tail the log (`-n 0` for everything, `--follow` to stream) |
| `opencode-jobs reinstall` | Re-point scheduler entries at the current binary |
| `opencode-jobs cleanup` | Dry-run or remove jobs and scheduler artifacts |
| `opencode-jobs doctor` | Check OpenCode, the backend, the config directory, and every job |
| `opencode-jobs version` | Print version and build information |

Most commands accept `--json` for scripts and agents. Errors are also returned as
JSON on stdout when `--json` is set.

### Useful flags for `add`

| Flag | Meaning |
| --- | --- |
| `--workdir DIR` | Directory the job runs from (defaults to the current directory) |
| `--timeout 30m` | Hard timeout (`0` disables; seconds also accepted) |
| `--agent NAME` | OpenCode agent |
| `--model provider/model` | OpenCode model |
| `--file PATH` | Attach a file (repeatable) |
| `--env KEY=VALUE` | Extra environment variable (repeatable) |
| `--server URL` | Connect to a specific OpenCode server |
| `--standalone` | Run with a private OpenCode server |
| `--archived` | Keep scheduled sessions out of the normal session list |
| `--no-auto` | Do not pass `--auto` to `opencode run` |
| `--title TEXT` | Session title (defaults to the job name) |

## How it works

1. `add` writes a job file under the job's workspace scope.
2. It installs a scheduler entry that invokes the same binary with the hidden
   `__supervise` command.
3. At the scheduled time the supervisor takes a lock, runs
   `opencode run --auto ... -- <prompt>` from the job's working directory, and
   appends stdout/stderr to the job log.
4. The supervisor updates the job's last-run metadata and appends a JSONL run
   record.

Scheduled runs are non-interactive. OpenCode V2 automatically cancels `question`
requests during `opencode run` and tells the model to continue with reasonable
assumptions. `--auto` approves other permission requests unless your config
explicitly denies them. Use `--no-auto` if you would rather reject them.

Jobs are scoped by their normalized working directory. Running `opencode-jobs
list` inside a project shows that project's jobs; `--all` shows every workspace.
Scopes use a short hash of the path so different projects do not collide even
when their directory names are the same.

### Reliability

- **No overlap:** a job lock prevents a second run while one is active. Stale
  locks from dead processes are cleaned up automatically.
- **Optional timeout:** `--timeout` sends SIGTERM to the whole process group,
  waits five seconds, then sends SIGKILL. Timed-out runs exit with code 124.
- **Missed runs:** systemd timers use `Persistent=true`; launchd runs missed
  calendar events when the machine wakes.
- **Logs are the source of truth:** each run appends a header, the OpenCode
  output, and a footer with status, exit code, and duration.

## Cron syntax

Standard five-field cron expressions are supported:

```text
┌───────────── minute (0-59)
│ ┌───────────── hour (0-23)
│ │ ┌───────────── day of month (1-31)
│ │ │ ┌───────────── month (1-12)
│ │ │ │ ┌───────────── day of week (0-6, Sunday=0; 7 is also Sunday)
│ │ │ │ │
* * * * *
```

Examples:

| Expression | Meaning |
| --- | --- |
| `0 9 * * *` | Daily at 09:00 |
| `*/15 * * * *` | Every 15 minutes |
| `0 */6 * * *` | Every 6 hours |
| `30 8 * * 1` | Mondays at 08:30 |
| `0 9,17 * * *` | Daily at 09:00 and 17:00 |
| `0 0 1 * *` | First day of every month at midnight |

Macros are accepted too: `@hourly`, `@daily`, `@midnight`, `@weekly`,
`@monthly`, `@yearly`, and `@annually`.

When both day-of-month and day-of-week are restricted, cron treats them as an
OR. The launchd and systemd converters preserve that behavior.

## Storage

```text
~/.config/opencode-jobs/
└── scopes/
    └── <scope>/
        ├── jobs/<id>.json       # job definition (0600)
        ├── logs/<id>.log        # append-only run log (0600)
        ├── runs/<id>.jsonl      # one record per run (0600)
        └── locks/<id>.json      # active run lock
```

Override the root with `OPENCODE_JOBS_HOME` or `XDG_CONFIG_HOME`.

Scheduler entries:

| Platform | Location |
| --- | --- |
| macOS | `~/Library/LaunchAgents/com.opencode.jobs.<scope>.<id>.plist` |
| Linux + systemd | `~/.config/systemd/user/opencode-jobs-<scope>-<id>.{service,timer}` |
| Linux without systemd | A marked block in the user's `crontab` |

## Environment

| Variable | Purpose |
| --- | --- |
| `OPENCODE_JOBS_HOME` | Override the config directory |
| `OPENCODE_JOBS_OPENCODE_PATH` | Use a specific `opencode` executable |
| `OPENCODE_JOBS_BACKEND` | Force `launchd`, `systemd`, or `cron` |
| `OPENCODE_JOBS_INSTALL_DIR` | Installer prefix |
| `OPENCODE_JOBS_VERSION` | Installer release tag |

The supervisor enriches `PATH` with common tool locations, sets `HOME`,
`TMPDIR`, `LANG`, `SHELL`, `NO_COLOR=1`, and applies any `--env` values.
Provider credentials should live in OpenCode's own auth store or in the
environment instead of a job prompt.

## Security

- Job files, logs, run records, and locks are written with `0600` permissions
  inside a `0700` directory tree.
- Scheduled jobs persist across reboots and run with your user's authority.
  Review prompts before scheduling them and use the narrowest `--workdir`.
- Jobs that read untrusted content (web pages, email, issues, API responses)
  should use `--no-auto` and the delimiter rules in the skill's prompt guide.
- Prefer Homebrew or `go install`. If you use the install script, download and
  review it before running rather than piping it to a shell.
- `--env` values are stored in the job file. Do not put long-lived secrets in a
  job unless you are comfortable with that file's permissions.
- Scheduled runs execute with your user's authority. Only create jobs you would
  be comfortable running yourself.
- `opencode-jobs cleanup` is a dry run until you pass `--confirm`.
- Jobs that are currently running cannot be removed or cleaned up.

## Troubleshooting

```bash
opencode-jobs doctor
opencode-jobs logs <id> -n 200
```

Common checks:

```bash
# macOS: are launch agents loaded?
launchctl list | grep opencode.jobs
launchctl print gui/$(id -u)/com.opencode.jobs.<scope>.<id>

# Linux: are the timers active?
systemctl --user list-timers | grep opencode-jobs
systemctl --user status opencode-jobs-<scope>-<id>.timer

# Linux cron fallback
crontab -l | grep -A2 opencode-jobs
```

If a scheduler entry points at an old binary after an upgrade or a move, run:

```bash
opencode-jobs reinstall --all
```

More detail is in [docs/install.md](docs/install.md) and the skill's
`references/troubleshooting.md`.

## Development

Requirements: Go 1.23 or newer, and macOS or Linux.

```bash
gofmt -w .
go vet ./...
go test ./...
go build -o opencode-jobs .
```

The CLI has no third-party Go dependencies. Backend code is split between
launchd, systemd, and cron implementations in `internal/jobs/backend.go`.

Release flow:

- Pushes to `main` build all four binaries, publish a rolling `latest`
  prerelease, and upload the release assets.
- Tags matching `v*` publish a versioned release with `checksums.txt`.
- Stable tags also update the formula in
  [`Martins6/homebrew-tap`](https://github.com/Martins6/homebrew-tap) when the
  `HOMEBREW_TAP_TOKEN` secret is configured.

See [AGENTS.md](AGENTS.md) for repository conventions.

## License

MIT. See [LICENSE](LICENSE).
