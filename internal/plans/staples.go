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
// on one of its dates (see IsOn), and not skipped.
func (s *Staple) OccursOn(date time.Time) bool {
	return s.IsOn(date) && !s.SkippedOn(date)
}

// IsOn reports whether a date at midnight UTC is one of the staple's,
// skipped or not: within its dates, on one of its weekdays.
func (s *Staple) IsOn(date time.Time) bool {
	switch {
	case !s.StartDate.IsZero() && date.Before(s.StartDate):
		return false
	case !s.EndDate.IsZero() && date.After(s.EndDate):
		return false
	case len(s.Weekdays) > 0 && !slices.Contains(s.Weekdays, date.Weekday()):
		return false
	}
	return true
}

// SkippedOn reports whether the staple is skipped on a date at midnight
// UTC.
func (s *Staple) SkippedOn(date time.Time) bool {
	return slices.ContainsFunc(s.SkippedDates, date.Equal)
}

// EatenOn reports whether the staple was marked eaten on a date at
// midnight UTC, which took its food out of the pantry.
func (s *Staple) EatenOn(date time.Time) bool {
	return slices.ContainsFunc(s.EatenDates, date.Equal)
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
