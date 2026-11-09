// Package plan implements the PlanService: the households' weekly plans of
// cooks, portions and staples, checked against people's rules and targets.
// It also implements the SuggestionService, which scores recipes for the
// plan's open slots, as it reads the plan and computes its nutrition the
// same way.
package plan

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/bs-tools/mealplann.ing/internal/services/amounts"

	"connectrpc.com/connect/v2"
	"github.com/bs-tools/mealplann.ing/internal/authn"
	"github.com/bs-tools/mealplann.ing/internal/errreport"
	"github.com/bs-tools/mealplann.ing/internal/foods"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	"github.com/bs-tools/mealplann.ing/internal/gen/plans/v1/plansv1connect"
	"github.com/bs-tools/mealplann.ing/internal/gen/suggestions/v1/suggestionsv1connect"
	"github.com/bs-tools/mealplann.ing/internal/nutrition"
	"github.com/bs-tools/mealplann.ing/internal/pantry"
	"github.com/bs-tools/mealplann.ing/internal/people"
	"github.com/bs-tools/mealplann.ing/internal/plans"
	"github.com/bs-tools/mealplann.ing/internal/protomask"
	"github.com/bs-tools/mealplann.ing/internal/recipes"
	"github.com/bs-tools/mealplann.ing/internal/services/summary"
	"github.com/bs-tools/mealplann.ing/internal/shopping"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/reflect/protoreflect"
)

var (
	_ plansv1connect.PlanServiceHandler             = (*Service)(nil)
	_ suggestionsv1connect.SuggestionServiceHandler = (*Service)(nil)
)

// PlanRepository stores plans.
type PlanRepository interface {
	GetPlan(ctx context.Context, householdID pgtype.UUID, from, to time.Time) (*plans.Plan, error)
	GetCook(ctx context.Context, householdID, id pgtype.UUID) (*plans.Cook, error)
	CreateCook(ctx context.Context, c *plans.Cook) (*plans.Cook, error)
	CreateCookWithPortions(ctx context.Context, c *plans.Cook, portions []*plans.Portion) (*plans.Cook, []*plans.Portion, error)
	CreateCooksWithPortions(ctx context.Context, list []*plans.CookWithPortions) ([]*plans.Cook, [][]*plans.Portion, error)
	UpdateCook(ctx context.Context, c *plans.Cook) (*plans.Cook, error)
	ListFrozenCooks(ctx context.Context, householdID pgtype.UUID) ([]*plans.FrozenCook, error)
	SetCookSupply(ctx context.Context, householdID pgtype.UUID, supply *plans.Supply, previous pgtype.UUID) error
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

// RecipeRepository reads the recipes plans cook, and those suggestions
// score.
type RecipeRepository interface {
	ListRecipes(ctx context.Context, householdID pgtype.UUID, filter recipes.ListFilter) ([]*recipes.Recipe, []string, error)
	ListRecipesByID(ctx context.Context, householdID pgtype.UUID, ids []pgtype.UUID) ([]*recipes.Recipe, error)
}

// FoodRepository reads the foods plans eat without a recipe.
type FoodRepository interface {
	ListFoodsByID(ctx context.Context, householdID pgtype.UUID, ids []pgtype.UUID) ([]*foods.Food, error)
}

// PantryRepository takes the ingredients of cooks marked done, and the
// food of staples marked eaten, out of the pantry, and puts them back. Suggestions read what's in it.
type PantryRepository interface {
	ListItems(ctx context.Context, householdID pgtype.UUID) ([]*pantry.Item, error)
	UseForCook(ctx context.Context, householdID, cookID pgtype.UUID, uses []pantry.Use) ([]pantry.Deduction, error)
	RestoreForCook(ctx context.Context, householdID, cookID pgtype.UUID) ([]pantry.Use, error)
	UseForStaple(ctx context.Context, householdID, stapleID pgtype.UUID, date time.Time, uses []pantry.Use) ([]pantry.Deduction, error)
	RestoreForStaple(ctx context.Context, householdID, stapleID pgtype.UUID, date time.Time) ([]pantry.Use, error)
}

// ShoppingRepository reads the shopping lists, whose shop suggestions
// plan for.
type ShoppingRepository interface {
	ListLists(ctx context.Context, householdID pgtype.UUID, limit, offset int32) ([]*shopping.List, error)
}

// Service implements the PlanService, the PlanTemplateService and the
// SuggestionService.
type Service struct {
	log      *slog.Logger
	report   *errreport.Reporter
	plans    PlanRepository
	people   PeopleRepository
	recipes  RecipeRepository
	foods    FoodRepository
	pantry   PantryRepository
	shopping ShoppingRepository
	// templates stores the plan templates.
	templates TemplateRepository
	// nowFn returns the current time, which today's date is taken from.
	nowFn func() time.Time
}

// NewService returns a PlanService and SuggestionService storing plans in
// the repository, for the people, recipes and foods in theirs, taking what
// cooks use out of the pantry, and suggesting for the shopping lists'
// shops.
func NewService(log *slog.Logger, report *errreport.Reporter, plans PlanRepository, people PeopleRepository, recipes RecipeRepository, foods FoodRepository, pantry PantryRepository, shopping ShoppingRepository, templates TemplateRepository) *Service {
	return &Service{
		log:       log,
		report:    report,
		plans:     plans,
		people:    people,
		recipes:   recipes,
		foods:     foods,
		pantry:    pantry,
		shopping:  shopping,
		templates: templates,
		nowFn:     time.Now,
	}
}

// The fields of the plan's resources updates don't change.
var (
	cookKeepFields    = []protoreflect.Name{"id", "recipe_name", "create_time", "update_time"}
	portionKeepFields = []protoreflect.Name{"id", "name", "create_time", "update_time"}
	stapleKeepFields  = []protoreflect.Name{"id", "name", "skipped_dates", "create_time", "update_time"}
)

// maxPrepAheadDays is the most days a recipe starts before it's cooked,
// as recipes allow.
const maxPrepAheadDays = 7

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
	// The week after is read too, as its cooks can start this week, like
	// beans soaked the night before.
	d, err := s.loadPlan(ctx, user.HouseholdID(), monday, sunday.AddDate(0, 0, maxPrepAheadDays))
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to get week", "")
	}
	cooks := make([]*plansv1.PlannedCook, 0, len(d.plan.Cooks))
	for _, c := range d.plan.Cooks {
		if !c.Date.After(sunday) {
			cooks = append(cooks, d.plannedCook(c))
		}
	}
	resp := plansv1.GetWeekResponse_builder{
		Monday: store.FormatDate(monday),
		Days:   d.days(monday, sunday),
		Cooks:  cooks,
	}.Build()
	if req.GetSummary() {
		summary.Summarize(resp)
	}
	return resp, nil
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
	if err := checkNotDraft(recipe); err != nil {
		return nil, err
	}
	eaters, err := mealEaters(d.people, meal, personIDs)
	if err != nil {
		return nil, err
	}
	// People with a staple at the meal eat it instead, unless they're
	// asked for.
	if len(personIDs) == 0 {
		if eaters, _ = withoutStapled(eaters, d.plan.Staples, meal, date); len(eaters) == 0 {
			return nil, allStapled(meal, date)
		}
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
	// The cooks of the recipe's components are planned with it.
	cooks, createdPortions, err := s.plans.CreateCooksWithPortions(ctx, withSuppliers([]*plans.CookWithPortions{{
		Cook: &plans.Cook{
			HouseholdID: user.HouseholdID(),
			RecipeID:    recipe.ID,
			Date:        date,
			Servings:    split.Servings,
			Meal:        meal,
			Notes:       notes,
		},
		Portions: portions,
	}}, d.recipes))
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to plan meal", recipeNotFound)
	}
	cook, created := cooks[0], createdPortions[0]

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

// stapledAt returns the staples a person has instead of a shared meal on
// a date, at their eating time for it: those that replace the meal, like
// their own breakfast, not those had with it, like a cup of tea.
func stapledAt(staples []*plans.Staple, p *people.Person, meal people.Meal, date time.Time) []*plans.Staple {
	var found []*plans.Staple
	for _, et := range p.EatingTimes {
		if et.Meal != meal {
			continue
		}
		for _, s := range staples {
			if s.ReplacesMeal && s.PersonID == p.ID && s.EatingTimeID == et.ID && s.OccursOn(date) {
				found = append(found, s)
			}
		}
	}
	return found
}

// withoutStapled returns the people who eat a shared meal on a date but
// have no staple that replaces it: those with one eat it instead, so the
// meal isn't planned for them. It also returns the names of the staples
// of those left out.
func withoutStapled(eaters []*people.Person, staples []*plans.Staple, meal people.Meal, date time.Time) ([]*people.Person, []string) {
	kept := make([]*people.Person, 0, len(eaters))
	var names []string
	for _, p := range eaters {
		found := stapledAt(staples, p, meal, date)
		if len(found) == 0 {
			kept = append(kept, p)
			continue
		}
		for _, s := range found {
			if !slices.Contains(names, s.Name) {
				names = append(names, s.Name)
			}
		}
	}
	return kept, names
}

// allStapled says that everyone who eats a shared meal on a date has a
// staple that replaces it, so it's not planned for anyone.
func allStapled(meal people.Meal, date time.Time) error {
	return connect.Errorf(connect.CodeFailedPrecondition,
		"everyone who eats the shared %s on %s has a staple that replaces it, which they eat instead: skip a staple on that date with SkipStaple, or give person_ids to plan the meal for them too",
		meal, store.FormatDate(date))
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
	if req.GetFromStock() {
		c.Done = true
	}
	// The cooks of the recipe's components are created with it, so its
	// recipe is read first, with them.
	list, err := s.recipes.ListRecipesByID(ctx, c.HouseholdID, []pgtype.UUID{c.RecipeID})
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to read recipe", "")
	}
	if len(list) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, referenceMessages["recipe_id"])
	}
	if err := checkNotDraft(list[0]); err != nil {
		return nil, err
	}
	toCreate := []*plans.CookWithPortions{{Cook: c}}
	if !req.GetFromStock() {
		toCreate = withSuppliers(toCreate, map[pgtype.UUID]*recipes.Recipe{list[0].ID: list[0]})
	}
	cooks, _, err := s.plans.CreateCooksWithPortions(ctx, toCreate)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to create cook", "")
	}
	created := cooks[0]
	var changes []*plansv1.PantryChange
	if created.Done && !req.GetFromStock() {
		uses, names, err := s.cookUses(ctx, created, nil)
		if err == nil {
			changes, err = s.useForCook(ctx, created, uses, names, false)
		}
		if err != nil {
			return nil, s.toConnectError(ctx, err, "failed to take the cook's ingredients out of the pantry", cookNotFound)
		}
	}
	return plansv1.CreateCookResponse_builder{Cook: cookToProto(created), PantryChanges: changes}.Build(), nil
}

// checkNotDraft fails if a recipe is a draft, which isn't planned until
// it's finished.
func checkNotDraft(r *recipes.Recipe) error {
	if r.Draft {
		return connect.Errorf(connect.CodeFailedPrecondition,
			"%s is a draft: finish it, matching its ingredients to foods, and save it with draft false before planning it", r.Name)
	}
	return nil
}

// foodUse is what was actually used of a food when cooking (see
// plansv1.FoodUse), parsed.
type foodUse struct {
	foodID, substituteID pgtype.UUID
	// amount and unit are what was used, if hasAmount.
	hasAmount bool
	amount    float64
	unit      nutrition.Unit
	skipped   bool
}

// foodUsesFromProto converts and checks what was actually used of foods.
func foodUsesFromProto(list []*plansv1.FoodUse) ([]foodUse, error) {
	if len(list) > maxFoodUses {
		return nil, connect.Errorf(connect.CodeInvalidArgument, "food_uses can have at most %d foods", maxFoodUses)
	}
	uses := make([]foodUse, 0, len(list))
	for i, p := range list {
		field := fmt.Sprintf("food_uses[%d]", i)
		id, err := parseID(field+".food_id", p.GetFoodId(), "find the recipe's foods with RecipeService.GetRecipe")
		if err != nil {
			return nil, err
		}
		if slices.ContainsFunc(uses, func(u foodUse) bool { return u.foodID == id }) {
			return nil, connect.Errorf(connect.CodeInvalidArgument, "%s gives a food a second time: give one use per food", field)
		}
		u := foodUse{foodID: id, skipped: p.GetSkipped()}
		if p.GetSubstituteFoodId() != "" {
			if u.substituteID, err = parseID(field+".substitute_food_id", p.GetSubstituteFoodId(), "find foods with ListFoods"); err != nil {
				return nil, err
			}
		}
		if p.HasAmount() {
			amount, unit, _, ok := amounts.FromProto(p.GetAmount(), p.GetUnit())
			switch {
			case !ok:
				return nil, connect.Errorf(connect.CodeInvalidArgument,
					"%s.unit is required with amount: %s", field, amounts.UnitsHelp)
			case math.IsNaN(amount) || amount < 0 || amount > maxFoodAmount:
				return nil, connect.Errorf(connect.CodeInvalidArgument, "%s.amount must be from 0 to %d g, ml or pieces", field, maxFoodAmount)
			}
			u.hasAmount, u.amount, u.unit = true, amount, unit
		}
		if u.skipped && (u.hasAmount || u.substituteID.Valid) {
			return nil, connect.Errorf(connect.CodeInvalidArgument,
				"%s is skipped, so it takes no amount or substitute_food_id: leave them out, or skipped unset", field)
		}
		uses = append(uses, u)
	}
	return uses, nil
}

// cookUses returns what a cook marked done takes out of the pantry: its
// recipe's ingredients scaled to its servings, without the optional ones,
// as changed by what was actually used, with the foods' names. Ingredients
// that can't be weighed are left in, as the pantry is in grams, but an
// amount actually used that can't be weighed fails, saying why.
func (s *Service) cookUses(ctx context.Context, c *plans.Cook, actual []foodUse) ([]pantry.Use, map[pgtype.UUID]string, error) {
	list, err := s.recipes.ListRecipesByID(ctx, c.HouseholdID, []pgtype.UUID{c.RecipeID})
	if err != nil || len(list) == 0 {
		return nil, nil, err
	}
	names := map[pgtype.UUID]string{}
	// grams are what the recipe uses of each food, in order.
	grams := map[pgtype.UUID]float64{}
	var order []pgtype.UUID
	// Components other cooks supply are taken out by those cooks.
	for _, in := range list[0].IngredientsWithout(c.Servings, shopping.SuppliedComponents(c)) {
		g, ok := in.Food.Grams(in.Amount, in.Unit)
		if !ok {
			continue
		}
		if _, seen := grams[in.FoodID]; !seen {
			order = append(order, in.FoodID)
		}
		names[in.FoodID] = in.FoodName
		grams[in.FoodID] += g
	}
	// The foods used instead, or as well, are read for their names and to
	// weigh their amounts.
	var ids []pgtype.UUID
	for _, u := range actual {
		ids = append(ids, u.foodID)
		if u.substituteID.Valid {
			ids = append(ids, u.substituteID)
		}
	}
	byID := map[pgtype.UUID]*foods.Food{}
	if len(ids) > 0 {
		found, err := s.foods.ListFoodsByID(ctx, c.HouseholdID, ids)
		if err != nil {
			return nil, nil, err
		}
		for _, f := range found {
			byID[f.ID] = f
		}
	}
	uses := make([]pantry.Use, 0, len(order))
	replaced := map[pgtype.UUID]bool{}
	var extra []pantry.Use
	for i, u := range actual {
		field := fmt.Sprintf("food_uses[%d]", i)
		used := u.foodID
		if u.substituteID.Valid {
			used = u.substituteID
		}
		f, ok := byID[used]
		if !ok {
			return nil, nil, connect.Errorf(connect.CodeInvalidArgument, "%s names a food that isn't one of your household's; find foods with ListFoods", field)
		}
		_, inRecipe := grams[u.foodID]
		if !inRecipe && !u.hasAmount {
			return nil, nil, connect.Errorf(connect.CodeInvalidArgument,
				"%s.food_id isn't one of the recipe's weighed foods: give its amount to take an extra food out, or check the food ID with GetRecipe", field)
		}
		replaced[u.foodID] = true
		if u.skipped {
			continue
		}
		g := grams[u.foodID]
		if u.hasAmount {
			if g, ok = f.Nutrition().Grams(u.amount, u.unit); !ok {
				return nil, nil, connect.Errorf(connect.CodeInvalidArgument,
					"%s: %s can't be weighed in that unit: give its amount in grams, or set its grams_per_piece or grams_per_milliliter", field, f.Name)
			}
		}
		names[used] = f.Name
		extra = append(extra, pantry.Use{FoodID: used, Grams: g})
	}
	for _, id := range order {
		if !replaced[id] {
			uses = append(uses, pantry.Use{FoodID: id, Grams: grams[id]})
		}
	}
	return append(uses, extra...), names, nil
}

// useForCook takes what a cook marked done uses out of the pantry (see
// cookUses), or with preview, only works it out from what's in stock, and
// returns it.
func (s *Service) useForCook(ctx context.Context, c *plans.Cook, uses []pantry.Use, names map[pgtype.UUID]string, preview bool) ([]*plansv1.PantryChange, error) {
	if len(uses) == 0 {
		return nil, nil
	}
	var deductions []pantry.Deduction
	var err error
	if preview {
		items, err := s.pantry.ListItems(ctx, c.HouseholdID)
		if err != nil {
			return nil, err
		}
		deductions = pantry.Deduct(items, uses)
	} else if deductions, err = s.pantry.UseForCook(ctx, c.HouseholdID, c.ID, uses); err != nil {
		return nil, err
	}
	return deductionChanges(deductions, names), nil
}

// deductionChanges converts what was taken out of lots, by food.
func deductionChanges(deductions []pantry.Deduction, names map[pgtype.UUID]string) []*plansv1.PantryChange {
	var used []pantry.Use
	for _, d := range deductions {
		if n := len(used); n > 0 && used[n-1].FoodID == d.Item.FoodID {
			used[n-1].Grams += d.Grams
			continue
		}
		used = append(used, pantry.Use{FoodID: d.Item.FoodID, Grams: d.Grams})
	}
	return pantryChanges(used, names)
}

// restoreForCook puts back what a cook took out of the pantry, and returns
// it.
func (s *Service) restoreForCook(ctx context.Context, c *plans.Cook) ([]*plansv1.PantryChange, error) {
	restored, err := s.pantry.RestoreForCook(ctx, c.HouseholdID, c.ID)
	if err != nil {
		return nil, err
	}
	return s.restoredChanges(ctx, c.HouseholdID, restored)
}

// restoredChanges converts what was put back into the pantry, with the
// foods' names.
func (s *Service) restoredChanges(ctx context.Context, householdID pgtype.UUID, restored []pantry.Use) ([]*plansv1.PantryChange, error) {
	if len(restored) == 0 {
		return nil, nil
	}
	names := map[pgtype.UUID]string{}
	var foodIDs []pgtype.UUID
	for _, u := range restored {
		foodIDs = append(foodIDs, u.FoodID)
	}
	list, err := s.foods.ListFoodsByID(ctx, householdID, foodIDs)
	if err != nil {
		return nil, err
	}
	for _, f := range list {
		names[f.ID] = f.Name
	}
	return pantryChanges(restored, names), nil
}

// pantryChanges converts what a cook took out of the pantry or put back,
// ordered by food name.
func pantryChanges(uses []pantry.Use, names map[pgtype.UUID]string) []*plansv1.PantryChange {
	changes := make([]*plansv1.PantryChange, 0, len(uses))
	for _, u := range uses {
		changes = append(changes, plansv1.PantryChange_builder{
			FoodId:   store.UUIDToString(u.FoodID),
			FoodName: names[u.FoodID],
			Grams:    round(u.Grams),
		}.Build())
	}
	slices.SortStableFunc(changes, func(a, b *plansv1.PantryChange) int {
		return strings.Compare(strings.ToLower(a.GetFoodName()), strings.ToLower(b.GetFoodName()))
	})
	return changes
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
	actual, err := foodUsesFromProto(req.GetFoodUses())
	if err != nil {
		return nil, err
	}
	marking := c.Done && !existing.Done
	if (len(actual) > 0 || req.GetPreview()) && !marking {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			"food_uses and preview are only for marking a cook done: set done to true, with the update_mask \"done\", on a cook not done yet")
	}
	// What marking it done takes out of the pantry is worked out first, so
	// a mistake in food_uses changes nothing. A preview stops there.
	var uses []pantry.Use
	var names map[pgtype.UUID]string
	if marking {
		c.Supplies, c.Supplying = existing.Supplies, existing.Supplying
		if uses, names, err = s.cookUses(ctx, c, actual); err != nil {
			return nil, s.toConnectError(ctx, err, "failed to work out the cook's pantry changes", cookNotFound)
		}
	}
	if req.GetPreview() {
		changes, err := s.useForCook(ctx, c, uses, names, true)
		if err != nil {
			return nil, s.toConnectError(ctx, err, "failed to work out the cook's pantry changes", cookNotFound)
		}
		return plansv1.UpdateCookResponse_builder{Cook: cookToProto(c), PantryChanges: changes, Previewed: true}.Build(), nil
	}
	updated, err := s.plans.UpdateCook(ctx, c)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to update cook", cookNotFound)
	}
	// Updating a cook doesn't change what supplies it, or what it supplies.
	updated.Supplies, updated.Supplying = existing.Supplies, existing.Supplying
	// Only a change of done changes the pantry, so cooks marked done before
	// the pantry existed don't take their ingredients out when edited. The
	// pantry's own records make taking out and putting back happen once
	// each, even if two phones mark the cook at the same time.
	var changes []*plansv1.PantryChange
	switch {
	case updated.Done && !existing.Done:
		changes, err = s.useForCook(ctx, updated, uses, names, false)
		if err != nil {
			return nil, s.toConnectError(ctx, err, "failed to take the cook's ingredients out of the pantry", cookNotFound)
		}
	case !updated.Done && existing.Done:
		changes, err = s.restoreForCook(ctx, updated)
		if err != nil {
			return nil, s.toConnectError(ctx, err, "failed to put the cook's ingredients back into the pantry", cookNotFound)
		}
	}
	return plansv1.UpdateCookResponse_builder{
		Cook:           cookToProto(updated),
		PantryChanges:  changes,
		PantryRestored: !updated.Done && existing.Done,
	}.Build(), nil
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
	if skipped {
		// A staple marked eaten took its food out of the pantry, which
		// skipping it would leave out.
		plan, err := s.plans.GetPlan(ctx, user.HouseholdID(), date, date)
		if err != nil {
			return nil, s.toConnectError(ctx, err, "failed to get plan", "")
		}
		for _, st := range plan.Staples {
			if st.ID == id && st.EatenOn(date) {
				return nil, connect.Errorf(connect.CodeFailedPrecondition,
					"%s was marked eaten on %s: un-mark it with SetStapleEaten first, which puts its food back in the pantry", st.Name, store.FormatDate(date))
			}
		}
	}
	st, err := s.plans.SetStapleSkipped(ctx, user.HouseholdID(), id, date, skipped)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to skip staple", stapleNotFound)
	}
	return stapleToProto(st), nil
}

func (s *Service) SetStapleEaten(ctx context.Context, req *plansv1.SetStapleEatenRequest) (*plansv1.SetStapleEatenResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	householdID := user.HouseholdID()
	id, err := parseID("id", req.GetId(), "find staples with ListStaples")
	if err != nil {
		return nil, err
	}
	date, err := parseDate("date", req.GetDate())
	if err != nil {
		return nil, err
	}
	st, err := s.plans.GetStaple(ctx, householdID, id)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to get staple", stapleNotFound)
	}
	resp := plansv1.SetStapleEatenResponse_builder{}
	if req.GetEaten() {
		if !st.IsOn(date) {
			return nil, connect.Errorf(connect.CodeInvalidArgument,
				"%s isn't eaten on %s: give one of its dates, by its weekdays, start_date and end_date, or change them with UpdateStaple", st.Name, store.FormatDate(date))
		}
		if st.SkippedOn(date) {
			if _, err := s.plans.SetStapleSkipped(ctx, householdID, id, date, false); err != nil {
				return nil, s.toConnectError(ctx, err, "failed to stop skipping staple", stapleNotFound)
			}
		}
		uses, names, err := s.stapleUses(ctx, st)
		if err != nil {
			return nil, s.toConnectError(ctx, err, "failed to read the staple's food", "")
		}
		deductions, err := s.pantry.UseForStaple(ctx, householdID, id, date, uses)
		if err != nil {
			return nil, s.toConnectError(ctx, err, "failed to take the staple's food out of the pantry", stapleNotFound)
		}
		resp.PantryChanges = deductionChanges(deductions, names)
	} else {
		restored, err := s.pantry.RestoreForStaple(ctx, householdID, id, date)
		if err != nil {
			return nil, s.toConnectError(ctx, err, "failed to put the staple's food back in the pantry", "")
		}
		if resp.PantryChanges, err = s.restoredChanges(ctx, householdID, restored); err != nil {
			return nil, s.toConnectError(ctx, err, "failed to read the staple's food", "")
		}
		resp.PantryRestored = len(restored) > 0
	}
	// The staple is read again, as marking it eaten may stop skipping it.
	if st, err = s.plans.GetStaple(ctx, householdID, id); err != nil {
		return nil, s.toConnectError(ctx, err, "failed to get staple", stapleNotFound)
	}
	resp.Staple = stapleToProto(st)
	return resp.Build(), nil
}

// stapleUses returns what a staple eaten once takes out of the pantry: its
// food's amount, or its recipe's ingredients scaled to its servings
// without the optional ones, with the foods' names. What can't be weighed
// is left in, as the pantry is in grams.
func (s *Service) stapleUses(ctx context.Context, st *plans.Staple) ([]pantry.Use, map[pgtype.UUID]string, error) {
	names := map[pgtype.UUID]string{}
	if !st.RecipeID.Valid {
		list, err := s.foods.ListFoodsByID(ctx, st.HouseholdID, []pgtype.UUID{st.FoodID})
		if err != nil || len(list) == 0 {
			return nil, nil, err
		}
		f := list[0]
		g, ok := f.Nutrition().Grams(st.Amount, st.Unit)
		if !ok {
			return nil, nil, nil
		}
		names[f.ID] = f.Name
		return []pantry.Use{{FoodID: f.ID, Grams: g}}, names, nil
	}
	list, err := s.recipes.ListRecipesByID(ctx, st.HouseholdID, []pgtype.UUID{st.RecipeID})
	if err != nil || len(list) == 0 {
		return nil, nil, err
	}
	var uses []pantry.Use
	index := map[pgtype.UUID]int{}
	for _, in := range list[0].IngredientsWithout(st.Servings, nil) {
		g, ok := in.Food.Grams(in.Amount, in.Unit)
		if !ok {
			continue
		}
		names[in.FoodID] = in.FoodName
		if i, seen := index[in.FoodID]; seen {
			uses[i].Grams += g
			continue
		}
		index[in.FoodID] = len(uses)
		uses = append(uses, pantry.Use{FoodID: in.FoodID, Grams: g})
	}
	return uses, names, nil
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
