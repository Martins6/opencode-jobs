package jobs

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeOpencode(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "opencode")
	script := "#!/bin/sh\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSuperviseSuccess(t *testing.T) {
	t.Setenv("OPENCODE_JOBS_HOME", t.TempDir())
	store, err := NewStore()
	if err != nil {
		t.Fatal(err)
	}
	job := testJob(t, "scope-1", "daily")
	job.Prompt = "hello"
	if err := store.Save(job); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENCODE_JOBS_OPENCODE_PATH", fakeOpencode(t, `echo "fake opencode: $*"; exit 0`))

	code := Supervise(store.JobPath(job.ScopeID, job.Slug), SuperviseOptions{
		Source: "test",
		RunID:  "run-success",
	}, io.Discard)
	if code != 0 {
		t.Fatalf("Supervise exit code = %d, want 0", code)
	}
	loaded, err := store.Load(job.ScopeID, job.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.LastRunStatus != "success" {
		t.Fatalf("last status = %q", loaded.LastRunStatus)
	}
	runs, err := store.ReadRuns(loaded, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].Status != "success" || runs[0].ExitCode != 0 {
		t.Fatalf("runs = %#v", runs)
	}
	logData, err := os.ReadFile(store.LogPath(loaded))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logData), "fake opencode: run --auto") {
		t.Fatalf("log did not contain the invocation:\n%s", logData)
	}
	if !strings.Contains(string(logData), "status=success") {
		t.Fatalf("log did not contain the footer:\n%s", logData)
	}
}

func TestSuperviseTimeout(t *testing.T) {
	t.Setenv("OPENCODE_JOBS_HOME", t.TempDir())
	store, err := NewStore()
	if err != nil {
		t.Fatal(err)
	}
	job := testJob(t, "scope-1", "slow")
	job.TimeoutSeconds = 1
	if err := store.Save(job); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENCODE_JOBS_OPENCODE_PATH", fakeOpencode(t, `sleep 10; exit 0`))

	started := time.Now()
	code := Supervise(store.JobPath(job.ScopeID, job.Slug), SuperviseOptions{
		Source: "test",
		RunID:  "run-timeout",
	}, io.Discard)
	elapsed := time.Since(started)
	if code != 124 {
		t.Fatalf("Supervise exit code = %d, want 124", code)
	}
	if elapsed > 8*time.Second {
		t.Fatalf("timeout took too long: %s", elapsed)
	}
	loaded, err := store.Load(job.ScopeID, job.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.LastRunStatus != "failed" || loaded.LastRunError != "timeout" {
		t.Fatalf("last run = %q / %q", loaded.LastRunStatus, loaded.LastRunError)
	}
	runs, err := store.ReadRuns(loaded, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].ExitCode != 124 || runs[0].Error != "timeout" {
		t.Fatalf("runs = %#v", runs)
	}
}
