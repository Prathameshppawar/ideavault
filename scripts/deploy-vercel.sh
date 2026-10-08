#!/usr/bin/env bash
# Deploy the IdeaVault web app to Vercel (apps/web). Requires `vercel login` (once).
#   API_ORIGIN=https://ideavault-api-production.up.railway.app ./scripts/deploy-vercel.sh
set -euo pipefail

VERCEL="${VERCEL:-npx -y vercel@latest}"
PROJECT="${PROJECT:-ideavault}"
: "${API_ORIGIN:?Set API_ORIGIN to the public URL of the API (Railway domain)}"

cd "$(dirname "$0")/../apps/web"
$VERCEL whoami >/dev/null
if [[ ! -f .vercel/project.json ]]; then
  $VERCEL link --yes --project "$PROJECT"
fi
# API_ORIGIN is a server-side variable used by the /api rewrite; it is never sent to browsers.
$VERCEL env rm API_ORIGIN production --yes >/dev/null 2>&1 || true
printf '%s' "$API_ORIGIN" | $VERCEL env add API_ORIGIN production
$VERCEL deploy --prod --yes
