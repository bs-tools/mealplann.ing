-- name: UpsertReferenceFood :exec
insert into reference_foods (
  source,
  source_id,
  data_type,
  description,
  french_description,
  category,
  portions,
  publication_date,
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
  potassium_milligrams_per_100_grams
) values (
  @source,
  @source_id,
  @data_type,
  @description,
  @french_description,
  @category,
  @portions,
  @publication_date,
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
  @potassium_milligrams_per_100_grams
)
on conflict (source, source_id) do update set
  data_type = excluded.data_type,
  description = excluded.description,
  french_description = excluded.french_description,
  category = excluded.category,
  portions = excluded.portions,
  publication_date = excluded.publication_date,
  energy_kcal_per_100_grams = excluded.energy_kcal_per_100_grams,
  protein_grams_per_100_grams = excluded.protein_grams_per_100_grams,
  carbohydrate_grams_per_100_grams = excluded.carbohydrate_grams_per_100_grams,
  sugars_grams_per_100_grams = excluded.sugars_grams_per_100_grams,
  fat_grams_per_100_grams = excluded.fat_grams_per_100_grams,
  fiber_grams_per_100_grams = excluded.fiber_grams_per_100_grams,
  vitamin_b12_micrograms_per_100_grams = excluded.vitamin_b12_micrograms_per_100_grams,
  iron_milligrams_per_100_grams = excluded.iron_milligrams_per_100_grams,
  calcium_milligrams_per_100_grams = excluded.calcium_milligrams_per_100_grams,
  vitamin_d_micrograms_per_100_grams = excluded.vitamin_d_micrograms_per_100_grams,
  iodine_micrograms_per_100_grams = excluded.iodine_micrograms_per_100_grams,
  zinc_milligrams_per_100_grams = excluded.zinc_milligrams_per_100_grams,
  omega3_ala_grams_per_100_grams = excluded.omega3_ala_grams_per_100_grams,
  folate_micrograms_per_100_grams = excluded.folate_micrograms_per_100_grams,
  vitamin_a_micrograms_per_100_grams = excluded.vitamin_a_micrograms_per_100_grams,
  vitamin_e_milligrams_per_100_grams = excluded.vitamin_e_milligrams_per_100_grams,
  vitamin_k_micrograms_per_100_grams = excluded.vitamin_k_micrograms_per_100_grams,
  copper_milligrams_per_100_grams = excluded.copper_milligrams_per_100_grams,
  magnesium_milligrams_per_100_grams = excluded.magnesium_milligrams_per_100_grams,
  sodium_milligrams_per_100_grams = excluded.sodium_milligrams_per_100_grams,
  potassium_milligrams_per_100_grams = excluded.potassium_milligrams_per_100_grams,
  update_time = now();

-- name: GetReferenceFood :one
select * from reference_foods where source = @source and source_id = @source_id;

-- name: SearchReferenceFoods :many
-- Foods of all sources match all the query's words, stemmed, or, for
-- misspelled and partial words, enough of its trigrams. The best matches
-- are those whose words are most like the query, especially at the start of
-- the description, where FDC and CNF name the food ("Lentils, raw"), then
-- the shortest, which are the plainest ("Spinach, raw" before "Spinach,
-- cooked, boiled, drained, with salt"). Neither source is preferred, so
-- foods both have are side by side.
select
  sqlc.embed(reference_foods),
  (
    select f.id from foods f
    where f.household_id = @household_id
      and (
        (f.reference_source = reference_foods.source and f.reference_id = reference_foods.source_id)
        -- Foods the previous revision created have only an FDC ID, see
        -- migration 005.
        or (f.reference_source is null and reference_foods.source = 'fdc' and f.fdc_id = reference_foods.source_id)
      )
    order by f.create_time, f.id
    limit 1
  )::uuid as food_id
from reference_foods
where to_tsvector('english', description) @@ websearch_to_tsquery('english', @query)
  or @query <% description
order by
  word_similarity(@query, description)
    + similarity(@query, split_part(description, ',', 1))
    + ts_rank(to_tsvector('english', description), websearch_to_tsquery('english', @query)) desc,
  length(description),
  source,
  source_id
limit @row_limit;
