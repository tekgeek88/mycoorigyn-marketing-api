# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]
### Added

### Changed

### Fixed

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
