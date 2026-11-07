# foodplann.ing design

A vegan meal planner for a household: recipes, weekly plans, a pantry and a
shopping list, with nutrition checked against each person's own targets.
Everything is also exposed as an MCP server, so an AI agent can enter
recipes and plan alongside you.

This document records the decisions made so far. It changes as we iterate.

## Tech stack

The same as trail-tools, without the geo parts:

- A Go server serving a Connect API, defined in Protocol Buffers under
  `api/<domain>/v1/`, and the same API as an MCP server generated from the
  protos.
- Postgres (Neon in production, a container locally), accessed with pgx and
  sqlc, migrated with golang-migrate. Plain Postgres, without PostGIS or
  pgRouting.
- A React single-page app in `web/`, bundled with esbuild and embedded in
  the server.
- Invite-only accounts with passkeys, OAuth for MCP clients, and
  `-local-sign-in` for local development.
- Cloud Run (one small instance that scales to zero) in a new GCP project,
  built with ko and deployed with Terraform. The same Makefile, `images/`,
  CI and AGENTS.md conventions.

The repository starts as a copy of trail-tools' skeleton with all of its
trail-specific domains removed. Pulling shared code into a common module is
left for later.

## Households and people

- A **household** owns everything: recipes, foods, plans, the pantry and the
  shopping list. Users are invited into a household, and everyone in it sees
  and edits the same data, so the shopping list can be shared in the store.
- A **person** is someone the household plans meals for. A person has an
  eating pattern and nutrition rules. People are usually users, but they
  don't have to be.
- An **eating pattern** is a person's ordered list of **eating times**, each
  either a *main* meal (breakfast, lunch, dinner) or a *small* meal. For
  example, one person eats three regular main meals a day, and another eats
  small main meals with small meals in between.

### Nutrition rules per person

The app checks rules and doesn't set them. The values come from the
person's care team and are entered as-is. Every rule is optional.

- **Daily targets** for any of the tracked nutrients, see Settled details.
- **Maximum portion per eating time**, in grams.
- **Minimum protein per eating time**, in grams.
- **Carbohydrate pairing**: a minimum ratio of protein and fat to
  carbohydrate at each eating time, for reactive hypoglycemia: (protein +
  fat) / carbohydrate, in grams. Fat counts like protein, as both slow
  absorption; fiber doesn't count separately, as it's part of
  carbohydrate. Eating times without carbohydrate pass.
- **Fast carbs**: a warning when an eating time relies on foods tagged as
  quickly absorbed.
- **Dumping risk**: a warning for eating times high in sugars, and
  optionally for drinks planned together with food.

Rule results are warnings shown on the plan and returned to the AI agent,
saying what to change.

## Foods and nutrition

- A **food** is an ingredient, such as chickpeas, a brand of tofu or soy
  milk. It has nutrients per 100 g, either from **USDA FoodData Central**
  (linked by FDC ID) or entered by hand or by an AI agent from a label.
- `foodplanning import-fdc` imports the Foundation Foods and SR Legacy
  datasets into Postgres, like trail-tools' `import-osm`. The Branded
  dataset is too big for the free tier and is left out; branded products
  are entered as custom foods.
- Each food also has:
  - A typical **weight per piece** ("1 onion" ≈ 150 g) and a **density** for
    milliliters, so amounts can be converted to grams.
  - Whether it's a **fast carb**, set by hand or by the agent, since FDC has
    no glycemic index.
  - A **store section** (produce, dry goods, chilled, frozen and so on) and
    optionally a **preferred store**.
  - A **shelf life** once bought and once opened, for planning the week
    around a single shop.

## Recipes

- A recipe has a name, the number of servings it makes, ingredients, steps,
  tags (such as breakfast or batch-friendly), and preparation and cooking
  times.
- An ingredient is a food with an amount in **grams, milliliters or pieces**
  (metric only), plus an optional note, such as "finely chopped", and the
  wording from the original recipe.
- Nutrition per serving and per 100 g is computed from the ingredients, so
  a portion can be measured in grams for a maximum portion size.
- Recipes are entered in the web app or by an AI agent through MCP.
  Importing from URLs is left for later.

## Planning

- Plans are by week, starting on Monday.
- A **cook** is a recipe made on a day for a number of servings. Its
  servings are split into **portions**, each eaten by a person at an eating
  time on some day. That covers:
  - **Shared meals with split portions**: a household dinner gives one
    person a regular portion, and the other a small one now and another
    small one later, as their next small meal. Planning a shared meal
    gives each person whose eating pattern eats from it one serving at
    that eating time. A serving over the person's maximum portion (less
    what's planned there already) is capped at it, and the rest goes to
    their next small eating times that day, then the next day's; what fits
    nowhere is left over on the cook.
  - **Batch cooking and leftovers**: portions of the same cook on later
    days. Ingredients are bought once.
- Portions can also be eaten without a recipe, such as a handful of nuts,
  as a food and an amount.
- **Staples** are recurring portions, such as a person's usual breakfast or
  daily soy milk, added to every week until changed.
- Staples aren't copied into each week: reading a plan expands them on
  the dates they're on, less the dates they're skipped, in one place that
  the shopping list reuses.
- Each day shows, for each person, their eating times, what they eat at
  each, the totals against their daily targets, and any rule warnings.
  Nutrients some foods lack are lower bounds, so a "low" warning on them
  only says the value may be low.

## Pantry

- The pantry tracks **quantities** of foods in stock, in grams, as
  **lots**: what was bought or added together, with an optional expiry date
  and an optional opened date. A food can have several lots, so the oldest
  is used first, and suggestions can find what's close to expiry. A lot's
  **use-by date** is the earlier of its expiry date and its opened date plus
  the food's opened shelf life.
- Amounts are added in grams, milliliters or pieces, and converted to grams
  with the food's weights; adding an amount that can't be converted fails,
  saying what to set on the food. A lot added without an expiry date expires
  after the food's shelf life.
- Foods the household always has, like salt, spices or oil, are marked
  **always in stock** on the food. The pantry doesn't weigh them, cooks don't
  take them out, and the shopping list leaves them out; they're added to a
  list by hand when they run out.
- Completing the shopping adds the checked items of foods to the pantry, as
  new lots of the grams bought, expiring after the food's shelf life from
  the day of the shop.
- **Marking a cook done** takes its ingredients out, scaled to its
  servings and without the optional ones, from the lots to use first, as
  far as they're in stock and never below zero. What it takes from each lot
  is recorded, so un-marking it puts back exactly that, into the same lots,
  recreating emptied ones. A cook takes its ingredients out once, even when
  two phones mark it at once. Changing or deleting a done cook doesn't change
  the pantry: un-mark it and mark it again to redo it. Ingredients that can't
  be weighed are left in.
- Only cooks take from the pantry. Portions of foods and staples aren't
  taken out, as they're eaten too irregularly to track; the pantry is
  corrected by hand when it's off.

## Shopping list

- A list is for the meals of a range of dates, a week by default, and is
  shopped for on its first date. It's stored, so the household's ticks are
  shared and survive reloads; phones read it again every few seconds while
  it's open, which is enough for two people in a store.
- Generated from the cooks not yet done, scaled to their servings, the
  portions of foods, and the staples on each date, with the same expansion
  as the plan's, recipe staples contributing their scaled ingredients.
  Optional ingredients and foods always in stock are left out.
- Needs are added up per food and date in grams, and the pantry covers them
  in date order, from the lots to use first, skipping lots past their
  use-by date on the date they're needed. What's left is rounded up to whole
  packs where the food has a pack size (such as a 400 g can), or to whole
  pieces where it was planned in pieces, and shown in the unit it was
  planned in: "2 × 400 g", "3 pieces (~450 g)", "750 ml". Amounts that
  can't be weighed are listed as they are, with what to set on the food.
- Grouped by **store**, then by **store section**, in the order of a
  typical shop, for ticking off on a phone in the store. A food goes to its
  preferred store, or else the **main store**, the first of the household's
  stores. Deleting a store moves its foods and items to the main store.
- It aims for **a single shop a week**: a food's needs on dates later than
  its shelf life after the shop are split into their own **top-up** item,
  saying when it's needed, to buy fresh closer to then or to move the meal
  earlier; the list counts them. Meals aren't moved automatically: that's
  for suggestions.
- Regenerating a list after the plan or the pantry changes replaces its
  generated items, keeping the checks and notes of items of the same food,
  and the items added by hand. A list whose shopping is complete doesn't
  change.
- Items can also be added by hand: a food, which goes to its store, or free
  text, like dish soap.

## Suggestions

`SuggestRecipes`, the only method of the `SuggestionService`, scores all of
the household's recipes for an open slot of the plan and explains each
score, for both the web app and the AI agent. A slot is a shared meal on a
date (breakfast, lunch or dinner, for everyone who eats it or some of them),
or one person's eating time, like a small meal. It returns the best few,
each with its total score and the components it's made of, each with points
and a reason for people, like "uses the other 200 g of the 400 g pack of
coconut milk bought for Tuesday's Thai curry" or "adds 9 g of protein toward
Anna's daily minimum of 60 g". It also says which portions it would plan,
what it needs bought beyond the week's shopping, and the rules it would
break.

- **It's its own service, implemented by the plan's.** Agents get its
  planning workflow as its own section of the MCP instructions, but it reads
  the plan and computes nutrition exactly as `PlanService` does, so the plan
  service implements both. It's read-only, like `Get` and `List` methods.
- **The scoring is a pure package**, `internal/suggest`, with the weights as
  documented constants and a test per component. It reuses the app's own
  logic rather than approximating it: `plans.SplitMeal` for the portions,
  the shopping list's needs, pantry coverage and pack rounding, the
  pantry's use-by order, and the rules' checks of eating times.
- **Two round trips**: the plan from a week before the slot to the end of
  its week, the people, all recipes, the latest shopping lists and the
  pantry are read together, then the foods of the recipes.
- **Rule breaks are penalized, not excluded**, and flagged with
  `fits_rules` false and the warnings, so the user can still choose.
- **The shop** is the start date of the latest shopping list not yet
  complete that covers the slot's date, or else today.

The components and their weights, in points; a good suggestion scores
roughly 15 to 35:

| Component | Points |
|---|---|
| **Shared ingredients**: each ingredient another cook of the week uses (not foods always in stock) | +1.5, +1 more if fresh (keeps a week or less); at most +6 |
| **New items to buy**: foods neither the pantry nor the week's shopping cover | +1 for each fewer than 3, −1 for each more, down to −5 |
| **Rest of a pack**: what's left of packs the week buys after rounding to whole packs or pieces | +6 for all of it, in proportion for part; at most +9 |
| **Pantry**: each food the pantry covers, after the week's own needs, from lots usable on the date | +2 in proportion to the share covered, +4 more for a lot to use within 3 days; at most +12 |
| **Nutrition**: each eater's daily minimums the portions fill, given the rest of the day | +12 for a whole minimum, in proportion; at most +15 in all |
| **Nutrition**: a daily maximum the portions go over | −4, −12 more per maximum's worth over |
| **Rules**: each rule of an eating time a portion breaks that wasn't broken already: maximum portion, minimum protein, maximum carbohydrate, carbohydrate pairing, sugars, fast carbs | −8, or −3 if uncertain as foods lack data, or for too little protein in the part of a serving moved to a later small meal |
| **Rules**: a portion meeting the minimum protein of the person's eating time | +2 |
| **Keeping**: each fresh food to buy that won't keep from the shop until the date | −4 |
| **Keeping**: all fresh foods to buy keep until a date 3 or more days after the shop | +2 |
| **Keeping**: leftovers on later days within the recipe's fridge days, or to freeze, or that won't keep | +1, −1 or −5 |
| **Flavor**: each curated pairing, matched on food names (`internal/suggest/pairings.go`) | +1.5, at most +3 |
| **Variety**: each cook of the recipe within 6 days, or a staple of an eater that day | −6, −3 more within 2 days |
| **Meal tag**: tagged for the slot's meal (or "snack" for small eating times) | +2, −6 if tagged only for another meal; lunch and dinner are interchangeable, and any recipe can be a small meal |

The web app shows the suggestions in the plan-a-meal dialog, and for an
empty small meal, with a bar for the score, the reasons as chips, and "Plan
this", which calls `PlanMeal`, or `CreateCook` and `CreatePortion` for a
small meal. The AI agent uses the same scores through MCP, and can reason
beyond them, like proposing a new recipe that would score well.

## Milestones

Version 1 is the first usable version, with suggestions. All five
milestones are done.

1. **Skeleton**: the trail-tools copy with its domains removed, households
   instead of per-user ownership, `make run` working locally.
2. **Foods and recipes**: FDC import, custom foods, recipes in the web app
   and over MCP, with nutrition per serving.
3. **People and plans**: eating patterns, rules, weekly plans with cooks
   and portions, leftovers, staples, daily totals and warnings.
4. **Pantry and shopping**: pantry quantities, the shopping list grouped by
   store and section, checking off into the pantry, and cooks consuming the
   pantry.
5. **Suggestions**: `SuggestRecipes` and planning for a single weekly shop.

6. **Deploying**: Terraform for Cloud Run and Neon in `us-west1`, deployed
   by CI from `main`, see the README.

Later: importing recipes from URLs, supplement tracking.

## Settled details

- **Pantry lots**: lots rather than one amount per food, so expiry dates and
  what to use first are kept; lots used up by cooks are deleted.

- **Name**: the app calls itself foodplann.ing, like trailrunn.ing.
- **Eating patterns**: one person eats a regular breakfast, lunch and
  dinner. The other eats six times a day: small portions of the shared
  breakfast, lunch and dinner, plus mid-morning, mid-afternoon and evening
  small meals.
- **Nutrients shown in version 1**: energy, protein, carbohydrate, sugars,
  fat and fiber; B12, iron, calcium, vitamin D, iodine, zinc and omega-3
  (ALA); folate, vitamins A, E and K, copper and magnesium, whose absorption
  can drop after gastrectomy; and sodium and potassium.
- **Stores**: one main supermarket plus one or two specialty or bulk
  stores. Stores are editable in the app, ordered with the main store
  first, and each food can have a preferred store.
