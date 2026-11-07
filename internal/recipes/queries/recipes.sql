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
  tags,
  source_url,
  fridge_days,
  freezable,
  notes
) values (
  @id,
  @household_id,
  @name,
  @description,
  @servings,
  @prep_seconds,
  @cook_seconds,
  @steps,
  @tags,
  @source_url,
  @fridge_days,
  @freezable,
  @notes
)
returning *;

-- name: GetRecipe :one
select * from recipes where id = @id and household_id = @household_id;

-- name: ListRecipes :many
select * from recipes
where household_id = @household_id
  and (
    @query::text = ''
    or strpos(lower(name), lower(@query)) > 0
    or strpos(lower(description), lower(@query)) > 0
  )
  and tags @> @tags::text[]
order by lower(name), id;

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
  tags = @tags,
  source_url = @source_url,
  fridge_days = @fridge_days,
  freezable = @freezable,
  notes = @notes,
  update_time = now()
where id = @id and household_id = @household_id
returning *;

-- name: DeleteRecipe :execrows
delete from recipes where id = @id and household_id = @household_id;

-- name: ListHouseholdFoodIDs :many
-- Of the given food IDs, those of the household's foods.
select id from foods
where household_id = @household_id and id = any(@ids::uuid[]);

-- name: DeleteRecipeIngredients :exec
delete from recipe_ingredients
where recipe_id = @recipe_id and household_id = @household_id;

-- name: CreateRecipeIngredients :exec
-- The ingredients are given as arrays of their fields, to insert them all in
-- one statement.
insert into recipe_ingredients (
  recipe_id,
  household_id,
  position,
  food_id,
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
  (@amounts::double precision[])[i],
  ((@units::text[])[i])::amount_unit,
  (@notes::text[])[i],
  (@original_texts::text[])[i],
  (@optionals::boolean[])[i]
from generate_subscripts(@food_ids::uuid[], 1) as i;

-- name: ListRecipeIngredients :many
-- The ingredients of the given recipes, or of all the household's recipes,
-- in order, with what the nutrition needs of their foods.
select
  sqlc.embed(recipe_ingredients),
  f.name as food_name,
  f.energy_kcal_per_100_grams, f.protein_grams_per_100_grams, f.carbohydrate_grams_per_100_grams, f.sugars_grams_per_100_grams, f.fat_grams_per_100_grams, f.fiber_grams_per_100_grams, f.vitamin_b12_micrograms_per_100_grams, f.iron_milligrams_per_100_grams, f.calcium_milligrams_per_100_grams, f.vitamin_d_micrograms_per_100_grams, f.iodine_micrograms_per_100_grams, f.zinc_milligrams_per_100_grams, f.omega3_ala_grams_per_100_grams, f.folate_micrograms_per_100_grams, f.vitamin_a_micrograms_per_100_grams, f.vitamin_e_milligrams_per_100_grams, f.vitamin_k_micrograms_per_100_grams, f.copper_milligrams_per_100_grams, f.magnesium_milligrams_per_100_grams, f.sodium_milligrams_per_100_grams, f.potassium_milligrams_per_100_grams,
  f.grams_per_piece,
  f.grams_per_milliliter,
  f.fast_carb
from recipe_ingredients
join foods f on f.id = recipe_ingredients.food_id
where recipe_ingredients.household_id = @household_id
  and (@all_recipes::boolean or recipe_ingredients.recipe_id = any(@recipe_ids::uuid[]))
order by recipe_ingredients.recipe_id, recipe_ingredients.position;
