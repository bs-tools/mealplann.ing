import BottomNav from "@components/header/bottom-nav";
import Header from "@components/header/header";
import { Code } from "@connectrpc/connect";
import { useQuery } from "@connectrpc/connect-query";
import { getCurrentUser } from "gen/users/v1/users-UserService_connectquery";
import type { User } from "gen/users/v1/users_pb";
import { hideSplash } from "helpers/splash";
import AutoPlanPage from "pages/auto-plan";
import CookModePage from "pages/cook-mode";
import CooksPage from "pages/cooks";
import FoodPage from "pages/food";
import FoodsPage from "pages/foods";
import HomePage from "pages/home";
import InvitePage from "pages/invite";
import OAuthConsentPage from "pages/oauth-consent";
import PantryPage from "pages/pantry";
import PantryAddPage from "pages/pantry-add";
import PantryCookPage from "pages/pantry-cook";
import PeoplePage from "pages/people";
import PlanPage from "pages/plan";
import PlanTemplatePage from "pages/plan-template";
import PlanTemplatesPage from "pages/plan-templates";
import PrivacyPage from "pages/privacy";
import RecipePage from "pages/recipe";
import RecipeEditPage from "pages/recipe-edit";
import RecipeImportPage from "pages/recipe-import";
import RecipeTimelinePage from "pages/recipe-timeline";
import RecipesPage from "pages/recipes";
import SettingsPage from "pages/settings";
import ShoppingPage from "pages/shopping";
import SignInPage from "pages/sign-in";
import TodayChangePage from "pages/today-change";
import React from "react";
import { Link, Outlet, Route, Routes } from "react-router";

/**
 * Layout is the header above the page, and on phones the tab bar below it,
 * which the page leaves room for.
 */
const Layout: React.FC<{ user: User | null }> = ({ user }) => (
  <div className="flex min-h-full flex-col">
    <Header user={user} />
    <main
      className={`flex grow justify-center bg-ground print:bg-white ${user === null ? "" : "pb-20 lg:pb-0 print:pb-0"}`}
    >
      <div className="flex w-full max-w-7xl min-w-0 grow flex-col px-4 py-5 sm:px-5 print:px-0 print:py-0">
        <Outlet />
      </div>
    </main>
    {user !== null && <BottomNav />}
  </div>
);

const NotFound: React.FC = () => (
  <div className="flex grow flex-col items-center gap-2 py-10">
    <h1 className="text-xl font-semibold">Page not found</h1>
    <Link to="/" className="text-brand-600 hover:underline">
      Back home
    </Link>
  </div>
);

const App: React.FC = () => {
  const { isPending, isError, data, error } = useQuery(
    getCurrentUser,
    {},
    {
      retry: (failureCount, err) => {
        // Disable automatic retries on Unauthenticated errors
        // Since we expect it if the user is not logged in.
        if (err.code === Code.Unauthenticated) {
          return false;
        }
        return failureCount < 3;
      },
      // 7 24 hour days
      staleTime: 1000 * 60 * 60 * 24 * 7,
    }
  );

  /**
   * The splash screen in index.html covers the page until the app has the
   * user and something to show.
   */
  React.useEffect(() => {
    if (!isPending) {
      hideSplash();
    }
  }, [isPending]);

  if (isPending) {
    return null;
  }

  // We got a response, let's check if it's an error
  // User is null if the user is not logged in.
  let user: User | null = null;
  if (isError) {
    if (error.code !== Code.Unauthenticated) {
      // Some other error, lets pretend the user is not logged in
      console.error("Failed to get current user:", error);
    }
    user = null;
  } else if (data.user === undefined) {
    // This is a programmer error, the user should always be set in the response.
    return (
      <div className="h-screen flex items-center justify-center">
        <h1>Error: No user data, please report this bug!</h1>
      </div>
    );
  } else {
    ({ user } = data);
  }

  /**
   * Pages that need a user are only routed to when signed in. Signed out,
   * any other page shows the sign in page instead.
   */
  return (
    <Routes>
      <Route element={<Layout user={user} />}>
        <Route path="/invite" element={<InvitePage />} />
        <Route path="/privacy" element={<PrivacyPage />} />
        {user !== null && (
          <>
            <Route path="/" element={<HomePage user={user} />} />
            <Route path="/today/change" element={<TodayChangePage />} />
            <Route path="/plan" element={<PlanPage />} />
            <Route path="/plan/auto" element={<AutoPlanPage />} />
            <Route path="/plan/templates" element={<PlanTemplatesPage />} />
            <Route path="/plan/templates/:id" element={<PlanTemplatePage />} />
            <Route path="/people" element={<PeoplePage />} />
            <Route path="/shopping" element={<ShoppingPage />} />
            <Route path="/pantry" element={<PantryPage />} />
            <Route path="/pantry/add" element={<PantryAddPage />} />
            <Route path="/pantry/cook" element={<PantryCookPage />} />
            <Route path="/recipes" element={<RecipesPage />} />
            <Route path="/recipes/new" element={<RecipeEditPage />} />
            <Route path="/recipes/import" element={<RecipeImportPage />} />
            <Route path="/cooks" element={<CooksPage />} />
            <Route path="/recipes/:id" element={<RecipePage />} />
            <Route path="/recipes/:id/edit" element={<RecipeEditPage />} />
            <Route path="/recipes/:id/cook" element={<CookModePage />} />
            <Route
              path="/recipes/:id/timeline"
              element={<RecipeTimelinePage />}
            />
            <Route path="/foods" element={<FoodsPage />} />
            <Route path="/foods/:id" element={<FoodPage />} />
            <Route path="/settings" element={<SettingsPage user={user} />} />
            <Route path="/oauth/authorize" element={<OAuthConsentPage />} />
            <Route path="*" element={<NotFound />} />
          </>
        )}
      </Route>
      {user === null && <Route path="*" element={<SignInPage />} />}
    </Routes>
  );
};

export default App;
