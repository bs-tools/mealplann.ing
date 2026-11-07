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

	"github.com/bs-tools/foodplann.ing/internal/nutrition"
	"github.com/bs-tools/foodplann.ing/internal/people"
	"github.com/bs-tools/foodplann.ing/internal/plans/internal/gen"
	"github.com/bs-tools/foodplann.ing/internal/store"
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
	// RecipeName and AssignedServings, the servings of all its portions,
	// are read, and ignored when writing.
	RecipeName       string
	AssignedServings float64
	CreateTime       time.Time
	UpdateTime       time.Time
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
	// Name, the name of the recipe or food, and CookDate and CookRecipeID,
	// of a share of a cook, are read, and ignored when writing.
	Name         string
	CookDate     time.Time
	CookRecipeID pgtype.UUID
	CreateTime   time.Time
	UpdateTime   time.Time
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
	// Name, the name of the recipe or food, and SkippedDates, the dates it's
	// skipped on in order, are read, and ignored when writing.
	Name         string
	SkippedDates []time.Time
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
}

// StapleOccurrences returns the staples on each date of the plan.
func (p *Plan) StapleOccurrences() []Occurrence {
	return ExpandStaples(p.Staples, p.From, p.To)
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
	fromDate, toDate := store.DateToPG(from), store.DateToPG(to)
	var b store.Batch
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
	if err := b.Send(ctx, r.db); err != nil {
		return nil, fmt.Errorf("failed to get plan: %w", err)
	}
	plan := &Plan{From: from, To: to}
	for _, row := range cooks {
		plan.Cooks = append(plan.Cooks, cookFromDB(row))
	}
	for _, row := range portions {
		plan.Portions = append(plan.Portions, portionFromDB(row))
	}
	plan.Staples = staplesFromDB(staples, skips)
	return plan, nil
}

// GetCook gets a household's cook. It returns store.ErrNotFound if the
// household has no such cook.
func (r *Repository) GetCook(ctx context.Context, householdID, id pgtype.UUID) (*Cook, error) {
	rows, err := gen.New(r.db).ListCooks(ctx, &gen.ListCooksParams{HouseholdID: householdID, Ids: []pgtype.UUID{id}})
	if err != nil {
		return nil, fmt.Errorf("failed to get cook: %w", err)
	}
	if len(rows) == 0 {
		return nil, store.ErrNotFound
	}
	return cookFromDB(rows[0]), nil
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
	id := store.NewUUID()
	p := &gen.CreateCookPortionsParams{HouseholdID: c.HouseholdID, CookID: id}
	for _, portion := range portions {
		p.Ids = append(p.Ids, store.NewUUID())
		p.PersonIds = append(p.PersonIds, portion.PersonID)
		p.EatingTimeIds = append(p.EatingTimeIds, portion.EatingTimeID)
		p.Dates = append(p.Dates, store.DateToPG(portion.Date))
		p.Servings = append(p.Servings, portion.Servings)
	}
	var cooks []*gen.ListCooksRow
	var rows []*gen.ListPortionsRow
	var b store.Batch
	q := gen.New(&b)
	_, _ = q.CreateCook(ctx, &gen.CreateCookParams{
		ID:          id,
		HouseholdID: c.HouseholdID,
		RecipeID:    c.RecipeID,
		Date:        store.DateToPG(c.Date),
		Servings:    c.Servings,
		Meal:        mealToDB(c.Meal),
		Notes:       c.Notes,
		Done:        c.Done,
	})
	if len(portions) > 0 {
		_, _ = q.CreateCookPortions(ctx, p)
	}
	b.Read(func(db store.Querier) error {
		var err error
		cooks, err = gen.New(db).ListCooks(ctx, &gen.ListCooksParams{HouseholdID: c.HouseholdID, Ids: []pgtype.UUID{id}})
		return err
	})
	if len(portions) > 0 {
		b.Read(func(db store.Querier) error {
			var err error
			rows, err = gen.New(db).ListPortions(ctx, &gen.ListPortionsParams{HouseholdID: c.HouseholdID, Ids: p.Ids})
			return err
		})
	}
	if err := b.Send(ctx, r.db); err != nil {
		return nil, nil, writeError("create cook", err)
	}
	if len(cooks) == 0 {
		return nil, nil, errors.New("failed to read the created cook")
	}
	// The portions are returned in the order they were given, rather than
	// the query's.
	byID := map[pgtype.UUID]*Portion{}
	for _, row := range rows {
		byID[row.Portion.ID] = portionFromDB(row)
	}
	created := make([]*Portion, 0, len(rows))
	for _, id := range p.Ids {
		if portion, ok := byID[id]; ok {
			created = append(created, portion)
		}
	}
	return cookFromDB(cooks[0]), created, nil
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
			ID:          c.ID,
			HouseholdID: c.HouseholdID,
			RecipeID:    c.RecipeID,
			Date:        store.DateToPG(c.Date),
			Servings:    c.Servings,
			Meal:        mealToDB(c.Meal),
			Notes:       c.Notes,
			Done:        c.Done,
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
	n, err := gen.New(r.db).DeleteCook(ctx, &gen.DeleteCookParams{ID: id, HouseholdID: householdID})
	if err != nil {
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
	return staplesFromDB(staples, skips), nil
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
		if list := staplesFromDB(rows, skips); len(list) > 0 {
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
		RecipeName:       row.RecipeName,
		AssignedServings: row.AssignedServings,
		CreateTime:       c.CreateTime.Time,
		UpdateTime:       c.UpdateTime.Time,
	}
}

func portionFromDB(row *gen.ListPortionsRow) *Portion {
	p := &row.Portion
	return &Portion{
		ID:           p.ID,
		HouseholdID:  p.HouseholdID,
		PersonID:     p.PersonID,
		EatingTimeID: p.EatingTimeID,
		Date:         store.DateFromPG(p.Date),
		CookID:       p.CookID,
		Servings:     orZero(p.Servings),
		FoodID:       p.FoodID,
		Amount:       orZero(p.Amount),
		Unit:         unitFromDB(p.Unit),
		Notes:        p.Notes,
		Name:         row.Name,
		CookDate:     store.DateFromPG(row.CookDate),
		CookRecipeID: row.CookRecipeID,
		CreateTime:   p.CreateTime.Time,
		UpdateTime:   p.UpdateTime.Time,
	}
}

// staplesFromDB converts staples, in order, with their skipped dates.
func staplesFromDB(rows []*gen.ListStaplesRow, skips []*gen.StapleSkip) []*Staple {
	skipped := map[pgtype.UUID][]time.Time{}
	for _, skip := range skips {
		skipped[skip.StapleID] = append(skipped[skip.StapleID], store.DateFromPG(skip.Date))
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
			Name:         row.Name,
			SkippedDates: skipped[s.ID],
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
