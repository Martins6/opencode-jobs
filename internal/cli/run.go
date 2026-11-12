package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Martins6/opencode-jobs/internal/jobs"
)

func (a *App) run(args []string) error {
	fs := newFlagSet("run")
	wait := fs.Bool("wait", false, "wait for the run to finish")
	all := fs.Bool("all", false, "search every workspace")
	jsonOut := fs.Bool("json", false, "output JSON")
	help, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if help {
		printRunUsage(a.Out)
		return nil
	}
	if fs.NArg() != 1 {
		return errors.New("usage: opencode-jobs run <job-id> [--wait] [--all]")
	}
	job, err := a.findJob(fs.Arg(0), *all)
	if err != nil {
		return err
	}
	jobPath := a.Store.JobPath(job.ScopeID, job.Slug)
	logPath := a.Store.LogPath(job)
	runID := jobs.NewRunID()
	jsonMode := a.JSON || *jsonOut

	if a.Store.IsRunning(job) {
		return fmt.Errorf("job %s is already running; wait for it to finish or check %s", job.Slug, logPath)
	}

	if *wait {
		code := jobs.Supervise(jobPath, jobs.SuperviseOptions{
			Source: "manual",
			RunID:  runID,
			Echo:   !jsonMode,
		}, a.Out)
		if jsonMode {
			return a.writeJSON(map[string]any{
				"success":  code == 0,
				"runId":    runID,
				"exitCode": code,
				"logPath":  logPath,
			})
		}
		if code != 0 {
			return &ExitError{Code: code, Err: fmt.Errorf("job %s failed with exit code %d (see %s)", job.Slug, code, logPath)}
		}
		return nil
	}

	if err := jobs.StartDetached(jobPath, runID, "manual"); err != nil {
		return err
	}
	if jsonMode {
		return a.writeJSON(map[string]any{
			"success":  true,
			"runId":    runID,
			"detached": true,
			"logPath":  logPath,
		})
	}
	fmt.Fprintf(a.Out, "Started %s (run %s)\n", job.Slug, runID)
	fmt.Fprintf(a.Out, "  Log: %s\n", logPath)
	fmt.Fprintf(a.Out, "  Follow: opencode-jobs logs %s --follow\n", job.Slug)
	return nil
}

func (a *App) logs(args []string) error {
	fs := newFlagSet("logs")
	all := fs.Bool("all", false, "search every workspace")
	lines := fs.Int("n", jobs.DefaultLogLines, "number of lines (0 for all)")
	follow := fs.Bool("follow", false, "follow the log")
	shortFollow := fs.Bool("f", false, "follow the log")
	pathOnly := fs.Bool("path", false, "print the log path only")
	jsonOut := fs.Bool("json", false, "output JSON")
	help, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if help {
		printLogsUsage(a.Out)
		return nil
	}
	if fs.NArg() != 1 {
		return errors.New("usage: opencode-jobs logs <job-id> [--follow] [-n N] [--all]")
	}
	job, err := a.findJob(fs.Arg(0), *all)
	if err != nil {
		return err
	}
	logPath := a.Store.LogPath(job)
	jsonMode := a.JSON || *jsonOut
	if jsonMode && (*follow || *shortFollow) {
		return errors.New("--json cannot be combined with --follow")
	}
	if *pathOnly {
		if jsonMode {
			return a.writeJSON(map[string]any{"path": logPath})
		}
		fmt.Fprintln(a.Out, logPath)
		return nil
	}
	if _, err := os.Stat(logPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no log yet for %s (run: opencode-jobs run %s --wait)", job.Slug, job.Slug)
		}
		return err
	}
	if jsonMode {
		tail, err := readLastLines(logPath, *lines)
		if err != nil {
			return err
		}
		return a.writeJSON(map[string]any{"path": logPath, "lines": tail})
	}
	if *follow || *shortFollow {
		return followLog(logPath, *lines, a.Out)
	}
	tail, err := readLastLines(logPath, *lines)
	if err != nil {
		return err
	}
	for _, line := range tail {
		fmt.Fprintln(a.Out, line)
	}
	return nil
}

func (a *App) reinstall(args []string) error {
	fs := newFlagSet("reinstall")
	all := fs.Bool("all", false, "reinstall jobs in every workspace")
	jsonOut := fs.Bool("json", false, "output JSON")
	help, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if help {
		printReinstallUsage(a.Out)
		return nil
	}

	var targets []*jobs.Job
	if fs.NArg() > 0 {
		for _, slug := range fs.Args() {
			job, err := a.findJob(slug, *all)
			if err != nil {
				return err
			}
			targets = append(targets, job)
		}
	} else {
		targets, err = a.Store.List(*all)
		if err != nil {
			return err
		}
	}
	if len(targets) == 0 {
		if a.JSON || *jsonOut {
			return a.writeJSON(map[string]any{"success": true, "jobs": []any{}})
		}
		fmt.Fprintln(a.Out, "No jobs to reinstall.")
		return nil
	}

	bin, err := jobs.SelfPath()
	if err != nil {
		return err
	}
	type result struct {
		Slug      string           `json:"slug"`
		Success   bool             `json:"success"`
		Scheduler *jobs.BackendRef `json:"scheduler,omitempty"`
		Error     string           `json:"error,omitempty"`
	}
	var results []result
	var failures []string
	for _, job := range targets {
		item := result{Slug: job.Slug}
		if err := jobs.Uninstall(job); err != nil {
			item.Error = err.Error()
			failures = append(failures, job.Slug+": "+err.Error())
			results = append(results, item)
			continue
		}
		ref, err := jobs.Install(job, bin, a.Store)
		if err != nil {
			item.Error = err.Error()
			failures = append(failures, job.Slug+": "+err.Error())
			results = append(results, item)
			continue
		}
		job.Backend = ref.Name
		job.Unit = ref.Unit
		job.InstalledBinary = bin
		if err := a.Store.Save(job); err != nil {
			item.Error = err.Error()
			failures = append(failures, job.Slug+": "+err.Error())
			results = append(results, item)
			continue
		}
		item.Success = true
		item.Scheduler = ref
		results = append(results, item)
		if !(a.JSON || *jsonOut) {
			fmt.Fprintf(a.Out, "Reinstalled %s (%s)\n", job.Slug, ref.Unit)
		}
	}

	if a.JSON || *jsonOut {
		if len(failures) > 0 {
			return a.writeJSON(map[string]any{"success": false, "jobs": results, "errors": failures})
		}
		return a.writeJSON(map[string]any{"success": true, "jobs": results})
	}
	if len(failures) > 0 {
		return fmt.Errorf("%d job(s) failed to reinstall: %s", len(failures), strings.Join(failures, "; "))
	}
	return nil
}

func (a *App) supervise(args []string) int {
	fs := newFlagSet("__supervise")
	runID := fs.String("run-id", "", "run id")
	source := fs.String("source", "scheduled", "run source")
	echo := fs.Bool("echo", false, "echo output to stdout")
	if _, err := parseFlags(fs, args); err != nil {
		fmt.Fprintf(a.Err, "error: %v\n", err)
		return 1
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(a.Err, "error: __supervise requires a job path")
		return 1
	}
	return jobs.Supervise(fs.Arg(0), jobs.SuperviseOptions{
		Source: *source,
		RunID:  *runID,
		Echo:   *echo,
	}, a.Out)
}

func readLastLines(path string, limit int) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	size := info.Size()
	if size == 0 {
		return nil, nil
	}
	if limit <= 0 {
		data, err := io.ReadAll(file)
		if err != nil {
			return nil, err
		}
		return splitLines(data), nil
	}

	const chunkSize int64 = 32 * 1024
	var chunks [][]byte
	newlines := 0
	offset := size
	for offset > 0 && newlines <= limit {
		length := chunkSize
		if offset < length {
			length = offset
		}
		offset -= length
		buf := make([]byte, length)
		if _, err := file.ReadAt(buf, offset); err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		chunks = append(chunks, buf)
		newlines += bytes.Count(buf, []byte{'\n'})
	}
	var data []byte
	for index := len(chunks) - 1; index >= 0; index-- {
		data = append(data, chunks[index]...)
	}
	lines := splitLines(data)
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return lines, nil
}

func splitLines(data []byte) []string {
	text := strings.TrimSuffix(string(data), "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

func followLog(path string, initial int, out io.Writer) error {
	tail, err := readLastLines(path, initial)
	if err != nil {
		return err
	}
	for _, line := range tail {
		fmt.Fprintln(out, line)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	offset := info.Size()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	buffer := make([]byte, 64*1024)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if info.Size() < offset {
			offset = 0
		}
		if info.Size() == offset {
			continue
		}
		for {
			n, err := file.ReadAt(buffer, offset)
			if n > 0 {
				_, _ = out.Write(buffer[:n])
				offset += int64(n)
			}
			if errors.Is(err, io.EOF) || n == 0 {
				break
			}
			if err != nil {
				return err
			}
		}
	}
}

func printRunUsage(out io.Writer) {
	fmt.Fprint(out, `Usage: opencode-jobs run <job-id> [--wait] [--all] [--json]

Triggers a job immediately. By default the run is detached and appends to the
job log. Use --wait to run in the foreground and receive the job's exit code.
`)
}

func printLogsUsage(out io.Writer) {
	fmt.Fprint(out, `Usage: opencode-jobs logs <job-id> [-n N] [--follow] [--path] [--all]

Prints the last N log lines (default 100). -n 0 prints the whole log.
Use --follow or -f to stream new output.
`)
}

func printReinstallUsage(out io.Writer) {
	fmt.Fprint(out, `Usage: opencode-jobs reinstall [job-id...] [--all] [--json]

Reinstalls scheduler entries using the current opencode-jobs binary. Run this
after moving or upgrading the binary.
`)
}
