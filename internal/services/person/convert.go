package person

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"connectrpc.com/connect/v2"
	peoplev1 "github.com/bs-tools/mealplann.ing/internal/gen/people/v1"
	"github.com/bs-tools/mealplann.ing/internal/nutrition"
	"github.com/bs-tools/mealplann.ing/internal/people"
	"github.com/bs-tools/mealplann.ing/internal/rules"
	"github.com/bs-tools/mealplann.ing/internal/services/food"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Limits of a person's fields. The rules' limits only keep out values that
// can't be meant, like a portion of 50 kg: the values themselves are the
// care team's.
const (
	maxNameLength           = 100
	maxNotesLength          = 2000
	maxEatingTimes          = 12
	maxEatingTimeNameLength = 50
	maxPortionGrams         = 5000
	maxNutrientGrams        = 1000
	maxPairingRatio         = 20
)

// timeOfDay matches "HH:MM" in 24 hours.
var timeOfDay = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

// KindsToProto, KindsFromProto, MealsToProto and MealsFromProto convert
// eating times' kinds and meals to and from the API.
var (
	KindsToProto = map[people.Kind]peoplev1.EatingTimeKind{
		people.KindMain:  peoplev1.EatingTimeKind_EATING_TIME_KIND_MAIN,
		people.KindSmall: peoplev1.EatingTimeKind_EATING_TIME_KIND_SMALL,
	}
	KindsFromProto = map[peoplev1.EatingTimeKind]people.Kind{
		peoplev1.EatingTimeKind_EATING_TIME_KIND_MAIN:  people.KindMain,
		peoplev1.EatingTimeKind_EATING_TIME_KIND_SMALL: people.KindSmall,
	}
	MealsToProto = map[people.Meal]peoplev1.HouseholdMeal{
		people.MealNone:      peoplev1.HouseholdMeal_HOUSEHOLD_MEAL_UNSPECIFIED,
		people.MealBreakfast: peoplev1.HouseholdMeal_HOUSEHOLD_MEAL_BREAKFAST,
		people.MealLunch:     peoplev1.HouseholdMeal_HOUSEHOLD_MEAL_LUNCH,
		people.MealDinner:    peoplev1.HouseholdMeal_HOUSEHOLD_MEAL_DINNER,
	}
	MealsFromProto = map[peoplev1.HouseholdMeal]people.Meal{
		peoplev1.HouseholdMeal_HOUSEHOLD_MEAL_UNSPECIFIED: people.MealNone,
		peoplev1.HouseholdMeal_HOUSEHOLD_MEAL_BREAKFAST:   people.MealBreakfast,
		peoplev1.HouseholdMeal_HOUSEHOLD_MEAL_LUNCH:       people.MealLunch,
		peoplev1.HouseholdMeal_HOUSEHOLD_MEAL_DINNER:      people.MealDinner,
	}
)

// PersonToProto converts a person to the API.
func PersonToProto(p *people.Person) *peoplev1.Person {
	eatingTimes := make([]*peoplev1.EatingTime, 0, len(p.EatingTimes))
	for _, et := range p.EatingTimes {
		eatingTimes = append(eatingTimes, EatingTimeToProto(et))
	}
	targets := make([]*peoplev1.NutrientTarget, 0, len(p.Targets))
	for _, t := range p.Targets {
		targets = append(targets, peoplev1.NutrientTarget_builder{
			Nutrient:  food.NutrientToProto(t.Nutrient),
			MinPerDay: t.Min,
			MaxPerDay: t.Max,
		}.Build())
	}
	var userID string
	if p.UserID.Valid {
		userID = store.UUIDToString(p.UserID)
	}
	var rulesProto *peoplev1.EatingRules
	if r := p.Rules; r != (rules.Rules{}) {
		rulesProto = peoplev1.EatingRules_builder{
			MaxPortionGrams:      r.MaxPortionGrams,
			MinProteinGrams:      r.MinProteinGrams,
			MaxCarbohydrateGrams: r.MaxCarbohydrateGrams,
			MinPairingRatio:      r.MinPairingRatio,
			MaxSugarsGrams:       r.MaxSugarsGrams,
			WarnFastCarbs:        r.WarnFastCarbs,
		}.Build()
	}
	return peoplev1.Person_builder{
		Id:           store.UUIDToString(p.ID),
		Name:         p.Name,
		UserId:       userID,
		EatingTimes:  eatingTimes,
		Rules:        rulesProto,
		DailyTargets: targets,
		Notes:        p.Notes,
		CreateTime:   timestamppb.New(p.CreateTime),
		UpdateTime:   timestamppb.New(p.UpdateTime),
		Profile:      profileToProto(&p.Profile),
		Tastes:       tastesToProto(&p.Profile),
		Recommended:  recommendationToProto(&p.Profile),
	}.Build()
}

// The profile's enums to and from the API.
var (
	sexes = map[people.Sex]peoplev1.Sex{
		people.SexUnknown: peoplev1.Sex_SEX_UNSPECIFIED,
		people.SexFemale:  peoplev1.Sex_SEX_FEMALE,
		people.SexMale:    peoplev1.Sex_SEX_MALE,
	}
	activities = map[people.Activity]peoplev1.Activity{
		people.ActivityUnknown: peoplev1.Activity_ACTIVITY_UNSPECIFIED,
		people.ActivityLow:     peoplev1.Activity_ACTIVITY_LOW,
		people.ActivityLight:   peoplev1.Activity_ACTIVITY_LIGHT,
		people.ActivityActive:  peoplev1.Activity_ACTIVITY_ACTIVE,
		people.ActivityVery:    peoplev1.Activity_ACTIVITY_VERY,
	}
	goals = map[people.Goal]peoplev1.Goal{
		people.GoalUnknown:       peoplev1.Goal_GOAL_UNSPECIFIED,
		people.GoalMaintain:      peoplev1.Goal_GOAL_MAINTAIN,
		people.GoalBuildStrength: peoplev1.Goal_GOAL_BUILD_STRENGTH,
		people.GoalLoseBodyFat:   peoplev1.Goal_GOAL_LOSE_BODY_FAT,
	}
	smallMealTastes = map[people.SmallMealTaste]peoplev1.SmallMealTaste{
		people.TasteEither: peoplev1.SmallMealTaste_SMALL_MEAL_TASTE_UNSPECIFIED,
		people.TasteSavory: peoplev1.SmallMealTaste_SMALL_MEAL_TASTE_SAVORY,
		people.TasteSweet:  peoplev1.SmallMealTaste_SMALL_MEAL_TASTE_SWEET,
	}
	spices = map[people.Spice]peoplev1.SpiceLevel{
		people.SpiceAny:    peoplev1.SpiceLevel_SPICE_LEVEL_UNSPECIFIED,
		people.SpiceMild:   peoplev1.SpiceLevel_SPICE_LEVEL_MILD,
		people.SpiceMedium: peoplev1.SpiceLevel_SPICE_LEVEL_MEDIUM,
		people.SpiceHot:    peoplev1.SpiceLevel_SPICE_LEVEL_HOT,
	}
)

// reverse returns a map's keys by value.
func reverse[K, V comparable](m map[K]V) map[V]K {
	r := make(map[V]K, len(m))
	for k, v := range m {
		r[v] = k
	}
	return r
}

var (
	sexesFromProto           = reverse(sexes)
	activitiesFromProto      = reverse(activities)
	goalsFromProto           = reverse(goals)
	smallMealTastesFromProto = reverse(smallMealTastes)
	spicesFromProto          = reverse(spices)
)

// Limits of a profile, which only keep out values that can't be meant.
const (
	minHeightCm    = 50
	maxHeightCm    = 250
	minWeightKg    = 20
	maxWeightKg    = 300
	minBirthYear   = 1900
	maxTastes      = 20
	maxTasteLength = 50
)

// profileToProto converts a person's body, activity and goal, nil if none
// is said.
func profileToProto(p *people.Profile) *peoplev1.Profile {
	if p.HeightCm == 0 && p.WeightKg == 0 && p.BirthYear == 0 && p.Sex == people.SexUnknown &&
		p.Activity == people.ActivityUnknown && p.Goal == people.GoalUnknown {
		return nil
	}
	b := peoplev1.Profile_builder{
		BirthYear: p.BirthYear,
		Sex:       sexes[p.Sex],
		Activity:  activities[p.Activity],
		Goal:      goals[p.Goal],
	}
	if p.HeightCm > 0 {
		b.HeightCm = &p.HeightCm
	}
	if p.WeightKg > 0 {
		b.WeightKg = &p.WeightKg
	}
	return b.Build()
}

// tastesToProto converts a person's tastes, nil if none is said.
func tastesToProto(p *people.Profile) *peoplev1.Tastes {
	if p.SmallMeals == people.TasteEither && p.Spice == people.SpiceAny && len(p.Likes) == 0 && len(p.Dislikes) == 0 {
		return nil
	}
	return peoplev1.Tastes_builder{
		SmallMeals: smallMealTastes[p.SmallMeals],
		Spice:      spices[p.Spice],
		Likes:      p.Likes,
		Dislikes:   p.Dislikes,
	}.Build()
}

// recommendationToProto converts what a profile recommends, nil if it
// doesn't say enough.
func recommendationToProto(p *people.Profile) *peoplev1.Recommendation {
	r, ok := p.Recommend(time.Now().Year())
	if !ok {
		return nil
	}
	return peoplev1.Recommendation_builder{
		EnergyKcal:        r.EnergyKcal,
		ProteinGrams:      r.ProteinGrams,
		CarbohydrateGrams: r.CarbohydrateGrams,
		FatGrams:          r.FatGrams,
	}.Build()
}

// profileFromProto converts and validates a person's profile and tastes.
func profileFromProto(body *peoplev1.Profile, tastes *peoplev1.Tastes) (people.Profile, error) {
	var p people.Profile
	if h := body.GetHeightCm(); body.HasHeightCm() {
		if h < minHeightCm || h > maxHeightCm {
			return p, connect.Errorf(connect.CodeInvalidArgument, "profile.height_cm must be from %d to %d centimeters", minHeightCm, maxHeightCm)
		}
		p.HeightCm = h
	}
	if w := body.GetWeightKg(); body.HasWeightKg() {
		if w < minWeightKg || w > maxWeightKg {
			return p, connect.Errorf(connect.CodeInvalidArgument, "profile.weight_kg must be from %d to %d kilograms", minWeightKg, maxWeightKg)
		}
		p.WeightKg = w
	}
	if y := body.GetBirthYear(); y != 0 {
		if y < minBirthYear || int(y) > time.Now().Year() {
			return p, connect.Errorf(connect.CodeInvalidArgument, "profile.birth_year must be from %d to this year, or 0 if unknown", minBirthYear)
		}
		p.BirthYear = y
	}
	var ok bool
	if p.Sex, ok = sexesFromProto[body.GetSex()]; !ok {
		return p, connect.NewError(connect.CodeInvalidArgument, "profile.sex is unknown: SEX_FEMALE, SEX_MALE or unspecified")
	}
	if p.Activity, ok = activitiesFromProto[body.GetActivity()]; !ok {
		return p, connect.NewError(connect.CodeInvalidArgument, "profile.activity is unknown: use one of the Activity values")
	}
	if p.Goal, ok = goalsFromProto[body.GetGoal()]; !ok {
		return p, connect.NewError(connect.CodeInvalidArgument, "profile.goal is unknown: use one of the Goal values")
	}
	if p.SmallMeals, ok = smallMealTastesFromProto[tastes.GetSmallMeals()]; !ok {
		return p, connect.NewError(connect.CodeInvalidArgument, "tastes.small_meals is unknown: use one of the SmallMealTaste values")
	}
	if p.Spice, ok = spicesFromProto[tastes.GetSpice()]; !ok {
		return p, connect.NewError(connect.CodeInvalidArgument, "tastes.spice is unknown: use one of the SpiceLevel values")
	}
	var err error
	if p.Likes, err = tasteList("tastes.likes", tastes.GetLikes()); err != nil {
		return p, err
	}
	if p.Dislikes, err = tasteList("tastes.dislikes", tastes.GetDislikes()); err != nil {
		return p, err
	}
	return p, nil
}

// tasteList validates likes or dislikes, trimmed, without blanks or
// repeats.
func tasteList(field string, list []string) ([]string, error) {
	var out []string
	for _, item := range list {
		item = strings.TrimSpace(item)
		if item == "" || slices.ContainsFunc(out, func(other string) bool { return strings.EqualFold(other, item) }) {
			continue
		}
		if utf8.RuneCountInString(item) > maxTasteLength {
			return nil, connect.Errorf(connect.CodeInvalidArgument, "each of %s must be at most %d characters", field, maxTasteLength)
		}
		out = append(out, item)
	}
	if len(out) > maxTastes {
		return nil, connect.Errorf(connect.CodeInvalidArgument, "%s can have at most %d", field, maxTastes)
	}
	return out, nil
}

// EatingTimeToProto converts an eating time to the API.
func EatingTimeToProto(et *people.EatingTime) *peoplev1.EatingTime {
	return peoplev1.EatingTime_builder{
		Id:   store.UUIDToString(et.ID),
		Name: et.Name,
		Kind: KindsToProto[et.Kind],
		Time: et.TimeOfDay,
		Meal: MealsToProto[et.Meal],
	}.Build()
}

// PersonFromProto converts and validates a person. existing is the person
// being updated, whose eating times' IDs can be kept, or nil for a new
// person. The ID, household and times are left for the caller to set.
func PersonFromProto(p *peoplev1.Person, existing *people.Person) (*people.Person, error) {
	name := strings.TrimSpace(p.GetName())
	notes := strings.TrimSpace(p.GetNotes())
	switch {
	case name == "":
		return nil, connect.NewError(connect.CodeInvalidArgument, `name is required: give the person a name, e.g. "Anna"`)
	case utf8.RuneCountInString(name) > maxNameLength:
		return nil, connect.Errorf(connect.CodeInvalidArgument, "name must be at most %d characters", maxNameLength)
	case utf8.RuneCountInString(notes) > maxNotesLength:
		return nil, connect.Errorf(connect.CodeInvalidArgument, "notes must be at most %d characters", maxNotesLength)
	}
	var userID pgtype.UUID
	if s := strings.TrimSpace(p.GetUserId()); s != "" {
		var err error
		userID, err = store.StringToUUID(s)
		if err != nil {
			return nil, connect.Errorf(connect.CodeInvalidArgument,
				"invalid user_id: %v; find the household's users with GetHousehold, or leave it empty", err).WithCause(err)
		}
	}
	eatingTimes, err := eatingTimesFromProto(p.GetEatingTimes(), existing)
	if err != nil {
		return nil, err
	}
	r, err := rulesFromProto(p.GetRules())
	if err != nil {
		return nil, err
	}
	targets, err := targetsFromProto(p.GetDailyTargets())
	if err != nil {
		return nil, err
	}
	profile, err := profileFromProto(p.GetProfile(), p.GetTastes())
	if err != nil {
		return nil, err
	}
	return &people.Person{
		Profile:     profile,
		Name:        name,
		UserID:      userID,
		EatingTimes: eatingTimes,
		Rules:       r,
		Targets:     targets,
		Notes:       notes,
	}, nil
}

func eatingTimesFromProto(list []*peoplev1.EatingTime, existing *people.Person) ([]*people.EatingTime, error) {
	if len(list) > maxEatingTimes {
		return nil, connect.Errorf(connect.CodeInvalidArgument, "a person can have at most %d eating times", maxEatingTimes)
	}
	eatingTimes := make([]*people.EatingTime, 0, len(list))
	meals := map[people.Meal]string{}
	for i, p := range list {
		field := fmt.Sprintf("eating_times[%d]", i)
		name := strings.TrimSpace(p.GetName())
		switch {
		case name == "":
			return nil, connect.Errorf(connect.CodeInvalidArgument, `%s.name is required, e.g. "Breakfast" or "Mid-morning"`, field)
		case utf8.RuneCountInString(name) > maxEatingTimeNameLength:
			return nil, connect.Errorf(connect.CodeInvalidArgument, "%s.name must be at most %d characters", field, maxEatingTimeNameLength)
		}
		kind, ok := KindsFromProto[p.GetKind()]
		if !ok {
			return nil, connect.Errorf(connect.CodeInvalidArgument, "%s.kind is required: EATING_TIME_KIND_MAIN or EATING_TIME_KIND_SMALL", field)
		}
		time := strings.TrimSpace(p.GetTime())
		if time != "" && !timeOfDay.MatchString(time) {
			return nil, connect.Errorf(connect.CodeInvalidArgument, `%s.time must be "HH:MM" in 24 hours, e.g. "10:30", or empty`, field)
		}
		meal, ok := MealsFromProto[p.GetMeal()]
		if !ok {
			return nil, connect.Errorf(connect.CodeInvalidArgument, "%s.meal is invalid", field)
		}
		if other, ok := meals[meal]; ok && meal != people.MealNone {
			return nil, connect.Errorf(connect.CodeInvalidArgument,
				"%s and %s both eat from the shared %s: a person eats from each shared meal at most once, so set one's meal to HOUSEHOLD_MEAL_UNSPECIFIED",
				other, name, meal)
		}
		meals[meal] = name
		id := store.NewUUID()
		if s := p.GetId(); s != "" && existing != nil {
			parsed, err := store.StringToUUID(s)
			if err != nil || existing.EatingTime(parsed) == nil {
				return nil, connect.Errorf(connect.CodeInvalidArgument,
					"%s.id isn't one of the person's eating times: keep the IDs GetPerson returns, or leave it empty for a new eating time", field)
			}
			id = parsed
		}
		if slices.ContainsFunc(eatingTimes, func(et *people.EatingTime) bool { return et.ID == id }) {
			return nil, connect.Errorf(connect.CodeInvalidArgument, "%s.id is repeated: each eating time must have its own ID", field)
		}
		eatingTimes = append(eatingTimes, &people.EatingTime{
			ID:        id,
			Name:      name,
			Kind:      kind,
			TimeOfDay: time,
			Meal:      meal,
		})
	}
	return eatingTimes, nil
}

func rulesFromProto(p *peoplev1.EatingRules) (rules.Rules, error) {
	optional := func(has bool, v float64, field string, most float64, positive bool) (*float64, error) {
		if !has {
			return nil, nil
		}
		if math.IsNaN(v) || v < 0 || v > most || (positive && v == 0) {
			low := "0"
			if positive {
				low = "more than 0"
			}
			return nil, connect.Errorf(connect.CodeInvalidArgument,
				"rules.%s must be from %s to %v, as the person's care team gave it; leave it unset for no rule", field, low, most)
		}
		return new(v), nil
	}
	var r rules.Rules
	var err error
	if r.MaxPortionGrams, err = optional(p.HasMaxPortionGrams(), p.GetMaxPortionGrams(), "max_portion_grams", maxPortionGrams, true); err != nil {
		return r, err
	}
	if r.MinProteinGrams, err = optional(p.HasMinProteinGrams(), p.GetMinProteinGrams(), "min_protein_grams", maxNutrientGrams, false); err != nil {
		return r, err
	}
	if r.MaxCarbohydrateGrams, err = optional(p.HasMaxCarbohydrateGrams(), p.GetMaxCarbohydrateGrams(), "max_carbohydrate_grams", maxNutrientGrams, false); err != nil {
		return r, err
	}
	if r.MinPairingRatio, err = optional(p.HasMinPairingRatio(), p.GetMinPairingRatio(), "min_pairing_ratio", maxPairingRatio, false); err != nil {
		return r, err
	}
	if r.MaxSugarsGrams, err = optional(p.HasMaxSugarsGrams(), p.GetMaxSugarsGrams(), "max_sugars_grams", maxNutrientGrams, false); err != nil {
		return r, err
	}
	r.WarnFastCarbs = p.GetWarnFastCarbs()
	return r, nil
}

// maxPerDay is the most of a nutrient a daily target can be: that of 10 kg
// of a food.
func maxPerDay(n nutrition.Nutrient) float64 {
	switch n.Unit() {
	case "kcal":
		return 20_000
	case "mg":
		return 1_000_000
	case "µg":
		return 1_000_000_000
	default:
		return 10_000
	}
}

func targetsFromProto(list []*peoplev1.NutrientTarget) ([]rules.Target, error) {
	targets := make([]rules.Target, 0, len(list))
	for i, p := range list {
		field := fmt.Sprintf("daily_targets[%d]", i)
		n, ok := food.NutrientFromProto(p.GetNutrient())
		if !ok {
			return nil, connect.Errorf(connect.CodeInvalidArgument, "%s.nutrient is required, e.g. NUTRIENT_PROTEIN_GRAMS", field)
		}
		if slices.ContainsFunc(targets, func(t rules.Target) bool { return t.Nutrient == n }) {
			return nil, connect.Errorf(connect.CodeInvalidArgument, "%s repeats the nutrient %s: give each nutrient one target, with a min and a max", field, n)
		}
		t := rules.Target{Nutrient: n}
		most := maxPerDay(n)
		for _, bound := range []struct {
			name  string
			has   bool
			value float64
			dst   **float64
		}{
			{"min_per_day", p.HasMinPerDay(), p.GetMinPerDay(), &t.Min},
			{"max_per_day", p.HasMaxPerDay(), p.GetMaxPerDay(), &t.Max},
		} {
			if !bound.has {
				continue
			}
			if math.IsNaN(bound.value) || bound.value < 0 || bound.value > most {
				return nil, connect.Errorf(connect.CodeInvalidArgument, "%s.%s must be from 0 to %v %s", field, bound.name, most, n.Unit())
			}
			*bound.dst = new(bound.value)
		}
		switch {
		case t.Min == nil && t.Max == nil:
			return nil, connect.Errorf(connect.CodeInvalidArgument, "%s needs min_per_day, max_per_day or both; remove it if there's no target", field)
		case t.Min != nil && t.Max != nil && *t.Min > *t.Max:
			return nil, connect.Errorf(connect.CodeInvalidArgument, "%s.min_per_day must be at most max_per_day", field)
		}
		targets = append(targets, t)
	}
	// Targets are stored, and so returned, in the nutrients' order.
	slices.SortFunc(targets, func(a, b rules.Target) int { return int(a.Nutrient) - int(b.Nutrient) })
	return targets, nil
}
