# mycoorigyn-marketing-api

`mycoorigyn-marketing-api` is a small Go and PostgreSQL service for public marketing-site submissions to the MycoOrigyn website.

## What this service is

- A standalone backend for public waitlist and early-access submissions.
- Company-owned marketing infrastructure for `mycoorigyn.com`.
- A simple JSON API with PostgreSQL persistence.

## What this service is not

- It is not part of the customer-installable MycoOrigyn product monorepo.
- It does not contain customer production workflows, product data models, authentication, application users, role permissions, or frontend code.
- It does not manage cultures, spawn, grow batches, monotubs, harvests, drying loads, inventory, labels, holds, incidents, or customer organizations.

## Architecture Note

This repository is intentionally separate from the customer-installable MycoOrigyn product. Customers deploy the product repo in their own environments. This service is company-owned infrastructure used by the public marketing site to collect waitlist and early-access interest only.

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

## Environment Variables

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `ENV` | No | `development` | Runtime environment label. Dotenv loading is skipped in `staging` and `production`. |
| `PORT` | No | `8080` | HTTP port used by the API process itself. |
| `APP_PORT` | No | `8080` | Host port published by Docker Compose for local access to the app container. |
| `POSTGRES_PORT` | No | `5432` | Host port used for local PostgreSQL access from shell-based tools like `make migrate-up` and as the local default for `DB_PORT`. |
| `DATABASE_URL` | Conditionally | none | PostgreSQL connection string. Preferred when provided. |
| `DB_NAME` | Conditionally | none | Database name when using split DB config instead of `DATABASE_URL`. |
| `DB_HOST` | Conditionally | none | Database host when using split DB config instead of `DATABASE_URL`. |
| `DB_PORT` | Conditionally | `POSTGRES_PORT` or `5432` | Database port when using split DB config instead of `DATABASE_URL`. |
| `DB_USER` | Conditionally | none | Database user when using split DB config instead of `DATABASE_URL`. |
| `DB_PASSWORD` | Conditionally | none | Database password when using split DB config instead of `DATABASE_URL`. |
| `DB_SSLMODE` | No | `disable` | PostgreSQL SSL mode for split DB config. Useful for cluster deployments. |
| `PUBLIC_CORS_ALLOWED_ORIGINS` | No | `http://localhost:5173` | Comma-separated allowed origins for `/public/*` routes. |
| `READ_TIMEOUT_SECONDS` | No | `10` | HTTP server read timeout in seconds. |
| `WRITE_TIMEOUT_SECONDS` | No | `10` | HTTP server write timeout in seconds. |
| `IDLE_TIMEOUT_SECONDS` | No | `60` | HTTP server idle timeout in seconds. |
| `SHUTDOWN_TIMEOUT_SECONDS` | No | `10` | Graceful shutdown timeout in seconds. |

The app accepts either:

- `DATABASE_URL`
- or all of `DB_NAME`, `DB_HOST`, `DB_PORT`, `DB_USER`, and `DB_PASSWORD`

The preferred K8s-style configuration is the split `DB_*` set, with `DATABASE_URL` kept as an override for platforms that inject a single DSN. For local development, `DB_PORT` defaults to `POSTGRES_PORT`, so changing `POSTGRES_PORT` is usually enough.

## Database Migrations

Migration files live in `migrations/` and are compatible with `golang-migrate`.

Using the `migrate` CLI:

```sh
make migrate-up
make migrate-down
```

If `DATABASE_URL` is not set explicitly, the `Makefile` derives a local default from `DB_*` variables. `DB_PORT` defaults to `POSTGRES_PORT`.

```sh
POSTGRES_PORT=55432 docker compose up -d postgres
POSTGRES_PORT=55432 make migrate-up
```

You can inspect the derived value with:

```sh
make print-db-url
```

If you do not have `migrate` installed, install it separately before using the make targets.

Using `psql` directly:

```sh
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f migrations/000001_create_early_access_submissions.up.sql
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f migrations/000001_create_early_access_submissions.down.sql
```

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

## CORS

CORS is applied only to `/public/*` routes.

- Allowed origins are configured with `PUBLIC_CORS_ALLOWED_ORIGINS`.
- Allowed origins receive `Access-Control-Allow-Origin`.
- Disallowed origins do not receive `Access-Control-Allow-Origin`.
- `POST` and `OPTIONS` are allowed for `/public/early-access`.
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
  -e ENV=staging \
  -e PORT=8080 \
  -e DB_NAME='mycoorigyn_marketing' \
  -e DB_HOST='host.docker.internal' \
  -e DB_PORT='5432' \
  -e DB_USER='mycoorigyn' \
  -e DB_PASSWORD='mycoorigyn' \
  -e DB_SSLMODE='disable' \
  -e PUBLIC_CORS_ALLOWED_ORIGINS='https://mycoorigyn.com' \
  mycoorigyn-marketing-api
```

## Docker Compose

The included `docker-compose.yml` is for local development only.

```sh
docker compose up --build
```

This starts:

- `postgres` on port `5432` by default
- `app` on host port `8080` by default

Run the migration after the database is ready.

If ports are already in use locally, override the published host ports:

```sh
APP_PORT=18080 POSTGRES_PORT=55432 docker compose up --build
```

The same `POSTGRES_PORT` value can be reused with `make migrate-up`, `make migrate-down`, and `make run`.

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

## Deployment Notes

- Run migrations before serving traffic from a new release.
- Provide `DATABASE_URL` through your deployment secret store.
- Set `PUBLIC_CORS_ALLOWED_ORIGINS` to the production marketing-site origins only.
- This service is suitable for deployment behind a load balancer, reverse proxy, or Kubernetes ingress.
- A static marketing site can call this API directly as long as the site origin is included in `PUBLIC_CORS_ALLOWED_ORIGINS`.

## Security Notes

- The public submission endpoint is intentionally unauthenticated.
- Request bodies are limited to 32 KiB.
- JSON decoding rejects malformed payloads and unknown fields.
- The honeypot field must be empty.
- Emails are normalized and validated server-side.
- Duplicate handling uses a normalized payload fingerprint with a recent duplicate window.
- Full request bodies are not logged.
- Public responses do not expose database errors or stack traces.

## Current Limitations

- No admin UI yet.
- No email notification yet.
- No rate limiting yet.
- No CAPTCHA yet.
- No CRM/newsletter integration yet.
