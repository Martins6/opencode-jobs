package jobs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderLaunchdPlist(t *testing.T) {
	job := &Job{
		Slug:    "daily",
		Name:    "Daily & More",
		Cron:    "0 9 1 * 1",
		Workdir: "/tmp/work dir",
		ScopeID: "scope-1",
		Prompt:  "hello",
	}
	plist, err := renderLaunchdPlist(job, "/usr/local/bin/opencode-jobs", "/tmp/job.json", "/tmp/log file.log")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<string>com.opencode.jobs.scope-1.daily</string>",
		"<string>/usr/local/bin/opencode-jobs</string>",
		"<string>__supervise</string>",
		"<string>/tmp/job.json</string>",
		"<string>/tmp/work dir</string>",
		"<string>/tmp/log file.log</string>",
		"<key>Minute</key>",
		"<integer>0</integer>",
		"<key>Hour</key>",
		"<key>Day</key>",
		"<key>Weekday</key>",
		"<key>RunAtLoad</key>",
		"<false/>",
	} {
		if !strings.Contains(plist, want) {
			t.Errorf("plist missing %q\n%s", want, plist)
		}
	}
}

func TestRenderSystemd(t *testing.T) {
	home := t.TempDir()
	workdir := filepath.Join(home, "work%dir")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	job := &Job{
		Slug:    "daily",
		Name:    "Daily",
		Cron:    "0 9 * * *",
		Workdir: workdir,
		ScopeID: "scope-1",
		Prompt:  "hello",
	}
	store := &Store{Home: home}
	service, err := renderSystemdService(job, "/usr/local/bin/opencode-jobs", store)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Type=oneshot",
		"WorkingDirectory=" + strings.ReplaceAll(workdir, "%", "%%"),
		`ExecStart="/usr/local/bin/opencode-jobs" "__supervise" `,
		`Environment="PATH=`,
		"StandardOutput=null",
	} {
		if !strings.Contains(service, want) {
			t.Errorf("service missing %q\n%s", want, service)
		}
	}
	if !strings.Contains(service, `%%dir`) {
		t.Errorf("percent sign was not escaped:\n%s", service)
	}

	timer, err := renderSystemdTimer(job)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"OnCalendar=*-*-* 09:00:00",
		"Persistent=true",
		"WantedBy=timers.target",
	} {
		if !strings.Contains(timer, want) {
			t.Errorf("timer missing %q\n%s", want, timer)
		}
	}
}

func TestCronBlock(t *testing.T) {
	job := &Job{
		Slug:    "daily",
		Cron:    "0 9 * * *",
		ScopeID: "scope-1",
		Workdir: "/tmp/work",
		Prompt:  "hello",
	}
	store := &Store{Home: t.TempDir()}
	block := cronBlock(job, "/usr/local/bin/opencode-jobs", store)
	for _, want := range []string{
		"# BEGIN opencode-jobs:scope-1.daily",
		"# END opencode-jobs:scope-1.daily",
		"0 9 * * * /usr/bin/env PATH=",
		"__supervise",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("cron block missing %q\n%s", want, block)
		}
	}

	existing := "MAILTO=user@example.com\n" + block + "\n\n# user entry\n0 1 * * * true\n"
	stripped := stripCronBlock(existing, "scope-1.daily")
	if strings.Contains(stripped, "opencode-jobs") {
		t.Fatalf("block was not stripped:\n%s", stripped)
	}
	if !strings.Contains(stripped, "MAILTO=user@example.com") || !strings.Contains(stripped, "0 1 * * * true") {
		t.Fatalf("user entries were lost:\n%s", stripped)
	}
}

func TestShellQuote(t *testing.T) {
	if got := shellQuote("a'b"); got != `'a'\''b'` {
		t.Fatalf("shellQuote = %q", got)
	}
}
