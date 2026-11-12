# Repository guide

`opencode-jobs` schedules OpenCode V2 prompts through native OS schedulers. It is
deliberately a CLI plus a skill, not an OpenCode plugin.

## Layout

```text
main.go                         Entry point
internal/cli/                   Command parsing and output
internal/jobs/job.go            Job schema, scopes, validation
internal/jobs/store.go          Config directory, locks, logs, run records
internal/jobs/cron.go           Cron parser and launchd/systemd conversion
internal/jobs/backend.go        launchd, systemd, and cron installers
internal/jobs/supervisor.go     Locking, timeout, child process, metadata
internal/jobs/invocation.go     `opencode run` arguments and environment
internal/opencode/              OpenCode discovery and PATH handling
skills/opencode-jobs/           Installable skill
docs/                           Installation and usage documentation
```

## Conventions

- Go 1.23+, standard library only. Avoid adding dependencies unless there is a
  strong reason.
- All user-facing commands support `--json`; keep the shape stable.
- Never edit a user's scheduler files by hand from another command. Use the
  functions in `internal/jobs/backend.go`.
- Job files, logs, runs, and locks are `0600` inside `0700` directories.
- Jobs are scoped by normalized workdir. Preserve the scope hash behavior; it is
  what keeps projects with the same directory name from colliding.
- The CLI targets OpenCode V2 command-line semantics:
  `opencode run --auto ... -- <prompt>`. Do not add V1 flags such as `--attach`,
  `--port`, `--command`, `--share`, or `--variant`.
- `--auto` is enabled by default. OpenCode V2 already cancels questions during
  non-interactive runs, so do not add permission overlays to the child
  environment.
- Keep the public docs and the skill in sync with the flags. The skill is part of
  the product, not an afterthought.

## Development

```bash
gofmt -w .
go vet ./...
go test ./...
go build -o opencode-jobs .
```

Cross-compile the release targets:

```bash
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -o /tmp/opencode-jobs .
CGO_ENABLED=0 GOOS=linux  GOARCH=amd64 go build -trimpath -o /tmp/opencode-jobs .
```

Unit tests do not install real scheduler entries. For a manual end-to-end test,
use a temporary home and remove the job afterward:

```bash
export OPENCODE_JOBS_HOME="$(mktemp -d)"
opencode-jobs add --name "Smoke" --cron "0 9 * * *" --prompt "Say hello."
opencode-jobs run smoke --wait
opencode-jobs remove smoke
unset OPENCODE_JOBS_HOME
```

On macOS this installs and removes a real LaunchAgent. On Linux it installs and
removes a systemd timer or a marked crontab block.

## Release

- Push to `main`: build darwin/linux on arm64/amd64, publish a rolling
  `latest` prerelease, and upload workflow artifacts.
- Push a tag `vX.Y.Z`: publish a stable release with `checksums.txt`.
- Stable tags update `Martins6/homebrew-tap` via
  `scripts/generate-homebrew-formula.sh` when `HOMEBREW_TAP_TOKEN` is set.

Do not edit release assets manually. Re-run the workflow or upload with
`gh release upload --clobber`.

## Skill

The skill lives at `skills/opencode-jobs/` so the Vercel `skills` CLI discovers it
with:

```bash
npx skills add Martins6/opencode-jobs --skill opencode-jobs -g -a opencode -y
```

Keep `SKILL.md` focused on the workflow and move long reference material into
`references/`.
