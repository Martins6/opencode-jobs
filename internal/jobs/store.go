package jobs

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Store owns the on-disk layout under ~/.config/opencode-jobs.
//
//	<home>/scopes/<scope>/jobs/<slug>.json
//	<home>/scopes/<scope>/logs/<slug>.log
//	<home>/scopes/<scope>/runs/<slug>.jsonl
//	<home>/scopes/<scope>/locks/<slug>.json
type Store struct {
	Home string
}

// NewStore resolves the store home from OPENCODE_JOBS_HOME, XDG_CONFIG_HOME,
// or ~/.config/opencode-jobs.
func NewStore() (*Store, error) {
	home := strings.TrimSpace(os.Getenv("OPENCODE_JOBS_HOME"))
	if home == "" {
		if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
			home = filepath.Join(xdg, "opencode-jobs")
		} else {
			userHome, err := os.UserHomeDir()
			if err != nil {
				return nil, fmt.Errorf("cannot determine home directory: %w", err)
			}
			home = filepath.Join(userHome, ".config", "opencode-jobs")
		}
	}
	abs, err := filepath.Abs(home)
	if err != nil {
		return nil, err
	}
	return &Store{Home: filepath.Clean(abs)}, nil
}

// StoreFromJobPath derives the store home from a job file path.
func StoreFromJobPath(jobPath string) (*Store, error) {
	abs, err := filepath.Abs(jobPath)
	if err != nil {
		return nil, err
	}
	sep := string(os.PathSeparator)
	marker := sep + "scopes" + sep
	if strings.HasPrefix(abs, sep+"scopes"+sep) {
		return &Store{Home: sep}, nil
	}
	idx := strings.LastIndex(abs, marker)
	if idx < 0 {
		return nil, fmt.Errorf("cannot derive opencode-jobs home from job path: %s", abs)
	}
	if idx == 0 {
		return &Store{Home: sep}, nil
	}
	return &Store{Home: abs[:idx]}, nil
}

// ScopeDir returns <home>/scopes/<scope>.
func (s *Store) ScopeDir(scope string) string {
	return filepath.Join(s.Home, "scopes", scope)
}

// JobsDir returns the job directory for a scope.
func (s *Store) JobsDir(scope string) string {
	return filepath.Join(s.ScopeDir(scope), "jobs")
}

// LogsDir returns the log directory for a scope.
func (s *Store) LogsDir(scope string) string {
	return filepath.Join(s.ScopeDir(scope), "logs")
}

// RunsDir returns the run-record directory for a scope.
func (s *Store) RunsDir(scope string) string {
	return filepath.Join(s.ScopeDir(scope), "runs")
}

// LocksDir returns the lock directory for a scope.
func (s *Store) LocksDir(scope string) string {
	return filepath.Join(s.ScopeDir(scope), "locks")
}

// JobPath returns the JSON file for a job.
func (s *Store) JobPath(scope, slug string) string {
	return filepath.Join(s.JobsDir(scope), slug+".json")
}

// LogPath returns the log file for a job.
func (s *Store) LogPath(j *Job) string {
	return filepath.Join(s.LogsDir(j.ScopeID), j.Slug+".log")
}

// RunsPath returns the run-record file for a job.
func (s *Store) RunsPath(j *Job) string {
	return filepath.Join(s.RunsDir(j.ScopeID), j.Slug+".jsonl")
}

// LockPath returns the lock file for a job.
func (s *Store) LockPath(j *Job) string {
	return filepath.Join(s.LocksDir(j.ScopeID), j.Slug+".json")
}

// CurrentScopeID returns the scope for the current working directory.
func (s *Store) CurrentScopeID() (string, error) {
	workdir, err := ResolveWorkdir("")
	if err != nil {
		return "", err
	}
	return DeriveScopeID(workdir), nil
}

// EnsureJobDirs creates the per-scope directories.
func (s *Store) EnsureJobDirs(j *Job) error {
	dirs := []string{
		s.JobsDir(j.ScopeID),
		s.LogsDir(j.ScopeID),
		s.RunsDir(j.ScopeID),
		s.LocksDir(j.ScopeID),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	return nil
}

// Save writes a job atomically with 0600 permissions.
func (s *Store) Save(j *Job) error {
	j.Normalize()
	if err := j.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(s.JobsDir(j.ScopeID), 0o700); err != nil {
		return err
	}
	now := j.UpdatedAt
	if now.IsZero() {
		now = timeNowUTC()
		j.UpdatedAt = now
	}
	if j.CreatedAt.IsZero() {
		j.CreatedAt = now
	}

	data, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(s.JobsDir(j.ScopeID), ".job-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, s.JobPath(j.ScopeID, j.Slug))
}

// Load reads a job from a scope.
func (s *Store) Load(scope, slug string) (*Job, error) {
	if !ValidSlug(slug) {
		return nil, fmt.Errorf("invalid job id %q", slug)
	}
	return s.LoadByPath(s.JobPath(scope, slug))
}

// LoadByPath reads a job file.
func (s *Store) LoadByPath(path string) (*Job, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, path)
		}
		return nil, err
	}
	var j Job
	if err := json.Unmarshal(data, &j); err != nil {
		return nil, fmt.Errorf("cannot parse job %s: %w", path, err)
	}
	j.Normalize()
	return &j, nil
}

// Delete removes a job file.
func (s *Store) Delete(j *Job) error {
	err := os.Remove(s.JobPath(j.ScopeID, j.Slug))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Find returns a job from the current scope. When all is true, it searches
// every scope and returns an error if more than one job matches.
func (s *Store) Find(slug string, all bool) (*Job, error) {
	if !ValidSlug(slug) {
		return nil, fmt.Errorf("invalid job id %q", slug)
	}
	scope, err := s.CurrentScopeID()
	if err != nil {
		return nil, err
	}
	job, err := s.Load(scope, slug)
	if err == nil {
		return job, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	if !all {
		return nil, fmt.Errorf("%w: %s in scope %s (use --all to search all scopes)", ErrNotFound, slug, scope)
	}

	allJobs, err := s.List(true)
	if err != nil {
		return nil, err
	}
	var matches []*Job
	for _, candidate := range allJobs {
		if candidate.Slug == slug {
			matches = append(matches, candidate)
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("%w: %s", ErrNotFound, slug)
	case 1:
		return matches[0], nil
	default:
		var scopes []string
		for _, match := range matches {
			scopes = append(scopes, match.ScopeID)
		}
		sort.Strings(scopes)
		return nil, fmt.Errorf("job id %q is ambiguous across scopes: %s", slug, strings.Join(scopes, ", "))
	}
}

// List returns jobs in the current scope, or in every scope when all is true.
func (s *Store) List(all bool) ([]*Job, error) {
	var scopes []string
	if all {
		var err error
		scopes, err = s.ScopeIDs()
		if err != nil {
			return nil, err
		}
	} else {
		scope, err := s.CurrentScopeID()
		if err != nil {
			return nil, err
		}
		scopes = []string{scope}
	}

	var out []*Job
	for _, scope := range scopes {
		entries, err := os.ReadDir(s.JobsDir(scope))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			job, err := s.LoadByPath(filepath.Join(s.JobsDir(scope), entry.Name()))
			if err != nil {
				return nil, err
			}
			out = append(out, job)
		}
	}
	SortJobs(out)
	return out, nil
}

// ScopeIDs lists scope directories that contain jobs.
func (s *Store) ScopeIDs() ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(s.Home, "scopes"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var scopes []string
	for _, entry := range entries {
		if entry.IsDir() {
			scopes = append(scopes, entry.Name())
		}
	}
	sort.Strings(scopes)
	return scopes, nil
}

// ReadLock reads a job lock.
func (s *Store) ReadLock(j *Job) (*LockInfo, error) {
	data, err := os.ReadFile(s.LockPath(j))
	if err != nil {
		return nil, err
	}
	var info LockInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// IsRunning reports whether a live process holds the job lock.
func (s *Store) IsRunning(j *Job) bool {
	info, err := s.ReadLock(j)
	if err != nil {
		return false
	}
	return info.PID > 0 && ProcessAlive(info.PID)
}

// AcquireLock creates the job lock. It removes stale locks whose PID is gone.
func (s *Store) AcquireLock(j *Job, info LockInfo) (func(), error) {
	if err := os.MkdirAll(s.LocksDir(j.ScopeID), 0o700); err != nil {
		return nil, err
	}
	path := s.LockPath(j)
	data, err := json.Marshal(info)
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')

	for attempt := 0; attempt < 3; attempt++ {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, writeErr := file.Write(data)
			closeErr := file.Close()
			if writeErr != nil {
				os.Remove(path)
				return nil, writeErr
			}
			if closeErr != nil {
				os.Remove(path)
				return nil, closeErr
			}
			return func() { _ = os.Remove(path) }, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		existing, readErr := s.ReadLock(j)
		if readErr == nil && existing.PID > 0 && ProcessAlive(existing.PID) {
			return nil, fmt.Errorf("%w (pid %d, run %s)", ErrAlreadyRunning, existing.PID, existing.RunID)
		}
		_ = os.Remove(path)
	}
	return nil, errors.New("could not acquire job lock")
}

// AppendRun appends a run record.
func (s *Store) AppendRun(j *Job, record RunRecord) error {
	if err := os.MkdirAll(s.RunsDir(j.ScopeID), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	file, err := os.OpenFile(s.RunsPath(j), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(data)
	return err
}

// ReadRuns returns the last limit run records in chronological order.
// A limit of zero or less returns every record.
func (s *Store) ReadRuns(j *Job, limit int) ([]RunRecord, error) {
	file, err := os.Open(s.RunsPath(j))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()

	var records []RunRecord
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var record RunRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			continue
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if limit > 0 && len(records) > limit {
		records = records[len(records)-limit:]
	}
	return records, nil
}

// DeleteHistory removes logs, run records, and the lock for a job.
func (s *Store) DeleteHistory(j *Job) error {
	var errs []error
	for _, path := range []string{s.LogPath(j), s.RunsPath(j), s.LockPath(j)} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// RemoveEmptyScope removes a scope directory when it has no jobs left. Only
// empty directories are removed, so kept logs and run history are preserved.
func (s *Store) RemoveEmptyScope(scope string) {
	for _, dir := range []string{
		s.JobsDir(scope),
		s.LogsDir(scope),
		s.RunsDir(scope),
		s.LocksDir(scope),
	} {
		_ = os.Remove(dir)
	}
	_ = os.Remove(s.ScopeDir(scope))
}

func timeNowUTC() time.Time {
	return time.Now().UTC()
}
