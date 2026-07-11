ifneq ("$(wildcard .env)","")
include .env
export
endif

ENV ?= development
PORT ?= 8080
POSTGRES_PORT ?= 5432
DB_NAME ?= mycoorigyn_marketing
DB_HOST ?= localhost
DB_PORT ?= $(POSTGRES_PORT)
DB_USER ?= mycoorigyn
DB_PASSWORD ?= mycoorigyn
DB_SSLMODE ?= disable
DATABASE_URL ?= postgres://$(DB_USER):$(DB_PASSWORD)@$(DB_HOST):$(DB_PORT)/$(DB_NAME)?sslmode=$(DB_SSLMODE)

.PHONY: test vet build run migrate-up migrate-down docker-build print-db-url

test:
	GOCACHE=$(CURDIR)/.gocache go test ./...

vet:
	GOCACHE=$(CURDIR)/.gocache go vet ./...

build:
	GOCACHE=$(CURDIR)/.gocache go build ./cmd/server

run:
	ENV=$(ENV) PORT=$(PORT) DB_NAME="$(DB_NAME)" DB_HOST="$(DB_HOST)" DB_PORT="$(DB_PORT)" DB_USER="$(DB_USER)" DB_PASSWORD="$(DB_PASSWORD)" DB_SSLMODE="$(DB_SSLMODE)" DATABASE_URL="$(DATABASE_URL)" GOCACHE=$(CURDIR)/.gocache go run ./cmd/server

migrate-up:
	migrate -path migrations -database "$(DATABASE_URL)" up

migrate-down:
	migrate -path migrations -database "$(DATABASE_URL)" down 1

print-db-url:
	@echo "DATABASE_URL = $(DATABASE_URL)"

docker-build:
	docker build -t mycoorigyn-marketing-api .
