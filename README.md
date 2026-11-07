# foodplann.ing

A vegan meal planner for a household: recipes, weekly meal plans, a pantry
and a shopping list, with nutrition checked against each person's own
targets, and recipe suggestions that share ingredients across the week.

Everything is also exposed as an [MCP](https://modelcontextprotocol.io) server,
so you can bring your own AI agent to enter recipes and plan with you.

See [docs/design.md](docs/design.md) for the design and the milestones. It's
built on the same stack as [trail-tools](https://github.com/johanbrandhorst/trail-tools).

## Status

Version 1 is done, milestones 1 to 5:

- Invite-only sign in with passkeys, households, the settings page, and the
  MCP server with OAuth and API tokens.
- Foods from USDA FoodData Central or entered by hand, and recipes with
  their nutrition per serving.
- The people you plan for, with their eating patterns and their care team's
  rules, and weekly plans of shared meals, leftovers and staples, checked
  against those rules.
- The pantry, which cooks marked done take from, and a shared shopping list
  for one shop a week, by store and aisle, which puts what's bought into the
  pantry.
- Recipe suggestions for each open meal or small meal of the week, explained
  point by point: the rest of a can another meal opens, what's in the pantry
  and going off, ingredients shared with the week's other meals, everyone's
  nutrition targets and rules, what keeps until the day of the meal from a
  single weekly shop, flavor pairings and variety. Your AI agent gets the
  same suggestions, and can propose new recipes beyond them.
- A home page with the week at a glance: today's meals, the shopping list,
  and what to use soon.

## Prerequisites

- Go
- Docker or [Apple `container`](https://github.com/apple/container)

## Running

To start Postgres and run the server locally, use:

```sh
$ make run
```

On a database without an administrator, opening http://localhost:8080 on the
same machine goes to the invitation to create the administrator account with a
passkey (`make watch` works the same way). It's
http://localhost:8080/invite#local-admin, if you need the link. The first
administrator starts a household called "Home", which you can rename under
Settings.

foodplann.ing is invite only. Invite the other people in your household, or
yourself to add a passkey on another device, from the settings page. People
who accept an invitation join the household of whoever invited them, and
share its recipes, plans, pantry and shopping list.

`make run` and `make watch` also let browsers and agents on your machine sign
in without a passkey, as the local administrator, at
http://localhost:8080/local-sign-in.

### Foods from USDA FoodData Central

Households create their foods from USDA FoodData Central (FDC), so their
nutrients are filled in, or enter them by hand. To make FDC's foods
available, download the **Foundation Foods** and **SR Legacy** datasets as
CSV from https://fdc.nal.usda.gov/download-datasets, and import them into
the local database, as the zip files or extracted:

```sh
$ make import-fdc FDC="FoodData_Central_foundation_food_csv_2025-04-24.zip FoodData_Central_sr_legacy_food_csv_2018-04.zip"
```

That runs `foodplanning import-fdc -database-url <url> <dataset>...`, which
also imports into other databases. It skips the categories of meat, fish,
dairy and eggs (see `excludedCategories` in `internal/fdc`), and importing
again, e.g. a newer release, updates the foods. The Branded dataset is too
big for the free tier: enter branded products as custom foods, from their
labels.

### AI agents (MCP)

The MCP server is at `<public URL>/mcp`, e.g. `http://localhost:8080/mcp`
(streamable HTTP). Clients connect with OAuth or an API token.

#### claude.ai, Claude Desktop and other OAuth clients

Custom connectors in claude.ai and Claude Desktop, ChatGPT and other remote MCP
clients sign in with OAuth:

1. In claude.ai, open Settings → Connectors → Add custom connector, and enter
   `https://<your domain>/mcp` as the URL. Leave the OAuth client ID and
   secret empty: clients register themselves.
2. Connect. You're sent to foodplann.ing, where you sign in with your passkey
   and approve the connection.
3. The connector is now available in claude.ai, and in the Claude apps signed
   in to the same account.

Connected apps are listed under Settings, where you can disconnect them. Their
access tokens last 30 days and are refreshed automatically; an app that isn't
used for 90 days needs to be connected again.

claude.ai connects from Anthropic's servers, so the server must be reachable
from the internet over HTTPS, and `-public-url` (`FOODPLANNING_PUBLIC_URL`)
must be the URL you enter, as it's the OAuth issuer and resource.

How it works: `/mcp` answers unauthenticated requests with a
`WWW-Authenticate` header pointing at the protected resource metadata
(`/.well-known/oauth-protected-resource/mcp`), which points at the
authorization server metadata (`/.well-known/oauth-authorization-server`).
Clients register dynamically at `/oauth/register`, without anything being
stored: the registration is encoded in the client ID. Only redirect URIs of
known clients are accepted: loopback addresses, HTTPS on an allowlist
(claude.ai, claude.com, chatgpt.com, vscode.dev) and a few desktop app schemes
(see `internal/oauth`). `/oauth/authorize` requires PKCE (S256) and shows a
consent page, and `/oauth/token` issues opaque access tokens and rotating
refresh tokens, stored hashed.

#### API tokens

Clients that support custom headers can use an API token instead. Create one
under Settings, then for example in Claude Code:

```sh
$ claude mcp add --transport http foodplanning http://localhost:8080/mcp --header "Authorization: Bearer <token>"
```

Without the header, Claude Code connects with OAuth instead.

#### Tools

The MCP tools are generated from the Connect API at startup: every RPC is a
tool with the same name, its input schema comes from the request message and
its description from the proto comments. Methods annotated with
`option (mcp.v1.method).exclude = true` (e.g. API token management) are not
exposed. Service comments are sent to the agent as the server's instructions,
so the proto comments are the agent's documentation; write them for an agent.

## Development

To run the server with auto-rebuild of the backend, frontend and CSS, run:

```sh
$ make watch
```

To run the tests (starts Postgres; each test gets its own database):

```sh
$ make test
```

To view available make targets, run:

```sh
$ make help
```

Settings can also be given as environment variables named after the flags,
e.g. `FOODPLANNING_DATABASE_URL` for `-database-url`.

### Tracing

The server is traced with [OpenTelemetry](https://opentelemetry.io): RPCs are
named by their procedure, e.g. `/users.v1.UserService/GetHousehold`, database
queries by their sqlc query, and requests it makes by their host.

Locally, the server sends traces over OTLP to `http://localhost:4317`, and
quietly drops them if nothing is listening. To look at them, start Grafana
with [Tempo](https://grafana.com/oss/tempo/), then run the server as usual
and open http://localhost:3000:

```sh
$ make grafana
$ make watch
```

To send traces elsewhere, set the OTLP gRPC endpoint. To turn tracing off,
set it to nothing:

```sh
$ FOODPLANNING_OTLP_ENDPOINT=http://collector.example:4317 make run
$ FOODPLANNING_OTLP_ENDPOINT= make run
```

`make clean` stops Grafana along with Postgres.

### Adding an RPC

1. Add the RPC to the service in `api/<domain>/v1/*.proto`, with comments
   describing it for both humans and agents.
2. Run `make gen`. This generates the Go and TypeScript code and the embedded
   descriptor set used for the MCP tool descriptions.
3. Implement the method in `internal/services/<domain>`. It's automatically
   exposed over MCP.

## Deploying

foodplann.ing runs on [Cloud Run](https://cloud.google.com/run) with a
[Neon](https://neon.com) serverless Postgres database, sized to stay within
the free tiers for personal use: Cloud Run scales to zero, runs at most one
instance and only uses CPU during requests, and Neon suspends the database
when it's idle. Both are in `us-west1`, as every database round trip
crosses between them.

The Google Cloud configuration is managed with Terraform in
[`infra/`](infra), with its state in a Cloud Storage bucket. Pushes to `main`
are deployed by the `deploy` job in [CI](.github/workflows/ci.yml) once all
checks pass: it builds the image with [ko](https://ko.build) and runs
`terraform apply` with it. Pull requests that change `infra/` get a comment
with the Terraform plan, from the [Terraform plan](.github/workflows/terraform.yml)
workflow.

GitHub Actions authenticates to Google Cloud with Workload Identity
Federation, so there are no stored credentials: pushes to `main` as the
`github-terraform` service account, which can apply changes, and pull
requests as the read-only `github-terraform-plan` service account. Both
workflows read the project from the repository variables `GCP_PROJECT_ID`
and `GCP_PROJECT_NUMBER`, and are skipped until they're set.

To plan or apply changes locally, with your gcloud credentials:

```sh
$ make infra-plan
$ make infra-apply
```

Both use the Terraform version CI applies with, the `terraform` stage in
[`images/Dockerfile`](images/Dockerfile), downloaded into `.bin` the first
time: Terraform refuses to read state written by a newer version.

### Bootstrapping the project

Once, in order:

1. Create the Google Cloud project and link it to a billing account.
2. Replace the placeholders: the project ID and billing account in
   [`infra/terraform.tfvars`](infra/terraform.tfvars), and the project ID in
   the state bucket's name in [`infra/versions.tf`](infra/versions.tf).
3. Create the state bucket:

   ```sh
   $ gcloud storage buckets create gs://<project>-tfstate --location us-west1 --uniform-bucket-level-access --public-access-prevention
   $ gcloud storage buckets update gs://<project>-tfstate --versioning
   ```

4. Apply without the service, which needs the secrets' values first:

   ```sh
   $ make infra-apply ARGS="-var service_enabled=false"
   ```

5. Let the Terraform service accounts manage and read the budget:

   ```sh
   $ gcloud billing accounts add-iam-policy-binding <billing account> --member serviceAccount:github-terraform@<project>.iam.gserviceaccount.com --role roles/billing.costsManager
   $ gcloud billing accounts add-iam-policy-binding <billing account> --member serviceAccount:github-terraform-plan@<project>.iam.gserviceaccount.com --role roles/billing.viewer
   ```

6. Add the secrets' values, see [Secrets](#secrets).
7. Set the repository variables `GCP_PROJECT_ID` and `GCP_PROJECT_NUMBER`
   under Settings → Secrets and variables → Actions → Variables. They aren't
   secret: the workflows only use them to name the project and its service
   accounts.
8. Merge to `main`, or rerun CI on it: the deploy job builds the image and
   creates the service.
9. Set up the [custom domain](#custom-domain), then create the first
   administrator on it.
10. [Import FoodData Central](#importing-fooddata-central).

### Secrets

Secrets are created by Terraform, but their values are added by hand so they
aren't stored in the Terraform state:

- `database-url`: the direct (not pooled) Neon connection string, as
  migrations need session advisory locks.

  ```sh
  $ pbpaste | gcloud secrets versions add database-url --data-file=- --project <project>
  ```

- `admin-bootstrap-token`: works as an invitation to create the first
  administrator at `/invite#<token>`, until there is one.

  ```sh
  $ openssl rand -hex 24 | gcloud secrets versions add admin-bootstrap-token --data-file=- --project <project>
  $ gcloud secrets versions access latest --secret admin-bootstrap-token --project <project>
  ```

  Passkeys are bound to the domain, so create the administrator on the
  domain you'll use.

### Custom domain

The app is served on `foodplann.ing`, set as `domain` in
[`infra/terraform.tfvars`](infra/terraform.tfvars). To set it up:

1. Verify the domain with `gcloud domains verify foodplann.ing`.
2. Set `domain_enabled = true` in [`infra/terraform.tfvars`](infra/terraform.tfvars)
   and apply it locally with `make infra-apply`. Only a verified owner of the
   domain can map it, and the GitHub service account is not one, so the first
   apply runs with your own credentials. Later applies from CI leave the
   existing mapping alone.
3. Create the DNS records from `make infra-output NAME=domain_records` at the
   domain's registrar. Cloud Run provisions and renews the TLS certificate,
   which takes up to about 15 minutes after the records resolve.

### Importing FoodData Central

The FoodData Central import is a few thousand foods, run once and again when
USDA publishes new data, so it runs from your machine against the database
rather than as a job in the cloud:

```sh
$ go run . import-fdc -database-url '<the database-url secret>' FoodData_Central_foundation_food_csv_*.zip FoodData_Central_sr_legacy_food_csv_*.zip
```

### Avatars

Avatars are stored in the `<project>-avatars` Cloud Storage bucket, as the
file system of a Cloud Run instance doesn't outlive it. It's private, in
`us-west1` within the free tier, and only the service account can read and
write it. Terraform passes it to the service as
`FOODPLANNING_AVATAR_BUCKET`.

### Traces and errors

On Cloud Run, the server sends traces with OTLP to Google Cloud's Telemetry
API, to view in [Cloud Trace](https://console.cloud.google.com/traces), and
reports errors to [Error Reporting](https://console.cloud.google.com/errors),
which can email about new ones. Both are free at the app's traffic.
