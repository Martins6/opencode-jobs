package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Martins6/opencode-jobs/internal/jobs"
)

func (a *App) add(args []string) error {
	fs := newFlagSet("add")
	name := fs.String("name", "", "job name (required)")
	cron := fs.String("cron", "", "five-field cron expression (required)")
	prompt := fs.String("prompt", "", "prompt passed to opencode run (required)")
	id := fs.String("id", "", "job id (defaults to a slug of --name)")
	workdir := fs.String("workdir", "", "working directory (defaults to the current directory)")
	timeout := fs.String("timeout", "0", "run timeout in seconds or as a duration such as 30m (0 disables)")
	agent := fs.String("agent", "", "OpenCode agent to use")
	model := fs.String("model", "", "OpenCode model in provider/model form")
	title := fs.String("title", "", "session title (defaults to the job name)")
	session := fs.String("session", "", "OpenCode session id to use")
	continueSession := fs.Bool("continue", false, "continue the last OpenCode session")
	server := fs.String("server", "", "OpenCode server URL")
	standalone := fs.Bool("standalone", false, "run with a private OpenCode server")
	archived := fs.Bool("archived", false, "create the session archived")
	noAuto := fs.Bool("no-auto", false, "do not pass --auto to opencode run")
	var files stringList
	var envs envList
	jsonOut := fs.Bool("json", false, "output JSON")
	fs.Var(&files, "file", "file to attach (repeatable)")
	fs.Var(&envs, "env", "extra environment variable KEY=VALUE (repeatable)")

	help, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if help {
		printAddUsage(a.Out)
		return nil
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if strings.TrimSpace(*name) == "" {
		return errors.New("--name is required")
	}
	if strings.TrimSpace(*cron) == "" {
		return errors.New("--cron is required")
	}
	if strings.TrimSpace(*prompt) == "" {
		return errors.New("--prompt is required")
	}
	timeoutSeconds, err := parseTimeout(*timeout)
	if err != nil {
		return err
	}
	resolvedWorkdir, err := jobs.ResolveWorkdir(*workdir)
	if err != nil {
		return err
	}
	scope := jobs.DeriveScopeID(resolvedWorkdir)
	slug := strings.TrimSpace(*id)
	if slug != "" {
		if !jobs.ValidSlug(slug) {
			return fmt.Errorf("invalid --id %q: use lowercase letters, numbers, and hyphens", slug)
		}
		if _, err := a.Store.Load(scope, slug); err == nil {
			return fmt.Errorf("job %q already exists in %s", slug, resolvedWorkdir)
		} else if !errors.Is(err, jobs.ErrNotFound) {
			return err
		}
	} else {
		slug, err = uniqueSlug(a.Store, scope, jobs.Slugify(*name))
		if err != nil {
			return err
		}
	}

	job := &jobs.Job{
		Version:        jobs.JobFileVersion,
		ScopeID:        scope,
		Slug:           slug,
		Name:           *name,
		Cron:           *cron,
		Workdir:        resolvedWorkdir,
		Prompt:         *prompt,
		Agent:          *agent,
		Model:          *model,
		Title:          *title,
		Files:          files,
		Session:        *session,
		Continue:       *continueSession,
		Server:         *server,
		Standalone:     *standalone,
		Archived:       *archived,
		DisableAuto:    *noAuto,
		TimeoutSeconds: timeoutSeconds,
		Env:            envs.values,
	}
	job.Normalize()
	if err := job.Validate(); err != nil {
		return err
	}
	if err := job.CheckWorkdir(); err != nil {
		return err
	}

	bin, err := jobs.SelfPath()
	if err != nil {
		return err
	}
	ref, err := jobs.Install(job, bin, a.Store)
	if err != nil {
		return err
	}
	job.Backend = ref.Name
	job.Unit = ref.Unit
	job.InstalledBinary = bin
	if err := a.Store.Save(job); err != nil {
		_ = jobs.Uninstall(job)
		return err
	}

	if a.JSON || *jsonOut {
		return a.writeJSON(map[string]any{
			"success":   true,
			"job":       job,
			"scheduler": ref,
			"logPath":   a.Store.LogPath(job),
		})
	}
	fmt.Fprintf(a.Out, "Created job %s\n", job.Slug)
	fmt.Fprintf(a.Out, "  Schedule: %s (%s)\n", job.Cron, describeCron(job.Cron))
	fmt.Fprintf(a.Out, "  Workdir:  %s\n", job.Workdir)
	fmt.Fprintf(a.Out, "  Backend:  %s (%s)\n", ref.Name, ref.Unit)
	fmt.Fprintf(a.Out, "  Log:      %s\n", a.Store.LogPath(job))
	fmt.Fprintf(a.Out, "  Test:     opencode-jobs run %s --wait\n", job.Slug)
	return nil
}

func (a *App) list(args []string) error {
	fs := newFlagSet("list")
	all := fs.Bool("all", false, "list jobs across every workspace")
	jsonOut := fs.Bool("json", false, "output JSON")
	help, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if help {
		printListUsage(a.Out)
		return nil
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	list, err := a.Store.List(*all)
	if err != nil {
		return err
	}
	if a.JSON || *jsonOut {
		type item struct {
			*jobs.Job
			Status  string `json:"status"`
			LogPath string `json:"logPath"`
		}
		items := make([]item, 0, len(list))
		for _, job := range list {
			items = append(items, item{
				Job:     job,
				Status:  jobStatus(a.Store, job),
				LogPath: a.Store.LogPath(job),
			})
		}
		return a.writeJSON(map[string]any{"jobs": items})
	}
	if len(list) == 0 {
		scope, _ := a.Store.CurrentScopeID()
		fmt.Fprintf(a.Out, "No jobs in scope %s.\n", scope)
		if !*all {
			fmt.Fprintln(a.Out, "Use --all to list jobs from other workspaces.")
		}
		return nil
	}
	writer := tabwriter.NewWriter(a.Out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "ID\tSCHEDULE\tSTATUS\tLAST RUN\tNAME\tWORKDIR")
	for _, job := range list {
		lastRun := "-"
		if job.LastRunAt != nil {
			lastRun = job.LastRunAt.Local().Format("2006-01-02 15:04")
		}
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\n",
			job.Slug, job.Cron, jobStatus(a.Store, job), lastRun, job.Name, job.Workdir)
	}
	return writer.Flush()
}

func (a *App) get(args []string) error {
	fs := newFlagSet("get")
	all := fs.Bool("all", false, "search every workspace")
	runsLimit := fs.Int("runs", 5, "number of recent runs to show")
	jsonOut := fs.Bool("json", false, "output JSON")
	help, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if help {
		printGetUsage(a.Out)
		return nil
	}
	if fs.NArg() != 1 {
		return errors.New("usage: opencode-jobs get <job-id> [--all]")
	}
	job, err := a.findJob(fs.Arg(0), *all)
	if err != nil {
		return err
	}
	runs, err := a.Store.ReadRuns(job, *runsLimit)
	if err != nil {
		return err
	}
	backendStatus, _ := jobs.Status(job)
	logPath := a.Store.LogPath(job)
	if a.JSON || *jsonOut {
		return a.writeJSON(map[string]any{
			"job":           job,
			"runs":          runs,
			"logPath":       logPath,
			"backendStatus": backendStatus,
		})
	}

	fmt.Fprintf(a.Out, "ID:        %s\n", job.Slug)
	fmt.Fprintf(a.Out, "Name:      %s\n", job.Name)
	fmt.Fprintf(a.Out, "Schedule:  %s (%s)\n", job.Cron, describeCron(job.Cron))
	fmt.Fprintf(a.Out, "Workdir:   %s\n", job.Workdir)
	fmt.Fprintf(a.Out, "Scope:     %s\n", job.ScopeID)
	if job.Backend == "" {
		fmt.Fprintln(a.Out, "Backend:   not installed")
	} else {
		fmt.Fprintf(a.Out, "Backend:   %s (%s)\n", job.Backend, backendStatus)
	}
	fmt.Fprintf(a.Out, "Log:       %s\n", logPath)
	if job.Agent != "" {
		fmt.Fprintf(a.Out, "Agent:     %s\n", job.Agent)
	}
	if job.Model != "" {
		fmt.Fprintf(a.Out, "Model:     %s\n", job.Model)
	}
	if job.TimeoutSeconds > 0 {
		fmt.Fprintf(a.Out, "Timeout:   %s\n", (time.Duration(job.TimeoutSeconds) * time.Second).String())
	}
	if job.LastRunAt != nil {
		fmt.Fprintf(a.Out, "Last run:  %s (%s, exit %s) %s\n",
			valueOr(job.LastRunStatus, "unknown"),
			valueOr(job.LastRunSource, "unknown"),
			exitCodeString(job.LastRunExitCode),
			job.LastRunAt.Local().Format("2006-01-02 15:04:05"),
		)
		if job.LastRunError != "" {
			fmt.Fprintf(a.Out, "Last error: %s\n", job.LastRunError)
		}
	} else {
		fmt.Fprintln(a.Out, "Last run:  never")
	}
	fmt.Fprintln(a.Out, "Prompt:")
	for _, line := range strings.Split(job.Prompt, "\n") {
		fmt.Fprintf(a.Out, "  %s\n", line)
	}
	if len(runs) > 0 {
		fmt.Fprintln(a.Out, "Recent runs:")
		for _, run := range runs {
			fmt.Fprintf(a.Out, "  %s  %-7s exit=%d  %s\n",
				run.StartedAt.Local().Format("2006-01-02 15:04:05"),
				run.Status, run.ExitCode, run.RunID)
		}
	}
	return nil
}

func (a *App) update(args []string) error {
	fs := newFlagSet("update")
	name := fs.String("name", "", "job name")
	cron := fs.String("cron", "", "five-field cron expression")
	prompt := fs.String("prompt", "", "prompt passed to opencode run")
	timeout := fs.String("timeout", "", "run timeout in seconds or as a duration such as 30m (0 disables)")
	agent := fs.String("agent", "", "OpenCode agent to use (empty clears)")
	model := fs.String("model", "", "OpenCode model in provider/model form (empty clears)")
	title := fs.String("title", "", "session title (empty clears)")
	session := fs.String("session", "", "OpenCode session id (empty clears)")
	continueSession := fs.Bool("continue", false, "continue the last OpenCode session")
	server := fs.String("server", "", "OpenCode server URL (empty clears)")
	standalone := fs.Bool("standalone", false, "run with a private OpenCode server")
	archived := fs.Bool("archived", false, "create the session archived")
	noAuto := fs.Bool("no-auto", false, "do not pass --auto to opencode run")
	workdir := fs.String("workdir", "", "not supported; recreate the job to change the workdir")
	all := fs.Bool("all", false, "search every workspace")
	var files stringList
	var envs envList
	jsonOut := fs.Bool("json", false, "output JSON")
	fs.Var(&files, "file", "file to attach (repeatable)")
	fs.Var(&envs, "env", "extra environment variable KEY=VALUE (repeatable)")

	help, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if help {
		printUpdateUsage(a.Out)
		return nil
	}
	if fs.NArg() != 1 {
		return errors.New("usage: opencode-jobs update <job-id> [options]")
	}
	if flagWasSet(fs, "workdir") {
		_ = workdir
		return errors.New("--workdir cannot be changed; remove the job and add it again")
	}
	original, err := a.findJob(fs.Arg(0), *all)
	if err != nil {
		return err
	}
	updated := *original

	if flagWasSet(fs, "name") {
		if strings.TrimSpace(*name) == "" {
			return errors.New("--name cannot be empty")
		}
		if updated.Title == updated.Name {
			updated.Title = strings.TrimSpace(*name)
		}
		updated.Name = strings.TrimSpace(*name)
	}
	if flagWasSet(fs, "cron") {
		updated.Cron = strings.TrimSpace(*cron)
	}
	if flagWasSet(fs, "prompt") {
		updated.Prompt = strings.TrimSpace(*prompt)
	}
	if flagWasSet(fs, "timeout") {
		seconds, err := parseTimeout(*timeout)
		if err != nil {
			return err
		}
		updated.TimeoutSeconds = seconds
	}
	if flagWasSet(fs, "agent") {
		updated.Agent = strings.TrimSpace(*agent)
	}
	if flagWasSet(fs, "model") {
		updated.Model = strings.TrimSpace(*model)
	}
	if flagWasSet(fs, "title") {
		updated.Title = strings.TrimSpace(*title)
	}
	if flagWasSet(fs, "session") {
		updated.Session = strings.TrimSpace(*session)
	}
	if flagWasSet(fs, "continue") {
		updated.Continue = *continueSession
	}
	if flagWasSet(fs, "server") {
		updated.Server = strings.TrimSpace(*server)
	}
	if flagWasSet(fs, "standalone") {
		updated.Standalone = *standalone
	}
	if flagWasSet(fs, "archived") {
		updated.Archived = *archived
	}
	if flagWasSet(fs, "no-auto") {
		updated.DisableAuto = *noAuto
	}
	if flagWasSet(fs, "file") {
		updated.Files = files
	}
	if flagWasSet(fs, "env") {
		updated.Env = envs.values
	}
	updated.Normalize()
	if err := updated.Validate(); err != nil {
		return err
	}
	if err := updated.CheckWorkdir(); err != nil {
		return err
	}

	bin, err := jobs.SelfPath()
	if err != nil {
		return err
	}
	if err := jobs.Uninstall(original); err != nil {
		return err
	}
	ref, err := jobs.Install(&updated, bin, a.Store)
	if err != nil {
		// Best effort: put the previous scheduler entry back.
		if restored, restoreErr := jobs.Install(original, bin, a.Store); restoreErr == nil {
			original.Backend = restored.Name
			original.Unit = restored.Unit
			original.InstalledBinary = bin
			_ = a.Store.Save(original)
		}
		return err
	}
	updated.Backend = ref.Name
	updated.Unit = ref.Unit
	updated.InstalledBinary = bin
	if err := a.Store.Save(&updated); err != nil {
		_ = jobs.Uninstall(&updated)
		_, _ = jobs.Install(original, bin, a.Store)
		return err
	}

	if a.JSON || *jsonOut {
		return a.writeJSON(map[string]any{
			"success":   true,
			"job":       &updated,
			"scheduler": ref,
		})
	}
	fmt.Fprintf(a.Out, "Updated job %s\n", updated.Slug)
	fmt.Fprintf(a.Out, "  Schedule: %s (%s)\n", updated.Cron, describeCron(updated.Cron))
	fmt.Fprintf(a.Out, "  Backend:  %s (%s)\n", ref.Name, ref.Unit)
	return nil
}

func (a *App) remove(args []string) error {
	fs := newFlagSet("remove")
	all := fs.Bool("all", false, "search every workspace")
	keepLogs := fs.Bool("keep-logs", false, "keep the job log")
	keepRuns := fs.Bool("keep-runs", false, "keep run history")
	jsonOut := fs.Bool("json", false, "output JSON")
	help, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if help {
		printRemoveUsage(a.Out)
		return nil
	}
	if fs.NArg() != 1 {
		return errors.New("usage: opencode-jobs remove <job-id> [--all]")
	}
	job, err := a.findJob(fs.Arg(0), *all)
	if err != nil {
		return err
	}
	if a.Store.IsRunning(job) {
		return fmt.Errorf("job %s is running; wait for it to finish before removing it", job.Slug)
	}
	if err := jobs.Uninstall(job); err != nil {
		return err
	}
	removed := []string{a.Store.JobPath(job.ScopeID, job.Slug)}
	if err := a.Store.Delete(job); err != nil {
		return err
	}
	if !*keepLogs {
		if err := os.Remove(a.Store.LogPath(job)); err == nil {
			removed = append(removed, a.Store.LogPath(job))
		}
	}
	if !*keepRuns {
		if err := os.Remove(a.Store.RunsPath(job)); err == nil {
			removed = append(removed, a.Store.RunsPath(job))
		}
	}
	_ = os.Remove(a.Store.LockPath(job))
	a.Store.RemoveEmptyScope(job.ScopeID)

	if a.JSON || *jsonOut {
		return a.writeJSON(map[string]any{"success": true, "removed": removed})
	}
	fmt.Fprintf(a.Out, "Removed job %s\n", job.Slug)
	for _, path := range removed {
		fmt.Fprintf(a.Out, "  %s\n", path)
	}
	return nil
}

func uniqueSlug(store *jobs.Store, scope, base string) (string, error) {
	if _, err := store.Load(scope, base); err == nil {
		// fall through to suffixes
	} else if errors.Is(err, jobs.ErrNotFound) {
		return base, nil
	} else {
		return "", err
	}
	for index := 2; index < 1000; index++ {
		candidate := fmt.Sprintf("%s-%d", base, index)
		if _, err := store.Load(scope, candidate); err == nil {
			continue
		} else if errors.Is(err, jobs.ErrNotFound) {
			return candidate, nil
		} else {
			return "", err
		}
	}
	return "", fmt.Errorf("cannot find a free job id based on %q", base)
}

func parseTimeout(value string) (int, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, nil
	}
	if seconds, err := strconv.Atoi(trimmed); err == nil {
		if seconds < 0 {
			return 0, errors.New("timeout cannot be negative")
		}
		return seconds, nil
	}
	duration, err := time.ParseDuration(trimmed)
	if err != nil {
		return 0, fmt.Errorf("invalid timeout %q: use seconds or a duration such as 30m", value)
	}
	if duration < 0 {
		return 0, errors.New("timeout cannot be negative")
	}
	return int(duration.Round(time.Second).Seconds()), nil
}

func flagWasSet(fs *flag.FlagSet, name string) bool {
	found := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

func jobStatus(store *jobs.Store, job *jobs.Job) string {
	if store.IsRunning(job) {
		return "running"
	}
	if job.LastRunStatus != "" {
		return job.LastRunStatus
	}
	return "never"
}

func describeCron(expression string) string {
	spec, err := jobs.ParseCron(expression)
	if err != nil {
		return "invalid schedule"
	}
	return spec.Describe()
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func exitCodeString(code *int) string {
	if code == nil {
		return "?"
	}
	return strconv.Itoa(*code)
}

func printAddUsage(out io.Writer) {
	fmt.Fprint(out, `Usage: opencode-jobs add --name NAME --cron CRON --prompt PROMPT [options]

Creates a job, installs it in launchd/systemd/cron, and stores its config.

Required:
  --name string       Job name
  --cron string       Five-field cron expression (or @daily, @hourly, ...)
  --prompt string     Prompt passed to opencode run

Options:
  --id string         Job id (defaults to a slug of --name)
  --workdir string    Working directory (defaults to the current directory)
  --timeout string    Seconds or duration (30m); 0 disables
  --agent string      OpenCode agent
  --model string      OpenCode model (provider/model)
  --title string      Session title (defaults to the job name)
  --file string       File to attach (repeatable)
  --session string    OpenCode session id
  --continue          Continue the last session
  --server string     OpenCode server URL
  --standalone        Use a private OpenCode server
  --archived          Create the session archived
  --no-auto           Do not pass --auto to opencode run
  --env KEY=VALUE     Extra environment variable (repeatable)
  --json              Output JSON
  -h, --help          Show this help

Examples:
  opencode-jobs add --name "Daily deals" --cron "0 9 * * *" \
    --prompt "Search for standing desks under $300 and summarize the best 5." \
    --timeout 30m
`)
}

func printListUsage(out io.Writer) {
	fmt.Fprint(out, `Usage: opencode-jobs list [--all] [--json]

Lists jobs in the current workspace. Use --all to include every workspace.
`)
}

func printGetUsage(out io.Writer) {
	fmt.Fprint(out, `Usage: opencode-jobs get <job-id> [--all] [--runs N] [--json]

Shows the job configuration, recent runs, log path, and scheduler status.
`)
}

func printUpdateUsage(out io.Writer) {
	fmt.Fprint(out, `Usage: opencode-jobs update <job-id> [options]

Only the flags you provide are changed. Use an empty value to clear an optional
field, for example --agent "" or --timeout 0.

Options:
  --name, --cron, --prompt, --timeout, --agent, --model, --title,
  --session, --continue, --server, --standalone, --archived, --no-auto,
  --file, --env
  --all      Search every workspace
  --json     Output JSON
`)
}

func printRemoveUsage(out io.Writer) {
	fmt.Fprint(out, `Usage: opencode-jobs remove <job-id> [--all] [--keep-logs] [--keep-runs] [--json]

Removes the scheduler entry and the job file. Logs and run history are removed
unless --keep-logs or --keep-runs is set.
`)
}
