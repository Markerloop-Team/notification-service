# Notification Service

Microservice for handling email notifications for the MarkerLoop ecosystem.

## Tech Stack
- **Language:** Go 1.23
- **Framework:** Gin Web Framework
- **SDK:** `gitlab.com/markerloop/go-sdk` (Logger, Config, Middleware, Wrapper)
- **Logging:** Logrus
- **Hot Reload:** Air (for development)
- **Containerization:** Docker (multi-stage build)

## Architecture Overview
The service follows a standard hexagonal/layered architecture:
- `cmd/server/main.go`: Application entry point and dependency injection.
- `internal/routes`: HTTP route definitions using Gin.
- `internal/handlers`: HTTP request processing and validation.
- `internal/services`: Core business logic (email orchestration and templating).
- `internal/email`: Low-level email adapters (SMTP, MailHog) and sender interface.
- `internal/models`: Data structures for requests and responses.
- `internal/config`: Environment-based configuration management.

## Project Structure
- `cmd/server/`: Main executable.
- `internal/`: Private application code.
  - `config/`: Configuration loading and environment mapping.
  - `email/`: Email provider implementations (adapters).
  - `handlers/`: HTTP handlers.
  - `models/`: DTOs and internal models.
  - `routes/`: Gin router configuration.
  - `services/`: Business logic and HTML template generation.
- `Dockerfile`: Multi-stage build for production.
- `.air.toml`: Air configuration for live reloading.

## Key Components
- **Email Service:** Orchestrates sending emails using various adapters.
- **Template Engine:** Generates responsive HTML emails for verification, welcome, and password reset actions.
- **Email Adapters:** Supports MailHog (for local dev) and Google SMTP (production).

## API Endpoints
Base path: `/api/v1/notifications`

### Email Notifications
| Method | Path | Description |
|--------|------|-------------|
| POST | `/email/send` | Send a generic custom email |
| POST | `/email/send-verification` | Send an email with a verification code |
| POST | `/email/send-welcome` | Send a welcome email to a new user |
| POST | `/email/send-password-reset` | Send an email with a password reset link |
| POST | `/internal/email/test` | Test SMTP configuration — service-to-service, requires `X-Service-Key` (SDL-322) |
| POST | `/internal/email/workspace-invite` | Workspace invitation email (ru/en/kk, `html/template`) — service-to-service, requires `X-Service-Key` (SDL-615) |
| POST | `/deploy` | Deploy webhook → Telegram alert via markerloop-bot (used by CI regress alerts, SDL-132) |
| GET | `/health` | Service health status |

Every `/email/*` route and `/deploy` require service-to-service authentication (the `X-Service-Key` header): they are
called by other services, never by end users.

## Configuration (Env Vars)
| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8083` | Service port |
| `MAIL_HOST` | `localhost` | SMTP host |
| `MAIL_PORT` | `1025` | SMTP port |
| `MAIL_USER` | - | SMTP username |
| `MAIL_PASSWORD` | - | SMTP password |
| `FROM_EMAIL` | `noreply@markerloop.com` | Sender email address |
| `MAIL_DISABLE_TLS` | `false` | Force disable TLS (auto-disabled for localhost:1025) |
| `SERVICE_KEY` | **Required** | Shared key for service-to-service auth (`X-Service-Key`) |
| `TELEGRAM_BOT_SERVICE_URL` | - | markerloop-bot URL for `/deploy` Telegram alerts |
| `LOG_LEVEL` | `info` | Logging verbosity (debug, info, warn, error) |

## Build & Run Instructions

### Local Development (with Air)
1. Install [Air](https://github.com/cosmtrek/air).
2. Create `.env` from `env.example`.
3. Run:
   ```bash
   air
   ```

### Docker
```bash
docker build \
  --build-arg DEPLOY_TOKEN_USERNAME=your_username \
  --build-arg DEPLOY_TOKEN_PASSWORD=your_password \
  -t notification-service .
docker run -p 8083:8083 notification-service
```
