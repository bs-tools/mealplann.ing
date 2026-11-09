-- name: CreateRecipe :one
insert into recipes (
  id,
  household_id,
  name,
  description,
  servings,
  prep_seconds,
  cook_seconds,
  steps,
  instructions,
  tags,
  source_url,
  fridge_days,
  freezable,
  freezer_days,
  prep_ahead_days,
  prep_ahead_note,
  notes,
  favorite
) values (
  @id,
  @household_id,
  @name,
  @description,
  @servings,
  @prep_seconds,
  @cook_seconds,
  @steps,
  @instructions,
  @tags,
  @source_url,
  @fridge_days,
  @freezable,
  @freezer_days,
  @prep_ahead_days,
  @prep_ahead_note,
  @notes,
  @favorite
)
returning *;

-- name: GetRecipe :one
select * from recipes where id = @id and household_id = @household_id;

-- name: ListRecipes :many
select r.* from recipes r
where r.household_id = @household_id
  and (
    @query::text = ''
    or strpos(lower(r.name), lower(@query)) > 0
    or strpos(lower(r.description), lower(@query)) > 0
  )
  and r.tags @> @tags::text[]
  and (not @favorites_only::boolean or r.favorite)
  and (not @without_instructions::boolean or r.instructions is null)
  and (
    cardinality(@food_ids::uuid[]) = 0
    or r.id in (
      select ri.recipe_id from recipe_ingredients ri
      where ri.household_id = @household_id and ri.food_id = any(@food_ids::uuid[])
    )
  )
order by lower(r.name), r.id;

-- name: FindRecipesByName :many
-- The recipe with the name, ignoring case, first, then up to the limit of
-- the recipes with similar names, most similar first.
select id, name from recipes
where household_id = @household_id
  and (lower(name) = lower(@name) or name % @name or strpos(lower(name), lower(@name)) > 0)
order by lower(name) = lower(@name) desc, similarity(name, @name) desc, lower(name), id
limit @row_limit;

-- name: ListRecipesByID :many
select * from recipes
where household_id = @household_id and id = any(@ids::uuid[])
order by lower(name), id;

-- name: ListRecipeTags :many
select distinct tag::text from recipes, unnest(tags) as tag
where household_id = @household_id
order by tag;

-- name: UpdateRecipe :one
update recipes set
  name = @name,
  description = @description,
  servings = @servings,
  prep_seconds = @prep_seconds,
  cook_seconds = @cook_seconds,
  steps = @steps,
  instructions = @instructions,
  tags = @tags,
  source_url = @source_url,
  fridge_days = @fridge_days,
  freezable = @freezable,
  freezer_days = @freezer_days,
  prep_ahead_days = @prep_ahead_days,
  prep_ahead_note = @prep_ahead_note,
  notes = @notes,
  favorite = @favorite,
  update_time = now()
where id = @id and household_id = @household_id
returning *;

-- name: DeleteRecipe :execrows
delete from recipes where id = @id and household_id = @household_id;

-- name: ListHouseholdRecipeIDs :many
-- Of the given recipe IDs, those of the household's recipes.
select id from recipes
where household_id = @household_id and id = any(@ids::uuid[]);

-- name: ListHouseholdFoodIDs :many
-- Of the given food IDs, those of the household's foods.
select id from foods
where household_id = @household_id and id = any(@ids::uuid[]);

-- name: DeleteRecipeIngredients :exec
delete from recipe_ingredients
where recipe_id = @recipe_id and household_id = @household_id;

-- name: CreateRecipeIngredients :exec
-- The ingredients are given as arrays of their fields, to insert them all in
-- one statement. A component has a null food and an empty unit, and a food
-- a null component.
insert into recipe_ingredients (
  recipe_id,
  household_id,
  position,
  food_id,
  component_recipe_id,
  amount,
  unit,
  note,
  original_text,
  optional
)
select
  @recipe_id,
  @household_id,
  i - 1,
  (@food_ids::uuid[])[i],
  (@component_recipe_ids::uuid[])[i],
  (@amounts::double precision[])[i],
  nullif((@units::text[])[i], '')::amount_unit,
  (@notes::text[])[i],
  (@original_texts::text[])[i],
  (@optionals::boolean[])[i]
from generate_subscripts(@amounts::double precision[], 1) as i;

-- name: ListRecipeIngredients :many
-- The ingredients of the given recipes, or of all the household's recipes,
-- in order, with what the nutrition needs of their foods.
select
  sqlc.embed(recipe_ingredients),
  coalesce(f.name, '')::text as food_name,
  f.energy_kcal_per_100_grams, f.protein_grams_per_100_grams, f.carbohydrate_grams_per_100_grams, f.sugars_grams_per_100_grams, f.fat_grams_per_100_grams, f.fiber_grams_per_100_grams, f.vitamin_b12_micrograms_per_100_grams, f.iron_milligrams_per_100_grams, f.calcium_milligrams_per_100_grams, f.vitamin_d_micrograms_per_100_grams, f.iodine_micrograms_per_100_grams, f.zinc_milligrams_per_100_grams, f.omega3_ala_grams_per_100_grams, f.folate_micrograms_per_100_grams, f.vitamin_a_micrograms_per_100_grams, f.vitamin_e_milligrams_per_100_grams, f.vitamin_k_micrograms_per_100_grams, f.copper_milligrams_per_100_grams, f.magnesium_milligrams_per_100_grams, f.sodium_milligrams_per_100_grams, f.potassium_milligrams_per_100_grams,
  f.grams_per_piece,
  f.grams_per_milliliter,
  f.measure,
  coalesce(f.fast_carb, false)::boolean as fast_carb,
  f.price_cents_per_kilogram
from recipe_ingredients
left join foods f on f.id = recipe_ingredients.food_id
where recipe_ingredients.household_id = @household_id
  and (@all_recipes::boolean or recipe_ingredients.recipe_id = any(@recipe_ids::uuid[]))
order by recipe_ingredients.recipe_id, recipe_ingredients.position;
