package jobs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testJob(t *testing.T, scope, slug string) *Job {
	t.Helper()
	workdir := t.TempDir()
	job := &Job{
		Slug:    slug,
		Name:    "Test Job",
		Cron:    "0 9 * * *",
		Workdir: workdir,
		Prompt:  "hello",
		ScopeID: scope,
	}
	job.Normalize()
	if err := job.Validate(); err != nil {
		t.Fatal(err)
	}
	return job
}

func TestStoreRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OPENCODE_JOBS_HOME", home)
	store, err := NewStore()
	if err != nil {
		t.Fatal(err)
	}
	cwd, err := ResolveWorkdir("")
	if err != nil {
		t.Fatal(err)
	}
	scope := DeriveScopeID(cwd)
	job := testJob(t, scope, "daily")
	job.Env = map[string]string{"TOKEN": "secret"}

	if err := store.Save(job); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(scope, "daily")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Name != job.Name || loaded.Prompt != job.Prompt {
		t.Fatalf("loaded job = %#v", loaded)
	}
	if loaded.Env["TOKEN"] != "secret" {
		t.Fatalf("env was not preserved: %#v", loaded.Env)
	}
	info, err := os.Stat(store.JobPath(scope, "daily"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("job permissions = %o, want 600", info.Mode().Perm())
	}

	list, err := store.List(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Slug != "daily" {
		t.Fatalf("List(false) = %#v", list)
	}
	all, err := store.List(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("List(true) = %#v", all)
	}
}

func TestStoreFindAndAmbiguity(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OPENCODE_JOBS_HOME", home)
	store, err := NewStore()
	if err != nil {
		t.Fatal(err)
	}
	cwd, _ := ResolveWorkdir("")
	currentScope := DeriveScopeID(cwd)
	first := testJob(t, currentScope, "daily")
	if err := store.Save(first); err != nil {
		t.Fatal(err)
	}
	found, err := store.Find("daily", false)
	if err != nil {
		t.Fatal(err)
	}
	if found.ScopeID != currentScope {
		t.Fatalf("Find returned scope %q", found.ScopeID)
	}

	other := testJob(t, DeriveScopeID(t.TempDir()), "daily")
	if err := store.Save(other); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Find("daily", false); err != nil {
		t.Fatalf("current-scope find should still work: %v", err)
	}

	// Remove the current-scope copy so --all must disambiguate.
	if err := store.Delete(first); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Find("daily", true); err != nil {
		t.Fatalf("single match across scopes failed: %v", err)
	}

	second := testJob(t, DeriveScopeID(t.TempDir()), "daily")
	if err := store.Save(second); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Find("daily", true); err == nil {
		t.Fatal("expected ambiguity error")
	}
}

func TestLock(t *testing.T) {
	t.Setenv("OPENCODE_JOBS_HOME", t.TempDir())
	store, err := NewStore()
	if err != nil {
		t.Fatal(err)
	}
	job := testJob(t, "scope-1", "daily")
	if err := store.EnsureJobDirs(job); err != nil {
		t.Fatal(err)
	}
	release, err := store.AcquireLock(job, LockInfo{PID: os.Getpid(), RunID: "run-1", Source: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if !store.IsRunning(job) {
		t.Fatal("IsRunning = false while lock is held")
	}
	if _, err := store.AcquireLock(job, LockInfo{PID: os.Getpid(), RunID: "run-2"}); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second lock error = %v, want ErrAlreadyRunning", err)
	}
	release()
	if store.IsRunning(job) {
		t.Fatal("IsRunning = true after release")
	}
	release2, err := store.AcquireLock(job, LockInfo{PID: os.Getpid(), RunID: "run-3"})
	if err != nil {
		t.Fatal(err)
	}
	release2()
}

func TestRuns(t *testing.T) {
	t.Setenv("OPENCODE_JOBS_HOME", t.TempDir())
	store, err := NewStore()
	if err != nil {
		t.Fatal(err)
	}
	job := testJob(t, "scope-1", "daily")
	for index := 0; index < 3; index++ {
		record := RunRecord{
			RunID:      string(rune('a' + index)),
			Slug:       job.Slug,
			ScopeID:    job.ScopeID,
			Status:     "success",
			StartedAt:  time.Now().UTC(),
			FinishedAt: time.Now().UTC(),
		}
		if err := store.AppendRun(job, record); err != nil {
			t.Fatal(err)
		}
	}
	runs, err := store.ReadRuns(job, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 || runs[0].RunID != "b" || runs[1].RunID != "c" {
		t.Fatalf("ReadRuns = %#v", runs)
	}
}

func TestRemoveEmptyScopePreservesHistory(t *testing.T) {
	t.Setenv("OPENCODE_JOBS_HOME", t.TempDir())
	store, err := NewStore()
	if err != nil {
		t.Fatal(err)
	}
	job := testJob(t, "scope-1", "daily")
	if err := store.EnsureJobDirs(job); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.LogPath(job), []byte("log"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(job); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(job); err != nil {
		t.Fatal(err)
	}
	store.RemoveEmptyScope(job.ScopeID)
	if _, err := os.Stat(store.LogPath(job)); err != nil {
		t.Fatalf("log was removed: %v", err)
	}
	if err := os.Remove(store.LogPath(job)); err != nil {
		t.Fatal(err)
	}
	store.RemoveEmptyScope(job.ScopeID)
	if _, err := os.Stat(filepath.Join(store.Home, "scopes", job.ScopeID)); !os.IsNotExist(err) {
		t.Fatalf("scope directory still exists: %v", err)
	}
}
