package cli

import (
	"bytes"
	"flag"
	"strings"
	"testing"
)

func TestReorderFlags(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	wait := fs.Bool("wait", false, "")
	name := fs.String("name", "", "")
	args := reorderFlags(fs, []string{"job-id", "--wait", "--name", "value", "extra"})
	want := []string{"--wait", "--name", "value", "job-id", "extra"}
	if strings.Join(args, " ") != strings.Join(want, " ") {
		t.Fatalf("reorderFlags = %#v, want %#v", args, want)
	}
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
	if !*wait || *name != "value" {
		t.Fatalf("wait=%v name=%q", *wait, *name)
	}
	if fs.NArg() != 2 || fs.Arg(0) != "job-id" || fs.Arg(1) != "extra" {
		t.Fatalf("args = %#v", fs.Args())
	}
}

func TestReorderFlagsConsumesDashValue(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	prompt := fs.String("prompt", "", "")
	args := reorderFlags(fs, []string{"job", "--prompt", "-hello"})
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
	if *prompt != "-hello" {
		t.Fatalf("prompt = %q", *prompt)
	}
	if fs.NArg() != 1 || fs.Arg(0) != "job" {
		t.Fatalf("args = %#v", fs.Args())
	}
}

func TestParseTimeout(t *testing.T) {
	tests := map[string]int{
		"":    0,
		"0":   0,
		"30":  30,
		"30m": 1800,
		"1h":  3600,
		"90s": 90,
	}
	for input, want := range tests {
		got, err := parseTimeout(input)
		if err != nil {
			t.Fatalf("parseTimeout(%q): %v", input, err)
		}
		if got != want {
			t.Fatalf("parseTimeout(%q) = %d, want %d", input, got, want)
		}
	}
	if _, err := parseTimeout("-1"); err == nil {
		t.Fatal("expected negative timeout error")
	}
	if _, err := parseTimeout("nope"); err == nil {
		t.Fatal("expected invalid timeout error")
	}
}

func TestRunVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"version"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "opencode-jobs") {
		t.Fatalf("output = %q", stdout.String())
	}
}

func TestRunUnknownCommandJSON(t *testing.T) {
	t.Setenv("OPENCODE_JOBS_HOME", t.TempDir())
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"--json", "bogus"}, &stdout, &stderr); code != 1 {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(stdout.String(), `"success":false`) {
		t.Fatalf("output = %q", stdout.String())
	}
}

func TestRunListEmpty(t *testing.T) {
	t.Setenv("OPENCODE_JOBS_HOME", t.TempDir())
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"list"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "No jobs") {
		t.Fatalf("output = %q", stdout.String())
	}
}

func TestRunHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(stdout.String(), "Usage:") {
		t.Fatalf("output = %q", stdout.String())
	}
}
