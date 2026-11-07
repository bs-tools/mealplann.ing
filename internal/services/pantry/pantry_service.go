// Package pantry implements the PantryService: the lots of foods the
// households have in stock.
package pantry

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"connectrpc.com/connect/v2"
	"github.com/bs-tools/foodplann.ing/internal/authn"
	"github.com/bs-tools/foodplann.ing/internal/errreport"
	"github.com/bs-tools/foodplann.ing/internal/foods"
	foodsv1 "github.com/bs-tools/foodplann.ing/internal/gen/foods/v1"
	pantryv1 "github.com/bs-tools/foodplann.ing/internal/gen/pantry/v1"
	"github.com/bs-tools/foodplann.ing/internal/gen/pantry/v1/pantryv1connect"
	"github.com/bs-tools/foodplann.ing/internal/nutrition"
	"github.com/bs-tools/foodplann.ing/internal/pantry"
	"github.com/bs-tools/foodplann.ing/internal/protomask"
	"github.com/bs-tools/foodplann.ing/internal/services/food"
	"github.com/bs-tools/foodplann.ing/internal/store"
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
}

// Service implements the PantryService.
type Service struct {
	log    *slog.Logger
	report *errreport.Reporter
	pantry Repository
	foods  FoodRepository
	// nowFn returns the current time, which today's date is taken from.
	nowFn func() time.Time
}

// NewService returns a PantryService storing the pantry in the repository,
// of the foods in theirs.
func NewService(log *slog.Logger, report *errreport.Reporter, pantry Repository, foods FoodRepository) *Service {
	return &Service{log: log, report: report, pantry: pantry, foods: foods, nowFn: time.Now}
}

// Limits of a lot's fields.
const (
	// MaxGrams is the most of a food in one lot, like a 25 kg sack of rice
	// several times over.
	MaxGrams      = 100_000
	maxNotesChars = 500
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

func (s *Service) ListPantry(ctx context.Context, _ *pantryv1.ListPantryRequest) (*pantryv1.ListPantryResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
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
		result = append(result, ItemToProto(it, today))
	}
	// Lots are sorted by food name, and each food's by use-by date.
	sortByFoodName(result)
	foodsInStock := make([]*pantryv1.AlwaysInStockFood, 0, len(stocked))
	for _, f := range stocked {
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
	foodID, err := parseID("food_id", req.GetFoodId(), "find foods with ListFoods")
	if err != nil {
		return nil, err
	}
	amount := req.GetAmount()
	if math.IsNaN(amount) || amount <= 0 || amount > MaxGrams {
		return nil, connect.Errorf(connect.CodeInvalidArgument, "amount must be more than 0 and at most %d", MaxGrams)
	}
	unit, ok := unitsFromProto[req.GetUnit()]
	if !ok {
		return nil, connect.NewError(connect.CodeInvalidArgument, "unit is required: AMOUNT_UNIT_GRAMS, AMOUNT_UNIT_MILLILITERS or AMOUNT_UNIT_PIECES")
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
	f, err := s.foods.GetFood(ctx, user.HouseholdID(), foodID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, foodNotFound)
		}
		return nil, s.toConnectError(ctx, err, "failed to get food")
	}
	if f.AlwaysInStock {
		return nil, connect.Errorf(connect.CodeFailedPrecondition,
			"%s is always in stock, so the pantry doesn't track it; set always_in_stock to false on the food with UpdateFood to track it", f.Name)
	}
	grams, err := Grams(f, amount, unit)
	if err != nil {
		return nil, err
	}
	if expiry.IsZero() && f.ShelfLifeDays > 0 {
		expiry = Today(s.nowFn()).AddDate(0, 0, int(f.ShelfLifeDays))
	}
	created, err := s.pantry.CreateItems(ctx, user.HouseholdID(), []*pantry.Item{{
		FoodID:     foodID,
		Grams:      grams,
		ExpiryDate: expiry,
		OpenedDate: opened,
		Notes:      notes,
	}})
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to add to pantry")
	}
	if len(created) == 0 {
		return nil, s.toConnectError(ctx, errors.New("no item created"), "failed to add to pantry")
	}
	return pantryv1.AddToPantryResponse_builder{Item: ItemToProto(created[0], Today(s.nowFn()))}.Build(), nil
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
	updated, err := s.pantry.UpdateItem(ctx, &pantry.Item{
		ID:          existing.ID,
		HouseholdID: existing.HouseholdID,
		Grams:       grams,
		ExpiryDate:  expiry,
		OpenedDate:  opened,
		Notes:       notes,
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

var unitsFromProto = map[foodsv1.AmountUnit]nutrition.Unit{
	foodsv1.AmountUnit_AMOUNT_UNIT_GRAMS:       nutrition.Grams,
	foodsv1.AmountUnit_AMOUNT_UNIT_MILLILITERS: nutrition.Milliliters,
	foodsv1.AmountUnit_AMOUNT_UNIT_PIECES:      nutrition.Pieces,
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
