package services_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	peoplev1 "github.com/bs-tools/mealplann.ing/internal/gen/people/v1"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
	usersv1 "github.com/bs-tools/mealplann.ing/internal/gen/users/v1"
	"github.com/bs-tools/mealplann.ing/internal/store"
)

// TestCalendarFeed checks that a user's calendar feed has the meals of
// the person they are, and the cooks they eat from, starting the recipe's
// time before the meal; that making a new feed stops the old URL working,
// as deleting it does; and that API tokens can't make one.
func TestCalendarFeed(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	me := createPerson(t, e, peoplev1.Person_builder{
		Name:        "Erin",
		UserId:      store.UUIDToString(e.userID),
		EatingTimes: []*peoplev1.EatingTime{eatingTime("Dinner", mainMeal, "19:00", dinner)},
	}.Build())
	tofu := customFood(t, e, "Tofu, firm", 144, 15, 3, 0.6, 9, nil)
	bowls, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: recipesv1.Recipe_builder{
		Name: "Tofu bowls; spicy", Servings: 2,
		Ingredients: []*recipesv1.Ingredient{ingredient(tofu.GetId(), 300, grams)},
		Instructions: []*recipesv1.Step{
			recipesv1.Step_builder{Text: "Fry the tofu", DurationSeconds: 1800}.Build(),
		},
	}.Build()}.Build())
	if err != nil {
		t.Fatalf("CreateRecipe: %v", err)
	}
	today := time.Now().UTC().Format(time.DateOnly)
	if _, err := e.plans.CreatePortion(ctx, plansv1.CreatePortionRequest_builder{
		Portion: plansv1.Portion_builder{
			PersonId: me.GetId(), EatingTimeId: eatingTimeID(t, me, "Dinner"), Date: today,
			Cook: plansv1.CookShare_builder{Servings: 1}.Build(),
		}.Build(),
		NewCook: plansv1.NewCook_builder{RecipeId: bowls.GetRecipe().GetId(), Servings: 2}.Build(),
	}.Build()); err != nil {
		t.Fatalf("CreatePortion: %v", err)
	}

	if _, err := e.anonUsers.CreateCalendarFeed(ctx, usersv1.CreateCalendarFeedRequest_builder{}.Build()); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Errorf("got %v anonymously, want Unauthenticated", err)
	}
	created, err := e.users.CreateCalendarFeed(ctx, usersv1.CreateCalendarFeedRequest_builder{}.Build())
	if err != nil {
		t.Fatalf("CreateCalendarFeed: %v", err)
	}
	feedURL := created.GetFeed().GetUrl()
	path, ok := strings.CutPrefix(feedURL, "https://mealplann.ing.test")
	if !ok || !strings.HasPrefix(path, "/calendar/cal_") || !strings.HasSuffix(path, ".ics") {
		t.Fatalf("got URL %q, want one on the app's public URL", feedURL)
	}
	get := func(path string) (int, string) {
		t.Helper()
		resp, err := e.srv.Client().Get(e.srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}
	code, body := get(path)
	if code != http.StatusOK {
		t.Fatalf("got %d: %s", code, body)
	}
	day := strings.ReplaceAll(today, "-", "")
	for _, want := range []string{
		"BEGIN:VCALENDAR\r\n",
		"X-WR-CALNAME:mealplann.ing · Erin\r\n",
		// Dinner at 19:00, for half an hour, with its name escaped.
		"DTSTART:" + day + "T190000\r\nDTEND:" + day + "T193000\r\nSUMMARY:Dinner: Tofu bowls\\; spicy\r\n",
		// The cook starts its 30 minutes before dinner.
		"DTSTART:" + day + "T183000\r\nDTEND:" + day + "T190000\r\nSUMMARY:Cook Tofu bowls\\; spicy (2 servings)\r\n",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("calendar is missing %q:\n%s", want, body)
		}
	}
	got, err := e.users.GetCalendarFeed(ctx, usersv1.GetCalendarFeedRequest_builder{}.Build())
	if err != nil || !got.HasFeed() || got.GetFeed().GetUrl() != "" || !got.GetFeed().HasLastUsedTime() {
		t.Errorf("got %v, %v, want a feed, read, without its URL", got, err)
	}

	// A new feed replaces the old one.
	renewed, err := e.users.CreateCalendarFeed(ctx, usersv1.CreateCalendarFeedRequest_builder{}.Build())
	if err != nil {
		t.Fatalf("CreateCalendarFeed: %v", err)
	}
	if code, _ := get(path); code != http.StatusNotFound {
		t.Errorf("got %d for the old URL, want 404", code)
	}
	newPath := strings.TrimPrefix(renewed.GetFeed().GetUrl(), "https://mealplann.ing.test")
	if _, err := e.users.DeleteCalendarFeed(ctx, usersv1.DeleteCalendarFeedRequest_builder{}.Build()); err != nil {
		t.Fatalf("DeleteCalendarFeed: %v", err)
	}
	if code, _ := get(newPath); code != http.StatusNotFound {
		t.Errorf("got %d once deleted, want 404", code)
	}
}
