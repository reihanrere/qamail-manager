# End-to-end tests

Drive the real app in Google Chrome against the running Docker stack:
log in (an unauthenticated visit must land on the login page and return after signing in),
generate an account in the UI, deliver an OTP email through the ingest endpoint, check it appears live, copy the code, and open the expanded message view.
A second test (`users.e2e.ts`) has an admin add a user on the Users page, checks that user sees only
their own accounts, then disables (login refused) and deletes them.
Playwright needs Node 22+.

The test delivers mail through the ingest endpoint, so the stack must use the own-domain provider and
publish the ingest port on loopback. In `.env` set `MAIL_PROVIDER=local`, `CATCHALL_DOMAIN=e2e.example.test`
and `INGEST_SECRET` (32+ characters), then:

```bash
# from the project root; the override publishes the ingest port on 127.0.0.1:8081
docker compose -f docker-compose.yml -f .github/e2e.compose.yml up -d --build
docker compose exec backend ./api user add e2e@example.com   # once; any role works
docker compose exec backend ./api user add e2e-admin@example.com --admin   # once; for the Users test
cd frontend && E2E_EMAIL=e2e@example.com E2E_PASSWORD=... \
  E2E_ADMIN_EMAIL=e2e-admin@example.com E2E_ADMIN_PASSWORD=... bun run test:e2e
```

| Variable                                 | Default                                                                                                                                              |
| ---------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| `E2E_BASE_URL`                           | `http://localhost:3000`                                                                                                                              |
| `E2E_INGEST_URL`                         | `http://localhost:8081/api/ingest`                                                                                                       |
| `INGEST_SECRET`                          | read from the project's `.env`                                                                                                                       |
| `E2E_EMAIL` / `E2E_PASSWORD`             | an existing user; read from the environment or the project's `.env` (no default, the test is skipped without them)                                   |
| `E2E_ADMIN_EMAIL` / `E2E_ADMIN_PASSWORD` | an existing **admin**, for the Users test (skipped without them): it adds a user, checks they see only their own accounts, disables and deletes them |
| `E2E_HEADED=1`                           | show the browser                                                                                                                                     |

Accounts created by the test are deleted afterwards (the test logs in again to do that, because accounts belong to their owner). Do not put a real admin password in a file that is committed.
