package jobs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/Martins6/opencode-jobs/internal/opencode"
)

// BackendRef describes the installed scheduler unit.
type BackendRef struct {
	Name  string   `json:"backend"`
	Unit  string   `json:"unit,omitempty"`
	Paths []string `json:"paths,omitempty"`
}

// DetectBackend returns the scheduler backend for this machine.
func DetectBackend() (string, error) {
	if override := strings.TrimSpace(os.Getenv("OPENCODE_JOBS_BACKEND")); override != "" {
		switch override {
		case "launchd":
			if runtime.GOOS != "darwin" {
				return "", fmt.Errorf("OPENCODE_JOBS_BACKEND=launchd requires macOS")
			}
			return "launchd", nil
		case "systemd":
			if !systemdUserAvailable() {
				return "", fmt.Errorf("OPENCODE_JOBS_BACKEND=systemd requested, but the systemd user manager is unavailable")
			}
			return "systemd", nil
		case "cron":
			if _, err := exec.LookPath("crontab"); err != nil {
				return "", fmt.Errorf("OPENCODE_JOBS_BACKEND=cron requested, but crontab was not found")
			}
			return "cron", nil
		default:
			return "", fmt.Errorf("unknown OPENCODE_JOBS_BACKEND %q (use launchd, systemd, or cron)", override)
		}
	}

	switch runtime.GOOS {
	case "darwin":
		return "launchd", nil
	case "linux":
		if systemdUserAvailable() {
			return "systemd", nil
		}
		if _, err := exec.LookPath("crontab"); err == nil {
			return "cron", nil
		}
		return "", errors.New("no scheduler backend found: systemd user manager is unavailable and crontab is not installed")
	default:
		return "", fmt.Errorf("unsupported operating system %s: opencode-jobs supports macOS and Linux", runtime.GOOS)
	}
}

// Install installs (or reinstalls) the OS scheduler entry for a job.
func Install(j *Job, bin string, store *Store) (*BackendRef, error) {
	backend, err := DetectBackend()
	if err != nil {
		return nil, err
	}
	if err := store.EnsureJobDirs(j); err != nil {
		return nil, err
	}
	switch backend {
	case "launchd":
		return installLaunchd(j, bin, store)
	case "systemd":
		return installSystemd(j, bin, store)
	case "cron":
		return installCron(j, bin, store)
	default:
		return nil, fmt.Errorf("unsupported backend %q", backend)
	}
}

// Uninstall removes the OS scheduler entry recorded on the job.
func Uninstall(j *Job) error {
	switch j.Backend {
	case "":
		return nil
	case "launchd":
		return uninstallLaunchd(j)
	case "systemd":
		return uninstallSystemd(j)
	case "cron":
		return uninstallCron(j)
	default:
		return fmt.Errorf("unknown backend %q on job %s", j.Backend, j.Slug)
	}
}

// Status returns a short scheduler status string for a job.
func Status(j *Job) (string, error) {
	switch j.Backend {
	case "":
		return "not installed", nil
	case "launchd":
		return statusLaunchd(j), nil
	case "systemd":
		return statusSystemd(j), nil
	case "cron":
		return statusCron(j), nil
	default:
		return "", fmt.Errorf("unknown backend %q on job %s", j.Backend, j.Slug)
	}
}

// === launchd ===

func launchdLabel(j *Job) string {
	return "com.opencode.jobs." + j.ScopeID + "." + j.Slug
}

func launchdPlistPath(j *Job) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", launchdLabel(j)+".plist"), nil
}

func installLaunchd(j *Job, bin string, store *Store) (*BackendRef, error) {
	plistPath, err := launchdPlistPath(j)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(plistPath), 0o755); err != nil {
		return nil, err
	}
	plist, err := renderLaunchdPlist(j, bin, store.JobPath(j.ScopeID, j.Slug), store.LogPath(j))
	if err != nil {
		return nil, err
	}

	uid := strconv.Itoa(os.Getuid())
	label := launchdLabel(j)
	// Best-effort removal of any previous instance.
	_, _ = runCommand("launchctl", "bootout", "gui/"+uid+"/"+label)
	_, _ = runCommand("launchctl", "bootout", "gui/"+uid, plistPath)
	_, _ = runCommand("launchctl", "unload", plistPath)

	if err := writeFileAtomic(plistPath, []byte(plist), 0o644); err != nil {
		return nil, err
	}
	if output, err := runCommand("launchctl", "bootstrap", "gui/"+uid, plistPath); err != nil {
		// Older macOS releases still accept the legacy loader.
		if output2, err2 := runCommand("launchctl", "load", "-w", plistPath); err2 != nil {
			return nil, fmt.Errorf("launchctl bootstrap failed: %s; launchctl load failed: %s", output, output2)
		}
	}
	_, _ = runCommand("launchctl", "enable", "gui/"+uid+"/"+label)
	return &BackendRef{Name: "launchd", Unit: label, Paths: []string{plistPath}}, nil
}

func uninstallLaunchd(j *Job) error {
	plistPath, err := launchdPlistPath(j)
	if err != nil {
		return err
	}
	uid := strconv.Itoa(os.Getuid())
	label := launchdLabel(j)
	_, _ = runCommand("launchctl", "bootout", "gui/"+uid+"/"+label)
	_, _ = runCommand("launchctl", "bootout", "gui/"+uid, plistPath)
	_, _ = runCommand("launchctl", "unload", plistPath)
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func statusLaunchd(j *Job) string {
	uid := strconv.Itoa(os.Getuid())
	if _, err := runCommand("launchctl", "print", "gui/"+uid+"/"+launchdLabel(j)); err == nil {
		return "loaded"
	}
	return "not loaded"
}

func renderLaunchdPlist(j *Job, bin, jobPath, logPath string) (string, error) {
	spec, err := ParseCron(j.Cron)
	if err != nil {
		return "", err
	}
	calendars := spec.LaunchdCalendars()

	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString("<plist version=\"1.0\">\n<dict>\n")
	writePlistKey(&b, "Label")
	writePlistString(&b, launchdLabel(j))
	writePlistKey(&b, "ProgramArguments")
	b.WriteString("\t<array>\n")
	for _, arg := range []string{bin, "__supervise", jobPath} {
		writePlistStringIndent(&b, arg, "\t\t")
	}
	b.WriteString("\t</array>\n")
	writePlistKey(&b, "WorkingDirectory")
	writePlistString(&b, j.Workdir)
	writePlistKey(&b, "EnvironmentVariables")
	b.WriteString("\t<dict>\n")
	writePlistKeyIndent(&b, "PATH", "\t\t")
	writePlistStringIndent(&b, opencode.EnhancedPath(os.Getenv("PATH")), "\t\t")
	b.WriteString("\t</dict>\n")
	writePlistKey(&b, "StartCalendarInterval")
	b.WriteString("\t<array>\n")
	orderedKeys := []string{"Minute", "Hour", "Day", "Month", "Weekday"}
	for _, calendar := range calendars {
		b.WriteString("\t\t<dict>\n")
		for _, key := range orderedKeys {
			value, ok := calendar[key]
			if !ok {
				continue
			}
			writePlistKeyIndent(&b, key, "\t\t\t")
			b.WriteString(fmt.Sprintf("\t\t\t<integer>%d</integer>\n", value))
		}
		b.WriteString("\t\t</dict>\n")
	}
	b.WriteString("\t</array>\n")
	writePlistKey(&b, "RunAtLoad")
	b.WriteString("\t<false/>\n")
	writePlistKey(&b, "ProcessType")
	writePlistString(&b, "Background")
	writePlistKey(&b, "StandardOutPath")
	writePlistString(&b, logPath)
	writePlistKey(&b, "StandardErrorPath")
	writePlistString(&b, logPath)
	b.WriteString("</dict>\n</plist>\n")
	return b.String(), nil
}

// === systemd ===

func systemdUnitBase(j *Job) string {
	return "opencode-jobs-" + j.ScopeID + "-" + j.Slug
}

func systemdUnitPaths(j *Job) (service string, timer string, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	dir := filepath.Join(home, ".config", "systemd", "user")
	return filepath.Join(dir, systemdUnitBase(j)+".service"),
		filepath.Join(dir, systemdUnitBase(j)+".timer"),
		nil
}

func installSystemd(j *Job, bin string, store *Store) (*BackendRef, error) {
	servicePath, timerPath, err := systemdUnitPaths(j)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(servicePath), 0o755); err != nil {
		return nil, err
	}
	service, err := renderSystemdService(j, bin, store)
	if err != nil {
		return nil, err
	}
	timer, err := renderSystemdTimer(j)
	if err != nil {
		return nil, err
	}

	timerUnit := systemdUnitBase(j) + ".timer"
	_, _ = runCommand("systemctl", "--user", "stop", timerUnit)
	_, _ = runCommand("systemctl", "--user", "disable", timerUnit)

	if err := writeFileAtomic(servicePath, []byte(service), 0o644); err != nil {
		return nil, err
	}
	if err := writeFileAtomic(timerPath, []byte(timer), 0o644); err != nil {
		return nil, err
	}
	if output, err := runCommand("systemctl", "--user", "daemon-reload"); err != nil {
		return nil, fmt.Errorf("systemctl --user daemon-reload failed: %s", output)
	}
	if output, err := runCommand("systemctl", "--user", "enable", "--now", timerUnit); err != nil {
		return nil, fmt.Errorf("systemctl --user enable --now %s failed: %s", timerUnit, output)
	}
	return &BackendRef{Name: "systemd", Unit: timerUnit, Paths: []string{servicePath, timerPath}}, nil
}

func uninstallSystemd(j *Job) error {
	servicePath, timerPath, err := systemdUnitPaths(j)
	if err != nil {
		return err
	}
	timerUnit := systemdUnitBase(j) + ".timer"
	_, _ = runCommand("systemctl", "--user", "disable", "--now", timerUnit)
	var errs []error
	for _, path := range []string{servicePath, timerPath} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
		}
	}
	_, _ = runCommand("systemctl", "--user", "daemon-reload")
	return errors.Join(errs...)
}

func statusSystemd(j *Job) string {
	timerUnit := systemdUnitBase(j) + ".timer"
	out, err := runCommand("systemctl", "--user", "is-active", timerUnit)
	if err != nil {
		if strings.TrimSpace(out) == "" {
			return "not installed"
		}
		return strings.TrimSpace(out)
	}
	return strings.TrimSpace(out)
}

func renderSystemdService(j *Job, bin string, store *Store) (string, error) {
	args := []string{bin, "__supervise", store.JobPath(j.ScopeID, j.Slug)}
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, systemdQuoteArg(arg))
	}
	var b strings.Builder
	b.WriteString("[Unit]\n")
	b.WriteString("Description=" + systemdEscape(j.Name) + "\n")
	b.WriteString("After=network-online.target\n")
	b.WriteString("Wants=network-online.target\n\n")
	b.WriteString("[Service]\n")
	b.WriteString("Type=oneshot\n")
	b.WriteString("WorkingDirectory=" + systemdEscape(j.Workdir) + "\n")
	b.WriteString("Environment=\"PATH=" + systemdQuoteValue(opencode.EnhancedPath(os.Getenv("PATH"))) + "\"\n")
	b.WriteString("ExecStart=" + strings.Join(quoted, " ") + "\n")
	b.WriteString("StandardOutput=null\n")
	b.WriteString("StandardError=append:" + systemdEscape(store.LogPath(j)) + "\n")
	return b.String(), nil
}

func renderSystemdTimer(j *Job) (string, error) {
	spec, err := ParseCron(j.Cron)
	if err != nil {
		return "", err
	}
	calendars := spec.SystemdCalendars()
	if len(calendars) == 0 {
		return "", errors.New("cron schedule produced no systemd calendar entries")
	}
	var b strings.Builder
	b.WriteString("[Unit]\n")
	b.WriteString("Description=Timer for OpenCode job: " + systemdEscape(j.Name) + "\n\n")
	b.WriteString("[Timer]\n")
	for _, calendar := range calendars {
		b.WriteString("OnCalendar=" + calendar + "\n")
	}
	b.WriteString("Persistent=true\n\n")
	b.WriteString("[Install]\n")
	b.WriteString("WantedBy=timers.target\n")
	return b.String(), nil
}

// === cron ===

func cronKey(j *Job) string {
	return j.ScopeID + "." + j.Slug
}

func cronBlock(j *Job, bin string, store *Store) string {
	entry := fmt.Sprintf("%s /usr/bin/env PATH=%s %s __supervise %s >/dev/null 2>&1",
		j.Cron,
		shellQuote(opencode.EnhancedPath(os.Getenv("PATH"))),
		shellQuote(bin),
		shellQuote(store.JobPath(j.ScopeID, j.Slug)),
	)
	return fmt.Sprintf("# BEGIN opencode-jobs:%s\n%s\n# END opencode-jobs:%s",
		cronKey(j), entry, cronKey(j))
}

func installCron(j *Job, bin string, store *Store) (*BackendRef, error) {
	if _, err := exec.LookPath("crontab"); err != nil {
		return nil, errors.New("crontab not found; install cron or use systemd")
	}
	current, err := readCrontab()
	if err != nil {
		return nil, err
	}
	cleaned := stripCronBlock(current, cronKey(j))
	next := strings.TrimRight(cleaned, "\n")
	if next != "" {
		next += "\n\n"
	}
	next += cronBlock(j, bin, store) + "\n"
	if err := writeCrontab(next); err != nil {
		return nil, err
	}
	return &BackendRef{Name: "cron", Unit: "crontab"}, nil
}

func uninstallCron(j *Job) error {
	current, err := readCrontab()
	if err != nil {
		return err
	}
	next := stripCronBlock(current, cronKey(j))
	if next == current {
		return nil
	}
	return writeCrontab(next)
}

func statusCron(j *Job) string {
	current, err := readCrontab()
	if err != nil {
		return "unknown"
	}
	if strings.Contains(current, "# BEGIN opencode-jobs:"+cronKey(j)) {
		return "installed"
	}
	return "missing"
}

func readCrontab() (string, error) {
	cmd := exec.Command("crontab", "-l")
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// "no crontab for user" exits 1 with no stdout.
			if len(out) == 0 {
				return "", nil
			}
		}
		return "", fmt.Errorf("cannot read crontab: %w", err)
	}
	return string(out), nil
}

func writeCrontab(content string) error {
	tmp, err := os.CreateTemp("", "opencode-jobs-crontab-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if output, err := runCommand("crontab", tmpName); err != nil {
		return fmt.Errorf("crontab update failed: %s", output)
	}
	return nil
}

func stripCronBlock(content, key string) string {
	lines := strings.Split(content, "\n")
	begin := "# BEGIN opencode-jobs:" + key
	end := "# END opencode-jobs:" + key
	var out []string
	skipping := false
	for _, line := range lines {
		switch strings.TrimSpace(line) {
		case begin:
			skipping = true
			continue
		case end:
			skipping = false
			continue
		}
		if !skipping {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// === helpers ===

func systemdUserAvailable() bool {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "systemctl", "--user", "show-environment").Run() == nil
}

func runCommand(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func writePlistKey(b *strings.Builder, key string) {
	writePlistKeyIndent(b, key, "\t")
}

func writePlistKeyIndent(b *strings.Builder, key, indent string) {
	b.WriteString(indent + "<key>" + key + "</key>\n")
}

func writePlistString(b *strings.Builder, value string) {
	writePlistStringIndent(b, value, "\t")
}

func writePlistStringIndent(b *strings.Builder, value, indent string) {
	b.WriteString(indent + "<string>" + xmlEscape(value) + "</string>\n")
}

func xmlEscape(value string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
	)
	return replacer.Replace(value)
}

func systemdEscape(value string) string {
	return strings.ReplaceAll(value, "%", "%%")
}

func systemdQuoteValue(value string) string {
	escaped := strings.ReplaceAll(value, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return systemdEscape(escaped)
}

func systemdQuoteArg(value string) string {
	return `"` + systemdQuoteValue(value) + `"`
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
