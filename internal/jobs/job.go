package jobs

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	// JobFileVersion is the current on-disk job schema version.
	JobFileVersion = 1
	// DefaultLogLines is the default number of log lines shown by `logs`.
	DefaultLogLines = 100
)

var (
	// ErrNotFound is returned when a job does not exist.
	ErrNotFound = errors.New("job not found")
	// ErrAlreadyRunning is returned when another run holds the job lock.
	ErrAlreadyRunning = errors.New("job is already running")

	slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	envPattern  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// Job is a scheduled OpenCode prompt.
type Job struct {
	Version  int      `json:"version"`
	ScopeID  string   `json:"scopeId"`
	Slug     string   `json:"slug"`
	Name     string   `json:"name"`
	Cron     string   `json:"cron"`
	Workdir  string   `json:"workdir"`
	Prompt   string   `json:"prompt"`
	Agent    string   `json:"agent,omitempty"`
	Model    string   `json:"model,omitempty"`
	Title    string   `json:"title,omitempty"`
	Files    []string `json:"files,omitempty"`
	Session  string   `json:"session,omitempty"`
	Continue bool     `json:"continue,omitempty"`

	Server     string `json:"server,omitempty"`
	Standalone bool   `json:"standalone,omitempty"`
	Archived   bool   `json:"archived,omitempty"`

	// DisableAuto omits `opencode run --auto`. Auto-approval is enabled by
	// default so scheduled runs do not reject permission requests.
	DisableAuto bool `json:"disableAuto,omitempty"`

	TimeoutSeconds int               `json:"timeoutSeconds,omitempty"`
	Env            map[string]string `json:"env,omitempty"`

	Backend         string `json:"backend,omitempty"`
	Unit            string `json:"unit,omitempty"`
	InstalledBinary string `json:"installedBinary,omitempty"`

	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt,omitempty"`
	LastRunAt *time.Time `json:"lastRunAt,omitempty"`

	LastRunSource   string `json:"lastRunSource,omitempty"`
	LastRunStatus   string `json:"lastRunStatus,omitempty"`
	LastRunExitCode *int   `json:"lastRunExitCode,omitempty"`
	LastRunError    string `json:"lastRunError,omitempty"`
}

// RunRecord is one entry in a job's runs/<slug>.jsonl file.
type RunRecord struct {
	RunID      string    `json:"runId"`
	ScopeID    string    `json:"scopeId"`
	Slug       string    `json:"slug"`
	Source     string    `json:"source"`
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt"`
	DurationMS int64     `json:"durationMs"`
	Status     string    `json:"status"`
	ExitCode   int       `json:"exitCode"`
	Error      string    `json:"error,omitempty"`
	PID        int       `json:"pid,omitempty"`
	LogPath    string    `json:"logPath"`
}

// LockInfo is written to locks/<slug>.json while a run is active.
type LockInfo struct {
	PID       int       `json:"pid"`
	RunID     string    `json:"runId"`
	Source    string    `json:"source"`
	StartedAt time.Time `json:"startedAt"`
}

// Slugify converts a display name into a stable job id.
func Slugify(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	lastDash := false
	for _, r := range lower {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case !lastDash:
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 48 {
		out = strings.TrimRight(out[:48], "-")
	}
	if out == "" {
		out = "job"
	}
	return out
}

// ValidSlug reports whether id is a valid job id.
func ValidSlug(id string) bool {
	return len(id) > 0 && len(id) <= 64 && slugPattern.MatchString(id)
}

// ResolveWorkdir normalizes a workdir path, expanding a leading ~ and resolving
// symlinks when the directory exists. Symlink resolution keeps scopes stable on
// macOS, where /var and /private/var refer to the same place.
func ResolveWorkdir(input string) (string, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		value = cwd
	}
	if value == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		value = home
	} else if strings.HasPrefix(value, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		value = filepath.Join(home, value[2:])
	}
	abs, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	return abs, nil
}

// DeriveScopeID returns the stable scope id for a workdir.
func DeriveScopeID(workdir string) string {
	normalized := filepath.Clean(workdir)
	base := Slugify(filepath.Base(normalized))
	if base == "job" {
		base = "workspace"
	}
	sum := sha256.Sum256([]byte(normalized))
	return base + "-" + hex.EncodeToString(sum[:])[:12]
}

// Normalize fills derived fields and trims user input.
func (j *Job) Normalize() {
	if j.Version == 0 {
		j.Version = JobFileVersion
	}
	j.Name = strings.TrimSpace(j.Name)
	j.Cron = strings.TrimSpace(j.Cron)
	j.Prompt = strings.TrimSpace(j.Prompt)
	j.Agent = strings.TrimSpace(j.Agent)
	j.Model = strings.TrimSpace(j.Model)
	j.Title = strings.TrimSpace(j.Title)
	j.Session = strings.TrimSpace(j.Session)
	j.Server = strings.TrimSpace(j.Server)

	if j.Workdir != "" {
		if resolved, err := ResolveWorkdir(j.Workdir); err == nil {
			j.Workdir = resolved
		}
	}
	if j.ScopeID == "" && j.Workdir != "" {
		j.ScopeID = DeriveScopeID(j.Workdir)
	}
	if j.Slug == "" {
		j.Slug = Slugify(j.Name)
	}
	if j.Title == "" {
		j.Title = j.Name
	}
	if len(j.Files) == 0 {
		j.Files = nil
	}
	if len(j.Env) == 0 {
		j.Env = nil
	}
}

// Validate checks syntax. It does not require the workdir to exist.
func (j *Job) Validate() error {
	if !ValidSlug(j.Slug) {
		return fmt.Errorf("invalid job id %q: use lowercase letters, numbers, and hyphens", j.Slug)
	}
	if j.Name == "" {
		return errors.New("job name is required")
	}
	if len(j.Name) > 200 {
		return errors.New("job name must be 200 characters or fewer")
	}
	if strings.ContainsAny(j.Name, "\n\r") {
		return errors.New("job name cannot contain newlines")
	}
	if j.Prompt == "" {
		return errors.New("prompt is required")
	}
	if j.Cron == "" {
		return errors.New("cron schedule is required")
	}
	if _, err := ParseCron(j.Cron); err != nil {
		return err
	}
	if j.Workdir == "" || !filepath.IsAbs(j.Workdir) {
		return fmt.Errorf("workdir must be an absolute path: %q", j.Workdir)
	}
	if strings.ContainsAny(j.Workdir, "\n\r") {
		return errors.New("workdir cannot contain newlines")
	}
	if j.TimeoutSeconds < 0 {
		return errors.New("timeout must be zero or a positive number of seconds")
	}
	if j.Continue && j.Session != "" {
		return errors.New("--continue and --session cannot be used together")
	}
	if j.Standalone && j.Server != "" {
		return errors.New("--standalone and --server cannot be used together")
	}
	if j.Server != "" && !strings.HasPrefix(j.Server, "http://") && !strings.HasPrefix(j.Server, "https://") {
		return fmt.Errorf("--server must be an http:// or https:// URL: %q", j.Server)
	}
	for i, file := range j.Files {
		if strings.TrimSpace(file) == "" {
			return fmt.Errorf("file %d is empty", i+1)
		}
	}
	for key := range j.Env {
		if !envPattern.MatchString(key) {
			return fmt.Errorf("invalid environment variable name %q", key)
		}
	}
	return nil
}

// CheckWorkdir verifies that the job's working directory exists.
func (j *Job) CheckWorkdir() error {
	info, err := os.Stat(j.Workdir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("workdir does not exist: %s", j.Workdir)
		}
		return fmt.Errorf("cannot access workdir %s: %w", j.Workdir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("workdir is not a directory: %s", j.Workdir)
	}
	return nil
}

// SetLastRun updates the last-run metadata.
func (j *Job) SetLastRun(at time.Time, source, status string, exitCode int, runErr string) {
	j.LastRunAt = &at
	j.LastRunSource = source
	j.LastRunStatus = status
	j.LastRunExitCode = &exitCode
	j.LastRunError = runErr
	j.UpdatedAt = at
}

// NewRunID returns a sortable, unique run id.
func NewRunID() string {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%s-%d", time.Now().UTC().Format("20060102T150405"), time.Now().UnixNano()%100000)
	}
	return time.Now().UTC().Format("20060102T150405") + "-" + hex.EncodeToString(buf)
}

// SortJobs sorts jobs by scope then slug.
func SortJobs(list []*Job) {
	sort.Slice(list, func(i, j int) bool {
		if list[i].ScopeID == list[j].ScopeID {
			return list[i].Slug < list[j].Slug
		}
		return list[i].ScopeID < list[j].ScopeID
	})
}
