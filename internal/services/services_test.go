// Package services_test contains integration tests of the API services,
// running against a real database through the HTTP handlers and
// authentication middleware.
package services_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"github.com/bs-tools/mealplann.ing/internal/apilimit"
	"github.com/bs-tools/mealplann.ing/internal/authn"
	"github.com/bs-tools/mealplann.ing/internal/blob"
	"github.com/bs-tools/mealplann.ing/internal/cnf"
	"github.com/bs-tools/mealplann.ing/internal/errreport"
	"github.com/bs-tools/mealplann.ing/internal/fdc"
	"github.com/bs-tools/mealplann.ing/internal/foods"
	"github.com/bs-tools/mealplann.ing/internal/gen/auth/v1/authv1connect"
	"github.com/bs-tools/mealplann.ing/internal/gen/foods/v1/foodsv1connect"
	"github.com/bs-tools/mealplann.ing/internal/gen/oauth/v1/oauthv1connect"
	"github.com/bs-tools/mealplann.ing/internal/gen/pantry/v1/pantryv1connect"
	"github.com/bs-tools/mealplann.ing/internal/gen/people/v1/peoplev1connect"
	"github.com/bs-tools/mealplann.ing/internal/gen/plans/v1/plansv1connect"
	"github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1/recipesv1connect"
	"github.com/bs-tools/mealplann.ing/internal/gen/shopping/v1/shoppingv1connect"
	"github.com/bs-tools/mealplann.ing/internal/gen/suggestions/v1/suggestionsv1connect"
	"github.com/bs-tools/mealplann.ing/internal/gen/users/v1/usersv1connect"
	"github.com/bs-tools/mealplann.ing/internal/mcp"
	"github.com/bs-tools/mealplann.ing/internal/oauth"
	"github.com/bs-tools/mealplann.ing/internal/pantry"
	"github.com/bs-tools/mealplann.ing/internal/people"
	"github.com/bs-tools/mealplann.ing/internal/plans"
	"github.com/bs-tools/mealplann.ing/internal/recipes"
	"github.com/bs-tools/mealplann.ing/internal/services/auth"
	"github.com/bs-tools/mealplann.ing/internal/services/food"
	oauthsvc "github.com/bs-tools/mealplann.ing/internal/services/oauth"
	pantrysvc "github.com/bs-tools/mealplann.ing/internal/services/pantry"
	"github.com/bs-tools/mealplann.ing/internal/services/person"
	"github.com/bs-tools/mealplann.ing/internal/services/plan"
	"github.com/bs-tools/mealplann.ing/internal/services/recipe"
	shoppingsvc "github.com/bs-tools/mealplann.ing/internal/services/shopping"
	"github.com/bs-tools/mealplann.ing/internal/services/user"
	"github.com/bs-tools/mealplann.ing/internal/shopping"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/bs-tools/mealplann.ing/internal/store/storetest"
	"github.com/bs-tools/mealplann.ing/internal/templates"
	"github.com/bs-tools/mealplann.ing/internal/users"
	"github.com/jackc/pgx/v5/pgtype"
)

// env is a running server for a test, with clients authenticated as a user.
type env struct {
	t        *testing.T
	db       *store.DB
	userRepo *users.Repository
	userID   pgtype.UUID
	// The user's API token, and session ID.
	token      string
	session    string
	otherToken string
	// The other user, whose token is otherToken, in another household.
	otherUserID pgtype.UUID
	srv         *httptest.Server

	// The user's settings, and OAuth connections below, are signed in with
	// the session; the other clients with the API token.
	users     usersv1connect.UserServiceClient
	anonUsers usersv1connect.UserServiceClient
	// The user service, signed in with the API token.
	tokenUsers usersv1connect.UserServiceClient
	oauth      oauthv1connect.OAuthServiceClient
	anonOAuth  oauthv1connect.OAuthServiceClient

	// The food and recipe services, signed in with the API token, as the
	// other user in another household, and anonymously.
	foods        foodsv1connect.FoodServiceClient
	otherFoods   foodsv1connect.FoodServiceClient
	anonFoods    foodsv1connect.FoodServiceClient
	recipes      recipesv1connect.RecipeServiceClient
	otherRecipes recipesv1connect.RecipeServiceClient
	anonRecipes  recipesv1connect.RecipeServiceClient
	foodRepo     *foods.Repository

	// The person and plan services, signed in with the API token, as the
	// other user in another household, and anonymously.
	people      peoplev1connect.PersonServiceClient
	otherPeople peoplev1connect.PersonServiceClient
	anonPeople  peoplev1connect.PersonServiceClient
	plans       plansv1connect.PlanServiceClient
	otherPlans  plansv1connect.PlanServiceClient
	anonPlans   plansv1connect.PlanServiceClient
	// templates, otherTemplates and anonTemplates are the plan template
	// clients of the same users.
	templates      plansv1connect.PlanTemplateServiceClient
	otherTemplates plansv1connect.PlanTemplateServiceClient
	anonTemplates  plansv1connect.PlanTemplateServiceClient

	// The pantry and shopping services, signed in with the API token, as
	// the other user in another household, and anonymously.
	pantry        pantryv1connect.PantryServiceClient
	otherPantry   pantryv1connect.PantryServiceClient
	anonPantry    pantryv1connect.PantryServiceClient
	shopping      shoppingv1connect.ShoppingServiceClient
	otherShopping shoppingv1connect.ShoppingServiceClient
	anonShopping  shoppingv1connect.ShoppingServiceClient

	// The suggestion service, signed in with the API token, as the other
	// user in another household, and anonymously.
	suggestions      suggestionsv1connect.SuggestionServiceClient
	otherSuggestions suggestionsv1connect.SuggestionServiceClient
	anonSuggestions  suggestionsv1connect.SuggestionServiceClient
}

// consentPage stands in for the frontend's OAuth consent page.
const consentPage = "consent page"

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	report := errreport.NewLocal(log)
	db := storetest.New(t)
	userRepo, err := users.NewRepository(log, db)
	if err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, db: db, userRepo: userRepo}
	e.userID = storetest.NewUser(t, db)
	_, e.token, err = userRepo.CreateAPIToken(ctx, e.userID, "test")
	if err != nil {
		t.Fatal(err)
	}
	e.session, err = userRepo.CreateSession(ctx, e.userID, nil, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	e.otherUserID = storetest.NewUser(t, db)
	_, e.otherToken, err = userRepo.CreateAPIToken(ctx, e.otherUserID, "other")
	if err != nil {
		t.Fatal(err)
	}

	authService, err := auth.NewService(log, report, userRepo, "http://localhost:8080", bootstrapToken)
	if err != nil {
		t.Fatal(err)
	}
	avatars, err := blob.NewDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	userService := user.NewService(log, report, userRepo, userRepo, userRepo, avatars)
	e.foodRepo = foods.NewRepository(log, db)
	foodService := food.NewService(log, report, e.foodRepo)
	recipeRepo := recipes.NewRepository(log, db)
	recipeService := recipe.NewService(log, report, recipeRepo)
	peopleRepo := people.NewRepository(log, db)
	personService := person.NewService(log, report, peopleRepo)
	planRepo := plans.NewRepository(log, db)
	pantryRepo := pantry.NewRepository(log, db)
	shoppingRepo := shopping.NewRepository(log, db)
	planService := plan.NewService(log, report, planRepo, peopleRepo, recipeRepo, e.foodRepo, pantryRepo, shoppingRepo, templates.NewRepository(log, db))
	pantryService := pantrysvc.NewService(log, report, pantryRepo, e.foodRepo)
	shoppingService := shoppingsvc.NewService(log, report, shoppingRepo, planRepo, recipeRepo, e.foodRepo, pantryRepo)

	// Start listening before creating the handler, as the OAuth server needs
	// the URL of the server.
	e.srv = httptest.NewUnstartedServer(nil)
	t.Cleanup(e.srv.Close)
	oauthServer := oauth.NewServer(log, report, userRepo, "http://"+e.srv.Listener.Addr().String(), "/mcp")
	resourceMetadataURL := oauthServer.ResourceMetadataURL()

	mux := http.NewServeMux()
	server := connect.NewServer()
	oauthv1connect.RegisterOAuthServiceHandler(server, oauthsvc.NewService(log, report, oauthServer, userRepo))
	connecthttp.Mount(mux, server)
	oauthServer.RegisterHandlers(mux, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, consentPage)
	}))
	srv2 := connect.NewServer()
	usersv1connect.RegisterUserServiceHandler(srv2, userService)
	authv1connect.RegisterAuthServiceHandler(srv2, authService)
	foodsv1connect.RegisterFoodServiceHandler(srv2, foodService)
	recipesv1connect.RegisterRecipeServiceHandler(srv2, recipeService)
	peoplev1connect.RegisterPersonServiceHandler(srv2, personService)
	plansv1connect.RegisterPlanServiceHandler(srv2, planService)
	plansv1connect.RegisterPlanTemplateServiceHandler(srv2, planService)
	pantryv1connect.RegisterPantryServiceHandler(srv2, pantryService)
	shoppingv1connect.RegisterShoppingServiceHandler(srv2, shoppingService)
	suggestionsv1connect.RegisterSuggestionServiceHandler(srv2, planService)
	apilimit.Mount(mux, srv2)
	mux.Handle(user.AvatarPath, userService.AvatarHandler())
	// The same services as main.go serves over MCP.
	mcpServer, err := mcp.NewServer(log, report,
		mcp.Service{Name: usersv1connect.UserServiceName, Handler: userService},
		mcp.Service{Name: foodsv1connect.FoodServiceName, Handler: foodService},
		mcp.Service{Name: recipesv1connect.RecipeServiceName, Handler: recipeService},
		mcp.Service{Name: peoplev1connect.PersonServiceName, Handler: personService},
		mcp.Service{Name: plansv1connect.PlanServiceName, Handler: planService},
		mcp.Service{Name: plansv1connect.PlanTemplateServiceName, Handler: planService},
		mcp.Service{Name: pantryv1connect.PantryServiceName, Handler: pantryService},
		mcp.Service{Name: shoppingv1connect.ShoppingServiceName, Handler: shoppingService},
		mcp.Service{Name: suggestionsv1connect.SuggestionServiceName, Handler: planService},
	)
	if err != nil {
		t.Fatal(err)
	}
	mux.Handle("/mcp", authn.RequireUser(resourceMetadataURL, mcpServer.Handler()))
	handler, err := authn.NewAuthnMiddleware(ctx, log, report, userRepo, mux, authn.WithResourceMetadata(resourceMetadataURL))
	if err != nil {
		t.Fatal(err)
	}
	e.srv.Config.Handler = handler
	e.srv.Start()

	client := e.srv.Client()
	authed := connect.NewClient(connecthttp.NewTransport(client, e.srv.URL), bearerInterceptor(e.token))
	anon := connect.NewClient(connecthttp.NewTransport(client, e.srv.URL))
	// Accounts are managed in the web app, which is signed in with a session
	// cookie rather than a token.
	signedIn := connect.NewClient(connecthttp.NewTransport(client, e.srv.URL), sessionInterceptor(e.session))
	e.users = usersv1connect.NewUserServiceClient(signedIn)
	e.anonUsers = usersv1connect.NewUserServiceClient(anon)
	e.tokenUsers = usersv1connect.NewUserServiceClient(authed)
	e.oauth = oauthv1connect.NewOAuthServiceClient(signedIn)
	e.anonOAuth = oauthv1connect.NewOAuthServiceClient(anon)
	other := connect.NewClient(connecthttp.NewTransport(client, e.srv.URL), bearerInterceptor(e.otherToken))
	e.foods = foodsv1connect.NewFoodServiceClient(authed)
	e.otherFoods = foodsv1connect.NewFoodServiceClient(other)
	e.anonFoods = foodsv1connect.NewFoodServiceClient(anon)
	e.recipes = recipesv1connect.NewRecipeServiceClient(authed)
	e.otherRecipes = recipesv1connect.NewRecipeServiceClient(other)
	e.anonRecipes = recipesv1connect.NewRecipeServiceClient(anon)
	e.people = peoplev1connect.NewPersonServiceClient(authed)
	e.otherPeople = peoplev1connect.NewPersonServiceClient(other)
	e.anonPeople = peoplev1connect.NewPersonServiceClient(anon)
	e.plans = plansv1connect.NewPlanServiceClient(authed)
	e.otherPlans = plansv1connect.NewPlanServiceClient(other)
	e.anonPlans = plansv1connect.NewPlanServiceClient(anon)
	e.templates = plansv1connect.NewPlanTemplateServiceClient(authed)
	e.otherTemplates = plansv1connect.NewPlanTemplateServiceClient(other)
	e.anonTemplates = plansv1connect.NewPlanTemplateServiceClient(anon)
	e.pantry = pantryv1connect.NewPantryServiceClient(authed)
	e.otherPantry = pantryv1connect.NewPantryServiceClient(other)
	e.anonPantry = pantryv1connect.NewPantryServiceClient(anon)
	e.shopping = shoppingv1connect.NewShoppingServiceClient(authed)
	e.otherShopping = shoppingv1connect.NewShoppingServiceClient(other)
	e.anonShopping = shoppingv1connect.NewShoppingServiceClient(anon)
	e.suggestions = suggestionsv1connect.NewSuggestionServiceClient(authed)
	e.otherSuggestions = suggestionsv1connect.NewSuggestionServiceClient(other)
	e.anonSuggestions = suggestionsv1connect.NewSuggestionServiceClient(anon)
	return e
}

// importFDC imports the FoodData Central fixtures of the importer's tests:
// chickpeas, tofu, lentils, spinach and onions from SR Legacy, and soy milk
// and yellow onions from Foundation Foods.
func (e *env) importFDC() {
	e.t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := fdc.Import(context.Background(), log, e.foodRepo, "../fdc/testdata/sr_legacy", "../fdc/testdata/foundation"); err != nil {
		e.t.Fatalf("importing FDC: %v", err)
	}
}

// importCNF imports the Canadian Nutrient File fixture of the importer's
// tests: a fortified soy beverage and margarine, canned chickpeas and an
// apple.
func (e *env) importCNF() {
	e.t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := cnf.Import(context.Background(), log, e.foodRepo, "../cnf/testdata/cnf_2015"); err != nil {
		e.t.Fatalf("importing CNF: %v", err)
	}
}

// bootstrapToken is the admin bootstrap token in tests.
const bootstrapToken = "bootstrap-secret"

// bearerInterceptor authenticates requests with an API token.
func bearerInterceptor(token string) connect.ClientInterceptor {
	return func(next connect.ClientFunc) connect.ClientFunc {
		return func(ctx context.Context, spec connect.Spec) (connect.ClientStream, error) {
			if info, ok := connect.CallInfoForClientContext(ctx); ok {
				info.RequestHeader().Set("Authorization", "Bearer "+token)
			}
			return next(ctx, spec)
		}
	}
}

// sessionInterceptor authenticates requests with a session cookie, like the
// web app.
func sessionInterceptor(sessionID string) connect.ClientInterceptor {
	return func(next connect.ClientFunc) connect.ClientFunc {
		return func(ctx context.Context, spec connect.Spec) (connect.ClientStream, error) {
			if info, ok := connect.CallInfoForClientContext(ctx); ok {
				info.RequestHeader().Set("Cookie", "__Host-session="+sessionID)
			}
			return next(ctx, spec)
		}
	}
}

// wantCode fails the test unless err is a connect error with the code.
func wantCode(t *testing.T, err error, code connect.Code) {
	t.Helper()
	if connect.CodeOf(err) != code {
		t.Fatalf("got error %v, want code %v", err, code)
	}
}
