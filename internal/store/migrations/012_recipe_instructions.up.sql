-- A recipe's instructions: its steps with their structure, as a JSON array
-- of objects with the step's text and, optionally, its section, the
-- ingredients it uses by position, its duration, whether it's waiting
-- time, and the steps it follows (see recipes.Step). Null for a recipe
-- still in the old format of plain steps. A recipe with instructions keeps
-- their texts in steps too, so readers of steps see them, until steps is
-- dropped once every recipe has instructions.
alter table recipes add column instructions jsonb;
