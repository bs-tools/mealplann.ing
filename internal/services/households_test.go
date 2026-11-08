package services_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	authv1 "github.com/bs-tools/mealplann.ing/internal/gen/auth/v1"
	usersv1 "github.com/bs-tools/mealplann.ing/internal/gen/users/v1"
	"github.com/bs-tools/mealplann.ing/internal/store"
)

func (b *browser) household(t *testing.T) *usersv1.Household {
	t.Helper()
	resp, err := b.users.GetHousehold(context.Background(), usersv1.GetHouseholdRequest_builder{}.Build())
	if err != nil {
		t.Fatalf("GetHousehold: %v", err)
	}
	return resp.GetHousehold()
}

func memberNames(h *usersv1.Household) []string {
	var names []string
	for _, m := range h.GetMembers() {
		names = append(names, m.GetName())
	}
	return names
}

func TestHouseholds(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()

	// The first administrator starts the first household.
	admin := e.newBrowser()
	if _, err := admin.register(t, newAuthenticator(t, "http://localhost:8080", "localhost"), bootstrapToken, "Johan"); err != nil {
		t.Fatalf("bootstrap registration: %v", err)
	}
	adminUser := admin.currentUser(t)
	home := admin.household(t)
	if home.GetName() != "Home" || home.GetId() != adminUser.GetHouseholdId() || home.GetId() == "" {
		t.Errorf("got household %v for admin %v, want a new household named Home", home, adminUser)
	}
	if got := memberNames(home); len(got) != 1 || got[0] != "Johan" {
		t.Errorf("got members %v, want only the admin", got)
	}
	// The test user, who isn't in it, has a household of their own.
	own, err := e.users.GetHousehold(ctx, usersv1.GetHouseholdRequest_builder{}.Build())
	if err != nil {
		t.Fatalf("GetHousehold of the test user: %v", err)
	}
	if own.GetHousehold().GetId() == home.GetId() {
		t.Error("the test user is in the admin's household")
	}

	// A user invited by the admin joins the admin's household.
	created, err := admin.auth.CreateInvitation(ctx, authv1.CreateInvitationRequest_builder{
		Kind: authv1.InvitationKind_INVITATION_KIND_NEW_USER,
	}.Build())
	if err != nil {
		t.Fatalf("CreateInvitation: %v", err)
	}
	token := strings.TrimPrefix(created.GetUrl(), "http://localhost:8080/invite#")
	partner := e.newBrowser()
	if _, err := partner.register(t, newAuthenticator(t, "http://localhost:8080", "localhost"), token, "Partner"); err != nil {
		t.Fatalf("registration with invitation: %v", err)
	}
	if u := partner.currentUser(t); u.GetHouseholdId() != home.GetId() {
		t.Errorf("the invited user is in household %q, want the admin's %q", u.GetHouseholdId(), home.GetId())
	}
	shared := partner.household(t)
	if got := memberNames(shared); len(got) != 2 || got[0] != "Johan" || got[1] != "Partner" {
		t.Errorf("got members %v, want the admin, then the invited user", got)
	}
	for _, m := range shared.GetMembers() {
		if m.GetUserId() == "" {
			t.Errorf("member %v has no user ID", m)
		}
	}

	// Either can rename the household, and both see the new name.
	renamed, err := partner.users.UpdateHousehold(ctx, usersv1.UpdateHouseholdRequest_builder{Name: "  The flat \n"}.Build())
	if err != nil {
		t.Fatalf("UpdateHousehold: %v", err)
	}
	if renamed.GetHousehold().GetName() != "The flat" || len(renamed.GetHousehold().GetMembers()) != 2 {
		t.Errorf("got %v, want the household renamed to the trimmed name, with its members", renamed.GetHousehold())
	}
	if got := admin.household(t).GetName(); got != "The flat" {
		t.Errorf("the admin sees the household named %q, want The flat", got)
	}
	// Others' households are unchanged.
	if got, err := e.users.GetHousehold(ctx, usersv1.GetHouseholdRequest_builder{}.Build()); err != nil || got.GetHousehold().GetName() != own.GetHousehold().GetName() {
		t.Errorf("the test user's household is %v (%v), want it unchanged", got, err)
	}

	for _, name := range []string{"", "   ", strings.Repeat("é", 101)} {
		_, err := admin.users.UpdateHousehold(ctx, usersv1.UpdateHouseholdRequest_builder{Name: name}.Build())
		wantCode(t, err, connect.CodeInvalidArgument)
	}
	if _, err := admin.users.UpdateHousehold(ctx, usersv1.UpdateHouseholdRequest_builder{Name: strings.Repeat("é", 100)}.Build()); err != nil {
		t.Errorf("UpdateHousehold with 100 characters: %v", err)
	}

	// Signing in is required.
	_, err = e.anonUsers.GetHousehold(ctx, usersv1.GetHouseholdRequest_builder{}.Build())
	wantCode(t, err, connect.CodeUnauthenticated)
	_, err = e.anonUsers.UpdateHousehold(ctx, usersv1.UpdateHouseholdRequest_builder{Name: "Mine"}.Build())
	wantCode(t, err, connect.CodeUnauthenticated)

	// Agents can read the household with an API token.
	if _, err := e.tokenUsers.GetHousehold(ctx, usersv1.GetHouseholdRequest_builder{}.Build()); err != nil {
		t.Errorf("GetHousehold with an API token: %v", err)
	}
}

func TestLocalAdminHousehold(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	adminID, err := e.userRepo.LocalAdmin(ctx)
	if err != nil {
		t.Fatalf("LocalAdmin: %v", err)
	}
	admin, err := e.userRepo.GetUser(ctx, adminID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if !admin.IsAdmin() || !admin.HouseholdID().Valid {
		t.Fatalf("got local admin %+v, want an administrator in a household", admin)
	}
	household, err := e.userRepo.GetHousehold(ctx, admin.HouseholdID())
	if err != nil {
		t.Fatalf("GetHousehold: %v", err)
	}
	if household.Name != "Home" || len(household.Members) != 1 || household.Members[0].UserID != adminID {
		t.Errorf("got household %+v, want Home with only the local admin", household)
	}
	// The test user's household is another.
	user, err := e.userRepo.GetUser(ctx, e.userID)
	if err != nil {
		t.Fatal(err)
	}
	if user.HouseholdID() == admin.HouseholdID() {
		t.Error("the local admin joined the test user's household")
	}
	// It's the same administrator, in the same household, the next time.
	again, err := e.userRepo.LocalAdmin(ctx)
	if err != nil || again != adminID {
		t.Errorf("LocalAdmin again = %v, %v, want %v", again, err, adminID)
	}
	if _, err := e.userRepo.GetHousehold(ctx, store.NewUUID()); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetHousehold of a household that doesn't exist: got %v, want not found", err)
	}
}
