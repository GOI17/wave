#!/bin/sh
set -eu

cd "$(dirname "$0")/.."
if ! command -v docker >/dev/null 2>&1; then
    echo 'Docker is required. This command never falls back to running tests on the host.' >&2
    exit 1
fi
if ! docker info >/dev/null 2>&1; then
    echo 'Start Docker Desktop, then run make test-sandbox again. No host tests were run.' >&2
    exit 1
fi

docker build --file Dockerfile.test --tag wave-tests:local .
# No host mounts, credentials, sockets, network, or elevated privileges.
# All writable test data disappears when the container exits.
docker run --rm --network none --read-only --cap-drop ALL \
    --security-opt no-new-privileges=true \
    --tmpfs /tmp:rw,exec,nosuid,nodev,size=1g,mode=1777 \
    --tmpfs /home/wave:rw,nosuid,nodev,size=64m,uid=10001,gid=10001,mode=700 \
    wave-tests:local "$@"
