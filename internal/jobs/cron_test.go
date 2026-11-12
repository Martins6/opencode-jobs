package jobs

import (
	"reflect"
	"testing"
)

func TestParseCron(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		want    *CronSpec
		wantErr bool
	}{
		{
			name: "daily",
			expr: "0 9 * * *",
			want: &CronSpec{Minute: []int{0}, Hour: []int{9}},
		},
		{
			name: "steps",
			expr: "*/15 * * * *",
			want: &CronSpec{Minute: []int{0, 15, 30, 45}},
		},
		{
			name: "weekdays",
			expr: "0 9 * * 1-5",
			want: &CronSpec{Minute: []int{0}, Hour: []int{9}, DayOfWeek: []int{1, 2, 3, 4, 5}},
		},
		{
			name: "sunday seven",
			expr: "0 0 * * 7",
			want: &CronSpec{Minute: []int{0}, Hour: []int{0}, DayOfWeek: []int{0}},
		},
		{
			name: "macro",
			expr: "@daily",
			want: &CronSpec{Minute: []int{0}, Hour: []int{0}},
		},
		{
			name: "full range becomes wildcard",
			expr: "0-59 0-23 1-31 1-12 0-6",
			want: &CronSpec{},
		},
		{
			name: "list and range",
			expr: "0 9,17 * * *",
			want: &CronSpec{Minute: []int{0}, Hour: []int{9, 17}},
		},
		{name: "too few fields", expr: "* * *", wantErr: true},
		{name: "minute out of range", expr: "60 * * * *", wantErr: true},
		{name: "zero step", expr: "*/0 * * * *", wantErr: true},
		{name: "weekday out of range", expr: "* * * * 8", wantErr: true},
		{name: "reversed range", expr: "10-1 * * * *", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseCron(test.expr)
			if test.wantErr {
				if err == nil {
					t.Fatalf("ParseCron(%q) returned no error", test.expr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseCron(%q) error: %v", test.expr, err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("ParseCron(%q) = %#v, want %#v", test.expr, got, test.want)
			}
		})
	}
}

func TestLaunchdCalendars(t *testing.T) {
	spec, err := ParseCron("0 9 * * *")
	if err != nil {
		t.Fatal(err)
	}
	got := spec.LaunchdCalendars()
	want := []map[string]int{{"Minute": 0, "Hour": 9}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LaunchdCalendars() = %#v, want %#v", got, want)
	}

	spec, err = ParseCron("0 9 1 * 1")
	if err != nil {
		t.Fatal(err)
	}
	got = spec.LaunchdCalendars()
	want = []map[string]int{
		{"Minute": 0, "Hour": 9, "Day": 1},
		{"Minute": 0, "Hour": 9, "Weekday": 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LaunchdCalendars() = %#v, want %#v", got, want)
	}

	spec, err = ParseCron("* 9 * * *")
	if err != nil {
		t.Fatal(err)
	}
	got = spec.LaunchdCalendars()
	want = []map[string]int{{"Hour": 9}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LaunchdCalendars() = %#v, want %#v", got, want)
	}
}

func TestSystemdCalendars(t *testing.T) {
	tests := []struct {
		expr string
		want []string
	}{
		{"0 9 * * *", []string{"*-*-* 09:00:00"}},
		{"30 10 * * 1", []string{"Mon *-*-* 10:30:00"}},
		{"0 0 1 6 *", []string{"*-06-01 00:00:00"}},
		{"* * * * *", []string{"*-*-* *:*:00"}},
		{"0 9 1 * 1", []string{"*-*-01 09:00:00", "Mon *-*-* 09:00:00"}},
	}
	for _, test := range tests {
		t.Run(test.expr, func(t *testing.T) {
			spec, err := ParseCron(test.expr)
			if err != nil {
				t.Fatal(err)
			}
			got := spec.SystemdCalendars()
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("SystemdCalendars(%q) = %#v, want %#v", test.expr, got, test.want)
			}
		})
	}
}

func TestDescribeCron(t *testing.T) {
	tests := map[string]string{
		"0 9 * * *":    "daily at 09:00",
		"*/15 * * * *": "every 15 minutes",
		"0 */6 * * *":  "every 6 hours",
		"30 8 * * 1":   "weekly on Mon at 08:30",
		"0 9,17 * * *": "custom schedule",
	}
	for expr, want := range tests {
		spec, err := ParseCron(expr)
		if err != nil {
			t.Fatal(err)
		}
		if got := spec.Describe(); got != want {
			t.Fatalf("Describe(%q) = %q, want %q", expr, got, want)
		}
	}
}
