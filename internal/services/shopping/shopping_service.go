// Package shopping implements the ShoppingService: the households' stores
// and shopping lists, generated from their plans less their pantries.
package shopping

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"connectrpc.com/connect/v2"
	"github.com/bs-tools/mealplann.ing/internal/authn"
	"github.com/bs-tools/mealplann.ing/internal/errreport"
	"github.com/bs-tools/mealplann.ing/internal/foods"
	pantryv1 "github.com/bs-tools/mealplann.ing/internal/gen/pantry/v1"
	shoppingv1 "github.com/bs-tools/mealplann.ing/internal/gen/shopping/v1"
	"github.com/bs-tools/mealplann.ing/internal/gen/shopping/v1/shoppingv1connect"
	"github.com/bs-tools/mealplann.ing/internal/pantry"
	"github.com/bs-tools/mealplann.ing/internal/plans"
	"github.com/bs-tools/mealplann.ing/internal/protomask"
	"github.com/bs-tools/mealplann.ing/internal/recipes"
	pantrysvc "github.com/bs-tools/mealplann.ing/internal/services/pantry"
	"github.com/bs-tools/mealplann.ing/internal/shopping"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/reflect/protoreflect"
)

var _ shoppingv1connect.ShoppingServiceHandler = (*Service)(nil)

// Repository stores stores and shopping lists.
type Repository interface {
	CreateStore(ctx context.Context, s *shopping.Store) (*shopping.Store, error)
	GetStore(ctx context.Context, householdID, id pgtype.UUID) (*shopping.Store, error)
	ListStores(ctx context.Context, householdID pgtype.UUID) ([]*shopping.Store, error)
	UpdateStore(ctx context.Context, s *shopping.Store) (*shopping.Store, error)
	DeleteStore(ctx context.Context, householdID, id pgtype.UUID) error
	CreateList(ctx context.Context, l *shopping.List, items []*shopping.Item) (*shopping.List, []*shopping.Store, error)
	GetList(ctx context.Context, householdID, id pgtype.UUID) (*shopping.List, []*shopping.Store, error)
	ListLists(ctx context.Context, householdID pgtype.UUID, limit, offset int32) ([]*shopping.List, error)
	ReplaceGeneratedItems(ctx context.Context, householdID, listID pgtype.UUID, items []*shopping.Item) (*shopping.List, []*shopping.Store, error)
	AddItem(ctx context.Context, it *shopping.Item) (*shopping.Item, error)
	SetItemChecked(ctx context.Context, householdID, id, userID pgtype.UUID, checked bool) (*shopping.Item, error)
	DeleteItem(ctx context.Context, householdID, id pgtype.UUID) error
	CompleteList(ctx context.Context, householdID, id pgtype.UUID) (time.Time, error)
	ReopenList(ctx context.Context, householdID, id pgtype.UUID) error
	DeleteList(ctx context.Context, householdID, id pgtype.UUID) error
}

// PlanRepository reads the plans lists are generated from.
type PlanRepository interface {
	GetPlan(ctx context.Context, householdID pgtype.UUID, from, to time.Time) (*plans.Plan, error)
}

// RecipeRepository reads the recipes plans cook.
type RecipeRepository interface {
	ListRecipesByID(ctx context.Context, householdID pgtype.UUID, ids []pgtype.UUID) ([]*recipes.Recipe, error)
}

// FoodRepository reads the foods bought.
type FoodRepository interface {
	GetFood(ctx context.Context, householdID, id pgtype.UUID) (*foods.Food, error)
	ListFoodsByID(ctx context.Context, householdID pgtype.UUID, ids []pgtype.UUID) ([]*foods.Food, error)
}

// PantryRepository reads what's in stock, and stocks what's bought.
type PantryRepository interface {
	ListItems(ctx context.Context, householdID pgtype.UUID) ([]*pantry.Item, error)
	CreateItems(ctx context.Context, householdID pgtype.UUID, items []*pantry.Item) ([]*pantry.Item, error)
}

// Service implements the ShoppingService.
type Service struct {
	log      *slog.Logger
	report   *errreport.Reporter
	shopping Repository
	plans    PlanRepository
	recipes  RecipeRepository
	foods    FoodRepository
	pantry   PantryRepository
	// nowFn returns the current time, which today's date is taken from.
	nowFn func() time.Time
}

// NewService returns a ShoppingService storing stores and lists in the
// repository, generating lists from the plans, recipes, foods and pantries
// in theirs.
func NewService(log *slog.Logger, report *errreport.Reporter, shopping Repository, plans PlanRepository, recipes RecipeRepository, foods FoodRepository, pantry PantryRepository) *Service {
	return &Service{
		log:      log,
		report:   report,
		shopping: shopping,
		plans:    plans,
		recipes:  recipes,
		foods:    foods,
		pantry:   pantry,
		nowFn:    time.Now,
	}
}

// Limits of the fields.
const (
	maxStoreNameChars = 100
	// maxStorePosition is far more stores than a household shops at.
	maxStorePosition  = 1000
	maxItemTextChars  = 200
	maxAmountTextChar = 100
	maxNoteChars      = 500
	// weekDays is the days after the start date of a list for a week.
	weekDays = 6
	// maxListDays is the most days after the start date of a list: two
	// weeks, as fresh food doesn't keep longer anyway.
	maxListDays = 13
	// defaultPageSize and maxPageSize are the default and most lists
	// ListShoppingLists returns at once.
	defaultPageSize = 20
	maxPageSize     = 100
	// minYear and maxYear bound dates, to catch typos like "0226".
	minYear = 2000
	maxYear = 2100
)

// storeKeepFields are the fields of a store updates don't change.
var storeKeepFields = []protoreflect.Name{"id", "create_time", "update_time"}

// What NotFound errors say is missing, and how to find it.
const (
	storeNotFound = "the store doesn't exist in your household; find stores with ListStores"
	listNotFound  = "the shopping list doesn't exist in your household; find lists with ListShoppingLists"
	itemNotFound  = "the item doesn't exist in your household's shopping lists; find items with GetShoppingList"
	foodNotFound  = "the food doesn't exist in your household; find foods with ListFoods"
)

func (s *Service) CreateStore(ctx context.Context, req *shoppingv1.CreateStoreRequest) (*shoppingv1.CreateStoreResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	st, err := storeFromProto(req.GetStore())
	if err != nil {
		return nil, err
	}
	st.HouseholdID = user.HouseholdID()
	created, err := s.shopping.CreateStore(ctx, st)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to create store", storeNotFound)
	}
	return shoppingv1.CreateStoreResponse_builder{Store: storeToProto(created)}.Build(), nil
}

func (s *Service) ListStores(ctx context.Context, _ *shoppingv1.ListStoresRequest) (*shoppingv1.ListStoresResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	stores, err := s.shopping.ListStores(ctx, user.HouseholdID())
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to list stores", "")
	}
	result := make([]*shoppingv1.Store, 0, len(stores))
	for _, st := range stores {
		result = append(result, storeToProto(st))
	}
	return shoppingv1.ListStoresResponse_builder{Stores: result}.Build(), nil
}

func (s *Service) UpdateStore(ctx context.Context, req *shoppingv1.UpdateStoreRequest) (*shoppingv1.UpdateStoreResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	id, err := parseID("store.id", req.GetStore().GetId(), "find stores with ListStores")
	if err != nil {
		return nil, err
	}
	existing, err := s.shopping.GetStore(ctx, user.HouseholdID(), id)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to get store", storeNotFound)
	}
	merged := storeToProto(existing)
	if err := protomask.Apply(merged, req.GetStore(), req.GetUpdateMask(), storeKeepFields...); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err.Error())
	}
	st, err := storeFromProto(merged)
	if err != nil {
		return nil, err
	}
	if p := merged.GetPosition(); p < 0 || p > maxStorePosition {
		return nil, connect.Errorf(connect.CodeInvalidArgument, "position must be from 0 to %d, with 0 for the main store", maxStorePosition)
	}
	st.ID = existing.ID
	st.HouseholdID = existing.HouseholdID
	st.Position = merged.GetPosition()
	updated, err := s.shopping.UpdateStore(ctx, st)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to update store", storeNotFound)
	}
	return shoppingv1.UpdateStoreResponse_builder{Store: storeToProto(updated)}.Build(), nil
}

func (s *Service) DeleteStore(ctx context.Context, req *shoppingv1.DeleteStoreRequest) (*shoppingv1.DeleteStoreResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	id, err := parseID("id", req.GetId(), "find stores with ListStores")
	if err != nil {
		return nil, err
	}
	if err := s.shopping.DeleteStore(ctx, user.HouseholdID(), id); err != nil {
		return nil, s.toConnectError(ctx, err, "failed to delete store", storeNotFound)
	}
	return shoppingv1.DeleteStoreResponse_builder{}.Build(), nil
}

func (s *Service) CreateShoppingList(ctx context.Context, req *shoppingv1.CreateShoppingListRequest) (*shoppingv1.CreateShoppingListResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	start, err := parseDate("start_date", req.GetStartDate())
	if err != nil {
		return nil, err
	}
	end := start.AddDate(0, 0, weekDays)
	if strings.TrimSpace(req.GetEndDate()) != "" {
		end, err = parseDate("end_date", req.GetEndDate())
		if err != nil {
			return nil, err
		}
		if end.Before(start) || end.After(start.AddDate(0, 0, maxListDays)) {
			return nil, connect.Errorf(connect.CodeInvalidArgument,
				"end_date must be from start_date to %d days after it, or empty for a week", maxListDays)
		}
	}
	items, err := s.generate(ctx, user.HouseholdID(), start, end)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to generate shopping list", "")
	}
	list, _, err := s.shopping.CreateList(ctx, &shopping.List{HouseholdID: user.HouseholdID(), StartDate: start, EndDate: end}, items)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to create shopping list", "")
	}
	return shoppingv1.CreateShoppingListResponse_builder{List: listToProto(list)}.Build(), nil
}

// generate generates the items of a household's list for the meals from
// one date to another, shopped for on the first. It reads the plan and the
// pantry together, then the recipes, then the foods: three round trips.
func (s *Service) generate(ctx context.Context, householdID pgtype.UUID, from, to time.Time) ([]*shopping.Item, error) {
	var plan *plans.Plan
	var lots []*pantry.Item
	var planErr, pantryErr error
	var wg sync.WaitGroup
	wg.Go(func() { plan, planErr = s.plans.GetPlan(ctx, householdID, from, to) })
	wg.Go(func() { lots, pantryErr = s.pantry.ListItems(ctx, householdID) })
	wg.Wait()
	if planErr != nil {
		return nil, planErr
	}
	if pantryErr != nil {
		return nil, pantryErr
	}
	var recipeIDs []pgtype.UUID
	for _, c := range plan.Cooks {
		if !c.Done {
			recipeIDs = append(recipeIDs, c.RecipeID)
		}
	}
	for _, st := range plan.Staples {
		if st.RecipeID.Valid {
			recipeIDs = append(recipeIDs, st.RecipeID)
		}
	}
	recipeList, err := s.recipes.ListRecipesByID(ctx, householdID, unique(recipeIDs))
	if err != nil {
		return nil, err
	}
	byID := map[pgtype.UUID]*recipes.Recipe{}
	for _, r := range recipeList {
		byID[r.ID] = r
	}
	needs := shopping.NeedsOf(plan, byID)
	var foodIDs []pgtype.UUID
	for _, n := range needs {
		foodIDs = append(foodIDs, n.FoodID)
		// The alternatives are read too, to use those the pantry has.
		foodIDs = append(foodIDs, n.Alternatives...)
	}
	foodList, err := s.foods.ListFoodsByID(ctx, householdID, unique(foodIDs))
	if err != nil {
		return nil, err
	}
	foodsByID := map[pgtype.UUID]*foods.Food{}
	for _, f := range foodList {
		foodsByID[f.ID] = f
	}
	return shopping.Generate(&shopping.Input{ShopDate: from, Needs: needs, Foods: foodsByID, Lots: lots}), nil
}

func (s *Service) GetShoppingList(ctx context.Context, req *shoppingv1.GetShoppingListRequest) (*shoppingv1.GetShoppingListResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	id, err := parseID("id", req.GetId(), "find lists with ListShoppingLists")
	if err != nil {
		return nil, err
	}
	list, _, err := s.shopping.GetList(ctx, user.HouseholdID(), id)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to get shopping list", listNotFound)
	}
	return shoppingv1.GetShoppingListResponse_builder{List: listToProto(list)}.Build(), nil
}

func (s *Service) ListShoppingLists(ctx context.Context, req *shoppingv1.ListShoppingListsRequest) (*shoppingv1.ListShoppingListsResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	size := req.GetPageSize()
	switch {
	case size == 0:
		size = defaultPageSize
	case size < 0 || size > maxPageSize:
		return nil, connect.Errorf(connect.CodeInvalidArgument, "page_size must be from 1 to %d", maxPageSize)
	}
	// Page tokens are the number of lists before the page. A household
	// makes about one list a week, so skipping them costs little.
	var offset int64
	if token := req.GetPageToken(); token != "" {
		var err error
		offset, err = strconv.ParseInt(token, 10, 32)
		if err != nil || offset < 0 {
			return nil, connect.NewError(connect.CodeInvalidArgument, "invalid page_token; pass the next_page_token of the previous page")
		}
	}
	// One more list than the page tells whether there's another page.
	lists, err := s.shopping.ListLists(ctx, user.HouseholdID(), size+1, int32(offset))
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to list shopping lists", "")
	}
	var next string
	if len(lists) > int(size) {
		lists = lists[:size]
		next = strconv.FormatInt(offset+int64(size), 10)
	}
	result := make([]*shoppingv1.ShoppingList, 0, len(lists))
	for _, l := range lists {
		result = append(result, listToProto(l))
	}
	return shoppingv1.ListShoppingListsResponse_builder{Lists: result, NextPageToken: next}.Build(), nil
}

func (s *Service) RegenerateShoppingList(ctx context.Context, req *shoppingv1.RegenerateShoppingListRequest) (*shoppingv1.RegenerateShoppingListResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	id, err := parseID("id", req.GetId(), "find lists with ListShoppingLists")
	if err != nil {
		return nil, err
	}
	existing, _, err := s.shopping.GetList(ctx, user.HouseholdID(), id)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to get shopping list", listNotFound)
	}
	if !existing.CompleteTime.IsZero() {
		return nil, s.toConnectError(ctx, shopping.ErrListComplete, "", "")
	}
	items, err := s.generate(ctx, user.HouseholdID(), existing.StartDate, existing.EndDate)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to generate shopping list", "")
	}
	shopping.KeepChecks(existing.Items, items)
	list, _, err := s.shopping.ReplaceGeneratedItems(ctx, user.HouseholdID(), id, items)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to regenerate shopping list", listNotFound)
	}
	return shoppingv1.RegenerateShoppingListResponse_builder{List: listToProto(list)}.Build(), nil
}

func (s *Service) CheckItem(ctx context.Context, req *shoppingv1.CheckItemRequest) (*shoppingv1.CheckItemResponse, error) {
	it, err := s.setChecked(ctx, req.GetId(), true)
	if err != nil {
		return nil, err
	}
	return shoppingv1.CheckItemResponse_builder{Item: it}.Build(), nil
}

func (s *Service) UncheckItem(ctx context.Context, req *shoppingv1.UncheckItemRequest) (*shoppingv1.UncheckItemResponse, error) {
	it, err := s.setChecked(ctx, req.GetId(), false)
	if err != nil {
		return nil, err
	}
	return shoppingv1.UncheckItemResponse_builder{Item: it}.Build(), nil
}

// setChecked checks or unchecks an item, in a single round trip, as it's
// done in the store, often on a weak connection.
func (s *Service) setChecked(ctx context.Context, rawID string, checked bool) (*shoppingv1.ShoppingItem, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	id, err := parseID("id", rawID, "find items with GetShoppingList")
	if err != nil {
		return nil, err
	}
	it, err := s.shopping.SetItemChecked(ctx, user.HouseholdID(), id, user.ID(), checked)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to check item", itemNotFound)
	}
	return itemToProto(it), nil
}

func (s *Service) AddManualItem(ctx context.Context, req *shoppingv1.AddManualItemRequest) (*shoppingv1.AddManualItemResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	listID, err := parseID("list_id", req.GetListId(), "find lists with ListShoppingLists")
	if err != nil {
		return nil, err
	}
	it := &shopping.Item{HouseholdID: user.HouseholdID(), ListID: listID, Manual: true}
	switch req.WhichWhat() {
	case shoppingv1.AddManualItemRequest_FoodId_case:
		foodID, err := parseID("food_id", req.GetFoodId(), "find foods with ListFoods")
		if err != nil {
			return nil, err
		}
		f, err := s.foods.GetFood(ctx, user.HouseholdID(), foodID)
		if err != nil {
			return nil, s.toConnectError(ctx, err, "failed to get food", foodNotFound)
		}
		it.FoodID, it.Name, it.StoreID, it.StoreSection = f.ID, f.Name, f.PreferredStoreID, f.StoreSection
	case shoppingv1.AddManualItemRequest_Text_case:
		text := strings.TrimSpace(req.GetText())
		if text == "" || utf8.RuneCountInString(text) > maxItemTextChars {
			return nil, connect.Errorf(connect.CodeInvalidArgument, `text must be from 1 to %d characters, e.g. "Dish soap"`, maxItemTextChars)
		}
		it.Name, it.StoreSection = text, foods.StoreSectionOther
	default:
		return nil, connect.NewError(connect.CodeInvalidArgument, `set food_id to a food of the household, or text, e.g. "Dish soap"`)
	}
	grams := req.GetGrams()
	if math.IsNaN(grams) || grams < 0 || grams > pantrysvc.MaxGrams {
		return nil, connect.Errorf(connect.CodeInvalidArgument, "grams must be from 0 to %d, with 0 if not given", pantrysvc.MaxGrams)
	}
	it.Grams, it.NeededGrams = grams, grams
	it.AmountText = strings.TrimSpace(req.GetAmountText())
	switch {
	case utf8.RuneCountInString(it.AmountText) > maxAmountTextChar:
		return nil, connect.Errorf(connect.CodeInvalidArgument, "amount_text must be at most %d characters", maxAmountTextChar)
	case it.AmountText == "" && grams > 0:
		it.AmountText = shopping.FormatGrams(grams)
	}
	if raw := strings.TrimSpace(req.GetStoreId()); raw != "" {
		it.StoreID, err = parseID("store_id", raw, "find stores with ListStores, or leave it empty")
		if err != nil {
			return nil, err
		}
	}
	it.Note, err = checkText("note", req.GetNote(), maxNoteChars)
	if err != nil {
		return nil, err
	}
	added, err := s.shopping.AddItem(ctx, it)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to add item", listNotFound)
	}
	return shoppingv1.AddManualItemResponse_builder{Item: itemToProto(added)}.Build(), nil
}

func (s *Service) DeleteItem(ctx context.Context, req *shoppingv1.DeleteItemRequest) (*shoppingv1.DeleteItemResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	id, err := parseID("id", req.GetId(), "find items with GetShoppingList")
	if err != nil {
		return nil, err
	}
	if err := s.shopping.DeleteItem(ctx, user.HouseholdID(), id); err != nil {
		return nil, s.toConnectError(ctx, err, "failed to delete item", itemNotFound)
	}
	return shoppingv1.DeleteItemResponse_builder{}.Build(), nil
}

func (s *Service) CompleteShopping(ctx context.Context, req *shoppingv1.CompleteShoppingRequest) (*shoppingv1.CompleteShoppingResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	id, err := parseID("id", req.GetId(), "find lists with ListShoppingLists")
	if err != nil {
		return nil, err
	}
	today := pantrysvc.Today(s.nowFn())
	date := today
	if strings.TrimSpace(req.GetDate()) != "" {
		date, err = parseDate("date", req.GetDate())
		if err != nil {
			return nil, err
		}
	}
	list, _, err := s.shopping.GetList(ctx, user.HouseholdID(), id)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to get shopping list", listNotFound)
	}
	if !list.CompleteTime.IsZero() {
		return nil, s.toConnectError(ctx, shopping.ErrListComplete, "", "")
	}

	// The checked items of foods with an amount go into the pantry, other
	// than foods always in stock.
	var bought []*shopping.Item
	var foodIDs []pgtype.UUID
	for _, it := range list.Items {
		if it.Checked && it.FoodID.Valid && it.Grams > 0 {
			bought = append(bought, it)
			foodIDs = append(foodIDs, it.FoodID)
		}
	}
	foodList, err := s.foods.ListFoodsByID(ctx, user.HouseholdID(), unique(foodIDs))
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to read foods", "")
	}
	foodsByID := map[pgtype.UUID]*foods.Food{}
	for _, f := range foodList {
		foodsByID[f.ID] = f
	}
	var lots []*pantry.Item
	for _, it := range bought {
		f, ok := foodsByID[it.FoodID]
		if !ok || f.AlwaysInStock {
			continue
		}
		lot := &pantry.Item{FoodID: f.ID, Grams: it.Grams}
		if f.ShelfLifeDays > 0 {
			lot.ExpiryDate = date.AddDate(0, 0, int(f.ShelfLifeDays))
		}
		lots = append(lots, lot)
	}

	// Completing the list first keeps a second request, like a double tap,
	// from adding the items again. If adding them fails, the list is
	// reopened, so completing it can be tried again.
	completed, err := s.shopping.CompleteList(ctx, user.HouseholdID(), id)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to complete shopping list", listNotFound)
	}
	added, err := s.pantry.CreateItems(ctx, user.HouseholdID(), lots)
	if err != nil {
		if reopenErr := s.shopping.ReopenList(context.WithoutCancel(ctx), user.HouseholdID(), id); reopenErr != nil {
			s.report.Report(ctx, "Failed to reopen a shopping list after failing to add its items to the pantry, leaving it complete", reopenErr)
		}
		return nil, s.toConnectError(ctx, err, "failed to add the items to the pantry", "")
	}
	list.CompleteTime = completed
	pantryItems := make([]*pantryv1.PantryItem, 0, len(added))
	for _, lot := range added {
		pantryItems = append(pantryItems, pantrysvc.ItemToProto(lot, today))
	}
	return shoppingv1.CompleteShoppingResponse_builder{List: listToProto(list), PantryItems: pantryItems}.Build(), nil
}

func (s *Service) DeleteShoppingList(ctx context.Context, req *shoppingv1.DeleteShoppingListRequest) (*shoppingv1.DeleteShoppingListResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	id, err := parseID("id", req.GetId(), "find lists with ListShoppingLists")
	if err != nil {
		return nil, err
	}
	if err := s.shopping.DeleteList(ctx, user.HouseholdID(), id); err != nil {
		return nil, s.toConnectError(ctx, err, "failed to delete shopping list", listNotFound)
	}
	return shoppingv1.DeleteShoppingListResponse_builder{}.Build(), nil
}

// toConnectError maps an error of the repositories to a Connect error, with
// notFound as the message of store.ErrNotFound.
func (s *Service) toConnectError(ctx context.Context, err error, msg, notFound string) error {
	if connectErr, ok := errors.AsType[*connect.Error](err); ok {
		return connectErr
	}
	switch {
	case errors.Is(err, store.ErrNotFound) && notFound != "":
		return connect.NewError(connect.CodeNotFound, notFound)
	case errors.Is(err, shopping.ErrStoreNameTaken):
		return connect.NewError(connect.CodeAlreadyExists, "the household already has a store with this name; use that store, or choose another name")
	case errors.Is(err, shopping.ErrListComplete):
		return connect.NewError(connect.CodeFailedPrecondition,
			"the shopping of this list is complete; create a new list with CreateShoppingList, or add items by hand with AddManualItem")
	case errors.Is(err, shopping.ErrUnknownFood):
		return connect.NewError(connect.CodeInvalidArgument, "food_id isn't one of your household's foods; find them with ListFoods")
	case errors.Is(err, shopping.ErrUnknownStore):
		return connect.NewError(connect.CodeInvalidArgument, "store_id isn't one of your household's stores; find them with ListStores, or leave it empty")
	case errors.Is(err, pantry.ErrUnknownFood):
		return connect.NewError(connect.CodeFailedPrecondition, "a food of the list was deleted; regenerate the list with RegenerateShoppingList")
	}
	s.report.Report(ctx, msg, err)
	return connect.NewError(connect.CodeInternal, msg).WithCause(err)
}

// unique returns the valid IDs, without repeats, in their order.
func unique(ids []pgtype.UUID) []pgtype.UUID {
	var result []pgtype.UUID
	for _, id := range ids {
		if id.Valid && !slices.Contains(result, id) {
			result = append(result, id)
		}
	}
	return result
}
