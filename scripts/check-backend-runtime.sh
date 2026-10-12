#!/bin/sh
set -eu

engine=${1:-docker}
image=${2:-justcd-backend-runtime-test}
server="justcd-runtime-http-$$"
cleanup() {
    result=$?
    if test "$result" -ne 0; then
        "$engine" logs "$server" >&2 || true
    fi
    "$engine" rm -f "$server" >/dev/null 2>&1 || true
}
trap cleanup EXIT

# Serve the fixture in a separate container so the production image needs no
# HTTP server, test tools or root privileges. No host port is exposed.
"$engine" run -d --name "$server" alpine:3.24.2 sh -ec '
    apk add --no-cache git busybox-extras
    git init -q /tmp/source
    printf "runtime check\n" > /tmp/source/marker
    git -C /tmp/source add marker
    git -C /tmp/source -c user.name=RuntimeCheck -c user.email=runtime@example.invalid commit -qm "Runtime check"
    mkdir -p /srv
    git clone -q --bare /tmp/source /srv/repository.git
    git -C /srv/repository.git update-server-info
    exec busybox-extras httpd -f -p 18080 -h /srv
' >/dev/null

"$engine" run --rm -i --network "container:$server" --entrypoint sh "$image" -eu <<'CHECK'
version=$(apk list --installed libexpat | awk '$1 ~ /^libexpat-/ {sub(/^libexpat-/, "", $1); print $1}')
test -n "$version"
test "$(apk version -t "$version" 2.9.0-r0)" != '<'

# The Git WebDAV helper is the binary that links to Expat.
ldd /usr/libexec/git-core/git-http-push > /tmp/git-libraries
grep 'libexpat.so.1' /tmp/git-libraries
if grep 'not found' /tmp/git-libraries; then
    exit 1
fi

scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
attempt=0
until wget -q -O /dev/null http://127.0.0.1:18080/repository.git/HEAD 2>/dev/null; do
    attempt=$((attempt + 1))
    test "$attempt" -lt 30
    sleep 1
done
git -c http.lowSpeedLimit=1 -c http.lowSpeedTime=10 clone -q http://127.0.0.1:18080/repository.git "$scratch/checkout"
printf 'runtime check\n' | cmp - "$scratch/checkout/marker"
printf 'Git HTTP clone passed with libexpat %s\n' "$version"
CHECK
