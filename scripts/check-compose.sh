#!/usr/bin/env bash
set -euo pipefail

# Test prebuilt service images in a disposable Compose project/database.
# CI builds these names first; overrides allow testing other local images.
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
scratch=$(mktemp -d)
project="justcd-compose-test-$$"
compose() {
  if [[ -n "${JUSTCD_COMPOSE_BIN:-}" ]]; then
    "$JUSTCD_COMPOSE_BIN" "$@"
  else
    docker compose "$@"
  fi
}
stack() {
  compose --project-name "$project" --env-file "$scratch/test.env" \
    -f "$root/compose.yaml" -f "$scratch/images.yaml" --profile docs "$@"
}
cleanup() {
  status=$?
  trap - EXIT
  if [[ "$status" -ne 0 ]]; then
    stack logs --tail=100 >&2 || true
  fi
  stack down --volumes --remove-orphans >/dev/null 2>&1 || true
  rm -rf "$scratch"
  exit "$status"
}
trap cleanup EXIT

cat > "$scratch/test.env" <<EOF
JUSTCD_POSTGRES_PASSWORD=$(openssl rand -hex 32)
JUSTCD_ENCRYPTION_KEY=$(openssl rand -hex 32)
JUSTCD_PUBLIC_URL=http://localhost:3000
JUSTCD_COOKIE_SECURE=false
JUSTCD_BIND_ADDRESS=127.0.0.1
JUSTCD_PORT=0
JUSTCD_DOCS_PORT=0
JUSTCD_DOCS_URL=https://docs.example.invalid
EOF
cat > "$scratch/images.yaml" <<'EOF'
services:
  docs:
    image: ${JUSTCD_COMPOSE_DOCS_IMAGE:-justcd-docs:local}
  backend:
    image: ${JUSTCD_COMPOSE_BACKEND_IMAGE:-justcd-backend:local}
  frontend:
    image: ${JUSTCD_COMPOSE_FRONTEND_IMAGE:-justcd-frontend:local}
EOF

# Check both the published-image configuration and source-build override.
compose --env-file "$scratch/test.env" -f "$root/compose.yaml" config --quiet
compose --env-file "$scratch/test.env" -f "$root/compose.yaml" \
  -f "$root/compose.build.yaml" config --quiet
# Documentation has no backend or database dependencies.
stack up -d --wait --wait-timeout 180 --pull missing docs
address=$(stack port docs 3000)
curl --fail --silent --show-error "http://$address/healthz" > /dev/null
curl --fail --silent --show-error "http://$address/guides/workflow" > /dev/null
curl --fail --silent --show-error "http://$address/api/search?query=deployment" \
  | python3 -c 'import json,sys; assert len(json.load(sys.stdin)) > 0'
stack up -d --wait --wait-timeout 180 --pull missing
address=$(stack port frontend 3000)
origin="http://$address"
curl --fail --silent --show-error "$origin/healthz" > /dev/null
curl --fail --silent --show-error "$origin/api/v1/health" > /dev/null
curl --fail --silent --show-error "$origin/signup" > /dev/null
# The frontend resolves the public docs destination from runtime configuration.
curl --silent --show-error --dump-header "$scratch/docs-redirect.headers" \
  --output /dev/null "$origin/docs/guides/repository-applications"
python3 - "$scratch/docs-redirect.headers" <<'PYDOCS'
import email.parser
import pathlib
import sys
status, headers = pathlib.Path(sys.argv[1]).read_text().split('\n', 1)
assert '307' in status, status
headers = email.parser.Parser().parsestr(headers)
assert headers['Location'] == 'https://docs.example.invalid/guides/repository-applications'
assert headers['Cache-Control'] == 'no-store'
PYDOCS

curl --fail --silent --show-error "$origin/api/v1/auth/setup" \
  | python3 -c 'import json,sys; assert json.load(sys.stdin)["signupAvailable"] is True'

cat > "$scratch/admin.json" <<'EOF'
{"email":"compose@example.invalid","displayName":"Compose smoke test","password":"Compose-smoke-test-password-123!"}
EOF
curl --fail --silent --show-error -H 'Content-Type: application/json' \
  --data-binary "@$scratch/admin.json" "$origin/api/v1/auth/signup" > /dev/null

# Remove all containers and the network while preserving the named DB volume.
stack down
stack up -d --wait --wait-timeout 180 --pull missing
address=$(stack port frontend 3000)
origin="http://$address"
curl --fail --silent --show-error "$origin/api/v1/auth/setup" \
  | python3 -c 'import json,sys; assert json.load(sys.stdin)["signupAvailable"] is False'
cat > "$scratch/login.json" <<'EOF'
{"email":"compose@example.invalid","password":"Compose-smoke-test-password-123!"}
EOF
curl --fail --silent --show-error -H 'Content-Type: application/json' \
  --data-binary "@$scratch/login.json" "$origin/api/v1/auth/login" > /dev/null
echo 'Docs standalone startup/search, Compose startup, API proxy, signup, persisted administrator, and login passed.'
