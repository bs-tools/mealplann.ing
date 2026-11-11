package people

import (
	"encoding/json"
	"fmt"
	"math"
)

// Sex is a person's sex, for the estimate of the energy they need.
type Sex string

// The sexes, and unknown.
const (
	SexUnknown Sex = ""
	SexFemale  Sex = "female"
	SexMale    Sex = "male"
)

// Activity is how active a person is through a week.
type Activity string

// The levels of activity.
const (
	ActivityUnknown Activity = ""
	ActivityLow     Activity = "low"
	ActivityLight   Activity = "light"
	ActivityActive  Activity = "active"
	ActivityVery    Activity = "very"
)

// Goal is what a person wants their eating to do for them.
type Goal string

// The goals.
const (
	GoalUnknown       Goal = ""
	GoalMaintain      Goal = "maintain"
	GoalBuildStrength Goal = "build_strength"
	GoalLoseBodyFat   Goal = "lose_body_fat"
)

// SmallMealTaste is whether a person likes their small meals sweet or
// savory.
type SmallMealTaste string

// The tastes of small meals.
const (
	TasteEither SmallMealTaste = ""
	TasteSavory SmallMealTaste = "savory"
	TasteSweet  SmallMealTaste = "sweet"
)

// Spice is how spicy a person likes their food.
type Spice string

// The levels of spice.
const (
	SpiceAny    Spice = ""
	SpiceMild   Spice = "mild"
	SpiceMedium Spice = "medium"
	SpiceHot    Spice = "hot"
)

// Profile is what a person tells about their body, activity and goal, to
// recommend how much they eat, and their tastes, which suggestions favor.
type Profile struct {
	// HeightCm and WeightKg are 0 if unknown.
	HeightCm float64 `json:"height_cm,omitempty"`
	WeightKg float64 `json:"weight_kg,omitempty"`
	// BirthYear is 0 if unknown.
	BirthYear int32    `json:"birth_year,omitempty"`
	Sex       Sex      `json:"sex,omitempty"`
	Activity  Activity `json:"activity,omitempty"`
	Goal      Goal     `json:"goal,omitempty"`

	SmallMeals SmallMealTaste `json:"small_meals,omitempty"`
	Spice      Spice          `json:"spice,omitempty"`
	// Likes and Dislikes are foods or dishes, like "noodles" or
	// "mushrooms", matched against recipes' names, tags and ingredients.
	Likes    []string `json:"likes,omitempty"`
	Dislikes []string `json:"dislikes,omitempty"`
}

// profileFromDB reads a profile stored as JSON.
func profileFromDB(raw []byte) Profile {
	var p Profile
	if len(raw) > 0 {
		// A profile that doesn't parse, which writing never stores, is
		// left empty rather than failing the person.
		_ = json.Unmarshal(raw, &p)
	}
	return p
}

// profileToDB stores a profile as JSON.
func profileToDB(p Profile) []byte {
	raw, err := json.Marshal(p)
	if err != nil {
		panic(fmt.Sprintf("profile doesn't marshal: %v", err))
	}
	return raw
}

// Recommendation is how much a person eats a day for their goal: energy
// and the macronutrients, in grams.
type Recommendation struct {
	EnergyKcal        float64
	ProteinGrams      float64
	CarbohydrateGrams float64
	FatGrams          float64
}

// activityFactors scale the energy a body burns at rest to the day's, by
// activity, as commonly used with the Mifflin-St Jeor equation.
var activityFactors = map[Activity]float64{
	ActivityLow:    1.2,
	ActivityLight:  1.375,
	ActivityActive: 1.55,
	ActivityVery:   1.725,
}

// goalEnergy scales the energy of the day by goal: a deficit of a fifth to
// lose body fat, a small surplus to build strength.
var goalEnergy = map[Goal]float64{
	GoalMaintain:      1,
	GoalBuildStrength: 1.1,
	GoalLoseBodyFat:   0.8,
}

// goalProtein is the protein of the day, in grams per kilogram of body
// weight, by goal: more to build strength, and most while losing body fat,
// to lose fat rather than muscle.
var goalProtein = map[Goal]float64{
	GoalMaintain:      1.0,
	GoalBuildStrength: 1.6,
	GoalLoseBodyFat:   1.8,
}

// goalFat is the share of the day's energy from fat, by goal: a little
// less while losing body fat, with the rest from carbohydrate.
var goalFat = map[Goal]float64{
	GoalMaintain:      0.3,
	GoalBuildStrength: 0.3,
	GoalLoseBodyFat:   0.25,
}

// Recommend returns how much a person with the profile eats a day for
// their goal, in a year, or false if the profile lacks what that needs:
// height, weight, birth year, sex, activity and goal. Energy is the
// Mifflin-St Jeor estimate of the energy burned at rest, scaled by
// activity and goal; protein is by body weight and goal, fat a share of
// the energy, and carbohydrate the rest. They're general estimates, for
// the person to adopt as their targets or not.
func (p *Profile) Recommend(year int) (Recommendation, bool) {
	factor, active := activityFactors[p.Activity]
	scale, goal := goalEnergy[p.Goal]
	if !active || !goal || p.HeightCm <= 0 || p.WeightKg <= 0 || p.BirthYear <= 0 || p.Sex == SexUnknown {
		return Recommendation{}, false
	}
	age := float64(year) - float64(p.BirthYear)
	rest := 10*p.WeightKg + 6.25*p.HeightCm - 5*age
	if p.Sex == SexMale {
		rest += 5
	} else {
		rest -= 161
	}
	energy := rest * factor * scale
	protein := p.WeightKg * goalProtein[p.Goal]
	fat := energy * goalFat[p.Goal] / 9
	carbohydrate := math.Max((energy-protein*4-fat*9)/4, 0)
	round := func(v, to float64) float64 { return math.Round(v/to) * to }
	return Recommendation{
		EnergyKcal:        round(energy, 10),
		ProteinGrams:      round(protein, 5),
		CarbohydrateGrams: round(carbohydrate, 5),
		FatGrams:          round(fat, 5),
	}, true
}
