# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]
### Added

### Changed

### Fixed

--------------------------------

## [0.0.5] - 2026-08-22
### Added
- Added the closed-alpha early-access approval workflow with durable pending, approved, and declined states.
- Added Resend-backed reviewer notifications and applicant approval emails in HTML and plain text.
- Added public review endpoints for resolving, approving, and declining early-access applications.
- Added authenticated server-to-server signup-grant endpoints for validation, claiming, consumption, and release.
- Added email-bound, expiring signup grants with idempotent claim leases to protect against concurrent provisioning.
- Added a private file-backed capability-token store while retaining only SHA-256 token digests and opaque references in PostgreSQL.
- Added migration `000003` for approval state, review capabilities, signup grants, expiration, and claim tracking.
- Added closed-alpha email, token-storage, provisioning-secret, URL, and TTL configuration options.
- Added unit, HTTP, Resend, token-store, and opt-in PostgreSQL integration coverage for the new workflow.
- Added operational documentation for closed-alpha approvals, migrations, staging, and releases.

### Changed
- Changed new `early_access` submissions to create one review capability and attempt one reviewer notification; `waitlist` submissions remain outside the approval workflow.
- Changed safe duplicate handling to reuse the existing application without generating another review notification.
- Changed staging and production startup validation to require Resend, HTTPS review/signup URLs, a durable token root, and a private provisioning shared-secret file.
- Updated the README with the v0.0.5 architecture, configuration, API, migration, deployment, security, and testing contracts.

### Fixed
- Removed tracked `.DS_Store` files and added repository-wide ignore rules for macOS metadata and local protected-token files.

### Security
- Added fragment-based review and signup links so capability tokens are not placed in URL query strings.
- Added constant-time provisioning bearer-secret checks and private-file validation for Resend and provisioning credentials.
- Added bounded, idempotent grant claims so concurrent workers cannot provision multiple hosted farms from one approval.
- Added `Cache-Control: no-store` to review and signup-grant responses.

--------------------------------

## [0.0.4] - 2026-07-11
### Added
- Added visitor counter.

--------------------------------

## [0.0.3] - 2026-07-11
### Added
- Added split `DB_*` database configuration support alongside `DATABASE_URL`.
- Added local dotenv loading from `.env` outside `staging` and `production`.
- Added `APP_PORT` support for Docker Compose host port publishing.
- Added `print-db-url` Makefile target for inspecting the derived local database URL.

### Changed
- Changed local development to center on `.env` instead of `.env.development`.
- Changed Docker Compose to publish the app on `APP_PORT` while keeping the container listen port at `8080`.
- Changed local database port derivation so `DB_PORT` defaults from `POSTGRES_PORT`.
- Changed the Docker build to compile `./cmd/server` directly in a multi-stage image build.

### Fixed
- Fixed `.gitignore` so `cmd/server/main.go` is tracked and available in CI release builds.
- Fixed Docker release builds that failed because the old Dockerfile expected an Ironlytic-style binary output path.
- Fixed local Docker Compose conflicts by separating app host port publishing from the internal app `PORT`.

--------------------------------

## [0.0.2] - 2026-07-11
### Fixed
- Fixed deployment action

--------------------------------

## [0.0.0] - 2026-07-11
### Added
- Initial release
