package jobs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSlugify(t *testing.T) {
	tests := map[string]string{
		"Daily Deals!":      "daily-deals",
		"  Weekly Report  ": "weekly-report",
		"":                  "job",
		"---":               "job",
		"Hello, World":      "hello-world",
	}
	for input, want := range tests {
		if got := Slugify(input); got != want {
			t.Fatalf("Slugify(%q) = %q, want %q", input, got, want)
		}
	}
	if got := Slugify(strings.Repeat("a", 80)); len(got) > 48 {
		t.Fatalf("Slugify() length = %d, want <= 48", len(got))
	}
}

func TestDeriveScopeID(t *testing.T) {
	first := DeriveScopeID("/tmp/project")
	second := DeriveScopeID("/tmp/project")
	if first != second {
		t.Fatalf("DeriveScopeID is not stable: %q != %q", first, second)
	}
	if first == DeriveScopeID("/tmp/other") {
		t.Fatal("different workdirs produced the same scope id")
	}
	if !strings.HasPrefix(first, "project-") {
		t.Fatalf("scope id %q does not include the directory name", first)
	}
}

func TestValidateJob(t *testing.T) {
	workdir := t.TempDir()
	valid := &Job{
		Slug:    "daily",
		Name:    "Daily",
		Cron:    "0 9 * * *",
		Workdir: workdir,
		Prompt:  "hello",
	}
	valid.Normalize()
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid job failed validation: %v", err)
	}
	if err := valid.CheckWorkdir(); err != nil {
		t.Fatalf("valid workdir failed: %v", err)
	}

	tests := []struct {
		name string
		edit func(*Job)
	}{
		{"invalid slug", func(j *Job) { j.Slug = "Daily Job" }},
		{"missing name", func(j *Job) { j.Name = "" }},
		{"missing prompt", func(j *Job) { j.Prompt = "" }},
		{"invalid cron", func(j *Job) { j.Cron = "nope" }},
		{"relative workdir", func(j *Job) { j.Workdir = "." }},
		{"negative timeout", func(j *Job) { j.TimeoutSeconds = -1 }},
		{"continue and session", func(j *Job) { j.Continue = true; j.Session = "ses_1" }},
		{"standalone and server", func(j *Job) { j.Standalone = true; j.Server = "http://localhost:4096" }},
		{"invalid env", func(j *Job) { j.Env = map[string]string{"BAD-KEY": "x"} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			job := *valid
			test.edit(&job)
			if err := job.Validate(); err == nil {
				t.Fatalf("expected validation error")
			}
		})
	}

	missing := &Job{Slug: "x", Name: "x", Cron: "0 9 * * *", Workdir: filepath.Join(workdir, "missing"), Prompt: "x"}
	if err := missing.CheckWorkdir(); err == nil {
		t.Fatal("expected missing workdir error")
	}
}

func TestBuildInvocation(t *testing.T) {
	job := &Job{
		Slug:     "daily",
		Name:     "Daily",
		Cron:     "0 9 * * *",
		Workdir:  t.TempDir(),
		Prompt:   "hello world",
		Agent:    "build",
		Model:    "provider/model",
		Files:    []string{"a.txt", "b.txt"},
		Archived: true,
	}
	job.Normalize()
	inv, err := BuildInvocation(job)
	if err != nil {
		t.Fatal(err)
	}
	wantSuffix := []string{
		"run", "--auto", "--agent", "build", "--model", "provider/model",
		"--title", "Daily", "--archived", "--file", "a.txt", "--file", "b.txt",
		"--", "hello world",
	}
	if strings.Join(inv.Args, " ") != strings.Join(wantSuffix, " ") {
		t.Fatalf("args = %#v, want suffix %#v", inv.Args, wantSuffix)
	}

	job.DisableAuto = true
	inv, err = BuildInvocation(job)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(inv.Args, " "), "--auto") {
		t.Fatalf("unexpected --auto: %#v", inv.Args)
	}
}

func TestBuildEnv(t *testing.T) {
	t.Setenv("OPENCODE_JOBS_HOME", "/tmp/oj-home")
	job := &Job{
		Slug:    "daily",
		Name:    "Daily",
		Cron:    "0 9 * * *",
		Workdir: t.TempDir(),
		Prompt:  "hello",
		Env:     map[string]string{"MY_JOB_VAR": "value"},
	}
	job.Normalize()
	env := envMap(BuildEnv(job, "run-1", "/tmp/oj-home"))
	if env["MY_JOB_VAR"] != "value" {
		t.Fatalf("MY_JOB_VAR = %q", env["MY_JOB_VAR"])
	}
	if env["OPENCODE_JOBS_RUN_ID"] != "run-1" {
		t.Fatalf("OPENCODE_JOBS_RUN_ID = %q", env["OPENCODE_JOBS_RUN_ID"])
	}
	if env["OPENCODE_JOBS_HOME"] != "/tmp/oj-home" {
		t.Fatalf("OPENCODE_JOBS_HOME = %q", env["OPENCODE_JOBS_HOME"])
	}
	if env["PWD"] != job.Workdir {
		t.Fatalf("PWD = %q, want %q", env["PWD"], job.Workdir)
	}
	if env["NO_COLOR"] != "1" {
		t.Fatalf("NO_COLOR = %q", env["NO_COLOR"])
	}
	if !strings.Contains(env["PATH"], "/usr/bin") {
		t.Fatalf("PATH = %q", env["PATH"])
	}
}

func TestResolveWorkdir(t *testing.T) {
	dir := t.TempDir()
	resolved, err := ResolveWorkdir(dir)
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != want {
		t.Fatalf("ResolveWorkdir(%q) = %q, want %q", dir, resolved, want)
	}
	home, _ := os.UserHomeDir()
	resolved, err = ResolveWorkdir("~")
	if err != nil {
		t.Fatal(err)
	}
	if resolved != home {
		t.Fatalf("ResolveWorkdir(~) = %q, want %q", resolved, home)
	}
}

func envMap(env []string) map[string]string {
	out := make(map[string]string, len(env))
	for _, entry := range env {
		key, value, _ := strings.Cut(entry, "=")
		out[key] = value
	}
	return out
}
