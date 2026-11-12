// Package cli implements the opencode-jobs command line interface.
package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Martins6/opencode-jobs/internal/jobs"
	"github.com/Martins6/opencode-jobs/internal/version"
)

// App holds command dependencies.
type App struct {
	Out   io.Writer
	Err   io.Writer
	Store *jobs.Store
	JSON  bool
}

// ExitError carries a specific process exit code.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string { return e.Err.Error() }
func (e *ExitError) Unwrap() error { return e.Err }

// Run executes a CLI invocation and returns a process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}

	jsonMode := false
	for _, arg := range args {
		if arg == "--json" {
			jsonMode = true
		}
	}

	// Accept a few global flags before the subcommand.
	index := 0
	for index < len(args) {
		switch args[index] {
		case "--json":
			jsonMode = true
			index++
		case "--version", "-v":
			printVersion(stdout)
			return 0
		case "--help", "-h":
			printUsage(stdout)
			return 0
		default:
			goto dispatch
		}
	}
dispatch:
	args = args[index:]
	if len(args) == 0 {
		printUsage(stdout)
		return 0
	}

	store, err := jobs.NewStore()
	if err != nil {
		fmt.Fprintf(stderr, "opencode-jobs: %v\n", err)
		return 1
	}
	app := &App{Out: stdout, Err: stderr, Store: store, JSON: jsonMode}

	command := args[0]
	rest := args[1:]
	switch command {
	case "add", "create", "schedule":
		err = app.add(rest)
	case "list", "ls":
		err = app.list(rest)
	case "get", "show":
		err = app.get(rest)
	case "update", "edit":
		err = app.update(rest)
	case "remove", "rm", "delete":
		err = app.remove(rest)
	case "run":
		err = app.run(rest)
	case "logs", "log":
		err = app.logs(rest)
	case "reinstall":
		err = app.reinstall(rest)
	case "cleanup", "clean":
		err = app.cleanup(rest)
	case "doctor", "status":
		err = app.doctor(rest)
	case "version":
		if jsonMode {
			_ = json.NewEncoder(stdout).Encode(map[string]string{
				"version": version.Version,
				"commit":  version.Commit,
				"date":    version.Date,
			})
			return 0
		}
		printVersion(stdout)
		return 0
	case "help":
		printUsage(stdout)
		return 0
	case "__supervise":
		return app.supervise(rest)
	default:
		err = fmt.Errorf("unknown command %q (run 'opencode-jobs help')", command)
	}

	if err != nil {
		var exitErr *ExitError
		if errors.As(err, &exitErr) {
			app.printError(exitErr.Err)
			return exitErr.Code
		}
		app.printError(err)
		return 1
	}
	return 0
}

func (a *App) printError(err error) {
	if a.JSON {
		_ = json.NewEncoder(a.Out).Encode(map[string]any{
			"success": false,
			"error":   err.Error(),
		})
		return
	}
	fmt.Fprintf(a.Err, "error: %v\n", err)
}

func (a *App) writeJSON(value any) error {
	encoder := json.NewEncoder(a.Out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func printVersion(out io.Writer) {
	fmt.Fprintf(out, "opencode-jobs %s\n", version.String())
}

func printUsage(out io.Writer) {
	fmt.Fprint(out, `opencode-jobs - schedule recurring OpenCode agent runs

Usage:
  opencode-jobs <command> [options]

Commands:
  add        Create a scheduled job
  list       List jobs in the current workspace
  get        Show a job, recent runs, and scheduler state
  update     Change a job
  remove     Delete a job and its scheduler entry
  run        Run a job now
  logs       Show or follow a job log
  reinstall  Reinstall scheduler entries with the current binary
  cleanup    Remove jobs and scheduler artifacts
  doctor     Check the installation
  version    Print version information

Examples:
  opencode-jobs add --name "Daily deals" --cron "0 9 * * *" \
    --prompt "Search for standing desks under $300 and summarize the best 5."

  opencode-jobs list
  opencode-jobs get daily-deals --json
  opencode-jobs run daily-deals --wait
  opencode-jobs logs daily-deals -n 50
  opencode-jobs remove daily-deals

Jobs are scoped to their working directory. Run commands from the project that
owns the job, or pass --all on commands that support it.

Environment:
  OPENCODE_JOBS_HOME             Override the config directory
  OPENCODE_JOBS_OPENCODE_PATH    Override the opencode executable
  OPENCODE_JOBS_BACKEND          Force launchd, systemd, or cron
`)
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

func parseFlags(fs *flag.FlagSet, args []string) (bool, error) {
	reordered := reorderFlags(fs, args)
	if err := fs.Parse(reordered); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return true, nil
		}
		return false, err
	}
	return false, nil
}

// reorderFlags moves flags before positional arguments. The standard flag
// package stops at the first positional, but `run job --wait` and
// `logs job -f` are natural to type.
func reorderFlags(fs *flag.FlagSet, args []string) []string {
	var flags []string
	var positional []string
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			positional = append(positional, args[index:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positional = append(positional, arg)
			continue
		}
		flags = append(flags, arg)
		name, _, hasValue := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		definition := fs.Lookup(name)
		if definition != nil && !hasValue && !isBoolFlag(definition.Value) && index+1 < len(args) {
			flags = append(flags, args[index+1])
			index++
		}
	}
	return append(flags, positional...)
}

type boolFlag interface {
	IsBoolFlag() bool
}

func isBoolFlag(value flag.Value) bool {
	boolean, ok := value.(boolFlag)
	return ok && boolean.IsBoolFlag()
}

func (a *App) findJob(slug string, all bool) (*jobs.Job, error) {
	if strings.TrimSpace(slug) == "" {
		return nil, errors.New("job id is required")
	}
	return a.Store.Find(slug, all)
}

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(value string) error {
	*s = append(*s, value)
	return nil
}

type envList struct {
	values map[string]string
}

func (e *envList) String() string {
	if e == nil || len(e.values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(e.values))
	for key := range e.values {
		keys = append(keys, key)
	}
	return strings.Join(keys, ",")
}

func (e *envList) Set(value string) error {
	key, val, ok := strings.Cut(value, "=")
	if !ok {
		return fmt.Errorf("environment variable must be KEY=VALUE: %q", value)
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("environment variable name is empty")
	}
	if e.values == nil {
		e.values = make(map[string]string)
	}
	e.values[key] = val
	return nil
}
