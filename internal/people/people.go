// Package people stores the people households plan meals for, with their
// eating patterns, the rules their eating times are checked against, and
// their daily targets.
package people

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/bs-tools/foodplann.ing/internal/nutrition"
	"github.com/bs-tools/foodplann.ing/internal/people/internal/gen"
	"github.com/bs-tools/foodplann.ing/internal/rules"
	"github.com/bs-tools/foodplann.ing/internal/store"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// ErrUserNotInHousehold is returned when a person's user isn't a member of
// the person's household.
var ErrUserNotInHousehold = errors.New("the user isn't in the household")

// ErrUserTaken is returned when another person of the household is the
// user.
var ErrUserTaken = errors.New("another person is the user")

// Meal is one of the meals a household cooks to share.
type Meal string

// The meals, as stored. The zero value is none.
const (
	MealNone      Meal = ""
	MealBreakfast Meal = "breakfast"
	MealLunch     Meal = "lunch"
	MealDinner    Meal = "dinner"
)

// Kind is whether an eating time is a main meal or a small one.
type Kind string

// The kinds of eating times, as stored.
const (
	KindMain  Kind = "main"
	KindSmall Kind = "small"
)

// Person is someone a household plans meals for.
type Person struct {
	ID          pgtype.UUID
	HouseholdID pgtype.UUID
	Name        string
	// UserID is the household's user the person is, invalid if none.
	UserID pgtype.UUID
	// EatingTimes are the person's eating times, in the order of the day.
	EatingTimes []*EatingTime
	// Rules are the rules each eating time is checked against.
	Rules rules.Rules
	// Targets are the daily targets, in the order of the nutrients.
	Targets    []rules.Target
	Notes      string
	CreateTime time.Time
	UpdateTime time.Time
}

// EatingTime is a time of day a person eats at.
type EatingTime struct {
	// ID is set by the caller, also for new eating times, so that updates
	// keep the eating times portions refer to.
	ID   pgtype.UUID
	Name string
	Kind Kind
	// TimeOfDay is "HH:MM", or empty if unknown.
	TimeOfDay string
	// Meal is the shared meal it eats from, MealNone if none.
	Meal Meal
}

// EatingTime returns the person's eating time with the ID, or nil.
func (p *Person) EatingTime(id pgtype.UUID) *EatingTime {
	for _, et := range p.EatingTimes {
		if et.ID == id {
			return et
		}
	}
	return nil
}

// Repository stores people.
type Repository struct {
	log     *slog.Logger
	db      *store.DB
	querier *gen.Queries
}

// NewRepository returns a repository of the people in the database.
func NewRepository(log *slog.Logger, db *store.DB) *Repository {
	return &Repository{log: log, db: db, querier: gen.New(db)}
}

// CreatePerson creates a person in their household, with their eating
// times and targets, and returns them. It returns ErrUserNotInHousehold or
// ErrUserTaken if the person's user can't be them.
func (r *Repository) CreatePerson(ctx context.Context, p *Person) (*Person, error) {
	id := store.NewUUID()
	var row *gen.Person
	var eatingTimes []*gen.EatingTime
	var targets []*gen.PersonTarget
	// The statements run in a single round trip, in an implicit
	// transaction, so a failed one creates nothing.
	var b store.Batch
	b.Read(func(db store.Querier) error {
		var err error
		row, err = gen.New(db).CreatePerson(ctx, &gen.CreatePersonParams{
			ID:                   id,
			HouseholdID:          p.HouseholdID,
			Name:                 p.Name,
			UserID:               p.UserID,
			MaxPortionGrams:      p.Rules.MaxPortionGrams,
			MinProteinGrams:      p.Rules.MinProteinGrams,
			MaxCarbohydrateGrams: p.Rules.MaxCarbohydrateGrams,
			MinPairingRatio:      p.Rules.MinPairingRatio,
			MaxSugarsGrams:       p.Rules.MaxSugarsGrams,
			WarnFastCarbs:        p.Rules.WarnFastCarbs,
			Notes:                p.Notes,
		})
		return err
	})
	writeChildren(ctx, &b, p.HouseholdID, id, p)
	readChildren(ctx, &b, p.HouseholdID, []pgtype.UUID{id}, &eatingTimes, &targets)
	if err := b.Send(ctx, r.db); err != nil {
		return nil, writeError("create", err)
	}
	return personFromDB(row, eatingTimes, targets), nil
}

// GetPerson gets a household's person. It returns store.ErrNotFound if the
// household has no such person.
func (r *Repository) GetPerson(ctx context.Context, householdID, id pgtype.UUID) (*Person, error) {
	var row *gen.Person
	var eatingTimes []*gen.EatingTime
	var targets []*gen.PersonTarget
	var b store.Batch
	b.Read(func(db store.Querier) error {
		var err error
		row, err = gen.New(db).GetPerson(ctx, &gen.GetPersonParams{ID: id, HouseholdID: householdID})
		if errors.Is(err, pgx.ErrNoRows) {
			return store.ErrNotFound
		}
		return err
	})
	readChildren(ctx, &b, householdID, []pgtype.UUID{id}, &eatingTimes, &targets)
	if err := b.Send(ctx, r.db); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		return nil, fmt.Errorf("failed to get person: %w", err)
	}
	return personFromDB(row, eatingTimes, targets), nil
}

// ListPeople lists a household's people, ordered by name.
func (r *Repository) ListPeople(ctx context.Context, householdID pgtype.UUID) ([]*Person, error) {
	var rows []*gen.Person
	var eatingTimes []*gen.EatingTime
	var targets []*gen.PersonTarget
	var b store.Batch
	b.Read(func(db store.Querier) error {
		var err error
		rows, err = gen.New(db).ListPeople(ctx, householdID)
		return err
	})
	readChildren(ctx, &b, householdID, nil, &eatingTimes, &targets)
	if err := b.Send(ctx, r.db); err != nil {
		return nil, fmt.Errorf("failed to list people: %w", err)
	}
	byPersonET := map[pgtype.UUID][]*gen.EatingTime{}
	for _, et := range eatingTimes {
		byPersonET[et.PersonID] = append(byPersonET[et.PersonID], et)
	}
	byPersonTarget := map[pgtype.UUID][]*gen.PersonTarget{}
	for _, t := range targets {
		byPersonTarget[t.PersonID] = append(byPersonTarget[t.PersonID], t)
	}
	people := make([]*Person, 0, len(rows))
	for _, row := range rows {
		people = append(people, personFromDB(row, byPersonET[row.ID], byPersonTarget[row.ID]))
	}
	return people, nil
}

// UpdatePerson replaces a household's person, other than their times, with
// their eating times and targets. Eating times are kept by ID: the
// person's eating times that aren't in p.EatingTimes are deleted, with
// their portions and staples. It returns store.ErrNotFound if the household
// has no such person, and ErrUserNotInHousehold or ErrUserTaken if the
// person's user can't be them.
func (r *Repository) UpdatePerson(ctx context.Context, p *Person) (*Person, error) {
	var row *gen.Person
	var eatingTimes []*gen.EatingTime
	var targets []*gen.PersonTarget
	var b store.Batch
	b.Read(func(db store.Querier) error {
		var err error
		row, err = gen.New(db).UpdatePerson(ctx, &gen.UpdatePersonParams{
			ID:                   p.ID,
			HouseholdID:          p.HouseholdID,
			Name:                 p.Name,
			UserID:               p.UserID,
			MaxPortionGrams:      p.Rules.MaxPortionGrams,
			MinProteinGrams:      p.Rules.MinProteinGrams,
			MaxCarbohydrateGrams: p.Rules.MaxCarbohydrateGrams,
			MinPairingRatio:      p.Rules.MinPairingRatio,
			MaxSugarsGrams:       p.Rules.MaxSugarsGrams,
			WarnFastCarbs:        p.Rules.WarnFastCarbs,
			Notes:                p.Notes,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return store.ErrNotFound
		}
		return err
	})
	keep := make([]pgtype.UUID, 0, len(p.EatingTimes))
	for _, et := range p.EatingTimes {
		keep = append(keep, et.ID)
	}
	q := gen.New(&b)
	_ = q.DeleteOtherEatingTimes(ctx, &gen.DeleteOtherEatingTimesParams{PersonID: p.ID, HouseholdID: p.HouseholdID, KeepIds: keep})
	_ = q.DeleteTargets(ctx, &gen.DeleteTargetsParams{PersonID: p.ID, HouseholdID: p.HouseholdID})
	writeChildren(ctx, &b, p.HouseholdID, p.ID, p)
	readChildren(ctx, &b, p.HouseholdID, []pgtype.UUID{p.ID}, &eatingTimes, &targets)
	if err := b.Send(ctx, r.db); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		return nil, writeError("update", err)
	}
	return personFromDB(row, eatingTimes, targets), nil
}

// DeletePerson deletes a household's person, with their portions and
// staples. It returns store.ErrNotFound if the household has no such
// person.
func (r *Repository) DeletePerson(ctx context.Context, householdID, id pgtype.UUID) error {
	n, err := r.querier.DeletePerson(ctx, &gen.DeletePersonParams{ID: id, HouseholdID: householdID})
	if err != nil {
		return fmt.Errorf("failed to delete person: %w", err)
	}
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

// writeChildren queues writing a person's eating times and targets.
func writeChildren(ctx context.Context, b *store.Batch, householdID, personID pgtype.UUID, p *Person) {
	et := &gen.UpsertEatingTimesParams{
		HouseholdID: householdID,
		PersonID:    personID,
		Ids:         make([]pgtype.UUID, 0, len(p.EatingTimes)),
		Names:       make([]string, 0, len(p.EatingTimes)),
		Kinds:       make([]string, 0, len(p.EatingTimes)),
		Times:       make([]string, 0, len(p.EatingTimes)),
		Meals:       make([]string, 0, len(p.EatingTimes)),
	}
	for _, e := range p.EatingTimes {
		et.Ids = append(et.Ids, e.ID)
		et.Names = append(et.Names, e.Name)
		et.Kinds = append(et.Kinds, string(e.Kind))
		et.Times = append(et.Times, e.TimeOfDay)
		et.Meals = append(et.Meals, string(e.Meal))
	}
	targets := &gen.CreateTargetsParams{
		HouseholdID: householdID,
		PersonID:    personID,
		Nutrients:   make([]string, 0, len(p.Targets)),
		Mins:        make([]float64, 0, len(p.Targets)),
		Maxes:       make([]float64, 0, len(p.Targets)),
	}
	for _, t := range p.Targets {
		targets.Nutrients = append(targets.Nutrients, t.Nutrient.String())
		targets.Mins = append(targets.Mins, orNaN(t.Min))
		targets.Maxes = append(targets.Maxes, orNaN(t.Max))
	}
	q := gen.New(b)
	_ = q.UpsertEatingTimes(ctx, et)
	_ = q.CreateTargets(ctx, targets)
}

// readChildren queues reading the eating times and targets of people, or
// of all the household's people if personIDs is nil.
func readChildren(ctx context.Context, b *store.Batch, householdID pgtype.UUID, personIDs []pgtype.UUID, eatingTimes *[]*gen.EatingTime, targets *[]*gen.PersonTarget) {
	b.Read(func(db store.Querier) error {
		var err error
		*eatingTimes, err = gen.New(db).ListEatingTimes(ctx, &gen.ListEatingTimesParams{
			HouseholdID: householdID,
			AllPeople:   personIDs == nil,
			PersonIds:   nonNil(personIDs),
		})
		return err
	})
	b.Read(func(db store.Querier) error {
		var err error
		*targets, err = gen.New(db).ListTargets(ctx, &gen.ListTargetsParams{
			HouseholdID: householdID,
			AllPeople:   personIDs == nil,
			PersonIds:   nonNil(personIDs),
		})
		return err
	})
}

// writeError translates the errors of writing a person.
func writeError(verb string, err error) error {
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		switch {
		case pgErr.Code == pgerrcode.ForeignKeyViolation && pgErr.ConstraintName == "people_household_id_user_id_fkey":
			return ErrUserNotInHousehold
		case pgErr.Code == pgerrcode.UniqueViolation && pgErr.ConstraintName == "people_household_id_user_id":
			return ErrUserTaken
		}
	}
	return fmt.Errorf("failed to %s person: %w", verb, err)
}

func personFromDB(row *gen.Person, eatingTimes []*gen.EatingTime, targets []*gen.PersonTarget) *Person {
	p := &Person{
		ID:          row.ID,
		HouseholdID: row.HouseholdID,
		Name:        row.Name,
		UserID:      row.UserID,
		EatingTimes: make([]*EatingTime, 0, len(eatingTimes)),
		Rules: rules.Rules{
			MaxPortionGrams:      row.MaxPortionGrams,
			MinProteinGrams:      row.MinProteinGrams,
			MaxCarbohydrateGrams: row.MaxCarbohydrateGrams,
			MinPairingRatio:      row.MinPairingRatio,
			MaxSugarsGrams:       row.MaxSugarsGrams,
			WarnFastCarbs:        row.WarnFastCarbs,
		},
		Targets:    make([]rules.Target, 0, len(targets)),
		Notes:      row.Notes,
		CreateTime: row.CreateTime.Time,
		UpdateTime: row.UpdateTime.Time,
	}
	for _, et := range eatingTimes {
		meal := MealNone
		if et.Meal != nil {
			meal = Meal(*et.Meal)
		}
		p.EatingTimes = append(p.EatingTimes, &EatingTime{
			ID:        et.ID,
			Name:      et.Name,
			Kind:      Kind(et.Kind),
			TimeOfDay: et.TimeOfDay,
			Meal:      meal,
		})
	}
	for _, t := range targets {
		n, ok := nutrition.ParseNutrient(string(t.Nutrient))
		if !ok {
			continue
		}
		p.Targets = append(p.Targets, rules.Target{Nutrient: n, Min: t.MinPerDay, Max: t.MaxPerDay})
	}
	return p
}

// orNaN returns *v, or NaN for nil, which the queries store as null, as
// arrays of nullable numbers can't be sent.
func orNaN(v *float64) float64 {
	if v == nil {
		return math.NaN()
	}
	return *v
}

// nonNil returns s, or an empty slice for nil, as null arrays match
// nothing.
func nonNil(s []pgtype.UUID) []pgtype.UUID {
	if s == nil {
		return []pgtype.UUID{}
	}
	return s
}
