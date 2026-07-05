# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o server ./cmd/server

# Runtime stage
FROM alpine:3.20

RUN apk --no-cache add ca-certificates wget && \
    addgroup -g 1000 -S app && \
    adduser -u 1000 -S app -G app

WORKDIR /app

COPY --from=builder /app/server .

USER app

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider "http://localhost:${PORT:-8080}/healthz" || exit 1

CMD ["./server"]
