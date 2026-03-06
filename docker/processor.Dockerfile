# ── Build stage ───────────────────────────────────────────────
FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-w -s" \
    -o /bin/processor \
    ./cmd/processor

# ── Runtime stage ─────────────────────────────────────────────
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /bin/processor .
COPY --from=builder /app/configs/processor.yaml ./configs/processor.yaml
COPY --from=builder /app/migrations             ./migrations

EXPOSE 8082

ENV APP_ENV=prod

ENTRYPOINT ["./processor"]
