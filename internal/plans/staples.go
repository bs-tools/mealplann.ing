package plans

import (
	"slices"
	"time"
)

// Occurrence is a staple on a date.
type Occurrence struct {
	Staple *Staple
	// Date is at midnight UTC.
	Date time.Time
}

// OccursOn reports whether the staple is eaten on a date at midnight UTC:
// within its dates, on one of its weekdays, and not skipped.
func (s *Staple) OccursOn(date time.Time) bool {
	switch {
	case !s.StartDate.IsZero() && date.Before(s.StartDate):
		return false
	case !s.EndDate.IsZero() && date.After(s.EndDate):
		return false
	case len(s.Weekdays) > 0 && !slices.Contains(s.Weekdays, date.Weekday()):
		return false
	}
	return !slices.ContainsFunc(s.SkippedDates, date.Equal)
}

// ExpandStaples returns the staples on each date from one date to another,
// inclusive, at midnight UTC, by date and then in the staples' order.
// Staples aren't stored as portions, so that changing one changes every
// week: everything that reads a plan expands them here.
func ExpandStaples(staples []*Staple, from, to time.Time) []Occurrence {
	var occurrences []Occurrence
	for date := from; !date.After(to); date = date.AddDate(0, 0, 1) {
		for _, s := range staples {
			if s.OccursOn(date) {
				occurrences = append(occurrences, Occurrence{Staple: s, Date: date})
			}
		}
	}
	return occurrences
}
