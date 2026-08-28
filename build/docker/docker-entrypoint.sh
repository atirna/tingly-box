#!/bin/sh
# Fixes ownership of the bind-mounted data directory so a freshly created
# host directory (owned by the invoking host user, not the container's
# non-root "tingly" user) doesn't fail with permission denied on first run.
# Only takes effect when the container is started as root (the default);
# `docker run --user ...` is left untouched.
set -e

DATA_DIR="/home/tingly/.tingly-box"

if [ "$(id -u)" = "0" ]; then
    mkdir -p "$DATA_DIR"
    owner="$(stat -c '%u:%g' "$DATA_DIR")"
    want="$(id -u tingly):$(id -g tingly)"
    if [ "$owner" != "$want" ]; then
        chown -R tingly:tingly "$DATA_DIR"
    fi
    exec su-exec tingly "$0" "$@"
fi

exec "$@"
