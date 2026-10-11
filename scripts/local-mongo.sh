#!/usr/bin/env bash
# Local MongoDB for development. Standalone: it has no connection to any cloud database.
#
#   scripts/local-mongo.sh up      start local mongo (docker, port 27017)
#   scripts/local-mongo.sh down    stop the container (data volume is kept)
#
# Auth is on. Credentials live in .env.local (MONGODB_URI is read-only,
# MONGODB_ADMIN_URI is the admin user).
set -euo pipefail

NAME=local-mongo
IMAGE=mongo:7
PORT=${LOCAL_MONGO_PORT:-27017}
LOCAL_URI="mongodb://localhost:${PORT}"

up() {
  if docker ps -a --format '{{.Names}}' | grep -qx "$NAME"; then
    docker start "$NAME" >/dev/null
  else
    docker run -d --name "$NAME" -p "127.0.0.1:${PORT}:27017" \
      -v local-mongo-data:/data/db "$IMAGE" --auth >/dev/null
  fi
  until docker exec "$NAME" mongosh --quiet --eval 'db.runCommand({ping:1}).ok' >/dev/null 2>&1; do
    sleep 1
  done
  echo "local mongo ready at $LOCAL_URI"
}

down() {
  docker stop "$NAME" >/dev/null && echo "stopped $NAME"
}

case "${1:-}" in
  up) up ;;
  down) down ;;
  *) echo "usage: $0 {up|down}" >&2; exit 1 ;;
esac
