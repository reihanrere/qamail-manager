#!/usr/bin/env sh
# Creates .env from .env.example with a random database password.
# Safe to run twice: an existing .env is never overwritten.
set -eu

cd "$(dirname "$0")"

if [ -f .env ]; then
  echo ".env already exists, leaving it as it is."
  exit 0
fi

if ! command -v openssl >/dev/null 2>&1; then
  echo "openssl is required to generate a password. Install it, or copy .env.example to .env and set DB_PASSWORD yourself." >&2
  exit 1
fi

password=$(openssl rand -hex 24)
umask 077
sed "s/^DB_PASSWORD=.*/DB_PASSWORD=${password}/" .env.example > .env

cat <<'EOF'
Created .env with a random DB_PASSWORD.

Next:
  docker compose up -d --build
  docker compose exec backend ./api user add you@example.com --admin

Then open http://localhost:3000 and log in.
EOF
