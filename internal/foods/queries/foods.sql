-- name: CreateFood :one
insert into foods (
  household_id,
  name,
  reference_source,
  reference_id,
  fdc_id,
  energy_kcal_per_100_grams,
  protein_grams_per_100_grams,
  carbohydrate_grams_per_100_grams,
  sugars_grams_per_100_grams,
  fat_grams_per_100_grams,
  fiber_grams_per_100_grams,
  vitamin_b12_micrograms_per_100_grams,
  iron_milligrams_per_100_grams,
  calcium_milligrams_per_100_grams,
  vitamin_d_micrograms_per_100_grams,
  iodine_micrograms_per_100_grams,
  zinc_milligrams_per_100_grams,
  omega3_ala_grams_per_100_grams,
  folate_micrograms_per_100_grams,
  vitamin_a_micrograms_per_100_grams,
  vitamin_e_milligrams_per_100_grams,
  vitamin_k_micrograms_per_100_grams,
  copper_milligrams_per_100_grams,
  magnesium_milligrams_per_100_grams,
  sodium_milligrams_per_100_grams,
  potassium_milligrams_per_100_grams,
  grams_per_piece,
  grams_per_milliliter,
  fast_carb,
  store_section,
  package_grams,
  shelf_life_days,
  opened_shelf_life_days,
  freezer_shelf_life_days,
  notes,
  preferred_store_id,
  always_in_stock
) values (
  @household_id,
  @name,
  @reference_source,
  @reference_id,
  @fdc_id,
  @energy_kcal_per_100_grams,
  @protein_grams_per_100_grams,
  @carbohydrate_grams_per_100_grams,
  @sugars_grams_per_100_grams,
  @fat_grams_per_100_grams,
  @fiber_grams_per_100_grams,
  @vitamin_b12_micrograms_per_100_grams,
  @iron_milligrams_per_100_grams,
  @calcium_milligrams_per_100_grams,
  @vitamin_d_micrograms_per_100_grams,
  @iodine_micrograms_per_100_grams,
  @zinc_milligrams_per_100_grams,
  @omega3_ala_grams_per_100_grams,
  @folate_micrograms_per_100_grams,
  @vitamin_a_micrograms_per_100_grams,
  @vitamin_e_milligrams_per_100_grams,
  @vitamin_k_micrograms_per_100_grams,
  @copper_milligrams_per_100_grams,
  @magnesium_milligrams_per_100_grams,
  @sodium_milligrams_per_100_grams,
  @potassium_milligrams_per_100_grams,
  @grams_per_piece,
  @grams_per_milliliter,
  @fast_carb,
  @store_section,
  @package_grams,
  @shelf_life_days,
  @opened_shelf_life_days,
  @freezer_shelf_life_days,
  @notes,
  @preferred_store_id,
  @always_in_stock
)
returning *;

-- name: GetFood :one
select * from foods where id = @id and household_id = @household_id;

-- name: ListFoods :many
-- Foods are ordered by name ignoring case, which the unique index on
-- lower(name) also makes unique.
select * from foods
where household_id = @household_id
  and (@query::text = '' or strpos(lower(name), lower(@query)) > 0)
order by lower(name)
limit @row_limit offset @row_offset;

-- name: UpdateFood :one
update foods set
  name = @name,
  energy_kcal_per_100_grams = @energy_kcal_per_100_grams,
  protein_grams_per_100_grams = @protein_grams_per_100_grams,
  carbohydrate_grams_per_100_grams = @carbohydrate_grams_per_100_grams,
  sugars_grams_per_100_grams = @sugars_grams_per_100_grams,
  fat_grams_per_100_grams = @fat_grams_per_100_grams,
  fiber_grams_per_100_grams = @fiber_grams_per_100_grams,
  vitamin_b12_micrograms_per_100_grams = @vitamin_b12_micrograms_per_100_grams,
  iron_milligrams_per_100_grams = @iron_milligrams_per_100_grams,
  calcium_milligrams_per_100_grams = @calcium_milligrams_per_100_grams,
  vitamin_d_micrograms_per_100_grams = @vitamin_d_micrograms_per_100_grams,
  iodine_micrograms_per_100_grams = @iodine_micrograms_per_100_grams,
  zinc_milligrams_per_100_grams = @zinc_milligrams_per_100_grams,
  omega3_ala_grams_per_100_grams = @omega3_ala_grams_per_100_grams,
  folate_micrograms_per_100_grams = @folate_micrograms_per_100_grams,
  vitamin_a_micrograms_per_100_grams = @vitamin_a_micrograms_per_100_grams,
  vitamin_e_milligrams_per_100_grams = @vitamin_e_milligrams_per_100_grams,
  vitamin_k_micrograms_per_100_grams = @vitamin_k_micrograms_per_100_grams,
  copper_milligrams_per_100_grams = @copper_milligrams_per_100_grams,
  magnesium_milligrams_per_100_grams = @magnesium_milligrams_per_100_grams,
  sodium_milligrams_per_100_grams = @sodium_milligrams_per_100_grams,
  potassium_milligrams_per_100_grams = @potassium_milligrams_per_100_grams,
  grams_per_piece = @grams_per_piece,
  grams_per_milliliter = @grams_per_milliliter,
  fast_carb = @fast_carb,
  store_section = @store_section,
  package_grams = @package_grams,
  shelf_life_days = @shelf_life_days,
  opened_shelf_life_days = @opened_shelf_life_days,
  freezer_shelf_life_days = @freezer_shelf_life_days,
  notes = @notes,
  preferred_store_id = @preferred_store_id,
  always_in_stock = @always_in_stock,
  update_time = now()
where id = @id and household_id = @household_id
returning *;

-- name: ListRecipesUsingFood :many
select r.name from recipes r
where r.household_id = @household_id
  and r.id in (
    select ri.recipe_id from recipe_ingredients ri
    where ri.household_id = @household_id and ri.food_id = @food_id
  )
order by lower(r.name), r.id;

-- name: CountFoodPlanUses :one
-- How many portions and staples of the plan eat the food.
select
  ((select count(*) from portions p where p.household_id = @household_id and p.food_id = @food_id)
  + (select count(*) from staples s where s.household_id = @household_id and s.food_id = @food_id))::bigint;

-- name: DeleteUnusedFood :execrows
-- Foods used by recipes, portions or staples aren't deleted, see
-- ListRecipesUsingFood and CountFoodPlanUses.
delete from foods f
where f.id = @id and f.household_id = @household_id
  and not exists (
    select 1 from recipe_ingredients ri
    where ri.household_id = @household_id and ri.food_id = @id
  )
  and not exists (
    select 1 from portions p
    where p.household_id = @household_id and p.food_id = @id
  )
  and not exists (
    select 1 from staples s
    where s.household_id = @household_id and s.food_id = @id
  );

-- name: ListFoodsByID :many
select * from foods
where household_id = @household_id and id = any(@ids::uuid[])
order by lower(name);

-- name: ListAlwaysInStockFoods :many
select * from foods
where household_id = @household_id and always_in_stock
order by lower(name);

-- name: GetFoodUsages :one
-- What uses a food: how many portions and staples of the plan eat it, and
-- its lots and grams in the pantry. Null if the household has no such food.
select
  (select count(*) from portions p where p.household_id = f.household_id and p.food_id = f.id)::bigint as portion_count,
  (select count(*) from staples s where s.household_id = f.household_id and s.food_id = f.id)::bigint as staple_count,
  (select count(*) from pantry_items pi where pi.household_id = f.household_id and pi.food_id = f.id)::bigint as pantry_lot_count,
  (select coalesce(sum(pi.grams), 0) from pantry_items pi where pi.household_id = f.household_id and pi.food_id = f.id)::double precision as pantry_grams
from foods f
where f.id = @id and f.household_id = @household_id;

-- name: ListFoodRecipes :many
-- The recipes with the food as an ingredient, by name.
select r.id, r.name from recipes r
where r.household_id = @household_id
  and r.id in (
    select ri.recipe_id from recipe_ingredients ri
    where ri.household_id = @household_id and ri.food_id = @food_id
  )
order by lower(r.name), r.id;

-- name: FindFoodsByName :many
-- The food with the name, ignoring case, first, then up to the limit of the
-- foods with similar names, most similar first.
select id, name from foods
where household_id = @household_id
  and (lower(name) = lower(@name) or name % @name or strpos(lower(name), lower(@name)) > 0)
order by lower(name) = lower(@name) desc, similarity(name, @name) desc, lower(name), id
limit @row_limit;
