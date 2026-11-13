package recipes

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/bs-tools/mealplann.ing/internal/blob"
	"github.com/bs-tools/mealplann.ing/internal/recipes/internal/gen"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

// SetCache makes the repository keep the households' recipes in the store
// between the server's starts, as well as in memory. The server scales to
// zero, so without it the first list after each start reads them all from
// the database; reading them from Cloud Storage in the same region costs
// nothing.
func (r *Repository) SetCache(cache blob.Store) {
	r.cache = cache
}

// recipesVersion is what changes whenever a household's recipes, or the
// foods they're made of, do: see GetRecipesVersion. Times are in
// microseconds, Postgres's precision, to compare exactly whether read from
// the database or the cache.
type recipesVersion struct {
	Recipes             int64
	RecipesUpdateMicros int64
	Foods               int64
	FoodsUpdateMicros   int64
}

// snapshot is all of a household's recipes as read from the database, at
// a version, in order of name. Its rows are never changed, as requests
// share them; each makes its own Recipes from them.
type snapshot struct {
	version     recipesVersion
	rows        []*gen.Recipe
	tags        []string
	ingredients map[pgtype.UUID][]*gen.ListRecipeIngredientsRow
	sides       map[pgtype.UUID][]*gen.ListRecipeSidesRow
}

// storedSnapshot is a snapshot as kept in the cache.
type storedSnapshot struct {
	Version     recipesVersion
	Recipes     []*gen.Recipe
	Tags        []string
	Ingredients []*gen.ListRecipeIngredientsRow
	Sides       []*gen.ListRecipeSidesRow
}

// snapshotKey names a household's snapshot in the cache, by the rows'
// types, so that one stored before a migration changed them isn't read.
var snapshotKeyPrefix = "cache/recipes/" + blob.Fingerprint(gen.Recipe{}, gen.ListRecipeIngredientsRow{}, gen.ListRecipeSidesRow{}) + "/"

// snapshot returns the household's recipes at the version. They're those
// in memory, or else in the cache, if they're of it. Otherwise only what
// changed since is read: the recipes changed, and those with ingredients
// whose foods changed, with all the sides, whose names change with their
// foods and recipes, and the recipes' IDs and update times, to find what
// changed or was deleted. That's kilobytes for a change of a recipe or a
// food, where reading every recipe is megabytes. Only a household with
// none kept anywhere reads them all.
func (r *Repository) snapshot(ctx context.Context, householdID pgtype.UUID, version recipesVersion) (*snapshot, error) {
	r.mu.Lock()
	base := r.snapshots[householdID]
	r.mu.Unlock()
	if base != nil && base.version == version {
		return base, nil
	}
	if base == nil {
		base = r.loadSnapshot(ctx, householdID)
	}
	snap := base
	if base == nil || base.version != version {
		var err error
		if base == nil {
			snap, err = r.readSnapshot(ctx, householdID, version)
		} else {
			snap, err = r.refreshSnapshot(ctx, householdID, base, version)
		}
		if err != nil {
			return nil, err
		}
		r.saveSnapshot(ctx, householdID, snap)
	}
	// A write between reading the version and the recipes makes them
	// newer than it says, which only means reading what changed again.
	r.mu.Lock()
	r.snapshots[householdID] = snap
	r.mu.Unlock()
	return snap, nil
}

// readSnapshot reads all the household's recipes.
func (r *Repository) readSnapshot(ctx context.Context, householdID pgtype.UUID, version recipesVersion) (*snapshot, error) {
	snap := &snapshot{version: version}
	var ingredients []*gen.ListRecipeIngredientsRow
	var sides []*gen.ListRecipeSidesRow
	var b store.Batch
	b.Read(func(db store.Querier) error {
		var err error
		snap.rows, err = gen.New(db).ListRecipes(ctx, &gen.ListRecipesParams{
			HouseholdID: householdID,
			Tags:        []string{},
			FoodIds:     []pgtype.UUID{},
		})
		return err
	})
	b.Read(func(db store.Querier) error {
		var err error
		snap.tags, err = gen.New(db).ListRecipeTags(ctx, householdID)
		return err
	})
	b.Read(func(db store.Querier) error {
		var err error
		ingredients, err = gen.New(db).ListRecipeIngredients(ctx, &gen.ListRecipeIngredientsParams{
			HouseholdID: householdID,
			AllRecipes:  true,
		})
		return err
	})
	readSides(ctx, &b, householdID, nil, true, &sides)
	if err := b.Send(ctx, r.db); err != nil {
		return nil, fmt.Errorf("failed to list recipes: %w", err)
	}
	snap.index(ingredients, sides)
	return snap, nil
}

// refreshSnapshot reads what changed of the household's recipes since a
// snapshot of them.
func (r *Repository) refreshSnapshot(ctx context.Context, householdID pgtype.UUID, base *snapshot, version recipesVersion) (*snapshot, error) {
	snap := &snapshot{version: version}
	var stamps []*gen.ListRecipeStampsRow
	var changedFoods []pgtype.UUID
	var sides []*gen.ListRecipeSidesRow
	var b store.Batch
	b.Read(func(db store.Querier) error {
		var err error
		stamps, err = gen.New(db).ListRecipeStamps(ctx, householdID)
		return err
	})
	b.Read(func(db store.Querier) error {
		var err error
		changedFoods, err = gen.New(db).ListFoodIDsChangedSince(ctx, &gen.ListFoodIDsChangedSinceParams{
			HouseholdID: householdID,
			Since:       microsToPG(base.version.FoodsUpdateMicros),
		})
		return err
	})
	b.Read(func(db store.Querier) error {
		var err error
		snap.tags, err = gen.New(db).ListRecipeTags(ctx, householdID)
		return err
	})
	readSides(ctx, &b, householdID, nil, true, &sides)
	if err := b.Send(ctx, r.db); err != nil {
		return nil, fmt.Errorf("failed to list recipes: %w", err)
	}

	baseRows := make(map[pgtype.UUID]*gen.Recipe, len(base.rows))
	for _, row := range base.rows {
		baseRows[row.ID] = row
	}
	foodChanged := make(map[pgtype.UUID]bool, len(changedFoods))
	for _, id := range changedFoods {
		foodChanged[id] = true
	}
	var reread []pgtype.UUID
	for _, st := range stamps {
		row, ok := baseRows[st.ID]
		changed := !ok || row.UpdateTime.Time.UnixMicro() != st.UpdateTime.Time.UnixMicro()
		for _, in := range base.ingredients[st.ID] {
			changed = changed || foodChanged[in.RecipeIngredient.FoodID]
		}
		if changed {
			reread = append(reread, st.ID)
		}
	}
	var rows []*gen.Recipe
	var ingredients []*gen.ListRecipeIngredientsRow
	if len(reread) > 0 {
		var b store.Batch
		b.Read(func(db store.Querier) error {
			var err error
			rows, err = gen.New(db).ListRecipesByID(ctx, &gen.ListRecipesByIDParams{HouseholdID: householdID, Ids: reread})
			return err
		})
		b.Read(func(db store.Querier) error {
			var err error
			ingredients, err = gen.New(db).ListRecipeIngredients(ctx, &gen.ListRecipeIngredientsParams{
				HouseholdID: householdID,
				RecipeIds:   reread,
			})
			return err
		})
		if err := b.Send(ctx, r.db); err != nil {
			return nil, fmt.Errorf("failed to list recipes: %w", err)
		}
	}

	// The recipes are those unchanged, which are still there, and those
	// read again; recipes deleted since are in neither.
	rereadSet := make(map[pgtype.UUID]bool, len(reread))
	for _, id := range reread {
		rereadSet[id] = true
	}
	for _, st := range stamps {
		if rereadSet[st.ID] {
			continue
		}
		rows = append(rows, baseRows[st.ID])
		ingredients = append(ingredients, base.ingredients[st.ID]...)
	}
	snap.rows = rows
	snap.index(ingredients, sides)
	return snap, nil
}

// index orders the snapshot's recipes by name, and indexes their
// ingredients and sides by recipe.
func (s *snapshot) index(ingredients []*gen.ListRecipeIngredientsRow, sides []*gen.ListRecipeSidesRow) {
	slices.SortFunc(s.rows, func(a, b *gen.Recipe) int {
		if c := cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); c != 0 {
			return c
		}
		return bytes.Compare(a.ID.Bytes[:], b.ID.Bytes[:])
	})
	s.ingredients = map[pgtype.UUID][]*gen.ListRecipeIngredientsRow{}
	for _, in := range ingredients {
		s.ingredients[in.RecipeIngredient.RecipeID] = append(s.ingredients[in.RecipeIngredient.RecipeID], in)
	}
	for _, list := range s.ingredients {
		slices.SortFunc(list, func(a, b *gen.ListRecipeIngredientsRow) int {
			return cmp.Compare(a.RecipeIngredient.Position, b.RecipeIngredient.Position)
		})
	}
	s.sides = map[pgtype.UUID][]*gen.ListRecipeSidesRow{}
	for _, side := range sides {
		s.sides[side.RecipeID] = append(s.sides[side.RecipeID], side)
	}
}

// loadSnapshot returns the household's recipes kept in the cache, or nil
// if there are none, or they can't be read, which only means reading them
// from the database.
func (r *Repository) loadSnapshot(ctx context.Context, householdID pgtype.UUID) *snapshot {
	if r.cache == nil {
		return nil
	}
	var stored storedSnapshot
	found, err := blob.GetJSON(ctx, r.cache, snapshotKeyPrefix+store.UUIDToString(householdID)+".json.gz", &stored)
	if err != nil {
		r.log.WarnContext(ctx, "Failed to read the recipes kept in the cache, reading them from the database", slog.Any("error", err))
		return nil
	}
	if !found {
		return nil
	}
	snap := &snapshot{version: stored.Version, rows: stored.Recipes, tags: stored.Tags}
	snap.index(stored.Ingredients, stored.Sides)
	return snap
}

// saveSnapshot keeps the household's recipes in the cache. Failing to only
// means reading them from the database after the next start.
func (r *Repository) saveSnapshot(ctx context.Context, householdID pgtype.UUID, snap *snapshot) {
	if r.cache == nil {
		return
	}
	stored := storedSnapshot{Version: snap.version, Recipes: snap.rows, Tags: snap.tags}
	for _, row := range snap.rows {
		stored.Ingredients = append(stored.Ingredients, snap.ingredients[row.ID]...)
		stored.Sides = append(stored.Sides, snap.sides[row.ID]...)
	}
	if err := blob.PutJSON(ctx, r.cache, snapshotKeyPrefix+store.UUIDToString(householdID)+".json.gz", stored); err != nil {
		r.log.WarnContext(ctx, "Failed to keep the recipes in the cache", slog.Any("error", err))
	}
}

// microsToPG returns a time in microseconds since the epoch as a
// timestamptz.
func microsToPG(micros int64) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: time.UnixMicro(micros), Valid: true}
}
