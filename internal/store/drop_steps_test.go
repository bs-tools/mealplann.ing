package store_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/bs-tools/mealplann.ing/internal/store/storetest"
)

// TestDropStepsMigration checks that migration 016 gives a recipe still in
// the old format of plain steps an instruction for each, keeps the
// instructions of the others, and drops the steps.
func TestDropStepsMigration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := storetest.New(t)
	if _, err := db.Exec(ctx, "create schema drop_steps_test"); err != nil {
		t.Fatal(err)
	}
	conf := *db.Config().ConnConfig.Copy()
	conf.RuntimeParams["search_path"] = "drop_steps_test, public"
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := store.MigrateSchema(ctx, logger, migrationsUpTo(t, "015"), conf, "postgres"); err != nil {
		t.Fatalf("MigrateSchema to 015: %v", err)
	}
	if _, err := db.Exec(ctx, `
		with h as (insert into drop_steps_test.households (name) values ('Home') returning id)
		insert into drop_steps_test.recipes (household_id, name, servings, steps, instructions)
		select id, 'Old', 2, array['Soak the beans.', 'Simmer them.'], null from h
		union all
		select id, 'New', 2, array['Fry.'], '[{"text": "Fry.", "duration_seconds": 300}]'::jsonb from h
		union all
		select id, 'Empty', 2, '{}', null from h;
	`); err != nil {
		t.Fatal(err)
	}

	if err := store.MigrateSchema(ctx, logger, migrationsUpTo(t, "999"), conf, "postgres"); err != nil {
		t.Fatalf("MigrateSchema: %v", err)
	}
	want := map[string]string{
		"Old":   `[{"text": "Soak the beans."}, {"text": "Simmer them."}]`,
		"New":   `[{"text": "Fry.", "duration_seconds": 300}]`,
		"Empty": `[]`,
	}
	rows, err := db.Query(ctx, `select name, instructions::text from drop_steps_test.recipes`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var name, instructions string
		if err := rows.Scan(&name, &instructions); err != nil {
			t.Fatal(err)
		}
		if instructions != want[name] {
			t.Errorf("%s: got instructions %s, want %s", name, instructions, want[name])
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	var columns int
	if err := db.QueryRow(ctx, `select count(*) from information_schema.columns
		where table_schema = 'drop_steps_test' and table_name = 'recipes' and column_name = 'steps'`).Scan(&columns); err != nil {
		t.Fatal(err)
	}
	if columns != 0 {
		t.Error("got the steps column, want it dropped")
	}
}
