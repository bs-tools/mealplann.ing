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

- **Daily targets**: energy, protein, carbohydrate, fat and fiber, plus the
  nutrients vegans watch: B12, iron, calcium, vitamin D, iodine, zinc and
  omega-3 (ALA).
- **Maximum portion per eating time**, in grams.
- **Minimum protein per eating time**, in grams.
- **Carbohydrate pairing**: a minimum ratio of protein (and optionally fat
  and fiber) to carbohydrate at each eating time, for reactive
  hypoglycemia.
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
    small one later, as their next small meal.
  - **Batch cooking and leftovers**: portions of the same cook on later
    days. Ingredients are bought once.
- Portions can also be eaten without a recipe, such as a handful of nuts,
  as a food and an amount.
- **Staples** are recurring portions, such as a person's usual breakfast or
  daily soy milk, added to every week until changed.
- Each day shows, for each person, their eating times, what they eat at
  each, the totals against their daily targets, and any rule warnings.

## Pantry

- The pantry tracks **quantities** of foods in stock, with an optional
  expiry date.
- Checking off the shopping list adds items to the pantry. Marking a cook as
  done takes its ingredients out.
- Amounts are kept in grams wherever possible, converted from pieces or
  milliliters with the food's weights.

## Shopping list

- Generated from a week's cooks and staples, minus what's in the pantry,
  with amounts rounded up to what's actually sold where known (such as a
  400 g can).
- Grouped by **store**, then by **store section**, for ticking off on a phone
  in the store. Everyone in the household sees it update.
- It aims for **a single shop a week**: perishable produce is planned for
  early in the week, and suggestions prefer recipes later in the week whose
  ingredients keep. A top-up trip is only suggested when a fresh ingredient
  won't last until it's cooked.
- Items can also be added by hand.

## Suggestions

`SuggestRecipes` scores the recipes for an open slot in a plan and explains
each score, for both the web app and the AI agent:

- **Shared base ingredients**: other recipes in the week use the same
  ingredients, so less is bought and the remainder of a pack is used.
- **Leftovers**: it uses what remains from the week's packs and cooks.
- **Pantry**: it uses what's in stock, especially foods close to expiry.
- **Nutrition**: it fills each person's gaps against their targets, such as
  iron paired with vitamin C, or legumes and grains together.
- **Flavor**: its ingredients go together, from a curated list of pairings
  to start with.
- **Rules**: it can be portioned within every person's rules, and it keeps
  well enough for its day in the week.

The AI agent uses the same scores through MCP, and can reason beyond them.

## Milestones

Version 1 is the first usable version, through the shopping list.

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

Later: importing recipes from URLs, supplement tracking, deploying to a new
GCP project with Neon.

## Open questions

- Her eating times: how many a day, at what times, and which line up with
  the household's main meals?
- Which nutrient targets should the first version track and display?
- Which stores, and what's bought where?
- What should the product be called in the app: foodplann.ing, or a name?
