# ═══════════════════════════════════════════════════════════════
#  Switchboard — Makefile
# ═══════════════════════════════════════════════════════════════

.PHONY: help infra infra-tools infra-down infra-clean \
        run-gateway run-notifier run-processor run-all \
        build build-gateway build-notifier build-processor \
        migrate migrate-down migrate-create \
        test lint tidy \
        logs-gateway logs-notifier logs-processor logs-infra

# ─── Config ────────────────────────────────────────────────────
BINARY_DIR   := bin
MIGRATE_URL  := postgres://switchboard:switchboard@localhost:5432/switchboard?sslmode=disable
MIGRATE_DIR  := migrations
APP_ENV      ?= dev

# ─── Default ───────────────────────────────────────────────────
help: ## Muestra esta ayuda
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'

# ─── Infraestructura ───────────────────────────────────────────
infra: ## Levanta Postgres, Redis, NATS, Kafka
	docker compose up -d --wait postgres redis nats zookeeper kafka
	@echo "\nInfra lista:"
	@echo "   Postgres  → localhost:5432"
	@echo "   Redis     → localhost:6379"
	@echo "   NATS      → localhost:4222  (monitoring: :8222)"
	@echo "   Kafka     → localhost:9092"

infra-tools: ## Levanta también RedisInsight y Kafka-UI
	docker compose --profile tools up -d --wait
	@echo "\n🔧  Tools:"
	@echo "   RedisInsight → http://localhost:5540"
	@echo "   Kafka UI     → http://localhost:8090"

infra-down: ## Para los contenedores (conserva volúmenes)
	docker compose --profile tools down

infra-clean: ## Para y elimina volúmenes (borra datos)
	docker compose --profile tools down -v
	@echo "Volúmenes eliminados"

infra-logs: ## Logs de todos los servicios de infra
	docker compose logs -f postgres redis nats kafka

# ─── Migraciones ───────────────────────────────────────────────
migrate: ## Aplica todas las migraciones pendientes
	migrate -path $(MIGRATE_DIR) -database "$(MIGRATE_URL)" up

migrate-down: ## Revierte la última migración
	migrate -path $(MIGRATE_DIR) -database "$(MIGRATE_URL)" down 1

migrate-reset: ## Revierte TODAS las migraciones (borra datos)
	migrate -path $(MIGRATE_DIR) -database "$(MIGRATE_URL)" drop -f
	$(MAKE) migrate

migrate-create: ## Crea archivos de migración: make migrate-create NAME=add_users
ifndef NAME
	$(error Falta NAME. Uso: make migrate-create NAME=add_users)
endif
	migrate create -ext sql -dir $(MIGRATE_DIR) -seq $(NAME)

# ─── Build ─────────────────────────────────────────────────────
build: build-gateway build-notifier build-processor ## Compila los 3 servicios

build-gateway: ## Compila gateway
	go build -o $(BINARY_DIR)/gateway ./cmd/gateway

build-notifier: ## Compila notifier
	go build -o $(BINARY_DIR)/notifier ./cmd/notifier

build-processor: ## Compila processor
	go build -o $(BINARY_DIR)/processor ./cmd/processor

# ─── Run (dev) ─────────────────────────────────────────────────
run-gateway: ## Corre gateway en dev
	APP_ENV=dev go run ./cmd/gateway

run-notifier: ## Corre notifier en dev
	APP_ENV=dev go run ./cmd/notifier

run-processor: ## Corre processor en dev
	APP_ENV=dev go run ./cmd/processor

run-all: ## Corre los 3 servicios en paralelo (requiere: brew install entr o similar)
	@echo "Iniciando los 3 servicios..."
	@APP_ENV=dev go run ./cmd/gateway   & \
	 APP_ENV=dev go run ./cmd/notifier  & \
	 APP_ENV=dev go run ./cmd/processor & \
	 wait

# ─── Desarrollo ────────────────────────────────────────────────
tidy: ## go mod tidy
	go mod tidy

lint: ## golangci-lint (requiere instalación previa)
	golangci-lint run ./...

test: ## Corre todos los tests
	go test ./... -v -race -count=1

test-short: ## Tests rápidos (sin integracion)
	go test ./... -short -race

# ─── Logs servicios Go ─────────────────────────────────────────
logs-gateway: ## Tail logs del binario gateway (si corre con > logs/gateway.log)
	tail -f logs/gateway.log | jq .

logs-notifier:
	tail -f logs/notifier.log | jq .

logs-processor:
	tail -f logs/processor.log | jq .

# ─── Utilidades ────────────────────────────────────────────────
kafka-topics: ## Lista topics en Kafka
	docker exec switchboard-kafka kafka-topics \
		--bootstrap-server localhost:9092 --list

kafka-create-topic: ## Crea topic: make kafka-create-topic TOPIC=switchboard.events
ifndef TOPIC
	$(error Falta TOPIC. Uso: make kafka-create-topic TOPIC=switchboard.events)
endif
	docker exec switchboard-kafka kafka-topics \
		--bootstrap-server localhost:9092 \
		--create --topic $(TOPIC) \
		--partitions 3 --replication-factor 1

redis-cli: ## Abre redis-cli interactivo
	docker exec -it switchboard-redis redis-cli

nats-sub: ## Suscribe a todos los mensajes NATS (requiere nats-cli)
	nats sub ">"

psql: ## Abre psql interactivo
	docker exec -it switchboard-postgres \
		psql -U switchboard -d switchboard

setup: infra migrate ##  Setup completo: infra + migraciones
	@echo "\nSwitchboard listo para desarrollo"