// Package plan implements the PlanService: the households' weekly plans of
// cooks, portions and staples, checked against people's rules and targets.
package plan

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"slices"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/bs-tools/foodplann.ing/internal/authn"
	"github.com/bs-tools/foodplann.ing/internal/errreport"
	"github.com/bs-tools/foodplann.ing/internal/foods"
	plansv1 "github.com/bs-tools/foodplann.ing/internal/gen/plans/v1"
	"github.com/bs-tools/foodplann.ing/internal/gen/plans/v1/plansv1connect"
	"github.com/bs-tools/foodplann.ing/internal/people"
	"github.com/bs-tools/foodplann.ing/internal/plans"
	"github.com/bs-tools/foodplann.ing/internal/protomask"
	"github.com/bs-tools/foodplann.ing/internal/recipes"
	"github.com/bs-tools/foodplann.ing/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/reflect/protoreflect"
)

var _ plansv1connect.PlanServiceHandler = (*Service)(nil)

// PlanRepository stores plans.
type PlanRepository interface {
	GetPlan(ctx context.Context, householdID pgtype.UUID, from, to time.Time) (*plans.Plan, error)
	GetCook(ctx context.Context, householdID, id pgtype.UUID) (*plans.Cook, error)
	CreateCook(ctx context.Context, c *plans.Cook) (*plans.Cook, error)
	CreateCookWithPortions(ctx context.Context, c *plans.Cook, portions []*plans.Portion) (*plans.Cook, []*plans.Portion, error)
	UpdateCook(ctx context.Context, c *plans.Cook) (*plans.Cook, error)
	DeleteCook(ctx context.Context, householdID, id pgtype.UUID) error
	GetPortion(ctx context.Context, householdID, id pgtype.UUID) (*plans.Portion, error)
	CreatePortion(ctx context.Context, p *plans.Portion) (*plans.Portion, error)
	UpdatePortion(ctx context.Context, p *plans.Portion) (*plans.Portion, error)
	DeletePortion(ctx context.Context, householdID, id pgtype.UUID) error
	GetStaple(ctx context.Context, householdID, id pgtype.UUID) (*plans.Staple, error)
	ListStaples(ctx context.Context, householdID, personID pgtype.UUID) ([]*plans.Staple, error)
	CreateStaple(ctx context.Context, s *plans.Staple) (*plans.Staple, error)
	UpdateStaple(ctx context.Context, s *plans.Staple) (*plans.Staple, error)
	DeleteStaple(ctx context.Context, householdID, id pgtype.UUID) error
	SetStapleSkipped(ctx context.Context, householdID, id pgtype.UUID, date time.Time, skipped bool) (*plans.Staple, error)
}

// PeopleRepository reads the people plans are for.
type PeopleRepository interface {
	ListPeople(ctx context.Context, householdID pgtype.UUID) ([]*people.Person, error)
}

// RecipeRepository reads the recipes plans cook.
type RecipeRepository interface {
	ListRecipesByID(ctx context.Context, householdID pgtype.UUID, ids []pgtype.UUID) ([]*recipes.Recipe, error)
}

// FoodRepository reads the foods plans eat without a recipe.
type FoodRepository interface {
	ListFoodsByID(ctx context.Context, householdID pgtype.UUID, ids []pgtype.UUID) ([]*foods.Food, error)
}

// Service implements the PlanService.
type Service struct {
	log     *slog.Logger
	report  *errreport.Reporter
	plans   PlanRepository
	people  PeopleRepository
	recipes RecipeRepository
	foods   FoodRepository
}

// NewService returns a PlanService storing plans in the repository, for
// the people, recipes and foods in theirs.
func NewService(log *slog.Logger, report *errreport.Reporter, plans PlanRepository, people PeopleRepository, recipes RecipeRepository, foods FoodRepository) *Service {
	return &Service{log: log, report: report, plans: plans, people: people, recipes: recipes, foods: foods}
}

// The fields of the plan's resources updates don't change.
var (
	cookKeepFields    = []protoreflect.Name{"id", "recipe_name", "create_time", "update_time"}
	portionKeepFields = []protoreflect.Name{"id", "name", "create_time", "update_time"}
	stapleKeepFields  = []protoreflect.Name{"id", "name", "skipped_dates", "create_time", "update_time"}
)

// What NotFound errors say is missing, and how to find it.
const (
	cookNotFound    = "the cook doesn't exist in your household's plan; find cooks with GetWeek"
	portionNotFound = "the portion doesn't exist in your household's plan; find portions with GetWeek"
	stapleNotFound  = "the staple doesn't exist in your household's plan; find staples with ListStaples"
	recipeNotFound  = "the recipe doesn't exist in your household; find recipes with ListRecipes"
)

func (s *Service) GetWeek(ctx context.Context, req *plansv1.GetWeekRequest) (*plansv1.GetWeekResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	monday, err := parseDate("monday", req.GetMonday())
	if err != nil {
		return nil, err
	}
	if monday.Weekday() != time.Monday {
		previous := monday.AddDate(0, 0, -int(monday.Weekday()+6)%7)
		return nil, connect.Errorf(connect.CodeInvalidArgument,
			"monday must be a Monday: the week of %s starts on %s", store.FormatDate(monday), store.FormatDate(previous))
	}
	sunday := monday.AddDate(0, 0, 6)
	d, err := s.loadPlan(ctx, user.HouseholdID(), monday, sunday)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to get week", "")
	}
	cooks := make([]*plansv1.PlannedCook, 0, len(d.plan.Cooks))
	for _, c := range d.plan.Cooks {
		cooks = append(cooks, d.plannedCook(c))
	}
	return plansv1.GetWeekResponse_builder{
		Monday: store.FormatDate(monday),
		Days:   d.days(monday, sunday),
		Cooks:  cooks,
	}.Build(), nil
}

func (s *Service) PlanMeal(ctx context.Context, req *plansv1.PlanMealRequest) (*plansv1.PlanMealResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	recipeID, err := parseID("recipe_id", req.GetRecipeId(), "find recipes with ListRecipes")
	if err != nil {
		return nil, err
	}
	date, err := parseDate("date", req.GetDate())
	if err != nil {
		return nil, err
	}
	meal, err := mealFromProto(req.GetMeal())
	if err != nil {
		return nil, err
	}
	var personIDs []pgtype.UUID
	for _, s := range req.GetPersonIds() {
		id, err := parseID("person_ids", s, "find people with ListPeople")
		if err != nil {
			return nil, err
		}
		personIDs = append(personIDs, id)
	}
	if v := req.GetServings(); math.IsNaN(v) || v < 0 || v > maxCookServings {
		return nil, connect.Errorf(connect.CodeInvalidArgument,
			"servings must be from 0 to %d, with 0 for the servings planned for people", maxCookServings)
	}
	notes, err := checkNotes("notes", req.GetNotes(), maxCookNotesLength)
	if err != nil {
		return nil, err
	}

	// Leftovers can go to the next day, which is read too, for what's
	// already planned there.
	d, err := s.loadPlan(ctx, user.HouseholdID(), date, date.AddDate(0, 0, 1), recipeID)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to read plan", "")
	}
	recipe, ok := d.recipes[recipeID]
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, recipeNotFound)
	}
	eaters, err := mealEaters(d.people, meal, personIDs)
	if err != nil {
		return nil, err
	}
	split, err := plans.SplitMeal(&plans.SplitInput{
		Date:            date,
		Meal:            meal,
		People:          eaters,
		GramsPerServing: d.recipeNutrition[recipeID].GramsPerServing,
		PlannedGrams:    d.plannedGrams(),
		Servings:        req.GetServings(),
	})
	if tooFew, ok := errors.AsType[*plans.TooFewServingsError](err); ok {
		return nil, connect.Errorf(connect.CodeInvalidArgument,
			"servings must be at least %v, the servings planned for people, or 0 to cook just those", math.Ceil(tooFew.Needed))
	}
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to split meal", "")
	}
	portions := make([]*plans.Portion, 0, len(split.Shares))
	for _, share := range split.Shares {
		portions = append(portions, &plans.Portion{
			PersonID:     share.PersonID,
			EatingTimeID: share.EatingTimeID,
			Date:         share.Date,
			Servings:     share.Servings,
		})
	}
	cook, created, err := s.plans.CreateCookWithPortions(ctx, &plans.Cook{
		HouseholdID: user.HouseholdID(),
		RecipeID:    recipe.ID,
		Date:        date,
		Servings:    split.Servings,
		Meal:        meal,
		Notes:       notes,
	}, portions)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to plan meal", recipeNotFound)
	}

	// The warnings are those of the eating times the portions are at, with
	// what was planned there already, and of the cook.
	d.plan.Cooks = append(d.plan.Cooks, cook)
	for _, p := range created {
		d.addItem(&item{portion: p, date: p.Date})
	}
	var warnings []*plansv1.Warning
	var checked []plans.Slot
	for _, p := range created {
		slot := plans.Slot{PersonID: p.PersonID, EatingTimeID: p.EatingTimeID, Date: p.Date}
		if slices.Contains(checked, slot) {
			continue
		}
		checked = append(checked, slot)
		eater := eaters[slices.IndexFunc(eaters, func(e *people.Person) bool { return e.ID == p.PersonID })]
		warnings = append(warnings, d.slot(eater, eater.EatingTime(p.EatingTimeID), p.Date).proto.GetWarnings()...)
	}
	warnings = append(warnings, d.plannedCook(cook).GetWarnings()...)
	result := make([]*plansv1.Portion, 0, len(created))
	for _, p := range created {
		result = append(result, portionToProto(p))
	}
	return plansv1.PlanMealResponse_builder{
		Cook:               cookToProto(cook),
		Portions:           result,
		UnassignedServings: split.Unassigned,
		Warnings:           warnings,
	}.Build(), nil
}

// mealEaters returns the people who eat a shared meal: those of personIDs,
// or everyone with an eating time for it if personIDs is empty.
func mealEaters(all []*people.Person, meal people.Meal, personIDs []pgtype.UUID) ([]*people.Person, error) {
	eatsMeal := func(p *people.Person) bool {
		return slices.ContainsFunc(p.EatingTimes, func(et *people.EatingTime) bool { return et.Meal == meal })
	}
	var eaters []*people.Person
	if len(personIDs) == 0 {
		for _, p := range all {
			if eatsMeal(p) {
				eaters = append(eaters, p)
			}
		}
		if len(eaters) == 0 {
			return nil, connect.Errorf(connect.CodeFailedPrecondition,
				"nobody eats from the shared %s: set an eating time's meal to it with UpdatePerson, or plan with CreateCook and CreatePortion", meal)
		}
		return eaters, nil
	}
	for _, id := range personIDs {
		i := slices.IndexFunc(all, func(p *people.Person) bool { return p.ID == id })
		if i < 0 {
			return nil, connect.NewError(connect.CodeInvalidArgument, "person_ids has someone who isn't in your household; find people with ListPeople")
		}
		p := all[i]
		if !eatsMeal(p) {
			return nil, connect.Errorf(connect.CodeInvalidArgument,
				"%s has no eating time for the shared %s: set one's meal to it with UpdatePerson, or plan their portion with CreatePortion", p.Name, meal)
		}
		if !slices.Contains(eaters, p) {
			eaters = append(eaters, p)
		}
	}
	return eaters, nil
}

// plannedGrams returns the grams planned at each slot of the plan.
func (d *planData) plannedGrams() map[plans.Slot]float64 {
	grams := map[plans.Slot]float64{}
	for slot, items := range d.items {
		for _, it := range items {
			e := d.eaten(it)
			grams[slot] += e.amount.Grams
		}
	}
	return grams
}

func (s *Service) CreateCook(ctx context.Context, req *plansv1.CreateCookRequest) (*plansv1.CreateCookResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	c, err := cookFromProto(req.GetCook())
	if err != nil {
		return nil, err
	}
	c.HouseholdID = user.HouseholdID()
	created, err := s.plans.CreateCook(ctx, c)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to create cook", "")
	}
	return plansv1.CreateCookResponse_builder{Cook: cookToProto(created)}.Build(), nil
}

func (s *Service) UpdateCook(ctx context.Context, req *plansv1.UpdateCookRequest) (*plansv1.UpdateCookResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	id, err := parseID("cook.id", req.GetCook().GetId(), "find cooks with GetWeek")
	if err != nil {
		return nil, err
	}
	existing, err := s.plans.GetCook(ctx, user.HouseholdID(), id)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to get cook", cookNotFound)
	}
	merged := cookToProto(existing)
	if err := protomask.Apply(merged, req.GetCook(), req.GetUpdateMask(), cookKeepFields...); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err.Error())
	}
	c, err := cookFromProto(merged)
	if err != nil {
		return nil, err
	}
	c.ID = existing.ID
	c.HouseholdID = existing.HouseholdID
	updated, err := s.plans.UpdateCook(ctx, c)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to update cook", cookNotFound)
	}
	return plansv1.UpdateCookResponse_builder{Cook: cookToProto(updated)}.Build(), nil
}

func (s *Service) DeleteCook(ctx context.Context, req *plansv1.DeleteCookRequest) (*plansv1.DeleteCookResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	id, err := parseID("id", req.GetId(), "find cooks with GetWeek")
	if err != nil {
		return nil, err
	}
	if err := s.plans.DeleteCook(ctx, user.HouseholdID(), id); err != nil {
		return nil, s.toConnectError(ctx, err, "failed to delete cook", cookNotFound)
	}
	return plansv1.DeleteCookResponse_builder{}.Build(), nil
}

func (s *Service) CreatePortion(ctx context.Context, req *plansv1.CreatePortionRequest) (*plansv1.CreatePortionResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	p, err := portionFromProto(req.GetPortion())
	if err != nil {
		return nil, err
	}
	p.HouseholdID = user.HouseholdID()
	created, err := s.plans.CreatePortion(ctx, p)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to create portion", "")
	}
	return plansv1.CreatePortionResponse_builder{Portion: portionToProto(created)}.Build(), nil
}

func (s *Service) UpdatePortion(ctx context.Context, req *plansv1.UpdatePortionRequest) (*plansv1.UpdatePortionResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	id, err := parseID("portion.id", req.GetPortion().GetId(), "find portions with GetWeek")
	if err != nil {
		return nil, err
	}
	existing, err := s.plans.GetPortion(ctx, user.HouseholdID(), id)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to get portion", portionNotFound)
	}
	merged := portionToProto(existing)
	if err := protomask.Apply(merged, req.GetPortion(), req.GetUpdateMask(), portionKeepFields...); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err.Error())
	}
	p, err := portionFromProto(merged)
	if err != nil {
		return nil, err
	}
	p.ID = existing.ID
	p.HouseholdID = existing.HouseholdID
	updated, err := s.plans.UpdatePortion(ctx, p)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to update portion", portionNotFound)
	}
	return plansv1.UpdatePortionResponse_builder{Portion: portionToProto(updated)}.Build(), nil
}

func (s *Service) DeletePortion(ctx context.Context, req *plansv1.DeletePortionRequest) (*plansv1.DeletePortionResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	id, err := parseID("id", req.GetId(), "find portions with GetWeek")
	if err != nil {
		return nil, err
	}
	if err := s.plans.DeletePortion(ctx, user.HouseholdID(), id); err != nil {
		return nil, s.toConnectError(ctx, err, "failed to delete portion", portionNotFound)
	}
	return plansv1.DeletePortionResponse_builder{}.Build(), nil
}

func (s *Service) CreateStaple(ctx context.Context, req *plansv1.CreateStapleRequest) (*plansv1.CreateStapleResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	st, err := stapleFromProto(req.GetStaple())
	if err != nil {
		return nil, err
	}
	st.HouseholdID = user.HouseholdID()
	created, err := s.plans.CreateStaple(ctx, st)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to create staple", "")
	}
	return plansv1.CreateStapleResponse_builder{Staple: stapleToProto(created)}.Build(), nil
}

func (s *Service) ListStaples(ctx context.Context, req *plansv1.ListStaplesRequest) (*plansv1.ListStaplesResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	var personID pgtype.UUID
	if req.GetPersonId() != "" {
		var err error
		personID, err = parseID("person_id", req.GetPersonId(), "find people with ListPeople")
		if err != nil {
			return nil, err
		}
	}
	list, err := s.plans.ListStaples(ctx, user.HouseholdID(), personID)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to list staples", "")
	}
	result := make([]*plansv1.Staple, 0, len(list))
	for _, st := range list {
		result = append(result, stapleToProto(st))
	}
	return plansv1.ListStaplesResponse_builder{Staples: result}.Build(), nil
}

func (s *Service) UpdateStaple(ctx context.Context, req *plansv1.UpdateStapleRequest) (*plansv1.UpdateStapleResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	id, err := parseID("staple.id", req.GetStaple().GetId(), "find staples with ListStaples")
	if err != nil {
		return nil, err
	}
	existing, err := s.plans.GetStaple(ctx, user.HouseholdID(), id)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to get staple", stapleNotFound)
	}
	merged := stapleToProto(existing)
	if err := protomask.Apply(merged, req.GetStaple(), req.GetUpdateMask(), stapleKeepFields...); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err.Error())
	}
	st, err := stapleFromProto(merged)
	if err != nil {
		return nil, err
	}
	st.ID = existing.ID
	st.HouseholdID = existing.HouseholdID
	updated, err := s.plans.UpdateStaple(ctx, st)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to update staple", stapleNotFound)
	}
	return plansv1.UpdateStapleResponse_builder{Staple: stapleToProto(updated)}.Build(), nil
}

func (s *Service) DeleteStaple(ctx context.Context, req *plansv1.DeleteStapleRequest) (*plansv1.DeleteStapleResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	id, err := parseID("id", req.GetId(), "find staples with ListStaples")
	if err != nil {
		return nil, err
	}
	if err := s.plans.DeleteStaple(ctx, user.HouseholdID(), id); err != nil {
		return nil, s.toConnectError(ctx, err, "failed to delete staple", stapleNotFound)
	}
	return plansv1.DeleteStapleResponse_builder{}.Build(), nil
}

func (s *Service) SkipStaple(ctx context.Context, req *plansv1.SkipStapleRequest) (*plansv1.SkipStapleResponse, error) {
	st, err := s.setStapleSkipped(ctx, req.GetId(), req.GetDate(), true)
	if err != nil {
		return nil, err
	}
	return plansv1.SkipStapleResponse_builder{Staple: st}.Build(), nil
}

func (s *Service) UnskipStaple(ctx context.Context, req *plansv1.UnskipStapleRequest) (*plansv1.UnskipStapleResponse, error) {
	st, err := s.setStapleSkipped(ctx, req.GetId(), req.GetDate(), false)
	if err != nil {
		return nil, err
	}
	return plansv1.UnskipStapleResponse_builder{Staple: st}.Build(), nil
}

// setStapleSkipped skips a staple on a date, or stops skipping it.
func (s *Service) setStapleSkipped(ctx context.Context, rawID, rawDate string, skipped bool) (*plansv1.Staple, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	id, err := parseID("id", rawID, "find staples with ListStaples")
	if err != nil {
		return nil, err
	}
	date, err := parseDate("date", rawDate)
	if err != nil {
		return nil, err
	}
	st, err := s.plans.SetStapleSkipped(ctx, user.HouseholdID(), id, date, skipped)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to skip staple", stapleNotFound)
	}
	return stapleToProto(st), nil
}

// referenceMessages say what to do about a reference to something that
// isn't the household's, by field.
var referenceMessages = map[string]string{
	"recipe_id":      "the recipe isn't one of your household's recipes; find them with ListRecipes",
	"person_id":      "person_id isn't one of your household's people; find them with ListPeople",
	"eating_time_id": "eating_time_id isn't one of the person's eating times; find them with GetPerson",
	"cook_id":        "the cook isn't in your household's plan; find cooks with GetWeek, or create one with CreateCook",
	"food_id":        "the food isn't one of your household's foods; find them with ListFoods",
	"id":             stapleNotFound,
}

// toConnectError maps an error of the repositories to a Connect error, with
// notFound as the message of store.ErrNotFound.
func (s *Service) toConnectError(ctx context.Context, err error, msg, notFound string) error {
	if connectErr, ok := errors.AsType[*connect.Error](err); ok {
		return connectErr
	}
	if ref, ok := errors.AsType[*plans.ReferenceError](err); ok {
		if ref.Field == "id" {
			return connect.NewError(connect.CodeNotFound, referenceMessages[ref.Field])
		}
		return connect.NewError(connect.CodeInvalidArgument, referenceMessages[ref.Field])
	}
	if errors.Is(err, store.ErrNotFound) && notFound != "" {
		return connect.NewError(connect.CodeNotFound, notFound)
	}
	s.report.Report(ctx, msg, err)
	return connect.NewError(connect.CodeInternal, msg).WithCause(err)
}
