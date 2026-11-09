// Package pantry implements the PantryService: the lots of foods the
// households have in stock.
package pantry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bs-tools/mealplann.ing/internal/services/amounts"

	"connectrpc.com/connect/v2"
	"github.com/bs-tools/mealplann.ing/internal/authn"
	"github.com/bs-tools/mealplann.ing/internal/errreport"
	"github.com/bs-tools/mealplann.ing/internal/foods"
	foodsv1 "github.com/bs-tools/mealplann.ing/internal/gen/foods/v1"
	pantryv1 "github.com/bs-tools/mealplann.ing/internal/gen/pantry/v1"
	"github.com/bs-tools/mealplann.ing/internal/gen/pantry/v1/pantryv1connect"
	"github.com/bs-tools/mealplann.ing/internal/idempotency"
	"github.com/bs-tools/mealplann.ing/internal/nutrition"
	"github.com/bs-tools/mealplann.ing/internal/pantry"
	"github.com/bs-tools/mealplann.ing/internal/protomask"
	"github.com/bs-tools/mealplann.ing/internal/services/batch"
	"github.com/bs-tools/mealplann.ing/internal/services/food"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var _ pantryv1connect.PantryServiceHandler = (*Service)(nil)

// Repository stores the pantry.
type Repository interface {
	ListItems(ctx context.Context, householdID pgtype.UUID) ([]*pantry.Item, error)
	GetItem(ctx context.Context, householdID, id pgtype.UUID) (*pantry.Item, error)
	CreateItems(ctx context.Context, householdID pgtype.UUID, items []*pantry.Item) ([]*pantry.Item, error)
	UpdateItem(ctx context.Context, it *pantry.Item) (*pantry.Item, error)
	DeleteItem(ctx context.Context, householdID, id pgtype.UUID) error
}

// FoodRepository reads the foods in stock.
type FoodRepository interface {
	GetFood(ctx context.Context, householdID, id pgtype.UUID) (*foods.Food, error)
	ListAlwaysInStockFoods(ctx context.Context, householdID pgtype.UUID) ([]*foods.Food, error)
	UpdateFood(ctx context.Context, f *foods.Food) (*foods.Food, error)
}

// Service implements the PantryService.
type Service struct {
	log      *slog.Logger
	report   *errreport.Reporter
	pantry   Repository
	foods    FoodRepository
	requests *idempotency.Store
	// nowFn returns the current time, which today's date is taken from.
	nowFn func() time.Time
}

// NewService returns a PantryService storing the pantry in the repository,
// of the foods in theirs, and the responses of requests with request IDs
// in requests.
func NewService(log *slog.Logger, report *errreport.Reporter, pantry Repository, foods FoodRepository, requests *idempotency.Store) *Service {
	return &Service{log: log, report: report, pantry: pantry, foods: foods, requests: requests, nowFn: time.Now}
}

// Limits of a lot's fields.
const (
	// MaxGrams is the most of a food in one lot, like a 25 kg sack of rice
	// several times over.
	MaxGrams      = 100_000
	maxNotesChars = 500
	// maxLabelChars is the longest location or label.
	maxLabelChars = 100
	// maxPackages is the most packages in one lot.
	maxPackages = 1000
	// minYear and maxYear bound dates, to catch typos like "0226".
	minYear = 2000
	maxYear = 2100
)

// keepFields are the fields of a lot updates don't change.
var keepFields = []protoreflect.Name{
	"id", "food_id", "food_name", "store_section", "grams_per_piece", "use_by_date", "days_to_expiry", "create_time", "update_time",
}

const (
	itemNotFound = "the pantry item doesn't exist in your household's pantry; find items with ListPantry"
	foodNotFound = "the food doesn't exist in your household; find foods with ListFoods"
)

// Today returns the date of a time in UTC, at midnight.
func Today(now time.Time) time.Time {
	y, m, d := now.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// maxFilterFoods is how many foods the pantry can be listed for.
const maxFilterFoods = 100

func (s *Service) ListPantry(ctx context.Context, req *pantryv1.ListPantryRequest) (*pantryv1.ListPantryResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	if len(req.GetFoodIds()) > maxFilterFoods {
		return nil, connect.Errorf(connect.CodeInvalidArgument, "food_ids must have at most %d foods", maxFilterFoods)
	}
	foodIDs := map[string]bool{}
	for _, id := range req.GetFoodIds() {
		parsed, err := parseID("food_ids", id, "find foods with FoodService.ListFoods")
		if err != nil {
			return nil, err
		}
		foodIDs[store.UUIDToString(parsed)] = true
	}
	if req.HasExpiringWithinDays() && req.GetExpiringWithinDays() < 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, "expiring_within_days must be 0 or more: 0 lists what to use today or is past its use-by date")
	}
	// keep reports whether a food is listed. The pantry is read whole and
	// filtered here: it's a few hundred lots at most, in one round trip.
	query := strings.ToLower(strings.TrimSpace(req.GetQuery()))
	keep := func(foodID, name string, section foodsv1.StoreSection) bool {
		return (len(foodIDs) == 0 || foodIDs[foodID]) &&
			(query == "" || strings.Contains(strings.ToLower(name), query)) &&
			(req.GetStoreSection() == foodsv1.StoreSection_STORE_SECTION_UNSPECIFIED || section == req.GetStoreSection())
	}
	items, err := s.pantry.ListItems(ctx, user.HouseholdID())
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to list pantry")
	}
	stocked, err := s.foods.ListAlwaysInStockFoods(ctx, user.HouseholdID())
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to list foods always in stock")
	}
	today := Today(s.nowFn())
	pantry.SortByUseBy(items)
	result := make([]*pantryv1.PantryItem, 0, len(items))
	for _, it := range items {
		item := ItemToProto(it, today)
		if !keep(item.GetFoodId(), item.GetFoodName(), item.GetStoreSection()) {
			continue
		}
		if req.HasExpiringWithinDays() && (!item.HasDaysToExpiry() || item.GetDaysToExpiry() > req.GetExpiringWithinDays()) {
			continue
		}
		if req.GetStorage() != pantryv1.Storage_STORAGE_UNSPECIFIED && item.GetStorage() != req.GetStorage() ||
			req.GetEstimatedOnly() && !item.GetEstimated() {
			continue
		}
		if req.GetSummary() {
			b := pantryv1.PantryItem_builder{
				Id:        item.GetId(),
				FoodId:    item.GetFoodId(),
				FoodName:  item.GetFoodName(),
				Grams:     item.GetGrams(),
				UseByDate: item.GetUseByDate(),
				Storage:   item.GetStorage(),
				Opened:    item.GetOpened(),
				Estimated: item.GetEstimated(),
			}
			if item.HasDaysToExpiry() {
				b.DaysToExpiry = new(item.GetDaysToExpiry())
			}
			item = b.Build()
		}
		result = append(result, item)
	}
	// Lots are sorted by food name, and each food's by use-by date.
	sortByFoodName(result)
	foodsInStock := make([]*pantryv1.AlwaysInStockFood, 0, len(stocked))
	for _, f := range stocked {
		section := food.StoreSectionToProto(f.StoreSection)
		// Foods always in stock never expire, nor have a storage or an
		// estimated amount, so they aren't listed with lots filtered by
		// those.
		if !keep(store.UUIDToString(f.ID), f.Name, section) || req.HasExpiringWithinDays() ||
			req.GetStorage() != pantryv1.Storage_STORAGE_UNSPECIFIED || req.GetEstimatedOnly() {
			continue
		}
		foodsInStock = append(foodsInStock, pantryv1.AlwaysInStockFood_builder{
			FoodId:       store.UUIDToString(f.ID),
			FoodName:     f.Name,
			StoreSection: food.StoreSectionToProto(f.StoreSection),
		}.Build())
	}
	return pantryv1.ListPantryResponse_builder{Items: result, AlwaysInStockFoods: foodsInStock}.Build(), nil
}

func (s *Service) AddToPantry(ctx context.Context, req *pantryv1.AddToPantryRequest) (*pantryv1.AddToPantryResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	return idempotency.Do(ctx, s.requests, user.HouseholdID(), "AddToPantry", req.GetRequestId(), func() (*pantryv1.AddToPantryResponse, error) {
		return s.addToPantry(ctx, user.HouseholdID(), req)
	})
}

// addToPantry adds a lot, without a request ID.
func (s *Service) addToPantry(ctx context.Context, householdID pgtype.UUID, req *pantryv1.AddToPantryRequest) (*pantryv1.AddToPantryResponse, error) {
	foodID, err := parseID("food_id", req.GetFoodId(), "find foods with ListFoods")
	if err != nil {
		return nil, err
	}
	amount, packages := req.GetAmount(), req.GetPackages()
	var unit nutrition.Unit
	switch {
	case packages != 0 && (amount != 0 || req.GetUnit() != foodsv1.AmountUnit_AMOUNT_UNIT_UNSPECIFIED):
		return nil, connect.NewError(connect.CodeInvalidArgument, "give amount and unit, or packages, not both")
	case packages != 0:
		if math.IsNaN(packages) || packages < 0 || packages > maxPackages {
			return nil, connect.Errorf(connect.CodeInvalidArgument, "packages must be more than 0 and at most %d", maxPackages)
		}
	default:
		var ok bool
		amount, unit, _, ok = amounts.FromProto(amount, req.GetUnit())
		if !ok {
			return nil, connect.Errorf(connect.CodeInvalidArgument, "unit is required: %s", amounts.UnitsHelp)
		}
		if math.IsNaN(amount) || amount <= 0 || amount > MaxGrams {
			return nil, connect.Errorf(connect.CodeInvalidArgument, "amount must be more than 0 and at most %d g, ml or pieces", MaxGrams)
		}
	}
	details, err := lotDetailsFromProto(req.GetStorage(), req.GetLocation(), req.GetLabel(), req.GetFrozenDate(), Today(s.nowFn()))
	if err != nil {
		return nil, err
	}
	expiry, err := parseOptionalDate("expiry_date", req.GetExpiryDate())
	if err != nil {
		return nil, err
	}
	opened, err := parseOptionalDate("opened_date", req.GetOpenedDate())
	if err != nil {
		return nil, err
	}
	notes, err := checkNotes(req.GetNotes())
	if err != nil {
		return nil, err
	}
	f, err := s.foods.GetFood(ctx, householdID, foodID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, foodNotFound)
		}
		return nil, s.toConnectError(ctx, err, "failed to get food")
	}
	if f.AlwaysInStock && !req.GetTrackStock() {
		return nil, connect.Errorf(connect.CodeFailedPrecondition,
			"%s is always in stock, so the pantry doesn't track it; to track it, add it again with track_stock set, which clears always_in_stock on the food", f.Name)
	}
	var grams float64
	var conversion string
	if packages != 0 {
		if f.PackageGrams <= 0 {
			return nil, connect.Errorf(connect.CodeFailedPrecondition,
				"%s has no package size, so packages can't be weighed: set package_grams on the food with UpdateFood, or give the amount and unit", f.Name)
		}
		grams = packages * f.PackageGrams
		if grams > MaxGrams {
			return nil, connect.Errorf(connect.CodeInvalidArgument, "%s packages of %s g is more than %d g", number(packages), number(f.PackageGrams), MaxGrams)
		}
		conversion = fmt.Sprintf("%s packages of %s g is %s g", number(packages), number(f.PackageGrams), number(grams))
	} else {
		grams, err = Grams(f, amount, unit)
		if err != nil {
			return nil, err
		}
		conversion = Conversion(f, amount, unit, grams)
	}
	stopped := false
	if f.AlwaysInStock {
		// Clearing the flag first means a failure to add the lot leaves the
		// food tracked with nothing in stock, which the shopping list then
		// buys, rather than a lot of a food the pantry ignores.
		f.AlwaysInStock = false
		if _, err := s.foods.UpdateFood(ctx, f); err != nil {
			return nil, s.toConnectError(ctx, err, "failed to track the food in the pantry")
		}
		stopped = true
	}
	// A lot going into the freezer keeps for the food's freezer shelf life
	// instead, so it gets no expiry date from its fridge or pantry one.
	if expiry.IsZero() && f.ShelfLifeDays > 0 && details.Storage != pantry.StorageFreezer {
		expiry = Today(s.nowFn()).AddDate(0, 0, int(f.ShelfLifeDays))
	}
	created, err := s.pantry.CreateItems(ctx, householdID, []*pantry.Item{{
		FoodID:     foodID,
		Grams:      grams,
		ExpiryDate: expiry,
		OpenedDate: opened,
		Notes:      notes,
		Storage:    details.Storage,
		Location:   details.Location,
		FrozenDate: details.FrozenDate,
		Opened:     req.GetOpened() || !opened.IsZero(),
		Estimated:  req.GetEstimated(),
		Label:      details.Label,
	}})
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to add to pantry")
	}
	if len(created) == 0 {
		return nil, s.toConnectError(ctx, errors.New("no item created"), "failed to add to pantry")
	}
	return pantryv1.AddToPantryResponse_builder{
		Item:                 ItemToProto(created[0], Today(s.nowFn())),
		Conversion:           conversion,
		StoppedAlwaysInStock: stopped,
	}.Build(), nil
}

// Conversion describes how an amount was converted to grams, e.g. "148 ml
// at 1.17 g/ml is 173 g", so the caller can check it without doing the
// arithmetic. It's empty for an amount in grams.
func Conversion(f *foods.Food, amount float64, unit nutrition.Unit, grams float64) string {
	switch unit {
	case nutrition.Milliliters:
		return fmt.Sprintf("%s ml at %s g/ml is %s g", number(amount), number(f.GramsPerMilliliter), number(grams))
	case nutrition.Pieces:
		piece := "pieces"
		if amount == 1 {
			piece = "piece"
		}
		return fmt.Sprintf("%s %s at %s g each is %s g", number(amount), piece, number(f.GramsPerPiece), number(grams))
	default:
		return ""
	}
}

// number formats a number with at most 2 decimals, without trailing zeros.
func number(v float64) string {
	return strconv.FormatFloat(math.Round(v*100)/100, 'f', -1, 64)
}

func (s *Service) AddLotsToPantry(ctx context.Context, req *pantryv1.AddLotsToPantryRequest) (*pantryv1.AddLotsToPantryResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	if err := batch.Check(len(req.GetRequests())); err != nil {
		return nil, err
	}
	return idempotency.Do(ctx, s.requests, user.HouseholdID(), "AddLotsToPantry", req.GetRequestId(), func() (*pantryv1.AddLotsToPantryResponse, error) {
		return s.addLotsToPantry(ctx, user.HouseholdID(), req)
	})
}

// addLotsToPantry adds lots, ignoring their requests' request IDs.
func (s *Service) addLotsToPantry(ctx context.Context, householdID pgtype.UUID, req *pantryv1.AddLotsToPantryRequest) (*pantryv1.AddLotsToPantryResponse, error) {
	results := batch.Run(ctx, req.GetRequests(), func(ctx context.Context, req *pantryv1.AddToPantryRequest) (*pantryv1.AddToPantryResponse, error) {
		return s.addToPantry(ctx, householdID, req)
	})
	out := make([]*pantryv1.AddLotResult, 0, len(results))
	for _, r := range results {
		out = append(out, pantryv1.AddLotResult_builder{
			Added:        r.Value,
			ErrorCode:    r.ErrorCode,
			ErrorMessage: r.ErrorMessage,
		}.Build())
	}
	return pantryv1.AddLotsToPantryResponse_builder{Results: out}.Build(), nil
}

// Grams converts an amount of a food to grams, failing with a message
// saying what to set on the food if it can't.
func Grams(f *foods.Food, amount float64, unit nutrition.Unit) (float64, error) {
	grams, ok := f.Nutrition().Grams(amount, unit)
	if ok {
		return grams, nil
	}
	if unit == nutrition.Pieces {
		return 0, connect.Errorf(connect.CodeFailedPrecondition,
			"%s has no weight per piece, so pieces can't be weighed: set grams_per_piece on the food with UpdateFood, or give the amount in grams", f.Name)
	}
	return 0, connect.Errorf(connect.CodeFailedPrecondition,
		"%s has no density, so milliliters can't be weighed: set grams_per_milliliter on the food with UpdateFood, or give the amount in grams", f.Name)
}

func (s *Service) UpdatePantryItem(ctx context.Context, req *pantryv1.UpdatePantryItemRequest) (*pantryv1.UpdatePantryItemResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	id, err := parseID("item.id", req.GetItem().GetId(), "find items with ListPantry")
	if err != nil {
		return nil, err
	}
	existing, err := s.pantry.GetItem(ctx, user.HouseholdID(), id)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to get pantry item")
	}
	today := Today(s.nowFn())
	merged := ItemToProto(existing, today)
	if err := protomask.Apply(merged, req.GetItem(), req.GetUpdateMask(), keepFields...); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err.Error())
	}
	grams := merged.GetGrams()
	if math.IsNaN(grams) || grams <= 0 || grams > MaxGrams {
		return nil, connect.Errorf(connect.CodeInvalidArgument,
			"grams must be more than 0 and at most %d; remove the item with RemoveFromPantry when it's used up", MaxGrams)
	}
	expiry, err := parseOptionalDate("expiry_date", merged.GetExpiryDate())
	if err != nil {
		return nil, err
	}
	opened, err := parseOptionalDate("opened_date", merged.GetOpenedDate())
	if err != nil {
		return nil, err
	}
	notes, err := checkNotes(merged.GetNotes())
	if err != nil {
		return nil, err
	}
	// A lot moved into the freezer was frozen today, unless the update
	// says when, as it had no frozen date; one moved out isn't frozen any
	// more.
	frozenDate := merged.GetFrozenDate()
	if merged.GetStorage() != pantryv1.Storage_STORAGE_FREEZER {
		frozenDate = ""
	}
	details, err := lotDetailsFromProto(merged.GetStorage(), merged.GetLocation(), merged.GetLabel(), frozenDate, today)
	if err != nil {
		return nil, err
	}
	updated, err := s.pantry.UpdateItem(ctx, &pantry.Item{
		ID:          existing.ID,
		HouseholdID: existing.HouseholdID,
		Grams:       grams,
		ExpiryDate:  expiry,
		OpenedDate:  opened,
		Notes:       notes,
		Storage:     details.Storage,
		Location:    details.Location,
		FrozenDate:  details.FrozenDate,
		Opened:      merged.GetOpened() || !opened.IsZero(),
		Estimated:   merged.GetEstimated(),
		Label:       details.Label,
	})
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to update pantry item")
	}
	return pantryv1.UpdatePantryItemResponse_builder{Item: ItemToProto(updated, today)}.Build(), nil
}

func (s *Service) RemoveFromPantry(ctx context.Context, req *pantryv1.RemoveFromPantryRequest) (*pantryv1.RemoveFromPantryResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	id, err := parseID("id", req.GetId(), "find items with ListPantry")
	if err != nil {
		return nil, err
	}
	if err := s.pantry.DeleteItem(ctx, user.HouseholdID(), id); err != nil {
		return nil, s.toConnectError(ctx, err, "failed to remove from pantry")
	}
	return pantryv1.RemoveFromPantryResponse_builder{}.Build(), nil
}

// ItemToProto converts a lot, counting its days to expiry from today.
func ItemToProto(it *pantry.Item, today time.Time) *pantryv1.PantryItem {
	b := pantryv1.PantryItem_builder{
		Id:            store.UUIDToString(it.ID),
		FoodId:        store.UUIDToString(it.FoodID),
		FoodName:      it.FoodName,
		StoreSection:  food.StoreSectionToProto(it.StoreSection),
		Grams:         math.Round(it.Grams*1000) / 1000,
		GramsPerPiece: it.GramsPerPiece,
		ExpiryDate:    store.FormatDate(it.ExpiryDate),
		OpenedDate:    store.FormatDate(it.OpenedDate),
		Notes:         it.Notes,
		CreateTime:    timestamppb.New(it.CreateTime),
		UpdateTime:    timestamppb.New(it.UpdateTime),
		Storage:       storagesToProto[it.Storage],
		Location:      it.Location,
		FrozenDate:    store.FormatDate(it.FrozenDate),
		Opened:        it.Opened,
		Estimated:     it.Estimated,
		Label:         it.Label,
	}
	if useBy := it.UseBy(); !useBy.IsZero() {
		b.UseByDate = store.FormatDate(useBy)
		b.DaysToExpiry = new(int32(pantry.DaysTo(today, useBy)))
	}
	return b.Build()
}

// sortByFoodName sorts lots by food name, ignoring case, keeping the order
// of each food's lots.
func sortByFoodName(items []*pantryv1.PantryItem) {
	key := func(it *pantryv1.PantryItem) string {
		return strings.ToLower(it.GetFoodName()) + "\x00" + it.GetFoodId()
	}
	slices.SortStableFunc(items, func(a, b *pantryv1.PantryItem) int { return strings.Compare(key(a), key(b)) })
}

// parseID parses a reference, naming field in errors, with how to find a
// valid one.
func parseID(field, s, find string) (pgtype.UUID, error) {
	id, err := store.StringToUUID(strings.TrimSpace(s))
	if err != nil {
		return pgtype.UUID{}, connect.Errorf(connect.CodeInvalidArgument, "invalid %s: %v; %s", field, err, find).WithCause(err)
	}
	return id, nil
}

// parseOptionalDate parses a date, zero if empty, naming field in errors.
func parseOptionalDate(field, s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	date, err := store.ParseDate(s)
	if err != nil {
		return time.Time{}, connect.Errorf(connect.CodeInvalidArgument, `%s must be a date, "YYYY-MM-DD", e.g. "2026-11-02", or empty`, field)
	}
	if date.Year() < minYear || date.Year() > maxYear {
		return time.Time{}, connect.Errorf(connect.CodeInvalidArgument, "%s must be from %d to %d", field, minYear, maxYear)
	}
	return date, nil
}

// lotDetails are a lot's storage, location, label and frozen date.
type lotDetails struct {
	Storage    pantry.Storage
	Location   string
	Label      string
	FrozenDate time.Time
}

var storagesToProto = map[pantry.Storage]pantryv1.Storage{
	pantry.StorageUnspecified: pantryv1.Storage_STORAGE_UNSPECIFIED,
	pantry.StoragePantry:      pantryv1.Storage_STORAGE_PANTRY,
	pantry.StorageFridge:      pantryv1.Storage_STORAGE_FRIDGE,
	pantry.StorageFreezer:     pantryv1.Storage_STORAGE_FREEZER,
}

// lotDetailsFromProto checks a lot's details. A lot in the freezer was
// frozen today unless frozenDate says when, and only lots in the freezer
// have a frozen date.
func lotDetailsFromProto(storage pantryv1.Storage, location, label, frozenDate string, today time.Time) (lotDetails, error) {
	var d lotDetails
	found := false
	for s, p := range storagesToProto {
		if p == storage {
			d.Storage, found = s, true
		}
	}
	if !found {
		return d, connect.NewError(connect.CodeInvalidArgument, "storage must be STORAGE_PANTRY, STORAGE_FRIDGE, STORAGE_FREEZER or STORAGE_UNSPECIFIED")
	}
	for _, f := range []struct {
		name  string
		value *string
	}{{"location", &location}, {"label", &label}} {
		*f.value = strings.TrimSpace(*f.value)
		if utf8.RuneCountInString(*f.value) > maxLabelChars {
			return d, connect.Errorf(connect.CodeInvalidArgument, "%s must be at most %d characters", f.name, maxLabelChars)
		}
	}
	d.Location, d.Label = location, label
	frozen, err := parseOptionalDate("frozen_date", frozenDate)
	if err != nil {
		return d, err
	}
	switch {
	case !frozen.IsZero() && d.Storage != pantry.StorageFreezer:
		return d, connect.NewError(connect.CodeInvalidArgument, "frozen_date is only for lots in the freezer: set storage to STORAGE_FREEZER, or leave frozen_date empty")
	case d.Storage == pantry.StorageFreezer && frozen.IsZero():
		frozen = today
	}
	d.FrozenDate = frozen
	return d, nil
}

func checkNotes(notes string) (string, error) {
	notes = strings.TrimSpace(notes)
	if utf8.RuneCountInString(notes) > maxNotesChars {
		return "", connect.Errorf(connect.CodeInvalidArgument, "notes must be at most %d characters", maxNotesChars)
	}
	return notes, nil
}

// toConnectError maps an error of the repositories to a Connect error.
func (s *Service) toConnectError(ctx context.Context, err error, msg string) error {
	if connectErr, ok := errors.AsType[*connect.Error](err); ok {
		return connectErr
	}
	switch {
	case errors.Is(err, store.ErrNotFound):
		return connect.NewError(connect.CodeNotFound, itemNotFound)
	case errors.Is(err, pantry.ErrUnknownFood):
		return connect.NewError(connect.CodeNotFound, foodNotFound)
	}
	s.report.Report(ctx, msg, err)
	return connect.NewError(connect.CodeInternal, msg).WithCause(err)
}
