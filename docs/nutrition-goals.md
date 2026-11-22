# Nutrition goals

Nutrition goals are each person's own nutrition rules and daily targets
(see the design's "Nutrition rules per person"). The rules cover the
largest portion, the least protein, carbohydrate pairing, sugars and fast
carbs at each eating time. The targets are daily minimums and maximums of
energy and nutrients.

In November 2026 we turned them off. Planning around them wasn't good
enough for the household to trust yet, so we're getting the plan right
without them first. This page says what's off, how to turn the goals back
on, and what we learned while they were on.

## What's off, and what's kept

The server's `-nutrition-goals` flag is off by default. While it's off:

- **Planning ignores the goals.** The plan service reads people through
  `people.WithoutGoals`, which strips their rules and targets, so:
  - "Plan for me" and suggestions score nothing for nutrition or rules
    (`NUTRITION`, `RULES`);
  - portions are a serving each, not scaled toward targets
    (`plans.ServingsToward`), and not split by the largest portion;
  - GetWeek gives no warnings about targets or rules, only the rest, like
    leftovers to thaw or servings nobody eats.
- **The data stays.** People's rules, targets and profiles are kept in
  the database, and PersonService still reads and writes them, so nothing
  is lost.
- **The web app hides the goals.** `GetCurrentUserResponse.nutrition_goals`
  tells it the flag's state, through `useNutritionGoals()`. It hides:
  - the daily target bars on Today and the Plan page;
  - the rules and targets on the People page and in the person dialog;
  - the body, activity, goal and recommendation on the profile (tastes
    stay);
  - the Nutrition emphasis, "Re-plan for nutrition" and the rules wording
    in "Plan for me";
  - the targets wording on the warnings page and in suggestions.
- **Food facts stay.** Recipes and foods still show their nutrition: the
  nutrition panel and the balance card are facts about the food, not
  anyone's goals.

The tests still build the plan service with goals on, so the code behind
them stays covered.

## Turning them back on

1. Pass `-nutrition-goals` to the server: in `infra/` for production, and
   in the Makefile's `run` and `watch` targets to try it locally.
2. Look over everyone's rules and targets: they're as they were, which
   may no longer be right.
3. Work through the learnings below first, or the same problems come back.

## What we learned

### Portion sizes

- **Scaling toward targets gave odd servings.** Portions were sized from
  each person's share of their daily targets, so the same lasagne split
  as 1.333 servings for one person and 0.667 for the other, and a
  Bolognese as 1.75 and 1. People eat a piece of lasagne, not two thirds
  of one, and the odd fractions spread into cooks and leftovers, like 0.06
  of a serving of udon left over.
- **Next time, size portions by a fixed amount per person.** A fixed
  number of servings per person per meal, like 1.5 for someone who eats
  more, is more predictable. Show how it compares to their targets rather
  than resizing to meet them.
- **The largest portion spilled into other eating times.** A meal over a
  person's largest portion moved the rest into another eating time ("the
  rest, eaten at…"). That's correct, but surprising when it happens on
  its own.

### Scoring

- **Nutrition outweighed practical concerns.** It could earn up to 15
  points, and each broken rule cost 8. Together they often beat what the
  household cared about more:
  - eating leftovers already cooked before they went off (since fixed:
    leftovers on hand now earn 36, see `OnHandPoints`);
  - cooking once a day;
  - using the pantry.
- **Reasons read as nutrition bookkeeping.** "Adds 41 g of carbohydrate
  toward Charlotte's daily minimum of 80 g" doesn't help choose dinner.
  Next time, keep nutrition a tiebreaker below leftovers, cooking effort
  and the shop, and show it only when it decides between suggestions.
- **The "Nutrition" emphasis and "Re-plan for nutrition" were rarely
  right.** Putting nutrition first broke what made the plan workable,
  like shared ingredients and leftovers.

### Warnings

- **Daily targets warned about days still being planned.** "Protein is
  4 g, under the daily minimum of 80 g. 3 eating times are still
  unplanned" was on most future days. Marking such warnings uncertain
  didn't stop the noise.
- **Next time, warn only about days that are fully planned.** Or compare
  a week's average with the targets: a day under target followed by a day
  over is fine.
- **Eaten-out meals and staples skewed the totals.** Eating out isn't
  counted, so those days always read low. Staples, like a breakfast
  smoothie, made the rest of the day look over.
- **Too many warnings meant they were ignored,** including the useful
  ones, like leftovers to thaw. Those had to be separated from the target
  warnings (`isTargetWarning`) to stay visible.

### Targets and recommendations

- **Targets came from formulas, not the care team.** The recommendations
  (Mifflin-St Jeor with activity and goal factors) were adopted as
  targets, rather than entered from the care team. Some, like 1440 kcal a
  day, were exceeded by nearly every ordinary plan, so they warned all the
  time.
- **Next time, make it obvious which targets came from where.** Treat a
  recommended target as softer than one the care team set.

### For the next attempt

- **Start with one goal at a time.** Protein per day is the one the
  household cares about most, as a soft score with no warnings.
- **Test with real weeks.** Write scenario tests from the household's own
  plans, like the lasagne week, rather than synthetic people, and check
  what the planner picks, not just the scores.
- **Keep leftovers first.** On-hand leftovers, cooking once a day and the
  pantry should stay ahead of nutrition in the scoring order.
