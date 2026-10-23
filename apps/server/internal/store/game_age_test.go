package store

import (
	"testing"
	"time"
)

func TestAgeOfDaysAcrossTheYearBoundary(t *testing.T) {
	tests := []struct {
		name string
		dob  string
		now  time.Time
		want int
	}{
		{"late December birthday, 18 on the day", "2008-12-31", time.Date(2026, 12, 31, 12, 0, 0, 0, time.UTC), 18},
		{"late December birthday, 17 the day before", "2008-12-31", time.Date(2026, 12, 30, 12, 0, 0, 0, time.UTC), 17},
		{"early January birthday, 18 on the day", "2008-01-01", time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC), 18},
		{"early January birthday, 17 the day before", "2008-01-01", time.Date(2025, 12, 31, 12, 0, 0, 0, time.UTC), 17},
		{"mid-year birthday, past", "2000-06-15", time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC), 26},
		{"invalid date", "not-a-date", time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC), 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := AgeOfDays(tc.dob, tc.now); got != tc.want {
				t.Fatalf("AgeOfDays(%q, %v) = %d, want %d", tc.dob, tc.now, got, tc.want)
			}
		})
	}
}
