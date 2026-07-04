# sws-scanner-service

Go backend service that powers the SwibSwap card scanner. Extracted from the original `sws-scanner-app` Vercel serverless functions so the scanner pipeline, pricing, variants, and marketplace APIs can run as a standalone SWS microservice.

## Architecture

Follows the standard SWS Go service layout:

```
cmd/server/          # HTTP entrypoint
internal/config/     # Environment config
internal/delivery/http/  # Gin handlers and routing
internal/domain/     # Entity definitions
internal/usecase/    # Business logic (scan, image, pricing, variants, marketplace, auctions)
internal/repository/ # Postgres implementations
internal/infrastructure/ # DB, Firebase, NATS, Vision, Anthropic, eBay clients
migrations/          # Embedded SQL migrations
static/              # DON/CN-anniv/logos reference assets
data/                # Catalog JSON files
```

## Quick start

```bash
# 1. Postgres is required for operational data
export DATABASE_URL="postgres://user:pass@localhost:5432/sws_scanner?sslmode=disable"

# 2. Optional but recommended for full scan pipeline
export ANTHROPIC_API_KEY="..."
export GOOGLE_VISION_API_KEY="..."
export FIREBASE_SERVICE_ACCOUNT_B64="..."
export FIREBASE_STORAGE_BUCKET="..."

# 3. Run
 go run ./cmd/server
```

Health checks:

```bash
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz
```

## Environment variables

| Variable | Required | Description |
|----------|----------|-------------|
| `DATABASE_URL` | yes | Postgres connection string |
| `PORT` | no | HTTP port (default `8080`) |
| `ANTHROPIC_API_KEY` | no | Claude Haiku API key |
| `GOOGLE_VISION_API_KEY` | no | Google Cloud Vision API key |
| `FIREBASE_SERVICE_ACCOUNT_B64` | no | Base64 Firebase Admin SDK JSON |
| `FIREBASE_STORAGE_BUCKET` | no | Firebase Storage bucket |
| `NATS_URL` | no | NATS URL for events |
| `CORS_ALLOWED_ORIGINS` | no | Comma-separated origins |
| `CRON_SECRET` | no | Secret for `/v1/auctions/tick` |

## API

Public routes are under `/v1/`. The SWS API gateway strips `/api` and forwards to this service.

| Method | Route | Description |
|--------|-------|-------------|
| POST | `/v1/scan` | Full card identification pipeline |
| POST | `/v1/quality` | CV + AI quality grading |
| POST | `/v1/watermark` | Vault/preview watermark |
| POST | `/v1/scan-phash` | Perceptual hash lookup |
| GET | `/v1/prices` | Market pricing |
| GET | `/v1/op-variants` | OP card variants |
| GET | `/v1/op-details` | Verified card details |
| GET | `/v1/don-cards` | DON catalog |
| GET | `/v1/cn-anniv-cards` | CN anniversary catalog |
| POST/GET | `/v1/transactions` | Marketplace transactions |
| POST/GET | `/v1/auctions` | Auctions |
| GET | `/v1/fx` | FX rates |
| GET | `/v1/whoami` | Identity echo |

Static assets are served at `/don-pdf/*`, `/don-pdf-wm/*`, `/cn-anniv/*`, `/logos/*`.

## Frontend integration

The existing React app switches to this service via `REACT_APP_API_BASE_URL`:

```bash
# Vercel serverless (legacy default)
REACT_APP_API_BASE_URL=/api

# Local Go service
REACT_APP_API_BASE_URL=http://localhost:8080/v1
```

`src/api.js` exports `apiUrl()`, `postJson()`, and `getJson()` helpers that prepend this base URL.

## Docker

```bash
docker build -t sws-scanner-service .
docker run -p 8080:8080 -e DATABASE_URL=... sws-scanner-service
```

## CI/CD

`.github/workflows/ci.yml` runs tests, `go vet`, and pushes the image to ECR on merges to `main`.

## gRPC

A proto contract is prepared in `sws-shared-protos/proto/scanner/v1/scanner.proto` for future internal service calls. The HTTP server is currently the only exposed interface.
