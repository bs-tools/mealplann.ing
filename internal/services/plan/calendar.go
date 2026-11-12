package plan

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/bs-tools/mealplann.ing/internal/authn"
	peoplev1 "github.com/bs-tools/mealplann.ing/internal/gen/people/v1"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	"github.com/bs-tools/mealplann.ing/internal/people"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/bs-tools/mealplann.ing/internal/users"
	"github.com/jackc/pgx/v5/pgtype"
)

// calendarWeeks is how many weeks a calendar feed has, from this one: what
// a household plans ahead, read in a request per week.
const calendarWeeks = 3

// minCookMinutes is the least time a cook is given in a calendar, like
// Today: a recipe of unknown time still takes a while.
const minCookMinutes = 15

// CalendarFeedUsers finds the user of a calendar feed by its secret.
type CalendarFeedUsers interface {
	GetCalendarFeedUser(ctx context.Context, secret string) (users.User, error)
}

// CalendarHandler serves calendar feeds at prefix followed by a feed's
// secret and ".ics": an iCalendar of the cooks, the prep ahead and the
// meals of the person the feed's user is, for a calendar app to subscribe
// to. Times are floating, in the calendar's own time zone, as the plan's
// are local times.
func (s *Service) CalendarHandler(prefix string, feeds CalendarFeedUsers) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secret, ok := strings.CutSuffix(strings.TrimPrefix(r.URL.Path, prefix), ".ics")
		if !ok || secret == "" || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
			http.NotFound(w, r)
			return
		}
		user, err := feeds.GetCalendarFeedUser(r.Context(), secret)
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "This calendar feed doesn't exist: make a new one in mealplann.ing's settings.", http.StatusNotFound)
			return
		}
		if err != nil {
			s.report.Report(r.Context(), "failed to read calendar feed", err)
			http.Error(w, "failed to read calendar feed", http.StatusInternalServerError)
			return
		}
		cal, err := s.calendar(authn.WithUser(r.Context(), user), user)
		if err != nil {
			s.report.Report(r.Context(), "failed to make calendar", err)
			http.Error(w, "failed to make calendar", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
		// Calendar apps poll; the plan changes only so often.
		w.Header().Set("Cache-Control", "private, max-age=900")
		_, _ = w.Write([]byte(cal))
	})
}

// calendar makes the iCalendar of a user's person's weeks.
func (s *Service) calendar(ctx context.Context, user users.User) (string, error) {
	everyone, err := s.people.ListPeople(ctx, user.HouseholdID())
	if err != nil {
		return "", err
	}
	var person *people.Person
	for _, p := range everyone {
		if p.UserID == user.ID() {
			person = p
		}
	}
	now := s.nowFn().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	monday := today.AddDate(0, 0, -int(today.Weekday()+6)%7)
	weeks := make([]*plansv1.GetWeekResponse, 0, calendarWeeks)
	for i := range calendarWeeks {
		week, err := s.GetWeek(ctx, plansv1.GetWeekRequest_builder{
			Monday: store.FormatDate(monday.AddDate(0, 0, 7*i)),
		}.Build())
		if err != nil {
			return "", err
		}
		weeks = append(weeks, week)
	}
	var recipeIDs []pgtype.UUID
	for _, week := range weeks {
		for _, c := range week.GetCooks() {
			if id, err := store.StringToUUID(c.GetCook().GetRecipeId()); err == nil {
				recipeIDs = append(recipeIDs, id)
			}
		}
	}
	minutes := map[string]int{}
	recipeList, err := s.recipes.ListRecipesByID(ctx, user.HouseholdID(), unique(recipeIDs))
	if err != nil {
		return "", err
	}
	for _, r := range recipeList {
		minutes[store.UUIDToString(r.ID)] = int(r.PrepSeconds+r.CookSeconds) / 60
	}
	name := "mealplann.ing"
	if person != nil {
		name += " · " + person.Name
	}
	c := &icsCalendar{name: name, stamp: now}
	for _, week := range weeks {
		calendarWeek(c, week, person, minutes)
	}
	return c.String(), nil
}

// calendarWeek adds a week's events: the person's meals, the cooks they
// eat from, at the time to start them, and the prep ahead for those cooks,
// or, for a user who isn't one of the household's people, every cook.
func calendarWeek(c *icsCalendar, week *plansv1.GetWeekResponse, person *people.Person, minutes map[string]int) {
	personID := ""
	if person != nil {
		personID = store.UUIDToString(person.ID)
	}
	// The cooks the person eats from, and the eating time of theirs each
	// is first eaten at on its date.
	mine := map[string]*plansv1.PlannedEatingTime{}
	eats := map[string]bool{}
	for _, day := range week.GetDays() {
		for _, pd := range day.GetPeople() {
			if pd.GetPersonId() != personID {
				continue
			}
			for _, et := range pd.GetEatingTimes() {
				for _, p := range et.GetPortions() {
					if p.GetCookId() == "" {
						continue
					}
					eats[p.GetCookId()] = true
					if p.GetCookDate() == day.GetDate() && mine[p.GetCookId()] == nil {
						mine[p.GetCookId()] = et
					}
				}
				calendarMeal(c, day.GetDate(), et)
			}
		}
	}
	for _, pc := range week.GetCooks() {
		cook := pc.GetCook()
		if person != nil && !eats[cook.GetId()] {
			continue
		}
		calendarCook(c, cook, mine[cook.GetId()], person, minutes[cook.GetRecipeId()])
	}
	for _, day := range week.GetDays() {
		for _, task := range day.GetPrepTasks() {
			if person != nil && !eats[task.GetCookId()] {
				continue
			}
			description := ""
			if names := task.GetForRecipeNames(); len(names) > 0 {
				description = "For " + strings.Join(names, ", ")
			} else {
				description = fmt.Sprintf("For %s on %s", task.GetRecipeName(), weekdayOf(task.GetCookDate()))
			}
			c.add(icsEvent{
				uid:         fmt.Sprintf("prep-%s-%s", task.GetCookId(), day.GetDate()),
				date:        day.GetDate(),
				summary:     task.GetNote(),
				description: description,
			})
		}
	}
}

// calendarMeal adds a meal: what's planned at an eating time, or that
// it's eaten out.
func calendarMeal(c *icsCalendar, date string, et *plansv1.PlannedEatingTime) {
	if !et.GetEatingOut() && len(et.GetPortions()) == 0 {
		return
	}
	var names, lines []string
	for _, p := range et.GetPortions() {
		names = append(names, p.GetName())
		if p.GetGrams() > 0 {
			lines = append(lines, fmt.Sprintf("%s, %.0f g", p.GetName(), p.GetGrams()))
		} else {
			lines = append(lines, p.GetName())
		}
	}
	eatingTime := et.GetEatingTime()
	summary := eatingTime.GetName() + ": " + strings.Join(names, ", ")
	if et.GetEatingOut() {
		summary = eatingTime.GetName() + ": eating out"
		lines = []string{et.GetEatingOutNotes()}
	}
	duration := 30
	if eatingTime.GetKind() == peoplev1.EatingTimeKind_EATING_TIME_KIND_SMALL {
		duration = 15
	}
	c.add(icsEvent{
		uid:         fmt.Sprintf("meal-%s-%s", eatingTime.GetId(), date),
		date:        date,
		at:          eatingTime.GetTime(),
		minutes:     duration,
		summary:     summary,
		description: strings.TrimSpace(strings.Join(lines, "\n")),
	})
}

// calendarCook adds a cook, ending when its meal is eaten: at its shared
// meal's time, or at the first of the person's eating times that day that
// eats from it, and starting its recipe's time before. A cook made ahead,
// whose meal is another day, is a day's event.
func calendarCook(c *icsCalendar, cook *plansv1.Cook, first *plansv1.PlannedEatingTime, person *people.Person, minutes int) {
	at := ""
	if person != nil && cook.GetMeal() != peoplev1.HouseholdMeal_HOUSEHOLD_MEAL_UNSPECIFIED {
		for _, et := range person.EatingTimes {
			if string(et.Meal) == mealName(cook.GetMeal()) {
				at = et.TimeOfDay
			}
		}
	}
	if at == "" && first != nil {
		at = first.GetEatingTime().GetTime()
	}
	minutes = max(minutes, minCookMinutes)
	start := ""
	if end, ok := clockMinutes(at); ok {
		start = fmt.Sprintf("%02d:%02d", max(end-minutes, 0)/60, max(end-minutes, 0)%60)
	}
	summary := fmt.Sprintf("Cook %s", cook.GetRecipeName())
	if cook.GetServings() != 1 {
		summary += fmt.Sprintf(" (%s servings)", formatNumber(cook.GetServings()))
	}
	description := cook.GetNotes()
	c.add(icsEvent{
		uid:         "cook-" + cook.GetId(),
		date:        cook.GetDate(),
		at:          start,
		minutes:     minutes,
		summary:     summary,
		description: description,
	})
}

// mealName returns the name of a shared meal as people.Meal stores it,
// like "dinner".
func mealName(m peoplev1.HouseholdMeal) string {
	return strings.ToLower(strings.TrimPrefix(m.String(), "HOUSEHOLD_MEAL_"))
}

// clockMinutes returns the minutes since midnight of "HH:MM".
func clockMinutes(clock string) (int, bool) {
	t, err := time.Parse("15:04", clock)
	if err != nil {
		return 0, false
	}
	return t.Hour()*60 + t.Minute(), true
}

// weekdayOf returns the weekday of a date, like "Thursday".
func weekdayOf(date string) string {
	t, err := time.Parse(time.DateOnly, date)
	if err != nil {
		return date
	}
	return t.Weekday().String()
}

// icsEvent is an event of an iCalendar: at a local time on a date, for
// some minutes, or all day if at is empty.
type icsEvent struct {
	uid         string
	date        string
	at          string
	minutes     int
	summary     string
	description string
}

// icsCalendar is an iCalendar being written.
type icsCalendar struct {
	name   string
	stamp  time.Time
	events []icsEvent
}

func (c *icsCalendar) add(e icsEvent) {
	// An event is added once, by its UID, as a cook is in two weeks' reads.
	if slices.ContainsFunc(c.events, func(other icsEvent) bool { return other.uid == e.uid }) {
		return
	}
	c.events = append(c.events, e)
}

// String writes the calendar, with CRLF line endings and long lines
// folded, as RFC 5545 requires.
func (c *icsCalendar) String() string {
	var b strings.Builder
	line := func(s string) {
		// Lines are folded at 75 octets, not splitting a character.
		for len(s) > 75 {
			cut := 75
			for cut > 0 && s[cut]&0xC0 == 0x80 {
				cut--
			}
			b.WriteString(s[:cut] + "\r\n ")
			s = s[cut:]
		}
		b.WriteString(s + "\r\n")
	}
	line("BEGIN:VCALENDAR")
	line("VERSION:2.0")
	line("PRODID:-//mealplann.ing//calendar feed//EN")
	line("CALSCALE:GREGORIAN")
	line("METHOD:PUBLISH")
	line("X-WR-CALNAME:" + icsText(c.name))
	line("REFRESH-INTERVAL;VALUE=DURATION:PT1H")
	line("X-PUBLISHED-TTL:PT1H")
	stamp := c.stamp.Format("20060102T150405Z")
	for _, e := range c.events {
		day, err := time.Parse(time.DateOnly, e.date)
		if err != nil {
			continue
		}
		line("BEGIN:VEVENT")
		line("UID:" + e.uid + "@mealplann.ing")
		line("DTSTAMP:" + stamp)
		if start, ok := clockMinutes(e.at); ok {
			begin := day.Add(time.Duration(start) * time.Minute)
			line("DTSTART:" + begin.Format("20060102T150405"))
			line("DTEND:" + begin.Add(time.Duration(e.minutes)*time.Minute).Format("20060102T150405"))
		} else {
			line("DTSTART;VALUE=DATE:" + day.Format("20060102"))
			line("DTEND;VALUE=DATE:" + day.AddDate(0, 0, 1).Format("20060102"))
			line("TRANSP:TRANSPARENT")
		}
		line("SUMMARY:" + icsText(e.summary))
		if e.description != "" {
			line("DESCRIPTION:" + icsText(e.description))
		}
		line("END:VEVENT")
	}
	line("END:VCALENDAR")
	return b.String()
}

// icsText escapes text for an iCalendar property.
func icsText(s string) string {
	return strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\r\n", `\n`, "\n", `\n`).Replace(s)
}
