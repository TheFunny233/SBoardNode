#!/bin/sh
set -eu

: "${SBOARD_ADMIN_TOKEN:?set SBOARD_ADMIN_TOKEN}"

SBOARD_URL="${SBOARD_URL:-http://127.0.0.1:8000}"
DOCKER_NETWORK="${SBOARD_DOCKER_NETWORK:-sboard_default}"
CONTAINER_NAME="sboardnode-acceptance"
SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
BINARY_PATH="${SBOARDNODE_BINARY:-$PROJECT_DIR/bin/sboardnode}"
TEST_XRAY_RESTART="${SBOARDNODE_TEST_XRAY_RESTART:-0}"
XRAY_FIXTURE="$PROJECT_DIR/bin/xray-fixture"
XRAY_SERVICE_FIXTURE="$PROJECT_DIR/scripts/fixtures/xray-openrc"

[ -f "$BINARY_PATH" ] || {
    printf 'acceptance: binary not found: %s\n' "$BINARY_PATH" >&2
    exit 1
}
if [ "$TEST_XRAY_RESTART" = "1" ]; then
    [ -f "$XRAY_FIXTURE" ] || {
        printf 'acceptance: xray fixture not found: %s\n' "$XRAY_FIXTURE" >&2
        exit 1
    }
fi
command -v curl >/dev/null 2>&1 || { printf 'acceptance: curl is required\n' >&2; exit 1; }
command -v python3 >/dev/null 2>&1 || { printf 'acceptance: python3 is required\n' >&2; exit 1; }
command -v docker >/dev/null 2>&1 || { printf 'acceptance: docker is required\n' >&2; exit 1; }

created="$(curl -fsS -X POST \
    -H "Authorization: Bearer $SBOARD_ADMIN_TOKEN" \
    -H "Content-Type: application/json" \
    --data-binary '{"name":"SBoardNode-acceptance"}' \
    "$SBOARD_URL/api/v1/agents")"
agent_id="$(printf '%s' "$created" | python3 -c 'import json,sys; print(json.load(sys.stdin)["agent"]["id"])')"
agent_token="$(printf '%s' "$created" | python3 -c 'import json,sys; print(json.load(sys.stdin)["token"])')"

cleanup() {
    docker rm -f "$CONTAINER_NAME" >/dev/null 2>&1 || true
    curl -fsS -X DELETE \
        -H "Authorization: Bearer $SBOARD_ADMIN_TOKEN" \
        "$SBOARD_URL/api/v1/agents/$agent_id" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

config_b64="$(python3 -c '
import base64, json, sys
config = {
    "server": "http://backend:8000",
    "node_id": sys.argv[1],
    "token": sys.argv[2],
    "heartbeat_interval": 10,
}
print(base64.b64encode(json.dumps(config).encode()).decode())
' "$agent_id" "$agent_token")"

docker rm -f "$CONTAINER_NAME" >/dev/null 2>&1 || true
if [ "$TEST_XRAY_RESTART" = "1" ]; then
    docker run -d \
        --name "$CONTAINER_NAME" \
        --memory=128m \
        --network "$DOCKER_NETWORK" \
        -e GOMEMLIMIT=20MiB \
        -e GOGC=50 \
        -e SBOARDNODE_CONFIG_B64="$config_b64" \
        -v "$BINARY_PATH:/usr/local/bin/sboardnode:ro" \
        -v "$XRAY_FIXTURE:/fixtures/xray:ro" \
        -v "$XRAY_SERVICE_FIXTURE:/fixtures/xray-openrc:ro" \
        alpine:3.22 \
        sh -c '
            set -eu
            apk add --no-cache openrc >/dev/null
            cp /fixtures/xray /usr/local/bin/xray
            cp /fixtures/xray-openrc /etc/init.d/xray
            chmod 0755 /usr/local/bin/xray /etc/init.d/xray
            mkdir -p /run/openrc
            touch /run/openrc/softlevel
            rc-service xray start
            printf %s "$SBOARDNODE_CONFIG_B64" | base64 -d > /tmp/config.json
            exec /usr/local/bin/sboardnode -config /tmp/config.json
        ' >/dev/null
else
    docker run -d \
        --name "$CONTAINER_NAME" \
        --memory=128m \
        --network "$DOCKER_NETWORK" \
        -e GOMEMLIMIT=20MiB \
        -e GOGC=50 \
        -e SBOARDNODE_CONFIG_B64="$config_b64" \
        -v "$BINARY_PATH:/usr/local/bin/sboardnode:ro" \
        alpine:3.22 \
        sh -c 'printf %s "$SBOARDNODE_CONFIG_B64" | base64 -d > /tmp/config.json && exec /usr/local/bin/sboardnode -config /tmp/config.json' \
        >/dev/null
fi

attempt=0
last_seen=""
detail=""
while [ "$attempt" -lt 30 ]; do
    detail="$(curl -fsS \
        -H "Authorization: Bearer $SBOARD_ADMIN_TOKEN" \
        "$SBOARD_URL/api/v1/agents/$agent_id")"
    last_seen="$(printf '%s' "$detail" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("last_seen_at") or "")')"
    [ -n "$last_seen" ] && break
    attempt=$((attempt + 1))
    sleep 1
done

if [ -z "$last_seen" ]; then
    docker logs "$CONTAINER_NAME" >&2
    printf 'acceptance: heartbeat was not received\n' >&2
    exit 1
fi

printf 'heartbeat_received=%s\n' "$last_seen"
printf 'container_memory='
docker stats --no-stream --format '{{.MemUsage}} ({{.MemPerc}})' "$CONTAINER_NAME"
printf 'agent_state='
printf '%s' "$detail" | python3 -c '
import json, sys
data = json.load(sys.stdin)
print("version=%s xray=%s memory_total=%s" % (
    data["version"], data["xray_status"], data["memory_total_bytes"]
))
'

if [ "$TEST_XRAY_RESTART" = "1" ]; then
    initial_state="$(printf '%s' "$detail" | python3 -c 'import json,sys; print(json.load(sys.stdin)["xray_status"])')"
    initial_ports="$(printf '%s' "$detail" | python3 -c 'import json,sys; print(" ".join(map(str, json.load(sys.stdin)["xray_ports"])))')"
    [ "$initial_state" = "running" ] || { printf 'acceptance: xray was not detected\n' >&2; exit 1; }
    case " $initial_ports " in *" 18443 "*) ;; *) printf 'acceptance: xray port was not detected\n' >&2; exit 1 ;; esac

    old_pid="$(docker exec "$CONTAINER_NAME" sh -c 'pidof xray | cut -d" " -f1')"
    docker exec "$CONTAINER_NAME" kill "$old_pid"
    restarted=0
    attempt=0
    while [ "$attempt" -lt 30 ]; do
        new_pid="$(docker exec "$CONTAINER_NAME" sh -c 'pidof xray 2>/dev/null | cut -d" " -f1' || true)"
        if [ -n "$new_pid" ] && [ "$new_pid" != "$old_pid" ] && \
           docker exec "$CONTAINER_NAME" kill -0 "$new_pid" 2>/dev/null; then
            restarted=1
            break
        fi
        attempt=$((attempt + 1))
        sleep 1
    done
    [ "$restarted" -eq 1 ] || { docker logs "$CONTAINER_NAME" >&2; printf 'acceptance: xray was not restarted\n' >&2; exit 1; }
    printf 'xray_restart=passed old_pid=%s new_pid=%s\n' "$old_pid" "$new_pid"
    printf 'restart_memory='
    docker stats --no-stream --format '{{.MemUsage}} ({{.MemPerc}})' "$CONTAINER_NAME"
fi

printf 'agent_logs:\n'
docker logs "$CONTAINER_NAME" 2>&1

cleanup
trap - EXIT INT TERM
