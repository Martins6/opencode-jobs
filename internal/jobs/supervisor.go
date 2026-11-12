package jobs

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// SuperviseOptions controls a supervised run.
type SuperviseOptions struct {
	Source string
	RunID  string
	Echo   bool
}

// Supervise runs a job once, enforcing the lock and optional timeout. It
// returns the same exit code as the child (124 on timeout).
func Supervise(jobPath string, opts SuperviseOptions, stdout io.Writer) int {
	if stdout == nil {
		stdout = os.Stdout
	}
	if opts.Source == "" {
		opts.Source = "scheduled"
	}
	if opts.RunID == "" {
		opts.RunID = NewRunID()
	}

	store, err := StoreFromJobPath(jobPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "opencode-jobs: %v\n", err)
		return 1
	}
	job, err := store.LoadByPath(jobPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "opencode-jobs: %v\n", err)
		return 1
	}
	if err := store.EnsureJobDirs(job); err != nil {
		fmt.Fprintf(os.Stderr, "opencode-jobs: %v\n", err)
		return 1
	}

	logFile, err := os.OpenFile(store.LogPath(job), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "opencode-jobs: cannot open log: %v\n", err)
		return 1
	}
	defer logFile.Close()

	var logWriter io.Writer = logFile
	if opts.Echo {
		logWriter = io.MultiWriter(logFile, stdout)
	}
	logf := func(format string, args ...any) {
		fmt.Fprintf(logWriter, format+"\n", args...)
	}

	startedAt := time.Now()
	logf("")
	logf("=== opencode-jobs run %s source=%s runId=%s job=%s/%s ===",
		startedAt.Format(time.RFC3339), opts.Source, opts.RunID, job.ScopeID, job.Slug)

	if err := job.CheckWorkdir(); err != nil {
		logf("=== failed: %v ===", err)
		finishRun(store, job, opts, startedAt, 1, "failed", err.Error(), 0, logf)
		return 1
	}

	release, err := store.AcquireLock(job, LockInfo{
		PID:       os.Getpid(),
		RunID:     opts.RunID,
		Source:    opts.Source,
		StartedAt: startedAt.UTC(),
	})
	if err != nil {
		if errors.Is(err, ErrAlreadyRunning) {
			logf("=== skipped: %v ===", err)
			return 0
		}
		logf("=== failed to acquire lock: %v ===", err)
		return 1
	}
	defer release()

	job.SetLastRun(startedAt.UTC(), opts.Source, "running", 0, "")
	if err := store.Save(job); err != nil {
		logf("warning: cannot update job metadata: %v", err)
	}

	invocation, err := BuildInvocation(job)
	if err != nil {
		logf("=== failed: %v ===", err)
		finishRun(store, job, opts, startedAt, 1, "failed", err.Error(), 0, logf)
		return 1
	}

	cmd := exec.Command(invocation.Command, invocation.Args...)
	cmd.Dir = job.Workdir
	cmd.Env = BuildEnv(job, opts.RunID, store.Home)
	cmd.Stdout = logWriter
	cmd.Stderr = logWriter
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	logf("$ %s %s", invocation.Command, strings.Join(redactArgs(invocation.Args), " "))
	if err := cmd.Start(); err != nil {
		logf("=== failed to start: %v ===", err)
		finishRun(store, job, opts, startedAt, 1, "failed", err.Error(), 0, logf)
		return 1
	}
	pid := cmd.Process.Pid

	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()

	var waitErr error
	timedOut := false
	if job.TimeoutSeconds > 0 {
		timer := time.NewTimer(time.Duration(job.TimeoutSeconds) * time.Second)
		select {
		case waitErr = <-waitCh:
			timer.Stop()
		case <-timer.C:
			timedOut = true
			logf("=== timeout after %ds; sending SIGTERM to process group %d ===", job.TimeoutSeconds, pid)
			signalProcessGroup(pid, syscall.SIGTERM)
			select {
			case waitErr = <-waitCh:
			case <-time.After(5 * time.Second):
				logf("=== process group did not exit; sending SIGKILL ===")
				signalProcessGroup(pid, syscall.SIGKILL)
				waitErr = <-waitCh
			}
		}
	} else {
		waitErr = <-waitCh
	}

	exitCode := 0
	status := "success"
	runErr := ""
	switch {
	case timedOut:
		exitCode = 124
		status = "failed"
		runErr = "timeout"
	case waitErr != nil:
		status = "failed"
		exitCode = 1
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			if code := exitErr.ExitCode(); code >= 0 {
				exitCode = code
			}
		}
		runErr = fmt.Sprintf("exit code %d", exitCode)
	}

	finishRun(store, job, opts, startedAt, exitCode, status, runErr, pid, logf)
	return exitCode
}

func finishRun(store *Store, job *Job, opts SuperviseOptions, startedAt time.Time, exitCode int, status, runErr string, pid int, logf func(string, ...any)) {
	finishedAt := time.Now()
	job.SetLastRun(startedAt.UTC(), opts.Source, status, exitCode, runErr)
	job.UpdatedAt = finishedAt.UTC()
	if err := store.Save(job); err != nil {
		logf("warning: cannot update job metadata: %v", err)
	}
	record := RunRecord{
		RunID:      opts.RunID,
		ScopeID:    job.ScopeID,
		Slug:       job.Slug,
		Source:     opts.Source,
		StartedAt:  startedAt.UTC(),
		FinishedAt: finishedAt.UTC(),
		DurationMS: finishedAt.Sub(startedAt).Milliseconds(),
		Status:     status,
		ExitCode:   exitCode,
		Error:      runErr,
		PID:        pid,
		LogPath:    store.LogPath(job),
	}
	if err := store.AppendRun(job, record); err != nil {
		logf("warning: cannot append run record: %v", err)
	}
	logf("=== finished %s status=%s exitCode=%d duration=%s ===",
		finishedAt.Format(time.RFC3339), status, exitCode, finishedAt.Sub(startedAt).Round(time.Millisecond))
}

// StartDetached launches `__supervise` in a new session and returns immediately.
func StartDetached(jobPath, runID, source string) error {
	self, err := SelfPath()
	if err != nil {
		return err
	}
	cmd := exec.Command(self, "__supervise", jobPath, "--run-id", runID, "--source", source)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer devNull.Close()
	cmd.Stdin = devNull
	cmd.Stdout = devNull
	cmd.Stderr = devNull
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// redactArgs hides the prompt body from logs while keeping the flags that
// explain how the run was configured.
func redactArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for index, arg := range args {
		if arg == "--" {
			out = append(out, arg)
			if index+1 < len(args) {
				out = append(out, fmt.Sprintf("<prompt: %d bytes>", len(args[index+1])))
			}
			break
		}
		out = append(out, arg)
	}
	return out
}
