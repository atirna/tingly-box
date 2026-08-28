#!/usr/bin/env bash
# Regression guard for #1662: a freshly `mkdir`ed host directory bind-mounted
# into a Tingly Box image, owned by the invoking host user (not the image's
# non-root "tingly", UID/GID 999), must come up without a manual `chown`.
#
# Runs <image> against exactly that kind of directory and fails if the
# container dies, its logs mention a permission error, or the entrypoint
# never actually writes into the mount (memory/logs/db all nest under it,
# see internal/config/app_config.go).
#
# Usage:
#   docker-smoke-test.sh <image> <container-data-dir>
#
#   <image>              image tag to run, e.g. tingly-box-smoke:build
#   <container-data-dir> bind-mount target inside the container, e.g.
#                        /home/tingly/.tingly-box or /app/.tingly-box
set -euo pipefail

IMAGE="${1:?usage: docker-smoke-test.sh <image> <container-data-dir>}"
DATA_DIR="${2:?usage: docker-smoke-test.sh <image> <container-data-dir>}"

HOST_DIR="$(mktemp -d)"
CONTAINER="tingly-box-smoke-$$"

cleanup() {
    docker logs "$CONTAINER" 2>&1 | tail -100 || true
    docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
    rm -rf "$HOST_DIR"
}
trap cleanup EXIT

echo "== host dir before run =="
ls -ld "$HOST_DIR"

# Deliberately NOT chowned — this is the exact shape of the bug: a plain
# `mkdir` on the host, owned by whoever ran it (the CI runner user here,
# UID/GID 999 the container's "tingly" in the reported issue).
docker run -d --name "$CONTAINER" -p 12580:12580 \
    -v "$HOST_DIR:$DATA_DIR" \
    "$IMAGE"

echo "== waiting for the entrypoint to fix ownership and the app to write into the mount =="
ok=0
for _ in $(seq 1 30); do
    if ! docker inspect -f '{{.State.Running}}' "$CONTAINER" 2>/dev/null | grep -q true; then
        echo "FAIL: container exited early" >&2
        break
    fi
    if docker logs "$CONTAINER" 2>&1 | grep -qi "permission denied"; then
        echo "FAIL: 'permission denied' in container logs" >&2
        break
    fi
    # Anything landing in the mount (config.json, memory/, logs/, db/) proves
    # the entrypoint's chown + privilege-drop actually let the app write here.
    if [ -n "$(ls -A "$HOST_DIR" 2>/dev/null)" ]; then
        ok=1
        break
    fi
    sleep 2
done

echo "== host dir after run =="
ls -la "$HOST_DIR"

if [ "$ok" != "1" ]; then
    echo "FAIL: nothing was written to $HOST_DIR (bind-mounted at $DATA_DIR) within the timeout" >&2
    exit 1
fi

echo "OK: $IMAGE wrote into a bind mount it did not own at container start"
