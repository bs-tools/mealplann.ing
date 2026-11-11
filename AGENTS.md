# mealplann.ing

A vegan meal planner for a household: recipes, weekly meal plans, a pantry and
a shopping list, with nutrition checked against each person's targets. A Go
server serves a React single-page app and a Connect API, which it also exposes
to AI agents as an MCP server. Data is in Postgres. It runs on Cloud Run, with
Neon Postgres, deployed with Terraform, like trail-tools, whose skeleton it was
copied from. `docs/design.md` has the design and the milestones.

Directories have their own AGENTS.md with the rules for their code: `api/`,
`internal/`, `internal/services/`, `internal/store/`, `web/` and `infra/`.

## Layout

- `api/<domain>/v1/*.proto`: the API, in Protocol Buffers.
- `main.go`: the server, its flags and wiring.
- `internal/<domain>/` (e.g. `users`): repositories over the database, with
  sqlc queries in `queries/`.
- `internal/services/<name>/`: the Connect services implementing the API.
- `internal/store/`: the database connection and migrations.
- `internal/` also has authentication (`authn`), the OAuth server (`oauth`),
  the MCP server (`mcp`), and telemetry and error reporting (`telemetry`,
  `errreport`).
- `web/`: the React app. Its bundle in `web/dist/` is embedded in the server.
- `infra/`: Terraform for Google Cloud.
- `images/Dockerfile`: the container images the Makefile and CI run, like
  Node's, Postgres's and the server's base image, and the Terraform version,
  kept there for Dependabot to update. `images/image.sh` reads them.

## Commands

```bash
make run      # Start Postgres and the server on http://localhost:8080
make watch    # The same, rebuilding the web app and restarting the server on changes
make test     # Start Postgres and run all Go tests with the race detector
make gen      # Regenerate code from the protos and SQL, and rebuild the web app
make format   # Format Go, TypeScript and protos
make lint     # Lint Go, TypeScript and protos
```

`make run` and `make watch` pass `-local-sign-in`, so agents and tests can
sign in to the local server without a passkey: open
http://localhost:8080/local-sign-in, optionally with `?next=/some/page`, to
sign in as the local administrator. The server only allows the flag on
localhost.

Node tools run in containers, and Go tools with `go tool` (buf, sqlc,
esbuild, gofumpt, staticcheck, ko). `make` with no target lists the rest.

CI fails if `make format` or `make gen` change anything, if `go mod tidy`
changes `go.mod`, or if lint or tests fail. Before finishing a change, run
the targets that cover it.

When you open a pull request, enable auto-merge on it with the squash method,
so it merges once the required checks pass. If its checks have already passed,
which leaves no auto-merge to enable, squash-merge it directly. Merge it as
yourself, not with a workflow's token, because merges with that token don't
start the run on main that deploys them.

## Generated code

Generated code is committed: `internal/gen/` and `web/gen/` from the protos,
`internal/*/internal/gen/` from the SQL, `internal/apidesc/descriptor.binpb`
and the bundle in `web/dist/`. Never edit it by hand; change the source and run
`make gen` (or `make buf`, `make sqlc`). `make buf` needs network access for
the TypeScript plugins.

## Conventions everywhere

- Comments and documentation use American spelling, like the code.
- Comments are full sentences, and explain why as well as what: the
  constraint, the cost, what would break otherwise.
- Quantities carry their unit in their name, in full words where they read
  well: `amount_grams`, `AmountGrams`, `protein_grams`, `cook_seconds`. Units
  are metric only.
- Calendar dates are `YYYY-MM-DD` strings in the API and `date` in the
  database. Instants are timestamps, named `*_time` (`create_time`,
  `update_time`).
- The API's comments are the documentation AI agents read through MCP. Error
  messages reach agents and users too, so make them say what to do.
- Data belongs to a household, not a user: every user is in one household,
  and its members share its recipes, plans, pantry and shopping list. Scope
  queries by `authn.GetUser(ctx).HouseholdID()`.
- Nutrition rules (portion limits, protein and carbohydrate targets) are
  each person's own. The app checks plans against them and warns. From a
  person's profile (body, activity and goal) it recommends daily energy and
  macronutrients with standard formulas, but they only become targets when
  the user adopts them; it never sets rules or targets itself.
- Keep the free tier in mind: the server runs as at most one small Cloud Run
  instance (512 MiB, 1 CPU) that scales to zero, and every database round trip
  costs about 28 ms in production.
