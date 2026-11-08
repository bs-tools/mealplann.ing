-- Reference foods from more than one source: USDA FoodData Central (FDC)
-- and Health Canada's Canadian Nutrient File (CNF), which households create
-- their foods from.
--
-- fdc_foods was keyed by fdc_id, and CNF's food IDs overlap FDC's, so CNF
-- foods can't go in it without changing its primary key, which the previous
-- revision's upserts (`on conflict (fdc_id)`) and searches rely on. The
-- reference foods get a table of their own instead, keyed by (source,
-- source_id), filled from fdc_foods here. The previous revision keeps
-- reading and writing fdc_foods while this one starts, and after a
-- rollback, without seeing CNF foods or later imports.
--
-- Follow-up, as this revision stops using fdc_foods and keeps foods.fdc_id
-- only for the previous revision (see below): in a later release, backfill
-- foods.reference_source and reference_id from fdc_id again, for the foods
-- the previous revision created meanwhile, and stop writing and reading
-- fdc_id; in the release after it, drop fdc_foods and foods.fdc_id.

-- Where a reference food comes from.
create type reference_food_source as enum (
  -- USDA FoodData Central's Foundation Foods and SR Legacy, imported by
  -- `foodplanning import-fdc`. source_id is the FDC ID.
  'fdc',
  -- Health Canada's Canadian Nutrient File, imported by `foodplanning
  -- import-cnf`. source_id is CNF's FoodID.
  'cnf'
);

-- The foods of the reference databases, without the animal categories.
-- They're shared by all households, which copy the ones they use into
-- foods, so all nutrition math reads one table. Imports upsert by (source,
-- source_id), and never delete.
create table reference_foods (
  source reference_food_source not null,
  -- The food's ID in its source: FDC's fdc_id, or CNF's FoodID.
  source_id integer not null,
  -- The dataset: 'foundation_food' or 'sr_legacy_food' for FDC, 'cnf' for
  -- CNF.
  data_type text not null,
  -- The English description, e.g. 'Chickpeas (garbanzo beans, bengal
  -- gram), mature seeds, raw'.
  description text not null,
  -- The French description, which CNF has, e.g. 'Pois chiches, grains
  -- entiers mûrs, crus'. Empty for FDC.
  french_description text not null default '',
  -- The food category or group, e.g. 'Legumes and Legume Products'.
  category text not null,
  -- The common portions, as a JSON array of objects with the portion's
  -- description, e.g. "1 cup, chopped" or "250ml", the amount of the unit
  -- it describes, e.g. 1 or 250, and its weight in grams:
  -- [{"description": "1 medium", "amount": 1, "grams": 110}].
  portions jsonb not null default '[]',
  publication_date date,
  energy_kcal_per_100_grams double precision,
  protein_grams_per_100_grams double precision,
  carbohydrate_grams_per_100_grams double precision,
  sugars_grams_per_100_grams double precision,
  fat_grams_per_100_grams double precision,
  fiber_grams_per_100_grams double precision,
  vitamin_b12_micrograms_per_100_grams double precision,
  iron_milligrams_per_100_grams double precision,
  calcium_milligrams_per_100_grams double precision,
  vitamin_d_micrograms_per_100_grams double precision,
  iodine_micrograms_per_100_grams double precision,
  zinc_milligrams_per_100_grams double precision,
  omega3_ala_grams_per_100_grams double precision,
  folate_micrograms_per_100_grams double precision,
  vitamin_a_micrograms_per_100_grams double precision,
  vitamin_e_milligrams_per_100_grams double precision,
  vitamin_k_micrograms_per_100_grams double precision,
  copper_milligrams_per_100_grams double precision,
  magnesium_milligrams_per_100_grams double precision,
  sodium_milligrams_per_100_grams double precision,
  potassium_milligrams_per_100_grams double precision,
  update_time timestamptz not null default current_timestamp,
  primary key (source, source_id)
);

-- FDC's foods so far. Its two datasets have about 8,000 foods, which copy
-- in well under the migrations' time limit.
insert into reference_foods (
  source,
  source_id,
  data_type,
  description,
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
  potassium_milligrams_per_100_grams,
  update_time
)
select
  'fdc',
  fdc_id,
  data_type,
  description,
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
  potassium_milligrams_per_100_grams,
  update_time
from fdc_foods;

-- Searches match whole words, stemmed ("chickpea" finds "Chickpeas"), and
-- fall back to trigrams for misspellings and partial words.
create index reference_foods_description_tsvector on reference_foods using gin (to_tsvector('english', description));
create index reference_foods_description_trgm on reference_foods using gin (description gin_trgm_ops);

-- The reference food a household's food's nutrients were copied from, null
-- for custom foods. They aren't a foreign key, so a database without the
-- imports can still hold foods copied from another, and the sources' IDs
-- never change.
alter table foods
  add column reference_source reference_food_source,
  add column reference_id integer,
  add constraint foods_reference_check check ((reference_source is null) = (reference_id is null));

-- foods.fdc_id is replaced by reference_source and reference_id. This
-- revision still writes fdc_id for foods from FDC, so the previous revision
-- shows them as from FDC after a rollback, and reads it for foods without a
-- reference, which the previous revision creates while this one starts.
-- Households have hundreds of foods, so this is quick.
update foods set reference_source = 'fdc', reference_id = fdc_id
where fdc_id is not null;
