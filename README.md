# ⚡ Switchboard

> A Go polyglot playground — a connection board that routes signals between systems.

Switchboard is a multi-service project that demonstrates real-time communication patterns in Go using 3 microservices, 6 messaging flows, and a live HTMX frontend.

![Go](https://img.shields.io/badge/Go-1.25-00ADD8?style=flat&logo=go)
![HTMX](https://img.shields.io/badge/HTMX-2.0-3D72D7?style=flat)
![Tailwind](https://img.shields.io/badge/Tailwind-4.0-38BDF8?style=flat&logo=tailwindcss)
![License](https://img.shields.io/badge/license-MIT-green?style=flat)

---

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                         browser                             │
│                    HTMX + WebSocket                         │
└──────────────────────────┬──────────────────────────────────┘
                           │ HTTP / WS / SSE
┌──────────────────────────▼──────────────────────────────────┐
│                       gateway :8080                         │
│         serves HTMX templates · WebSocket hub               │
│         publishes to NATS · reads/writes Redis              │
└────────┬─────────────────┬────────────────────┬─────────────┘
         │ NATS            │ Redis pub/sub      │ Kafka produce
┌────────▼────────┐        │            ┌───────▼──────────────┐
│  notifier :8081 │        │            │   processor :8082    │
│  NATS subscribe │        │            │   Kafka consume      │
│  relay → Redis  │        │            │   Postgres write     │
│  reply requests │        │            │   Redis cache        │
└─────────────────┘        │            │   NATS emit          │
                           │            └──────────────────────┘
┌──────────────────────────▼──────────────────────────────────┐
│                      Infrastructure                         │
│   Postgres · Redis · NATS (JetStream) · Kafka + Zookeeper   │
└─────────────────────────────────────────────────────────────┘
```

---

## The 6 Flows

| # | Flow | Description |
|---|------|-------------|
| 1 | **Postgres** | CRUD with `pgx/v5` · `hx-swap="afterbegin"` · infinite scroll |
| 2 | **Redis** | `get/set` panel + `pub/sub` via SSE stream |
| 3 | **NATS** | `request/reply` between gateway and notifier — sub-ms latency |
| 4 | **WebSocket** | Live chat with hub pattern · `gorilla/websocket` |
| 5 | **Kafka** | Produce from browser → processor consumes → Postgres write → NATS → SSE |
| 6 | **Firehose** | All services emit to `switchboard.firehose` — aggregated in real-time |

---

## Project Structure

```
switchboard/
├── cmd/
│   ├── gateway/        # entrypoint for gateway service
│   ├── notifier/       # entrypoint for notifier service
│   └── processor/      # entrypoint for processor service
│
├── internal/
│   ├── config/         # Viper config loader + zerolog setup
│   ├── events/         # shared firehose event emitter
│   ├── messaging/
│   │   ├── kafka/      # sarama producer + consumer group
│   │   └── nats/       # nats.go client + request/reply helpers
│   ├── store/
│   │   ├── postgres/   # pgx/v5 pool + named queries
│   │   └── redis/      # go-redis client + cache + pub/sub
│   └── transport/
│       └── ws/         # gorilla/websocket hub + client pumps
│
├── services/
│   ├── gateway/        # HTTP server + all handlers + templates
│   ├── notifier/       # NATS worker + HTTP server
│   └── processor/      # Kafka consumer + Postgres writer
│
├── configs/
│   ├── gateway.yaml
│   ├── notifier.yaml
│   └── processor.yaml
│
├── migrations/         # golang-migrate SQL files
├── compose.yml         # development infrastructure
├── compose.prod.yml    # production multi-service compose
└── Makefile
```

---

## Stack

| Layer | Technology |
|-------|------------|
| Language | Go 1.25 |
| Frontend | HTMX 2.0 + Tailwind CSS 4 (browser CDN) |
| Icons | Lucide |
| Fonts | Albert Sans · Poppins · JetBrains Mono |
| Theme | Catppuccin Mocha |
| Postgres client | `pgx/v5` — no ORM, raw SQL with named args |
| Redis client | `go-redis/v9` |
| NATS client | `nats.go` |
| Kafka client | `github.com/IBM/sarama` |
| WebSocket | `gorilla/websocket` |
| Config | Viper with YAML + env var overrides |
| Logging | zerolog — JSON in prod, colored console in dev |
| Migrations | `golang-migrate` with numbered `.sql` files |

---

## Prerequisites

- [Go 1.25+](https://golang.org/dl/)
- [Docker](https://docs.docker.com/get-docker/) + Docker Compose
- [golang-migrate](https://github.com/golang-migrate/migrate)

```bash
# Install golang-migrate
brew install golang-migrate
```

---

## Quick Start

```bash
# 1. Clone
git clone https://github.com/fn-9li9/switchboard
cd switchboard

# 2. Start infrastructure + run migrations
make setup

# 3. Run all 3 services (separate terminals)
make run-gateway    # :8080
make run-notifier   # :8081
make run-processor  # :8082
```

Open [http://localhost:8080](http://localhost:8080)

---

## Configuration

Each service reads from `configs/<service>.yaml` and supports env var overrides.

The prefix is the service name in uppercase:

```bash
# Override gateway port
GATEWAY_SERVER_PORT=9090 make run-gateway

# Override processor Postgres DSN
PROCESSOR_POSTGRES_DSN=postgres://user:pass@host:5432/db make run-processor
```

**`configs/gateway.yaml`**
```yaml
env: dev
service: gateway
server:
  host: 0.0.0.0
  port: 8080
redis:
  addr: localhost:6379
nats:
  url: nats://localhost:4222
kafka:
  brokers:
    - localhost:9092
```

---

## Makefile Reference

```bash
make setup              # start infra + run migrations
make infra              # start Postgres, Redis, NATS, Kafka
make infra-tools        # also start RedisInsight (:5540) + Kafka UI (:8090)
make infra-down         # stop containers
make infra-clean        # stop + delete volumes

make run-gateway        # run gateway in dev mode
make run-notifier       # run notifier in dev mode
make run-processor      # run processor in dev mode

make migrate            # apply pending migrations
make migrate-down       # revert last migration
make migrate-create NAME=add_users  # create new migration files

make build              # compile all 3 services
make test               # run all tests
make lint               # golangci-lint
make tidy               # go mod tidy

make kafka-topics       # list Kafka topics
make redis-cli          # open redis-cli
make psql               # open psql
```

---

## Migrations

```bash
# Create a new migration
make migrate-create NAME=add_users

# Apply
make migrate

# Revert last
make migrate-down

# Reset everything
make migrate-reset
```

---

## Go Conventions

- `cmd/` — entrypoints only (wiring: config → logger → dependencies → server)
- `internal/` — stateless clients, wrappers, helpers — no business logic
- `services/` — business logic per service
- Named SQL arguments via `pgx.NamedArgs` — no ORM
- `golang-migrate` with numbered `.sql` files — no Go migrations
- Viper with YAML + uppercase env prefix overrides
- zerolog: JSON in production, colored `ConsoleWriter` in dev
- `github.com/IBM/sarama` — community fork, not deprecated Shopify version

---

## Development Tools (optional)

Start with `make infra-tools`:

| Tool | URL | Description |
|------|-----|-------------|
| RedisInsight | http://localhost:5540 | Redis GUI |
| Kafka UI | http://localhost:8090 | Kafka topics + messages |
| NATS Monitoring | http://localhost:8222 | NATS server stats |

---

## License

MIT
