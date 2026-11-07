// Package food implements the FoodService: the households' foods, and the
// foods of USDA FoodData Central they're created from.
package food

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"unicode/utf8"

	"connectrpc.com/connect/v2"
	"github.com/bs-tools/foodplann.ing/internal/authn"
	"github.com/bs-tools/foodplann.ing/internal/errreport"
	"github.com/bs-tools/foodplann.ing/internal/foods"
	foodsv1 "github.com/bs-tools/foodplann.ing/internal/gen/foods/v1"
	"github.com/bs-tools/foodplann.ing/internal/gen/foods/v1/foodsv1connect"
	"github.com/bs-tools/foodplann.ing/internal/protomask"
	"github.com/bs-tools/foodplann.ing/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/reflect/protoreflect"
)

var _ foodsv1connect.FoodServiceHandler = (*Service)(nil)

// Repository stores foods and the FoodData Central foods.
type Repository interface {
	CreateFood(ctx context.Context, f *foods.Food) (*foods.Food, error)
	GetFood(ctx context.Context, householdID, id pgtype.UUID) (*foods.Food, error)
	ListFoods(ctx context.Context, householdID pgtype.UUID, query string, limit, offset int32) ([]*foods.Food, error)
	UpdateFood(ctx context.Context, f *foods.Food) (*foods.Food, error)
	DeleteFood(ctx context.Context, householdID, id pgtype.UUID) error
	GetFDCFood(ctx context.Context, fdcID int32) (*foods.FDCFood, error)
	SearchFDCFoods(ctx context.Context, householdID pgtype.UUID, query string, limit int32) ([]*foods.FDCMatch, error)
}

// Service implements the FoodService.
type Service struct {
	log    *slog.Logger
	report *errreport.Reporter
	foods  Repository
}

// NewService returns a FoodService storing foods in the repository.
func NewService(log *slog.Logger, report *errreport.Reporter, foods Repository) *Service {
	return &Service{log: log, report: report, foods: foods}
}

const (
	// defaultSearchSize and maxSearchSize are the default and most FDC
	// foods SearchFoodData returns. Each comes with its nutrients and
	// portions, which cost agents tokens.
	defaultSearchSize = 10
	maxSearchSize     = 50
	// maxQueryLength is the most characters of a search.
	maxQueryLength = 200
	// defaultPageSize and maxPageSize are the default and most foods
	// ListFoods returns at once.
	defaultPageSize = 50
	maxPageSize     = 200
)

// keepFields are the fields of a food updates don't change.
var keepFields = []protoreflect.Name{"id", "source", "fdc_id", "create_time", "update_time"}

func (s *Service) SearchFoodData(ctx context.Context, req *foodsv1.SearchFoodDataRequest) (*foodsv1.SearchFoodDataResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	query := strings.TrimSpace(req.GetQuery())
	switch {
	case query == "":
		return nil, connect.NewError(connect.CodeInvalidArgument, `query is required: give the food to search for, e.g. "lentils raw"`)
	case utf8.RuneCountInString(query) > maxQueryLength:
		return nil, connect.Errorf(connect.CodeInvalidArgument, "query must be at most %d characters", maxQueryLength)
	}
	size, err := pageSize(req.GetPageSize(), defaultSearchSize, maxSearchSize)
	if err != nil {
		return nil, err
	}
	matches, err := s.foods.SearchFDCFoods(ctx, user.HouseholdID(), query, size)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to search FoodData Central")
	}
	result := make([]*foodsv1.FoodData, 0, len(matches))
	for _, m := range matches {
		result = append(result, foodDataToProto(m))
	}
	return foodsv1.SearchFoodDataResponse_builder{Foods: result}.Build(), nil
}

func (s *Service) CreateFood(ctx context.Context, req *foodsv1.CreateFoodRequest) (*foodsv1.CreateFoodResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	in := req.GetFood()
	if in == nil {
		in = &foodsv1.Food{}
	}
	var f *foods.Food
	if fdcID := req.GetFdcId(); fdcID != 0 {
		fdcFood, err := s.foods.GetFDCFood(ctx, fdcID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil, connect.Errorf(connect.CodeInvalidArgument,
					"there's no FoodData Central food %d; find one with SearchFoodData", fdcID)
			}
			return nil, s.toConnectError(ctx, err, "failed to get FoodData Central food")
		}
		f, err = foodFromFDC(fdcFood, in)
		if err != nil {
			return nil, err
		}
	} else {
		var err error
		f, err = foodFromProto(in)
		if err != nil {
			return nil, err
		}
	}
	f.HouseholdID = user.HouseholdID()
	created, err := s.foods.CreateFood(ctx, f)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to create food")
	}
	return foodsv1.CreateFoodResponse_builder{Food: foodToProto(created)}.Build(), nil
}

func (s *Service) GetFood(ctx context.Context, req *foodsv1.GetFoodRequest) (*foodsv1.GetFoodResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	id, err := parseFoodID(req.GetId())
	if err != nil {
		return nil, err
	}
	f, err := s.foods.GetFood(ctx, user.HouseholdID(), id)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to get food")
	}
	return foodsv1.GetFoodResponse_builder{Food: foodToProto(f)}.Build(), nil
}

func (s *Service) ListFoods(ctx context.Context, req *foodsv1.ListFoodsRequest) (*foodsv1.ListFoodsResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	query := strings.TrimSpace(req.GetQuery())
	if utf8.RuneCountInString(query) > maxQueryLength {
		return nil, connect.Errorf(connect.CodeInvalidArgument, "query must be at most %d characters", maxQueryLength)
	}
	size, err := pageSize(req.GetPageSize(), defaultPageSize, maxPageSize)
	if err != nil {
		return nil, err
	}
	// Page tokens are the number of foods before the page. Households have
	// at most hundreds of foods, so skipping them costs little.
	var offset int64
	if token := req.GetPageToken(); token != "" {
		offset, err = strconv.ParseInt(token, 10, 32)
		if err != nil || offset < 0 {
			return nil, connect.NewError(connect.CodeInvalidArgument, "invalid page_token; pass the next_page_token of the previous page")
		}
	}
	// One more food than the page tells whether there's another page.
	list, err := s.foods.ListFoods(ctx, user.HouseholdID(), query, size+1, int32(offset))
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to list foods")
	}
	var next string
	if len(list) > int(size) {
		list = list[:size]
		next = strconv.FormatInt(offset+int64(size), 10)
	}
	result := make([]*foodsv1.Food, 0, len(list))
	for _, f := range list {
		result = append(result, foodToProto(f))
	}
	return foodsv1.ListFoodsResponse_builder{Foods: result, NextPageToken: next}.Build(), nil
}

func (s *Service) UpdateFood(ctx context.Context, req *foodsv1.UpdateFoodRequest) (*foodsv1.UpdateFoodResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	id, err := parseFoodID(req.GetFood().GetId())
	if err != nil {
		return nil, err
	}
	existing, err := s.foods.GetFood(ctx, user.HouseholdID(), id)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to get food")
	}
	merged := foodToProto(existing)
	in := req.GetFood()
	if err := protomask.Apply(merged, in, req.GetUpdateMask(), keepFields...); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err.Error())
	}
	f, err := foodFromProto(merged)
	if err != nil {
		return nil, err
	}
	f.ID = existing.ID
	f.HouseholdID = existing.HouseholdID
	updated, err := s.foods.UpdateFood(ctx, f)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to update food")
	}
	return foodsv1.UpdateFoodResponse_builder{Food: foodToProto(updated)}.Build(), nil
}

func (s *Service) DeleteFood(ctx context.Context, req *foodsv1.DeleteFoodRequest) (*foodsv1.DeleteFoodResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	id, err := parseFoodID(req.GetId())
	if err != nil {
		return nil, err
	}
	if err := s.foods.DeleteFood(ctx, user.HouseholdID(), id); err != nil {
		return nil, s.toConnectError(ctx, err, "failed to delete food")
	}
	return foodsv1.DeleteFoodResponse_builder{}.Build(), nil
}

// pageSize returns the page size requested, or the default for 0.
func pageSize(requested, defaultSize, maxSize int32) (int32, error) {
	switch {
	case requested == 0:
		return defaultSize, nil
	case requested < 0 || requested > maxSize:
		return 0, connect.Errorf(connect.CodeInvalidArgument, "page_size must be from 1 to %d", maxSize)
	}
	return requested, nil
}

func parseFoodID(s string) (pgtype.UUID, error) {
	id, err := store.StringToUUID(s)
	if err != nil {
		return pgtype.UUID{}, connect.Errorf(connect.CodeInvalidArgument, "invalid food ID: %v; find foods with ListFoods", err).WithCause(err)
	}
	return id, nil
}

// toConnectError maps an error of the repository to a Connect error.
func (s *Service) toConnectError(ctx context.Context, err error, msg string) error {
	if connectErr, ok := errors.AsType[*connect.Error](err); ok {
		return connectErr
	}
	if inUse, ok := errors.AsType[*foods.InUseError](err); ok {
		if len(inUse.Recipes) == 0 {
			return connect.NewError(connect.CodeFailedPrecondition, "the food is used by recipes; remove it from them first")
		}
		return connect.Errorf(connect.CodeFailedPrecondition,
			"the food is used by the recipes %s; remove it from them first, or keep it", quoteList(inUse.Recipes))
	}
	switch {
	case errors.Is(err, store.ErrNotFound):
		return connect.NewError(connect.CodeNotFound, "the food doesn't exist in your household; find foods with ListFoods")
	case errors.Is(err, foods.ErrNameTaken):
		return connect.NewError(connect.CodeAlreadyExists, "the household already has a food with this name; use that food, or choose another name")
	}
	s.report.Report(ctx, msg, err)
	return connect.NewError(connect.CodeInternal, msg).WithCause(err)
}

// quoteList lists names in quotes, e.g. `"Hummus" and "Dal"`.
func quoteList(names []string) string {
	quoted := make([]string, 0, len(names))
	for _, n := range names {
		quoted = append(quoted, strconv.Quote(n))
	}
	if len(quoted) == 1 {
		return quoted[0]
	}
	return strings.Join(quoted[:len(quoted)-1], ", ") + " and " + quoted[len(quoted)-1]
}
