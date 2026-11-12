package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/Martins6/opencode-jobs/internal/jobs"
)

func (a *App) cleanup(args []string) error {
	fs := newFlagSet("cleanup")
	confirm := fs.Bool("confirm", false, "actually remove jobs (default is a dry run)")
	allScopes := fs.Bool("all-scopes", false, "clean every workspace")
	includeHistory := fs.Bool("include-history", false, "also remove logs and run history")
	jsonOut := fs.Bool("json", false, "output JSON")
	help, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if help {
		printCleanupUsage(a.Out)
		return nil
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	list, err := a.Store.List(*allScopes)
	if err != nil {
		return err
	}
	type item struct {
		Slug    string `json:"slug"`
		ScopeID string `json:"scopeId"`
		Backend string `json:"backend,omitempty"`
		Unit    string `json:"unit,omitempty"`
		Status  string `json:"status"`
	}
	items := make([]item, 0, len(list))
	for _, job := range list {
		status, _ := jobs.Status(job)
		items = append(items, item{
			Slug:    job.Slug,
			ScopeID: job.ScopeID,
			Backend: job.Backend,
			Unit:    job.Unit,
			Status:  status,
		})
	}
	if !*confirm {
		if a.JSON || *jsonOut {
			return a.writeJSON(map[string]any{
				"success":        true,
				"dryRun":         true,
				"jobs":           items,
				"includeHistory": *includeHistory,
			})
		}
		if len(items) == 0 {
			fmt.Fprintln(a.Out, "Nothing to clean up.")
			return nil
		}
		scope := "current workspace"
		if *allScopes {
			scope = "all workspaces"
		}
		fmt.Fprintf(a.Out, "Cleanup plan (%s):\n", scope)
		for _, entry := range items {
			backend := entry.Backend
			if backend == "" {
				backend = "not installed"
			}
			fmt.Fprintf(a.Out, "  %-28s %-10s %-10s %s\n", entry.Slug, backend, entry.Status, entry.ScopeID)
		}
		if *includeHistory {
			fmt.Fprintln(a.Out, "Logs and run history will also be removed.")
		} else {
			fmt.Fprintln(a.Out, "Logs and run history will be kept (use --include-history to remove them).")
		}
		fmt.Fprintf(a.Out, "Run with --confirm to remove %d job(s).\n", len(items))
		return nil
	}

	var failures []string
	removed := 0
	scopes := map[string]struct{}{}
	for _, job := range list {
		scopes[job.ScopeID] = struct{}{}
		if a.Store.IsRunning(job) {
			failures = append(failures, job.Slug+": job is running")
			continue
		}
		if err := jobs.Uninstall(job); err != nil {
			failures = append(failures, job.Slug+": "+err.Error())
			continue
		}
		if err := a.Store.Delete(job); err != nil {
			failures = append(failures, job.Slug+": "+err.Error())
			continue
		}
		if *includeHistory {
			if err := a.Store.DeleteHistory(job); err != nil {
				failures = append(failures, job.Slug+": "+err.Error())
				continue
			}
		}
		removed++
	}
	for scope := range scopes {
		a.Store.RemoveEmptyScope(scope)
	}

	if a.JSON || *jsonOut {
		payload := map[string]any{
			"success": len(failures) == 0,
			"dryRun":  false,
			"removed": removed,
			"jobs":    items,
		}
		if len(failures) > 0 {
			payload["errors"] = failures
		}
		return a.writeJSON(payload)
	}
	fmt.Fprintf(a.Out, "Removed %d job(s).\n", removed)
	if len(failures) > 0 {
		return fmt.Errorf("%d job(s) failed to clean up: %s", len(failures), strings.Join(failures, "; "))
	}
	return nil
}

func printCleanupUsage(out io.Writer) {
	fmt.Fprint(out, `Usage: opencode-jobs cleanup [--confirm] [--all-scopes] [--include-history] [--json]

Dry-runs by default. Use --confirm to remove the scheduler entries and job
files. Logs and run history are kept unless --include-history is set.
`)
}
