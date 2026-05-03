<p align="center">
  https://iris.amanyd.me
</p>

<h1 align="center">Iris</h1>
<p align="center"><strong>AI-first automation platform — describe workflows in plain English, build them instantly.</strong></p>

<p align="center">
  <a href="#quick-start">Quick Start</a> ·
  <a href="#architecture">Architecture</a> ·
  <a href="#features">Features</a> ·
  <a href="#environment-variables">Configuration</a> ·
  <a href="#deployment">Deployment</a>
</p>

---

## What is Iris?

Iris is a self-hosted automation platform that lets you build and manage event-driven workflows ("relays") by describing them in plain English — via a web dashboard, or directly on **Telegram** using text or voice notes.

You say: *"Create a relay that checks Bitcoin price every hour and sends me a Telegram message if it drops below $60k."*

Iris builds the relay, schedules it, and runs it. No code required.

---

## Features

- **AI relay builder** — Describe automation workflows in plain English (powered by Gemini)
- **Visual DAG builder** — Drag-and-drop node-based relay editor in the browser
- **Telegram bot** — Create, list, trigger, enable/disable relays by chat or voice note
- **Voice notes → relays** — Send a voice note on Telegram, ElevenLabs transcribes it, Gemini builds the relay
- **Webhook triggers** — Each relay gets a unique webhook URL for external integrations
- **Cron triggers** — Schedule relays with natural-language cron expressions
- **Secrets manager** — Encrypted secret storage for API keys used in relay actions
- **Execution history** — Full audit trail of every relay run with step-level logs
- **Self-hosted** — Runs on a single $24/month VPS

---

## Architecture

```
┌─────────────────────────────────────────────────────┐
│                    iris-web (Next.js)                │
│         Dashboard · Visual Builder · Connections     │
└───────────────────────┬─────────────────────────────┘
                        │ REST API
┌───────────────────────▼─────────────────────────────┐
│                   iris-core (Go)                     │
│   Auth · Relays · Secrets · AI · Settings · NATS    │
└──────────┬────────────────────────┬─────────────────┘
           │ NATS JetStream         │ NATS
┌──────────▼──────────┐  ┌─────────▼─────────────────┐
│   iris-worker (Go)  │  │     iris-hooks (Go)        │
│  Executes relay     │  │  Receives webhook POSTs    │
│  actions (HTTP,     │  │  → publishes to NATS       │
│  NATS, cron, etc.)  │  └───────────────────────────┘
└─────────────────────┘
┌─────────────────────────────────────────────────────┐
│               iris-telegram (Go)                     │
│  Telegram bot · STT · AI relay creation via chat    │
└─────────────────────────────────────────────────────┘
┌──────────────────────┐  ┌──────────────────────────┐
│  PostgreSQL          │  │  NATS JetStream           │
│  (all state)         │  │  (event bus)              │
└──────────────────────┘  └──────────────────────────┘
```

### Services

| Service | Language | Port | Purpose |
|---------|----------|------|---------|
| `iris-core` | Go | 3000 | Main REST API — auth, relays, AI, settings |
| `iris-hooks` | Go | 8080 | Public webhook ingestion endpoint |
| `iris-worker` | Go | — | Background relay executor + cron scheduler |
| `iris-telegram` | Go | — | Telegram bot (polling, no webhook needed) |
| `iris-web` | Next.js | 3001 | Frontend dashboard |

---

## Quick Start

### Prerequisites

- [Go 1.22+](https://go.dev/dl/)
- [Node.js 20+](https://nodejs.org/)
- [Docker + Docker Compose](https://docs.docker.com/get-docker/)
- A [Google AI Studio](https://aistudio.google.com/) API key (free tier works)

### 1 — Clone

```bash
git clone https://github.com/youruser/Iris.git
cd Iris
```

### 2 — Configure environment

Copy the example env and fill in the required values:

```bash
cp .env.example .env
```

Open `.env` and set at minimum:

```env
# Required
DATABASE_URL=postgres://user:password@localhost:5432/iris?sslmode=disable
NATS_URL=nats://localhost:4222
JWT_SECRET=change-me-to-a-long-random-string-256bit
ENCRYPTION_KEY=64-char-hex-string-for-secret-encryption

# AI (required for relay generation)
LLM_PROVIDER=gemini
LLM_API_KEY=your-google-ai-api-key
LLM_MODEL=gemini-2.0-flash

# Optional — enable voice notes on Telegram
ELEVENLABS_API_KEY=your-elevenlabs-key

# Optional — set Telegram bot token here OR via the Connections page in the UI
# TELEGRAM_BOT_TOKEN=
TELEGRAM_BOT_USERNAME=YourBotUsername

# URLs
IRIS_CORE_URL=http://localhost:3000
IRIS_HOOKS_URL=http://localhost:8080
FRONTEND_URL=http://localhost:3001
```

### 3 — Start infrastructure

```bash
# Start Postgres + NATS
make infra-up

# Wait a few seconds, then run migrations
make db-migrate-up
```

> If `migrate` CLI is not installed: `go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest`

### 4 — Start all backend services

```bash
# Starts core + hooks + worker + telegram bot (waits for core before starting bot)
make dev-all
```

Each service logs to stdout with structured JSON:

```json
{"time":"...","level":"INFO","msg":"http server listening","service":"iris-core","addr":":3000"}
{"time":"...","level":"INFO","msg":"iris-worker ready","service":"iris-worker","workers":10}
{"time":"...","level":"INFO","msg":"telegram bot authorized","service":"iris-telegram","username":"YourBotUsername"}
```

### 5 — Start the frontend

```bash
cd web/iris-web
npm install
npm run dev
```

Open [http://localhost:3001](http://localhost:3001).

### 6 — Create your account

1. Go to [http://localhost:3001](http://localhost:3001)
2. Click **Sign Up** → create an account
3. You're in the dashboard

### 7 — Connect Telegram (optional)

1. Get a bot token from [@BotFather](https://t.me/BotFather) on Telegram → `/newbot`
2. In the dashboard: **Connections** → paste your token → **Save**
3. The bot fetches the token from the database automatically — no restart needed if already running
4. Open your bot on Telegram → `/login <your-iris-jwt-token>`
   - Copy your JWT from the Connections page
5. Start sending voice notes or text to create relays!

---

## Makefile reference

```bash
make infra-up          # Start Postgres + NATS via Docker
make infra-down        # Stop Docker infra

make db-migrate-up     # Run all pending database migrations
make db-migrate-down   # Roll back last migration
make db-reset          # Drop and re-run all migrations (destructive!)

make dev-core          # Start iris-core only
make dev-hooks         # Start iris-hooks only
make dev-worker        # Start iris-worker only
make dev-telegram      # Start iris-telegram only
make dev-backend       # Start core + hooks + worker (no telegram)
make dev-all           # Start all 4 services (telegram waits for core)

make build             # Build all binaries to bin/
make lint              # Run golangci-lint
```

---

## Environment Variables

### iris-core

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `DATABASE_URL` | ✅ | — | PostgreSQL connection string |
| `NATS_URL` | ✅ | — | NATS server URL |
| `JWT_SECRET` | ✅ | — | Secret for signing JWTs |
| `ENCRYPTION_KEY` | ✅ | — | 64-char hex key for encrypting secrets |
| `CORE_PORT` | — | `3000` | HTTP port |
| `FRONTEND_URL` | — | `http://localhost:3001` | Allowed CORS origin |
| `LLM_PROVIDER` | — | `openai` | `gemini` or `openai` |
| `LLM_API_KEY` | — | — | API key for your LLM provider |
| `LLM_MODEL` | — | `gpt-4o-mini` | Model name |
| `SERVICE_SECRET` | — | — | Shared secret for internal service calls |

### iris-hooks

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `DATABASE_URL` | ✅ | — | PostgreSQL connection string |
| `NATS_URL` | ✅ | — | NATS server URL |
| `HOOKS_PORT` | — | `8080` | HTTP port |

### iris-worker

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `DATABASE_URL` | ✅ | — | PostgreSQL connection string |
| `NATS_URL` | ✅ | — | NATS server URL |

### iris-telegram

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `DATABASE_URL` | ✅ | — | PostgreSQL connection string |
| `IRIS_CORE_URL` | — | `http://localhost:3000` | iris-core base URL |
| `IRIS_HOOKS_URL` | — | `http://localhost:8080` | iris-hooks base URL (shown in webhook relay messages) |
| `TELEGRAM_BOT_TOKEN` | — | — | Bot token from @BotFather. If empty, fetched from DB (set via Connections page) |
| `TELEGRAM_BOT_USERNAME` | — | — | Bot username without `@` (used for deep links) |
| `ELEVENLABS_API_KEY` | — | — | Enables voice note → text transcription |
| `SERVICE_SECRET` | — | — | Must match iris-core's `SERVICE_SECRET` |
| `SESSION_TTL` | — | `24h` | Telegram session inactivity timeout |

---

## Telegram Bot Commands

| Command | Description |
|---------|-------------|
| `/start` | Welcome message |
| `/login <jwt>` | Link your Iris account (paste JWT from dashboard) |
| `/logout` | Unlink your account |
| `/new` | Start creating a relay with AI |
| `/list` | List all your relays |
| `/toggle <name or ID>` | Flip a relay's active/inactive state |
| `/enable <name or ID>` | Enable a relay |
| `/disable <name or ID>` | Disable a relay |
| `/trigger <name or ID>` | Manually trigger a relay |
| `/status <name>` | Show recent executions for a relay |
| `/delete <name>` | Delete a relay |
| `/templates` | Quick-start template gallery |
| `/cancel` | Cancel current operation |
| `/help` | Show all commands |

**Voice notes:** Just send a voice note while logged in — the bot transcribes it with ElevenLabs and creates a relay. No `/new` needed.

---

## Relay Actions

Each relay is a DAG of actions. Currently supported action types:

| Action | What it does |
|--------|-------------|
| `http_request` | Make an HTTP GET/POST/PUT/DELETE request |
| `send_email` | Send an email via SMTP |
| `telegram_message` | Send a Telegram message to the linked user |
| `nats_publish` | Publish a message to a NATS subject |
| `transform` | Transform data with a template |
| `condition` | Branch the DAG based on a condition |

---

## Trigger Types

| Trigger | Description |
|---------|-------------|
| `webhook` | Triggered by a POST to `<hooks-url>/hooks/<relay-id>` |
| `cron` | Scheduled (e.g. every hour, every morning at 9am) |
| `manual` | Only triggered via `/trigger` command or dashboard |

---

## Database Migrations

Migrations live in `services/core/db/migrations/`. Each migration is a pair of `.up.sql` / `.down.sql` files.

```bash
# Create a new migration
make db-migrate-create NAME=add_something

# Run all pending
make db-migrate-up

# Roll back one
make db-migrate-down
```

---

## Deployment (DigitalOcean VPS)

Everything runs on a single **$24/month DigitalOcean Droplet** (2 vCPU / 4 GB RAM).

**Stack:**
- Go binaries managed by **systemd** (auto-restart on crash)
- Next.js frontend managed by **PM2**
- Postgres + NATS via **Docker Compose**
- **Nginx** as reverse proxy for 3 subdomains
- **Let's Encrypt** for free SSL (auto-renews)

For full step-by-step instructions see [docs/deployment.md](docs/deployment.md).

**Quick overview:**
```
yourdomain.com          → Next.js frontend
api.yourdomain.com      → iris-core (REST API)
hooks.yourdomain.com    → iris-hooks (webhook ingestion)
```

Set `TELEGRAM_BOT_TOKEN` via the Connections page in the dashboard — no secrets in `.env` on the server.

---

## Project Structure

```
Iris/
├── services/
│   ├── core/               # Main API (Go)
│   │   ├── cmd/api/        # Entry point
│   │   ├── db/migrations/  # SQL migrations
│   │   └── internal/
│   │       ├── api/        # HTTP handlers, router, middleware
│   │       ├── ai/         # LLM client + prompts
│   │       ├── models/     # Request/response types
│   │       └── store/      # PostgreSQL data access
│   ├── hooks/              # Webhook ingestion service (Go)
│   ├── worker/             # Relay executor + cron scheduler (Go)
│   └── iris-telegram/      # Telegram bot (Go)
│       ├── cmd/bot/        # Entry point
│       └── internal/
│           ├── bot/        # Command handlers, session state
│           ├── iris/       # iris-core API client
│           ├── stt/        # ElevenLabs STT client
│           └── store/      # Telegram DB (links, sessions)
├── web/
│   └── iris-web/           # Next.js 16 frontend
│       └── app/dashboard/  # Dashboard pages
├── packages/
│   └── logger/             # Shared structured logger
├── docker-compose.yml      # Postgres + NATS
├── Makefile                # Dev commands
└── .env                    # Local environment (never commit)
```

---

## Contributing

1. Fork the repo
2. Create a branch: `git checkout -b feature/my-feature`
3. Make your changes
4. Run `make lint` and `make build`
5. Open a pull request

---

## License

MIT
