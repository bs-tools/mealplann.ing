package foods

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
	"github.com/bs-tools/mealplann.ing/internal/foods/internal/gen"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

// SetCache makes the repository keep the households' foods in the store
// between the server's starts, as well as in memory. The server scales to
// zero, so without it the first read after each start reads them all from
// the database; reading them from Cloud Storage in the same region costs
// nothing.
func (r *Repository) SetCache(cache blob.Store) {
	r.cache = cache
}

// foodsVersion is what changes whenever a household's foods do: see
// GetFoodsVersion. The time is in microseconds, Postgres's precision, to
// compare exactly whether read from the database or the cache.
type foodsVersion struct {
	Count        int64
	UpdateMicros int64
}

// foodSnapshot is all of a household's foods as read from the database, at
// a version, in order of name. Its rows are never changed, as requests
// share them.
type foodSnapshot struct {
	Version foodsVersion
	Rows    []*gen.Food
}

// foodSnapshotKeyPrefix names households' snapshots in the cache, by the
// rows' type, so that one stored before a migration changed it isn't read.
var foodSnapshotKeyPrefix = "cache/foods/" + blob.Fingerprint(gen.Food{}) + "/"

// snapshot returns the household's foods at the version. They're those in
// memory, or else in the cache, if they're of it. Otherwise only the foods
// changed since are read, and their IDs if some were deleted. Only a
// household with none kept anywhere reads them all.
func (r *Repository) snapshot(ctx context.Context, householdID pgtype.UUID, version foodsVersion) (*foodSnapshot, error) {
	r.mu.Lock()
	base := r.snapshots[householdID]
	r.mu.Unlock()
	if base != nil && base.Version == version {
		return base, nil
	}
	key := foodSnapshotKeyPrefix + store.UUIDToString(householdID) + ".json.gz"
	if base == nil && r.cache != nil {
		var stored foodSnapshot
		found, err := blob.GetJSON(ctx, r.cache, key, &stored)
		if err != nil {
			r.log.WarnContext(ctx, "Failed to read the foods kept in the cache, reading them from the database", slog.Any("error", err))
		} else if found {
			base = &stored
		}
	}
	snap := base
	if base == nil || base.Version != version {
		var err error
		snap, err = r.readSnapshot(ctx, householdID, base, version)
		if err != nil {
			return nil, err
		}
		if r.cache != nil {
			if err := blob.PutJSON(ctx, r.cache, key, snap); err != nil {
				r.log.WarnContext(ctx, "Failed to keep the foods in the cache", slog.Any("error", err))
			}
		}
	}
	// A write between reading the version and the foods makes them newer
	// than it says, which only means reading those changed again.
	r.mu.Lock()
	r.snapshots[householdID] = snap
	r.mu.Unlock()
	return snap, nil
}

// readSnapshot reads the household's foods: all of them, or with a
// snapshot, only those changed since.
func (r *Repository) readSnapshot(ctx context.Context, householdID pgtype.UUID, base *foodSnapshot, version foodsVersion) (*foodSnapshot, error) {
	snap := &foodSnapshot{Version: version}
	if base == nil {
		rows, err := r.querier.ListHouseholdFoods(ctx, householdID)
		if err != nil {
			return nil, fmt.Errorf("failed to list foods: %w", err)
		}
		snap.Rows = rows
		return snap, nil
	}
	changed, err := r.querier.ListFoodsChangedSince(ctx, &gen.ListFoodsChangedSinceParams{
		HouseholdID: householdID,
		Since:       pgtype.Timestamptz{Time: time.UnixMicro(base.Version.UpdateMicros), Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list foods: %w", err)
	}
	byID := make(map[pgtype.UUID]*gen.Food, len(base.Rows)+len(changed))
	for _, row := range base.Rows {
		byID[row.ID] = row
	}
	for _, row := range changed {
		byID[row.ID] = row
	}
	// Without deletions, there are as many foods as kept and created;
	// otherwise the IDs show which are gone.
	if int64(len(byID)) != version.Count {
		ids, err := r.querier.ListFoodIDs(ctx, householdID)
		if err != nil {
			return nil, fmt.Errorf("failed to list foods: %w", err)
		}
		kept := make(map[pgtype.UUID]*gen.Food, len(ids))
		for _, id := range ids {
			if row, ok := byID[id]; ok {
				kept[id] = row
			}
		}
		byID = kept
	}
	snap.Rows = make([]*gen.Food, 0, len(byID))
	for _, row := range byID {
		snap.Rows = append(snap.Rows, row)
	}
	slices.SortFunc(snap.Rows, func(a, b *gen.Food) int {
		if c := cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); c != 0 {
			return c
		}
		return bytes.Compare(a.ID.Bytes[:], b.ID.Bytes[:])
	})
	return snap, nil
}
