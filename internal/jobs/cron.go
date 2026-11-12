package jobs

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// CronSpec is a parsed five-field cron expression. A nil field means wildcard.
type CronSpec struct {
	Minute     []int
	Hour       []int
	DayOfMonth []int
	Month      []int
	DayOfWeek  []int
}

var cronMacros = map[string]string{
	"@yearly":   "0 0 1 1 *",
	"@annually": "0 0 1 1 *",
	"@monthly":  "0 0 1 * *",
	"@weekly":   "0 0 * * 0",
	"@daily":    "0 0 * * *",
	"@midnight": "0 0 * * *",
	"@hourly":   "0 * * * *",
}

var weekdayNames = []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}

// ParseCron parses a standard five-field cron expression or one of the
// @hourly, @daily, @midnight, @weekly, @monthly, @yearly, @annually macros.
func ParseCron(expression string) (*CronSpec, error) {
	expr := strings.TrimSpace(expression)
	if expr == "" {
		return nil, fmt.Errorf("cron expression is empty")
	}
	if replacement, ok := cronMacros[strings.ToLower(expr)]; ok {
		expr = replacement
	}
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return nil, fmt.Errorf("invalid cron %q: expected 5 fields (minute hour day-of-month month day-of-week)", expression)
	}

	minute, err := parseCronField(fields[0], 0, 59)
	if err != nil {
		return nil, fmt.Errorf("invalid cron minute %q: %w", fields[0], err)
	}
	hour, err := parseCronField(fields[1], 0, 23)
	if err != nil {
		return nil, fmt.Errorf("invalid cron hour %q: %w", fields[1], err)
	}
	dayOfMonth, err := parseCronField(fields[2], 1, 31)
	if err != nil {
		return nil, fmt.Errorf("invalid cron day-of-month %q: %w", fields[2], err)
	}
	month, err := parseCronField(fields[3], 1, 12)
	if err != nil {
		return nil, fmt.Errorf("invalid cron month %q: %w", fields[3], err)
	}
	dayOfWeek, err := parseCronField(fields[4], 0, 7)
	if err != nil {
		return nil, fmt.Errorf("invalid cron day-of-week %q: %w", fields[4], err)
	}
	if dayOfWeek != nil {
		dayOfWeek = normalizeWeekdays(dayOfWeek)
		if len(dayOfWeek) == 7 {
			dayOfWeek = nil
		}
	}

	return &CronSpec{
		Minute:     minute,
		Hour:       hour,
		DayOfMonth: dayOfMonth,
		Month:      month,
		DayOfWeek:  dayOfWeek,
	}, nil
}

func parseCronField(field string, minValue, maxValue int) ([]int, error) {
	field = strings.TrimSpace(field)
	if field == "" {
		return nil, fmt.Errorf("field is empty")
	}
	if field == "*" {
		return nil, nil
	}

	seen := make(map[int]struct{})
	for _, part := range strings.Split(field, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("empty list item")
		}

		step := 1
		base := part
		hasStep := false
		if strings.Contains(part, "/") {
			chunks := strings.Split(part, "/")
			if len(chunks) != 2 || chunks[1] == "" {
				return nil, fmt.Errorf("invalid step %q", part)
			}
			base = chunks[0]
			parsed, err := strconv.Atoi(chunks[1])
			if err != nil || parsed <= 0 {
				return nil, fmt.Errorf("invalid step %q", part)
			}
			step = parsed
			hasStep = true
		}

		start, end := 0, 0
		switch {
		case base == "*":
			start, end = minValue, maxValue
		case strings.Contains(base, "-"):
			chunks := strings.Split(base, "-")
			if len(chunks) != 2 {
				return nil, fmt.Errorf("invalid range %q", base)
			}
			var err error
			start, err = strconv.Atoi(chunks[0])
			if err != nil {
				return nil, fmt.Errorf("invalid range %q", base)
			}
			end, err = strconv.Atoi(chunks[1])
			if err != nil {
				return nil, fmt.Errorf("invalid range %q", base)
			}
			if start > end {
				return nil, fmt.Errorf("range start is after end: %q", base)
			}
		default:
			value, err := strconv.Atoi(base)
			if err != nil {
				return nil, fmt.Errorf("invalid value %q", base)
			}
			start = value
			end = value
			if hasStep {
				end = maxValue
			}
		}

		if start < minValue || start > maxValue || end < minValue || end > maxValue {
			return nil, fmt.Errorf("value out of range %d-%d: %q", minValue, maxValue, part)
		}
		for value := start; value <= end; value += step {
			seen[value] = struct{}{}
		}
	}

	values := make([]int, 0, len(seen))
	for value := range seen {
		values = append(values, value)
	}
	sort.Ints(values)
	if len(values) == maxValue-minValue+1 {
		return nil, nil
	}
	return values, nil
}

func normalizeWeekdays(values []int) []int {
	seen := make(map[int]struct{}, len(values))
	for _, value := range values {
		if value == 7 {
			value = 0
		}
		seen[value] = struct{}{}
	}
	out := make([]int, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	sort.Ints(out)
	return out
}

type calendarField struct {
	key    string
	values []int
}

func expandCalendars(fields ...calendarField) []map[string]int {
	sets := []map[string]int{{}}
	for _, field := range fields {
		if field.values == nil {
			continue
		}
		next := make([]map[string]int, 0, len(sets)*len(field.values))
		for _, set := range sets {
			for _, value := range field.values {
				clone := make(map[string]int, len(set)+1)
				for key, existing := range set {
					clone[key] = existing
				}
				clone[field.key] = value
				next = append(next, clone)
			}
		}
		sets = next
	}
	return sets
}

// LaunchdCalendars converts the expression into launchd StartCalendarInterval
// dictionaries. launchd treats omitted keys as wildcards.
func (c *CronSpec) LaunchdCalendars() []map[string]int {
	common := []calendarField{
		{key: "Minute", values: c.Minute},
		{key: "Hour", values: c.Hour},
		{key: "Month", values: c.Month},
	}
	if c.DayOfMonth != nil && c.DayOfWeek != nil {
		// Cron ORs day-of-month and day-of-week when both are restricted.
		byDay := expandCalendars(append(common, calendarField{key: "Day", values: c.DayOfMonth})...)
		byWeekday := expandCalendars(append(common, calendarField{key: "Weekday", values: c.DayOfWeek})...)
		return append(byDay, byWeekday...)
	}
	return expandCalendars(append(common,
		calendarField{key: "Day", values: c.DayOfMonth},
		calendarField{key: "Weekday", values: c.DayOfWeek},
	)...)
}

// SystemdCalendars converts the expression into systemd OnCalendar values.
func (c *CronSpec) SystemdCalendars() []string {
	minutes := intStrings(c.Minute, 2)
	hours := intStrings(c.Hour, 2)
	days := intStrings(c.DayOfMonth, 2)
	months := intStrings(c.Month, 2)
	weekdays := weekdayStrings(c.DayOfWeek)

	var calendars []string
	build := func(dayValues, weekdays []string) {
		for _, minute := range minutes {
			for _, hour := range hours {
				for _, day := range dayValues {
					for _, month := range months {
						for _, weekday := range weekdays {
							prefix := ""
							if weekday != "*" {
								prefix = weekday + " "
							}
							calendars = append(calendars,
								fmt.Sprintf("%s*-%s-%s %s:%s:00", prefix, month, day, hour, minute))
						}
					}
				}
			}
		}
	}

	if c.DayOfMonth != nil && c.DayOfWeek != nil {
		build(days, []string{"*"})
		build([]string{"*"}, weekdays)
	} else {
		build(days, weekdays)
	}
	return calendars
}

func intStrings(values []int, width int) []string {
	if values == nil {
		return []string{"*"}
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, fmt.Sprintf("%0*d", width, value))
	}
	return out
}

func weekdayStrings(values []int) []string {
	if values == nil {
		return []string{"*"}
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value < 0 || value >= len(weekdayNames) {
			out = append(out, "*")
			continue
		}
		out = append(out, weekdayNames[value])
	}
	return out
}

// Describe returns a short human-readable summary for common schedules.
func (c *CronSpec) Describe() string {
	if len(c.Minute) == 1 && len(c.Hour) == 1 &&
		c.DayOfMonth == nil && c.Month == nil && c.DayOfWeek == nil {
		return fmt.Sprintf("daily at %02d:%02d", c.Hour[0], c.Minute[0])
	}
	if c.Hour == nil && c.DayOfMonth == nil && c.Month == nil && c.DayOfWeek == nil &&
		len(c.Minute) > 1 {
		if step, ok := uniformStep(c.Minute); ok {
			return fmt.Sprintf("every %d minutes", step)
		}
	}
	if c.Minute != nil && len(c.Minute) == 1 && c.Minute[0] == 0 &&
		c.Hour == nil && c.DayOfMonth == nil && c.Month == nil && c.DayOfWeek == nil {
		return "hourly"
	}
	if len(c.Hour) > 1 && c.Minute != nil && len(c.Minute) == 1 &&
		c.DayOfMonth == nil && c.Month == nil && c.DayOfWeek == nil {
		if step, ok := uniformStep(c.Hour); ok {
			return fmt.Sprintf("every %d hours", step)
		}
	}
	if len(c.Hour) == 1 && len(c.Minute) == 1 && len(c.DayOfWeek) == 1 &&
		c.DayOfMonth == nil && c.Month == nil {
		return fmt.Sprintf("weekly on %s at %02d:%02d",
			weekdayNames[c.DayOfWeek[0]], c.Hour[0], c.Minute[0])
	}
	return "custom schedule"
}

func uniformStep(values []int) (int, bool) {
	if len(values) < 2 || values[0] != 0 {
		return 0, false
	}
	step := values[1] - values[0]
	if step <= 0 {
		return 0, false
	}
	for i := 2; i < len(values); i++ {
		if values[i]-values[i-1] != step {
			return 0, false
		}
	}
	return step, true
}
