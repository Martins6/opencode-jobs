# Writing job prompts

A scheduled prompt has no human in the loop. Everything the job needs must be
stated or discoverable from the working directory. The prompt is sent to
`opencode run` exactly as written, so do not rely on placeholders, injected
dates, or interactive approvals.

## Template

```text
You are running a scheduled job: <job name>.

Working directory: <absolute path>
Schedule: <cron expression, local timezone>

Goal
<One or two sentences describing the successful outcome.>

Inputs
- <files, APIs, MCP tools, or accounts to use>
- <where credentials come from, e.g. "use the Telegram env vars already configured in opencode.json">

Steps
1. Preflight: verify required tools, files, and environment variables. If a
   required dependency is missing, stop and report a single concise reason.
2. Compute runtime values (dates, IDs) with shell commands; never hard-code
   today's date.
3. Do the work.
4. Write durable outputs under outputs/<job-id>/.
5. Update the idempotency state only after a successful side effect.
6. Send the notification if there is something new to report.
7. Print a compact summary.

Constraints
- Non-interactive: never wait for a question, QR login, or confirmation.
- Idempotent: rerunning the job must not duplicate messages or records.
- Do not invent data. If a source is unavailable, say so.
- Keep side effects inside the working directory unless the task explicitly
  requires an external service.
- Do not print secrets, tokens, or full API responses.

Output contract
End with:
- Status: success | skipped | failed
- Reason: one line
- Outputs: paths written
- Notifications: service and message id, if any
```

## Runtime dates

Scheduled runs start fresh shells. Compute dates at runtime.

Portable:

```bash
TODAY="$(date +%F)"
YESTERDAY="$(date -v-1d +%F 2>/dev/null || date -d 'yesterday' +%F)"
TOMORROW="$(date -v+1d +%F 2>/dev/null || date -d 'tomorrow' +%F)"
```

If the task depends on a timezone, make it explicit:

```bash
TZ=America/Los_Angeles date +%F
```

## Idempotency

For "new items only" jobs, keep a seen list:

```text
outputs/<job-id>/seen.json
```

Pattern:

1. Read the seen list; treat a missing file as an empty list.
2. Filter source items against the list.
3. Do the work for new items.
4. Append only successfully handled item ids.
5. Write the file atomically (write a temp file, then rename).
6. If nothing is new, print `Status: skipped` and exit successfully.

Always store stable source ids, not titles or positions that can change between
runs.

## Notifications

Prefer a non-interactive API over a browser automation flow. For Telegram, use
the Bot API with credentials already available to OpenCode:

```bash
curl -fsS "https://api.telegram.org/bot${TELEGRAM_BOT_TOKEN}/sendMessage" \
  -d "chat_id=${TELEGRAM_CHAT_ID}" \
  --data-urlencode "text=${MESSAGE}"
```

Rules:

- Never put the bot token or chat id in the job prompt.
- Put credentials in the project's `opencode.json`/MCP config, the user's
  environment, or OpenCode's auth store.
- Check for API errors and report them; do not silently mark a failed send as
  success.
- Record the returned message id so reruns can be audited.

For Slack, email, or webhooks, use the service's API and the same rules.

## Pick the right scope

Use `--workdir` to point the job at the project whose configuration the task
needs:

```bash
opencode-jobs add \
  --name "Weekly repo report" \
  --cron "0 8 * * 1" \
  --prompt "Summarize merged PRs from the last 7 days and write reports/weekly.md." \
  --workdir "$HOME/code/my-project" \
  --timeout 20m
```

Jobs run from that directory, so relative paths in the prompt resolve there and
OpenCode loads that project's `opencode.json`, MCP servers, and skills.

If the job needs a project skill, mention it at the top of the prompt:

```text
@my-project-skill

<task>
```

## Choose an agent or model

Only set these when the user has a preference or the task needs a specific
capability:

```bash
opencode-jobs add ... --agent build --model provider/model
```

Use the model format accepted by OpenCode V2, including an optional variant:

```text
provider/model#variant
```

Leaving both unset uses the project's normal defaults.

## Test checklist

Before declaring the job ready:

```bash
opencode-jobs get <id> --json
opencode-jobs run <id> --wait
opencode-jobs logs <id> -n 200
```

Confirm:

- exit code is zero
- the log shows the expected work
- output files exist and contain real data
- the notification was sent exactly once
- rerunning produces `skipped` or updates data without duplicating side effects
- the timeout is long enough for a normal run but not unbounded

## Common tasks

### Daily research digest

```text
Collect the top 10 items from <sources> published in the last 24 hours.
Deduplicate against outputs/digest/seen.json. Score each item for relevance to
<topic>. Write the top 5 to outputs/digest/YYYY-MM-DD.md with links and a
one-paragraph summary. Send the digest to Telegram and record the message id.
```

### Weekly report

```text
Summarize <repository/metrics/inbox> for the last 7 days. Include:
- what changed
- blockers or risks
- three recommended actions
Write the report to reports/weekly-YYYY-MM-DD.md. Do not send a notification if
there is nothing notable.
```

### Uptime monitor

```text
Check https://example.com/health every run. If it fails three consecutive times,
send one alert and record the failure state in outputs/health/state.json. Do not
send repeated alerts while the state is unchanged. Recover silently and reset the
counter after a successful check.
```

### Reminder

```text
Read reminders.json in the working directory. Send the reminders whose due date
is today. Mark them as sent in reminders.json. If none are due, print
`Status: skipped`.
```

## Anti-patterns

- **Hard-coded dates**: "today" must be computed at runtime.
- **Interactive steps**: QR codes, browser logins, "ask the user", or
  confirmation dialogs will not work unattended.
- **Unbounded loops**: set a timeout and make the task finite.
- **Duplicate notifications**: keep state and check it before sending.
- **Relative assumptions**: state the workdir or use absolute paths.
- **Secrets in prompts**: use environment variables or OpenCode configuration.
- **Silent failures**: always print a final status and reason.
- **Destructive defaults**: never delete or overwrite user data without an
  explicit instruction in the prompt and confirmation from the user.
