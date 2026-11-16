# Installation

`opencode-jobs` ships as a single statically linked Go binary for macOS and
Linux. It installs scheduler entries that call the binary directly, so there is
no runtime, Node.js, or OpenCode plugin to keep loaded.

## Requirements

- OpenCode V2 (`opencode run` must work from your shell)
- macOS 12+ or a Linux distribution with systemd or cron
- `curl` or `wget` for the script installer

Check OpenCode first:

```bash
opencode --version
opencode run "Say hello"
```

## Homebrew

```bash
brew tap Martins6/tap
brew install opencode-jobs
```

The formula downloads the prebuilt release for your OS and architecture
(macOS/Linux, arm64/amd64), installs the binary into Homebrew's `bin`, and
includes a `version` smoke test for `brew test`.

Upgrade:

```bash
brew update
brew upgrade opencode-jobs
opencode-jobs reinstall --all
```

`reinstall` is only needed when existing scheduler entries must point at the new
binary path. Homebrew's stable symlink usually makes this unnecessary, but it is
safe to run.

## Install script

Prefer Homebrew or `go install`. The script is a fallback for machines without
them. It runs with your user's authority, so download it, review it, and run it
from a file. Do not pipe it into a shell.

```bash
curl -fsSL https://raw.githubusercontent.com/Martins6/opencode-jobs/main/install.sh \
  -o /tmp/opencode-jobs-install.sh
less /tmp/opencode-jobs-install.sh
bash /tmp/opencode-jobs-install.sh
```

The script:

1. Detects macOS/Linux and arm64/amd64.
2. Downloads the tarball and `checksums.txt` from the selected release.
3. Verifies the SHA-256 checksum.
4. Installs to `~/.local/bin/opencode-jobs` (override with `--prefix`).
5. Prints a PATH hint if the directory is not on `PATH`.

The script verifies the release tarball. It does not verify itself; review it,
or compare it with the copy in the repository before running.

Examples:

```bash
# Install a specific release
bash /tmp/opencode-jobs-install.sh --version v0.1.0

# Latest rolling build from main (prerelease)
bash /tmp/opencode-jobs-install.sh --main

# Custom location
bash /tmp/opencode-jobs-install.sh --prefix "$HOME/bin"
```

Environment equivalents:

```bash
OPENCODE_JOBS_VERSION=v0.1.0 \
OPENCODE_JOBS_INSTALL_DIR="$HOME/bin" \
  bash /tmp/opencode-jobs-install.sh
```

## Go install

```bash
go install github.com/Martins6/opencode-jobs@latest
```

Make sure `$(go env GOPATH)/bin` is on `PATH`.

## From source

```bash
git clone https://github.com/Martins6/opencode-jobs.git
cd opencode-jobs
go build -o opencode-jobs .
install -m 0755 opencode-jobs ~/.local/bin/opencode-jobs
```

## Install the skill

The skill is the recommended way to use the CLI from OpenCode. It contains the
command reference, prompt-writing guidance, and troubleshooting steps.

Global install (available in every project):

```bash
npx skills add Martins6/opencode-jobs --skill opencode-jobs -g -a opencode -y
```

Project install (committed with the repository):

```bash
npx skills add Martins6/opencode-jobs --skill opencode-jobs -a opencode -y
```

List the available skills without installing:

```bash
npx skills add Martins6/opencode-jobs --list
```

Update an installed skill:

```bash
npx skills update opencode-jobs
```

After installation, start a new OpenCode session so the skill is discovered.

## Verify the installation

```bash
opencode-jobs version
opencode-jobs doctor
```

`doctor` prints:

- the config directory and current workspace scope
- the detected scheduler backend
- the `opencode` executable and version
- every job, its scheduler status, and its last run
- concrete issues and the `reinstall` hint when entries are stale

Create a harmless test job:

```bash
mkdir -p /tmp/opencode-jobs-smoke
cd /tmp/opencode-jobs-smoke

opencode-jobs add \
  --name "Smoke test" \
  --cron "0 9 * * *" \
  --prompt "Print a short hello message." \
  --timeout 2m

opencode-jobs run smoke-test --wait
opencode-jobs logs smoke-test -n 20
opencode-jobs remove smoke-test
```

## PATH

If the installer says the directory is not on `PATH`, add it to your shell
profile:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

For `fish`:

```fish
fish_add_path $HOME/.local/bin
```

Open a new terminal or reload the profile, then confirm:

```bash
command -v opencode-jobs
opencode-jobs version
```

## Uninstall

Remove every job (dry run first):

```bash
opencode-jobs cleanup --all-scopes
opencode-jobs cleanup --all-scopes --include-history --confirm
```

Then remove the binary:

```bash
# Homebrew
brew uninstall opencode-jobs
# Optional: only if you no longer use other formulas from this tap.
# brew untap Martins6/tap

# Script or source install
rm -f ~/.local/bin/opencode-jobs

# Config, logs, and run history
rm -rf ~/.config/opencode-jobs
```

On Linux, also remove any leftover user units if you deleted files manually:

```bash
systemctl --user list-timers | grep opencode-jobs
systemctl --user disable --now opencode-jobs-<scope>-<id>.timer
```

## Release channels

| Channel | How to install | Notes |
| --- | --- | --- |
| Stable | `brew install opencode-jobs` or `install.sh` | Tagged releases such as `v0.1.0` |
| Specific tag | `install.sh --version v0.1.0` | Reproducible |
| Rolling main | `install.sh --main` | Prerelease built on every push to `main` |

Release assets are tarballs named
`opencode-jobs_<os>_<arch>.tar.gz` plus `checksums.txt`.

## Homebrew tap maintenance

The formula lives in
[`Martins6/homebrew-tap`](https://github.com/Martins6/homebrew-tap) at
`Formula/opencode-jobs.rb`. The release workflow regenerates it for stable tags
using `scripts/generate-homebrew-formula.sh` and pushes it when the
`HOMEBREW_TAP_TOKEN` repository secret is available. Without the secret, the
workflow warns and skips the tap update; run the script manually and commit the
formula.

To enable automatic updates, create a token that can write to
`Martins6/homebrew-tap` (a fine-grained token with **Contents: Read and write**
is enough) and store it in the main repository:

```bash
gh secret set HOMEBREW_TAP_TOKEN -R Martins6/opencode-jobs
```

## Troubleshooting installation

**`opencode-jobs: command not found`**

The install directory is not on `PATH`. Check `command -v opencode-jobs` and
add the directory as shown above.

**`opencode not found` from `doctor`**

Install OpenCode V2, or point the tool at the executable:

```bash
export OPENCODE_JOBS_OPENCODE_PATH="$HOME/.opencode/bin/opencode"
```

**`no scheduler backend found` on Linux**

Install systemd (user manager) or cron:

```bash
command -v systemctl
command -v crontab
```

On minimal containers, there may be no user scheduler at all. Use a machine with
systemd or cron, or run jobs through another supervisor.

**A job still points at a deleted binary**

```bash
opencode-jobs reinstall --all
```

**`doctor` reports `scheduler entry is inactive`**

Reload the entry:

```bash
opencode-jobs reinstall <id>
```

On Linux, confirm that user services are available and that lingering is enabled
if jobs must run while you are logged out:

```bash
loginctl enable-linger "$USER"
```
