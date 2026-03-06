# ── Build stage ───────────────────────────────────────────────
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Dependencias primero para aprovechar cache de capas
COPY go.mod go.sum ./
RUN go mod download

# Código fuente
COPY . .

# Compilar solo el gateway — binario estático
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-w -s" \
    -o /bin/gateway \
    ./cmd/gateway

# ── Runtime stage ─────────────────────────────────────────────
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# Binario
COPY --from=builder /bin/gateway .

# Templates y configs (necesarios en runtime)
COPY --from=builder /app/services/gateway/templates ./services/gateway/templates
COPY --from=builder /app/configs/gateway.yaml       ./configs/gateway.yaml

EXPOSE 8080

ENV APP_ENV=prod

ENTRYPOINT ["./gateway"]
