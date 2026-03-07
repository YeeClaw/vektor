# Vektor

A self-hosted project management tool built for individuals and small teams. Designed for homelab use with a Linear-inspired interface, a CLI for automation, and git forge integration.

## Goals

- **Fast, clean UI** inspired by Linear's keyboard-driven workflow
- **Self-hosted** with minimal infrastructure (single binary + SQLite)
- **CLI-first automation** for scripting and homelab integration
- **Git forge integration** with GitHub, GitLab, and Forgejo (branch/commit linking, webhooks)
- **OIDC authentication** for SSO with existing identity providers (Authelia, Keycloak, etc.)
- **Local auth** option for development and simple setups

## Tech Stack

| Component | Technology |
|-----------|------------|
| Backend | Go (standard library HTTP server, Cobra CLI) |
| Frontend | React + TypeScript + Vite |
| Database | SQLite (via modernc.org/sqlite, pure Go) |
| Auth | OIDC (go-oidc) + optional local registration (bcrypt) |
| Deployment | Single binary with embedded frontend, Docker |

## Running

### Local development (local auth)

```sh
export VEKTOR_LOCAL_AUTH=true
go run ./cmd/vektor serve
```

The server starts on `:8659` by default.

### With OIDC

```sh
export VEKTOR_OIDC_ISSUER=https://auth.example.com
export VEKTOR_OIDC_CLIENT_ID=vektor
export VEKTOR_OIDC_CLIENT_SECRET=your-secret
export VEKTOR_OIDC_REDIRECT_URL=https://vektor.example.com/auth/callback
go run ./cmd/vektor serve
```

### Docker

```sh
docker compose up -d
```

Edit `docker-compose.yml` to configure your OIDC provider and other settings.

## Configuration

All configuration is via environment variables.

| Variable | Default | Description |
|----------|---------|-------------|
| `VEKTOR_LISTEN` | `:8659` | Address and port to listen on |
| `VEKTOR_DATA_DIR` | `./data` | Directory for SQLite database |
| `VEKTOR_LOCAL_AUTH` | `false` | Enable local user registration and login |
| `VEKTOR_OIDC_ISSUER` | | OIDC provider URL (required unless local auth is enabled) |
| `VEKTOR_OIDC_CLIENT_ID` | | OIDC client ID (required unless local auth is enabled) |
| `VEKTOR_OIDC_CLIENT_SECRET` | | OIDC client secret (required unless local auth is enabled) |
| `VEKTOR_OIDC_REDIRECT_URL` | | OIDC redirect URL after login |

## Project Structure

```
cmd/vektor/          Entry point, CLI commands
internal/
  api/               HTTP handlers and routing
  auth/              OIDC and local authentication
  config/            Environment-based configuration
  db/                SQLite connection and migrations
  models/            Data types (projects, issues, labels, users)
web/                 React frontend (embedded in binary at build time)
```

## API

### Auth

- `POST /auth/register` - Register a local user (when local auth is enabled)
- `POST /auth/login` - Log in with email/password (when local auth is enabled)
- `GET /auth/login` - Redirect to OIDC provider (when OIDC is configured)
- `GET /auth/callback` - OIDC callback

### Projects

- `GET /api/projects` - List all projects
- `POST /api/projects` - Create a project

### Issues

- `GET /api/projects/{projectKey}/issues` - List issues for a project
- `POST /api/projects/{projectKey}/issues` - Create an issue
- `PATCH /api/issues/{id}` - Update an issue

All `/api/` routes require authentication.

## License

TBD
