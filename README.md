# QAMail Manager

Self-hosted tool for QA teams: generate throwaway email addresses, read their inboxes, and pick out
OTP codes and verification links, without sharing a real mailbox.

It supports two mail providers:

- **Mail.tm**: the free public disposable-email service. Works out of the box, no domain needed.
  Some signup forms block its domains as "temporary email".
- **Own domain (`local`)**: a catch-all domain you own, receiving mail through Cloudflare Email
  Routing. Looks like a normal domain to signup forms. Needs a domain and a free Cloudflare account.

## Features

- Realistic generated addresses, **bulk generate**, and **CSV export** of the filtered list
  (passwords are never exported)
- **OTP detection** and verification, login and reset links, with Open and Copy buttons
- Message view with sandboxed HTML, inline images, attachments, raw source and an expanded modal
- **Live updates** (server-sent events): new mail appears at once, one grouped toast per burst
- **Reply** from own-domain accounts when an SMTP server is configured
- Several catch-all domains, with a domain picker when generating
- Account lifecycle rules: mark accounts as used automatically, block or delete idle accounts
- **Users and roles** (admin / user), per-user isolation and a per-user account quota
- Most behaviour editable live on the **Settings** page, no restart needed
- English and Indonesian interface; API error messages in both languages (`Accept-Language`)
- Offline queue in the Email Worker: mail sent while your server is down arrives later
- Sample emails for manual QA in [`tools/test-emails`](tools/test-emails)

## Architecture

```
browser ──▶ frontend (nginx: React app, forwards /api) ──▶ backend (Go API) ──▶ PostgreSQL
                                                              │
            Mail.tm ◀─────────────────────────────────────────┘   (provider: mailtm)

sender ──▶ Cloudflare Email Routing ──▶ Email Worker ──▶ tunnel ──▶ backend ingest port   (provider: local)
```

| Folder                                           | What it is                                              |
| ------------------------------------------------ | ------------------------------------------------------- |
| [`backend/`](backend/README.md)                  | Go (Fiber + GORM) API, PostgreSQL storage, ingest server |
| [`frontend/`](frontend/README.md)                | React + Vite web app                                    |
| [`cloudflare/email-worker/`](cloudflare/email-worker/README.md) | Cloudflare Email Worker that forwards own-domain mail to the backend |
| [`tools/test-emails/`](tools/test-emails)        | Sample emails for manual testing                        |

## Prerequisites

- **Docker** with Compose v2 (Docker Desktop, or Docker Engine plus the compose plugin)
- Only for the own-domain provider: a domain you own, a free [Cloudflare](https://www.cloudflare.com)
  account, and Node 22 to deploy the Email Worker
- Only for a public HTTPS setup: a server with a public IP, ports 80 and 443 open, and a DNS record

## Quickstart (local, Mail.tm provider)

```bash
git clone https://github.com/reihanrere/qamail-manager.git
cd qamail-manager
./setup.sh                      # creates .env with a random DB_PASSWORD
docker compose up -d --build    # the first build takes a few minutes
docker compose exec backend ./api user add you@example.com --admin   # asks for a password
```

Open **http://localhost:3000** and log in with that email and password. That is all: the default
provider is Mail.tm, so no domain or account elsewhere is needed.

There is no default account and no default password. `setup.sh` only writes `.env`; if you prefer to
do that by hand, run `cp .env.example .env` and set `DB_PASSWORD` (the stack refuses to start without it).

<details>
<summary>Create the first admin from <code>.env</code> instead (for scripted installs)</summary>

Set both values before the first start:

```bash
ADMIN_EMAIL=you@example.com
ADMIN_PASSWORD=<a long password>      # at least 10 characters
```

They are only used while no user exists. Afterwards empty **both** in `.env` and restart, so the
password does not stay on disk; the backend refuses to start when only one of them is set.

</details>

The web app listens on `127.0.0.1` only, because it shows OTPs and plain HTTP does not protect the
login. To serve it publicly, use [Public HTTPS](#public-https) below.

## Public HTTPS

`docker-compose.prod.yml` adds [Caddy](https://caddy.community) in front of the web container. It
gets and renews a Let's Encrypt certificate by itself, marks the session cookie `Secure`, stops
publishing the web port on the host and turns the Swagger UI off.

1. Create a DNS `A` (and `AAAA`) record for your domain pointing at the server. Open ports 80 and 443.
2. Set `DOMAIN=mail.example.com` in `.env`.
3. Start with both files:

   ```bash
   docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d --build
   ```

Already run a reverse proxy (Traefik, nginx, ...)? Skip the prod file. Publish the web port to your
proxy (`WEB_PORT`, `WEB_BIND`), set `COOKIE_SECURE=true`, and set `TRUSTED_PROXY_CIDR` to the proxy's
network so the login rate limit sees real client addresses.

## Own domain (catch-all) setup

Skip this if Mail.tm is enough. It lets you generate addresses on your own domain, such as
`name@inbox.example.com`. Replace `example.com` with your domain throughout.

```
sender ──▶ Cloudflare Email Routing ──▶ Email Worker ──▶ HTTPS URL ──▶ backend ingest port ──▶ PostgreSQL
```

1. **DNS.** Add the domain to Cloudflare (Free plan) and point the registrar's nameservers at it.
   Enable **Email → Email Routing**; it adds the MX and SPF records.
2. **Secret and domain.** In `.env`, set the shared secret and the domain:

   ```bash
   INGEST_SECRET=<output of: openssl rand -hex 32>   # at least 32 characters
   CATCHALL_DOMAIN=inbox.example.com       # comma-separate several domains
   MAIL_PROVIDER=local                     # optional: make it the default provider
   ```

   The backend refuses to start if `CATCHALL_DOMAIN` is set while `INGEST_SECRET` is empty.
3. **A public HTTPS URL for the ingest port.** The Worker must reach the backend's ingest server
   (port `INGEST_PORT`, default 8081, never published by the compose file). The bundled option is a
   Cloudflare Tunnel: in **Zero Trust → Networks → Tunnels → Create (cloudflared)** copy the token
   into `.env` and add a public hostname such as `ingest.example.com` with service
   `http://backend:8081`:

   ```bash
   COMPOSE_PROFILES=tunnel
   TUNNEL_TOKEN=<token from Cloudflare>
   ```

   Any other HTTPS endpoint that forwards `/api/ingest` to `backend:8081` works too. Do not expose
   the rest of the API through it.
4. **Worker.** Deploy [`cloudflare/email-worker`](cloudflare/email-worker/README.md) with the same
   `INGEST_SECRET` and your `INGEST_URL` (for example `https://ingest.example.com/api/ingest`), then set
   the Email Routing **catch-all** action to **Send to a Worker**.
5. Restart: `docker compose up -d --build`. **Settings → Mail Providers** shows whether each
   provider is available. Send a test mail with [`tools/test-emails/send.sh`](tools/test-emails).
6. **Reputation (recommended).** Add a DMARC record so the domain looks maintained to signup forms:
   TXT `_dmarc.inbox.example.com` = `v=DMARC1; p=none`. A simple landing page on the apex domain also
   helps against "unknown domain" checks.

Several domains can share one setup: list them in `CATCHALL_DOMAIN` and enable Email Routing with the
same Worker on each. To send and reply from own-domain addresses, configure an SMTP server that is
allowed to send for the domain (`SMTP_*`).

## Configuration

Copy `.env.example` to `.env`. Everything has a safe default except `DB_PASSWORD`. The defaults live
in [`backend/internal/config/config.go`](backend/internal/config/config.go); a bad value stops the
backend at startup with a clear message. Settings marked **live** can also be changed on the
**Settings** page; a value saved there overrides `.env` until it is reset.

### Compose and web

| Variable            | Default           | Purpose                                                                                     |
| ------------------- | ----------------- | ------------------------------------------------------------------------------------------- |
| `WEB_PORT`          | `3000`            | Host port of the web app                                                                    |
| `WEB_BIND`          | `127.0.0.1`       | Host interface. `0.0.0.0` exposes the app to your network: only behind HTTPS                |
| `DOMAIN`            | none              | Public domain for `docker-compose.prod.yml` (required there)                                |
| `TRUSTED_PROXY_CIDR`| `127.0.0.1`       | Network of the reverse proxy in front of the web container (trusts nobody by default)       |
| `DB_USER`           | `postgres`        | PostgreSQL user                                                                             |
| `DB_PASSWORD`       | **required**      | PostgreSQL password (the database is not published outside Docker)                          |
| `DB_NAME`           | `qamail_manager`  | Database name                                                                               |
| `TZ`                | `UTC`             | Time zone of the containers                                                                 |
| `CLOUDFLARED_VERSION` | pinned          | Version of the optional `cloudflared` image                                                 |
| `COMPOSE_PROFILES`  | empty             | Set to `tunnel` to start the Cloudflare Tunnel container                                    |
| `TUNNEL_TOKEN`      | empty             | Cloudflare Tunnel token (with the `tunnel` profile)                                         |

### Mail providers and ingest

| Variable                 | Default              | Purpose                                                                          |
| ------------------------ | -------------------- | -------------------------------------------------------------------------------- |
| `MAIL_PROVIDER`          | `mailtm`             | Default provider for new accounts: `mailtm` or `local` (**live**)                |
| `MAILTM_BASE_URL`        | `https://api.mail.tm`| Mail.tm API                                                                      |
| `CATCHALL_DOMAIN`        | empty                | Own domain(s), comma-separated; empty disables the `local` provider              |
| `INGEST_SECRET`          | empty                | Shared with the Worker, 32+ characters. Empty disables the ingest server. Required when `CATCHALL_DOMAIN` is set |
| `INGEST_PORT`            | `8081`               | Ingest-only server inside the backend container                                  |
| `INGEST_MAX_BYTES`       | `26214400`           | Largest raw email accepted (Cloudflare caps messages at 25 MiB)                  |
| `INGEST_MAX_MIME_PARTS`  | `200`                | Most multipart containers and parts in one email; more is refused                |
| `MESSAGE_RETENTION`      | `720h`               | Delete stored own-domain mail older than this; `0` keeps it forever (**live**)   |
| `MESSAGE_PRUNE_INTERVAL` | `1h`                 | How often retention and account cleanup run                                      |

### Inbox and generated accounts

| Variable                    | Default | Purpose                                                                            |
| --------------------------- | ------- | ---------------------------------------------------------------------------------- |
| `INBOX_SYNC_INTERVAL`       | `2m`    | How often every account's newest-message time is refreshed; `0` disables (**live**)|
| `MAILTM_REQUEST_DELAY`      | `400ms` | Pause between Mail.tm accounts during the background sync (**live**)               |
| `INBOX_SEARCH_MAX_PAGES`    | `10`    | Most inbox pages (30 messages each) a search scans (**live**)                      |
| `EVENTS_HEARTBEAT_INTERVAL` | `20s`   | Keep-alive of the live-update stream through proxies                               |
| `USERNAME_MAX_ATTEMPTS`     | `5`     | Retries when a generated address is taken (**live**)                               |
| `USERNAME_FIRST_NAMES` / `USERNAME_LAST_NAMES` | built-in | Comma-separated name pools for generated addresses (**live**) |
| `LEGACY_USERNAME_PATTERN`   | empty   | Regexp on the part before `@`; matches get an "Old format" badge and a Replace action (**live**) |
| `TAG_MAX_LENGTH` / `NOTE_MAX_LENGTH` | `50` / `500` | Label length limits (**live**)                                       |
| `BULK_GENERATE_MAX`         | `50`    | Most accounts one bulk generate may create (**live**)                              |
| `AUTO_MARK_USED`            | `off`   | `off`, `first_message` or `otp_copied` (**live**)                                  |
| `ACCOUNT_CLEANUP_AFTER`     | `0`     | Block or delete accounts without mail for this long; `0` disables (**live**)       |
| `ACCOUNT_CLEANUP_ACTION`    | `block` | `block` or `delete` (**live**)                                                     |

### Sending (optional)

| Variable                              | Default    | Purpose                                                   |
| ------------------------------------- | ---------- | --------------------------------------------------------- |
| `SMTP_HOST`                           | empty      | SMTP server allowed to send for your domain; empty disables sending |
| `SMTP_PORT`                           | `587`      | SMTP port                                                 |
| `SMTP_USERNAME` / `SMTP_PASSWORD`     | empty      | SMTP credentials                                          |
| `SMTP_SECURITY`                       | `starttls` | `starttls`, `tls` or `none` (local test servers only)     |

### Users and login

| Variable                        | Default                              | Purpose                                                                   |
| ------------------------------- | ------------------------------------ | ------------------------------------------------------------------------- |
| `ADMIN_EMAIL` / `ADMIN_PASSWORD`| empty                                | First admin, created only while no user exists. Set both, then empty both |
| `PASSWORD_MIN_LENGTH`           | `10`                                 | Shortest accepted password (at least 8)                                   |
| `SESSION_TTL`                   | `168h`                               | How long a login stays valid                                              |
| `LOGIN_MAX_ATTEMPTS`            | `5`                                  | Failed logins per IP and email within `LOGIN_WINDOW` before refusing      |
| `LOGIN_WINDOW`                  | `15m`                                | Window of the login limit                                                 |
| `LOGIN_MAX_CONCURRENT_VERIFIES` | `4`                                  | Password checks at once (each uses 64 MiB)                                |
| `LOGIN_VERIFY_WAIT`             | `2s`                                 | How long a login waits for a free slot before a 429                       |
| `COOKIE_SECURE`                 | `false` (`true` with the prod file)  | Mark the session cookie `Secure`. Required over HTTPS                     |
| `USER_ACCOUNT_QUOTA`            | `10`                                 | Accounts a non-admin may own; admins are unlimited (**live**)             |
| `CORS_ALLOWED_ORIGINS`          | empty                                | Browser origins allowed to call the API with cookies (dev server only)    |
| `TRUSTED_PROXIES`               | private ranges                       | Proxies trusted to report the client address                              |
| `CLIENT_IP_HEADER`              | `X-Real-IP`                          | Header carrying the client address                                        |

## Users and login

Every page and every `/api` route except `POST /api/auth/login` needs a session.

- **Roles.** `admin` manages users and settings and can send email. `user` generates and reads mail,
  up to `USER_ACCOUNT_QUOTA` accounts.
- **Isolation.** Each user sees only their own accounts, inboxes and live events.
- **Sessions.** HttpOnly, SameSite=Lax cookie, valid for `SESSION_TTL`. Logout, a password change,
  disabling or deleting the user ends their sessions at once. Logins are rate limited per IP and email.
- **Login requests.** Only a JSON body is accepted, and a browser's `Origin` must match the app's host
  or `CORS_ALLOWED_ORIGINS`.

Admins manage users on the **Users** page. The same actions exist on the command line, which is also
the way back in when no admin can log in:

```bash
docker compose exec backend ./api user add someone@example.com           # role "user"
docker compose exec backend ./api user add boss@example.com --admin
docker compose exec backend ./api user list
docker compose exec backend ./api user passwd someone@example.com        # also signs them out
docker compose exec backend ./api user disable someone@example.com
docker compose exec backend ./api user enable someone@example.com
docker compose exec backend ./api user delete someone@example.com        # --force also deletes their accounts
```

Passwords are read from a hidden prompt or from stdin, never from an argument.

## Operations

```bash
docker compose up -d --build   # start, rebuilding after code changes
docker compose ps              # status and health
docker compose logs -f backend # follow backend logs
docker compose down            # stop; data stays in the volume
```

`docker compose down -v` also deletes the database volume: **all accounts are lost**.

Run these from any folder with `-f /path/to/qamail-manager/docker-compose.yml`. Use `-p <name>` to run a
second, separate stack on the same machine (and change `WEB_PORT`).

### Backup and restore

```bash
# Backup
docker compose exec -T postgres sh -c 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" --no-owner' > backup.sql

# Restore into an empty database
docker compose up -d postgres
docker compose exec -T postgres sh -c 'psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"' < backup.sql
docker compose up -d
```

Schedule the backup with cron and copy the dumps off the machine. Test a restore once before you rely on it.

### Updating

```bash
git pull
docker compose up -d --build
```

Database migrations run when the backend starts. Take a backup first.

## Troubleshooting

| Symptom | Cause and fix |
| ------- | ------------- |
| `Set DB_PASSWORD in .env` when starting | `.env` is missing or `DB_PASSWORD` is empty. Run `./setup.sh`. |
| Backend log: `password authentication failed` | The database volume was created with another `DB_PASSWORD`. Restore the old value, or, if there is nothing to keep, run `docker compose down -v` and start again. |
| `port is already allocated` | Another program uses port 3000. Set `WEB_PORT` in `.env`. |
| Cannot open the app from another computer | The app listens on `127.0.0.1` by default. See [Public HTTPS](#public-https). |
| `Too many failed attempts` at login | The limit is per IP and email (`LOGIN_MAX_ATTEMPTS` per `LOGIN_WINDOW`). Wait the time shown, or restart the backend: the counter lives in memory. |
| Forgot the admin password | `docker compose exec backend ./api user passwd you@example.com` |
| Backend refuses to start: `ADMIN_EMAIL and ADMIN_PASSWORD must be set together` | Set both, or empty both. |
| Backend refuses to start: `INGEST_SECRET is required` | `CATCHALL_DOMAIN` is set. Add a secret (`openssl rand -hex 32`) or empty the domain. |
| New mail does not arrive (own domain) | Check **Settings → Mail Providers**, the Worker logs (`npx wrangler tail`) and that the tunnel hostname points at `http://backend:8081`. |
| Mail.tm accounts fail to generate | Mail.tm rate limits (about 8 requests per second) or is down. Wait and retry. |

## API

Swagger UI is at http://localhost:3000/docs (off in the public HTTPS setup); the spec is
[`backend/docs/openapi.yaml`](backend/docs/openapi.yaml). Failed requests return
`{ "success": false, "code": "...", "message": "...", "params": {...} }`. `message` is English or
Indonesian, chosen from the `Accept-Language` request header (English by default).

## Development

Run the backend and frontend separately with hot reload; see [`backend/README.md`](backend/README.md)
and [`frontend/README.md`](frontend/README.md). Contributions are welcome: read
[`CONTRIBUTING.md`](CONTRIBUTING.md) first.

```bash
cd backend && go build ./... && go test ./...
cd frontend && bun install --frozen-lockfile && bun run lint && bun run test
cd cloudflare/email-worker && npm ci && npm test        # Node 22
```

Some backend tests need a PostgreSQL database; see the backend README. The browser end-to-end test is
described in [`frontend/e2e/README.md`](frontend/e2e/README.md).

## Security

Read [`SECURITY.md`](SECURITY.md) to report a vulnerability and for hardening notes.

## License

[MIT](LICENSE).
