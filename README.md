# mycoorigyn-marketing-api

`mycoorigyn-marketing-api` is a Go and PostgreSQL service for MycoOrigyn marketing-site submissions, visitor counts, and the closed-alpha early-access approval flow.

## What this service is

- A standalone backend for public waitlist and early-access submissions.
- Company-owned marketing infrastructure for `mycoorigyn.com`.
- The approval gate that lets an operator review an early-access application and issue a one-time hosted-signup grant.
- A JSON API with PostgreSQL persistence, Resend transactional email delivery, and protected capability-token storage.

## What this service is not

- It is not part of the customer-installable MycoOrigyn product monorepo.
- It does not contain customer production workflows, product data models, application users, role permissions, or frontend code.
- It does not manage cultures, spawn, grow batches, monotubs, harvests, drying loads, inventory, labels, holds, incidents, or customer organizations.
- It does not create tenants, databases, platform users, memberships, passwords, sessions, subscriptions, or billing records. The main MycoOrigyn application remains responsible for provisioning.

## Architecture Note

This repository is intentionally separate from the customer-installable MycoOrigyn product. Customers deploy the product repo in their own environments. This service is company-owned infrastructure used by the public marketing site to collect interest and authorize one hosted-farm signup after an operator approves a closed-alpha application.

## Release 0.0.5

Release 0.0.5 adds the closed-alpha approval and signup-grant foundation delivered in [PR #7](https://github.com/tekgeek88/mycoorigyn-marketing-api/pull/7):

- New `early_access` submissions enter a durable `pending` approval state and trigger a reviewer email through Resend. Waitlist submissions remain outside the approval flow.
- Reviewers can securely resolve, approve, or decline applications with short-lived capability tokens passed in URL fragments.
- Approval creates one email-bound signup grant and sends the applicant a hosted-signup link. Approval and email retry behavior is idempotent.
- An authenticated server-to-server API supports grant validation, claiming, consumption, and release so concurrent provisioning attempts cannot create multiple farms.
- PostgreSQL migration `000003` adds approval state, review capabilities, signup grants, expiration, and claim leasing.
- Plaintext capabilities are kept in a private file-backed token store; PostgreSQL stores only SHA-256 digests and opaque file references.
- Unit, HTTP, email, token-store, and opt-in PostgreSQL integration tests cover approval retries and concurrent grant claims.
- New operational guides cover the closed-alpha contract, migrations, staging, and the release process.
- `.DS_Store` and local token files are excluded from Git.

## Local Development

1. Copy the example environment file.
2. Start PostgreSQL.
3. Run the migration.
4. Start the API.

```sh
cp .env.example .env
set -a; . ./.env; set +a
docker compose up -d postgres
make migrate-up
make run
```

The service listens on `http://localhost:8080` by default.
For local Docker Compose usage, `APP_PORT` controls the host port and `PORT` remains the app's internal listen port.
For local development, the config package loads `.env`. In `staging` and `production`, dotenv loading is skipped so container and cluster environments rely on their injected environment variables directly.
The `Makefile` follows the same local pattern and automatically includes `.env` if that file exists.

Local development defaults `MARKETING_EMAIL_PROVIDER` to `disabled` and stores protected review and signup tokens below `.local/marketing-tokens`. Submissions and approval state can be exercised locally, but no reviewer or applicant email is delivered unless an email sender is configured.

## Environment Variables

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `ENV` | No | `development` | Runtime environment label. Dotenv loading is skipped in `staging` and `production`. |
| `APP_LISTEN_PORT` | No | `8080` | Single source for host/API port. Controls `PORT` and `APP_PORT`. |
| `DB_LISTEN_PORT` | No | `5432` | Single source for host PostgreSQL port used by `POSTGRES_PORT` and `DB_PORT`. |
| `DATABASE_URL` | Conditionally | none | PostgreSQL connection string. Preferred when provided. |
| `DB_NAME` | Conditionally | none | Database name when using split DB config instead of `DATABASE_URL`. |
| `DB_HOST` | Conditionally | none | Database host when using split DB config instead of `DATABASE_URL`. |
| `DB_PORT` | Conditionally | `DB_LISTEN_PORT` or `5432` | Database port when using split DB config instead of `DATABASE_URL`. |
| `DB_USER` | Conditionally | none | Database user when using split DB config instead of `DATABASE_URL`. |
| `DB_PASSWORD` | Conditionally | none | Database password when using split DB config instead of `DATABASE_URL`. |
| `DB_SSLMODE` | No | `disable` | PostgreSQL SSL mode for split DB config. Useful for cluster deployments. |
| `PUBLIC_CORS_ALLOWED_ORIGINS` | No | `http://localhost:5173` | Comma-separated allowed origins for `/public/*` routes. |
| `READ_TIMEOUT_SECONDS` | No | `10` | HTTP server read timeout in seconds. |
| `WRITE_TIMEOUT_SECONDS` | No | `10` | HTTP server write timeout in seconds. |
| `IDLE_TIMEOUT_SECONDS` | No | `60` | HTTP server idle timeout in seconds. |
| `SHUTDOWN_TIMEOUT_SECONDS` | No | `10` | Graceful shutdown timeout in seconds. |
| `MARKETING_EMAIL_PROVIDER` | Environment-dependent | `disabled` | Transactional email provider. Accepts `disabled` or `resend`; staging and production require `resend`. |
| `MARKETING_EMAIL_FROM` | With Resend | none | Sender used for reviewer and applicant messages. |
| `MARKETING_EMAIL_REPLY_TO` | No | none | Optional reply-to address for transactional email. |
| `MARKETING_RESEND_API_KEY_FILE` | With Resend | none | Path to a private, regular, non-symlink file containing the Resend API key. |
| `MARKETING_EARLY_ACCESS_REVIEW_RECIPIENT` | With Resend | none | Operator address that receives new early-access review notifications. |
| `MARKETING_REVIEW_BASE_URL` | With Resend | none | Frontend review-page URL. Staging and production require HTTPS with no query or fragment. |
| `MYCOORIGYN_HOSTED_SIGNUP_BASE_URL` | With Resend | none | Hosted signup-page URL. Staging and production require HTTPS with no query or fragment. |
| `MARKETING_TOKEN_SECRET_ROOT` | Outside local development | `.local/marketing-tokens` in development/testing | Durable, writable, private directory for recoverable review and signup tokens. |
| `MARKETING_PROVISIONING_SHARED_SECRET_FILE` | In staging/production | none | Path to a private file containing the provisioning API bearer secret; it must be at least 32 bytes on one line. |
| `MARKETING_REVIEW_TOKEN_TTL_SECONDS` | No | `604800` | Review capability lifetime (7 days). |
| `MARKETING_SIGNUP_GRANT_TTL_SECONDS` | No | `604800` | Signup grant lifetime (7 days). |
| `MARKETING_SIGNUP_GRANT_CLAIM_TTL_SECONDS` | No | `1800` | Provisioning claim lease lifetime (30 minutes). |

The app accepts either:

- `DATABASE_URL`
- or all of `DB_NAME`, `DB_HOST`, `DB_PORT`, `DB_USER`, and `DB_PASSWORD`

The preferred K8s-style configuration is the split `DB_*` set, with `DATABASE_URL` kept as an override for platforms that inject a single DSN. For local development, `PORT` and `APP_PORT` default to `APP_LISTEN_PORT`; `DB_PORT` defaults to `DB_LISTEN_PORT`.

See [.env.example](.env.example) for a complete local configuration. Staging and production must mount the Resend and provisioning secrets as private files and use a durable volume for `MARKETING_TOKEN_SECRET_ROOT`.

## Database Migrations

Migration files live in `migrations/` and are compatible with `golang-migrate`.

Using the `migrate` CLI:

```sh
make migrate-up
make migrate-down
```

If `DATABASE_URL` is not set explicitly, the `Makefile` derives a local default from `DB_*` variables. `DB_PORT` defaults to `DB_LISTEN_PORT`.

```sh
APP_LISTEN_PORT=18080 DB_LISTEN_PORT=55432 make start
DB_LISTEN_PORT=55432 make migrate-up
```

You can inspect the derived value with:

```sh
make print-db-url
```

If you do not have `migrate` installed, install it separately before using the make targets.

Using `psql` directly:

```sh
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f migrations/000001_create_early_access_submissions.up.sql
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f migrations/000002_create_page_views.up.sql
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f migrations/000003_add_early_access_approvals.up.sql
```

Migration `000003` must be applied before deploying v0.0.5. It adds approval state to early-access submissions and creates the review-capability and signup-grant tables. The normal release workflow publishes a migration image that runs these `golang-migrate` files before the application serves traffic. See [Database Migrations](docs/MIGRATIONS.md) for migration authoring and rollback guidance.

## API Endpoints

### `GET /healthz`

Returns:

```json
{
  "status": "ok"
}
```

### `POST /public/early-access`

Supported submission types:

- `waitlist`
- `early_access`

Accepted submissions and safe duplicates return:

```json
{
  "success": true,
  "message": "Thank you for your interest in MycoOrigyn."
}
```

The public response deliberately does not reveal whether an email already exists or whether an application has been approved. A new `waitlist` submission is stored without entering the review flow. A new `early_access` submission is stored as `pending`, receives a review capability, and triggers one reviewer-notification attempt. Safe duplicates reuse the existing application and do not send another notification. A delivery failure does not discard the stored application.

Structured error responses use this shape:

```json
{
  "error": {
    "code": "validation_error",
    "message": "Please check the form and try again."
  }
}
```

### Request Fields

- `submission_type`: required, `waitlist` or `early_access`
- `email`: required
- `source`: optional, defaults to `marketing_site`
- `website`: optional honeypot, must be empty
- `name`: optional
- `farm_name`: optional
- `farm_type`: optional
- `production_scale`: optional
- `current_tracking_method`: optional
- `features_of_interest`: optional string array
- `interested_in_testing`: optional boolean
- `message`: optional

### `POST /public/visitor-count`

Records a visit for a page and returns updated counters.

```json
{
  "page": "landing",
  "visitor_id": "optional-visitor-id"
}
```

Response:

```json
{
  "page": "landing",
  "total_visits": 1248,
  "unique_visitors": 912
}
```

If `visitor_id` is omitted, the total visit is still counted and the unique count is not incremented.

### `GET /public/visitor-count`

Returns counters for a page.

```
GET /public/visitor-count?page=landing
```

Response:

```json
{
  "page": "landing",
  "total_visits": 1248,
  "unique_visitors": 912
}
```

### Example Waitlist Request

```sh
curl -i http://localhost:8080/public/early-access \
  -H 'Content-Type: application/json' \
  -d '{
    "submission_type": "waitlist",
    "email": "grower@example.com",
    "source": "marketing_site",
    "website": ""
  }'
```

### Example Early-Access Request

```sh
curl -i http://localhost:8080/public/early-access \
  -H 'Content-Type: application/json' \
  -d '{
    "submission_type": "early_access",
    "email": "grower@example.com",
    "name": "Jane Grower",
    "farm_name": "Example Mushroom Farm",
    "farm_type": "Boutique gourmet farm",
    "production_scale": "50-100 lb/week",
    "current_tracking_method": "Spreadsheets and paper logs",
    "features_of_interest": ["Traceability", "Reporting", "Mobile access", "Operational visibility"],
    "interested_in_testing": true,
    "message": "Interested in testing the prototype.",
    "source": "marketing_site",
    "website": ""
  }'
```

### Closed-Alpha Review API

The reviewer frontend uses these public `POST` routes:

| Endpoint | Purpose |
| --- | --- |
| `/public/early-access/review/resolve` | Resolve an active review capability and return the application projection. |
| `/public/early-access/review/approve` | Approve the application, create or reuse its signup grant, and email the applicant. |
| `/public/early-access/review/decline` | Decline the application without creating a signup grant. |

Each route accepts the same body:

```json
{
  "token": "review-capability-token"
}
```

Review links use `<MARKETING_REVIEW_BASE_URL>#token=<token>`. The frontend must immediately remove the fragment from browser history and keep the token only in memory. No `GET` endpoint resolves or changes a review decision.

Approval returns `{"status":"approved","delivery_status":"delivered"}`. If applicant email delivery fails after the decision commits, the API returns `503 approval_delivery_failed`; retrying the same approval with the same in-memory token reuses the durable grant. Decline returns `{"status":"declined"}`. Repeating the same terminal decision is idempotent, while trying the opposite decision returns a conflict.

### Provisioning Signup-Grant API

The provisioning service uses these internal `POST` routes:

| Endpoint | Purpose |
| --- | --- |
| `/internal/signup-grants/validate` | Verify the token, normalized approved email, status, and expiration without reserving it. |
| `/internal/signup-grants/claim` | Reserve an active grant for a stable provisioning operation reference. |
| `/internal/signup-grants/consume` | Consume the matching claim after provisioning commits successfully. |
| `/internal/signup-grants/release` | Release the matching claim after a pre-commit provisioning failure. |

All internal grant routes require `Authorization: Bearer <provisioning-shared-secret>` and `Content-Type: application/json`. Validate accepts `token` and `email`; claim, consume, and release additionally require a stable `claim_reference`:

```json
{
  "token": "signup-grant-token",
  "email": "approved@example.com",
  "claim_reference": "stable-provisioning-operation-reference"
}
```

The intended lifecycle is `validate` → `claim` → provision and durably commit → `consume`. On failure before the commit boundary, call `release` and retry later. The claim lease and stable operation reference make retries idempotent and prevent concurrent workers from independently provisioning the same grant.

For the full frontend handoff, error, expiration, and retry contracts, see [Closed-alpha early-access approval and signup grants](docs/closed-alpha-approval.md).

## CORS

CORS is applied only to `/public/*` routes.

- Allowed origins are configured with `PUBLIC_CORS_ALLOWED_ORIGINS`.
- Allowed origins receive `Access-Control-Allow-Origin`.
- Disallowed origins do not receive `Access-Control-Allow-Origin`.
- `GET`, `POST`, and `OPTIONS` are allowed for `/public/visitor-count`.
- `POST` and `OPTIONS` are allowed for `/public/early-access`.
- `POST` and `OPTIONS` are allowed for the three `/public/early-access/review/*` routes.
- `Content-Type` is allowed.
- Wildcard origins are not used.

Example local value:

```sh
PUBLIC_CORS_ALLOWED_ORIGINS=http://localhost:5173,http://localhost:5174
```

Example production value:

```sh
PUBLIC_CORS_ALLOWED_ORIGINS=https://mycoorigyn.com,https://www.mycoorigyn.com
```

## Docker

Build the image:

```sh
make docker-build
```

Run the image manually:

```sh
docker run --rm -p 8080:8080 \
  -e ENV=development \
  -e PORT=8080 \
  -e DB_NAME='mycoorigyn_marketing' \
  -e DB_HOST='host.docker.internal' \
  -e DB_PORT='5432' \
  -e DB_USER='mycoorigyn' \
  -e DB_PASSWORD='mycoorigyn' \
  -e DB_SSLMODE='disable' \
  -e PUBLIC_CORS_ALLOWED_ORIGINS='http://localhost:5173' \
  mycoorigyn-marketing-api
```

This development example leaves transactional email disabled and uses the default container-local token directory. A staging or production container must instead receive the complete closed-alpha configuration and durable/private mounts described above.

## Docker Compose

The included `docker-compose.yml` is for local development only.

```sh
docker compose up --build
```

This starts:

- `postgres` on port from `DB_LISTEN_PORT` by default (`5432`)
- `app` on host port from `APP_LISTEN_PORT` by default (`8080`)

Run the migration after the database is ready.

If ports are already in use locally, override the published host ports:

```sh
APP_LISTEN_PORT=18080 DB_LISTEN_PORT=55432 make start
```

The same `DB_LISTEN_PORT` value can be reused with `make migrate-up`, `make migrate-down`, and `make run`.

You can also run compose + migrations in one command:

```sh
make up-with-migrations
```

## Test Commands

```sh
gofmt -w ./cmd ./internal
GOCACHE=$(pwd)/.gocache go test ./...
GOCACHE=$(pwd)/.gocache go vet ./...
GOCACHE=$(pwd)/.gocache go build ./cmd/server
```

Or use the make targets:

```sh
make test
make vet
make build
```

The PostgreSQL approval integration tests are opt-in and require a migrated disposable database:

```sh
MARKETING_OPERATIONAL_TEST=1 \
TEST_DATABASE_URL='postgres://user:password@localhost:5432/mycoorigyn_marketing_test?sslmode=disable' \
GOCACHE=$(pwd)/.gocache go test ./internal/postgres
```

## Deployment Notes

- Run migrations before serving traffic from a new release.
- Provide `DATABASE_URL` or the split `DB_*` values through your deployment configuration and secret store.
- Set `PUBLIC_CORS_ALLOWED_ORIGINS` to the production marketing-site origins only.
- Configure Resend, the reviewer and signup HTTPS URLs, a durable private token volume, and both private secret files before starting in `staging` or `production`.
- Deliver migration `000003` through the existing migration image and release workflow; do not introduce a separate deployment path for the approval feature.
- This service is suitable for deployment behind a load balancer, reverse proxy, or Kubernetes ingress.
- A static marketing site can call this API directly as long as the site origin is included in `PUBLIC_CORS_ALLOWED_ORIGINS`.

## Security Notes

- The public submission endpoint is intentionally unauthenticated.
- Request bodies are limited to 32 KiB.
- JSON decoding rejects malformed payloads and unknown fields.
- The honeypot field must be empty.
- Emails are normalized and validated server-side.
- Duplicate handling uses a normalized payload fingerprint with a recent duplicate window.
- Review and signup tokens are high-entropy capabilities transported in URL fragments, not query strings.
- PostgreSQL stores token digests and opaque references; recoverable plaintext tokens are written atomically to private files below `MARKETING_TOKEN_SECRET_ROOT`.
- Internal signup-grant routes require a bearer secret loaded from a private file and compare it in constant time.
- Grant claims use bounded leases and hashed stable operation references to prevent concurrent provisioning replays.
- Review and grant responses set `Cache-Control: no-store`.
- Full request bodies are not logged.
- Public responses do not expose database errors or stack traces.

## Current Limitations

- No built-in reviewer/admin UI; the marketing frontend must implement the review-page contract.
- The marketing API authorizes signup but does not provision hosted farms itself.
- No rate limiting yet.
- No CAPTCHA yet.
- No CRM/newsletter integration yet.

## Operations Documentation

- [Closed-alpha approval and signup grants](docs/closed-alpha-approval.md) — security boundaries, frontend handoff, retries, and provisioning contract.
- [Database migrations](docs/MIGRATIONS.md) — creating, applying, verifying, and rolling back migrations.
- [Staging](docs/STAGING.md) — staging environment and deployment workflow.
- [Release process](docs/RELEASE.md) — release branches, production PRs, tags, and hotfixes.
- [Changelog](CHANGELOG.md) — version-by-version release notes.
