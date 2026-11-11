package services_test

import (
	"context"
	"slices"
	"testing"

	"connectrpc.com/connect/v2"
	foodsv1 "github.com/bs-tools/mealplann.ing/internal/gen/foods/v1"
	peoplev1 "github.com/bs-tools/mealplann.ing/internal/gen/people/v1"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

const (
	mainMeal  = peoplev1.EatingTimeKind_EATING_TIME_KIND_MAIN
	smallMeal = peoplev1.EatingTimeKind_EATING_TIME_KIND_SMALL
	breakfast = peoplev1.HouseholdMeal_HOUSEHOLD_MEAL_BREAKFAST
	lunch     = peoplev1.HouseholdMeal_HOUSEHOLD_MEAL_LUNCH
	dinner    = peoplev1.HouseholdMeal_HOUSEHOLD_MEAL_DINNER
	noMeal    = peoplev1.HouseholdMeal_HOUSEHOLD_MEAL_UNSPECIFIED
)

func eatingTime(name string, kind peoplev1.EatingTimeKind, time string, meal peoplev1.HouseholdMeal) *peoplev1.EatingTime {
	return peoplev1.EatingTime_builder{Name: name, Kind: kind, Time: time, Meal: meal}.Build()
}

// ben is someone who eats a regular breakfast, lunch and dinner, with no
// rules.
func ben() *peoplev1.Person {
	return peoplev1.Person_builder{
		Name: "Ben",
		EatingTimes: []*peoplev1.EatingTime{
			eatingTime("Breakfast", mainMeal, "07:30", breakfast),
			eatingTime("Lunch", mainMeal, "12:30", lunch),
			eatingTime("Dinner", mainMeal, "19:00", dinner),
		},
		DailyTargets: []*peoplev1.NutrientTarget{
			peoplev1.NutrientTarget_builder{Nutrient: foodsv1.Nutrient_NUTRIENT_PROTEIN_GRAMS, MinPerDay: new(60.0)}.Build(),
		},
	}.Build()
}

// anna is someone who had a total gastrectomy and has reactive
// hypoglycemia: she eats six small meals a day, three of them from the
// shared meals, with the rules her care team gave.
func anna() *peoplev1.Person {
	return peoplev1.Person_builder{
		Name: "Anna",
		EatingTimes: []*peoplev1.EatingTime{
			eatingTime("Breakfast", mainMeal, "07:30", breakfast),
			eatingTime("Mid-morning", smallMeal, "10:00", noMeal),
			eatingTime("Lunch", mainMeal, "12:30", lunch),
			eatingTime("Mid-afternoon", smallMeal, "15:30", noMeal),
			eatingTime("Dinner", mainMeal, "19:00", dinner),
			eatingTime("Evening", smallMeal, "21:00", noMeal),
		},
		Rules: peoplev1.EatingRules_builder{
			MaxPortionGrams:      new(200.0),
			MinProteinGrams:      new(10.0),
			MaxCarbohydrateGrams: new(30.0),
			MinPairingRatio:      new(0.5),
			MaxSugarsGrams:       new(10.0),
			WarnFastCarbs:        true,
		}.Build(),
		DailyTargets: []*peoplev1.NutrientTarget{
			peoplev1.NutrientTarget_builder{Nutrient: foodsv1.Nutrient_NUTRIENT_IRON_MILLIGRAMS, MinPerDay: new(14.0)}.Build(),
			peoplev1.NutrientTarget_builder{Nutrient: foodsv1.Nutrient_NUTRIENT_PROTEIN_GRAMS, MinPerDay: new(60.0), MaxPerDay: new(120.0)}.Build(),
		},
		Notes: "Values from the dietitian, March.",
	}.Build()
}

func createPerson(t *testing.T, e *env, p *peoplev1.Person) *peoplev1.Person {
	t.Helper()
	resp, err := e.people.CreatePerson(context.Background(), peoplev1.CreatePersonRequest_builder{Person: p}.Build())
	if err != nil {
		t.Fatalf("CreatePerson: %v", err)
	}
	return resp.GetPerson()
}

// eatingTimeID returns the ID of a person's eating time by name.
func eatingTimeID(t *testing.T, p *peoplev1.Person, name string) string {
	t.Helper()
	for _, et := range p.GetEatingTimes() {
		if et.GetName() == name {
			return et.GetId()
		}
	}
	t.Fatalf("%s has no eating time %s", p.GetName(), name)
	return ""
}

func eatingTimeNames(p *peoplev1.Person) []string {
	var names []string
	for _, et := range p.GetEatingTimes() {
		names = append(names, et.GetName())
	}
	return names
}

func TestPeople(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()

	// Create the two people, Ben being the signed in user.
	b := ben()
	b.SetUserId(store.UUIDToString(e.userID))
	createdBen := createPerson(t, e, b)
	createdAnna := createPerson(t, e, anna())
	if createdBen.GetId() == "" || createdBen.GetUserId() != store.UUIDToString(e.userID) || createdBen.HasRules() ||
		!slices.Equal(eatingTimeNames(createdBen), []string{"Breakfast", "Lunch", "Dinner"}) {
		t.Errorf("got %v, want Ben", createdBen)
	}
	for _, et := range createdAnna.GetEatingTimes() {
		if et.GetId() == "" {
			t.Errorf("eating time %v has no ID", et)
		}
	}
	if r := createdAnna.GetRules(); r.GetMaxPortionGrams() != 200 || r.GetMinPairingRatio() != 0.5 || !r.GetWarnFastCarbs() || !r.HasMaxCarbohydrateGrams() {
		t.Errorf("got rules %v, want Anna's", r)
	}
	// Targets come back in the nutrients' order.
	if targets := createdAnna.GetDailyTargets(); len(targets) != 2 ||
		targets[0].GetNutrient() != foodsv1.Nutrient_NUTRIENT_PROTEIN_GRAMS || targets[0].GetMaxPerDay() != 120 ||
		targets[1].GetNutrient() != foodsv1.Nutrient_NUTRIENT_IRON_MILLIGRAMS || targets[1].HasMaxPerDay() {
		t.Errorf("got targets %v, want protein then iron", targets)
	}
	if createdAnna.GetEatingTimes()[4].GetMeal() != dinner || createdAnna.GetEatingTimes()[5].GetTime() != "21:00" {
		t.Errorf("got eating times %v, want Anna's", createdAnna.GetEatingTimes())
	}

	// People are listed by name.
	list, err := e.people.ListPeople(ctx, peoplev1.ListPeopleRequest_builder{}.Build())
	if err != nil {
		t.Fatalf("ListPeople: %v", err)
	}
	if len(list.GetPeople()) != 2 || list.GetPeople()[0].GetName() != "Anna" || len(list.GetPeople()[0].GetEatingTimes()) != 6 {
		t.Errorf("got %v, want Anna and Ben", list.GetPeople())
	}

	// A rule changes on its own.
	updated, err := e.people.UpdatePerson(ctx, peoplev1.UpdatePersonRequest_builder{
		Person: peoplev1.Person_builder{
			Id:    createdAnna.GetId(),
			Rules: peoplev1.EatingRules_builder{MinProteinGrams: new(12.0)}.Build(),
		}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"rules.min_protein_grams"}},
	}.Build())
	if err != nil {
		t.Fatalf("UpdatePerson: %v", err)
	}
	if r := updated.GetPerson().GetRules(); r.GetMinProteinGrams() != 12 || r.GetMaxPortionGrams() != 200 || !r.GetWarnFastCarbs() {
		t.Errorf("got rules %v, want only the protein changed", r)
	}
	// A rule left unset in the mask is removed.
	updated, err = e.people.UpdatePerson(ctx, peoplev1.UpdatePersonRequest_builder{
		Person:     peoplev1.Person_builder{Id: createdAnna.GetId()}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"rules.max_sugars_grams"}},
	}.Build())
	if err != nil {
		t.Fatalf("UpdatePerson: %v", err)
	}
	if r := updated.GetPerson().GetRules(); r.HasMaxSugarsGrams() || r.GetMinProteinGrams() != 12 {
		t.Errorf("got rules %v, want no maximum of sugars", r)
	}

	// A profile and tastes are kept, and the profile gives a recommendation.
	updated, err = e.people.UpdatePerson(ctx, peoplev1.UpdatePersonRequest_builder{
		Person: peoplev1.Person_builder{
			Id: createdAnna.GetId(),
			Profile: peoplev1.Profile_builder{
				HeightCm: new(168.0), WeightKg: new(62.0), BirthYear: 1990,
				Sex: peoplev1.Sex_SEX_FEMALE, Activity: peoplev1.Activity_ACTIVITY_LIGHT, Goal: peoplev1.Goal_GOAL_LOSE_BODY_FAT,
			}.Build(),
			Tastes: peoplev1.Tastes_builder{
				SmallMeals: peoplev1.SmallMealTaste_SMALL_MEAL_TASTE_SAVORY,
				Dislikes:   []string{" mushrooms ", "Mushrooms", ""},
			}.Build(),
		}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"profile", "tastes"}},
	}.Build())
	if err != nil {
		t.Fatalf("UpdatePerson: %v", err)
	}
	if p := updated.GetPerson(); p.GetProfile().GetWeightKg() != 62 || p.GetRecommended().GetProteinGrams() == 0 ||
		!slices.Equal(p.GetTastes().GetDislikes(), []string{"mushrooms"}) || p.GetRules().GetMinProteinGrams() != 12 {
		t.Errorf("got %v, want the profile, a recommendation and one dislike", p)
	}
	_, err = e.people.UpdatePerson(ctx, peoplev1.UpdatePersonRequest_builder{
		Person:     peoplev1.Person_builder{Id: createdAnna.GetId(), Profile: peoplev1.Profile_builder{WeightKg: new(2.0)}.Build()}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"profile.weight_kg"}},
	}.Build())
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("got %v for a weight of 2 kg, want invalid argument", err)
	}

	// Eating times keep their IDs when renamed and reordered, and new ones
	// get new IDs.
	ets := updated.GetPerson().GetEatingTimes()
	ets[1].SetName("Snack")
	reordered := []*peoplev1.EatingTime{ets[0], ets[2], ets[1], ets[3], ets[4], ets[5], eatingTime("Night", smallMeal, "", noMeal)}
	updated, err = e.people.UpdatePerson(ctx, peoplev1.UpdatePersonRequest_builder{
		Person:     peoplev1.Person_builder{Id: createdAnna.GetId(), EatingTimes: reordered}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"eating_times"}},
	}.Build())
	if err != nil {
		t.Fatalf("UpdatePerson: %v", err)
	}
	got := updated.GetPerson().GetEatingTimes()
	if !slices.Equal(eatingTimeNames(updated.GetPerson()), []string{"Breakfast", "Lunch", "Snack", "Mid-afternoon", "Dinner", "Evening", "Night"}) ||
		got[2].GetId() != ets[1].GetId() || got[1].GetId() != ets[2].GetId() || got[6].GetId() == "" {
		t.Errorf("got eating times %v, want them reordered with their IDs", got)
	}
	if updated.GetPerson().GetRules().GetMinProteinGrams() != 12 || updated.GetPerson().GetNotes() != "Values from the dietitian, March." {
		t.Errorf("got %v, want the rest unchanged", updated.GetPerson())
	}

	// Invalid people are refused, saying what's wrong.
	for name, p := range map[string]*peoplev1.Person{
		"no name": peoplev1.Person_builder{Name: " "}.Build(),
		"no kind": peoplev1.Person_builder{Name: "Cleo", EatingTimes: []*peoplev1.EatingTime{
			peoplev1.EatingTime_builder{Name: "Breakfast"}.Build(),
		}}.Build(),
		"bad time": peoplev1.Person_builder{Name: "Cleo", EatingTimes: []*peoplev1.EatingTime{
			eatingTime("Breakfast", mainMeal, "7:30am", breakfast),
		}}.Build(),
		"meal twice": peoplev1.Person_builder{Name: "Cleo", EatingTimes: []*peoplev1.EatingTime{
			eatingTime("Lunch", mainMeal, "", lunch), eatingTime("Late lunch", mainMeal, "", lunch),
		}}.Build(),
		"negative rule": peoplev1.Person_builder{Name: "Cleo", Rules: peoplev1.EatingRules_builder{MinProteinGrams: new(-1.0)}.Build()}.Build(),
		"zero portion":  peoplev1.Person_builder{Name: "Cleo", Rules: peoplev1.EatingRules_builder{MaxPortionGrams: new(0.0)}.Build()}.Build(),
		"empty target": peoplev1.Person_builder{Name: "Cleo", DailyTargets: []*peoplev1.NutrientTarget{
			peoplev1.NutrientTarget_builder{Nutrient: foodsv1.Nutrient_NUTRIENT_IRON_MILLIGRAMS}.Build(),
		}}.Build(),
		"min over max": peoplev1.Person_builder{Name: "Cleo", DailyTargets: []*peoplev1.NutrientTarget{
			peoplev1.NutrientTarget_builder{Nutrient: foodsv1.Nutrient_NUTRIENT_IRON_MILLIGRAMS, MinPerDay: new(20.0), MaxPerDay: new(10.0)}.Build(),
		}}.Build(),
		"target twice": peoplev1.Person_builder{Name: "Cleo", DailyTargets: []*peoplev1.NutrientTarget{
			peoplev1.NutrientTarget_builder{Nutrient: foodsv1.Nutrient_NUTRIENT_IRON_MILLIGRAMS, MinPerDay: new(10.0)}.Build(),
			peoplev1.NutrientTarget_builder{Nutrient: foodsv1.Nutrient_NUTRIENT_IRON_MILLIGRAMS, MaxPerDay: new(40.0)}.Build(),
		}}.Build(),
		"no nutrient": peoplev1.Person_builder{Name: "Cleo", DailyTargets: []*peoplev1.NutrientTarget{
			peoplev1.NutrientTarget_builder{MinPerDay: new(10.0)}.Build(),
		}}.Build(),
		"another household's user": peoplev1.Person_builder{Name: "Cleo", UserId: store.UUIDToString(e.otherUserID)}.Build(),
	} {
		_, err := e.people.CreatePerson(ctx, peoplev1.CreatePersonRequest_builder{Person: p}.Build())
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s: got %v, want InvalidArgument", name, err)
		}
	}
	// A user is at most one person.
	_, err = e.people.CreatePerson(ctx, peoplev1.CreatePersonRequest_builder{
		Person: peoplev1.Person_builder{Name: "Benjamin", UserId: store.UUIDToString(e.userID)}.Build(),
	}.Build())
	wantCode(t, err, connect.CodeAlreadyExists)
	// An eating time ID must be the person's.
	_, err = e.people.UpdatePerson(ctx, peoplev1.UpdatePersonRequest_builder{
		Person: peoplev1.Person_builder{Id: createdBen.GetId(), EatingTimes: []*peoplev1.EatingTime{
			peoplev1.EatingTime_builder{Id: got[0].GetId(), Name: "Breakfast", Kind: mainMeal}.Build(),
		}}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"eating_times"}},
	}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)
	// Unknown mask paths are refused.
	_, err = e.people.UpdatePerson(ctx, peoplev1.UpdatePersonRequest_builder{
		Person:     peoplev1.Person_builder{Id: createdBen.GetId()}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"rules.max_fun"}},
	}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)

	// Another household can't see or change the people.
	otherList, err := e.otherPeople.ListPeople(ctx, peoplev1.ListPeopleRequest_builder{}.Build())
	if err != nil {
		t.Fatalf("ListPeople: %v", err)
	}
	if len(otherList.GetPeople()) != 0 {
		t.Errorf("another household sees %v", otherList.GetPeople())
	}
	_, err = e.otherPeople.GetPerson(ctx, peoplev1.GetPersonRequest_builder{Id: createdAnna.GetId()}.Build())
	wantCode(t, err, connect.CodeNotFound)
	_, err = e.otherPeople.UpdatePerson(ctx, peoplev1.UpdatePersonRequest_builder{
		Person: peoplev1.Person_builder{Id: createdAnna.GetId(), Name: "Mallory"}.Build(),
	}.Build())
	wantCode(t, err, connect.CodeNotFound)
	_, err = e.otherPeople.DeletePerson(ctx, peoplev1.DeletePersonRequest_builder{Id: createdAnna.GetId()}.Build())
	wantCode(t, err, connect.CodeNotFound)
	_, err = e.anonPeople.ListPeople(ctx, peoplev1.ListPeopleRequest_builder{}.Build())
	wantCode(t, err, connect.CodeUnauthenticated)

	// Delete Ben.
	if _, err := e.people.DeletePerson(ctx, peoplev1.DeletePersonRequest_builder{Id: createdBen.GetId()}.Build()); err != nil {
		t.Fatalf("DeletePerson: %v", err)
	}
	_, err = e.people.GetPerson(ctx, peoplev1.GetPersonRequest_builder{Id: createdBen.GetId()}.Build())
	wantCode(t, err, connect.CodeNotFound)
}
