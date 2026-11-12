# Troubleshooting scheduled jobs

Start with the built-in checks:

```bash
opencode-jobs doctor
opencode-jobs get <id> --json
opencode-jobs logs <id> -n 200
```

`doctor` reports the config home, current scope, backend, OpenCode version, every
job, and stale scheduler entries.

## The job never runs

1. Confirm the scheduler entry exists:

```bash
opencode-jobs get <id>
```

2. Check the backend directly.

**macOS (launchd)**

```bash
launchctl list | grep opencode.jobs
launchctl print gui/$(id -u)/com.opencode.jobs.<scope>.<id>
plutil -lint ~/Library/LaunchAgents/com.opencode.jobs.<scope>.<id>.plist
```

**Linux (systemd user)**

```bash
systemctl --user list-timers | grep opencode-jobs
systemctl --user status opencode-jobs-<scope>-<id>.timer
systemctl --user status opencode-jobs-<scope>-<id>.service
journalctl --user -u opencode-jobs-<scope>-<id>.service -n 100
```

**Linux (cron fallback)**

```bash
crontab -l
crontab -l | grep -A2 opencode-jobs
```

3. Repair stale entries:

```bash
opencode-jobs reinstall <id>
# or every job
opencode-jobs reinstall --all
```

## The job runs but fails immediately

Read the log:

```bash
opencode-jobs logs <id> -n 200
```

Common causes:

| Symptom | Fix |
| --- | --- |
| `opencode: command not found` | Install OpenCode or set `OPENCODE_JOBS_OPENCODE_PATH`, then `reinstall --all` |
| Provider/auth error | Log in interactively with OpenCode; scheduled runs use the same auth store |
| MCP tools missing | Run from a workdir containing the right `opencode.json`/`.opencode` config |
| Model unavailable | Pass a valid `--model provider/model` or let the project default apply |
| Permission denied | Use the default `--auto`, or grant the permission in the project config |
| Timeout, exit 124 | Raise `--timeout` or make the task smaller |

## The job is skipped

A lock is held by another run:

```bash
opencode-jobs get <id>
```

Wait for the active run to finish. Stale locks left by dead supervisor processes
are removed automatically on the next attempt. Do not delete lock files while a
run is active.

## The job runs twice or sends duplicate notifications

- Keep an idempotency state file in the prompt, for example
  `outputs/<job-id>/seen.json`.
- Only update the state after a successful side effect.
- Check the run history:

```bash
opencode-jobs get <id> --runs 20
```

If the OS scheduler fired a missed-run catch-up (launchd wake behavior or
systemd `Persistent=true`), the job may run shortly after wake. The lock still
prevents overlap, but idempotency is what prevents duplicate messages.

## The scheduler entry points at an old binary

```bash
opencode-jobs reinstall --all
```

This is common after moving the binary out of a temporary build directory or
changing install prefixes. Homebrew's stable symlink usually avoids it.

## Nothing works on Linux

Check the scheduler backends:

```bash
command -v systemctl
systemctl --user show-environment
command -v crontab
```

- If `systemctl --user` fails, the environment has no user systemd manager. The
  CLI falls back to cron when `crontab` exists.
- If neither is available, use a machine with systemd or cron. Minimal
  containers often have no scheduler.
- To run user timers while logged out:

```bash
loginctl enable-linger "$USER"
```

## "No jobs in scope ..."

Jobs are scoped by working directory. Run the command from the project that owns
the job, or use `--all`:

```bash
opencode-jobs list --all
opencode-jobs get <id> --all
```

## Cleanup and removal

```bash
# Dry run for the current project
opencode-jobs cleanup

# Remove every job in every project, keeping logs/history
opencode-jobs cleanup --all-scopes --confirm

# Remove logs and history too
opencode-jobs cleanup --all-scopes --include-history --confirm
```

Jobs that are currently running cannot be removed or cleaned up. Wait for them
to finish.

## Collecting diagnostics

```bash
opencode-jobs version
opencode-jobs doctor --json
opencode-jobs list --all --json
opencode-jobs logs <id> -n 200
```

Redact prompts, tokens, chat ids, and file contents before sharing logs.
