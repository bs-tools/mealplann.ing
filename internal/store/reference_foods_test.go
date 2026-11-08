package store_test

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/bs-tools/foodplann.ing/internal/store"
	"github.com/bs-tools/foodplann.ing/internal/store/storetest"
)

// migrationsUpTo returns the migrations up to and including version, by
// their file names' prefix.
func migrationsUpTo(t *testing.T, version string) fstest.MapFS {
	t.Helper()
	entries, err := os.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	fsys := fstest.MapFS{}
	for _, e := range entries {
		if e.Name() > version+"~" {
			continue
		}
		data, err := fs.ReadFile(os.DirFS("migrations"), e.Name())
		if err != nil {
			t.Fatal(err)
		}
		fsys["migrations/"+e.Name()] = &fstest.MapFile{Data: data}
	}
	return fsys
}

// TestReferenceFoodsMigration checks that migration 005 copies the FDC
// foods into reference_foods, and links the households' foods from FDC to
// them, while the previous revision's columns and table stay.
func TestReferenceFoodsMigration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := storetest.New(t)
	// The migrations run in a schema of their own, finding pg_trgm, which
	// the test database already has, in public.
	if _, err := db.Exec(ctx, "create schema reference_test"); err != nil {
		t.Fatal(err)
	}
	conf := *db.Config().ConnConfig.Copy()
	conf.RuntimeParams["search_path"] = "reference_test, public"
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := store.MigrateSchema(ctx, logger, migrationsUpTo(t, "004"), conf, "postgres"); err != nil {
		t.Fatalf("MigrateSchema to 004: %v", err)
	}

	// The previous revision's data: an FDC food, a household's food made
	// from it and a custom food.
	if _, err := db.Exec(ctx, `
		insert into reference_test.fdc_foods (fdc_id, data_type, description, category, portions, publication_date, protein_grams_per_100_grams)
		values (173756, 'sr_legacy_food', 'Chickpeas, mature seeds, raw', 'Legumes and Legume Products',
		  '[{"description": "1 cup", "amount": 1, "grams": 200}]', '2019-04-01', 20.47);
		with h as (insert into reference_test.households (name) values ('Home') returning id)
		insert into reference_test.foods (household_id, name, fdc_id)
		select id, 'Chickpeas', 173756 from h
		union all
		select id, 'Soy yogurt', null from h;
	`); err != nil {
		t.Fatal(err)
	}

	if err := store.MigrateSchema(ctx, logger, migrationsUpTo(t, "999"), conf, "postgres"); err != nil {
		t.Fatalf("MigrateSchema: %v", err)
	}

	var source, description, portions string
	var protein float64
	if err := db.QueryRow(ctx, `
		select source::text, description, portions::text, protein_grams_per_100_grams
		from reference_test.reference_foods where source_id = 173756`).Scan(&source, &description, &portions, &protein); err != nil {
		t.Fatalf("reading the reference food: %v", err)
	}
	if source != "fdc" || description != "Chickpeas, mature seeds, raw" || !strings.Contains(portions, `"grams": 200`) || protein != 20.47 {
		t.Errorf("got %s food %q with portions %s and %v g protein, want FDC's chickpeas", source, description, portions, protein)
	}

	rows, err := db.Query(ctx, `
		select name, coalesce(reference_source::text, ''), coalesce(reference_id, 0), coalesce(fdc_id, 0)
		from reference_test.foods order by name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var name, refSource string
		var refID, fdcID int32
		if err := rows.Scan(&name, &refSource, &refID, &fdcID); err != nil {
			t.Fatal(err)
		}
		got = append(got, fmt.Sprintf("%s/%s/%d/%d", name, refSource, refID, fdcID))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	// The food from FDC is linked to it, keeping its FDC ID for the
	// previous revision, and the custom food has no reference.
	if want := "Chickpeas/fdc/173756/173756,Soy yogurt//0/0"; strings.Join(got, ",") != want {
		t.Errorf("got foods %v, want %s", got, want)
	}

	// A reference must have both its source and its ID.
	if _, err := db.Exec(ctx, `update reference_test.foods set reference_id = null where name = 'Chickpeas'`); err == nil {
		t.Error("cleared a food's reference ID without its source")
	}
}
