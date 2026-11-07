# foodplann.ing

A vegan meal planner for a household: recipes, weekly meal plans, a pantry
and a shopping list, with nutrition checked against each person's own
targets, and recipe suggestions that share ingredients across the week.

Everything is also exposed as an [MCP](https://modelcontextprotocol.io) server,
so you can bring your own AI agent to enter recipes and plan with you.

See [docs/design.md](docs/design.md) for the design and the milestones. It's
built on the same stack as [trail-tools](https://github.com/johanbrandhorst/trail-tools).

## Status

Milestone 1, the skeleton: invite-only sign in with passkeys, households, the
settings page, and the MCP server with OAuth and API tokens. Foods, recipes,
plans, the pantry and the shopping list come next.

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

Not yet: it will run on Cloud Run in a new Google Cloud project, with Neon
Postgres, deployed with Terraform like trail-tools.
