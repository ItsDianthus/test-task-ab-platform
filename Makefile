DC = docker compose

ifneq (,$(wildcard .env.secrets))
include .env.secrets
export
endif

POSTGRES_USER ?= lotty
POSTGRES_PASSWORD ?= lotty
POSTGRES_DB ?= lotty

POSTGRES_PORT ?= 5433
DB_HOST ?= localhost
DB_PORT ?= $(POSTGRES_PORT)
DB_SSLMODE ?= disable
DB_DSN ?= postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@$(DB_HOST):$(DB_PORT)/$(POSTGRES_DB)?sslmode=$(DB_SSLMODE)

# Windows moment
export DB_DSN POSTGRES_PORT POSTGRES_USER POSTGRES_PASSWORD POSTGRES_DB

.PHONY: up down migrate run replay demo demo-smoke demo-positive demo-negative demo-all test test-integration vet

up:
	$(DC) up -d postgres

down:
	$(DC) down

migrate:
	$(DC) exec -T postgres psql -U $(POSTGRES_USER) -d $(POSTGRES_DB) -f /migrations/001_initial_schema.sql

run:
	go run ./cmd/server

replay:
	curl -X POST http://localhost:8080/v1/admin/replay \
	  -H "Content-Type: application/json" \
	  -H "X-User-ID: admin" -H "X-Role: admin" \
	  -d "{\"experiment_id\":\"$(EXP_ID)\",\"from\":\"$(FROM)\",\"to\":\"$(TO)\"}"

demo:
	go run ./scripts/scenario-positive

demo-smoke:
	go run ./scripts/scenario-smoke

demo-positive:
	go run ./scripts/scenario-positive

demo-negative:
	go run ./scripts/scenario-negative

demo-all:
	$(MAKE) demo-smoke
	$(MAKE) demo-positive
	$(MAKE) demo-negative

test:
	go test ./...

test-integration:
	go test -tags integration ./internal/infrastructure/db/postgres

vet:
	go vet ./...
