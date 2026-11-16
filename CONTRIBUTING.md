# Contributing to QAMail Manager

Thanks for helping. Bug reports, fixes, docs and new features are all welcome. For a large change,
open an issue first so we can agree on the approach before you spend time on it.

## Ground rules

- Be respectful and keep discussions on topic.
- One pull request does one thing. Keep it small enough to review.
- Never commit secrets, real email addresses, real domains, database dumps or `.env` files. Use
  `example.com` style placeholders in code, tests, docs and screenshots.
- Report security problems privately, not in a public issue. See [SECURITY.md](SECURITY.md).

## Development setup

You need Go (the version in `backend/go.mod`), [Bun](https://bun.sh) 1.3, Node 22 (Email Worker
tests and end-to-end tests) and Docker.

```bash
git clone https://github.com/reihanrere/qamail-manager.git
cd qamail-manager
```

- **Whole stack in Docker:** `./setup.sh`, then `docker compose up -d --build`.
- **Backend with hot reload and frontend dev server:** see [backend/README.md](backend/README.md) and
  [frontend/README.md](frontend/README.md).

## Running the tests

```bash
cd backend && go build ./... && go vet ./... && go test -p 1 ./...
cd frontend && bun install --frozen-lockfile && bun run lint && bun run build && bun run test
cd cloudflare/email-worker && npm ci && npm test
```

Some backend tests need PostgreSQL and run only when `TEST_DATABASE_DSN` is set. **They empty every
table in that database, so never point them at data you care about.** Use a throwaway container:

```bash
docker run -d --rm --name qamail-test-pg -e POSTGRES_PASSWORD=pw -p 55432:5432 postgres:17-alpine
export TEST_DATABASE_DSN="host=localhost port=55432 user=postgres password=pw dbname=postgres sslmode=disable"
cd backend && go test -p 1 ./...
```

The browser end-to-end test is described in [frontend/e2e/README.md](frontend/e2e/README.md). CI runs
everything above on each pull request.

## Code conventions

- **Go:** `gofmt`, `go vet` clean. Settings with defaults belong in
  `backend/internal/config/config.go` and `.env.example`, not hard-coded. Add a test for new behaviour.
- **Frontend:** `bun run lint` clean, TypeScript strict. Follow the existing folder layout under
  `frontend/src/features`.
- **Two languages.** Every user-facing text exists in English and Indonesian:
  - UI strings: `frontend/src/i18n/locales/en.ts` and `id.ts`.
  - API error messages: add the code to both languages in `backend/internal/i18n/messages.go`. A test
    fails when an emitted error code has no translation or the two languages differ.
- **Deploy-specific values** (domains, hosts, ports, names) are configuration. Do not hard-code them.
- Update the README, `.env.example` or `backend/docs/openapi.yaml` when behaviour or settings change.

## Commits and pull requests

- Use [Conventional Commits](https://www.conventionalcommits.org): `feat:`, `fix:`, `docs:`,
  `refactor:`, `test:`, `chore:`. Keep the subject under about 72 characters and say why in the body.
- Fill in the pull request template. Link the issue it closes.
- Make sure the tests above pass before you ask for a review.

By contributing you agree that your contribution is released under the project's [LICENSE](LICENSE).
