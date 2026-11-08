// Package recipe implements the RecipeService: the households' recipes,
// with their nutrition.
package recipe

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"unicode/utf8"

	"connectrpc.com/connect/v2"
	"github.com/bs-tools/mealplann.ing/internal/authn"
	"github.com/bs-tools/mealplann.ing/internal/errreport"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
	"github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1/recipesv1connect"
	"github.com/bs-tools/mealplann.ing/internal/protomask"
	"github.com/bs-tools/mealplann.ing/internal/recipes"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/reflect/protoreflect"
)

var _ recipesv1connect.RecipeServiceHandler = (*Service)(nil)

// Repository stores recipes.
type Repository interface {
	CreateRecipe(ctx context.Context, recipe *recipes.Recipe) (*recipes.Recipe, error)
	GetRecipe(ctx context.Context, householdID, id pgtype.UUID) (*recipes.Recipe, error)
	ListRecipes(ctx context.Context, householdID pgtype.UUID, filter recipes.ListFilter) ([]*recipes.Recipe, []string, error)
	UpdateRecipe(ctx context.Context, recipe *recipes.Recipe) (*recipes.Recipe, error)
	DeleteRecipe(ctx context.Context, householdID, id pgtype.UUID) error
	ListRecipesByID(ctx context.Context, householdID pgtype.UUID, ids []pgtype.UUID) ([]*recipes.Recipe, error)
}

// Service implements the RecipeService.
type Service struct {
	log     *slog.Logger
	report  *errreport.Reporter
	recipes Repository
}

// NewService returns a RecipeService storing recipes in the repository.
func NewService(log *slog.Logger, report *errreport.Reporter, recipes Repository) *Service {
	return &Service{log: log, report: report, recipes: recipes}
}

// checkComponents checks that a recipe's components don't use it, as
// that would make it of itself, and don't nest deeper than
// recipes.MaxComponentDepth. Components that aren't the household's are
// left for the repository to refuse.
func (s *Service) checkComponents(ctx context.Context, recipe *recipes.Recipe) error {
	var ids []pgtype.UUID
	for _, in := range recipe.Ingredients {
		if in.IsComponent() {
			ids = append(ids, in.ComponentRecipeID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	components, err := s.recipes.ListRecipesByID(ctx, recipe.HouseholdID, ids)
	if err != nil {
		return s.toConnectError(ctx, err, "failed to read the recipe's components")
	}
	for _, c := range components {
		switch {
		case recipe.ID.Valid && c.ID == recipe.ID:
			return connect.NewError(connect.CodeInvalidArgument, "a recipe can't be an ingredient of itself")
		case recipe.ID.Valid && c.UsesRecipe(recipe.ID):
			return connect.Errorf(connect.CodeInvalidArgument,
				"%s uses this recipe as an ingredient, so it can't be one of this recipe's", c.Name)
		case 1+c.Depth() > recipes.MaxComponentDepth:
			return connect.Errorf(connect.CodeInvalidArgument,
				"%s has components with components of their own, which nests too deep: components can have components, but no deeper", c.Name)
		}
	}
	return nil
}

// keepFields are the fields of a recipe updates don't change.
var keepFields = []protoreflect.Name{"id", "nutrition", "create_time", "update_time"}

func (s *Service) CreateRecipe(ctx context.Context, req *recipesv1.CreateRecipeRequest) (*recipesv1.CreateRecipeResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	recipe, err := recipeFromProto(req.GetRecipe())
	if err != nil {
		return nil, err
	}
	recipe.HouseholdID = user.HouseholdID()
	if err := s.checkComponents(ctx, recipe); err != nil {
		return nil, err
	}
	created, err := s.recipes.CreateRecipe(ctx, recipe)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to create recipe")
	}
	return recipesv1.CreateRecipeResponse_builder{Recipe: recipeToProto(created)}.Build(), nil
}

func (s *Service) GetRecipe(ctx context.Context, req *recipesv1.GetRecipeRequest) (*recipesv1.GetRecipeResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	id, err := parseRecipeID(req.GetId())
	if err != nil {
		return nil, err
	}
	recipe, err := s.recipes.GetRecipe(ctx, user.HouseholdID(), id)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to get recipe")
	}
	return recipesv1.GetRecipeResponse_builder{Recipe: recipeToProto(recipe)}.Build(), nil
}

func (s *Service) ListRecipes(ctx context.Context, req *recipesv1.ListRecipesRequest) (*recipesv1.ListRecipesResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	query := strings.TrimSpace(req.GetQuery())
	if utf8.RuneCountInString(query) > maxQueryLength {
		return nil, connect.Errorf(connect.CodeInvalidArgument, "query must be at most %d characters", maxQueryLength)
	}
	tags, err := normalizeTags(req.GetTags())
	if err != nil {
		return nil, err
	}
	list, allTags, err := s.recipes.ListRecipes(ctx, user.HouseholdID(), recipes.ListFilter{
		Query:         query,
		Tags:          tags,
		FavoritesOnly: req.GetFavoritesOnly(),
	})
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to list recipes")
	}
	summaries := make([]*recipesv1.RecipeSummary, 0, len(list))
	for _, r := range list {
		summaries = append(summaries, summaryToProto(r))
	}
	return recipesv1.ListRecipesResponse_builder{Recipes: summaries, Tags: allTags}.Build(), nil
}

func (s *Service) UpdateRecipe(ctx context.Context, req *recipesv1.UpdateRecipeRequest) (*recipesv1.UpdateRecipeResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	id, err := parseRecipeID(req.GetRecipe().GetId())
	if err != nil {
		return nil, err
	}
	existing, err := s.recipes.GetRecipe(ctx, user.HouseholdID(), id)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to get recipe")
	}
	merged := recipeToProto(existing)
	if err := protomask.Apply(merged, req.GetRecipe(), req.GetUpdateMask(), keepFields...); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err.Error())
	}
	recipe, err := recipeFromProto(merged)
	if err != nil {
		return nil, err
	}
	recipe.ID = existing.ID
	recipe.HouseholdID = existing.HouseholdID
	if err := s.checkComponents(ctx, recipe); err != nil {
		return nil, err
	}
	updated, err := s.recipes.UpdateRecipe(ctx, recipe)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to update recipe")
	}
	return recipesv1.UpdateRecipeResponse_builder{Recipe: recipeToProto(updated)}.Build(), nil
}

func (s *Service) DeleteRecipe(ctx context.Context, req *recipesv1.DeleteRecipeRequest) (*recipesv1.DeleteRecipeResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	id, err := parseRecipeID(req.GetId())
	if err != nil {
		return nil, err
	}
	if err := s.recipes.DeleteRecipe(ctx, user.HouseholdID(), id); err != nil {
		return nil, s.toConnectError(ctx, err, "failed to delete recipe")
	}
	return recipesv1.DeleteRecipeResponse_builder{}.Build(), nil
}

func parseRecipeID(s string) (pgtype.UUID, error) {
	id, err := store.StringToUUID(s)
	if err != nil {
		return pgtype.UUID{}, connect.Errorf(connect.CodeInvalidArgument, "invalid recipe ID: %v; find recipes with ListRecipes", err).WithCause(err)
	}
	return id, nil
}

// toConnectError maps an error of the repository to a Connect error.
func (s *Service) toConnectError(ctx context.Context, err error, msg string) error {
	if connectErr, ok := errors.AsType[*connect.Error](err); ok {
		return connectErr
	}
	if unknown, ok := errors.AsType[*recipes.UnknownFoodsError](err); ok {
		ids := make([]string, 0, len(unknown.FoodIDs))
		for _, id := range unknown.FoodIDs {
			ids = append(ids, store.UUIDToString(id))
		}
		return connect.Errorf(connect.CodeInvalidArgument,
			"the ingredients' foods %s aren't in your household; find the household's foods with ListFoods, or create them with CreateFood",
			strings.Join(ids, ", "))
	}
	if errors.Is(err, recipes.ErrUsedAsComponent) {
		return connect.NewError(connect.CodeFailedPrecondition,
			"other recipes use the recipe as an ingredient: remove it from them first with UpdateRecipe, or keep the recipe")
	}
	if unknown, ok := errors.AsType[*recipes.UnknownComponentsError](err); ok {
		ids := make([]string, 0, len(unknown.RecipeIDs))
		for _, id := range unknown.RecipeIDs {
			ids = append(ids, store.UUIDToString(id))
		}
		return connect.Errorf(connect.CodeInvalidArgument,
			"the ingredients' recipes %s aren't in your household; find the household's recipes with ListRecipes",
			strings.Join(ids, ", "))
	}
	if errors.Is(err, recipes.ErrPlanned) {
		return connect.NewError(connect.CodeFailedPrecondition,
			"the recipe is planned: delete its cooks and staples from the plan first (see PlanService), or keep the recipe")
	}
	if errors.Is(err, store.ErrNotFound) {
		return connect.NewError(connect.CodeNotFound, "the recipe doesn't exist in your household; find recipes with ListRecipes")
	}
	s.report.Report(ctx, msg, err)
	return connect.NewError(connect.CodeInternal, msg).WithCause(err)
}
