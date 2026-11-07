package pantry

import (
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func date(s string) time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return t
}

func id(b byte) pgtype.UUID {
	return pgtype.UUID{Bytes: [16]byte{b}, Valid: true}
}

func TestUseBy(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		item Item
		want time.Time
	}{
		"nothing known":        {Item{}, time.Time{}},
		"expiry":               {Item{ExpiryDate: date("2026-11-10")}, date("2026-11-10")},
		"opened without shelf": {Item{OpenedDate: date("2026-11-01")}, time.Time{}},
		"opened, earlier": {
			Item{ExpiryDate: date("2026-12-01"), OpenedDate: date("2026-11-01"), OpenedShelfLifeDays: 4},
			date("2026-11-05"),
		},
		"opened, later than expiry": {
			Item{ExpiryDate: date("2026-11-03"), OpenedDate: date("2026-11-01"), OpenedShelfLifeDays: 4},
			date("2026-11-03"),
		},
		"opened, no expiry": {Item{OpenedDate: date("2026-11-01"), OpenedShelfLifeDays: 2}, date("2026-11-03")},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := tt.item.UseBy(); !got.Equal(tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDeduct(t *testing.T) {
	t.Parallel()
	lentils, rice, tofu := id(1), id(2), id(3)
	created := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	// Lentils: a lot without expiry, one expiring later, one sooner, and an
	// older lot without expiry.
	noExpiry := &Item{ID: id(10), FoodID: lentils, Grams: 500, CreateTime: created.Add(time.Hour)}
	later := &Item{ID: id(11), FoodID: lentils, Grams: 200, ExpiryDate: date("2026-12-01"), CreateTime: created}
	sooner := &Item{ID: id(12), FoodID: lentils, Grams: 100, ExpiryDate: date("2026-11-10"), CreateTime: created}
	older := &Item{ID: id(13), FoodID: lentils, Grams: 50, CreateTime: created}
	// Rice: 300 g, opened, expiring in a day.
	opened := &Item{ID: id(20), FoodID: rice, Grams: 300, OpenedDate: date("2026-11-01"), OpenedShelfLifeDays: 1, CreateTime: created}
	lots := []*Item{noExpiry, later, sooner, older, opened}

	type deduction struct {
		id    pgtype.UUID
		grams float64
	}
	tests := map[string]struct {
		uses []Use
		want []deduction
	}{
		"soonest first": {
			uses: []Use{{FoodID: lentils, Grams: 250}},
			want: []deduction{{sooner.ID, 100}, {later.ID, 150}},
		},
		"oldest lot without expiry after those with one": {
			uses: []Use{{FoodID: lentils, Grams: 320}},
			want: []deduction{{sooner.ID, 100}, {later.ID, 200}, {older.ID, 20}},
		},
		"never below zero": {
			uses: []Use{{FoodID: lentils, Grams: 2000}},
			want: []deduction{{sooner.ID, 100}, {later.ID, 200}, {older.ID, 50}, {noExpiry.ID, 500}},
		},
		"uses of a food add up, in the order of the foods": {
			uses: []Use{{FoodID: rice, Grams: 100}, {FoodID: lentils, Grams: 50}, {FoodID: rice, Grams: 50}},
			want: []deduction{{opened.ID, 150}, {sooner.ID, 50}},
		},
		"a crumb left empties the lot": {
			uses: []Use{{FoodID: rice, Grams: 299.8}},
			want: []deduction{{opened.ID, 300}},
		},
		"nothing in stock": {
			uses: []Use{{FoodID: tofu, Grams: 200}},
		},
		"nothing used": {
			uses: []Use{{FoodID: rice, Grams: 0}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var got []deduction
			for _, d := range Deduct(lots, tt.uses) {
				got = append(got, deduction{d.Item.ID, d.Grams})
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
	// The lots don't change.
	if noExpiry.Grams != 500 || lots[0] != noExpiry {
		t.Errorf("Deduct changed the lots")
	}
}
