package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/bs-tools/foodplann.ing/internal/errreport"
	authv1 "github.com/bs-tools/foodplann.ing/internal/gen/auth/v1"
	"github.com/bs-tools/foodplann.ing/internal/gen/auth/v1/authv1connect"
	foodsv1 "github.com/bs-tools/foodplann.ing/internal/gen/foods/v1"
	"github.com/bs-tools/foodplann.ing/internal/gen/foods/v1/foodsv1connect"
	"github.com/bs-tools/foodplann.ing/internal/gen/oauth/v1/oauthv1connect"
	"github.com/bs-tools/foodplann.ing/internal/gen/pantry/v1/pantryv1connect"
	"github.com/bs-tools/foodplann.ing/internal/gen/people/v1/peoplev1connect"
	"github.com/bs-tools/foodplann.ing/internal/gen/plans/v1/plansv1connect"
	recipesv1 "github.com/bs-tools/foodplann.ing/internal/gen/recipes/v1"
	"github.com/bs-tools/foodplann.ing/internal/gen/recipes/v1/recipesv1connect"
	"github.com/bs-tools/foodplann.ing/internal/gen/shopping/v1/shoppingv1connect"
	usersv1 "github.com/bs-tools/foodplann.ing/internal/gen/users/v1"
	"github.com/bs-tools/foodplann.ing/internal/gen/users/v1/usersv1connect"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// allServices returns every API service, with unimplemented handlers. The
// server doesn't serve the auth and OAuth services over MCP, but they have
// more kinds of methods to test.
func allServices() []Service {
	return []Service{
		{Name: authv1connect.AuthServiceName, Handler: authv1connect.UnimplementedAuthServiceHandler{}},
		{Name: oauthv1connect.OAuthServiceName, Handler: oauthv1connect.UnimplementedOAuthServiceHandler{}},
		{Name: usersv1connect.UserServiceName, Handler: usersv1connect.UnimplementedUserServiceHandler{}},
		{Name: foodsv1connect.FoodServiceName, Handler: foodsv1connect.UnimplementedFoodServiceHandler{}},
		{Name: recipesv1connect.RecipeServiceName, Handler: recipesv1connect.UnimplementedRecipeServiceHandler{}},
		{Name: peoplev1connect.PersonServiceName, Handler: peoplev1connect.UnimplementedPersonServiceHandler{}},
		{Name: plansv1connect.PlanServiceName, Handler: plansv1connect.UnimplementedPlanServiceHandler{}},
		{Name: pantryv1connect.PantryServiceName, Handler: pantryv1connect.UnimplementedPantryServiceHandler{}},
		{Name: shoppingv1connect.ShoppingServiceName, Handler: shoppingv1connect.UnimplementedShoppingServiceHandler{}},
	}
}

func TestNewServer(t *testing.T) {
	srv, err := NewServer(discardLogger(), errreport.NewLocal(discardLogger()), allServices()...)
	if err != nil {
		t.Fatal(err)
	}
	tools := map[string]*mcp.Tool{}
	for _, tool := range srv.Tools() {
		tools[tool.Name] = tool
	}

	for _, name := range []string{
		"CreateAPIToken", "ListAPITokens", "DeleteAPIToken", "UploadAvatar", "DeleteAvatar", "UpdateHousehold",
	} {
		if _, ok := tools[name]; ok {
			t.Errorf("excluded method %s is a tool", name)
		}
	}
	for _, name := range []string{
		"GetCurrentUser", "GetHousehold", "CreateInvitation", "ListInvitations", "ApproveAuthorization",
		"SearchFoodData", "CreateFood", "UpdateFood", "DeleteFood", "CreateRecipe", "ListRecipes", "UpdateRecipe",
		"CreatePerson", "ListPeople", "UpdatePerson", "GetWeek", "PlanMeal", "CreatePortion", "CreateStaple", "SkipStaple",
	} {
		if _, ok := tools[name]; !ok {
			t.Errorf("method %s is not a tool", name)
		}
	}

	tests := []struct {
		name                              string
		title                             string
		readOnly, destructive, idempotent bool
		description                       string
	}{
		{
			name:        "CreateInvitation",
			title:       "Create Invitation",
			description: "Create an invitation. Only administrators can invite new users. Any user can invite themselves to add a passkey on another device.",
		},
		{
			name:        "GetHousehold",
			title:       "Get Household",
			readOnly:    true,
			idempotent:  true,
			description: "Get the signed in user's household: the people who share recipes, plans, the pantry and the shopping list.",
		},
		{
			name:        "DeleteInvitation",
			title:       "Delete Invitation",
			destructive: true,
			idempotent:  true,
			description: "Delete an invitation created by the signed in user.",
		},
		{
			name:       "ApproveAuthorization",
			title:      "Approve Authorization",
			idempotent: true,
		},
		{
			name:       "ListConnections",
			title:      "List Connections",
			readOnly:   true,
			idempotent: true,
		},
		{
			name:       "SignOutOtherSessions",
			title:      "Sign Out Other Sessions",
			idempotent: true,
		},
		{
			name:       "SearchFoodData",
			title:      "Search Food Data",
			readOnly:   true,
			idempotent: true,
		},
		{
			name:        "DeleteFood",
			title:       "Delete Food",
			destructive: true,
			idempotent:  true,
			description: "Delete one of the household's foods. Fails with FAILED_PRECONDITION, naming the recipes, if recipes use it: remove it from them first. Also fails with FAILED_PRECONDITION if portions or staples of the plan (see PlanService) eat it: delete them first. Its stock in the pantry is deleted, and shopping list items of it are kept by name.",
		},
		{
			name:  "CreateRecipe",
			title: "Create Recipe",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := tools[tt.name]
			if tool == nil {
				t.Fatal("tool not found")
			}
			if tool.Title != tt.title {
				t.Errorf("title = %q, want %q", tool.Title, tt.title)
			}
			if tt.description != "" && tool.Description != tt.description {
				t.Errorf("description = %q, want %q", tool.Description, tt.description)
			}
			a := tool.Annotations
			if a.ReadOnlyHint != tt.readOnly {
				t.Errorf("readOnlyHint = %v, want %v", a.ReadOnlyHint, tt.readOnly)
			}
			if *a.DestructiveHint != tt.destructive {
				t.Errorf("destructiveHint = %v, want %v", *a.DestructiveHint, tt.destructive)
			}
			if a.IdempotentHint != tt.idempotent {
				t.Errorf("idempotentHint = %v, want %v", a.IdempotentHint, tt.idempotent)
			}
			if *a.OpenWorldHint {
				t.Error("openWorldHint = true, want false")
			}
			if tool.InputSchema.(*jsonschema.Schema).Type != "object" {
				t.Error("input schema is not an object")
			}
			if tool.OutputSchema.(*jsonschema.Schema).Type != "object" {
				t.Error("output schema is not an object")
			}
		})
	}

	// Every tool from a documented service has a description.
	for _, tool := range srv.Tools() {
		if tool.Description == "" {
			t.Errorf("tool %s has no description", tool.Name)
		}
	}
}

func TestNewServerErrors(t *testing.T) {
	tests := []struct {
		name     string
		services []Service
		wantErr  string
	}{
		{
			name: "unknown service",
			services: []Service{
				{Name: "foo.v1.FooService", Handler: struct{}{}},
			},
			wantErr: "foo.v1.FooService",
		},
		{
			name: "nil handler",
			services: []Service{
				{Name: usersv1connect.UserServiceName},
			},
			wantErr: "no handler",
		},
		{
			name: "missing method",
			services: []Service{
				{Name: usersv1connect.UserServiceName, Handler: struct{}{}},
			},
			wantErr: "has no method GetCurrentUser",
		},
		{
			name: "wrong service implementation",
			services: []Service{
				{Name: usersv1connect.UserServiceName, Handler: wrongSignature{}},
			},
			wantErr: "want *usersv1.GetCurrentUserRequest",
		},
		{
			name: "duplicate tool name",
			services: []Service{
				{Name: usersv1connect.UserServiceName, Handler: usersv1connect.UnimplementedUserServiceHandler{}},
				{Name: usersv1connect.UserServiceName, Handler: usersv1connect.UnimplementedUserServiceHandler{}},
			},
			wantErr: `duplicate tool name "GetCurrentUser"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewServer(discardLogger(), errreport.NewLocal(discardLogger()), tt.services...)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err, tt.wantErr)
			}
		})
	}
}

type wrongSignature struct{}

func (wrongSignature) GetCurrentUser(context.Context, *usersv1.GetHouseholdRequest) (*usersv1.GetCurrentUserResponse, error) {
	return nil, nil
}

func TestValidateSignature(t *testing.T) {
	in := (&usersv1.GetHouseholdRequest{}).ProtoReflect().Type()
	out := (&usersv1.GetHouseholdResponse{}).ProtoReflect().Type()
	tests := []struct {
		name    string
		fn      any
		wantErr bool
	}{
		{
			name: "valid",
			fn:   usersv1connect.UnimplementedUserServiceHandler{}.GetHousehold,
		},
		{
			name:    "wrong response",
			fn:      usersv1connect.UnimplementedUserServiceHandler{}.UpdateHousehold,
			wantErr: true,
		},
		{
			name: "no context",
			fn: func(*usersv1.GetHouseholdRequest) (*usersv1.GetHouseholdResponse, error) {
				return nil, nil
			},
			wantErr: true,
		},
		{
			name: "wrong request",
			fn: func(context.Context, *usersv1.UpdateHouseholdRequest) (*usersv1.GetHouseholdResponse, error) {
				return nil, nil
			},
			wantErr: true,
		},
		{
			name: "no response",
			fn: func(context.Context, *usersv1.GetHouseholdRequest) error {
				return nil
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSignature(reflect.TypeOf(tt.fn), in, out)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateSignature() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestInstructions(t *testing.T) {
	srv, err := NewServer(discardLogger(), errreport.NewLocal(discardLogger()), allServices()...)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	session := connectClient(t, srv.Handler())
	initRes := session.InitializeResult()
	if initRes == nil {
		t.Fatal("no initialize result")
	}
	if initRes.ServerInfo.Name != "foodplanning" {
		t.Errorf("server name = %q, want foodplanning", initRes.ServerInfo.Name)
	}
	for _, want := range []string{
		"## users.v1.UserService\n\nUserService provides information about the signed in user",
		"## auth.v1.AuthService\n\nAuthService signs users in with passkeys.",
		"## oauth.v1.OAuthService\n\nOAuthService lets users connect OAuth clients",
		"## foods.v1.FoodService\n\nFoodService manages the household's foods",
		"## recipes.v1.RecipeService\n\nRecipeService manages the household's recipes.",
		// Numbered lists keep an item per line, so they stay lists in
		// Markdown.
		"To add an ingredient:\n\n1. Look for it among the household's foods with ListFoods, and reuse it if it's there.\n2. Otherwise,",
		"## people.v1.PersonService\n\nPersonService manages the people the household plans meals for.",
		"## plans.v1.PlanService\n\nPlanService plans the household's meals, by week.",
		"A plan is made of:\n\n1. Cooks: a recipe (see RecipeService) made on a date for a number of servings, scaling the recipe,",
		"\n2. Portions: what a person (see PersonService) eats at one of their eating times on a date.",
		"\n3. Staples: recurring portions,",
		"## pantry.v1.PantryService\n\nPantryService tracks the household's pantry",
		"The pantry changes by itself in two ways:\n\n1. Completing a shopping list (see ShoppingService.CompleteShopping) adds the checked items,",
		"\n2. Marking a cook done (see PlanService.UpdateCook) takes its ingredients out,",
		"## shopping.v1.ShoppingService\n\nShoppingService makes the household's shopping lists from its meal plan",
		"To help with the weekly shop:\n\n1. Plan the week first with PlanService, and check the pantry with PantryService.ListPantry,",
		"\n2. Create the list with CreateShoppingList, from the day of the shop.",
		"\n3. Look at the items with top_up set:",
		"\n4. Add anything else the user needs with AddManualItem.",
	} {
		if !strings.Contains(initRes.Instructions, want) {
			t.Errorf("instructions do not contain %q:\n%s", want, initRes.Instructions)
		}
	}

	var names []string
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, tool.Name)
	}
	if len(names) != len(srv.Tools()) {
		t.Errorf("listed %d tools, want %d", len(names), len(srv.Tools()))
	}
}

type ctxKey struct{}

// fakeAuth is an auth service implementation for testing.
type fakeAuth struct {
	authv1connect.UnimplementedAuthServiceHandler

	mu       sync.Mutex
	requests []proto.Message
}

func (f *fakeAuth) record(m proto.Message) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, m)
}

func (f *fakeAuth) CreateInvitation(ctx context.Context, req *authv1.CreateInvitationRequest) (*authv1.CreateInvitationResponse, error) {
	f.record(req)
	user, ok := ctx.Value(ctxKey{}).(string)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "no user in context")
	}
	if req.GetNote() == "missing" {
		return nil, connect.NewError(connect.CodeNotFound, "user not found")
	}
	return authv1.CreateInvitationResponse_builder{
		Invitation: authv1.Invitation_builder{
			Id:            "i1",
			Kind:          req.GetKind(),
			Note:          req.GetNote(),
			CreatedByName: user,
			CreateTime:    timestamppb.New(time.Date(2026, 5, 1, 7, 30, 0, 0, time.UTC)),
		}.Build(),
		Url: "https://foodplann.example/invite#secret",
	}.Build(), nil
}

func (f *fakeAuth) DeleteInvitation(context.Context, *authv1.DeleteInvitationRequest) (*authv1.DeleteInvitationResponse, error) {
	return nil, errors.New("database is down")
}

func (f *fakeAuth) ListInvitations(context.Context, *authv1.ListInvitationsRequest) (*authv1.ListInvitationsResponse, error) {
	panic("boom")
}

// connectClient connects an MCP client to an HTTP server serving h.
func connectClient(t *testing.T, h http.Handler) *mcp.ClientSession {
	t.Helper()
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: ts.URL}, nil)
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func TestCallTool(t *testing.T) {
	auth := &fakeAuth{}
	srv, err := NewServer(discardLogger(), errreport.NewLocal(discardLogger()),
		Service{Name: authv1connect.AuthServiceName, Handler: auth},
	)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate authentication middleware that puts the user on the context.
	authn := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), ctxKey{}, "alice")
		srv.Handler().ServeHTTP(w, r.WithContext(ctx))
	})
	session := connectClient(t, authn)
	ctx := t.Context()

	t.Run("success", func(t *testing.T) {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "CreateInvitation",
			Arguments: map[string]any{"kind": "INVITATION_KIND_NEW_USER", "note": "For Bob"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if res.IsError {
			t.Fatalf("tool error: %v", res.Content)
		}
		got := &authv1.CreateInvitationResponse{}
		data, err := json.Marshal(res.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		if err := protojson.Unmarshal(data, got); err != nil {
			t.Fatalf("unmarshaling structured content %s: %v", data, err)
		}
		if got.GetInvitation().GetCreatedByName() != "alice" {
			t.Errorf("created by = %q, want the context value to reach the handler", got.GetInvitation().GetCreatedByName())
		}
		if got.GetInvitation().GetNote() != "For Bob" || got.GetInvitation().GetKind() != authv1.InvitationKind_INVITATION_KIND_NEW_USER {
			t.Errorf("invitation = %v, want the arguments to reach the handler", got.GetInvitation())
		}
		// Fields with default values are included, so agents can tell
		// them from fields they don't know.
		var fields struct {
			Invitation map[string]any `json:"invitation"`
		}
		if err := json.Unmarshal(data, &fields); err != nil {
			t.Fatal(err)
		}
		if v, ok := fields.Invitation["userName"]; !ok || v != "" {
			t.Errorf("invitation userName = %v (present %v), want empty", v, ok)
		}
		if v, ok := fields.Invitation["bootstrap"]; !ok || v != false {
			t.Errorf("invitation bootstrap = %v (present %v), want false", v, ok)
		}
		if len(res.Content) != 1 {
			t.Fatalf("got %d content blocks, want 1", len(res.Content))
		}
		text := res.Content[0].(*mcp.TextContent).Text
		textMsg := &authv1.CreateInvitationResponse{}
		if err := protojson.Unmarshal([]byte(text), textMsg); err != nil {
			t.Fatalf("unmarshaling text content: %v", err)
		}
		if !proto.Equal(got, textMsg) {
			t.Errorf("text content %v != structured content %v", textMsg, got)
		}

		// The structured content validates against the output schema.
		var tool *mcp.Tool
		for _, tl := range srv.Tools() {
			if tl.Name == "CreateInvitation" {
				tool = tl
			}
		}
		resolved, err := tool.OutputSchema.(*jsonschema.Schema).Resolve(nil)
		if err != nil {
			t.Fatal(err)
		}
		var instance any
		if err := json.Unmarshal(data, &instance); err != nil {
			t.Fatal(err)
		}
		if err := resolved.Validate(instance); err != nil {
			t.Errorf("structured content does not validate against the output schema: %v", err)
		}
	})

	t.Run("no arguments", func(t *testing.T) {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "CreateInvitation"})
		if err != nil {
			t.Fatal(err)
		}
		if res.IsError {
			t.Fatalf("tool error: %v", res.Content)
		}
	})

	tests := []struct {
		name     string
		tool     string
		args     map[string]any
		wantText string
	}{
		{
			name:     "connect error",
			tool:     "CreateInvitation",
			args:     map[string]any{"note": "missing"},
			wantText: "not_found: user not found",
		},
		{
			name:     "plain error",
			tool:     "DeleteInvitation",
			args:     map[string]any{"id": "i1"},
			wantText: "unknown: database is down",
		},
		{
			name:     "unimplemented",
			tool:     "ListPasskeys",
			wantText: "unimplemented: auth.v1.AuthService.ListPasskeys is not implemented",
		},
		{
			name:     "panic",
			tool:     "ListInvitations",
			wantText: "internal: internal error",
		},
		{
			name:     "unknown field",
			tool:     "CreateInvitation",
			args:     map[string]any{"invitationNote": "x"},
			wantText: `invalid_argument: invalid arguments, check them against the tool input schema: proto:`,
		},
		{
			name:     "wrong type",
			tool:     "CreateInvitation",
			args:     map[string]any{"note": 1},
			wantText: `invalid_argument: invalid arguments`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tt.tool, Arguments: tt.args})
			if err != nil {
				t.Fatal(err)
			}
			if !res.IsError {
				t.Fatal("expected a tool error")
			}
			text := res.Content[0].(*mcp.TextContent).Text
			if !strings.HasPrefix(text, tt.wantText) {
				t.Errorf("error text = %q, want prefix %q", text, tt.wantText)
			}
		})
	}
}

// fakeRecipes is a recipe service recording the recipes it's asked to
// update, and returning them as they are.
type fakeRecipes struct {
	recipesv1connect.UnimplementedRecipeServiceHandler

	mu      sync.Mutex
	updates []*recipesv1.UpdateRecipeRequest
}

func (f *fakeRecipes) UpdateRecipe(_ context.Context, req *recipesv1.UpdateRecipeRequest) (*recipesv1.UpdateRecipeResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, req)
	return recipesv1.UpdateRecipeResponse_builder{Recipe: req.GetRecipe()}.Build(), nil
}

func TestCallToolClearsOutputOnly(t *testing.T) {
	recipes := &fakeRecipes{}
	srv, err := NewServer(discardLogger(), errreport.NewLocal(discardLogger()),
		Service{Name: recipesv1connect.RecipeServiceName, Handler: recipes},
	)
	if err != nil {
		t.Fatal(err)
	}
	session := connectClient(t, srv.Handler())

	// An agent sends back a recipe as it got it, with the fields the server
	// computes, which the input schema doesn't have.
	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: "UpdateRecipe",
		Arguments: map[string]any{
			"recipe": map[string]any{
				"id":         "r1",
				"name":       "Lentil soup",
				"servings":   4,
				"createTime": "2026-05-01T07:30:00Z",
				"nutrition":  map[string]any{"totalGrams": 900},
				"ingredients": []any{
					map[string]any{"foodId": "f1", "foodName": "Lentils", "amount": 200, "unit": "AMOUNT_UNIT_GRAMS"},
				},
			},
			"updateMask": "name,servings",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("tool error: %v", res.Content)
	}
	if len(recipes.updates) != 1 {
		t.Fatalf("got %d updates, want 1", len(recipes.updates))
	}
	got := recipes.updates[0]
	want := recipesv1.UpdateRecipeRequest_builder{
		Recipe: recipesv1.Recipe_builder{
			Id:       "r1",
			Name:     "Lentil soup",
			Servings: 4,
			Ingredients: []*recipesv1.Ingredient{
				recipesv1.Ingredient_builder{FoodId: "f1", Amount: 200, Unit: foodsv1.AmountUnit_AMOUNT_UNIT_GRAMS}.Build(),
			},
		}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"name", "servings"}},
	}.Build()
	if !proto.Equal(got, want) {
		t.Errorf("got request %v, want the output only fields cleared, even in the ingredients: %v", got, want)
	}

	// The input schema leaves them out, but keeps the ID to update.
	var tool *mcp.Tool
	for _, tl := range srv.Tools() {
		if tl.Name == "UpdateRecipe" {
			tool = tl
		}
	}
	recipe := tool.InputSchema.(*jsonschema.Schema).Properties["recipe"]
	if recipe == nil {
		t.Fatal("no recipe in the input schema")
	}
	for _, name := range []string{"nutrition", "createTime", "updateTime"} {
		if _, ok := recipe.Properties[name]; ok {
			t.Errorf("the input schema has the output only field %s", name)
		}
	}
	if _, ok := recipe.Properties["id"]; !ok {
		t.Error("the input schema has no id")
	}
}

func TestClearOutputOnly(t *testing.T) {
	res := usersv1.GetHouseholdResponse_builder{
		Household: usersv1.Household_builder{
			Id:   "h1",
			Name: "Home",
			Members: []*usersv1.HouseholdMember{
				usersv1.HouseholdMember_builder{UserId: "u1", Name: "Alice"}.Build(),
			},
		}.Build(),
	}.Build()
	clearOutputOnly(res.ProtoReflect())
	want := usersv1.GetHouseholdResponse_builder{
		Household: usersv1.Household_builder{Name: "Home"}.Build(),
	}.Build()
	if !proto.Equal(res, want) {
		t.Errorf("got %v, want the output only fields of the nested household cleared: %v", res, want)
	}
}

// TODO(milestone 3): restore TestCallToolImage, which checks that
// mcp.v1.Image fields of results become image content, once a method
// returns an image, like a photo of a recipe or a cooked meal. No method of
// foods and recipes does yet.
