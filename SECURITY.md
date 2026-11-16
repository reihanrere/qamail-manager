# Security policy

## Reporting a vulnerability

Please **do not open a public issue** for a security problem.

Use GitHub's private reporting instead: open the repository's **Security** tab and choose
**Report a vulnerability**. Include what you found, how to reproduce it, the version or commit, and
the impact you expect. Do not include real credentials or other people's data.

You can expect an acknowledgement within a few days. We will keep you updated while we investigate,
fix the problem and publish a release, and we will credit you if you want. Please give us reasonable
time to fix the issue before you disclose it.

## Supported versions

Security fixes go to the latest release and the `main` branch. Older versions are not patched; update
to the latest release.

## Scope

In scope: this repository's backend, frontend, Email Worker, Docker and Compose files, and the
documentation's recommended setup. Typical examples are authentication and session bugs, access to
another user's accounts or mail, injection, XSS from email content, SSRF, and secret leaks.

Out of scope: problems in third-party services (Mail.tm, Cloudflare), issues that need a
compromised server or admin account, denial of service by sheer traffic, and reports on a
deployment that ignores the hardening notes below.

## Hardening notes for operators

- Set a strong `DB_PASSWORD`. The database is not published outside the Docker network; keep it so.
- Serve the app over HTTPS and set `COOKIE_SECURE=true` ([`docker-compose.prod.yml`](docker-compose.prod.yml)
  does both). Keep `WEB_BIND=127.0.0.1` unless a trusted proxy or network is in front.
- Set `TRUSTED_PROXY_CIDR` to your reverse proxy's network, so the login rate limit sees real client
  addresses. The default trusts nobody.
- Use a random `INGEST_SECRET` of 32 or more characters (`openssl rand -hex 32`), and expose only the
  ingest endpoint to the internet, never the rest of the API. Keep the secret out of Git.
- There is no default account. After the first start, empty `ADMIN_EMAIL` and `ADMIN_PASSWORD` in `.env`.
- Keep the Swagger UI (`/docs`) off on public deployments. The production compose file does that.
- Mail content is untrusted. The app renders HTML mail in a sandbox and serves attachments as
  downloads; do not weaken those protections.
- Back up the database, keep the dumps private, and update the images regularly.
