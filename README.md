# Infinite-AI

Infinite-AI is a full-stack AI chat platform with a ChatGPT-style web app,
admin console, billing/redeem flows, OpenAI/Anthropic-compatible developer APIs,
multimodal chat, image generation/editing, and provider fallback logic.

The repository is intentionally small and self-contained: React/Vite on the
frontend, Go services on the backend, PostgreSQL for durable data, Redis for
session/rate-limit state, MinIO for attachments, NATS for background work, and
SearXNG as the local web-search fallback.

## Features

- Chat conversations with streaming run events, cancellation, artifacts, sharing,
  shared-message collaboration, attachments, and vision-capable image input.
- Provider routing for OpenAI-compatible and Anthropic-compatible model routes,
  including endpoint probing and persisted endpoint-adaptation cache.
- Web search that prefers native provider tools first, then falls back to SearXNG
  with query planning, relevance scoring, deduplication, and second-pass fetches.
- Image generation and image editing, including prompt planning and reference
  image support.
- OpenAI/Anthropic-compatible public API endpoints:
  `/v1/chat/completions`, `/v1/responses`, `/v1/messages`,
  `/v1/images/generations`, and `/v1/images/edits`.
- Admin console for users, model routes, membership limits, system logs, service
  alerts, OAuth providers, email/SMS gateway settings, search provider settings,
  finance settings, and redeem campaigns.
- User account flows with password auth, captcha, contact verification, OAuth,
  API keys, plan/subscription views, exports, and account deletion.

## Repository Layout

```text
apps/web                 React + TypeScript + Vite web client
db/migrations            PostgreSQL schema used by all services
deploy                   Docker Compose, Dockerfiles, Nginx, SearXNG config
services/bff             Public BFF for sessions, CSRF, auth, OAuth, proxying
services/core            Core product API, chat, admin, billing, developer API
services/shared          Shared Go packages for config, auth, store, HTTP utils
services/worker          Background maintenance worker
```

## Quick Start

Requirements:

- Docker and Docker Compose for the full stack
- Go 1.25+ for local backend builds/tests
- Node.js 25+ and npm for local frontend development

Run the full local stack:

```bash
docker compose -f deploy/docker-compose.yml up --build
```

Local URLs:

```text
User app:      http://127.0.0.1:1001
API/BFF:       http://127.0.0.1:1002
Admin console: http://127.0.0.1:1003
Redeem page:   http://127.0.0.1:1004
MinIO console: http://127.0.0.1:1007
SearXNG:       http://127.0.0.1:1011
```

The Docker Compose stack uses local development credentials. Replace every
secret before exposing the service outside a trusted machine.

## Local Development

Backend:

```bash
go test ./services/...
go build ./services/...
```

Frontend:

```bash
cd apps/web
npm install
npm run dev
npm run build
```

Optional environment overrides can be copied from `.env.example`:

```bash
cp .env.example .env
```

## Default Local Admin

```text
Email:       admin@infinite.local
Password:    ChangeThisAdminPassword123!
TOTP secret: JBSWY3DPEHPK3PXP
```

Use the TOTP secret in an authenticator app to generate the admin MFA code.
These defaults are for local development only.

## Configuration Notes

- Model providers are configured in the admin console under model management.
  Endpoints and secrets are encrypted in the database.
- OpenAI-compatible providers are probed for the best supported endpoint shape;
  successful adaptations are persisted so restarts do not re-probe slowly.
- Native OpenAI Responses Web Search and Anthropic Web Search are attempted
  before the SearXNG fallback.
- Payment orders are persisted. IF-Pay remains inactive until real merchant
  credentials are configured.

## Production Checklist

- Set strong values for `INTERNAL_JWT_SECRET`, `COOKIE_HASH_SECRET`,
  `MASTER_KEY`, database credentials, MinIO credentials, and admin credentials.
- Serve all public origins over HTTPS and update `PUBLIC_BASE_URL`,
  `API_BASE_URL`, `ADMIN_BASE_URL`, and `REDEEM_BASE_URL`.
- Configure persistent volumes/backups for PostgreSQL, Redis, MinIO, and logs.
- Configure real OAuth, email/SMS gateways, payment credentials, and model
  provider endpoints in the admin console.
- Keep `.env`, private keys, provider secrets, and exported database dumps out of
  Git. The root `.gitignore` and `.dockerignore` are set up for this.
