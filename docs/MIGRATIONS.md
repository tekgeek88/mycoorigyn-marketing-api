# 📦 Database Migrations

This project uses [golang-migrate](https://github.com/golang-migrate/migrate) to manage schema changes for the PostgreSQL database.

## 🗂 Folder Structure

All migration files live in the `migrations/` directory and follow this format:
```
migrations/
├── 000001_create_users_table.up.sql
└── 000001_create_users_table.down.sql
```


Each migration must include both an `.up.sql` (apply) and `.down.sql` (revert) file.

---

## 🚀 Installing the CLI

Install `migrate` CLI:

```bash
  brew install golang-migrate
```

#### Or install from github
```bash
  curl -L https://github.com/golang-migrate/migrate/releases/latest/download/migrate.linux-amd64.tar.gz | tar xvz
  sudo mv migrate /usr/local/bin
```

### 2. 🐳 Find Your Postgres Container
#### Look for the container running Postgres (e.g., qme_postgres).
```bash 
docker ps
```

### 3. 💡 Create a Migration
```bash
  migrate create -ext sql -dir migrations -seq create_users_table
```

This creates:
```
migrations/
  000001_create_users_table.up.sql
  000001_create_users_table.down.sql
```

#### Or you can use the Makefile shortcuts
##### Non dev migrate commands will affect all environments
Dev migrate commands are for seed data needed for dev and test environments

- make migrate-up
- make migrate-down
- make migrate-new
- make seed
- make reset-db

### 4. 🔌 Run a Migration (inside Docker)
#### Option A: Use the container hostname (postgres, db, etc.)
```bash
  migrate -path ./migrations \
    -database "postgres://qme_development_user:qme_password@localhost:5432/qme_development?sslmode=disable" up
```

### 5. To roll back one migration:
#### `down 1` signifies migration should only be rolled back by one migration
```bash
  migrate -path ./migrations \
    -database "postgres://qme_development_user:qme_password@localhost:5432/qme_development?sslmode=disable" down 1
```

✅ Tips
- Never edit applied migrations — create new files for changes.
- The migration history is stored in a _migrations table.
- Always test .down.sql logic locally before using in prod.

### 🔍 Verifying Migration Status
#### This shows the current schema version and dirty state.
```bash
  migrate -path ./migrations -database "postgres://qme_development_user:qme_password@localhost:5432/qme_development?sslmode=disable" version
```

### Generate seed data for your dev environment
```bash
make seed
```

### To reset your data in the dev environment
```bash
make reset-database
```
