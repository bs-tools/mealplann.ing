package templates

import (
	"cmp"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/bs-tools/mealplann.ing/internal/people"
	"github.com/bs-tools/mealplann.ing/internal/plans"
	"github.com/jackc/pgx/v5/pgtype"
)

// minCookServings is the fewest servings a cook can have, like the plan's.
const minCookServings = 0.25

// Skip is an entry someone eats that applying a template leaves them out
// of, as no eating time of theirs matches its slot.
type Skip struct {
	Entry  *Entry
	Slot   *Slot
	Person *people.Person
}

// String says what's skipped, for people.
func (s *Skip) String() string {
	return fmt.Sprintf("%s has no eating time for the %s slot, so they're left out of entry %s on day %d",
		s.Person.Name, s.Slot.Name, s.Entry.Key, s.Entry.Day+1)
}

// Applied is a template applied to people from a start date: the cooks to
// create, each with its portions, by date.
type Applied struct {
	Cooks []*plans.CookWithPortions
	// Skips are the entries people are left out of.
	Skips []Skip
	// Unused are the entries nobody eats, by key, as everyone's servings
	// are 0 or nobody's eating time matches.
	Unused []string
}

// Apply applies a template to people from a date at midnight UTC. Each
// entry gives a portion to each person with an eating time for its slot
// and servings of it. Each batch is a cook of its entries' recipe on its
// day, with their portions; each entry outside a batch is a cook of its
// own on its day. A cook's servings are its batch's, or else its
// portions', and its meal is that of a slot it's eaten at on its day.
// The cooks have no household or IDs, which the caller sets.
func Apply(t *Template, ps []*people.Person, start time.Time) *Applied {
	applied := &Applied{}
	portions := map[string][]*plans.Portion{}
	for _, e := range t.Entries {
		slot := t.Slot(e.SlotKey)
		if slot == nil {
			continue
		}
		for _, p := range ps {
			servings := e.ServingsFor(p.ID)
			if servings <= 0 {
				continue
			}
			et := t.EatingTimeFor(slot, p)
			if et == nil {
				applied.Skips = append(applied.Skips, Skip{Entry: e, Slot: slot, Person: p})
				continue
			}
			portions[e.Key] = append(portions[e.Key], &plans.Portion{
				PersonID:     p.ID,
				EatingTimeID: et.ID,
				Date:         start.AddDate(0, 0, int(e.Day)),
				Servings:     servings,
			})
		}
		if len(portions[e.Key]) == 0 {
			applied.Unused = append(applied.Unused, e.Key)
		}
	}

	batched := map[string]bool{}
	addCook := func(day int32, servings float64, entries []*Entry) {
		var list []*plans.Portion
		var assigned float64
		for _, e := range entries {
			list = append(list, portions[e.Key]...)
			for _, p := range portions[e.Key] {
				assigned += p.Servings
			}
		}
		if len(list) == 0 {
			return
		}
		if servings <= 0 {
			servings = max(math.Round(assigned*100)/100, minCookServings)
		}
		meal := people.MealNone
		for _, e := range entries {
			if s := t.Slot(e.SlotKey); e.Day == day && s.Meal != people.MealNone {
				meal = s.Meal
				break
			}
		}
		applied.Cooks = append(applied.Cooks, &plans.CookWithPortions{
			Cook: &plans.Cook{
				RecipeID: entries[0].RecipeID,
				Date:     start.AddDate(0, 0, int(day)),
				Servings: servings,
				Meal:     meal,
			},
			Portions: list,
		})
	}
	for _, b := range t.Batches {
		var entries []*Entry
		for _, key := range b.EntryKeys {
			if e := t.Entry(key); e != nil {
				entries = append(entries, e)
				batched[key] = true
			}
		}
		if len(entries) > 0 {
			addCook(b.Day, b.Servings, entries)
		}
	}
	for _, e := range t.Entries {
		if !batched[e.Key] && t.Slot(e.SlotKey) != nil {
			addCook(e.Day, 0, []*Entry{e})
		}
	}
	slices.SortStableFunc(applied.Cooks, func(a, b *plans.CookWithPortions) int {
		return cmp.Or(a.Cook.Date.Compare(b.Cook.Date), cmp.Compare(mealOrder(a.Cook.Meal), mealOrder(b.Cook.Meal)))
	})
	return applied
}

// mealOrder orders shared meals as in a day, with cooks for no meal last.
func mealOrder(m people.Meal) int {
	switch m {
	case people.MealBreakfast:
		return 0
	case people.MealLunch:
		return 1
	case people.MealDinner:
		return 2
	default:
		return 3
	}
}

// SlotsOf returns the slots of a template for people: one for each shared
// meal someone eats, and one for each eating time without a shared meal, by
// kind and position, named after the first person's, in the order of the
// eating times of the person with the most.
func SlotsOf(ps []*people.Person) []*Slot {
	byCount := slices.Clone(ps)
	slices.SortStableFunc(byCount, func(a, b *people.Person) int { return cmp.Compare(len(b.EatingTimes), len(a.EatingTimes)) })
	var slots []*Slot
	seen := map[string]bool{}
	for _, p := range byCount {
		positions := map[people.Kind]int{}
		for _, et := range p.EatingTimes {
			key := string(et.Meal)
			if et.Meal == people.MealNone {
				positions[et.Kind]++
				key = fmt.Sprintf("%s-%d", et.Kind, positions[et.Kind])
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			slots = append(slots, &Slot{Key: key, Name: et.Name, Kind: et.Kind, Meal: et.Meal})
		}
	}
	return slots
}

// FromPlan makes the slots, entries and batches of a template from the
// cooks of a plan eaten from one date to the days after it, for people:
// each person's portions of a cook at a slot on a day are an entry, and a
// cook whose portions are on several days or slots, or that leaves
// servings over, is a batch. Portions of foods without a recipe, and
// staples, are left out.
func FromPlan(plan *plans.Plan, ps []*people.Person, from time.Time, days int32) *Template {
	t := &Template{Days: days, Slots: SlotsOf(ps)}
	to := from.AddDate(0, 0, int(days))
	type group struct {
		cook     *plans.Cook
		day      int32
		slot     *Slot
		portions []*plans.Portion
	}
	cooks := map[pgtype.UUID]*plans.Cook{}
	for _, c := range plan.Cooks {
		cooks[c.ID] = c
	}
	byPerson := map[pgtype.UUID]*people.Person{}
	for _, p := range ps {
		byPerson[p.ID] = p
	}
	var groups []*group
	for _, portion := range plan.Portions {
		c, ok := cooks[portion.CookID]
		p := byPerson[portion.PersonID]
		if !ok || p == nil || portion.Date.Before(from) || !portion.Date.Before(to) {
			continue
		}
		et := p.EatingTime(portion.EatingTimeID)
		if et == nil {
			continue
		}
		slot := slotOf(t, p, et)
		if slot == nil {
			continue
		}
		day := int32(portion.Date.Sub(from).Hours() / 24)
		i := slices.IndexFunc(groups, func(g *group) bool { return g.cook == c && g.day == day && g.slot == slot })
		if i < 0 {
			groups = append(groups, &group{cook: c, day: day, slot: slot})
			i = len(groups) - 1
		}
		groups[i].portions = append(groups[i].portions, portion)
	}
	slices.SortStableFunc(groups, func(a, b *group) int {
		return cmp.Or(cmp.Compare(a.day, b.day), cmp.Compare(slices.Index(t.Slots, a.slot), slices.Index(t.Slots, b.slot)))
	})

	byCook := map[*plans.Cook][]*Entry{}
	assigned := map[*plans.Cook]float64{}
	var cookOrder []*plans.Cook
	for _, g := range groups {
		for _, portion := range g.portions {
			assigned[g.cook] += portion.Servings
		}
		e := &Entry{
			Key:      uniqueKey(t, fmt.Sprintf("d%d-%s", g.day+1, g.slot.Key)),
			Day:      g.day,
			SlotKey:  g.slot.Key,
			RecipeID: g.cook.RecipeID,
			Servings: commonServings(g.portions),
		}
		// Everyone who didn't eat it gets 0, so applying the template
		// doesn't say they're left out, and anyone who ate a different
		// amount gets theirs.
		for _, p := range ps {
			servings := 0.0
			for _, portion := range g.portions {
				if portion.PersonID == p.ID {
					servings += portion.Servings
				}
			}
			if servings != e.Servings {
				e.PersonServings = append(e.PersonServings, PersonServings{PersonID: p.ID, Servings: servings})
			}
		}
		t.Entries = append(t.Entries, e)
		if _, ok := byCook[g.cook]; !ok {
			cookOrder = append(cookOrder, g.cook)
		}
		byCook[g.cook] = append(byCook[g.cook], e)
	}
	for _, c := range cookOrder {
		entries := byCook[c]
		cookDay := max(int32(c.Date.Sub(from).Hours()/24), 0)
		if len(entries) == 1 && entries[0].Day == cookDay && math.Abs(assigned[c]-c.Servings) < 0.01 {
			continue
		}
		b := &Batch{Day: min(cookDay, entries[0].Day), Servings: c.Servings}
		for _, e := range entries {
			b.EntryKeys = append(b.EntryKeys, e.Key)
		}
		t.Batches = append(t.Batches, b)
	}
	return t
}

// slotOf returns the template's slot a person's eating time maps to.
func slotOf(t *Template, p *people.Person, et *people.EatingTime) *Slot {
	for _, s := range t.Slots {
		if t.EatingTimeFor(s, p) == et {
			return s
		}
	}
	return nil
}

// commonServings returns the servings most of the portions have, the
// largest if tied, as the entry's servings for everyone.
func commonServings(portions []*plans.Portion) float64 {
	counts := map[float64]int{}
	for _, p := range portions {
		counts[p.Servings]++
	}
	best, bestCount := 0.0, 0
	for _, s := range slices.Sorted(maps.Keys(counts)) {
		if counts[s] >= bestCount {
			best, bestCount = s, counts[s]
		}
	}
	return best
}

// uniqueKey returns key, or key with a number after it if an entry of the
// template has it already.
func uniqueKey(t *Template, key string) string {
	key = strings.ToLower(key)
	candidate := key
	for n := 2; t.Entry(candidate) != nil; n++ {
		candidate = fmt.Sprintf("%s-%d", key, n)
	}
	return candidate
}

// NewKey returns a key for an entry without one: the day and slot, e.g.
// "d1-dinner", made unique.
func NewKey(t *Template, e *Entry) string {
	return uniqueKey(t, fmt.Sprintf("d%d-%s", e.Day+1, e.SlotKey))
}
