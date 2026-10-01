#!/usr/bin/env bash
set -euo pipefail

mode=${1:-smoke}
if [[ "$mode" != smoke && "$mode" != full ]]; then
  echo "Usage: scripts/integration.sh [smoke|full]" >&2
  exit 2
fi

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
artifacts=${JUSTCD_E2E_ARTIFACTS:-"$root/artifacts/e2e"}
mkdir -p "$artifacts"
scratch=$(mktemp -d)
container="justcd-e2e-pg-$$"
cluster="justcd-e2e-$$"
cluster_created=false

if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
  engine=docker
elif command -v podman >/dev/null 2>&1 && podman info >/dev/null 2>&1; then
  engine=podman
  export KIND_EXPERIMENTAL_PROVIDER=podman
else
  echo "Docker or Podman is required." >&2
  exit 1
fi

cleanup() {
  status=$?
  trap - EXIT
  if [[ "$cluster_created" == true ]]; then
    if [[ "$status" -ne 0 ]]; then
      kind export logs --name "$cluster" "$artifacts/kind" >/dev/null 2>&1 || true
    fi
    kind delete cluster --name "$cluster" >/dev/null 2>&1 || true
  fi
  "$engine" rm -f "$container" >/dev/null 2>&1 || true
  rm -rf "$scratch"
  exit "$status"
}
trap cleanup EXIT

"$engine" run --rm -d --name "$container" \
  -e POSTGRES_PASSWORD=e2eonly -e POSTGRES_DB=justcd_e2e \
  -p 127.0.0.1::5432 docker.io/library/postgres:16 >/dev/null
port=$("$engine" port "$container" 5432/tcp | head -1 | awk -F: '{print $NF}')
if [[ ! "$port" =~ ^[0-9]+$ ]]; then
  echo "Could not determine the isolated PostgreSQL port." >&2
  exit 1
fi
for attempt in {1..45}; do
  if "$engine" exec "$container" pg_isready -U postgres >/dev/null 2>&1; then
    break
  fi
  if [[ "$attempt" == 45 ]]; then
    echo "PostgreSQL did not become ready." >&2
    exit 1
  fi
  sleep 1
done
export JUSTCD_E2E_DATABASE_URL="postgres://postgres:e2eonly@127.0.0.1:$port/justcd_e2e?sslmode=disable"

cd "$root/services/backend"
go test -tags integration ./internal/store -run '^TestIntegrationMigrationUpgradeAndPoller$' -count=1 -v -timeout 2m 2>&1 | tee "$artifacts/database.log"

go test -tags integration ./internal/syncer -run '^TestIntegrationRepositoryConfiguration$' -count=1 -v -timeout 2m 2>&1 | tee "$artifacts/repository.log"

go test -tags integration ./internal/api -run '^TestIntegrationClusterAgent$' -count=1 -v -timeout 2m 2>&1 | tee "$artifacts/agents.log"

go test -tags integration ./internal/api -run '^TestIntegrationPRCredentialsAndCommentApprovals$' -count=1 -v -timeout 2m 2>&1 | tee "$artifacts/pr-workflow.log"

if [[ "$mode" == full ]]; then
  if ! command -v kind >/dev/null 2>&1; then
    echo "Install kind to run the full cluster suite." >&2
    exit 1
  fi
  kind create cluster --name "$cluster" --wait 5m --kubeconfig "$scratch/kubeconfig" 2>&1 | tee "$artifacts/kind-create.log"
  cluster_created=true
  export JUSTCD_E2E_KUBECONFIG="$scratch/kubeconfig"
  go test -tags integration ./internal/syncer -run '^TestIntegrationDeliveryAgainstKind$' -count=1 -v -timeout 7m 2>&1 | tee "$artifacts/delivery.log"
fi
