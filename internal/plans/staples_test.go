package plans

import (
	"slices"
	"testing"
	"time"
)

func TestExpandStaples(t *testing.T) {
	t.Parallel()
	day := func(d int) time.Time { return time.Date(2026, 11, d, 0, 0, 0, 0, time.UTC) }
	// The week from Monday 2 November 2026.
	monday, sunday := day(2), day(8)
	daily := &Staple{Name: "Soy milk"}
	weekdays := &Staple{Name: "Oats", Weekdays: []time.Weekday{time.Monday, time.Wednesday, time.Friday}}
	skipped := &Staple{Name: "Nuts", SkippedDates: []time.Time{day(3), day(4)}}
	bounded := &Staple{Name: "Soup", StartDate: day(6), EndDate: day(7)}
	ended := &Staple{Name: "Juice", EndDate: day(1)}
	got := ExpandStaples([]*Staple{daily, weekdays, skipped, bounded, ended}, monday, sunday)
	counts := map[*Staple][]int{}
	for _, o := range got {
		counts[o.Staple] = append(counts[o.Staple], o.Date.Day())
	}
	for _, tt := range []struct {
		staple *Staple
		want   []int
	}{
		{daily, []int{2, 3, 4, 5, 6, 7, 8}},
		{weekdays, []int{2, 4, 6}},
		{skipped, []int{2, 5, 6, 7, 8}},
		{bounded, []int{6, 7}},
		{ended, nil},
	} {
		if g := counts[tt.staple]; !slices.Equal(g, tt.want) {
			t.Errorf("%s is on %v, want %v", tt.staple.Name, g, tt.want)
		}
	}
	// Occurrences are by date, then in the staples' order.
	if got[0].Staple != daily || got[1].Staple != weekdays || got[2].Staple != skipped || !got[3].Date.Equal(day(3)) {
		t.Errorf("got occurrences out of order: %v", got[:4])
	}
}
