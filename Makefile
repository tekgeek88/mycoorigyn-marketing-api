ifneq ("$(wildcard .env)","")
include .env
export
endif

ENV ?= development
APP_LISTEN_PORT ?= 8080
DB_LISTEN_PORT ?= 5432

define first_free_port
$(shell p=$(1); while true; do if lsof -nP -iTCP:$$p -sTCP:LISTEN >/dev/null 2>&1; then p=$$((p + 1)); else echo $$p; break; fi; done)
endef

APP_LISTEN_PORT_EFFECTIVE := $(call first_free_port,$(APP_LISTEN_PORT))
DB_LISTEN_PORT_EFFECTIVE := $(call first_free_port,$(DB_LISTEN_PORT))

PORT ?= $(APP_LISTEN_PORT_EFFECTIVE)
APP_PORT ?= $(PORT)
POSTGRES_PORT ?= $(DB_LISTEN_PORT_EFFECTIVE)
DB_NAME ?= mycoorigyn_marketing
DB_HOST ?= localhost
DB_PORT ?= $(POSTGRES_PORT)
DB_USER ?= mycoorigyn
DB_PASSWORD ?= mycoorigyn
DB_SSLMODE ?= disable
DATABASE_URL ?= postgres://$(DB_USER):$(DB_PASSWORD)@$(DB_HOST):$(DB_PORT)/$(DB_NAME)?sslmode=$(DB_SSLMODE)

.PHONY: test vet build run migrate-up migrate-down docker-build start stop up-with-migrations print-db-url print-ports

test:
	GOCACHE=$(CURDIR)/.gocache go test ./...

vet:
	GOCACHE=$(CURDIR)/.gocache go vet ./...

build:
	GOCACHE=$(CURDIR)/.gocache go build ./cmd/server

run:
	ENV=$(ENV) PORT=$(PORT) DB_NAME="$(DB_NAME)" DB_HOST="$(DB_HOST)" DB_PORT="$(DB_PORT)" DB_USER="$(DB_USER)" DB_PASSWORD="$(DB_PASSWORD)" DB_SSLMODE="$(DB_SSLMODE)" DATABASE_URL="$(DATABASE_URL)" GOCACHE=$(CURDIR)/.gocache go run ./cmd/server

start:
	@echo "Starting marketing API on :$(APP_PORT), postgres on host :$(POSTGRES_PORT)"
	APP_PORT=$(APP_PORT) POSTGRES_PORT=$(POSTGRES_PORT) docker compose up -d --build

up-with-migrations:
	@echo "Starting marketing API on :$(APP_PORT), postgres on host :$(POSTGRES_PORT)"
	APP_PORT=$(APP_PORT) POSTGRES_PORT=$(POSTGRES_PORT) docker compose up -d --build
	$(MAKE) migrate-up POSTGRES_PORT="$(POSTGRES_PORT)" DB_PORT="$(DB_PORT)" DATABASE_URL="$(DATABASE_URL)"

stop:
	docker compose down

migrate-up:
	migrate -path migrations -database "$(DATABASE_URL)" up

migrate-down:
	migrate -path migrations -database "$(DATABASE_URL)" down 1

print-db-url:
	@echo "DATABASE_URL = $(DATABASE_URL)"

print-ports:
	@echo "APP_LISTEN_PORT = $(PORT)"
	@echo "APP_PORT = $(APP_PORT)"
	@echo "DB_LISTEN_PORT = $(DB_LISTEN_PORT_EFFECTIVE)"
	@echo "POSTGRES_PORT = $(POSTGRES_PORT)"
	@echo "DB_PORT = $(DB_PORT)"

docker-build:
	docker build -t mycoorigyn-marketing-api .
