# Installing opencode-jobs

`opencode-jobs` is a single Go binary. It installs native scheduler entries and
does not require an OpenCode plugin.

## Requirements

- OpenCode V2 with a working `opencode run`
- macOS 12+ or Linux with systemd or cron
- `curl` or `wget` for the script installer

Verify OpenCode:

```bash
opencode --version
opencode run "Say hello"
```

## Homebrew

```bash
brew tap Martins6/tap
brew install opencode-jobs
opencode-jobs version
```

Upgrade:

```bash
brew update && brew upgrade opencode-jobs
opencode-jobs reinstall --all
```

## Install script

```bash
curl -fsSL https://raw.githubusercontent.com/Martins6/opencode-jobs/main/install.sh | bash
```

The default prefix is `~/.local/bin`. The script verifies the release checksum.

```bash
# Specific release
curl -fsSL https://raw.githubusercontent.com/Martins6/opencode-jobs/main/install.sh \
  | bash -s -- --version v0.1.0

# Latest main build
curl -fsSL https://raw.githubusercontent.com/Martins6/opencode-jobs/main/install.sh \
  | bash -s -- --main

# Custom prefix
curl -fsSL https://raw.githubusercontent.com/Martins6/opencode-jobs/main/install.sh \
  | bash -s -- --prefix "$HOME/bin"
```

## Go

```bash
go install github.com/Martins6/opencode-jobs@latest
```

## Skill

Global:

```bash
npx skills add Martins6/opencode-jobs --skill opencode-jobs -g -a opencode -y
```

Project-local:

```bash
npx skills add Martins6/opencode-jobs --skill opencode-jobs -a opencode -y
```

Update:

```bash
npx skills update opencode-jobs
```

Start a new OpenCode session after installing the skill.

## Verify

```bash
opencode-jobs version
opencode-jobs doctor
```

If the command is not found, add the install directory to `PATH`:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

## Uninstall

```bash
opencode-jobs cleanup --all-scopes
opencode-jobs cleanup --all-scopes --include-history --confirm
brew uninstall opencode-jobs   # or: rm -f ~/.local/bin/opencode-jobs
rm -rf ~/.config/opencode-jobs
```

## Release channels

| Channel | Command |
| --- | --- |
| Stable | `brew install opencode-jobs` or default `install.sh` |
| Tag | `install.sh --version v0.1.0` |
| Rolling main | `install.sh --main` |

Releases are published at
<https://github.com/Martins6/opencode-jobs/releases>.
