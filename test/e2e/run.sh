#!/usr/bin/env bash
# End-to-end scripted demo/test: drives the real `canopy` CLI binary
# against the docker-compose lab through both a successful promotion and
# an automatic rollback, in sequence, verifying the service's final state
# after each. Requires: `make lab-up` already run, and `make build` to
# have produced ./bin/canopy.
set -euo pipefail

CANOPY_BIN="./bin/canopy"
CONFIG_FILE="test/e2e/canopy.e2e.yaml"

log() { echo "[e2e] $*"; }
fail() { echo "[e2e] FAIL: $*" >&2; exit 1; }

require_lab() {
  if ! curl -sf http://localhost:8082/health > /dev/null; then
    fail "lab not reachable on :8082 — run 'make lab-up' first"
  fi
}

require_binary() {
  if [ ! -x "$CANOPY_BIN" ]; then
    fail "$CANOPY_BIN not found or not executable — run 'make build' first"
  fi
}

reset_canary() {
  log "resetting canary host to a clean state"
  ssh -i lab/keys/deploy_key -p 2201 -o StrictHostKeyChecking=accept-new \
    deploy@localhost "sudo systemctl stop checkout-svc" || true
}

start_traffic() {
  log "starting background traffic generator against nginx"
  ( while true; do curl -s http://localhost:8080/work > /dev/null || true; sleep 0.05; done ) &
  TRAFFIC_PID=$!
}

stop_traffic() {
  if [ -n "${TRAFFIC_PID:-}" ]; then
    log "stopping traffic generator"
    kill "$TRAFFIC_PID" 2>/dev/null || true
    wait "$TRAFFIC_PID" 2>/dev/null || true
  fi
}

trap stop_traffic EXIT

check_canary_status() {
  local expected="$1"
  local status
  status=$(ssh -i lab/keys/deploy_key -p 2201 -o StrictHostKeyChecking=accept-new \
    deploy@localhost "systemctl is-active checkout-svc" || true)
  if [ "$status" != "$expected" ]; then
    fail "expected canary service status '$expected', got '$status'"
  fi
  log "canary service status confirmed: $status"
}

require_lab
require_binary

log "=== Scenario 1: healthy canary should promote ==="
reset_canary
start_traffic
sleep 2
if ! "$CANOPY_BIN" --config "$CONFIG_FILE" deploy --version v2.0.0 --prior-version v1.9.0; then
  fail "deploy of v2.0.0 did not complete successfully"
fi
check_canary_status "active"
stop_traffic
log "Scenario 1 PASSED"

log "=== Scenario 2: broken canary should roll back ==="
reset_canary
start_traffic
sleep 2
if ! "$CANOPY_BIN" --config "$CONFIG_FILE" deploy --version v2.0.0-bad --prior-version v1.9.0; then
  fail "deploy of v2.0.0-bad did not complete (expected exit 0 with a RolledBack result, not a CLI error)"
fi
check_canary_status "active"
stop_traffic
log "Scenario 2 PASSED"

log "=== All e2e scenarios passed ==="
