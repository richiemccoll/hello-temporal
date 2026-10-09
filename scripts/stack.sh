#!/usr/bin/env bash
# Runs the whole local stack natively (no Docker): Temporal dev server, Envoy, worker, API.
# Usage: scripts/stack.sh up|down|status|logs
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RUN="$ROOT/.run"
mkdir -p "$RUN"

start() { # name, command...
  local name=$1; shift
  if [[ -f "$RUN/$name.pid" ]] && kill -0 "$(cat "$RUN/$name.pid")" 2>/dev/null; then
    echo "  $name already running (pid $(cat "$RUN/$name.pid"))"; return
  fi
  nohup "$@" </dev/null >"$RUN/$name.log" 2>&1 &
  echo $! >"$RUN/$name.pid"
  echo "  started $name (pid $!, log .run/$name.log)"
}

wait_for() { # description, command...
  local desc=$1; shift
  for _ in $(seq 1 60); do
    if "$@" >/dev/null 2>&1; then echo "  ready: $desc"; return; fi
    sleep 0.5
  done
  echo "  timed out waiting for $desc" >&2; exit 1
}

up() {
  echo "building binaries..."
  (cd "$ROOT" && go build -o "$RUN/bin/api" ./cmd/api && go build -o "$RUN/bin/worker" ./cmd/worker)

  start temporal temporal server start-dev --ip 127.0.0.1 --port 7233 --ui-port 8233 --log-level warn
  wait_for "temporal :7233" temporal operator cluster health --address 127.0.0.1:7233

  # Start dependencies before the proxy so Envoy's first active health check passes.
  start worker "$RUN/bin/worker"
  start api "$RUN/bin/api"
  wait_for "api :8081" curl -sf -X POST -H 'Content-Type: application/json' -d '{}' \
    http://127.0.0.1:8081/grpc.health.v1.Health/Check

  start envoy envoy -c "$ROOT/deploy/envoy/envoy.yaml" --log-level warn
  wait_for "envoy admin :9901" curl -sf http://127.0.0.1:9901/ready
  wait_for "api via envoy :9080" curl -sf -X POST -H 'Content-Type: application/json' -d '{}' \
    http://127.0.0.1:9080/grpc.health.v1.Health/Check

  cat <<MSG

Stack is up:
  Envoy ingress   http://127.0.0.1:9080   (Connect, gRPC, gRPC-Web)
  Envoy egress    http://127.0.0.1:10000  (-> https://api.thecatapi.com)
  Envoy admin     http://127.0.0.1:9901
  API (direct)    http://127.0.0.1:8081
  Temporal        127.0.0.1:7233, UI http://127.0.0.1:8233
MSG
}

down() {
  for name in api worker envoy temporal; do
    if [[ -f "$RUN/$name.pid" ]]; then
      kill "$(cat "$RUN/$name.pid")" 2>/dev/null && echo "  stopped $name" || true
      rm -f "$RUN/$name.pid"
    fi
  done
}

status() {
  for name in temporal envoy worker api; do
    if [[ -f "$RUN/$name.pid" ]] && kill -0 "$(cat "$RUN/$name.pid")" 2>/dev/null; then
      echo "  $name: running (pid $(cat "$RUN/$name.pid"))"
    else
      echo "  $name: stopped"
    fi
  done
}

case "${1:-}" in
  up) up ;;
  down) down ;;
  status) status ;;
  logs) tail -n 50 -f "$RUN"/*.log ;;
  *) echo "usage: $0 up|down|status|logs" >&2; exit 2 ;;
esac
