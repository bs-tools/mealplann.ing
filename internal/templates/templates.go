// Package templates stores the households' plan templates: plans of some
// days, independent of dates, to plan again from a start date. It also
// applies a template to people and a date, and makes a template from a
// plan, as pure functions.
package templates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/bs-tools/mealplann.ing/internal/people"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/bs-tools/mealplann.ing/internal/templates/internal/gen"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// ErrNameTaken is returned when a household already has a template of the
// name.
var ErrNameTaken = errors.New("the household already has a template of this name")

// Template is a household's plan template.
type Template struct {
	ID          pgtype.UUID
	HouseholdID pgtype.UUID
	Name        string
	// Source is where it comes from, e.g. "Dietitian, week 1".
	Source string
	Notes  string
	// Days is how many days it plans.
	Days int32
	// Slots are the times of day its entries are eaten at, in the order of
	// a day.
	Slots []*Slot
	// Entries are what people eat, by day.
	Entries []*Entry
	// Batches are the entries cooked together, as one cook.
	Batches    []*Batch
	CreateTime time.Time
	UpdateTime time.Time
}

// Slot is a time of day of a template, which maps to an eating time of
// each person: the one of its shared meal, or else the one of its kind in
// the same position among the person's eating times of that kind without a
// shared meal. See EatingTimeFor.
type Slot struct {
	// Key is how entries refer to it, e.g. "dinner" or "small-2".
	Key  string      `json:"key"`
	Name string      `json:"name"`
	Kind people.Kind `json:"kind"`
	// Meal is the shared meal it eats from, MealNone if none.
	Meal people.Meal `json:"meal,omitempty"`
}

// Entry is a recipe people eat at a slot on a day of a template.
type Entry struct {
	// Key is how batches refer to it, unique in the template.
	Key string `json:"key"`
	// Day is the day, from 0 for the first.
	Day      int32       `json:"day"`
	SlotKey  string      `json:"slot"`
	RecipeID pgtype.UUID `json:"recipe_id"`
	// Servings is how many servings each person eats, unless they have
	// their own in PersonServings.
	Servings       float64          `json:"servings"`
	PersonServings []PersonServings `json:"person_servings,omitempty"`
}

// PersonServings is how many servings of an entry a person eats, 0 if they
// don't eat it.
type PersonServings struct {
	PersonID pgtype.UUID `json:"person_id"`
	Servings float64     `json:"servings"`
}

// ServingsFor returns how many servings of the entry a person eats.
func (e *Entry) ServingsFor(personID pgtype.UUID) float64 {
	for _, ps := range e.PersonServings {
		if ps.PersonID == personID {
			return ps.Servings
		}
	}
	return e.Servings
}

// Batch is entries of the same recipe cooked together on a day, so that
// those on later days are leftovers.
type Batch struct {
	// Day is the day it's cooked, from 0 for the first, on or before its
	// entries' days.
	Day int32 `json:"day"`
	// Servings is how many servings to cook, 0 for the servings its entries
	// plan.
	Servings  float64  `json:"servings,omitempty"`
	EntryKeys []string `json:"entries"`
}

// Slot returns the template's slot with the key, or nil.
func (t *Template) Slot(key string) *Slot {
	for _, s := range t.Slots {
		if s.Key == key {
			return s
		}
	}
	return nil
}

// Entry returns the template's entry with the key, or nil.
func (t *Template) Entry(key string) *Entry {
	for _, e := range t.Entries {
		if e.Key == key {
			return e
		}
	}
	return nil
}

// EatingTimeFor returns the eating time of a person a slot of the template
// maps to, or nil if none does: the person's eating time of the slot's
// shared meal, or, for a slot without one, their eating time of the same
// kind in the same position among those without a shared meal. So the
// second small-meal slot is each person's second small eating time.
func (t *Template) EatingTimeFor(slot *Slot, p *people.Person) *people.EatingTime {
	if slot.Meal != people.MealNone {
		for _, et := range p.EatingTimes {
			if et.Meal == slot.Meal {
				return et
			}
		}
		return nil
	}
	position := 0
	for _, s := range t.Slots {
		if s == slot {
			break
		}
		if s.Meal == people.MealNone && s.Kind == slot.Kind {
			position++
		}
	}
	for _, et := range p.EatingTimes {
		if et.Meal != people.MealNone || et.Kind != slot.Kind {
			continue
		}
		if position == 0 {
			return et
		}
		position--
	}
	return nil
}

// body is what's stored of a template as JSON.
type body struct {
	Slots   []*Slot  `json:"slots"`
	Entries []*Entry `json:"entries"`
	Batches []*Batch `json:"batches"`
}

// Repository stores plan templates.
type Repository struct {
	log     *slog.Logger
	db      *store.DB
	querier *gen.Queries
}

// NewRepository returns a repository of the plan templates in the
// database.
func NewRepository(log *slog.Logger, db *store.DB) *Repository {
	return &Repository{log: log, db: db, querier: gen.New(db)}
}

// CreateTemplate creates a template in its household, and returns it. It
// returns ErrNameTaken if the household has one of the name.
func (r *Repository) CreateTemplate(ctx context.Context, t *Template) (*Template, error) {
	b, err := bodyOf(t)
	if err != nil {
		return nil, err
	}
	row, err := r.querier.CreateTemplate(ctx, &gen.CreateTemplateParams{
		ID:          store.NewUUID(),
		HouseholdID: t.HouseholdID,
		Name:        t.Name,
		Source:      t.Source,
		Notes:       t.Notes,
		Days:        t.Days,
		Body:        b,
	})
	if err != nil {
		return nil, writeError("create", err)
	}
	return templateFromDB(row)
}

// GetTemplate gets a household's template. It returns store.ErrNotFound
// if the household has no such template.
func (r *Repository) GetTemplate(ctx context.Context, householdID, id pgtype.UUID) (*Template, error) {
	row, err := r.querier.GetTemplate(ctx, &gen.GetTemplateParams{ID: id, HouseholdID: householdID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get template: %w", err)
	}
	return templateFromDB(row)
}

// ListTemplates lists a household's templates, ordered by name.
func (r *Repository) ListTemplates(ctx context.Context, householdID pgtype.UUID) ([]*Template, error) {
	rows, err := r.querier.ListTemplates(ctx, householdID)
	if err != nil {
		return nil, fmt.Errorf("failed to list templates: %w", err)
	}
	list := make([]*Template, 0, len(rows))
	for _, row := range rows {
		t, err := templateFromDB(row)
		if err != nil {
			return nil, err
		}
		list = append(list, t)
	}
	return list, nil
}

// UpdateTemplate replaces a household's template, other than its times. It
// returns store.ErrNotFound if the household has no such template, and
// ErrNameTaken if it has another of the name.
func (r *Repository) UpdateTemplate(ctx context.Context, t *Template) (*Template, error) {
	b, err := bodyOf(t)
	if err != nil {
		return nil, err
	}
	row, err := r.querier.UpdateTemplate(ctx, &gen.UpdateTemplateParams{
		ID:          t.ID,
		HouseholdID: t.HouseholdID,
		Name:        t.Name,
		Source:      t.Source,
		Notes:       t.Notes,
		Days:        t.Days,
		Body:        b,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, writeError("update", err)
	}
	return templateFromDB(row)
}

// DeleteTemplate deletes a household's template. It returns
// store.ErrNotFound if the household has no such template.
func (r *Repository) DeleteTemplate(ctx context.Context, householdID, id pgtype.UUID) error {
	n, err := r.querier.DeleteTemplate(ctx, &gen.DeleteTemplateParams{ID: id, HouseholdID: householdID})
	if err != nil {
		return fmt.Errorf("failed to delete template: %w", err)
	}
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

func writeError(what string, err error) error {
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == pgerrcode.UniqueViolation &&
		pgErr.ConstraintName == "plan_templates_household_id_lower_name" {
		return ErrNameTaken
	}
	return fmt.Errorf("failed to %s template: %w", what, err)
}

func bodyOf(t *Template) (json.RawMessage, error) {
	b := body{Slots: t.Slots, Entries: t.Entries, Batches: t.Batches}
	if b.Slots == nil {
		b.Slots = []*Slot{}
	}
	if b.Entries == nil {
		b.Entries = []*Entry{}
	}
	if b.Batches == nil {
		b.Batches = []*Batch{}
	}
	data, err := json.Marshal(b)
	if err != nil {
		return nil, fmt.Errorf("failed to encode template: %w", err)
	}
	return data, nil
}

func templateFromDB(row *gen.PlanTemplate) (*Template, error) {
	var b body
	if err := json.Unmarshal(row.Body, &b); err != nil {
		return nil, fmt.Errorf("failed to decode template %s: %w", store.UUIDToString(row.ID), err)
	}
	return &Template{
		ID:          row.ID,
		HouseholdID: row.HouseholdID,
		Name:        row.Name,
		Source:      row.Source,
		Notes:       row.Notes,
		Days:        row.Days,
		Slots:       b.Slots,
		Entries:     b.Entries,
		Batches:     b.Batches,
		CreateTime:  row.CreateTime.Time,
		UpdateTime:  row.UpdateTime.Time,
	}, nil
}
