// Package plans stores the households' meal plans: cooks of recipes, the
// portions people eat of them or of foods, and staples, the portions
// people eat regularly. It also expands staples into the dates they're on,
// and splits a shared meal into portions, for every reader of a plan to
// share: the week's view, the shopping list and the suggestions.
package plans

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/bs-tools/mealplann.ing/internal/nutrition"
	"github.com/bs-tools/mealplann.ing/internal/people"
	"github.com/bs-tools/mealplann.ing/internal/plans/internal/gen"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// ReferenceError is returned when a cook, portion or staple refers to
// something that isn't the household's, or an eating time that isn't the
// person's.
type ReferenceError struct {
	// Field is the API's name of the field with the reference, e.g.
	// "recipe_id" or "eating_time_id".
	Field string
}

func (e *ReferenceError) Error() string {
	return fmt.Sprintf("the %s isn't the household's", e.Field)
}

// referenceFields are the API's fields of the foreign keys of the plan, by
// their constraint names.
var referenceFields = map[string]string{
	"cooks_household_id_recipe_id_fkey":        "recipe_id",
	"portions_household_id_person_id_fkey":     "person_id",
	"portions_person_id_eating_time_id_fkey":   "eating_time_id",
	"portions_household_id_cook_id_fkey":       "cook_id",
	"portions_household_id_food_id_fkey":       "food_id",
	"staples_household_id_person_id_fkey":      "person_id",
	"staples_person_id_eating_time_id_fkey":    "eating_time_id",
	"staples_household_id_recipe_id_fkey":      "recipe_id",
	"staples_household_id_food_id_fkey":        "food_id",
	"staple_skips_household_id_staple_id_fkey": "id",
}

// Cook is a recipe made on a date, for a number of servings.
type Cook struct {
	ID          pgtype.UUID
	HouseholdID pgtype.UUID
	RecipeID    pgtype.UUID
	// Date is the date it's cooked on, at midnight UTC.
	Date time.Time
	// Servings scales the recipe by Servings / the recipe's servings.
	Servings float64
	// Meal is the shared meal it's for, MealNone if none.
	Meal  people.Meal
	Notes string
	Done  bool
	// FrozenServings is how many of its servings are frozen, on its date:
	// those its portions eat after the recipe's fridge days come from, and
	// the rest are in the freezer.
	FrozenServings float64
	// OffPlanServings are its servings taken without a portion: eaten
	// unplanned, given away or thrown out, like a frozen serving grabbed
	// for a lunch.
	OffPlanServings float64
	// RecipeName, AssignedServings, the servings of all its portions,
	// LateServings, those of its portions eaten after the recipe's fridge
	// days, and SuppliedServings, those it makes for other cooks, are read,
	// and ignored when writing.
	RecipeName       string
	AssignedServings float64
	LateServings     float64
	SuppliedServings float64
	// Supplies are the cooks supplying its components, and Supplying the
	// cooks it supplies. They're read with plans and cooks, and ignored
	// when writing a cook.
	Supplies   []*Supply
	Supplying  []*Supply
	CreateTime time.Time
	UpdateTime time.Time
}

// Supply is a cook making a component of another cook: e.g. a pot of
// chickpeas for a chili.
type Supply struct {
	ConsumerCookID    pgtype.UUID
	ComponentRecipeID pgtype.UUID
	SupplierCookID    pgtype.UUID
	// The cooks' dates, at midnight UTC, and recipes' names, and the
	// servings the supplier makes for the consumer, are read, and ignored
	// when writing.
	ConsumerDate       time.Time
	ConsumerRecipeName string
	SupplierDate       time.Time
	SupplierRecipeName string
	Servings           float64
}

// Portion is what a person eats at one of their eating times on a date:
// either a share of a cook, or an amount of a food.
type Portion struct {
	ID           pgtype.UUID
	HouseholdID  pgtype.UUID
	PersonID     pgtype.UUID
	EatingTimeID pgtype.UUID
	// Date is the date it's eaten on, at midnight UTC.
	Date time.Time
	// CookID is the cook it's a share of, and Servings how many of its
	// servings, if valid.
	CookID   pgtype.UUID
	Servings float64
	// FoodID is the food it's an amount of, if valid, and Amount and Unit
	// the amount.
	FoodID pgtype.UUID
	Amount float64
	Unit   nutrition.Unit
	Notes  string
	// FromFreezer is whether a share of a cook is eaten from its frozen
	// servings, whatever its date. Portions after the recipe's fridge
	// days are too, without it.
	FromFreezer bool
	// Name, the name of the recipe or food, and CookDate, CookRecipeID and
	// CookFrozenServings, of a share of a cook, are read, and ignored when
	// writing.
	Name               string
	CookDate           time.Time
	CookRecipeID       pgtype.UUID
	CookFrozenServings float64
	CreateTime         time.Time
	UpdateTime         time.Time
}

// Staple is a portion a person eats regularly: servings of a recipe or an
// amount of a food, on some weekdays.
type Staple struct {
	ID           pgtype.UUID
	HouseholdID  pgtype.UUID
	PersonID     pgtype.UUID
	EatingTimeID pgtype.UUID
	// RecipeID is the recipe it's servings of, if valid, and Servings how
	// many.
	RecipeID pgtype.UUID
	Servings float64
	// FoodID is the food it's an amount of, if valid, and Amount and Unit
	// the amount.
	FoodID pgtype.UUID
	Amount float64
	Unit   nutrition.Unit
	// Weekdays are the days of the week it's on, all if empty.
	Weekdays []time.Weekday
	// StartDate and EndDate are its first and last dates, at midnight UTC,
	// zero if unbounded.
	StartDate time.Time
	EndDate   time.Time
	Notes     string
	// ReplacesMeal is whether its person eats it instead of the shared meal
	// at its eating time, so the meal isn't planned for them.
	ReplacesMeal bool
	// Name, the name of the recipe or food, SkippedDates, the dates it's
	// skipped on in order, and EatenDates, the dates it was marked eaten on
	// in order, which took its food out of the pantry, are read, and
	// ignored when writing. EatenDates are only read with a plan, of its
	// range, as a daily staple marked eaten every day would otherwise list
	// every day since it started.
	Name         string
	SkippedDates []time.Time
	EatenDates   []time.Time
	CreateTime   time.Time
	UpdateTime   time.Time
}

// Plan is the plan of a range of dates.
type Plan struct {
	// From and To are the first and last dates, at midnight UTC.
	From time.Time
	To   time.Time
	// Cooks are the cooks cooked in the range, by date.
	Cooks []*Cook
	// Portions are the portions eaten in the range, by date.
	Portions []*Portion
	// Staples are the staples on some date of the range, with the dates
	// they're skipped on in the range, ordered by person and eating time.
	Staples []*Staple
	// Supplies are the links of cooks of which either is cooked in the
	// range, by the consumer's date. They're also in the cooks' Supplies
	// and Supplying.
	Supplies []*Supply
	// EatingOut are the eating times people eat out at in the range, by
	// date.
	EatingOut []*EatingOut
}

// EatingOut is a person's eating time on a date that they eat out at, like
// lunch at a restaurant: nothing is planned there.
type EatingOut struct {
	PersonID     pgtype.UUID
	EatingTimeID pgtype.UUID
	// Date is the date, at midnight UTC.
	Date  time.Time
	Notes string
}

// Slot returns the slot eaten out at.
func (e *EatingOut) Slot() Slot {
	return Slot{PersonID: e.PersonID, EatingTimeID: e.EatingTimeID, Date: e.Date}
}

// EatingOutAt returns the person's eating out at a slot, or nil if they
// don't eat out there.
func (p *Plan) EatingOutAt(slot Slot) *EatingOut {
	for _, e := range p.EatingOut {
		if e.Slot() == slot {
			return e
		}
	}
	return nil
}

// EatingOutSlots returns the slots people eat out at, or nil for a nil
// plan.
func (p *Plan) EatingOutSlots() map[Slot]bool {
	if p == nil {
		return nil
	}
	slots := map[Slot]bool{}
	for _, e := range p.EatingOut {
		slots[e.Slot()] = true
	}
	return slots
}

// StapleOccurrences returns the staples on each date of the plan, but those
// at eating times eaten out at, which aren't eaten, as if skipped.
func (p *Plan) StapleOccurrences() []Occurrence {
	all := ExpandStaples(p.Staples, p.From, p.To)
	if len(p.EatingOut) == 0 {
		return all
	}
	occurrences := all[:0]
	for _, o := range all {
		slot := Slot{PersonID: o.Staple.PersonID, EatingTimeID: o.Staple.EatingTimeID, Date: o.Date}
		if p.EatingOutAt(slot) == nil {
			occurrences = append(occurrences, o)
		}
	}
	return occurrences
}

// Repository stores plans.
type Repository struct {
	log *slog.Logger
	db  *store.DB
}

// NewRepository returns a repository of the plans in the database.
func NewRepository(log *slog.Logger, db *store.DB) *Repository {
	return &Repository{log: log, db: db}
}

// GetPlan gets a household's plan from one date to another, inclusive.
func (r *Repository) GetPlan(ctx context.Context, householdID pgtype.UUID, from, to time.Time) (*Plan, error) {
	var cooks []*gen.ListCooksRow
	var portions []*gen.ListPortionsRow
	var staples []*gen.ListStaplesRow
	var skips []*gen.StapleSkip
	var eaten []*gen.ListStapleEatenDatesRow
	var out []*gen.EatingOut
	fromDate, toDate := store.DateToPG(from), store.DateToPG(to)
	var b store.Batch
	b.Read(func(db store.Querier) error {
		var err error
		out, err = gen.New(db).ListEatingOut(ctx, &gen.ListEatingOutParams{
			HouseholdID: householdID, FromDate: fromDate, ToDate: toDate,
		})
		return err
	})
	b.Read(func(db store.Querier) error {
		var err error
		cooks, err = gen.New(db).ListCooks(ctx, &gen.ListCooksParams{
			HouseholdID: householdID, Ids: []pgtype.UUID{}, FromDate: fromDate, ToDate: toDate,
		})
		return err
	})
	b.Read(func(db store.Querier) error {
		var err error
		portions, err = gen.New(db).ListPortions(ctx, &gen.ListPortionsParams{
			HouseholdID: householdID, Ids: []pgtype.UUID{}, FromDate: fromDate, ToDate: toDate,
		})
		return err
	})
	var supplies []*gen.ListCookSuppliesRow
	b.Read(func(db store.Querier) error {
		var err error
		supplies, err = gen.New(db).ListCookSupplies(ctx, &gen.ListCookSuppliesParams{
			HouseholdID: householdID, Ids: []pgtype.UUID{}, FromDate: fromDate, ToDate: toDate,
		})
		return err
	})
	b.Read(func(db store.Querier) error {
		var err error
		staples, err = gen.New(db).ListStaples(ctx, &gen.ListStaplesParams{
			HouseholdID: householdID, FromDate: fromDate, ToDate: toDate,
		})
		return err
	})
	b.Read(func(db store.Querier) error {
		var err error
		skips, err = gen.New(db).ListStapleSkips(ctx, &gen.ListStapleSkipsParams{
			HouseholdID: householdID, FromDate: fromDate, ToDate: toDate,
		})
		return err
	})
	b.Read(func(db store.Querier) error {
		var err error
		eaten, err = gen.New(db).ListStapleEatenDates(ctx, &gen.ListStapleEatenDatesParams{
			HouseholdID: householdID, FromDate: fromDate, ToDate: toDate,
		})
		return err
	})
	if err := b.Send(ctx, r.db); err != nil {
		return nil, fmt.Errorf("failed to get plan: %w", err)
	}
	plan := &Plan{From: from, To: to}
	byID := map[pgtype.UUID]*Cook{}
	for _, row := range cooks {
		c := cookFromDB(row)
		plan.Cooks = append(plan.Cooks, c)
		byID[c.ID] = c
	}
	plan.Supplies = attachSupplies(byID, supplies)
	for _, row := range portions {
		plan.Portions = append(plan.Portions, portionFromDB(row))
	}
	plan.Staples = staplesFromDB(staples, skips, eaten)
	for _, row := range out {
		plan.EatingOut = append(plan.EatingOut, &EatingOut{
			PersonID:     row.PersonID,
			EatingTimeID: row.EatingTimeID,
			Date:         store.DateFromPG(row.Date),
			Notes:        row.Notes,
		})
	}
	return plan, nil
}

// SetEatingOut marks the household's people as eating out at slots, with
// notes, or stops marking them. The slots' people and eating times must be
// the household's.
func (r *Repository) SetEatingOut(ctx context.Context, householdID pgtype.UUID, slots []Slot, out bool, notes string) error {
	var b store.Batch
	q := gen.New(&b)
	for _, slot := range slots {
		if out {
			_ = q.SetEatingOut(ctx, &gen.SetEatingOutParams{
				HouseholdID: householdID, PersonID: slot.PersonID, EatingTimeID: slot.EatingTimeID,
				Date: store.DateToPG(slot.Date), Notes: notes,
			})
		} else {
			_ = q.DeleteEatingOut(ctx, &gen.DeleteEatingOutParams{
				HouseholdID: householdID, PersonID: slot.PersonID, EatingTimeID: slot.EatingTimeID,
				Date: store.DateToPG(slot.Date),
			})
		}
	}
	if err := b.Send(ctx, r.db); err != nil {
		return writeError("set eating out", err)
	}
	return nil
}

// GetCook gets a household's cook. It returns store.ErrNotFound if the
// household has no such cook.
func (r *Repository) GetCook(ctx context.Context, householdID, id pgtype.UUID) (*Cook, error) {
	var rows []*gen.ListCooksRow
	var supplies []*gen.ListCookSuppliesRow
	var b store.Batch
	b.Read(func(db store.Querier) error {
		var err error
		rows, err = gen.New(db).ListCooks(ctx, &gen.ListCooksParams{HouseholdID: householdID, Ids: []pgtype.UUID{id}})
		return err
	})
	b.Read(func(db store.Querier) error {
		var err error
		supplies, err = gen.New(db).ListCookSupplies(ctx, &gen.ListCookSuppliesParams{HouseholdID: householdID, Ids: []pgtype.UUID{id}})
		return err
	})
	if err := b.Send(ctx, r.db); err != nil {
		return nil, fmt.Errorf("failed to get cook: %w", err)
	}
	if len(rows) == 0 {
		return nil, store.ErrNotFound
	}
	c := cookFromDB(rows[0])
	attachSupplies(map[pgtype.UUID]*Cook{c.ID: c}, supplies)
	return c, nil
}

// SetCookSupply sets the cook supplying a component of a cook, and deletes
// the cook that supplied it before if it's left unused: nobody eats it, it
// supplies no other cook, and it isn't cooked yet. It returns a
// *ReferenceError if a cook isn't the household's.
func (r *Repository) SetCookSupply(ctx context.Context, householdID pgtype.UUID, supply *Supply, previous pgtype.UUID) error {
	var b store.Batch
	q := gen.New(&b)
	_, _ = q.SetCookSupply(ctx, &gen.SetCookSupplyParams{
		HouseholdID:       householdID,
		ConsumerCookID:    supply.ConsumerCookID,
		ComponentRecipeID: supply.ComponentRecipeID,
		SupplierCookID:    supply.SupplierCookID,
	})
	if previous.Valid && previous != supply.SupplierCookID {
		_, _ = q.DeleteUnusedCook(ctx, &gen.DeleteUnusedCookParams{HouseholdID: householdID, ID: previous})
	}
	if err := b.Send(ctx, r.db); err != nil {
		return writeError("set cook supply", err)
	}
	return nil
}

// attachSupplies converts links, and adds them to the cooks among them.
func attachSupplies(cooks map[pgtype.UUID]*Cook, rows []*gen.ListCookSuppliesRow) []*Supply {
	supplies := make([]*Supply, 0, len(rows))
	for _, row := range rows {
		s := &Supply{
			ConsumerCookID:     row.ConsumerCookID,
			ComponentRecipeID:  row.ComponentRecipeID,
			SupplierCookID:     row.SupplierCookID,
			ConsumerDate:       store.DateFromPG(row.ConsumerDate),
			ConsumerRecipeName: row.ConsumerRecipeName,
			SupplierDate:       store.DateFromPG(row.SupplierDate),
			SupplierRecipeName: row.SupplierRecipeName,
			Servings:           row.Servings,
		}
		supplies = append(supplies, s)
		if c, ok := cooks[s.ConsumerCookID]; ok {
			c.Supplies = append(c.Supplies, s)
		}
		if c, ok := cooks[s.SupplierCookID]; ok {
			c.Supplying = append(c.Supplying, s)
		}
	}
	return supplies
}

// CreateCook creates a cook in its household, and returns it. It returns a
// *ReferenceError if its recipe isn't the household's.
func (r *Repository) CreateCook(ctx context.Context, c *Cook) (*Cook, error) {
	created, _, err := r.CreateCookWithPortions(ctx, c, nil)
	return created, err
}

// CreateCookWithPortions creates a cook in its household, and shares of it
// for people, ignoring their CookID, all or nothing. It returns them, the
// portions in their order, and a *ReferenceError if they refer to something
// that isn't the household's.
func (r *Repository) CreateCookWithPortions(ctx context.Context, c *Cook, portions []*Portion) (*Cook, []*Portion, error) {
	cooks, created, err := r.CreateCooksWithPortions(ctx, []*CookWithPortions{{Cook: c, Portions: portions}})
	if err != nil {
		return nil, nil, err
	}
	return cooks[0], created[0], nil
}

// CookWithPortions is a cook to create with shares of it for people.
type CookWithPortions struct {
	// Cook is the cook, with its ID if valid, so that supplies can refer to
	// cooks created together, or a new one.
	Cook *Cook
	// Portions are the shares, whose CookID is ignored.
	Portions []*Portion
	// Supplies are the cooks supplying its components, by their IDs, among
	// those created or existing, whose ConsumerCookID is ignored.
	Supplies []*Supply
	// Existing is whether the cook exists already, with its ID: it isn't
	// created, but grows by Cook.Servings, for Portions, and keeps its
	// supplies.
	Existing bool
}

// CreateCooksWithPortions creates cooks in their household, or grows
// existing ones, each with shares of it for people, all or nothing, in a
// single round trip. It
// returns the cooks in their order, and the portions of each in theirs,
// and a *ReferenceError if they refer to something that isn't the
// household's.
func (r *Repository) CreateCooksWithPortions(ctx context.Context, list []*CookWithPortions) ([]*Cook, [][]*Portion, error) {
	if len(list) == 0 {
		return nil, nil, nil
	}
	householdID := list[0].Cook.HouseholdID
	var b store.Batch
	q := gen.New(&b)
	cookIDs := make([]pgtype.UUID, 0, len(list))
	portionIDs := make([][]pgtype.UUID, 0, len(list))
	var allPortionIDs []pgtype.UUID
	for _, cp := range list {
		c := cp.Cook
		id := c.ID
		if !id.Valid {
			id = store.NewUUID()
		}
		cookIDs = append(cookIDs, id)
		if cp.Existing {
			_, _ = q.GrowCook(ctx, &gen.GrowCookParams{ID: id, HouseholdID: c.HouseholdID, Servings: c.Servings})
		} else {
			_, _ = q.CreateCook(ctx, &gen.CreateCookParams{
				ID:              id,
				HouseholdID:     c.HouseholdID,
				RecipeID:        c.RecipeID,
				Date:            store.DateToPG(c.Date),
				Servings:        c.Servings,
				Meal:            mealToDB(c.Meal),
				Notes:           c.Notes,
				Done:            c.Done,
				FrozenServings:  c.FrozenServings,
				OffPlanServings: c.OffPlanServings,
			})
		}
		p := &gen.CreateCookPortionsParams{HouseholdID: c.HouseholdID, CookID: id}
		for _, portion := range cp.Portions {
			p.Ids = append(p.Ids, store.NewUUID())
			p.PersonIds = append(p.PersonIds, portion.PersonID)
			p.EatingTimeIds = append(p.EatingTimeIds, portion.EatingTimeID)
			p.Dates = append(p.Dates, store.DateToPG(portion.Date))
			p.Servings = append(p.Servings, portion.Servings)
		}
		if len(cp.Portions) > 0 {
			_, _ = q.CreateCookPortions(ctx, p)
		}
		portionIDs = append(portionIDs, p.Ids)
		allPortionIDs = append(allPortionIDs, p.Ids...)
	}
	// The supplies are created after all the cooks, which they refer to.
	sp := &gen.CreateCookSuppliesParams{HouseholdID: householdID}
	for i, cp := range list {
		for _, supply := range cp.Supplies {
			sp.ConsumerCookIds = append(sp.ConsumerCookIds, cookIDs[i])
			sp.ComponentRecipeIds = append(sp.ComponentRecipeIds, supply.ComponentRecipeID)
			sp.SupplierCookIds = append(sp.SupplierCookIds, supply.SupplierCookID)
		}
	}
	if len(sp.ConsumerCookIds) > 0 {
		_ = q.CreateCookSupplies(ctx, sp)
	}
	var cookRows []*gen.ListCooksRow
	var portionRows []*gen.ListPortionsRow
	var supplyRows []*gen.ListCookSuppliesRow
	b.Read(func(db store.Querier) error {
		var err error
		cookRows, err = gen.New(db).ListCooks(ctx, &gen.ListCooksParams{HouseholdID: householdID, Ids: cookIDs})
		return err
	})
	if len(allPortionIDs) > 0 {
		b.Read(func(db store.Querier) error {
			var err error
			portionRows, err = gen.New(db).ListPortions(ctx, &gen.ListPortionsParams{HouseholdID: householdID, Ids: allPortionIDs})
			return err
		})
	}
	if len(sp.ConsumerCookIds) > 0 {
		b.Read(func(db store.Querier) error {
			var err error
			supplyRows, err = gen.New(db).ListCookSupplies(ctx, &gen.ListCookSuppliesParams{HouseholdID: householdID, Ids: cookIDs})
			return err
		})
	}
	if err := b.Send(ctx, r.db); err != nil {
		return nil, nil, writeError("create cook", err)
	}
	// The cooks and portions are returned in the order they were given,
	// rather than the queries'.
	cooksByID := map[pgtype.UUID]*Cook{}
	for _, row := range cookRows {
		cooksByID[row.Cook.ID] = cookFromDB(row)
	}
	attachSupplies(cooksByID, supplyRows)
	portionsByID := map[pgtype.UUID]*Portion{}
	for _, row := range portionRows {
		portionsByID[row.Portion.ID] = portionFromDB(row)
	}
	cooks := make([]*Cook, 0, len(list))
	portions := make([][]*Portion, 0, len(list))
	for i, id := range cookIDs {
		c, ok := cooksByID[id]
		if !ok {
			return nil, nil, errors.New("failed to read a created cook")
		}
		cooks = append(cooks, c)
		created := make([]*Portion, 0, len(portionIDs[i]))
		for _, pid := range portionIDs[i] {
			if portion, ok := portionsByID[pid]; ok {
				created = append(created, portion)
			}
		}
		portions = append(portions, created)
	}
	return cooks, portions, nil
}

// FrozenCook is a cook with frozen servings in the freezer.
type FrozenCook struct {
	*Cook
	// FridgeDays and FreezerDays are how many days its recipe's leftovers
	// keep in the fridge and frozen, nil if unknown.
	FridgeDays  *int32
	FreezerDays *int32
}

// ListFrozenCooks lists a household's cooks with frozen servings not all
// planned yet, by date: what's in the freezer.
func (r *Repository) ListFrozenCooks(ctx context.Context, householdID pgtype.UUID) ([]*FrozenCook, error) {
	rows, err := gen.New(r.db).ListFrozenCooks(ctx, householdID)
	if err != nil {
		return nil, fmt.Errorf("failed to list frozen cooks: %w", err)
	}
	list := make([]*FrozenCook, 0, len(rows))
	for _, row := range rows {
		c := cookFromDB(&gen.ListCooksRow{Cook: row.Cook, RecipeName: row.RecipeName, LateServings: row.LateServings})
		list = append(list, &FrozenCook{Cook: c, FridgeDays: row.FridgeDays, FreezerDays: row.FreezerDays})
	}
	return list, nil
}

// UpdateCook replaces a household's cook, other than its times. It returns
// store.ErrNotFound if the household has no such cook, and a
// *ReferenceError if its recipe isn't the household's.
func (r *Repository) UpdateCook(ctx context.Context, c *Cook) (*Cook, error) {
	var updated int64
	var rows []*gen.ListCooksRow
	var b store.Batch
	b.Read(func(db store.Querier) error {
		var err error
		updated, err = gen.New(db).UpdateCook(ctx, &gen.UpdateCookParams{
			ID:              c.ID,
			HouseholdID:     c.HouseholdID,
			RecipeID:        c.RecipeID,
			Date:            store.DateToPG(c.Date),
			Servings:        c.Servings,
			Meal:            mealToDB(c.Meal),
			Notes:           c.Notes,
			Done:            c.Done,
			FrozenServings:  c.FrozenServings,
			OffPlanServings: c.OffPlanServings,
		})
		return err
	})
	b.Read(func(db store.Querier) error {
		var err error
		rows, err = gen.New(db).ListCooks(ctx, &gen.ListCooksParams{HouseholdID: c.HouseholdID, Ids: []pgtype.UUID{c.ID}})
		return err
	})
	if err := b.Send(ctx, r.db); err != nil {
		return nil, writeError("update cook", err)
	}
	if updated == 0 || len(rows) == 0 {
		return nil, store.ErrNotFound
	}
	return cookFromDB(rows[0]), nil
}

// DeleteCook deletes a household's cook, with its portions. It returns
// store.ErrNotFound if the household has no such cook.
func (r *Repository) DeleteCook(ctx context.Context, householdID, id pgtype.UUID) error {
	// The cooks only supplying it go with it, in the same round trip.
	var n int64
	var b store.Batch
	_ = gen.New(&b).DeleteOrphanSuppliers(ctx, &gen.DeleteOrphanSuppliersParams{HouseholdID: householdID, ConsumerCookID: id})
	b.Read(func(db store.Querier) error {
		var err error
		n, err = gen.New(db).DeleteCook(ctx, &gen.DeleteCookParams{ID: id, HouseholdID: householdID})
		return err
	})
	if err := b.Send(ctx, r.db); err != nil {
		return fmt.Errorf("failed to delete cook: %w", err)
	}
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

// GetPortion gets a household's portion. It returns store.ErrNotFound if
// the household has no such portion.
func (r *Repository) GetPortion(ctx context.Context, householdID, id pgtype.UUID) (*Portion, error) {
	rows, err := gen.New(r.db).ListPortions(ctx, &gen.ListPortionsParams{HouseholdID: householdID, Ids: []pgtype.UUID{id}})
	if err != nil {
		return nil, fmt.Errorf("failed to get portion: %w", err)
	}
	if len(rows) == 0 {
		return nil, store.ErrNotFound
	}
	return portionFromDB(rows[0]), nil
}

// CreatePortion creates a portion in its household, and returns it. It
// returns a *ReferenceError if it refers to something that isn't the
// household's.
func (r *Repository) CreatePortion(ctx context.Context, p *Portion) (*Portion, error) {
	id := store.NewUUID()
	var rows []*gen.ListPortionsRow
	var b store.Batch
	_, _ = gen.New(&b).CreatePortion(ctx, &gen.CreatePortionParams{
		ID:           id,
		HouseholdID:  p.HouseholdID,
		PersonID:     p.PersonID,
		EatingTimeID: p.EatingTimeID,
		Date:         store.DateToPG(p.Date),
		CookID:       p.CookID,
		Servings:     positive(p.Servings, p.CookID.Valid),
		FoodID:       p.FoodID,
		Amount:       positive(p.Amount, p.FoodID.Valid),
		Unit:         unitToDB(p.Unit, p.FoodID.Valid),
		Notes:        p.Notes,
		FromFreezer:  p.FromFreezer,
	})
	b.Read(func(db store.Querier) error {
		var err error
		rows, err = gen.New(db).ListPortions(ctx, &gen.ListPortionsParams{HouseholdID: p.HouseholdID, Ids: []pgtype.UUID{id}})
		return err
	})
	if err := b.Send(ctx, r.db); err != nil {
		return nil, writeError("create portion", err)
	}
	if len(rows) == 0 {
		return nil, errors.New("failed to read the created portion")
	}
	return portionFromDB(rows[0]), nil
}

// UpdatePortion replaces a household's portion, other than its times. It
// returns store.ErrNotFound if the household has no such portion, and a
// *ReferenceError if it refers to something that isn't the household's.
func (r *Repository) UpdatePortion(ctx context.Context, p *Portion) (*Portion, error) {
	var updated int64
	var rows []*gen.ListPortionsRow
	var b store.Batch
	b.Read(func(db store.Querier) error {
		var err error
		updated, err = gen.New(db).UpdatePortion(ctx, &gen.UpdatePortionParams{
			ID:           p.ID,
			HouseholdID:  p.HouseholdID,
			PersonID:     p.PersonID,
			EatingTimeID: p.EatingTimeID,
			Date:         store.DateToPG(p.Date),
			CookID:       p.CookID,
			Servings:     positive(p.Servings, p.CookID.Valid),
			FoodID:       p.FoodID,
			Amount:       positive(p.Amount, p.FoodID.Valid),
			Unit:         unitToDB(p.Unit, p.FoodID.Valid),
			Notes:        p.Notes,
		})
		return err
	})
	b.Read(func(db store.Querier) error {
		var err error
		rows, err = gen.New(db).ListPortions(ctx, &gen.ListPortionsParams{HouseholdID: p.HouseholdID, Ids: []pgtype.UUID{p.ID}})
		return err
	})
	if err := b.Send(ctx, r.db); err != nil {
		return nil, writeError("update portion", err)
	}
	if updated == 0 || len(rows) == 0 {
		return nil, store.ErrNotFound
	}
	return portionFromDB(rows[0]), nil
}

// DeletePortion deletes a household's portion. It returns
// store.ErrNotFound if the household has no such portion.
func (r *Repository) DeletePortion(ctx context.Context, householdID, id pgtype.UUID) error {
	n, err := gen.New(r.db).DeletePortion(ctx, &gen.DeletePortionParams{ID: id, HouseholdID: householdID})
	if err != nil {
		return fmt.Errorf("failed to delete portion: %w", err)
	}
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

// GetStaple gets a household's staple, with all the dates it's skipped on.
// It returns store.ErrNotFound if the household has no such staple.
func (r *Repository) GetStaple(ctx context.Context, householdID, id pgtype.UUID) (*Staple, error) {
	var b store.Batch
	var staple *Staple
	readStaple(ctx, &b, householdID, id, &staple)
	if err := b.Send(ctx, r.db); err != nil {
		return nil, fmt.Errorf("failed to get staple: %w", err)
	}
	if staple == nil {
		return nil, store.ErrNotFound
	}
	return staple, nil
}

// ListStaples lists a household's staples, or a person's if personID is
// valid, with all the dates they're skipped on, ordered by person and
// eating time.
func (r *Repository) ListStaples(ctx context.Context, householdID, personID pgtype.UUID) ([]*Staple, error) {
	var staples []*gen.ListStaplesRow
	var skips []*gen.StapleSkip
	var b store.Batch
	b.Read(func(db store.Querier) error {
		var err error
		staples, err = gen.New(db).ListStaples(ctx, &gen.ListStaplesParams{HouseholdID: householdID, PersonID: personID})
		return err
	})
	b.Read(func(db store.Querier) error {
		var err error
		skips, err = gen.New(db).ListStapleSkips(ctx, &gen.ListStapleSkipsParams{HouseholdID: householdID})
		return err
	})
	if err := b.Send(ctx, r.db); err != nil {
		return nil, fmt.Errorf("failed to list staples: %w", err)
	}
	return staplesFromDB(staples, skips, nil), nil
}

// CreateStaple creates a staple in its household, and returns it. It
// returns a *ReferenceError if it refers to something that isn't the
// household's.
func (r *Repository) CreateStaple(ctx context.Context, s *Staple) (*Staple, error) {
	id := store.NewUUID()
	var b store.Batch
	_, _ = gen.New(&b).CreateStaple(ctx, &gen.CreateStapleParams{
		ID:           id,
		HouseholdID:  s.HouseholdID,
		PersonID:     s.PersonID,
		EatingTimeID: s.EatingTimeID,
		RecipeID:     s.RecipeID,
		Servings:     positive(s.Servings, s.RecipeID.Valid),
		FoodID:       s.FoodID,
		Amount:       positive(s.Amount, s.FoodID.Valid),
		Unit:         unitToDB(s.Unit, s.FoodID.Valid),
		Weekdays:     weekdaysToDB(s.Weekdays),
		StartDate:    store.DateToPG(s.StartDate),
		EndDate:      store.DateToPG(s.EndDate),
		Notes:        s.Notes,
		ReplacesMeal: s.ReplacesMeal,
	})
	var created *Staple
	readStaple(ctx, &b, s.HouseholdID, id, &created)
	if err := b.Send(ctx, r.db); err != nil {
		return nil, writeError("create staple", err)
	}
	if created == nil {
		return nil, errors.New("failed to read the created staple")
	}
	return created, nil
}

// UpdateStaple replaces a household's staple, other than its times and
// skipped dates. It returns store.ErrNotFound if the household has no such
// staple, and a *ReferenceError if it refers to something that isn't the
// household's.
func (r *Repository) UpdateStaple(ctx context.Context, s *Staple) (*Staple, error) {
	var b store.Batch
	_, _ = gen.New(&b).UpdateStaple(ctx, &gen.UpdateStapleParams{
		ID:           s.ID,
		HouseholdID:  s.HouseholdID,
		PersonID:     s.PersonID,
		EatingTimeID: s.EatingTimeID,
		RecipeID:     s.RecipeID,
		Servings:     positive(s.Servings, s.RecipeID.Valid),
		FoodID:       s.FoodID,
		Amount:       positive(s.Amount, s.FoodID.Valid),
		Unit:         unitToDB(s.Unit, s.FoodID.Valid),
		Weekdays:     weekdaysToDB(s.Weekdays),
		StartDate:    store.DateToPG(s.StartDate),
		EndDate:      store.DateToPG(s.EndDate),
		Notes:        s.Notes,
		ReplacesMeal: s.ReplacesMeal,
	})
	var updated *Staple
	readStaple(ctx, &b, s.HouseholdID, s.ID, &updated)
	if err := b.Send(ctx, r.db); err != nil {
		return nil, writeError("update staple", err)
	}
	if updated == nil {
		return nil, store.ErrNotFound
	}
	return updated, nil
}

// DeleteStaple deletes a household's staple. It returns store.ErrNotFound
// if the household has no such staple.
func (r *Repository) DeleteStaple(ctx context.Context, householdID, id pgtype.UUID) error {
	n, err := gen.New(r.db).DeleteStaple(ctx, &gen.DeleteStapleParams{ID: id, HouseholdID: householdID})
	if err != nil {
		return fmt.Errorf("failed to delete staple: %w", err)
	}
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

// SetStapleSkipped skips a household's staple on a date, or stops skipping
// it, and returns the staple. It returns store.ErrNotFound if the household
// has no such staple.
func (r *Repository) SetStapleSkipped(ctx context.Context, householdID, id pgtype.UUID, date time.Time, skipped bool) (*Staple, error) {
	var b store.Batch
	q := gen.New(&b)
	if skipped {
		_ = q.SkipStaple(ctx, &gen.SkipStapleParams{HouseholdID: householdID, StapleID: id, Date: store.DateToPG(date)})
	} else {
		_ = q.UnskipStaple(ctx, &gen.UnskipStapleParams{HouseholdID: householdID, StapleID: id, Date: store.DateToPG(date)})
	}
	var staple *Staple
	readStaple(ctx, &b, householdID, id, &staple)
	if err := b.Send(ctx, r.db); err != nil {
		return nil, fmt.Errorf("failed to skip staple: %w", err)
	}
	if staple == nil {
		return nil, store.ErrNotFound
	}
	return staple, nil
}

// readStaple queues reading a staple with all its skipped dates into
// staple, which stays nil if the household has no such staple.
func readStaple(ctx context.Context, b *store.Batch, householdID, id pgtype.UUID, staple **Staple) {
	var rows []*gen.ListStaplesRow
	b.Read(func(db store.Querier) error {
		var err error
		rows, err = gen.New(db).ListStaples(ctx, &gen.ListStaplesParams{HouseholdID: householdID, ID: id})
		return err
	})
	b.Read(func(db store.Querier) error {
		skips, err := gen.New(db).ListStapleSkips(ctx, &gen.ListStapleSkipsParams{HouseholdID: householdID, StapleID: id})
		if err != nil {
			return err
		}
		if list := staplesFromDB(rows, skips, nil); len(list) > 0 {
			*staple = list[0]
		}
		return nil
	})
}

// writeError translates the errors of writing a plan.
func writeError(what string, err error) error {
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == pgerrcode.ForeignKeyViolation {
		if field, ok := referenceFields[pgErr.ConstraintName]; ok {
			return &ReferenceError{Field: field}
		}
	}
	return fmt.Errorf("failed to %s: %w", what, err)
}

func cookFromDB(row *gen.ListCooksRow) *Cook {
	c := &row.Cook
	return &Cook{
		ID:               c.ID,
		HouseholdID:      c.HouseholdID,
		RecipeID:         c.RecipeID,
		Date:             store.DateFromPG(c.Date),
		Servings:         c.Servings,
		Meal:             mealFromDB(c.Meal),
		Notes:            c.Notes,
		Done:             c.Done,
		FrozenServings:   c.FrozenServings,
		OffPlanServings:  c.OffPlanServings,
		RecipeName:       row.RecipeName,
		AssignedServings: row.AssignedServings,
		LateServings:     row.LateServings,
		SuppliedServings: row.SuppliedServings,
		CreateTime:       c.CreateTime.Time,
		UpdateTime:       c.UpdateTime.Time,
	}
}

func portionFromDB(row *gen.ListPortionsRow) *Portion {
	p := &row.Portion
	return &Portion{
		ID:                 p.ID,
		HouseholdID:        p.HouseholdID,
		PersonID:           p.PersonID,
		EatingTimeID:       p.EatingTimeID,
		Date:               store.DateFromPG(p.Date),
		CookID:             p.CookID,
		Servings:           orZero(p.Servings),
		FoodID:             p.FoodID,
		Amount:             orZero(p.Amount),
		Unit:               unitFromDB(p.Unit),
		Notes:              p.Notes,
		FromFreezer:        p.FromFreezer,
		Name:               row.Name,
		CookDate:           store.DateFromPG(row.CookDate),
		CookRecipeID:       row.CookRecipeID,
		CookFrozenServings: orZero(row.CookFrozenServings),
		CreateTime:         p.CreateTime.Time,
		UpdateTime:         p.UpdateTime.Time,
	}
}

// staplesFromDB converts staples, in order, with their skipped dates and
// dates marked eaten.
func staplesFromDB(rows []*gen.ListStaplesRow, skips []*gen.StapleSkip, eaten []*gen.ListStapleEatenDatesRow) []*Staple {
	skipped := map[pgtype.UUID][]time.Time{}
	for _, skip := range skips {
		skipped[skip.StapleID] = append(skipped[skip.StapleID], store.DateFromPG(skip.Date))
	}
	eatenDates := map[pgtype.UUID][]time.Time{}
	for _, e := range eaten {
		eatenDates[e.StapleID] = append(eatenDates[e.StapleID], store.DateFromPG(e.Date))
	}
	staples := make([]*Staple, 0, len(rows))
	for _, row := range rows {
		s := &row.Staple
		weekdays := make([]time.Weekday, 0, len(s.Weekdays))
		for _, iso := range s.Weekdays {
			weekdays = append(weekdays, time.Weekday(iso%7))
		}
		staples = append(staples, &Staple{
			ID:           s.ID,
			HouseholdID:  s.HouseholdID,
			PersonID:     s.PersonID,
			EatingTimeID: s.EatingTimeID,
			RecipeID:     s.RecipeID,
			Servings:     orZero(s.Servings),
			FoodID:       s.FoodID,
			Amount:       orZero(s.Amount),
			Unit:         unitFromDB(s.Unit),
			Weekdays:     weekdays,
			StartDate:    store.DateFromPG(s.StartDate),
			EndDate:      store.DateFromPG(s.EndDate),
			Notes:        s.Notes,
			ReplacesMeal: s.ReplacesMeal,
			Name:         row.Name,
			SkippedDates: skipped[s.ID],
			EatenDates:   eatenDates[s.ID],
			CreateTime:   s.CreateTime.Time,
			UpdateTime:   s.UpdateTime.Time,
		})
	}
	return staples
}

// weekdaysToDB converts weekdays to ISO weekdays, from 1 for Monday to 7
// for Sunday.
func weekdaysToDB(weekdays []time.Weekday) []int16 {
	iso := make([]int16, 0, len(weekdays))
	for _, d := range weekdays {
		iso = append(iso, int16((d+6)%7+1))
	}
	return iso
}

func mealToDB(m people.Meal) *gen.HouseholdMeal {
	if m == people.MealNone {
		return nil
	}
	return new(gen.HouseholdMeal(m))
}

func mealFromDB(m *gen.HouseholdMeal) people.Meal {
	if m == nil {
		return people.MealNone
	}
	return people.Meal(*m)
}

// unitToDB converts the unit of an amount, which is null when there's no
// amount.
func unitToDB(u nutrition.Unit, set bool) *gen.AmountUnit {
	if !set {
		return nil
	}
	switch u {
	case nutrition.Milliliters:
		return new(gen.AmountUnitMilliliters)
	case nutrition.Pieces:
		return new(gen.AmountUnitPieces)
	default:
		return new(gen.AmountUnitGrams)
	}
}

func unitFromDB(u *gen.AmountUnit) nutrition.Unit {
	if u == nil {
		return 0
	}
	switch *u {
	case gen.AmountUnitMilliliters:
		return nutrition.Milliliters
	case gen.AmountUnitPieces:
		return nutrition.Pieces
	default:
		return nutrition.Grams
	}
}

// positive returns v if set, and null otherwise, as the columns of what a
// portion or staple doesn't eat are null.
func positive(v float64, set bool) *float64 {
	if !set {
		return nil
	}
	return &v
}

// orZero returns *v, or 0 for null.
func orZero(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}

// ListLeftoverCooks lists a household's cooks before a date, from up to 14
// days before it, whose leftovers still keep in the fridge on it, by their
// recipe's fridge days, by date.
func (r *Repository) ListLeftoverCooks(ctx context.Context, householdID pgtype.UUID, before time.Time) ([]*Cook, error) {
	rows, err := gen.New(r.db).ListLeftoverCooks(ctx, &gen.ListLeftoverCooksParams{HouseholdID: householdID, Before: store.DateToPG(before)})
	if err != nil {
		return nil, fmt.Errorf("failed to list leftover cooks: %w", err)
	}
	cooks := make([]*Cook, 0, len(rows))
	for _, row := range rows {
		cooks = append(cooks, cookFromDB(&gen.ListCooksRow{
			Cook: row.Cook, RecipeName: row.RecipeName, AssignedServings: row.AssignedServings,
			LateServings: row.LateServings, SuppliedServings: row.SuppliedServings,
		}))
	}
	return cooks, nil
}
