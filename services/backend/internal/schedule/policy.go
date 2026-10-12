// Package schedule evaluates deployment restrictions in local wall-clock time.
package schedule

import (
	"fmt"
	"strings"
	"time"
	_ "time/tzdata" // Keep IANA zones available in minimal container images.
)

type Window struct {
	TimeZone string `json:"timeZone"`
	Days     []int  `json:"days"` // Sunday=0, Saturday=6; days refer to the start day.
	Start    string `json:"start"`
	End      string `json:"end"`
}
type Freeze struct {
	Reason   string     `json:"reason"`
	Start    *time.Time `json:"start,omitempty"`
	End      *time.Time `json:"end,omitempty"`
	Schedule *Window    `json:"schedule,omitempty"`
}
type Policy struct {
	Windows []Window `json:"windows"`
	Freezes []Freeze `json:"freezes"`
}
type Decision struct {
	Allowed       bool       `json:"allowed"`
	Reasons       []string   `json:"reasons"`
	NextAllowedAt *time.Time `json:"nextAllowedAt,omitempty"`
}

func minute(value string) (int, error) {
	t, err := time.Parse("15:04", value)
	if err != nil || t.Format("15:04") != value {
		return 0, fmt.Errorf("time must use HH:MM")
	}
	return t.Hour()*60 + t.Minute(), nil
}
func (w Window) Validate() error {
	if w.TimeZone == "" || w.TimeZone == "Local" {
		return fmt.Errorf("an explicit IANA timeZone is required")
	}
	if _, err := time.LoadLocation(w.TimeZone); err != nil {
		return fmt.Errorf("unknown timeZone %q", w.TimeZone)
	}
	if len(w.Days) == 0 || len(w.Days) > 7 {
		return fmt.Errorf("days must contain weekdays 0 through 6")
	}
	seen := map[int]bool{}
	for _, d := range w.Days {
		if d < 0 || d > 6 || seen[d] {
			return fmt.Errorf("days must be unique weekdays 0 through 6")
		}
		seen[d] = true
	}
	if _, err := minute(w.Start); err != nil {
		return err
	}
	if _, err := minute(w.End); err != nil {
		return err
	}
	return nil
}
func (p Policy) Validate() error {
	if len(p.Windows) > 16 || len(p.Freezes) > 16 {
		return fmt.Errorf("at most 16 windows and 16 freezes are allowed per scope")
	}
	for _, w := range p.Windows {
		if err := w.Validate(); err != nil {
			return err
		}
	}
	for _, f := range p.Freezes {
		if strings.TrimSpace(f.Reason) == "" || len(f.Reason) > 1000 {
			return fmt.Errorf("freeze reason is required (at most 1000 characters)")
		}
		if f.Schedule != nil {
			if f.Start != nil || f.End != nil {
				return fmt.Errorf("recurring freezes cannot include start or end timestamps")
			}
			if err := f.Schedule.Validate(); err != nil {
				return err
			}
		} else {
			if f.End != nil && f.Start != nil && !f.End.After(*f.Start) {
				return fmt.Errorf("freeze end must be after start")
			}
		}
	}
	return nil
}

// Contains compares wall-clock labels, rather than adding 24-hour durations.
// Both occurrences of a repeated fall-back minute match; nonexistent spring
// minutes never match. Equal start/end denotes the entire local start day.
func (w Window) Contains(at time.Time) bool {
	loc, err := time.LoadLocation(w.TimeZone)
	if err != nil {
		return false
	}
	return w.contains(at, loc)
}
func (w Window) contains(at time.Time, loc *time.Location) bool {
	if loc == nil {
		return false
	}
	local := at.In(loc)
	m := local.Hour()*60 + local.Minute()
	start, _ := minute(w.Start)
	end, _ := minute(w.End)
	day := int(local.Weekday())
	if start == end {
		for _, d := range w.Days {
			if day == d {
				return true
			}
		}
		return false
	}
	if start > end {
		if m < end {
			day = (day + 6) % 7
		} else if m < start {
			return false
		}
	} else if m < start || m >= end {
		return false
	}
	for _, d := range w.Days {
		if d == day {
			return true
		}
	}
	return false
}

type ScopedPolicy struct {
	Scope  string
	Policy Policy
}

func state(policies []ScopedPolicy, at time.Time, locations map[string]*time.Location) ([]string, *time.Time, bool) {
	reasons := []string{}
	var jump *time.Time
	permanent := false
	for _, scoped := range policies {
		p := scoped.Policy
		if len(p.Windows) > 0 {
			inside := false
			for _, w := range p.Windows {
				inside = inside || w.contains(at, locations[w.TimeZone])
			}
			if !inside {
				reasons = append(reasons, scoped.Scope+": outside maintenance windows")
			}
		}
		for _, f := range p.Freezes {
			active := false
			if f.Schedule != nil {
				active = f.Schedule.contains(at, locations[f.Schedule.TimeZone])
			} else {
				active = (f.Start == nil || !at.Before(*f.Start)) && (f.End == nil || at.Before(*f.End))
			}
			if !active {
				continue
			}
			reasons = append(reasons, scoped.Scope+": deployment freeze — "+f.Reason)
			if f.Schedule == nil {
				if f.End == nil {
					permanent = true
				} else if jump == nil || f.End.After(*jump) {
					v := f.End.UTC()
					jump = &v
				}
			}
		}
	}
	return reasons, jump, permanent
}

// Evaluate unions windows within a scope and intersects restrictions across
// scopes. Freeze precedence is absolute. Search one full weekly cycle after
// finite freezes, jumping over long freezes without a duration-based DST guess.
func Evaluate(policies []ScopedPolicy, now time.Time) Decision {
	locations := map[string]*time.Location{}
	for _, scoped := range policies {
		for _, w := range scoped.Policy.Windows {
			if _, ok := locations[w.TimeZone]; !ok {
				locations[w.TimeZone], _ = time.LoadLocation(w.TimeZone)
			}
		}
		for _, f := range scoped.Policy.Freezes {
			if f.Schedule != nil {
				zone := f.Schedule.TimeZone
				if _, ok := locations[zone]; !ok {
					locations[zone], _ = time.LoadLocation(zone)
				}
			}
		}
	}
	reasons, _, permanent := state(policies, now, locations)
	result := Decision{Allowed: len(reasons) == 0, Reasons: reasons}
	if result.Allowed || permanent {
		return result
	}
	candidate := now.UTC()
	// Different IANA zones can change their relative offsets during the next
	// week. Eight days includes a complete weekly cycle across DST transitions.
	deadline := candidate.Add(8 * 24 * time.Hour)
	for steps := 0; steps < 12000; steps++ {
		blocked, jump, forever := state(policies, candidate, locations)
		if len(blocked) == 0 {
			result.NextAllowedAt = &candidate
			return result
		}
		if forever {
			return result
		}
		if jump != nil {
			candidate = *jump
			deadline = candidate.Add(8 * 24 * time.Hour)
			continue
		}
		candidate = candidate.Truncate(time.Minute).Add(time.Minute)
		if candidate.After(deadline) {
			break
		}
	}
	return result
}
