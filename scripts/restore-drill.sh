#!/usr/bin/env bash
# Creates and destroys its own kind cluster. Never uses the current kube context.
set -euo pipefail
umask 077
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
scratch=$(mktemp -d)
cluster="justcd-restore-$$"
created=false
forward_pid=''
if command -v docker >/dev/null && docker info >/dev/null 2>&1; then
  engine=docker
elif command -v podman >/dev/null && podman info >/dev/null 2>&1; then
  engine=podman
  export KIND_EXPERIMENTAL_PROVIDER=podman
else
  echo 'Docker or Podman is required' >&2; exit 1
fi
for tool in kind kubectl helm go openssl rg age age-keygen; do command -v "$tool" >/dev/null; done
cleanup() {
  status=$?
  trap - EXIT
  if [[ -n "$forward_pid" ]]; then kill "$forward_pid" 2>/dev/null || true; wait "$forward_pid" 2>/dev/null || true; fi
  if [[ "$created" == true ]]; then kind delete cluster --name "$cluster" >/dev/null 2>&1 || true; fi
  rm -rf "$scratch"
  exit "$status"
}
trap cleanup EXIT
backend_image=${JUSTCD_DRILL_BACKEND_IMAGE:-"localhost/justcd-drill-backend:local"}
frontend_image=${JUSTCD_DRILL_FRONTEND_IMAGE:-"localhost/justcd-drill-frontend:local"}
if [[ -z "${JUSTCD_DRILL_BACKEND_IMAGE:-}" ]]; then "$engine" build -t "$backend_image" "$root/services/backend"; fi
if [[ -z "${JUSTCD_DRILL_FRONTEND_IMAGE:-}" ]]; then "$engine" build -t "$frontend_image" "$root/services/frontend"; fi
created=true
kind create cluster --name "$cluster" --image kindest/node:v1.37.0 --wait 5m --kubeconfig "$scratch/kubeconfig"
# Every Kubernetes/Helm command is pinned to this disposable cluster.
export KUBECONFIG="$scratch/kubeconfig"
for image in "$backend_image" "$frontend_image"; do
  "$engine" save -o "$scratch/image.tar" "$image"
  kind load image-archive --name "$cluster" "$scratch/image.tar"
done
kubectl create namespace justcd
kubectl -n justcd create secret generic postgres-password --from-literal=password=synthetic-drill-only
kubectl apply -f - <<'YAML'
apiVersion: v1
kind: Pod
metadata:
  name: postgres
  namespace: justcd
spec:
  containers:
    - name: postgres
      image: docker.io/library/postgres:16
      env:
        - name: POSTGRES_PASSWORD
          valueFrom:
            secretKeyRef:
              name: postgres-password
              key: password
        - name: POSTGRES_DB
          value: justcd
      readinessProbe:
        exec:
          command: [pg_isready, -U, postgres]
      ports:
        - containerPort: 5432
---
apiVersion: v1
kind: Service
metadata:
  name: postgres
  namespace: justcd
spec:
  selector: {app: postgres}
  ports: [{port: 5432}]
YAML
kubectl -n justcd label pod postgres app=postgres
kubectl -n justcd wait --for=condition=Ready pod/postgres --timeout=180s
key=$(openssl rand -hex 32)
printf 'JUSTCD_DATABASE_URL=postgres://postgres:synthetic-drill-only@postgres:5432/justcd?sslmode=disable\nJUSTCD_ENCRYPTION_KEY=%s\n' "$key" > "$scratch/backend.env"
kubectl -n justcd create secret generic justcd-config --from-env-file="$scratch/backend.env"
helm upgrade --install justcd "$root/charts/justcd" -n justcd \
  --set publicUrl=http://justcd.invalid --set cookieSecure=false \
  --set backend.existingSecret=justcd-config \
  --set backend.image.repository="${backend_image%:*}" --set backend.image.tag="${backend_image##*:}" \
  --set frontend.image.repository="${frontend_image%:*}" --set frontend.image.tag="${frontend_image##*:}" \
  --wait --timeout 5m
kubectl -n justcd port-forward service/postgres :5432 > "$scratch/port-forward.log" 2>&1 &
forward_pid=$!
for attempt in {1..60}; do
  if rg -q 'Forwarding from 127.0.0.1:' "$scratch/port-forward.log"; then break; fi
  sleep 1
done
port=$(sed -n 's/Forwarding from 127.0.0.1:\([0-9]*\).*/\1/p' "$scratch/port-forward.log" | head -1)
[[ "$port" =~ ^[0-9]+$ ]]
export JUSTCD_RESTORE_DRILL=disposable JUSTCD_ENCRYPTION_KEY="$key"
export JUSTCD_DATABASE_URL="postgres://postgres:synthetic-drill-only@127.0.0.1:$port/justcd?sslmode=disable"
(cd "$root/services/backend" && go build -o "$scratch/fixture" ./cmd/restore-drill)
# Quiesce before the coordinated backup; wait for every backend pod to exit.
kubectl -n justcd scale deployment justcd-backend --replicas=0
kubectl -n justcd wait --for=delete pod -l app.kubernetes.io/component=backend --timeout=90s
"$scratch/fixture" seed > "$scratch/before.sha256"
kubectl -n justcd exec postgres -- pg_dump -U postgres -d justcd --format=custom --no-owner --no-acl > "$scratch/justcd.dump"
# Separate the key backup from database archive. These files never leave scratch.
printf '%s' "$key" > "$scratch/encryption-key"
chmod 600 "$scratch/encryption-key"
# Separate recipients/identities exercise independent database/key backup access.
age-keygen -o "$scratch/database-identity" > /dev/null 2>&1
age-keygen -o "$scratch/key-identity" > /dev/null 2>&1
recipient=$(age-keygen -y "$scratch/database-identity")
key_recipient=$(age-keygen -y "$scratch/key-identity")
age --recipient "$recipient" --output "$scratch/justcd.dump.age" "$scratch/justcd.dump"
age --recipient "$key_recipient" --output "$scratch/encryption-key.age" "$scratch/encryption-key"
age --decrypt --identity "$scratch/database-identity" --output "$scratch/recovered.dump" "$scratch/justcd.dump.age"
age --decrypt --identity "$scratch/key-identity" --output "$scratch/recovered-key" "$scratch/encryption-key.age"
cmp "$scratch/justcd.dump" "$scratch/recovered.dump"
cmp "$scratch/encryption-key" "$scratch/recovered-key"
kubectl -n justcd exec postgres -- createdb -U postgres justcd_restored
kubectl -n justcd exec -i postgres -- pg_restore -U postgres -d justcd_restored --no-owner --no-acl --exit-on-error --single-transaction < "$scratch/recovered.dump"
export JUSTCD_DATABASE_URL="postgres://postgres:synthetic-drill-only@127.0.0.1:$port/justcd_restored?sslmode=disable"
export JUSTCD_ENCRYPTION_KEY=$(cat "$scratch/recovered-key")
"$scratch/fixture" verify > "$scratch/after.sha256"
cmp "$scratch/before.sha256" "$scratch/after.sha256"
JUSTCD_ENCRYPTION_KEY=$(openssl rand -hex 32) "$scratch/fixture" wrong-key
# Execute the exact cold-restore quarantine procedure before enabling workers.
kubectl -n justcd exec -i postgres -- psql -U postgres -d justcd_restored -v ON_ERROR_STOP=1 < "$root/docs/operations/quarantine-restored.sql"
[[ $(kubectl -n justcd exec postgres -- psql -U postgres -d justcd_restored -Atc "SELECT count(*) FROM operations WHERE status IN ('queued','running')") == 0 ]]
[[ $(kubectl -n justcd exec postgres -- psql -U postgres -d justcd_restored -Atc "SELECT jsonb_array_length(progress->'completed') FROM operations WHERE id='dr-running' AND status='failed'") == 1 ]]
[[ $(kubectl -n justcd exec postgres -- psql -U postgres -d justcd_restored -Atc "SELECT count(*) FROM plans WHERE status='current'") == 0 ]]
"$scratch/fixture" verify > "$scratch/quarantined.sha256"
# Restore the same Helm release against the replacement database and original key.
printf 'JUSTCD_DATABASE_URL=postgres://postgres:synthetic-drill-only@postgres:5432/justcd_restored?sslmode=disable\nJUSTCD_ENCRYPTION_KEY=%s\n' "$JUSTCD_ENCRYPTION_KEY" > "$scratch/backend.env"
kubectl -n justcd create secret generic justcd-config --from-env-file="$scratch/backend.env" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
kubectl -n justcd scale deployment justcd-backend --replicas=1
kubectl -n justcd rollout status deployment/justcd-backend --timeout=180s
kubectl -n justcd exec deployment/justcd-backend -- wget -qO- http://127.0.0.1:8080/healthz
"$scratch/fixture" verify > "$scratch/after-start.sha256"
cmp "$scratch/quarantined.sha256" "$scratch/after-start.sha256"
# Validate both RBAC recipes, including denied cross-namespace/cluster permissions.
kubectl apply -f "$root/docs/operations/rbac-namespaced.yaml"
kubectl apply -f "$root/docs/operations/rbac-cluster-scoped.yaml"
identity=system:serviceaccount:justcd-target:justcd-deployer
[[ $(kubectl auth can-i create configmaps -n justcd-target --as="$identity") == yes ]]
[[ $(kubectl auth can-i create configmaps -n default --as="$identity" || true) == no ]]
[[ $(kubectl auth can-i create namespaces --as="$identity" || true) == no ]]
[[ $(kubectl auth can-i create clusterroles --as="$identity" || true) == no ]]
identity=system:serviceaccount:justcd-target:justcd-cluster-deployer
[[ $(kubectl auth can-i create namespaces --as="$identity") == yes ]]
[[ $(kubectl auth can-i create clusterroles --as="$identity" || true) == no ]]
echo 'PASS: Helm deployment restored; Git/Kubernetes credential decryption, exact ciphertext/history/plan/operation preservation, incorrect-key rejection, and scoped RBAC verified.'
