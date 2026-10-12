package schedule

import (
	"testing"
	"time"
)

func at(value string) time.Time {
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return t
}
func stamp(value string) *time.Time { t := at(value); return &t }
func TestWallClockWindows(t *testing.T) {
	for _, tc := range []struct {
		name, zone, start, end, now string
		days                        []int
		want                        bool
	}{
		{"start inclusive", "UTC", "09:00", "10:00", "2026-10-12T09:00:00Z", []int{1}, true},
		{"end exclusive", "UTC", "09:00", "10:00", "2026-10-12T10:00:00Z", []int{1}, false},
		{"overnight previous day", "Europe/Berlin", "23:00", "02:00", "2026-10-13T23:30:00Z", []int{2}, true},
		{"overnight end", "Europe/Berlin", "23:00", "02:00", "2026-10-14T00:00:00Z", []int{2}, false},
		{"whole local day", "Pacific/Auckland", "00:00", "00:00", "2026-10-11T11:00:00Z", []int{1}, true},
		{"spring skips nonexistent minute", "Europe/Berlin", "02:15", "02:45", "2026-03-29T01:20:00Z", []int{0}, false},
		{"spring spanning jump", "Europe/Berlin", "01:30", "03:30", "2026-03-29T01:20:00Z", []int{0}, true},
		{"fall first occurrence", "Europe/Berlin", "02:15", "02:45", "2026-10-25T00:20:00Z", []int{0}, true},
		{"fall second occurrence", "Europe/Berlin", "02:15", "02:45", "2026-10-25T01:20:00Z", []int{0}, true},
		{"US fall occurrence", "America/New_York", "01:00", "02:00", "2026-11-01T06:30:00Z", []int{0}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := Window{TimeZone: tc.zone, Days: tc.days, Start: tc.start, End: tc.end}
			if err := w.Validate(); err != nil {
				t.Fatal(err)
			}
			if got := w.Contains(at(tc.now)); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}
func TestEffectiveSchedule(t *testing.T) {
	w := func(start, end, zone string) Window {
		return Window{TimeZone: zone, Days: []int{1}, Start: start, End: end}
	}
	for _, tc := range []struct {
		name, now, next string
		policies        []ScopedPolicy
		allowed         bool
		reasons         int
	}{
		{name: "unrestricted", now: "2026-10-12T08:00:00Z", allowed: true},
		{name: "overlapping windows union", now: "2026-10-12T10:30:00Z", policies: []ScopedPolicy{{Scope: "Workspace", Policy: Policy{Windows: []Window{w("09:00", "11:00", "UTC"), w("10:00", "12:00", "UTC")}}}}, allowed: true},
		{name: "scope intersection and zones", now: "2026-10-12T07:00:00Z", next: "2026-10-12T09:00:00Z", reasons: 2, policies: []ScopedPolicy{{Scope: "Workspace", Policy: Policy{Windows: []Window{w("09:00", "12:00", "UTC")}}}, {Scope: "Application", Policy: Policy{Windows: []Window{w("10:00", "12:00", "Europe/Berlin")}}}}},
		{name: "nonoverlapping scopes", now: "2026-10-12T09:30:00Z", reasons: 1, policies: []ScopedPolicy{{Scope: "Workspace", Policy: Policy{Windows: []Window{w("09:00", "10:00", "UTC")}}}, {Scope: "Application", Policy: Policy{Windows: []Window{w("10:00", "11:00", "UTC")}}}}},
		{name: "indefinite freeze", now: "2026-10-12T09:30:00Z", reasons: 1, policies: []ScopedPolicy{{Scope: "Workspace", Policy: Policy{Freezes: []Freeze{{Reason: "Incident"}}}}}},
		{name: "freeze end exact seconds", now: "2026-10-12T09:30:00Z", next: "2026-10-12T10:00:15Z", reasons: 1, policies: []ScopedPolicy{{Scope: "Workspace", Policy: Policy{Freezes: []Freeze{{Reason: "Release", Start: stamp("2026-10-12T09:00:00Z"), End: stamp("2026-10-12T10:00:15Z")}}}}}},
		{name: "long freeze jumps to next local window", now: "2026-10-12T09:30:00Z", next: "2026-11-02T08:00:00Z", reasons: 1, policies: []ScopedPolicy{{Scope: "Workspace", Policy: Policy{Windows: []Window{w("09:00", "12:00", "Europe/Berlin")}, Freezes: []Freeze{{Reason: "Release", End: stamp("2026-11-01T00:00:00Z")}}}}}},
		{name: "recurring freeze wins", now: "2026-10-12T09:30:00Z", next: "2026-10-12T10:00:00Z", reasons: 1, policies: []ScopedPolicy{{Scope: "Application", Policy: Policy{Windows: []Window{w("09:00", "11:00", "UTC")}, Freezes: []Freeze{{Reason: "Backup", Schedule: func() *Window { v := w("09:00", "10:00", "UTC"); return &v }()}}}}}},
		{name: "spring missing opening next week", now: "2026-03-29T00:00:00Z", next: "2026-04-05T00:15:00Z", reasons: 1, policies: []ScopedPolicy{{Scope: "Workspace", Policy: Policy{Windows: []Window{{TimeZone: "Europe/Berlin", Days: []int{0}, Start: "02:15", End: "02:45"}}}}}},
		{name: "fall second opening", now: "2026-10-25T00:50:00Z", next: "2026-10-25T01:15:00Z", reasons: 1, policies: []ScopedPolicy{{Scope: "Workspace", Policy: Policy{Windows: []Window{{TimeZone: "Europe/Berlin", Days: []int{0}, Start: "02:15", End: "02:45"}}}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := Evaluate(tc.policies, at(tc.now))
			if d.Allowed != tc.allowed || len(d.Reasons) != tc.reasons {
				t.Fatalf("unexpected decision %+v", d)
			}
			if tc.next == "" {
				if d.NextAllowedAt != nil {
					t.Fatalf("unexpected next %v", d.NextAllowedAt)
				}
			} else if d.NextAllowedAt == nil || !d.NextAllowedAt.Equal(at(tc.next)) {
				t.Fatalf("got next %v want %s", d.NextAllowedAt, tc.next)
			}
		})
	}
}
func TestPolicyValidation(t *testing.T) {
	valid := Window{TimeZone: "UTC", Days: []int{1}, Start: "09:00", End: "10:00"}
	for _, change := range []func(*Window){func(w *Window) { w.TimeZone = "" }, func(w *Window) { w.TimeZone = "Local" }, func(w *Window) { w.TimeZone = "Invalid/Zone" }, func(w *Window) { w.Days = nil }, func(w *Window) { w.Days = []int{1, 1} }, func(w *Window) { w.Days = []int{7} }, func(w *Window) { w.Start = "9:00" }, func(w *Window) { w.End = "24:00" }} {
		w := valid
		change(&w)
		if w.Validate() == nil {
			t.Fatalf("accepted invalid %+v", w)
		}
	}
	for _, f := range []Freeze{{Reason: " "}, {Reason: "bad range", Start: stamp("2026-01-02T00:00:00Z"), End: stamp("2026-01-01T00:00:00Z")}, {Reason: "mixed", Start: stamp("2026-01-01T00:00:00Z"), Schedule: &valid}} {
		if (Policy{Freezes: []Freeze{f}}).Validate() == nil {
			t.Fatalf("accepted invalid %+v", f)
		}
	}
}
