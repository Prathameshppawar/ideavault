#!/usr/bin/env bash
# Deploy the IdeaVault API to Railway: a pgvector PostgreSQL service with a volume,
# and the API service built from apps/api/Dockerfile (see railway.toml).
#
# Prerequisites: `railway login` (once). Run from the repository root:
#   WEB_ORIGIN=https://ideavault.vercel.app GROQ_API_KEY=... ./scripts/deploy-railway.sh
# Re-running is safe: existing services are reused and variables are updated.
set -euo pipefail

RAILWAY="${RAILWAY:-npx -y @railway/cli@latest}"
PROJECT_NAME="${PROJECT_NAME:-ideavault}"
API_SERVICE="${API_SERVICE:-ideavault-api}"
DB_SERVICE="${DB_SERVICE:-postgres}"
WEB_ORIGIN="${WEB_ORIGIN:-https://ideavault.vercel.app}"
STATE_FILE=".railway-deploy.env"   # gitignored (.env.*): keeps generated secrets stable across runs

cd "$(dirname "$0")/.."
$RAILWAY whoami >/dev/null || { echo "Run 'railway login' first." >&2; exit 1; }

# Stable generated secrets.
if [[ -f "$STATE_FILE" ]]; then source "$STATE_FILE"; fi
DB_PASSWORD="${DB_PASSWORD:-$(openssl rand -hex 24)}"
MASTER_KEY="${MASTER_KEY:-$(openssl rand -base64 32)}"
cat > "$STATE_FILE" <<EOF
DB_PASSWORD=$DB_PASSWORD
MASTER_KEY=$MASTER_KEY
EOF
chmod 600 "$STATE_FILE"

# 1. Project
if ! $RAILWAY status >/dev/null 2>&1; then
  $RAILWAY init --name "$PROJECT_NAME"
fi

# 2. PostgreSQL with pgvector (official pgvector image) + persistent volume
if ! $RAILWAY variable list --service "$DB_SERVICE" >/dev/null 2>&1; then
  $RAILWAY add --service "$DB_SERVICE" --image pgvector/pgvector:pg17 \
    --variables "POSTGRES_USER=ideavault" \
    --variables "POSTGRES_PASSWORD=$DB_PASSWORD" \
    --variables "POSTGRES_DB=ideavault" \
    --variables "PGDATA=/var/lib/postgresql/data/pgdata"
  $RAILWAY volume --service "$DB_SERVICE" add --mount-path /var/lib/postgresql/data
fi

# 3. API service
if ! $RAILWAY variable list --service "$API_SERVICE" >/dev/null 2>&1; then
  $RAILWAY add --service "$API_SERVICE"
fi
$RAILWAY variable set --service "$API_SERVICE" --skip-deploys \
  "APP_ENV=production" \
  "DATABASE_URL=postgres://ideavault:${DB_PASSWORD}@${DB_SERVICE}.railway.internal:5432/ideavault?sslmode=disable" \
  "MASTER_KEY=$MASTER_KEY" \
  "WEB_ORIGINS=$WEB_ORIGIN" \
  "COOKIE_SECURE=true" \
  "LOG_FORMAT=json" \
  "EMBEDDING_PROVIDER=auto"
for key in GROQ_API_KEY OPENAI_API_KEY ANTHROPIC_API_KEY GEMINI_API_KEY; do
  if [[ -n "${!key:-}" ]]; then
    printf '%s' "${!key}" | $RAILWAY variable set --service "$API_SERVICE" --skip-deploys --stdin "$key"
  fi
done

# 4. Deploy (Dockerfile build from repo root) and expose a public domain
$RAILWAY up --service "$API_SERVICE" --detach --ci
$RAILWAY domain --service "$API_SERVICE" --port 8080 || true
echo
echo "API deployed. Set the web app's API_ORIGIN to the domain above (https://…up.railway.app)."
