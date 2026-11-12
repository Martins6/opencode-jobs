// Package opencode locates and inspects the OpenCode CLI.
package opencode

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CommonDirs are added to PATH for scheduled runs, which start with a minimal
// environment under launchd, systemd, and cron.
func CommonDirs() []string {
	home, _ := os.UserHomeDir()
	dirs := []string{
		"/opt/homebrew/bin",
		"/usr/local/bin",
		"/usr/bin",
		"/bin",
		"/usr/sbin",
		"/sbin",
	}
	if home != "" {
		dirs = append(dirs,
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, ".opencode", "bin"),
			filepath.Join(home, "go", "bin"),
			filepath.Join(home, ".bun", "bin"),
			filepath.Join(home, ".npm-global", "bin"),
		)
	}
	return dirs
}

// EnhancedPath appends common tool directories to an existing PATH.
func EnhancedPath(existing string) string {
	var dirs []string
	seen := map[string]struct{}{}
	add := func(dir string) {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			return
		}
		if _, ok := seen[dir]; ok {
			return
		}
		seen[dir] = struct{}{}
		dirs = append(dirs, dir)
	}
	for _, dir := range filepath.SplitList(existing) {
		add(dir)
	}
	for _, dir := range CommonDirs() {
		add(dir)
	}
	return strings.Join(dirs, string(os.PathListSeparator))
}

// Find returns the opencode executable path, or "opencode" when it cannot be
// resolved. OPENCODE_JOBS_OPENCODE_PATH overrides discovery.
func Find() string {
	if override := strings.TrimSpace(os.Getenv("OPENCODE_JOBS_OPENCODE_PATH")); override != "" {
		return override
	}
	for _, dir := range filepath.SplitList(EnhancedPath(os.Getenv("PATH"))) {
		candidate := filepath.Join(dir, "opencode")
		if isExecutable(candidate) {
			return candidate
		}
	}
	if resolved, err := exec.LookPath("opencode"); err == nil {
		return resolved
	}
	return "opencode"
}

// Version runs `opencode --version` and returns the first output line.
func Version() (string, error) {
	command := Find()
	cmd := exec.Command(command, "--version")
	cmd.Env = replaceEnv(os.Environ(), "PATH", EnhancedPath(os.Getenv("PATH")))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("cannot run %s --version: %w", command, err)
	}
	line := strings.TrimSpace(string(out))
	if index := strings.IndexByte(line, '\n'); index >= 0 {
		line = line[:index]
	}
	return line, nil
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode()&0o111 != 0
}

func replaceEnv(env []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	replaced := false
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			if !replaced {
				out = append(out, prefix+value)
				replaced = true
			}
			continue
		}
		out = append(out, entry)
	}
	if !replaced {
		out = append(out, prefix+value)
	}
	return out
}
