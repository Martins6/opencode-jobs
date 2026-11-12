package jobs

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// SelfPath returns a stable absolute path to the running CLI.
//
// os.Args[0] is preferred over os.Executable because package managers such as
// Homebrew invoke the stable symlink (for example /opt/homebrew/bin/opencode-jobs)
// while os.Executable may resolve it to a versioned Cellar path that disappears
// on upgrade.
func SelfPath() (string, error) {
	arg0 := os.Args[0]
	if strings.ContainsRune(arg0, os.PathSeparator) {
		if abs, err := filepath.Abs(arg0); err == nil {
			return abs, nil
		}
	}
	if resolved, err := exec.LookPath(arg0); err == nil {
		if abs, err := filepath.Abs(resolved); err == nil {
			return abs, nil
		}
		return resolved, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("cannot determine opencode-jobs binary path: %w", err)
	}
	return exe, nil
}
