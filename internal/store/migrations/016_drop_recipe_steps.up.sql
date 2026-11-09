-- Every recipe has instructions now, so the old format of plain steps is
-- dropped, and a recipe's steps are read from its instructions' texts. A
-- recipe still without instructions gets one for each of its steps, in
-- order, so none is lost.
update recipes set instructions = coalesce(
  (
    select jsonb_agg(jsonb_build_object('text', s.text) order by s.position)
    from unnest(recipes.steps) with ordinality as s(text, position)
  ),
  '[]'::jsonb
)
where instructions is null;

alter table recipes
  alter column instructions set default '[]'::jsonb,
  alter column instructions set not null,
  drop column steps;
