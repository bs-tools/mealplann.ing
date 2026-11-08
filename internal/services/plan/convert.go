package plan

import (
	"math"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"connectrpc.com/connect/v2"
	foodsv1 "github.com/bs-tools/mealplann.ing/internal/gen/foods/v1"
	peoplev1 "github.com/bs-tools/mealplann.ing/internal/gen/people/v1"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	"github.com/bs-tools/mealplann.ing/internal/nutrition"
	"github.com/bs-tools/mealplann.ing/internal/people"
	"github.com/bs-tools/mealplann.ing/internal/plans"
	"github.com/bs-tools/mealplann.ing/internal/services/person"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Limits of the plan's fields.
const (
	minCookServings = 0.25
	maxCookServings = 100
	// maxShareServings is the most servings of a cook or recipe a person
	// eats at once.
	maxShareServings = 20
	// maxFoodAmount is the most of a food eaten at once, in its unit.
	maxFoodAmount       = 5000
	maxCookNotesLength  = 2000
	maxOtherNotesLength = 500
	// minYear and maxYear bound plan dates, to catch typos like "0226".
	minYear = 2000
	maxYear = 2100
)

var unitsToProto = map[nutrition.Unit]foodsv1.AmountUnit{
	nutrition.Grams:       foodsv1.AmountUnit_AMOUNT_UNIT_GRAMS,
	nutrition.Milliliters: foodsv1.AmountUnit_AMOUNT_UNIT_MILLILITERS,
	nutrition.Pieces:      foodsv1.AmountUnit_AMOUNT_UNIT_PIECES,
}

var unitsFromProto = map[foodsv1.AmountUnit]nutrition.Unit{
	foodsv1.AmountUnit_AMOUNT_UNIT_GRAMS:       nutrition.Grams,
	foodsv1.AmountUnit_AMOUNT_UNIT_MILLILITERS: nutrition.Milliliters,
	foodsv1.AmountUnit_AMOUNT_UNIT_PIECES:      nutrition.Pieces,
}

// parseDate parses a plan's date, naming field in errors.
func parseDate(field, s string) (time.Time, error) {
	date, err := store.ParseDate(strings.TrimSpace(s))
	if err != nil {
		return time.Time{}, connect.Errorf(connect.CodeInvalidArgument, `%s must be a date, "YYYY-MM-DD", e.g. "2026-11-02"`, field)
	}
	if date.Year() < minYear || date.Year() > maxYear {
		return time.Time{}, connect.Errorf(connect.CodeInvalidArgument, "%s must be from %d to %d", field, minYear, maxYear)
	}
	return date, nil
}

// parseID parses a reference, naming field in errors, with how to find a
// valid one.
func parseID(field, s, find string) (pgtype.UUID, error) {
	id, err := store.StringToUUID(strings.TrimSpace(s))
	if err != nil {
		return pgtype.UUID{}, connect.Errorf(connect.CodeInvalidArgument, "invalid %s: %v; %s", field, err, find).WithCause(err)
	}
	return id, nil
}

// optionalID returns the string of an ID, empty if it's invalid.
func optionalID(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	return store.UUIDToString(id)
}

func checkNotes(field, notes string, most int) (string, error) {
	notes = strings.TrimSpace(notes)
	if utf8.RuneCountInString(notes) > most {
		return "", connect.Errorf(connect.CodeInvalidArgument, "%s must be at most %d characters", field, most)
	}
	return notes, nil
}

// checkServings checks a share's servings.
func checkServings(field string, v float64) error {
	if math.IsNaN(v) || v <= 0 || v > maxShareServings {
		return connect.Errorf(connect.CodeInvalidArgument, "%s must be more than 0 and at most %d, e.g. 0.5 for half a serving", field, maxShareServings)
	}
	return nil
}

func cookToProto(c *plans.Cook) *plansv1.Cook {
	return plansv1.Cook_builder{
		Id:         store.UUIDToString(c.ID),
		RecipeId:   store.UUIDToString(c.RecipeID),
		RecipeName: c.RecipeName,
		Date:       store.FormatDate(c.Date),
		Servings:   c.Servings,
		Meal:       person.MealsToProto[c.Meal],
		Notes:      c.Notes,
		Done:       c.Done,
		CreateTime: timestamppb.New(c.CreateTime),
		UpdateTime: timestamppb.New(c.UpdateTime),
	}.Build()
}

// cookFromProto converts and validates a cook. Its ID, household and
// times are left for the caller to set.
func cookFromProto(p *plansv1.Cook) (*plans.Cook, error) {
	recipeID, err := parseID("recipe_id", p.GetRecipeId(), "find recipes with ListRecipes")
	if err != nil {
		return nil, err
	}
	date, err := parseDate("date", p.GetDate())
	if err != nil {
		return nil, err
	}
	if s := p.GetServings(); math.IsNaN(s) || s < minCookServings || s > maxCookServings {
		return nil, connect.Errorf(connect.CodeInvalidArgument, "servings must be from %v to %d", minCookServings, maxCookServings)
	}
	meal, ok := person.MealsFromProto[p.GetMeal()]
	if !ok {
		return nil, connect.NewError(connect.CodeInvalidArgument, "meal is invalid")
	}
	notes, err := checkNotes("notes", p.GetNotes(), maxCookNotesLength)
	if err != nil {
		return nil, err
	}
	return &plans.Cook{
		RecipeID: recipeID,
		Date:     date,
		Servings: p.GetServings(),
		Meal:     meal,
		Notes:    notes,
		Done:     p.GetDone(),
	}, nil
}

func foodAmountToProto(foodID pgtype.UUID, amount float64, unit nutrition.Unit) *plansv1.FoodAmount {
	return plansv1.FoodAmount_builder{
		FoodId: store.UUIDToString(foodID),
		Amount: amount,
		Unit:   unitsToProto[unit],
	}.Build()
}

// foodAmountFromProto converts and validates an amount of a food.
func foodAmountFromProto(p *plansv1.FoodAmount) (pgtype.UUID, float64, nutrition.Unit, error) {
	foodID, err := parseID("food.food_id", p.GetFoodId(), "find foods with ListFoods")
	if err != nil {
		return pgtype.UUID{}, 0, 0, err
	}
	if a := p.GetAmount(); math.IsNaN(a) || a <= 0 || a > maxFoodAmount {
		return pgtype.UUID{}, 0, 0, connect.Errorf(connect.CodeInvalidArgument, "food.amount must be more than 0 and at most %d", maxFoodAmount)
	}
	unit, ok := unitsFromProto[p.GetUnit()]
	if !ok {
		return pgtype.UUID{}, 0, 0, connect.NewError(connect.CodeInvalidArgument,
			"food.unit is required: AMOUNT_UNIT_GRAMS, AMOUNT_UNIT_MILLILITERS or AMOUNT_UNIT_PIECES")
	}
	return foodID, p.GetAmount(), unit, nil
}

func portionToProto(p *plans.Portion) *plansv1.Portion {
	b := plansv1.Portion_builder{
		Id:           store.UUIDToString(p.ID),
		PersonId:     store.UUIDToString(p.PersonID),
		EatingTimeId: store.UUIDToString(p.EatingTimeID),
		Date:         store.FormatDate(p.Date),
		Notes:        p.Notes,
		Name:         p.Name,
		CreateTime:   timestamppb.New(p.CreateTime),
		UpdateTime:   timestamppb.New(p.UpdateTime),
	}
	if p.CookID.Valid {
		b.Cook = plansv1.CookShare_builder{CookId: store.UUIDToString(p.CookID), Servings: p.Servings}.Build()
	} else {
		b.Food = foodAmountToProto(p.FoodID, p.Amount, p.Unit)
	}
	return b.Build()
}

// portionFromProto converts and validates a portion. Its ID, household and
// times are left for the caller to set.
func portionFromProto(p *plansv1.Portion) (*plans.Portion, error) {
	personID, err := parseID("person_id", p.GetPersonId(), "find people with ListPeople")
	if err != nil {
		return nil, err
	}
	eatingTimeID, err := parseID("eating_time_id", p.GetEatingTimeId(), "find the person's eating times with GetPerson")
	if err != nil {
		return nil, err
	}
	date, err := parseDate("date", p.GetDate())
	if err != nil {
		return nil, err
	}
	notes, err := checkNotes("notes", p.GetNotes(), maxOtherNotesLength)
	if err != nil {
		return nil, err
	}
	portion := &plans.Portion{PersonID: personID, EatingTimeID: eatingTimeID, Date: date, Notes: notes}
	switch {
	case p.HasCook():
		portion.CookID, err = parseID("cook.cook_id", p.GetCook().GetCookId(), "find cooks with GetWeek, or create one with CreateCook")
		if err != nil {
			return nil, err
		}
		if err := checkServings("cook.servings", p.GetCook().GetServings()); err != nil {
			return nil, err
		}
		portion.Servings = p.GetCook().GetServings()
	case p.HasFood():
		portion.FoodID, portion.Amount, portion.Unit, err = foodAmountFromProto(p.GetFood())
		if err != nil {
			return nil, err
		}
	default:
		return nil, connect.NewError(connect.CodeInvalidArgument,
			"set what's eaten: cook, a share of a cook in servings, or food, an amount of a food")
	}
	return portion, nil
}

// weekdaysToProto converts weekdays to the API, whose values are ISO
// weekdays, from 1 for Monday.
func weekdaysToProto(weekdays []time.Weekday) []plansv1.Weekday {
	list := make([]plansv1.Weekday, 0, len(weekdays))
	for _, d := range weekdays {
		list = append(list, plansv1.Weekday((d+6)%7+1))
	}
	return list
}

func stapleToProto(s *plans.Staple) *plansv1.Staple {
	skipped := make([]string, 0, len(s.SkippedDates))
	for _, d := range s.SkippedDates {
		skipped = append(skipped, store.FormatDate(d))
	}
	b := plansv1.Staple_builder{
		Id:           store.UUIDToString(s.ID),
		PersonId:     store.UUIDToString(s.PersonID),
		EatingTimeId: store.UUIDToString(s.EatingTimeID),
		Weekdays:     weekdaysToProto(s.Weekdays),
		StartDate:    store.FormatDate(s.StartDate),
		EndDate:      store.FormatDate(s.EndDate),
		Notes:        s.Notes,
		Name:         s.Name,
		SkippedDates: skipped,
		CreateTime:   timestamppb.New(s.CreateTime),
		UpdateTime:   timestamppb.New(s.UpdateTime),
	}
	if s.RecipeID.Valid {
		b.Recipe = plansv1.RecipeServings_builder{RecipeId: store.UUIDToString(s.RecipeID), Servings: s.Servings}.Build()
	} else {
		b.Food = foodAmountToProto(s.FoodID, s.Amount, s.Unit)
	}
	return b.Build()
}

// stapleFromProto converts and validates a staple. Its ID, household,
// skipped dates and times are left for the caller to set.
func stapleFromProto(p *plansv1.Staple) (*plans.Staple, error) {
	personID, err := parseID("person_id", p.GetPersonId(), "find people with ListPeople")
	if err != nil {
		return nil, err
	}
	eatingTimeID, err := parseID("eating_time_id", p.GetEatingTimeId(), "find the person's eating times with GetPerson")
	if err != nil {
		return nil, err
	}
	notes, err := checkNotes("notes", p.GetNotes(), maxOtherNotesLength)
	if err != nil {
		return nil, err
	}
	s := &plans.Staple{PersonID: personID, EatingTimeID: eatingTimeID, Notes: notes}
	switch {
	case p.HasRecipe():
		s.RecipeID, err = parseID("recipe.recipe_id", p.GetRecipe().GetRecipeId(), "find recipes with ListRecipes")
		if err != nil {
			return nil, err
		}
		if err := checkServings("recipe.servings", p.GetRecipe().GetServings()); err != nil {
			return nil, err
		}
		s.Servings = p.GetRecipe().GetServings()
	case p.HasFood():
		s.FoodID, s.Amount, s.Unit, err = foodAmountFromProto(p.GetFood())
		if err != nil {
			return nil, err
		}
	default:
		return nil, connect.NewError(connect.CodeInvalidArgument,
			"set what's eaten: recipe, servings of a recipe, or food, an amount of a food")
	}
	for _, d := range p.GetWeekdays() {
		if d < plansv1.Weekday_WEEKDAY_MONDAY || d > plansv1.Weekday_WEEKDAY_SUNDAY {
			return nil, connect.NewError(connect.CodeInvalidArgument, "weekdays must be from WEEKDAY_MONDAY to WEEKDAY_SUNDAY; leave it empty for every day")
		}
		weekday := time.Weekday(int(d) % 7)
		if slices.Contains(s.Weekdays, weekday) {
			return nil, connect.Errorf(connect.CodeInvalidArgument, "weekdays repeats %s; list each day once", d)
		}
		s.Weekdays = append(s.Weekdays, weekday)
	}
	if p.GetStartDate() != "" {
		if s.StartDate, err = parseDate("start_date", p.GetStartDate()); err != nil {
			return nil, err
		}
	}
	if p.GetEndDate() != "" {
		if s.EndDate, err = parseDate("end_date", p.GetEndDate()); err != nil {
			return nil, err
		}
	}
	if !s.StartDate.IsZero() && !s.EndDate.IsZero() && s.EndDate.Before(s.StartDate) {
		return nil, connect.NewError(connect.CodeInvalidArgument, "end_date must be on or after start_date")
	}
	return s, nil
}

// mealFromProto converts a required shared meal.
func mealFromProto(m peoplev1.HouseholdMeal) (people.Meal, error) {
	meal, ok := person.MealsFromProto[m]
	if !ok || meal == people.MealNone {
		return people.MealNone, connect.NewError(connect.CodeInvalidArgument,
			"meal is required: HOUSEHOLD_MEAL_BREAKFAST, HOUSEHOLD_MEAL_LUNCH or HOUSEHOLD_MEAL_DINNER")
	}
	return meal, nil
}
