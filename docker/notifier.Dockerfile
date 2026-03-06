# ── Build stage ───────────────────────────────────────────────
FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-w -s" \
    -o /bin/notifier \
    ./cmd/notifier

# ── Runtime stage ─────────────────────────────────────────────
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /bin/notifier .
COPY --from=builder /app/configs/notifier.yaml ./configs/notifier.yaml

EXPOSE 8081

ENV APP_ENV=prod

ENTRYPOINT ["./notifier"]
