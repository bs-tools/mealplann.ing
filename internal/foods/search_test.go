package foods

import (
	"context"
	"log/slog"
	"testing"

	"github.com/bs-tools/mealplann.ing/internal/store/storetest"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestTextQuery(t *testing.T) {
	t.Parallel()
	for query, want := range map[string]string{
		"lentils, raw":         "lentils & raw",
		"Nori sheets":          "(nori | laver | seaweed) & sheets",
		"garbanzos":            "(chickpea | garbanzo)",
		"unsweetened soy milk": "unsweetened & ((soy & milk) | soymilk | (soy & beverage))",
		"green onions":         "(scallion | (green & onion) | (spring & onion))",
		"it's: a 'test' & | !": "it & s & a & test",
		"  ":                   "",
	} {
		if got := textQuery(query); got != want {
			t.Errorf("textQuery(%q) = %q, want %q", query, got, want)
		}
	}
}

func TestSearchReferenceFoods(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := NewRepository(slog.New(slog.DiscardHandler), storetest.New(t))
	var foods []*ReferenceFood
	for i, description := range []string{
		"Mission Foods, flour tortillas",
		"Miso",
		"Seaweed, laver, raw",
		"Bread, italian",
	} {
		foods = append(foods, &ReferenceFood{Source: SourceFDC, SourceID: int32(i + 1), DataType: "sr_legacy_food", Description: description})
	}
	if err := repo.UpsertReferenceFoods(ctx, foods); err != nil {
		t.Fatalf("UpsertReferenceFoods: %v", err)
	}
	for query, want := range map[string]string{
		// The word comes before a longer word sharing its trigrams.
		"miso": "Miso",
		// Names the sources don't use find foods by their synonyms.
		"nori":           "Seaweed, laver, raw",
		"ciabatta bread": "Bread, italian",
		"doenjang":       "Miso",
	} {
		matches, err := repo.SearchReferenceFoods(ctx, pgtype.UUID{}, query, 10)
		if err != nil {
			t.Fatalf("SearchReferenceFoods(%q): %v", query, err)
		}
		if len(matches) == 0 || matches[0].Description != want {
			var got []string
			for _, m := range matches {
				got = append(got, m.Description)
			}
			t.Errorf("SearchReferenceFoods(%q) = %v, want %q first", query, got, want)
		}
	}
}
