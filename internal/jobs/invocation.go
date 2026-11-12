package jobs

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/Martins6/opencode-jobs/internal/opencode"
)

// Invocation is the command line handed to the OS scheduler.
type Invocation struct {
	Command string
	Args    []string
}

// BuildInvocation renders the OpenCode V2 `run` command for a job.
func BuildInvocation(j *Job) (*Invocation, error) {
	if strings.TrimSpace(j.Prompt) == "" {
		return nil, fmt.Errorf("job %s has no prompt", j.Slug)
	}
	args := []string{"run"}
	if !j.DisableAuto {
		args = append(args, "--auto")
	}
	if j.Standalone {
		args = append(args, "--standalone")
	}
	if j.Server != "" {
		args = append(args, "--server", j.Server)
	}
	if j.Agent != "" {
		args = append(args, "--agent", j.Agent)
	}
	if j.Model != "" {
		args = append(args, "--model", j.Model)
	}
	title := j.Title
	if title == "" {
		title = j.Name
	}
	if title != "" {
		args = append(args, "--title", title)
	}
	if j.Archived {
		args = append(args, "--archived")
	}
	if j.Continue {
		args = append(args, "--continue")
	}
	if j.Session != "" {
		args = append(args, "--session", j.Session)
	}
	for _, file := range j.Files {
		args = append(args, "--file", file)
	}
	args = append(args, "--", j.Prompt)
	return &Invocation{Command: opencode.Find(), Args: args}, nil
}

// BuildEnv returns the child environment for a run. Job-level variables win
// over inherited variables; PATH is always enriched for GUI-less schedulers.
func BuildEnv(j *Job, runID, storeHome string) []string {
	values := make(map[string]string)
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		values[key] = value
	}
	for key, value := range j.Env {
		values[key] = value
	}

	values["PATH"] = opencode.EnhancedPath(values["PATH"])
	if values["HOME"] == "" {
		if home, err := os.UserHomeDir(); err == nil {
			values["HOME"] = home
		}
	}
	if values["TMPDIR"] == "" {
		values["TMPDIR"] = os.TempDir()
	}
	if values["LANG"] == "" {
		values["LANG"] = "en_US.UTF-8"
	}
	if values["SHELL"] == "" {
		values["SHELL"] = "/bin/sh"
	}
	if values["NO_COLOR"] == "" {
		values["NO_COLOR"] = "1"
	}
	if values["CLICOLOR"] == "" {
		values["CLICOLOR"] = "0"
	}
	values["OPENCODE_JOBS_HOME"] = storeHome
	values["OPENCODE_JOBS_RUN_ID"] = runID
	values["OPENCODE_JOBS_JOB"] = j.ScopeID + "/" + j.Slug
	values["OPENCODE_JOBS_SCOPE"] = j.ScopeID
	values["PWD"] = j.Workdir

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	env := make([]string, 0, len(keys))
	for _, key := range keys {
		env = append(env, key+"="+values[key])
	}
	return env
}
