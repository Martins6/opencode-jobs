package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Martins6/opencode-jobs/internal/jobs"
	"github.com/Martins6/opencode-jobs/internal/opencode"
	"github.com/Martins6/opencode-jobs/internal/version"
)

type doctorJob struct {
	Slug            string   `json:"slug"`
	ScopeID         string   `json:"scopeId"`
	Backend         string   `json:"backend,omitempty"`
	Unit            string   `json:"unit,omitempty"`
	Status          string   `json:"status"`
	Running         bool     `json:"running"`
	Workdir         string   `json:"workdir"`
	WorkdirExists   bool     `json:"workdirExists"`
	InstalledBinary string   `json:"installedBinary,omitempty"`
	BinaryMatches   bool     `json:"binaryMatches"`
	LastRunStatus   string   `json:"lastRunStatus,omitempty"`
	LastRunAt       string   `json:"lastRunAt,omitempty"`
	Issues          []string `json:"issues,omitempty"`
}

type doctorReport struct {
	Version         string      `json:"version"`
	Home            string      `json:"home"`
	Workspace       string      `json:"workspace"`
	Scope           string      `json:"scope"`
	Backend         string      `json:"backend,omitempty"`
	BackendError    string      `json:"backendError,omitempty"`
	OpencodePath    string      `json:"opencodePath"`
	OpencodeVersion string      `json:"opencodeVersion,omitempty"`
	OpencodeError   string      `json:"opencodeError,omitempty"`
	BinaryPath      string      `json:"binaryPath,omitempty"`
	Jobs            []doctorJob `json:"jobs"`
	Issues          []string    `json:"issues"`
}

func (a *App) doctor(args []string) error {
	fs := newFlagSet("doctor")
	jsonOut := fs.Bool("json", false, "output JSON")
	help, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if help {
		printDoctorUsage(a.Out)
		return nil
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	workdir, err := jobs.ResolveWorkdir("")
	if err != nil {
		return err
	}
	report := doctorReport{
		Version:   version.String(),
		Home:      a.Store.Home,
		Workspace: workdir,
		Scope:     jobs.DeriveScopeID(workdir),
		Jobs:      []doctorJob{},
		Issues:    []string{},
	}
	report.Issues = append(report.Issues, checkWritable(a.Store.Home)...)

	backend, backendErr := jobs.DetectBackend()
	if backendErr != nil {
		report.BackendError = backendErr.Error()
		report.Issues = append(report.Issues, "scheduler backend: "+backendErr.Error())
	} else {
		report.Backend = backend
	}

	report.OpencodePath = opencode.Find()
	if opencodeVersion, err := opencode.Version(); err != nil {
		report.OpencodeError = err.Error()
		report.Issues = append(report.Issues, "opencode: "+err.Error())
	} else {
		report.OpencodeVersion = opencodeVersion
	}

	bin, _ := jobs.SelfPath()
	report.BinaryPath = bin

	list, err := a.Store.List(true)
	if err != nil {
		return err
	}
	for _, job := range list {
		status, statusErr := jobs.Status(job)
		if statusErr != nil {
			status = "error: " + statusErr.Error()
		}
		workdirInfo, workdirErr := os.Stat(job.Workdir)
		exists := workdirErr == nil && workdirInfo.IsDir()
		binaryMatches := job.InstalledBinary != "" && job.InstalledBinary == bin
		entry := doctorJob{
			Slug:            job.Slug,
			ScopeID:         job.ScopeID,
			Backend:         job.Backend,
			Unit:            job.Unit,
			Status:          status,
			Running:         a.Store.IsRunning(job),
			Workdir:         job.Workdir,
			WorkdirExists:   exists,
			InstalledBinary: job.InstalledBinary,
			BinaryMatches:   binaryMatches,
			LastRunStatus:   job.LastRunStatus,
		}
		if job.LastRunAt != nil {
			entry.LastRunAt = job.LastRunAt.Local().Format(time.RFC3339)
		}

		if job.Backend == "" {
			entry.Issues = append(entry.Issues, "no scheduler entry")
		} else if !backendHealthy(job.Backend, status) {
			entry.Issues = append(entry.Issues, "scheduler entry is "+status)
		}
		if !binaryMatches {
			if job.InstalledBinary == "" {
				entry.Issues = append(entry.Issues, "scheduler entry was not installed by this binary; run reinstall")
			} else {
				entry.Issues = append(entry.Issues, "scheduler entry uses "+job.InstalledBinary+"; run reinstall")
			}
		}
		if !exists {
			entry.Issues = append(entry.Issues, "workdir does not exist")
		}
		for _, issue := range entry.Issues {
			report.Issues = append(report.Issues, job.ScopeID+"/"+job.Slug+": "+issue)
		}
		report.Jobs = append(report.Jobs, entry)
	}

	if a.JSON || *jsonOut {
		return a.writeJSON(report)
	}

	fmt.Fprintf(a.Out, "opencode-jobs %s\n", report.Version)
	fmt.Fprintf(a.Out, "Home:       %s\n", report.Home)
	fmt.Fprintf(a.Out, "Workspace:  %s\n", report.Workspace)
	fmt.Fprintf(a.Out, "Scope:      %s\n", report.Scope)
	if report.Backend != "" {
		fmt.Fprintf(a.Out, "Backend:    %s\n", report.Backend)
	} else {
		fmt.Fprintf(a.Out, "Backend:    unavailable (%s)\n", report.BackendError)
	}
	if report.OpencodeVersion != "" {
		fmt.Fprintf(a.Out, "opencode:   %s (%s)\n", report.OpencodePath, report.OpencodeVersion)
	} else {
		fmt.Fprintf(a.Out, "opencode:   %s (not runnable)\n", report.OpencodePath)
	}
	if report.BinaryPath != "" {
		fmt.Fprintf(a.Out, "CLI binary: %s\n", report.BinaryPath)
	}
	fmt.Fprintln(a.Out)

	if len(report.Jobs) == 0 {
		fmt.Fprintln(a.Out, "Jobs: none")
	} else {
		fmt.Fprintf(a.Out, "Jobs (%d):\n", len(report.Jobs))
		writer := tabwriter.NewWriter(a.Out, 0, 4, 2, ' ', 0)
		fmt.Fprintln(writer, "  ID\tBACKEND\tSTATUS\tLAST RUN\tWORKDIR")
		for _, job := range report.Jobs {
			lastRun := "-"
			if job.LastRunAt != "" {
				if parsed, err := time.Parse(time.RFC3339, job.LastRunAt); err == nil {
					lastRun = parsed.Format("2006-01-02 15:04")
				}
			}
			status := job.Status
			if job.Running {
				status = "running"
			}
			fmt.Fprintf(writer, "  %s\t%s\t%s\t%s\t%s\n",
				job.Slug, valueOr(job.Backend, "none"), status, lastRun, job.Workdir)
		}
		_ = writer.Flush()
	}
	fmt.Fprintln(a.Out)
	if len(report.Issues) == 0 {
		fmt.Fprintln(a.Out, "No issues found.")
	} else {
		fmt.Fprintln(a.Out, "Issues:")
		for _, issue := range report.Issues {
			fmt.Fprintf(a.Out, "  - %s\n", issue)
		}
		fmt.Fprintln(a.Out)
		fmt.Fprintln(a.Out, "Fix scheduler entries with: opencode-jobs reinstall --all")
	}
	return nil
}

func checkWritable(dir string) []string {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return []string{"cannot create config directory " + dir + ": " + err.Error()}
	}
	file, err := os.CreateTemp(dir, ".doctor-*")
	if err != nil {
		return []string{"cannot write to config directory " + dir + ": " + err.Error()}
	}
	name := file.Name()
	_ = file.Close()
	_ = os.Remove(name)
	return nil
}

func backendHealthy(backend, status string) bool {
	switch backend {
	case "launchd":
		return status == "loaded"
	case "systemd":
		return status == "active"
	case "cron":
		return status == "installed"
	default:
		return false
	}
}

func printDoctorUsage(out io.Writer) {
	fmt.Fprint(out, `Usage: opencode-jobs doctor [--json]

Checks the OpenCode binary, scheduler backend, config directory, and every
installed job. Use 'opencode-jobs reinstall --all' to repair stale entries.
`)
}
