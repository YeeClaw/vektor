# Vektor

A local-first issue tracker for individuals and small teams; project management without the
ceremony. A desktop app and CLI, with an optional hosted server on the same core. Go, SQLite (pure
Go driver), Vite + React, and Wails v3.

Product overview: [`site/`](site/). Design notes and the decision record are kept outside the repo
for now; the [issues](https://forge.coltco.net/austin/vektor/issues) are the live backlog.

## Status

Not released. Desktop is being developed first focusing on the local experience; the server
afterwards. What runs today is `vek serve` — an HTTP server with local accounts or OIDC, HMAC-signed
sessions, and a JSON API for projects and issues. Phase 1 (migrations, service layer, CLI in local
mode, comments, import, UI, desktop window) is in progress. The CLI is `vek serve` and nothing else
until then.

## Requirements

- Go 1.26+
- Node 22+ (frontend build only; the Go binary embeds `web/dist`)
- For the desktop build, once it exists, on Linux: GTK and WebKitGTK **dev** packages to compile
  (`libgtk-3-dev libwebkit2gtk-4.1-dev` on Debian/Ubuntu; the GTK4/`webkitgtk-6.0` flavour also
  works but has less distro reach), and the matching **runtime** library on any machine that runs it
  (`libwebkit2gtk-4.1-0` / `webkit2gtk4.1` / `webkit2gtk-4.1`, usually already installed). macOS and
  Windows have WKWebView/WebView2 built in. The headless build has no runtime dependencies at all.

## Build

```sh
make build     # bin/vek (pure Go today; becomes the desktop build when `vek app` lands)
make test      # go test ./...
make check     # go vet + gofmt
make dev       # load .env, run `vek serve`
make clean
make dev-reset # delete ./data
```

Planned alongside `vek app`: `make headless` (`CGO_ENABLED=0 -tags noapp`, static, every command
except `vek app`) and `make test-app`. `web/dist/` is gitignored; the Dockerfile builds it in a node
stage. `docker compose up -d` builds and runs the server.

## Run the server

```sh
export VEKTOR_LOCAL_AUTH=true
export VEKTOR_SESSION_SECRET=$(openssl rand -hex 32)
make dev                       # :8659
```

With OIDC instead of local accounts:

```sh
export VEKTOR_OIDC_ISSUER=https://auth.example.com
export VEKTOR_OIDC_CLIENT_ID=vektor
export VEKTOR_OIDC_CLIENT_SECRET=…
export VEKTOR_OIDC_REDIRECT_URL=https://vektor.example.com/auth/callback
export VEKTOR_SESSION_SECRET=…
go run ./cmd/vek serve
```

### Server configuration

All server configuration is by environment variable; `.env.example` lists them. Local mode (the
desktop app and CLI against the local file) uses none of these.

| Variable | Default | Description |
|---|---|---|
| `VEKTOR_LISTEN` | `:8659` | Address and port |
| `VEKTOR_DATA_DIR` | `./data` | Directory for the SQLite database |
| `VEKTOR_SESSION_SECRET` | | HMAC key for session cookies (required; <32 chars warns) |
| `VEKTOR_LOCAL_AUTH` | `false` | Enable local registration and login |
| `VEKTOR_OIDC_ISSUER` | | OIDC provider URL (required unless local auth) |
| `VEKTOR_OIDC_CLIENT_ID` | | OIDC client ID |
| `VEKTOR_OIDC_CLIENT_SECRET` | | OIDC client secret |
| `VEKTOR_OIDC_REDIRECT_URL` | | Redirect URL after login |

### CLI configuration (planned, #4)

`~/.config/vektor/config.json`: `mode` (`local`|`server`), `data_dir` (default
`$XDG_DATA_HOME/vektor`), `server_url`, `token`, `project`. Env overrides `VEKTOR_MODE`,
`VEKTOR_DATA_DIR`, `VEKTOR_SERVER`, `VEKTOR_TOKEN`, `VEKTOR_PROJECT`. Local mode needs no file.

## HTTP API

All `/api/` routes require a session; Bearer tokens (#3) are planned.

```
POST /auth/register                     local auth
POST /auth/login                        local auth
GET  /auth/login, GET /auth/callback    OIDC
GET  /api/me
GET  /api/projects
POST /api/projects
GET  /api/projects/{key}/issues
POST /api/projects/{key}/issues
PATCH /api/issues/{id}
```

Planned: `GET /api/projects/{key}`, `GET|PATCH /api/projects/{key}/issues/{number}`, `GET
/api/issues?q=`, comments, tokens, `POST /api/projects/{key}/import` (the only route that accepts an
issue `number`), and one JSON error shape `{"error", "message", "details":[{"field","message"}]}`
with codes `validation_error | not_found | unauthorized | conflict | internal_error`.

## Layout

```
cmd/vek/          entry point and commands
internal/
  api/            HTTP handlers, routing, (planned) errors and static serving
  authn/          Authenticator interface, OIDC, local accounts, sessions, middleware
  config/         server env config
  db/             Store (embeds *sql.DB), migrations
  models/         Project, Issue, (planned) Comment, User
  cli/            (planned) config, Backend interface: local file or HTTP client
  service/        (planned) validation and rules, shared by api and the local CLI backend
web/              Vite + React + TS; web/dist is embedded
site/             static product page
```

### Architecture notes

- **Two modes, one config.** Local (SQLite file, no auth; identity from `git config`) or server
  (hosted instance, Bearer token). The desktop app and the CLI read the same config file. Switching
  modes does not move data; `vek import` does.
- **One core.** `internal/service` holds validation, authorship, and the import-only issue-number
  rule; the HTTP handlers and the CLI's local backend both call it. A parity test runs the same
  inputs through both and requires identical results.
- **Two builds.** `cmd/vek/app.go` (`!noapp`, imports Wails) and `app_stub.go` (`noapp`). Nothing
  else knows the tag; `go list -tags noapp -deps` must show no Wails packages.
- **Two writers.** Any process holding the local database (`vek app` or `vek serve`) writes
  `server.json` to the data dir; the CLI routes through it instead of opening the file alongside.
- **SQLite.** WAL and foreign keys on and verified at open; timestamps are RFC3339 UTC text written
  by Go; migrations are embedded SQL files, contiguous from 1, one transaction each.
- **Not a forge.** No webhooks, no repository access; a branch chip links out.
- **Capture the data, don't build the ceremony.** The schema may grow dates, milestones, history,
  and metrics; the UI does not grow sprints, roadmaps, notifications, or permission models.

## Backups

WAL is enabled. `sqlite3 vektor.db ".backup out.db"` against the live file; never `cp` it. Restore
one before you need to. Do not put the database under Syncthing or any file-sync tool.

## Contributing

Branch per issue (`feat/48-sql-migrations`), PR against `main`, merge commits. CI runs `make build`,
`make test`, `make check` on Forgejo Actions.

## License

[AGPL-3.0](./LICENSE)
