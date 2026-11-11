package people

import "testing"

func TestRecommend(t *testing.T) {
	t.Parallel()
	p := Profile{HeightCm: 168, WeightKg: 62, BirthYear: 1990, Sex: SexFemale, Activity: ActivityLight, Goal: GoalLoseBodyFat}
	got, ok := p.Recommend(2026)
	if !ok {
		t.Fatal("got no recommendation for a whole profile")
	}
	// At rest: 620 + 1050 - 180 - 161 = 1329 kcal, × 1.375 × 0.8.
	if got.EnergyKcal != 1460 || got.ProteinGrams != 110 {
		t.Errorf("got %v, want 1460 kcal and 110 g of protein", got)
	}
	if got.FatGrams != 40 || got.CarbohydrateGrams != 165 {
		t.Errorf("got %v, want 40 g of fat and 165 g of carbohydrate", got)
	}
	p.Goal = GoalBuildStrength
	if more, _ := p.Recommend(2026); more.EnergyKcal <= got.EnergyKcal || more.ProteinGrams >= got.ProteinGrams {
		t.Errorf("got %v building strength, want more energy and less protein than losing body fat", more)
	}
	p.Sex = SexUnknown
	if _, ok := p.Recommend(2026); ok {
		t.Error("got a recommendation without the sex")
	}
}
