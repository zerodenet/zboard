param(
    [string]$Target = "gitlab",
    [string]$RemoteRoot = "/data/zboard-next",
    [string]$Project = "",
    [int]$Port = 18081,
    [int]$NodeCount = 1000,
    [int]$EndpointCount = 5000,
    [int]$UserCount = 5000,
    [int]$BusinessPlanCount = 5000,
    [int]$BusinessSubscriptionCount = 10000,
    [int]$OrderCount = 10000,
    [int]$PlanSKUCount = 5000,
    [int]$TemplateCount = 5000,
    [int]$PageSize = 50,
    [int]$NodeQueryCeiling = 12,
    [int]$ProtocolQueryCeiling = 15,
    [int]$BusinessQueryCeiling = 12,
    [int]$AuditCount = 10000,
    [int]$TrafficCount = 10000,
    [int]$OperationPerSourceCount = 5000,
    [int]$TaskTargetCount = 10000,
    [int]$HistoryQueryCeiling = 12,
    [switch]$SkipBuild,
    [switch]$KeepEnvironment,
    [switch]$CleanupOnly
)

$ErrorActionPreference = "Stop"

$projectWasProvided = -not [string]::IsNullOrWhiteSpace($Project)
$stamp = (Get-Date).ToUniversalTime().ToString("yyyyMMddHHmmss")
if ([string]::IsNullOrWhiteSpace($Project)) {
    $Project = "zboard_scale_validation_$stamp"
}

if ($CleanupOnly -and -not $projectWasProvided) {
    throw "CleanupOnly requires the exact Project name returned by a retained validation run."
}
if ($CleanupOnly -and $KeepEnvironment) {
    throw "CleanupOnly and KeepEnvironment cannot be used together."
}

if ($RemoteRoot -notmatch '^/data/[A-Za-z0-9._/-]+$') {
    throw "RemoteRoot must be an absolute path below /data."
}
if ($Target -notmatch '^[A-Za-z0-9_.@-]+$') {
    throw "Target contains unsupported characters."
}
if ($Project -notmatch '^zboard_scale_validation_[a-z0-9_-]+$') {
    throw "Project must start with zboard_scale_validation_ and contain only lowercase letters, numbers, underscores or hyphens."
}
if ($Project -eq "zboard_next") {
    throw "The production Compose project cannot be used for scale validation."
}
if ($Port -lt 1024 -or $Port -gt 65535) {
    throw "Port must be between 1024 and 65535."
}
if ($NodeCount -lt 3 -or $NodeCount -gt 100000) {
    throw "NodeCount must be between 3 and 100000 so task recovery can cover completed, running and failed items."
}
if ($EndpointCount -lt 1 -or $EndpointCount -gt 500000) {
    throw "EndpointCount must be between 1 and 500000."
}
if ($UserCount -lt $PageSize -or $UserCount -gt 100000) {
    throw "UserCount must cover one page and stay at or below 100000."
}
if ($BusinessPlanCount -lt $PageSize -or $BusinessPlanCount -gt 100000) {
    throw "BusinessPlanCount must cover one page and stay at or below 100000."
}
if ($BusinessSubscriptionCount -lt $PageSize -or $BusinessSubscriptionCount -gt 500000) {
    throw "BusinessSubscriptionCount must cover one page and stay at or below 500000."
}
if ($OrderCount -lt $PageSize -or $OrderCount -gt 500000) {
    throw "OrderCount must cover one page and stay at or below 500000."
}
if ($PlanSKUCount -lt 1 -or $PlanSKUCount -gt 100000) {
    throw "PlanSKUCount must be between 1 and 100000."
}
if ($TemplateCount -lt 1 -or $TemplateCount -gt 100000) {
    throw "TemplateCount must be between 1 and 100000."
}
if ($PageSize -lt 1 -or $PageSize -gt 100) {
    throw "PageSize must be between 1 and 100."
}
if ($NodeQueryCeiling -lt 1 -or $ProtocolQueryCeiling -lt 1 -or $BusinessQueryCeiling -lt 1) {
    throw "Query ceilings must be positive integers."
}
if ($AuditCount -lt $PageSize -or $AuditCount -gt 500000 -or $TrafficCount -lt $PageSize -or $TrafficCount -gt 500000 -or $OperationPerSourceCount -lt $PageSize -or $OperationPerSourceCount -gt 100000) {
    throw "History fixture counts must cover one page and stay within the validation safety bounds."
}
if ($TaskTargetCount -lt 20 -or $TaskTargetCount -gt 10000) {
    throw "TaskTargetCount must be between 20 and the backend task limit of 10000."
}
if ($HistoryQueryCeiling -lt 1) {
    throw "HistoryQueryCeiling must be a positive integer."
}
foreach ($command in @("ssh", "scp")) {
    if (-not (Get-Command $command -ErrorAction SilentlyContinue)) {
        throw "$command is required."
    }
}

$buildFlag = if ($SkipBuild) { "false" } else { "true" }
$keepEnvironmentFlag = if ($KeepEnvironment) { "true" } else { "false" }
$cleanupOnlyFlag = if ($CleanupOnly) { "true" } else { "false" }
$credentialLocalPath = Join-Path ([System.IO.Path]::GetTempPath()) "$Project-browser-credentials.txt"
if ($KeepEnvironment -and (Test-Path -LiteralPath $credentialLocalPath)) {
    throw "The isolated browser credential file already exists: $credentialLocalPath"
}
$remoteScript = @'
set -Eeuo pipefail
trap 'status=$?; printf "scale validation command failed line=%s status=%s\n" "$LINENO" "$status" >&2' ERR

ROOT='__REMOTE_ROOT__'
PROJECT='__PROJECT__'
PORT='__PORT__'
NODE_COUNT='__NODE_COUNT__'
ENDPOINT_COUNT='__ENDPOINT_COUNT__'
USER_COUNT='__USER_COUNT__'
BUSINESS_PLAN_COUNT='__BUSINESS_PLAN_COUNT__'
BUSINESS_SUBSCRIPTION_COUNT='__BUSINESS_SUBSCRIPTION_COUNT__'
ORDER_COUNT='__ORDER_COUNT__'
PLAN_SKU_COUNT='__PLAN_SKU_COUNT__'
TEMPLATE_COUNT='__TEMPLATE_COUNT__'
PAGE_SIZE='__PAGE_SIZE__'
NODE_QUERY_CEILING='__NODE_QUERY_CEILING__'
PROTOCOL_QUERY_CEILING='__PROTOCOL_QUERY_CEILING__'
BUSINESS_QUERY_CEILING='__BUSINESS_QUERY_CEILING__'
AUDIT_COUNT='__AUDIT_COUNT__'
TRAFFIC_COUNT='__TRAFFIC_COUNT__'
OPERATION_PER_SOURCE_COUNT='__OPERATION_PER_SOURCE_COUNT__'
TASK_TARGET_COUNT='__TASK_TARGET_COUNT__'
HISTORY_QUERY_CEILING='__HISTORY_QUERY_CEILING__'
BUILD_IMAGE='__BUILD_IMAGE__'
KEEP_ENVIRONMENT='__KEEP_ENVIRONMENT__'
CLEANUP_ONLY='__CLEANUP_ONLY__'
COMPOSE_FILE="$ROOT/app/deploy/docker/docker-compose.validation.yml"
BASE="http://127.0.0.1:$PORT"
WORK_DIR="/tmp/$PROJECT"

test -d "$ROOT/app"
test -f "$COMPOSE_FILE"
case "$PROJECT" in
  zboard_scale_validation_[a-z0-9_-]*) ;;
  *) printf 'Unsafe validation project name.\n' >&2; exit 2 ;;
esac
test "$PROJECT" != 'zboard_next'

if [ "$CLEANUP_ONLY" = true ]; then
  export ZBOARD_MYSQL_ROOT_PASSWORD='validation-cleanup'
  export ZBOARD_MYSQL_PASSWORD='validation-cleanup'
  export ZBOARD_JWT_SECRET='validation-cleanup'
  export ZBOARD_CREDENTIAL_ENCRYPTION_KEY='validation-cleanup'
  export ZBOARD_BOOTSTRAP_ADMIN_EMAIL='validation-cleanup@example.invalid'
  export ZBOARD_BOOTSTRAP_ADMIN_PASSWORD='validation-cleanup'
  export ZBOARD_ZERO_ARTIFACT_HOST_DIR="$WORK_DIR/artifacts"
  export ZBOARD_VALIDATION_HTTP_PORT="$PORT"
  cd "$ROOT/app"
  docker compose -p "$PROJECT" -f "$COMPOSE_FILE" down -v --remove-orphans >/dev/null
  docker image rm "$PROJECT-zboard" >/dev/null 2>&1 || true
  rm -rf -- "$WORK_DIR"
  test -z "$(docker ps -a --filter "label=com.docker.compose.project=$PROJECT" -q)"
  test -z "$(docker volume ls --filter "label=com.docker.compose.project=$PROJECT" -q)"
  test -z "$(docker network ls --filter "label=com.docker.compose.project=$PROJECT" -q)"
  test -z "$(docker image ls "$PROJECT-zboard" -q)"
  printf 'cleanup project=%s containers=0 volumes=0 networks=0 image=removed\n' "$PROJECT"
  exit 0
fi

if docker ps -a --filter "label=com.docker.compose.project=$PROJECT" -q | grep -q .; then
  printf 'Compose project already has containers: %s\n' "$PROJECT" >&2
  exit 2
fi
if docker volume ls --filter "label=com.docker.compose.project=$PROJECT" -q | grep -q .; then
  printf 'Compose project already has volumes: %s\n' "$PROJECT" >&2
  exit 2
fi
if command -v ss >/dev/null 2>&1 && ss -ltn "sport = :$PORT" | tail -n +2 | grep -q .; then
  printf 'Validation port is already in use: %s\n' "$PORT" >&2
  exit 2
fi

mkdir -p "$WORK_DIR/artifacts"
cleanup() {
  set +e
  cd "$ROOT/app"
  docker compose -p "$PROJECT" -f "$COMPOSE_FILE" down -v --remove-orphans >/dev/null 2>&1
  if [ "$BUILD_IMAGE" = true ]; then docker image rm "$PROJECT-zboard" >/dev/null 2>&1; fi
  rm -rf -- "$WORK_DIR"
}
trap cleanup EXIT INT TERM

export ZBOARD_MYSQL_ROOT_PASSWORD="$(openssl rand -hex 24)"
export ZBOARD_MYSQL_PASSWORD="$(openssl rand -hex 24)"
export ZBOARD_JWT_SECRET="$(openssl rand -hex 32)"
export ZBOARD_CREDENTIAL_ENCRYPTION_KEY="$(openssl rand -hex 32)"
export ZBOARD_BOOTSTRAP_ADMIN_EMAIL='scale-validation@example.invalid'
export ZBOARD_BOOTSTRAP_ADMIN_PASSWORD="Scale-$(openssl rand -hex 24)"
export ZBOARD_ZERO_ARTIFACT_HOST_DIR="$WORK_DIR/artifacts"
export ZBOARD_VALIDATION_HTTP_PORT="$PORT"
export ZBOARD_VALIDATION_VERSION="v0.0.1-scale-$PROJECT"
export ZBOARD_VALIDATION_COMMIT='working-tree'
export ZBOARD_VALIDATION_BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

cd "$ROOT/app"
if [ "$BUILD_IMAGE" = true ]; then
  docker compose -p "$PROJECT" -f "$COMPOSE_FILE" up -d --build
else
  docker compose -p "$PROJECT" -f "$COMPOSE_FILE" up -d
fi

healthy=false
for attempt in $(seq 1 80); do
  state=$(docker inspect -f '{{.State.Health.Status}}' "$PROJECT-zboard-1" 2>/dev/null || true)
  if [ "$state" = healthy ]; then healthy=true; break; fi
  sleep 2
done
if [ "$healthy" != true ]; then
  docker logs --tail 120 "$PROJECT-zboard-1" >&2 || true
  printf 'Validation application did not become healthy.\n' >&2
  exit 1
fi

RECURSION_LIMIT=$((ENDPOINT_COUNT + NODE_COUNT + USER_COUNT + BUSINESS_PLAN_COUNT + BUSINESS_SUBSCRIPTION_COUNT + ORDER_COUNT + PLAN_SKU_COUNT + TEMPLATE_COUNT + AUDIT_COUNT + TRAFFIC_COUNT + OPERATION_PER_SOURCE_COUNT + TASK_TARGET_COUNT + 100))
docker exec -e MYSQL_PWD="$ZBOARD_MYSQL_ROOT_PASSWORD" -i "$PROJECT-mysql-1" mysql -uroot zboard <<SQL
SET SESSION cte_max_recursion_depth = $RECURSION_LIMIT;
INSERT INTO nodes (name, region, address)
WITH RECURSIVE seq AS (
  SELECT 1 AS n
  UNION ALL SELECT n + 1 FROM seq WHERE n < $NODE_COUNT
)
SELECT CONCAT('scale-node-', LPAD(n, 6, '0')),
       CONCAT('region-', MOD(n, 20)),
       CONCAT('10.43.', FLOOR((n - 1) / 254), '.', MOD(n - 1, 254) + 1)
FROM seq;

SET SESSION cte_max_recursion_depth = $RECURSION_LIMIT;
INSERT INTO protocol_endpoints (
  node_id, name, runtime_key, protocol, address, port, public_port,
  multiplier_milli, is_active, sort_order
)
WITH RECURSIVE seq AS (
  SELECT 1 AS n
  UNION ALL SELECT n + 1 FROM seq WHERE n < $ENDPOINT_COUNT
), seeded_nodes AS (
  SELECT id, ROW_NUMBER() OVER (ORDER BY id) AS rn
  FROM nodes
  WHERE name LIKE 'scale-node-%'
)
SELECT seeded_nodes.id,
       CONCAT('scale-endpoint-', LPAD(seq.n, 7, '0')),
       UUID(),
       'vless',
       CONCAT('edge-', seq.n, '.example.invalid'),
       10000 + MOD(seq.n, 5000),
       10000 + MOD(seq.n, 5000),
       1000,
       1,
       seq.n
FROM seq
JOIN seeded_nodes ON seeded_nodes.rn = MOD(seq.n - 1, $NODE_COUNT) + 1;

SET @history_now = UTC_TIMESTAMP(3);
SET @admin_id = (SELECT id FROM users WHERE email = 'scale-validation@example.invalid' LIMIT 1);
SET @seed_node_id = (SELECT id FROM nodes WHERE name LIKE 'scale-node-%' ORDER BY id LIMIT 1);
SET @seed_endpoint_id = (SELECT id FROM protocol_endpoints WHERE name LIKE 'scale-endpoint-%' ORDER BY id LIMIT 1);

SET SESSION cte_max_recursion_depth = $RECURSION_LIMIT;
INSERT INTO audit_logs (user_id, actor, action, target, detail, created_at)
WITH RECURSIVE seq AS (
  SELECT 1 AS n
  UNION ALL SELECT n + 1 FROM seq WHERE n < $AUDIT_COUNT
)
SELECT @admin_id, 'scale-history', 'scale.verify', CONCAT('fixture:', n), CONCAT('sequence=', n),
       TIMESTAMPADD(SECOND, -n, @history_now)
FROM seq;

SET SESSION cte_max_recursion_depth = $RECURSION_LIMIT;
INSERT INTO traffic_records (
  user_id, node_id, protocol_endpoint_id, report_id, flow_id, event_type, event_revision, nonce,
  raw_bytes, upload_bytes, download_bytes, traffic_calc_mode, protocol_multiplier_milli, used_bytes,
  record_at, meta, created_at, updated_at
)
WITH RECURSIVE seq AS (
  SELECT 1 AS n
  UNION ALL SELECT n + 1 FROM seq WHERE n < $TRAFFIC_COUNT
)
SELECT @admin_id, @seed_node_id, @seed_endpoint_id, CONCAT('scale-report-', n), CONCAT('scale-flow-', n),
       'scale-history', n, CONCAT('scale-nonce-', n), n * 1024, n * 512, n * 512, 0, 1000, n * 1024,
       TIMESTAMPADD(SECOND, -n, @history_now), '{}', TIMESTAMPADD(SECOND, -n, @history_now), TIMESTAMPADD(SECOND, -n, @history_now)
FROM seq;

SET SESSION cte_max_recursion_depth = $RECURSION_LIMIT;
INSERT INTO protocol_deployments (
  protocol_endpoint_id, node_id, config_revision, status, requested_by, created_at, updated_at
)
WITH RECURSIVE seq AS (
  SELECT 1 AS n
  UNION ALL SELECT n + 1 FROM seq WHERE n < $OPERATION_PER_SOURCE_COUNT
)
SELECT @seed_endpoint_id, @seed_node_id, n, 'succeeded', @admin_id,
       TIMESTAMPADD(SECOND, -n, @history_now), TIMESTAMPADD(SECOND, -n, @history_now)
FROM seq;

SET SESSION cte_max_recursion_depth = $RECURSION_LIMIT;
INSERT INTO node_operations (
  node_id, operation_type, status, phase, requested_by, result_summary, error, created_at, updated_at
)
WITH RECURSIVE seq AS (
  SELECT 1 AS n
  UNION ALL SELECT n + 1 FROM seq WHERE n < $OPERATION_PER_SOURCE_COUNT
)
SELECT @seed_node_id, 'detect', 'succeeded', 'completed', @admin_id, CONCAT('sequence ', n), '',
       TIMESTAMPADD(SECOND, -n, @history_now), TIMESTAMPADD(SECOND, -n, @history_now)
FROM seq;

SET SESSION cte_max_recursion_depth = $RECURSION_LIMIT;
INSERT INTO tasks (type, scope, content, status, total, current, attempts, max_attempts, created_at, updated_at)
WITH RECURSIVE seq AS (
  SELECT 1 AS n
  UNION ALL SELECT n + 1 FROM seq WHERE n < $OPERATION_PER_SOURCE_COUNT
)
SELECT 'scale', '{}', '{}', 2, 1, 1, 1, 3,
       TIMESTAMPADD(SECOND, -n, @history_now), TIMESTAMPADD(SECOND, -n, @history_now)
FROM seq;
SQL

LOGIN_PAYLOAD=$(printf '{"email":"%s","password":"%s"}' "$ZBOARD_BOOTSTRAP_ADMIN_EMAIL" "$ZBOARD_BOOTSTRAP_ADMIN_PASSWORD")
LOGIN_RESPONSE=$(curl -fsS -H 'Content-Type: application/json' --data-binary "$LOGIN_PAYLOAD" "$BASE/api/v1/auth/login")
TOKEN=$(printf '%s' "$LOGIN_RESPONSE" | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')
test -n "$TOKEN"

if [ "$KEEP_ENVIRONMENT" = true ]; then
  umask 077
  printf '%s\n%s\n' "$ZBOARD_BOOTSTRAP_ADMIN_EMAIL" "$ZBOARD_BOOTSTRAP_ADMIN_PASSWORD" > "$WORK_DIR/browser-credentials.txt"
fi

SECOND_ADMIN_EMAIL='scale-validation-2@example.invalid'
SECOND_ADMIN_PASSWORD="Scale-$(openssl rand -hex 24)"
SECOND_ADMIN_PAYLOAD=$(printf '{"email":"%s","password":"%s","is_admin":true,"status":"active"}' "$SECOND_ADMIN_EMAIL" "$SECOND_ADMIN_PASSWORD")
curl -fsS -o "$WORK_DIR/second-admin.json" -H 'Content-Type: application/json' -H "Authorization: Bearer $TOKEN" --data-binary "$SECOND_ADMIN_PAYLOAD" "$BASE/api/v1/admin/users"
SECOND_LOGIN_PAYLOAD=$(printf '{"email":"%s","password":"%s"}' "$SECOND_ADMIN_EMAIL" "$SECOND_ADMIN_PASSWORD")
SECOND_LOGIN_RESPONSE=$(curl -fsS -H 'Content-Type: application/json' --data-binary "$SECOND_LOGIN_PAYLOAD" "$BASE/api/v1/auth/login")
SECOND_TOKEN=$(printf '%s' "$SECOND_LOGIN_RESPONSE" | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')
test -n "$SECOND_TOKEN"

SECOND_ADMIN_ID=$(docker exec -e MYSQL_PWD="$ZBOARD_MYSQL_ROOT_PASSWORD" "$PROJECT-mysql-1" mysql -N -uroot zboard -e "SELECT id FROM users WHERE email = 'scale-validation-2@example.invalid' LIMIT 1;")
test -n "$SECOND_ADMIN_ID"

docker exec -e MYSQL_PWD="$ZBOARD_MYSQL_ROOT_PASSWORD" -i "$PROJECT-mysql-1" mysql -uroot zboard <<SQL
SET SESSION cte_max_recursion_depth = $RECURSION_LIMIT;
SET @business_now = UTC_TIMESTAMP(3);
SET @business_endpoint_id = (SELECT id FROM protocol_endpoints WHERE name LIKE 'scale-endpoint-%' ORDER BY id LIMIT 1);

INSERT INTO users (
  account_name, email, password, is_admin, status, created_at, updated_at
)
WITH RECURSIVE seq AS (
  SELECT 1 AS n
  UNION ALL SELECT n + 1 FROM seq WHERE n < $USER_COUNT
)
SELECT CONCAT('Scale user ', LPAD(n, 6, '0')),
       CONCAT('scale-user-', LPAD(n, 6, '0'), '@example.invalid'),
       'scale-list-password-hash',
       0,
       CASE WHEN MOD(n, 20) = 0 THEN 'suspended' WHEN MOD(n, 25) = 0 THEN 'deactivated' ELSE 'active' END,
       TIMESTAMPADD(SECOND, -n, @business_now),
       TIMESTAMPADD(SECOND, -n, @business_now)
FROM seq;

INSERT INTO node_groups (name, code, description, is_enabled, created_at, updated_at)
WITH RECURSIVE seq AS (
  SELECT 1 AS n
  UNION ALL SELECT n + 1 FROM seq WHERE n < $BUSINESS_PLAN_COUNT
)
SELECT CONCAT('Scale business group ', LPAD(n, 6, '0')),
       CONCAT('scale-business-group-', LPAD(n, 6, '0')),
       'Distributed business-list aggregation fixture',
       1,
       TIMESTAMPADD(SECOND, -n, @business_now),
       TIMESTAMPADD(SECOND, -n, @business_now)
FROM seq;

INSERT INTO node_group_endpoints (node_group_id, protocol_endpoint_id, sort_order, created_at)
SELECT id, @business_endpoint_id, 0, created_at
FROM node_groups
WHERE code LIKE 'scale-business-group-%';

INSERT INTO plans (
  name, slug, summary, description, node_group_id, traffic_bytes, speed_limit_mbps,
  max_active_subscriptions, is_renewable, device_limit, family_limit, reset_policy,
  traffic_calc_mode, is_active, sort_order, created_at, updated_at
)
WITH RECURSIVE seq AS (
  SELECT 1 AS n
  UNION ALL SELECT n + 1 FROM seq WHERE n < $BUSINESS_PLAN_COUNT
)
SELECT CONCAT('Scale business plan ', LPAD(seq.n, 6, '0')),
       CONCAT('scale-business-plan-', LPAD(seq.n, 6, '0')),
       'Distributed plan aggregation fixture',
       'Detail-only scale plan description',
       seeded_group.id,
       1073741824,
       MOD(seq.n, 100),
       0,
       1,
       MOD(seq.n, 5) + 1,
       0,
       0,
       0,
       1,
       seq.n,
       TIMESTAMPADD(SECOND, -seq.n, @business_now),
       TIMESTAMPADD(SECOND, -seq.n, @business_now)
FROM seq
JOIN node_groups seeded_group
  ON seeded_group.code = CONCAT('scale-business-group-', LPAD(seq.n, 6, '0'));

INSERT INTO plan_skus (
  plan_id, code, name, sku_type, billing_unit, billing_value, price_cents, currency,
  traffic_bytes, device_limit, speed_limit_mbps, is_active, sort_order, created_at, updated_at
)
SELECT plan.id,
       CONCAT('scale-business-sku-', RIGHT(plan.slug, 6)),
       CONCAT('Scale business SKU ', RIGHT(plan.slug, 6)),
       'new',
       'month',
       1,
       plan.sort_order,
       'CNY',
       plan.traffic_bytes,
       plan.device_limit,
       plan.speed_limit_mbps,
       1,
       0,
       plan.created_at,
       plan.updated_at
FROM plans plan
WHERE plan.slug LIKE 'scale-business-plan-%';

INSERT INTO subscriptions (
  user_id, plan_id, plan_sku_id, node_group_id, subscription_type, start_at, end_at,
  status, flow_total, flow_used, speed_limit_mbps, device_limit, family_limit,
  renewal_price_minor, reset_policy, traffic_calc_mode, config, created_at, updated_at
)
WITH RECURSIVE seq AS (
  SELECT 1 AS n
  UNION ALL SELECT n + 1 FROM seq WHERE n < $BUSINESS_SUBSCRIPTION_COUNT
)
SELECT seeded_user.id,
       seeded_plan.id,
       seeded_sku.id,
       seeded_plan.node_group_id,
       1,
       TIMESTAMPADD(DAY, -30, @business_now),
       CASE WHEN MOD(seq.n, 7) = 0 THEN TIMESTAMPADD(DAY, -1, @business_now) ELSE TIMESTAMPADD(DAY, 30, @business_now) END,
       CASE WHEN MOD(seq.n, 11) = 0 THEN 'canceled' ELSE 'active' END,
       1073741824,
       CASE WHEN MOD(seq.n, 13) = 0 THEN 1073741824 ELSE MOD(seq.n, 1000) * 1048576 END,
       seeded_plan.speed_limit_mbps,
       seeded_plan.device_limit,
       seeded_plan.family_limit,
       seeded_sku.price_cents,
       0,
       0,
       JSON_OBJECT('fixture_sequence', seq.n),
       TIMESTAMPADD(SECOND, -seq.n, @business_now),
       TIMESTAMPADD(SECOND, -seq.n, @business_now)
FROM seq
JOIN users seeded_user
  ON seeded_user.email = CONCAT('scale-user-', LPAD(MOD(seq.n - 1, $USER_COUNT) + 1, 6, '0'), '@example.invalid')
JOIN plans seeded_plan
  ON seeded_plan.slug = CONCAT('scale-business-plan-', LPAD(MOD(seq.n - 1, $BUSINESS_PLAN_COUNT) + 1, 6, '0'))
JOIN plan_skus seeded_sku
  ON seeded_sku.plan_id = seeded_plan.id AND seeded_sku.code LIKE 'scale-business-sku-%';

INSERT INTO orders (
  user_id, subscription_id, plan_id, plan_sku_id, trade_no, order_type,
  amount_cents, payable_amount, paid_amount, refund_amount, discount_amount,
  currency, channel, status, plan_name, sku_name, billing_unit, billing_value,
  traffic_bytes, device_limit, speed_limit_mbps, raw_callback, failure_reason,
  created_at, updated_at
)
WITH RECURSIVE seq AS (
  SELECT 1 AS n
  UNION ALL SELECT n + 1 FROM seq WHERE n < $ORDER_COUNT
)
SELECT seeded_user.id,
       NULL,
       seeded_plan.id,
       seeded_sku.id,
       CONCAT('scale-business-order-', LPAD(seq.n, 7, '0')),
       CASE WHEN MOD(seq.n, 4) = 0 THEN 'renewal' ELSE 'new' END,
       seeded_sku.price_cents,
       seeded_sku.price_cents,
       CASE WHEN MOD(seq.n, 4) = 1 THEN seeded_sku.price_cents ELSE 0 END,
       0,
       0,
       'CNY',
       'manual',
       CASE MOD(seq.n, 4) WHEN 0 THEN 'pending' WHEN 1 THEN 'paid' WHEN 2 THEN 'failed' ELSE 'canceled' END,
       seeded_plan.name,
       seeded_sku.name,
       seeded_sku.billing_unit,
       seeded_sku.billing_value,
       seeded_sku.traffic_bytes,
       seeded_sku.device_limit,
       seeded_sku.speed_limit_mbps,
       JSON_OBJECT('private', 'scale-order-secret'),
       CASE WHEN MOD(seq.n, 4) = 2 THEN 'scale processor diagnostic' ELSE '' END,
       TIMESTAMPADD(SECOND, -seq.n, @business_now),
       TIMESTAMPADD(SECOND, -seq.n, @business_now)
FROM seq
JOIN users seeded_user
  ON seeded_user.email = CONCAT('scale-user-', LPAD(MOD(seq.n - 1, $USER_COUNT) + 1, 6, '0'), '@example.invalid')
JOIN plans seeded_plan
  ON seeded_plan.slug = CONCAT('scale-business-plan-', LPAD(MOD(seq.n - 1, $BUSINESS_PLAN_COUNT) + 1, 6, '0'))
JOIN plan_skus seeded_sku
  ON seeded_sku.plan_id = seeded_plan.id AND seeded_sku.code LIKE 'scale-business-sku-%';

INSERT INTO node_groups (name, code, description, is_enabled)
VALUES ('Plan SKU scale fixture', 'plan-sku-scale-fixture', 'Bounded SKU workbench verification.', 1);
SET @sku_group_id = LAST_INSERT_ID();
INSERT INTO plans (
  name, slug, summary, description, node_group_id, traffic_bytes, speed_limit_mbps,
  max_active_subscriptions, is_renewable, device_limit, family_limit, reset_policy,
  traffic_calc_mode, is_active, sort_order
) VALUES (
  'Plan SKU scale fixture', 'plan-sku-scale-fixture', 'Bounded SKU workbench verification', '',
  @sku_group_id, 1073741824, 0, 0, 1, 1, 0, 0, 0, 1, 0
);
SET @sku_plan_id = LAST_INSERT_ID();
INSERT INTO plan_skus (
  plan_id, code, name, sku_type, billing_unit, billing_value, price_cents, currency,
  traffic_bytes, device_limit, speed_limit_mbps, is_active, sort_order
)
WITH RECURSIVE seq AS (
  SELECT 1 AS n
  UNION ALL SELECT n + 1 FROM seq WHERE n < $PLAN_SKU_COUNT
)
SELECT @sku_plan_id, CONCAT('scale-catalog-', LPAD(n, 6, '0')),
       CONCAT('Scale catalog ', LPAD(n, 6, '0')), 'new', 'month', 1, n, 'CNY',
       1073741824, 1, 0, MOD(n, 2) = 1, n
FROM seq;
INSERT INTO subscription_templates (
  name, slug, description, content_type, template_body, is_active, sort_order, revision,
  created_at, updated_at
)
WITH RECURSIVE template_seq AS (
  SELECT 1 AS n
  UNION ALL SELECT n + 1 FROM template_seq WHERE n < $TEMPLATE_COUNT
)
SELECT CONCAT('Scale template ', LPAD(n, 6, '0')),
       CONCAT('scale-template-', LPAD(n, 6, '0')),
       'Bounded active template search fixture', 'text/plain', 'scale-template-body',
       1, n, 1, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)
FROM template_seq;
SQL
SKU_SCALE_PLAN_ID=$(docker exec -e MYSQL_PWD="$ZBOARD_MYSQL_ROOT_PASSWORD" "$PROJECT-mysql-1" mysql -N -uroot zboard -e "SELECT id FROM plans WHERE slug = 'plan-sku-scale-fixture' LIMIT 1;")
SKU_SCALE_FIRST_ID=$(docker exec -e MYSQL_PWD="$ZBOARD_MYSQL_ROOT_PASSWORD" "$PROJECT-mysql-1" mysql -N -uroot zboard -e "SELECT id FROM plan_skus WHERE plan_id = $SKU_SCALE_PLAN_ID ORDER BY sort_order, id LIMIT 1;")
test -n "$SKU_SCALE_PLAN_ID"
test -n "$SKU_SCALE_FIRST_ID"
SKU_SCALE_DETAIL=$(curl -fsS -H "Authorization: Bearer $TOKEN" "$BASE/api/v1/admin/plans/$SKU_SCALE_PLAN_ID")
SKU_SCALE_PAGE=$(curl -fsS -H "Authorization: Bearer $TOKEN" "$BASE/api/v1/admin/plans/$SKU_SCALE_PLAN_ID/skus?paged=true&limit=$PAGE_SIZE&offset=0")
SKU_SCALE_ITEM=$(curl -fsS -H "Authorization: Bearer $TOKEN" "$BASE/api/v1/admin/plan-skus/$SKU_SCALE_FIRST_ID")
SKU_SCALE_TOTAL=$(printf '%s' "$SKU_SCALE_PAGE" | grep -o '"total":[0-9][0-9]*' | head -n 1 | cut -d: -f2)
SKU_SCALE_ITEMS=$(printf '%s' "$SKU_SCALE_PAGE" | grep -o "\"plan_id\":$SKU_SCALE_PLAN_ID" | wc -l | tr -d ' ')
test "$SKU_SCALE_TOTAL" = "$PLAN_SKU_COUNT"
test "$SKU_SCALE_ITEMS" = "$PAGE_SIZE"
case "$SKU_SCALE_DETAIL" in
  *'"sku_count":'"$PLAN_SKU_COUNT"*'"active_sku_count":'*) ;;
  *)
    printf 'plan detail omitted SKU counts: %s\n' "$SKU_SCALE_DETAIL" >&2
    exit 1
    ;;
esac
case "$SKU_SCALE_DETAIL" in
  *'"skus":'*|*'scale-catalog-'*)
    printf 'plan detail embedded unbounded SKU data\n' >&2
    exit 1
    ;;
esac
case "$SKU_SCALE_ITEM" in
  *'"code":"scale-catalog-000001"'*) ;;
  *)
    printf 'plan SKU detail mismatch: %s\n' "$SKU_SCALE_ITEM" >&2
    exit 1
    ;;
esac
printf 'plan-sku-scale total=%s items=%s detail_embedded=0 single_fetch=passed\n' "$SKU_SCALE_TOTAL" "$SKU_SCALE_ITEMS"

PUBLIC_PLAN_PAGE=$(curl -fsS "$BASE/api/v1/plans?paged=true&q=plan-sku-scale-fixture&limit=$PAGE_SIZE&offset=0")
PUBLIC_PLAN_DETAIL=$(curl -fsS "$BASE/api/v1/plans/$SKU_SCALE_PLAN_ID")
PUBLIC_SKU_PAGE=$(curl -fsS "$BASE/api/v1/plans/$SKU_SCALE_PLAN_ID/skus?sku_type=new&limit=$PAGE_SIZE&offset=0")
PUBLIC_PLAN_TOTAL=$(printf '%s' "$PUBLIC_PLAN_PAGE" | grep -o '"total":[0-9][0-9]*' | head -n 1 | cut -d: -f2)
PUBLIC_SKU_TOTAL=$(printf '%s' "$PUBLIC_SKU_PAGE" | grep -o '"total":[0-9][0-9]*' | head -n 1 | cut -d: -f2)
PUBLIC_SKU_ITEMS=$(printf '%s' "$PUBLIC_SKU_PAGE" | grep -o "\"plan_id\":$SKU_SCALE_PLAN_ID" | wc -l | tr -d ' ')
EXPECTED_PUBLIC_SKU_TOTAL=$(((PLAN_SKU_COUNT + 1) / 2))
test "$PUBLIC_PLAN_TOTAL" = 1
test "$PUBLIC_SKU_TOTAL" = "$EXPECTED_PUBLIC_SKU_TOTAL"
test "$PUBLIC_SKU_ITEMS" = "$PAGE_SIZE"
case "$PUBLIC_PLAN_PAGE" in
  *'"primary_sku":'*'"code":"scale-catalog-000001"'*) ;;
  *)
    printf 'public plan page omitted its bounded primary SKU: %s\n' "$PUBLIC_PLAN_PAGE" >&2
    exit 1
    ;;
esac
case "$PUBLIC_PLAN_PAGE$PUBLIC_PLAN_DETAIL" in
  *'"skus":'*)
    printf 'public plan catalog embedded an unbounded SKU collection\n' >&2
    exit 1
    ;;
esac

TEMPLATE_SCALE_PAGE=$(curl -fsS -H "Authorization: Bearer $TOKEN" "$BASE/api/v1/subscription-templates?paged=true&q=scale-template&limit=$PAGE_SIZE&offset=0")
TEMPLATE_SCALE_TOTAL=$(printf '%s' "$TEMPLATE_SCALE_PAGE" | grep -o '"total":[0-9][0-9]*' | head -n 1 | cut -d: -f2)
TEMPLATE_SCALE_ITEMS=$(printf '%s' "$TEMPLATE_SCALE_PAGE" | grep -o '"slug":"scale-template-' | wc -l | tr -d ' ')
test "$TEMPLATE_SCALE_TOTAL" = "$TEMPLATE_COUNT"
test "$TEMPLATE_SCALE_ITEMS" = "$PAGE_SIZE"
case "$TEMPLATE_SCALE_PAGE" in
  *'"template":"scale-template-body"'*)
    printf 'active template page leaked template source\n' >&2
    exit 1
    ;;
esac
printf 'self-service-catalog plan_total=%s active_skus=%s sku_items=%s templates=%s template_items=%s embedded_skus=0 template_source=0\n' "$PUBLIC_PLAN_TOTAL" "$PUBLIC_SKU_TOTAL" "$PUBLIC_SKU_ITEMS" "$TEMPLATE_SCALE_TOTAL" "$TEMPLATE_SCALE_ITEMS"

REVISION_ENDPOINT_ID=$(docker exec -e MYSQL_PWD="$ZBOARD_MYSQL_ROOT_PASSWORD" "$PROJECT-mysql-1" mysql -N -uroot zboard -e "SELECT id FROM protocol_endpoints WHERE name LIKE 'scale-endpoint-%' ORDER BY id LIMIT 1;")
test -n "$REVISION_ENDPOINT_ID"
docker exec -e MYSQL_PWD="$ZBOARD_MYSQL_ROOT_PASSWORD" "$PROJECT-mysql-1" mysql -uroot zboard -e "
  INSERT INTO node_groups (name, code, description, is_enabled, created_at, updated_at)
  VALUES ('Scale revision group', 'scale-revision-group', 'initial', 0, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3));
  SET @revision_group_id = LAST_INSERT_ID();
  INSERT INTO node_group_endpoints (node_group_id, protocol_endpoint_id, sort_order, created_at)
  VALUES (@revision_group_id, $REVISION_ENDPOINT_ID, 0, UTC_TIMESTAMP(3));
"
REVISION_GROUP_ID=$(docker exec -e MYSQL_PWD="$ZBOARD_MYSQL_ROOT_PASSWORD" "$PROJECT-mysql-1" mysql -N -uroot zboard -e "SELECT id FROM node_groups WHERE code = 'scale-revision-group' LIMIT 1;")
test -n "$REVISION_GROUP_ID"
REVISION_DETAIL=$(curl -fsS -H "Authorization: Bearer $SECOND_TOKEN" "$BASE/api/v1/admin/node-groups/$REVISION_GROUP_ID")
REVISION_BASE=$(printf '%s' "$REVISION_DETAIL" | sed -n 's/.*"revision":\([0-9][0-9]*\).*/\1/p')
test "$REVISION_BASE" = "1"
REVISION_MISSING_STATUS=$(curl -sS -o "$WORK_DIR/node-group-missing-revision.json" -w '%{http_code}' -H 'Content-Type: application/json' -H "Authorization: Bearer $SECOND_TOKEN" -X PUT --data-binary '{"description":"missing revision"}' "$BASE/api/v1/admin/node-groups/$REVISION_GROUP_ID")
if [ "$REVISION_MISSING_STATUS" != "428" ]; then
  printf 'node-group missing precondition status=%s expected=428\n' "$REVISION_MISSING_STATUS" >&2
  exit 1
fi
REVISION_FIRST_PAYLOAD=$(printf '{"description":"first administrator","expected_revision":%s}' "$REVISION_BASE")
curl -fsS -o "$WORK_DIR/node-group-first-update.json" -H 'Content-Type: application/json' -H "Authorization: Bearer $TOKEN" -X PUT --data-binary "$REVISION_FIRST_PAYLOAD" "$BASE/api/v1/admin/node-groups/$REVISION_GROUP_ID"
REVISION_STALE_PAYLOAD=$(printf '{"description":"stale administrator","expected_revision":%s}' "$REVISION_BASE")
REVISION_STALE_STATUS=$(curl -sS -o "$WORK_DIR/node-group-stale-update.json" -w '%{http_code}' -H 'Content-Type: application/json' -H "Authorization: Bearer $SECOND_TOKEN" -X PUT --data-binary "$REVISION_STALE_PAYLOAD" "$BASE/api/v1/admin/node-groups/$REVISION_GROUP_ID")
if [ "$REVISION_STALE_STATUS" != "409" ]; then
  printf 'node-group stale update status=%s expected=409\n' "$REVISION_STALE_STATUS" >&2
  exit 1
fi
REVISION_FINAL=$(curl -fsS -H "Authorization: Bearer $TOKEN" "$BASE/api/v1/admin/node-groups/$REVISION_GROUP_ID")
case "$REVISION_FINAL" in
  *'"description":"first administrator"'*'"revision":2'*) ;;
  *)
    printf 'node-group optimistic concurrency lost the accepted update: %s\n' "$REVISION_FINAL" >&2
    exit 1
    ;;
esac
printf 'node-group optimistic concurrency missing_status=%s stale_status=%s revision=2\n' "$REVISION_MISSING_STATUS" "$REVISION_STALE_STATUS"
docker exec -e MYSQL_PWD="$ZBOARD_MYSQL_ROOT_PASSWORD" "$PROJECT-mysql-1" mysql -uroot zboard -e "
  DELETE FROM node_group_endpoints WHERE node_group_id = $REVISION_GROUP_ID;
  DELETE FROM node_groups WHERE id = $REVISION_GROUP_ID;
"

docker exec -e MYSQL_PWD="$ZBOARD_MYSQL_ROOT_PASSWORD" -i "$PROJECT-mysql-1" mysql -uroot zboard <<SQL
SET @admin_id = $SECOND_ADMIN_ID;
SET @node_id = (SELECT id FROM nodes WHERE name LIKE 'scale-node-%' ORDER BY id LIMIT 1);
SET @endpoint_id = (SELECT id FROM protocol_endpoints WHERE name LIKE 'scale-endpoint-%' ORDER BY id LIMIT 1);
INSERT INTO node_groups (name, code, description, is_enabled)
VALUES ('Traffic reconciliation fixture', 'traffic-reconciliation-fixture', 'Isolated aggregate verification.', 1);
SET @group_id = LAST_INSERT_ID();
INSERT INTO plans (
  name, slug, summary, description, node_group_id, traffic_bytes, speed_limit_mbps,
  max_active_subscriptions, is_renewable, device_limit, family_limit, reset_policy,
  traffic_calc_mode, is_active, sort_order
) VALUES (
  'Traffic reconciliation fixture', 'traffic-reconciliation-fixture', 'Aggregate verification', '', @group_id,
  10485760, 0, 0, 1, 1, 0, 0, 0, 1, 0
);
SET @plan_id = LAST_INSERT_ID();
INSERT INTO plan_skus (
  plan_id, code, name, sku_type, billing_unit, billing_value, price_cents, currency,
  traffic_bytes, device_limit, speed_limit_mbps, is_active, sort_order
) VALUES (
  @plan_id, 'traffic-reconciliation-fixture', 'Traffic reconciliation fixture', 'new', 'day', 30, 100,
  'USD', 10485760, 1, 0, 1, 0
);
SET @sku_id = LAST_INSERT_ID();
INSERT INTO subscriptions (
  user_id, plan_id, plan_sku_id, node_group_id, subscription_type, start_at, end_at,
  status, flow_total, flow_used, speed_limit_mbps, device_limit, family_limit,
  renewal_price_minor, reset_policy, traffic_calc_mode, config
) VALUES
  (@admin_id, @plan_id, @sku_id, @group_id, 1, UTC_TIMESTAMP(3), DATE_ADD(UTC_TIMESTAMP(3), INTERVAL 30 DAY), 'active', 10485760, 1000, 0, 1, 0, 100, 0, 0, '{}'),
  (@admin_id, @plan_id, @sku_id, @group_id, 1, UTC_TIMESTAMP(3), DATE_ADD(UTC_TIMESTAMP(3), INTERVAL 30 DAY), 'active', 10485760, 3000, 0, 1, 0, 100, 0, 0, '{}'),
  (@admin_id, @plan_id, @sku_id, @group_id, 1, UTC_TIMESTAMP(3), DATE_ADD(UTC_TIMESTAMP(3), INTERVAL 30 DAY), 'active', 10485760, 1000, 0, 1, 0, 100, 0, 0, '{}');
SET @first_subscription_id = LAST_INSERT_ID();
INSERT INTO traffic_records (
  user_id, subscription_id, node_id, protocol_endpoint_id, report_id, flow_id, event_type,
  event_revision, nonce, raw_bytes, upload_bytes, download_bytes, traffic_calc_mode,
  protocol_multiplier_milli, used_bytes, record_at, meta, created_at, updated_at
) VALUES
  (@admin_id, @first_subscription_id, @node_id, @endpoint_id, 'reconciliation-matched', 'reconciliation-matched', 'scale-reconciliation', 1, 'reconciliation-matched', 1000, 500, 500, 0, 1000, 1000, UTC_TIMESTAMP(3), '{}', UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)),
  (@admin_id, @first_subscription_id + 1, @node_id, @endpoint_id, 'reconciliation-missing', 'reconciliation-missing', 'scale-reconciliation', 1, 'reconciliation-missing', 1000, 500, 500, 0, 1000, 1000, UTC_TIMESTAMP(3), '{}', UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)),
  (@admin_id, @first_subscription_id + 2, @node_id, @endpoint_id, 'reconciliation-over', 'reconciliation-over', 'scale-reconciliation', 1, 'reconciliation-over', 4000, 2000, 2000, 0, 1000, 4000, UTC_TIMESTAMP(3), '{}', UTC_TIMESTAMP(3), UTC_TIMESTAMP(3));
INSERT INTO orders (
  user_id, subscription_id, plan_id, plan_sku_id, trade_no, order_type,
  amount_cents, payable_amount, paid_amount, refund_amount, discount_amount,
  currency, channel, status, plan_name, sku_name, billing_unit, billing_value,
  traffic_bytes, device_limit, speed_limit_mbps, raw_callback, failure_reason,
  created_at, updated_at
) VALUES
  (@admin_id, @first_subscription_id, @plan_id, @sku_id, 'scale-self-order-1', 'new',
   100, 100, 0, 0, 0, 'USD', 'manual', 'pending', 'Traffic reconciliation fixture',
   'Traffic reconciliation fixture', 'day', 30, 10485760, 1, 0, '', '', UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)),
  (@admin_id, @first_subscription_id + 1, @plan_id, @sku_id, 'scale-self-order-2', 'new',
   100, 100, 100, 0, 0, 'USD', 'manual', 'paid', 'Traffic reconciliation fixture',
   'Traffic reconciliation fixture', 'day', 30, 10485760, 1, 0, '', '', UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)),
  (@admin_id, @first_subscription_id + 2, @plan_id, @sku_id, 'scale-self-order-3', 'new',
   100, 100, 0, 0, 0, 'USD', 'manual', 'failed', 'Traffic reconciliation fixture',
   'Traffic reconciliation fixture', 'day', 30, 10485760, 1, 0, '', 'fixture failure', UTC_TIMESTAMP(3), UTC_TIMESTAMP(3));
SQL

SELF_ORDERS=$(curl -fsS -H "Authorization: Bearer $SECOND_TOKEN" "$BASE/api/v1/orders?paged=true&limit=2")
SELF_PENDING_ORDERS=$(curl -fsS -H "Authorization: Bearer $SECOND_TOKEN" "$BASE/api/v1/orders?paged=true&status=pending&limit=1")
SELF_SUBSCRIPTIONS=$(curl -fsS -H "Authorization: Bearer $SECOND_TOKEN" "$BASE/api/v1/subscriptions?paged=true&limit=2")
SELF_TRAFFIC=$(curl -fsS -H "Authorization: Bearer $SECOND_TOKEN" "$BASE/api/v1/traffic/records?paged=true&limit=2")
SELF_ORDER_TOTAL=$(printf '%s' "$SELF_ORDERS" | grep -o '"total":[0-9][0-9]*' | head -n 1 | cut -d: -f2)
SELF_PENDING_TOTAL=$(printf '%s' "$SELF_PENDING_ORDERS" | grep -o '"total":[0-9][0-9]*' | head -n 1 | cut -d: -f2)
SELF_SUBSCRIPTION_TOTAL=$(printf '%s' "$SELF_SUBSCRIPTIONS" | grep -o '"total":[0-9][0-9]*' | head -n 1 | cut -d: -f2)
SELF_TRAFFIC_TOTAL=$(printf '%s' "$SELF_TRAFFIC" | grep -o '"total":[0-9][0-9]*' | head -n 1 | cut -d: -f2)
test "$SELF_ORDER_TOTAL" = 3
test "$SELF_PENDING_TOTAL" = 1
test "$SELF_SUBSCRIPTION_TOTAL" = 3
test "$SELF_TRAFFIC_TOTAL" = 3
test "$(printf '%s' "$SELF_ORDERS" | grep -o '"trade_no":"scale-self-order-[0-9]"' | wc -l | tr -d ' ')" = 2
test "$(printf '%s' "$SELF_SUBSCRIPTIONS" | grep -o '"plan_name":"Traffic reconciliation fixture"' | wc -l | tr -d ' ')" = 2
SELF_TRAFFIC_ITEMS=$(printf '%s' "$SELF_TRAFFIC" | grep -o '"record_at":"' | wc -l | tr -d ' ')
if [ "$SELF_TRAFFIC_ITEMS" != 2 ]; then
  printf 'self-service traffic page item count=%s expected=2 response_bytes=%s\n' \
    "$SELF_TRAFFIC_ITEMS" "$(printf '%s' "$SELF_TRAFFIC" | wc -c | tr -d ' ')" >&2
  exit 1
fi
case "$SELF_SUBSCRIPTIONS" in
  *'"config":'*)
    printf 'self-service subscription page leaked subscription config\n' >&2
    exit 1
    ;;
esac
case "$SELF_TRAFFIC" in
  *'"report_id":'*|*'"event_type":'*|*'"meta":'*)
    printf 'self-service traffic page leaked raw report metadata\n' >&2
    exit 1
    ;;
  *'"upload_bytes":'*'"download_bytes":'*'"next_cursor":'*) ;;
  *)
    printf 'self-service traffic page omitted bounded display fields or cursor metadata\n' >&2
    exit 1
    ;;
esac
printf 'self-service-pages admin-account-scope=isolated orders=%s pending=%s subscriptions=%s traffic=%s page_size=2\n' "$SELF_ORDER_TOTAL" "$SELF_PENDING_TOTAL" "$SELF_SUBSCRIPTION_TOTAL" "$SELF_TRAFFIC_TOTAL"

RECONCILIATION_ISSUES=$(curl -fsS -H "Authorization: Bearer $TOKEN" "$BASE/api/v1/admin/traffic/reconciliation?paged=true&issues_only=true&user_id=$SECOND_ADMIN_ID&limit=1")
RECONCILIATION_ALL=$(curl -fsS -H "Authorization: Bearer $TOKEN" "$BASE/api/v1/admin/traffic/reconciliation?paged=true&issues_only=false&user_id=$SECOND_ADMIN_ID&limit=1")
RECONCILIATION_ISSUE_TOTAL=$(printf '%s' "$RECONCILIATION_ISSUES" | grep -o '"total":[0-9][0-9]*' | head -n 1 | cut -d: -f2)
RECONCILIATION_ALL_TOTAL=$(printf '%s' "$RECONCILIATION_ALL" | grep -o '"total":[0-9][0-9]*' | head -n 1 | cut -d: -f2)
RECONCILIATION_ISSUE_AGGREGATES=$(printf '%s' "$RECONCILIATION_ISSUES" | sed -n 's/.*"aggregates":\({[^}]*}\).*/\1/p')
RECONCILIATION_ALL_AGGREGATES=$(printf '%s' "$RECONCILIATION_ALL" | sed -n 's/.*"aggregates":\({[^}]*}\).*/\1/p')
test "$RECONCILIATION_ISSUE_TOTAL" = 2
test "$RECONCILIATION_ALL_TOTAL" = 3
test "$RECONCILIATION_ISSUE_AGGREGATES" = "$RECONCILIATION_ALL_AGGREGATES"
for EXPECTED_AGGREGATE in \
  '"subscription_count":3' '"matched_count":1' '"missing_records_count":1' '"over_recorded_count":1' \
  '"flow_used":5000' '"recorded_bytes":6000' '"missing_bytes":2000' '"over_recorded_bytes":3000'
do
  case "$RECONCILIATION_ISSUE_AGGREGATES" in
    *"$EXPECTED_AGGREGATE"*) ;;
    *)
      printf 'traffic reconciliation aggregate missing %s in %s\n' "$EXPECTED_AGGREGATE" "$RECONCILIATION_ISSUE_AGGREGATES" >&2
      exit 1
      ;;
  esac
done
printf 'traffic-reconciliation issues_total=%s all_total=%s aggregates_scope=stable counts=3,1,1,1 bytes=5000,6000,2000,3000\n' "$RECONCILIATION_ISSUE_TOTAL" "$RECONCILIATION_ALL_TOTAL"

docker exec -e MYSQL_PWD="$ZBOARD_MYSQL_ROOT_PASSWORD" -i "$PROJECT-mysql-1" mysql -uroot zboard <<SQL
SET SESSION cte_max_recursion_depth = $RECURSION_LIMIT;
SET @admin_id = $SECOND_ADMIN_ID;
INSERT INTO node_groups (name, code, description, is_enabled)
VALUES ('Maximum task recovery fixture', 'max-task-recovery-fixture', 'Maximum target recovery verification.', 1);
SET @group_id = LAST_INSERT_ID();
INSERT INTO plans (
  name, slug, summary, description, node_group_id, traffic_bytes, speed_limit_mbps,
  max_active_subscriptions, is_renewable, device_limit, family_limit, reset_policy,
  traffic_calc_mode, is_active, sort_order
) VALUES (
  'Maximum task recovery fixture', 'max-task-recovery-fixture', 'Maximum recovery verification', '', @group_id,
  10485760, 0, 0, 1, 1, 0, 0, 0, 1, 0
);
SET @plan_id = LAST_INSERT_ID();
INSERT INTO plan_skus (
  plan_id, code, name, sku_type, billing_unit, billing_value, price_cents, currency,
  traffic_bytes, device_limit, speed_limit_mbps, is_active, sort_order
) VALUES (
  @plan_id, 'max-task-recovery-fixture', 'Maximum task recovery fixture', 'new', 'day', 30, 100,
  'USD', 10485760, 1, 0, 1, 0
);
SET @sku_id = LAST_INSERT_ID();
INSERT INTO subscriptions (
  user_id, plan_id, plan_sku_id, node_group_id, subscription_type, start_at, end_at,
  status, flow_total, flow_used, speed_limit_mbps, device_limit, family_limit,
  renewal_price_minor, reset_policy, traffic_calc_mode, config
)
WITH RECURSIVE seq AS (
  SELECT 1 AS n
  UNION ALL SELECT n + 1 FROM seq WHERE n < $TASK_TARGET_COUNT
)
SELECT @admin_id, @plan_id, @sku_id, @group_id, 1, UTC_TIMESTAMP(3),
       DATE_ADD(UTC_TIMESTAMP(3), INTERVAL 30 DAY), 'active', 10485760, 0, 0, 1, 0,
       100, 0, 0, JSON_OBJECT('fixture_sequence', n)
FROM seq;
SQL

MAX_TASK_SUBSCRIPTION_ROWS=$(docker exec -e MYSQL_PWD="$ZBOARD_MYSQL_ROOT_PASSWORD" "$PROJECT-mysql-1" mysql -N -B -uroot zboard -e "
  SELECT subscriptions.id
  FROM subscriptions
  JOIN plans ON plans.id = subscriptions.plan_id
  WHERE plans.slug = 'max-task-recovery-fixture'
  ORDER BY subscriptions.id;
")
test "$(printf '%s\n' "$MAX_TASK_SUBSCRIPTION_ROWS" | sed '/^$/d' | wc -l | tr -d ' ')" = "$TASK_TARGET_COUNT"
MAX_TASK_SUBSCRIPTION_IDS=$(printf '%s\n' "$MAX_TASK_SUBSCRIPTION_ROWS" | sed '/^$/d' | paste -sd, -)
MAX_TASK_PAYLOAD=$(printf '{"type":"quota","scope":{"subscription_ids":[%s]},"content":{"delta_mb":1,"reason":"maximum target recovery verification"},"idempotency_key":"max-task-recovery-fixture","max_attempts":3,"auto_run":false}' "$MAX_TASK_SUBSCRIPTION_IDS")
MAX_TASK_RESPONSE=$(curl -fsS -H 'Content-Type: application/json' -H "Authorization: Bearer $TOKEN" --data-binary "$MAX_TASK_PAYLOAD" "$BASE/api/v1/admin/tasks")
MAX_TASK_ID=$(printf '%s' "$MAX_TASK_RESPONSE" | grep -o '"id":[0-9][0-9]*' | head -n 1 | cut -d: -f2)
test -n "$MAX_TASK_ID"

MAX_TASK_TOTAL=$(docker exec -e MYSQL_PWD="$ZBOARD_MYSQL_ROOT_PASSWORD" "$PROJECT-mysql-1" mysql -N -uroot zboard -e "SELECT total FROM tasks WHERE id = $MAX_TASK_ID;")
test "$MAX_TASK_TOTAL" = "$TASK_TARGET_COUNT"

docker exec -e MYSQL_PWD="$ZBOARD_MYSQL_ROOT_PASSWORD" -i "$PROJECT-mysql-1" mysql -uroot zboard <<SQL
SET @delta = 1048576;
INSERT INTO quota_events (
  subscription_id, event_type, delta_bytes, balance_before, balance_after,
  reference_type, reference_id, detail
)
SELECT CAST(item.target_id AS UNSIGNED), 'task_adjustment', @delta, 10485760, 11534336,
       'task_item', CAST(item.id AS CHAR), JSON_OBJECT('fixture', true)
FROM task_items item
WHERE item.task_id = $MAX_TASK_ID
ORDER BY item.id
LIMIT 20;

UPDATE subscriptions subscription
JOIN (
  SELECT target_id
  FROM task_items
  WHERE task_id = $MAX_TASK_ID
  ORDER BY id
  LIMIT 20
) seeded ON subscription.id = CAST(seeded.target_id AS UNSIGNED)
SET subscription.flow_total = subscription.flow_total + @delta;

UPDATE task_items item
JOIN (
  SELECT id
  FROM task_items
  WHERE task_id = $MAX_TASK_ID
  ORDER BY id
  LIMIT 10
) seeded ON seeded.id = item.id
SET item.status = 2, item.attempts = 1, item.error = '',
    item.started_at = UTC_TIMESTAMP(3), item.finished_at = UTC_TIMESTAMP(3);

UPDATE task_items item
JOIN (
  SELECT id
  FROM task_items
  WHERE task_id = $MAX_TASK_ID
  ORDER BY id
  LIMIT 10 OFFSET 10
) seeded ON seeded.id = item.id
SET item.status = 1, item.attempts = 1, item.error = '',
    item.started_at = UTC_TIMESTAMP(3), item.finished_at = NULL;

UPDATE tasks
SET status = 1, errors = '', current = 10, attempts = 1,
    started_at = UTC_TIMESTAMP(3), finished_at = NULL,
    locked_by = 'max-task-recovery-dead-worker',
    locked_until = DATE_SUB(UTC_TIMESTAMP(3), INTERVAL 1 MINUTE)
WHERE id = $MAX_TASK_ID;
SQL

MAX_TASK_STARTED_MS=$(date +%s%3N)
MAX_STALE_LOCK_STATUS=$(curl -sS -o "$WORK_DIR/max-task-recovery.json" -w '%{http_code}' -X POST -H "Authorization: Bearer $TOKEN" "$BASE/api/v1/admin/tasks/$MAX_TASK_ID/run")
test "$MAX_STALE_LOCK_STATUS" = 202

MAX_TASK_STATUS=''
for attempt in $(seq 1 2400); do
  MAX_TASK_STATUS=$(docker exec -e MYSQL_PWD="$ZBOARD_MYSQL_ROOT_PASSWORD" "$PROJECT-mysql-1" mysql -N -uroot zboard -e "SELECT status FROM tasks WHERE id = $MAX_TASK_ID;")
  if [ "$MAX_TASK_STATUS" = 2 ] || [ "$MAX_TASK_STATUS" = 3 ]; then break; fi
  sleep 0.5
done
MAX_TASK_ELAPSED_MS=$(($(date +%s%3N) - MAX_TASK_STARTED_MS))
test "$MAX_TASK_STATUS" = 2

MAX_TASK_FINAL=$(docker exec -e MYSQL_PWD="$ZBOARD_MYSQL_ROOT_PASSWORD" "$PROJECT-mysql-1" mysql -N -uroot zboard -e "
SELECT CONCAT_WS('|', task.status, task.current, task.total, task.attempts,
  IF(task.locked_by = '', 'empty', task.locked_by), IF(task.locked_until IS NULL, 'null', 'set'),
  (SELECT COUNT(*) FROM task_items WHERE task_id = task.id AND status = 2),
  (SELECT COUNT(*) FROM task_items WHERE task_id = task.id AND attempts = 1),
  (SELECT COUNT(*) FROM task_items WHERE task_id = task.id AND attempts = 2),
  (SELECT MAX(attempts) FROM task_items WHERE task_id = task.id),
  (SELECT COUNT(*) FROM quota_events event JOIN task_items item ON event.reference_id = CAST(item.id AS CHAR) WHERE item.task_id = task.id AND event.subscription_id = CAST(item.target_id AS UNSIGNED) AND event.event_type = 'task_adjustment' AND event.reference_type = 'task_item'),
  (SELECT COUNT(DISTINCT event.reference_id) FROM quota_events event JOIN task_items item ON event.reference_id = CAST(item.id AS CHAR) WHERE item.task_id = task.id AND event.subscription_id = CAST(item.target_id AS UNSIGNED) AND event.event_type = 'task_adjustment' AND event.reference_type = 'task_item'),
  (SELECT COUNT(*) FROM subscriptions WHERE plan_id = (SELECT id FROM plans WHERE slug = 'max-task-recovery-fixture') AND flow_total = 11534336),
  (SELECT COUNT(*) FROM audit_logs WHERE action = 'task.run' AND target = CONCAT('task:', task.id)))
FROM tasks task
WHERE task.id = $MAX_TASK_ID;")
IFS='|' read -r MAX_FINAL_STATUS MAX_FINAL_CURRENT MAX_FINAL_TOTAL MAX_FINAL_ATTEMPTS MAX_FINAL_LOCK MAX_FINAL_LOCK_UNTIL MAX_COMPLETED_ITEMS MAX_ATTEMPTS_ONE MAX_ATTEMPTS_TWO MAX_ITEM_ATTEMPTS MAX_QUOTA_EVENTS MAX_DISTINCT_REFERENCES MAX_ADJUSTED_SUBSCRIPTIONS MAX_RUN_AUDITS <<< "$MAX_TASK_FINAL"
test "$MAX_FINAL_STATUS" = 2
test "$MAX_FINAL_CURRENT" = "$TASK_TARGET_COUNT"
test "$MAX_FINAL_TOTAL" = "$TASK_TARGET_COUNT"
test "$MAX_FINAL_ATTEMPTS" = 2
test "$MAX_FINAL_LOCK" = empty
test "$MAX_FINAL_LOCK_UNTIL" = null
test "$MAX_COMPLETED_ITEMS" = "$TASK_TARGET_COUNT"
test "$MAX_ATTEMPTS_ONE" = "$((TASK_TARGET_COUNT - 10))"
test "$MAX_ATTEMPTS_TWO" = 10
test "$MAX_ITEM_ATTEMPTS" = 2
test "$MAX_QUOTA_EVENTS" = "$TASK_TARGET_COUNT"
test "$MAX_DISTINCT_REFERENCES" = "$TASK_TARGET_COUNT"
test "$MAX_ADJUSTED_SUBSCRIPTIONS" = "$TASK_TARGET_COUNT"
test "$MAX_RUN_AUDITS" = 1
printf 'task-max-recovery targets=%s stale_lock=%s task_attempts=%s item_attempts_one=%s item_attempts_two=%s quota_events=%s adjusted=%s run_audits=%s elapsed_ms=%s duplicate_execution=0\n' "$MAX_FINAL_TOTAL" "$MAX_STALE_LOCK_STATUS" "$MAX_FINAL_ATTEMPTS" "$MAX_ATTEMPTS_ONE" "$MAX_ATTEMPTS_TWO" "$MAX_QUOTA_EVENTS" "$MAX_ADJUSTED_SUBSCRIPTIONS" "$MAX_RUN_AUDITS" "$MAX_TASK_ELAPSED_MS"

docker exec -e MYSQL_PWD="$ZBOARD_MYSQL_ROOT_PASSWORD" -i "$PROJECT-mysql-1" mysql -uroot zboard <<'SQL'
SET @admin_id = (SELECT id FROM users WHERE email = 'scale-validation@example.invalid' LIMIT 1);
INSERT INTO node_groups (name, code, description, is_enabled)
VALUES ('Task recovery fixture', 'task-recovery-fixture', 'Isolated task recovery verification.', 1);
SET @group_id = LAST_INSERT_ID();
INSERT INTO plans (
  name, slug, summary, description, node_group_id, traffic_bytes, speed_limit_mbps,
  max_active_subscriptions, is_renewable, device_limit, family_limit, reset_policy,
  traffic_calc_mode, is_active, sort_order
) VALUES (
  'Task recovery fixture', 'task-recovery-fixture', 'Recovery verification', '', @group_id,
  10485760, 0, 0, 1, 1, 0, 0, 0, 1, 0
);
SET @plan_id = LAST_INSERT_ID();
INSERT INTO plan_skus (
  plan_id, code, name, sku_type, billing_unit, billing_value, price_cents, currency,
  traffic_bytes, device_limit, speed_limit_mbps, is_active, sort_order
) VALUES (
  @plan_id, 'task-recovery-fixture', 'Task recovery fixture', 'new', 'day', 30, 100,
  'USD', 10485760, 1, 0, 1, 0
);
SET @sku_id = LAST_INSERT_ID();
INSERT INTO subscriptions (
  user_id, plan_id, plan_sku_id, node_group_id, subscription_type, start_at, end_at,
  status, flow_total, flow_used, speed_limit_mbps, device_limit, family_limit,
  renewal_price_minor, reset_policy, traffic_calc_mode, config
) VALUES
  (@admin_id, @plan_id, @sku_id, @group_id, 1, UTC_TIMESTAMP(3), DATE_ADD(UTC_TIMESTAMP(3), INTERVAL 30 DAY), 'active', 10485760, 0, 0, 1, 0, 100, 0, 0, '{}'),
  (@admin_id, @plan_id, @sku_id, @group_id, 1, UTC_TIMESTAMP(3), DATE_ADD(UTC_TIMESTAMP(3), INTERVAL 30 DAY), 'active', 10485760, 0, 0, 1, 0, 100, 0, 0, '{}'),
  (@admin_id, @plan_id, @sku_id, @group_id, 1, UTC_TIMESTAMP(3), DATE_ADD(UTC_TIMESTAMP(3), INTERVAL 30 DAY), 'active', 10485760, 0, 0, 1, 0, 100, 0, 0, '{}');
SQL

RECOVERY_SUB_IDS=$(docker exec -e MYSQL_PWD="$ZBOARD_MYSQL_ROOT_PASSWORD" "$PROJECT-mysql-1" mysql -N -uroot zboard -e "SELECT id FROM subscriptions WHERE plan_id = (SELECT id FROM plans WHERE slug = 'task-recovery-fixture') ORDER BY id;")
set -- $RECOVERY_SUB_IDS
RECOVERY_SUB_ONE="${1:-}"
RECOVERY_SUB_TWO="${2:-}"
RECOVERY_SUB_THREE="${3:-}"
test -n "$RECOVERY_SUB_ONE"
test -n "$RECOVERY_SUB_TWO"
test -n "$RECOVERY_SUB_THREE"

RECOVERY_TASK_PAYLOAD=$(printf '{"type":"quota","scope":{"subscription_ids":[%s,%s]},"content":{"delta_mb":1,"reason":"task recovery verification"},"idempotency_key":"task-recovery-fixture","max_attempts":3,"auto_run":false}' "$RECOVERY_SUB_ONE" "$RECOVERY_SUB_TWO")
RECOVERY_TASK_RESPONSE=$(curl -fsS -H 'Content-Type: application/json' -H "Authorization: Bearer $TOKEN" --data-binary "$RECOVERY_TASK_PAYLOAD" "$BASE/api/v1/admin/tasks")
RECOVERY_TASK_ID=$(printf '%s' "$RECOVERY_TASK_RESPONSE" | grep -o '"id":[0-9][0-9]*' | head -n 1 | cut -d: -f2)
test -n "$RECOVERY_TASK_ID"

RECOVERY_ITEM_IDS=$(docker exec -e MYSQL_PWD="$ZBOARD_MYSQL_ROOT_PASSWORD" "$PROJECT-mysql-1" mysql -N -uroot zboard -e "SELECT id FROM task_items WHERE task_id = $RECOVERY_TASK_ID ORDER BY id;")
set -- $RECOVERY_ITEM_IDS
RECOVERY_ITEM_ONE="${1:-}"
RECOVERY_ITEM_TWO="${2:-}"
test -n "$RECOVERY_ITEM_ONE"
test -n "$RECOVERY_ITEM_TWO"

docker exec -e MYSQL_PWD="$ZBOARD_MYSQL_ROOT_PASSWORD" -i "$PROJECT-mysql-1" mysql -uroot zboard <<SQL
SET @delta = 1048576;
UPDATE subscriptions SET flow_total = flow_total + @delta WHERE id IN ($RECOVERY_SUB_ONE, $RECOVERY_SUB_TWO);
INSERT INTO quota_events (
  subscription_id, event_type, delta_bytes, balance_before, balance_after,
  reference_type, reference_id, detail
) VALUES
  ($RECOVERY_SUB_ONE, 'task_adjustment', @delta, 10485760, 11534336, 'task_item', '$RECOVERY_ITEM_ONE', JSON_OBJECT('fixture', true)),
  ($RECOVERY_SUB_TWO, 'task_adjustment', @delta, 10485760, 11534336, 'task_item', '$RECOVERY_ITEM_TWO', JSON_OBJECT('fixture', true));
UPDATE task_items
SET status = 2, attempts = 1, error = '', started_at = UTC_TIMESTAMP(3), finished_at = UTC_TIMESTAMP(3)
WHERE id = $RECOVERY_ITEM_ONE;
UPDATE task_items
SET status = 1, attempts = 1, error = '', started_at = UTC_TIMESTAMP(3), finished_at = NULL
WHERE id = $RECOVERY_ITEM_TWO;
UPDATE tasks
SET status = 1, errors = '', current = 1, attempts = 1, started_at = UTC_TIMESTAMP(3), finished_at = NULL,
    locked_by = 'task-recovery-live-worker', locked_until = DATE_ADD(UTC_TIMESTAMP(3), INTERVAL 5 MINUTE)
WHERE id = $RECOVERY_TASK_ID;
SQL

ACTIVE_LOCK_STATUS=$(curl -sS -o "$WORK_DIR/task-recovery-active-lock.json" -w '%{http_code}' -X POST -H "Authorization: Bearer $TOKEN" "$BASE/api/v1/admin/tasks/$RECOVERY_TASK_ID/run")
test "$ACTIVE_LOCK_STATUS" = 409
ACTIVE_LOCK_ATTEMPTS=$(docker exec -e MYSQL_PWD="$ZBOARD_MYSQL_ROOT_PASSWORD" "$PROJECT-mysql-1" mysql -N -uroot zboard -e "SELECT attempts FROM tasks WHERE id = $RECOVERY_TASK_ID;")
test "$ACTIVE_LOCK_ATTEMPTS" = 1

docker exec -e MYSQL_PWD="$ZBOARD_MYSQL_ROOT_PASSWORD" "$PROJECT-mysql-1" mysql -uroot zboard -e "UPDATE tasks SET locked_until = DATE_SUB(UTC_TIMESTAMP(3), INTERVAL 1 MINUTE) WHERE id = $RECOVERY_TASK_ID;" >/dev/null
STALE_LOCK_STATUS=$(curl -sS -o "$WORK_DIR/task-recovery-stale-lock.json" -w '%{http_code}' -X POST -H "Authorization: Bearer $TOKEN" "$BASE/api/v1/admin/tasks/$RECOVERY_TASK_ID/run")
test "$STALE_LOCK_STATUS" = 202

RECOVERY_TASK_STATUS=''
for attempt in $(seq 1 50); do
  RECOVERY_TASK_DETAIL=$(curl -fsS -H "Authorization: Bearer $TOKEN" "$BASE/api/v1/admin/tasks/$RECOVERY_TASK_ID")
  RECOVERY_TASK_STATUS=$(printf '%s' "$RECOVERY_TASK_DETAIL" | grep -o '"status":[0-9][0-9]*' | head -n 1 | cut -d: -f2)
  if [ "$RECOVERY_TASK_STATUS" = 2 ] || [ "$RECOVERY_TASK_STATUS" = 3 ]; then break; fi
  sleep 0.2
done
test "$RECOVERY_TASK_STATUS" = 2

RECOVERY_FINAL=$(docker exec -e MYSQL_PWD="$ZBOARD_MYSQL_ROOT_PASSWORD" "$PROJECT-mysql-1" mysql -N -uroot zboard -e "
SELECT CONCAT_WS('|', task.status, task.current, task.attempts,
  IF(task.locked_by = '', 'empty', task.locked_by), IF(task.locked_until IS NULL, 'null', 'set'),
  item_one.status, item_one.attempts, item_two.status, item_two.attempts,
  subscription_one.flow_total, subscription_two.flow_total,
  (SELECT COUNT(*) FROM quota_events WHERE reference_type = 'task_item' AND reference_id IN ('$RECOVERY_ITEM_ONE', '$RECOVERY_ITEM_TWO')))
FROM tasks task
JOIN task_items item_one ON item_one.id = $RECOVERY_ITEM_ONE
JOIN task_items item_two ON item_two.id = $RECOVERY_ITEM_TWO
JOIN subscriptions subscription_one ON subscription_one.id = $RECOVERY_SUB_ONE
JOIN subscriptions subscription_two ON subscription_two.id = $RECOVERY_SUB_TWO
WHERE task.id = $RECOVERY_TASK_ID;")
IFS='|' read -r FINAL_TASK_STATUS FINAL_CURRENT FINAL_ATTEMPTS FINAL_LOCK FINAL_LOCK_UNTIL FINAL_ITEM_ONE_STATUS FINAL_ITEM_ONE_ATTEMPTS FINAL_ITEM_TWO_STATUS FINAL_ITEM_TWO_ATTEMPTS FINAL_FLOW_ONE FINAL_FLOW_TWO FINAL_QUOTA_EVENTS <<< "$RECOVERY_FINAL"
test "$FINAL_TASK_STATUS" = 2
test "$FINAL_CURRENT" = 2
test "$FINAL_ATTEMPTS" = 2
test "$FINAL_LOCK" = empty
test "$FINAL_LOCK_UNTIL" = null
test "$FINAL_ITEM_ONE_STATUS" = 2
test "$FINAL_ITEM_ONE_ATTEMPTS" = 1
test "$FINAL_ITEM_TWO_STATUS" = 2
test "$FINAL_ITEM_TWO_ATTEMPTS" = 2
test "$FINAL_FLOW_ONE" = 11534336
test "$FINAL_FLOW_TWO" = 11534336
test "$FINAL_QUOTA_EVENTS" = 2
printf 'task-recovery active_lock=%s stale_lock=%s task_attempts=%s completed_item_attempts=%s recovered_item_attempts=%s quota_events=%s duplicate_delta=0\n' "$ACTIVE_LOCK_STATUS" "$STALE_LOCK_STATUS" "$FINAL_ATTEMPTS" "$FINAL_ITEM_ONE_ATTEMPTS" "$FINAL_ITEM_TWO_ATTEMPTS" "$FINAL_QUOTA_EVENTS"

CLAIM_TASK_PAYLOAD=$(printf '{"type":"quota","scope":{"subscription_ids":[%s]},"content":{"delta_mb":2,"reason":"simultaneous administrator claim verification"},"idempotency_key":"task-claim-race-fixture","max_attempts":3,"auto_run":false}' "$RECOVERY_SUB_THREE")
CLAIM_TASK_RESPONSE=$(curl -fsS -H 'Content-Type: application/json' -H "Authorization: Bearer $TOKEN" --data-binary "$CLAIM_TASK_PAYLOAD" "$BASE/api/v1/admin/tasks")
CLAIM_TASK_ID=$(printf '%s' "$CLAIM_TASK_RESPONSE" | grep -o '"id":[0-9][0-9]*' | head -n 1 | cut -d: -f2)
test -n "$CLAIM_TASK_ID"
CLAIM_ITEM_ID=$(docker exec -e MYSQL_PWD="$ZBOARD_MYSQL_ROOT_PASSWORD" "$PROJECT-mysql-1" mysql -N -uroot zboard -e "SELECT id FROM task_items WHERE task_id = $CLAIM_TASK_ID LIMIT 1;")
test -n "$CLAIM_ITEM_ID"

(
  sleep 0.1
  curl -sS -o "$WORK_DIR/task-claim-admin-one.json" -w '%{http_code}' -X POST -H "Authorization: Bearer $TOKEN" "$BASE/api/v1/admin/tasks/$CLAIM_TASK_ID/run" > "$WORK_DIR/task-claim-admin-one.status"
) &
CLAIM_ONE_PID=$!
(
  sleep 0.1
  curl -sS -o "$WORK_DIR/task-claim-admin-two.json" -w '%{http_code}' -X POST -H "Authorization: Bearer $SECOND_TOKEN" "$BASE/api/v1/admin/tasks/$CLAIM_TASK_ID/run" > "$WORK_DIR/task-claim-admin-two.status"
) &
CLAIM_TWO_PID=$!
wait "$CLAIM_ONE_PID"
wait "$CLAIM_TWO_PID"
CLAIM_ONE_STATUS=$(cat "$WORK_DIR/task-claim-admin-one.status")
CLAIM_TWO_STATUS=$(cat "$WORK_DIR/task-claim-admin-two.status")
CLAIM_STATUS_PAIR=$(printf '%s\n%s\n' "$CLAIM_ONE_STATUS" "$CLAIM_TWO_STATUS" | sort -n | tr '\n' ',' | sed 's/,$//')
test "$CLAIM_STATUS_PAIR" = '202,409'

CLAIM_TASK_STATUS=''
for attempt in $(seq 1 50); do
  CLAIM_TASK_DETAIL=$(curl -fsS -H "Authorization: Bearer $TOKEN" "$BASE/api/v1/admin/tasks/$CLAIM_TASK_ID")
  CLAIM_TASK_STATUS=$(printf '%s' "$CLAIM_TASK_DETAIL" | grep -o '"status":[0-9][0-9]*' | head -n 1 | cut -d: -f2)
  if [ "$CLAIM_TASK_STATUS" = 2 ] || [ "$CLAIM_TASK_STATUS" = 3 ]; then break; fi
  sleep 0.2
done
test "$CLAIM_TASK_STATUS" = 2

CLAIM_FINAL=$(docker exec -e MYSQL_PWD="$ZBOARD_MYSQL_ROOT_PASSWORD" "$PROJECT-mysql-1" mysql -N -uroot zboard -e "
SELECT CONCAT_WS('|', task.status, task.current, task.attempts,
  IF(task.locked_by = '', 'empty', task.locked_by), IF(task.locked_until IS NULL, 'null', 'set'),
  item.status, item.attempts, subscription.flow_total,
  (SELECT COUNT(*) FROM quota_events WHERE reference_type = 'task_item' AND reference_id = '$CLAIM_ITEM_ID'),
  (SELECT COUNT(*) FROM audit_logs WHERE action = 'task.run' AND target = 'task:$CLAIM_TASK_ID'))
FROM tasks task
JOIN task_items item ON item.id = $CLAIM_ITEM_ID
JOIN subscriptions subscription ON subscription.id = $RECOVERY_SUB_THREE
WHERE task.id = $CLAIM_TASK_ID;")
IFS='|' read -r CLAIM_FINAL_STATUS CLAIM_FINAL_CURRENT CLAIM_FINAL_ATTEMPTS CLAIM_FINAL_LOCK CLAIM_FINAL_LOCK_UNTIL CLAIM_ITEM_STATUS CLAIM_ITEM_ATTEMPTS CLAIM_FLOW_TOTAL CLAIM_QUOTA_EVENTS CLAIM_RUN_AUDITS <<< "$CLAIM_FINAL"
test "$CLAIM_FINAL_STATUS" = 2
test "$CLAIM_FINAL_CURRENT" = 1
test "$CLAIM_FINAL_ATTEMPTS" = 1
test "$CLAIM_FINAL_LOCK" = empty
test "$CLAIM_FINAL_LOCK_UNTIL" = null
test "$CLAIM_ITEM_STATUS" = 2
test "$CLAIM_ITEM_ATTEMPTS" = 1
test "$CLAIM_FLOW_TOTAL" = 12582912
test "$CLAIM_QUOTA_EVENTS" = 1
test "$CLAIM_RUN_AUDITS" = 1
printf 'task-claim-race statuses=%s task_attempts=%s item_attempts=%s quota_events=%s run_audits=%s duplicate_execution=0\n' "$CLAIM_STATUS_PAIR" "$CLAIM_FINAL_ATTEMPTS" "$CLAIM_ITEM_ATTEMPTS" "$CLAIM_QUOTA_EVENTS" "$CLAIM_RUN_AUDITS"

RECOVERY_NODE_IDS=$(docker exec -e MYSQL_PWD="$ZBOARD_MYSQL_ROOT_PASSWORD" "$PROJECT-mysql-1" mysql -N -uroot zboard -e "SELECT id FROM nodes WHERE name LIKE 'scale-node-%' ORDER BY id LIMIT 3;")
set -- $RECOVERY_NODE_IDS
RECOVERY_NODE_ONE="${1:-}"
RECOVERY_NODE_TWO="${2:-}"
RECOVERY_NODE_THREE="${3:-}"
test -n "$RECOVERY_NODE_ONE"
test -n "$RECOVERY_NODE_TWO"
test -n "$RECOVERY_NODE_THREE"

docker exec -e MYSQL_PWD="$ZBOARD_MYSQL_ROOT_PASSWORD" -i "$PROJECT-mysql-1" mysql -uroot zboard <<SQL
SET @admin_id = (SELECT id FROM users WHERE email = 'scale-validation@example.invalid' LIMIT 1);
INSERT INTO tasks (
  type, scope, content, status, errors, total, current, idempotency_key, priority,
  started_at, attempts, max_attempts, locked_by, locked_until
) VALUES (
  'node_lifecycle', JSON_OBJECT('node_ids', JSON_ARRAY($RECOVERY_NODE_ONE, $RECOVERY_NODE_TWO, $RECOVERY_NODE_THREE)),
  JSON_OBJECT('requested_by', @admin_id, 'actor', 'scale-validation@example.invalid', 'lifecycle_status', 'maintenance'),
  1, '', 3, 1, 'node-task-recovery-fixture', 0, UTC_TIMESTAMP(3), 1, 3,
  'node-task-recovery-dead-worker', DATE_SUB(UTC_TIMESTAMP(3), INTERVAL 1 MINUTE)
);
SET @task_id = LAST_INSERT_ID();
INSERT INTO task_items (task_id, target_type, target_id, payload, status, attempts, error, started_at, finished_at)
VALUES
  (@task_id, 'node', '$RECOVERY_NODE_ONE', '{}', 2, 1, '', UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)),
  (@task_id, 'node', '$RECOVERY_NODE_TWO', '{}', 1, 1, '', UTC_TIMESTAMP(3), NULL),
  (@task_id, 'node', '$RECOVERY_NODE_THREE', '{}', 3, 1, 'interrupted worker', UTC_TIMESTAMP(3), UTC_TIMESTAMP(3));
UPDATE nodes SET lifecycle_status = 'maintenance', is_enabled = 0
WHERE id IN ($RECOVERY_NODE_ONE, $RECOVERY_NODE_TWO, $RECOVERY_NODE_THREE);
INSERT INTO audit_logs (user_id, actor, action, target, detail)
VALUES
  (@admin_id, 'scale-validation@example.invalid', 'node.lifecycle.batch', CONCAT('node:', $RECOVERY_NODE_ONE), 'pre-crash applied result'),
  (@admin_id, 'scale-validation@example.invalid', 'node.lifecycle.batch', CONCAT('node:', $RECOVERY_NODE_TWO), 'pre-crash applied result'),
  (@admin_id, 'scale-validation@example.invalid', 'node.lifecycle.batch', CONCAT('node:', $RECOVERY_NODE_THREE), 'pre-crash applied result');
SQL

NODE_RECOVERY_TASK_ID=$(docker exec -e MYSQL_PWD="$ZBOARD_MYSQL_ROOT_PASSWORD" "$PROJECT-mysql-1" mysql -N -uroot zboard -e "SELECT id FROM tasks WHERE idempotency_key = 'node-task-recovery-fixture';")
test -n "$NODE_RECOVERY_TASK_ID"
NODE_RECOVERY_STATUS=$(curl -sS -o "$WORK_DIR/node-task-recovery.json" -w '%{http_code}' -X POST -H "Authorization: Bearer $TOKEN" "$BASE/api/v1/admin/tasks/$NODE_RECOVERY_TASK_ID/run")
test "$NODE_RECOVERY_STATUS" = 202

NODE_RECOVERY_TASK_STATUS=''
for attempt in $(seq 1 50); do
  NODE_RECOVERY_DETAIL=$(curl -fsS -H "Authorization: Bearer $TOKEN" "$BASE/api/v1/admin/tasks/$NODE_RECOVERY_TASK_ID")
  NODE_RECOVERY_TASK_STATUS=$(printf '%s' "$NODE_RECOVERY_DETAIL" | grep -o '"status":[0-9][0-9]*' | head -n 1 | cut -d: -f2)
  if [ "$NODE_RECOVERY_TASK_STATUS" = 2 ] || [ "$NODE_RECOVERY_TASK_STATUS" = 3 ]; then break; fi
  sleep 0.2
done
test "$NODE_RECOVERY_TASK_STATUS" = 2

NODE_RECOVERY_FINAL=$(docker exec -e MYSQL_PWD="$ZBOARD_MYSQL_ROOT_PASSWORD" "$PROJECT-mysql-1" mysql -N -uroot zboard -e "
SELECT CONCAT_WS('|', task.status, task.current, task.attempts,
  GROUP_CONCAT(CONCAT(item.status, ':', item.attempts) ORDER BY item.id SEPARATOR ','),
  SUM(node.lifecycle_status = 'maintenance' AND node.is_enabled = 0),
  (SELECT COUNT(*) FROM audit_logs WHERE action = 'node.lifecycle.batch' AND target IN ('node:$RECOVERY_NODE_ONE', 'node:$RECOVERY_NODE_TWO', 'node:$RECOVERY_NODE_THREE')))
FROM tasks task
JOIN task_items item ON item.task_id = task.id
JOIN nodes node ON node.id = CAST(item.target_id AS UNSIGNED)
WHERE task.id = $NODE_RECOVERY_TASK_ID
GROUP BY task.id, task.status, task.current, task.attempts;")
IFS='|' read -r NODE_FINAL_STATUS NODE_FINAL_CURRENT NODE_FINAL_ATTEMPTS NODE_ITEM_ATTEMPTS NODE_FINAL_STATES NODE_FINAL_AUDITS <<< "$NODE_RECOVERY_FINAL"
test "$NODE_FINAL_STATUS" = 2
test "$NODE_FINAL_CURRENT" = 3
test "$NODE_FINAL_ATTEMPTS" = 2
test "$NODE_ITEM_ATTEMPTS" = '2:1,2:2,2:2'
test "$NODE_FINAL_STATES" = 3
test "$NODE_FINAL_AUDITS" = 3
printf 'node-batch-recovery stale_lock=%s task_attempts=%s item_status_attempts=%s applied_nodes=%s audit_rows=%s duplicate_audit=0\n' "$NODE_RECOVERY_STATUS" "$NODE_FINAL_ATTEMPTS" "$NODE_ITEM_ATTEMPTS" "$NODE_FINAL_STATES" "$NODE_FINAL_AUDITS"

measure_page() {
  name="$1"
  url="$2"
  marker="$3"
  expected_total="$4"
  ceiling="$5"
  response_file="$WORK_DIR/$name.json"

  docker exec -e MYSQL_PWD="$ZBOARD_MYSQL_ROOT_PASSWORD" -i "$PROJECT-mysql-1" mysql -uroot mysql <<'SQL'
SET GLOBAL log_output='TABLE';
SET GLOBAL general_log='OFF';
TRUNCATE TABLE mysql.general_log;
SET GLOBAL general_log='ON';
SQL
  elapsed=$(curl -fsS -o "$response_file" -w '%{time_total}' -H "Authorization: Bearer $TOKEN" "$url")
  docker exec -e MYSQL_PWD="$ZBOARD_MYSQL_ROOT_PASSWORD" -i "$PROJECT-mysql-1" mysql -uroot mysql -e "SET GLOBAL general_log='OFF';" >/dev/null

  total=$(grep -o '"total":[0-9][0-9]*' "$response_file" | head -n 1 | cut -d: -f2)
  items=$(grep -o "\"$marker\"" "$response_file" | wc -l | tr -d ' ')
  queries=$(docker exec -e MYSQL_PWD="$ZBOARD_MYSQL_ROOT_PASSWORD" -i "$PROJECT-mysql-1" mysql -N -uroot mysql -e "SELECT COUNT(*) FROM general_log WHERE user_host LIKE 'zboard%' AND command_type IN ('Query','Execute');")

  printf '%s total=%s items=%s logical_queries=%s ceiling=%s elapsed_seconds=%s\n' "$name" "$total" "$items" "$queries" "$ceiling" "$elapsed"
  if [ "$total" != "$expected_total" ]; then
    printf '%s total mismatch: expected=%s actual=%s\n' "$name" "$expected_total" "$total" >&2
    return 1
  fi
  if [ "$items" != "$PAGE_SIZE" ]; then
    printf '%s page-size mismatch: expected=%s actual=%s\n' "$name" "$PAGE_SIZE" "$items" >&2
    return 1
  fi
  if [ "$queries" -gt "$ceiling" ]; then
    printf '%s query ceiling exceeded: ceiling=%s actual=%s\n' "$name" "$ceiling" "$queries" >&2
    return 1
  fi
}

measure_business_page() {
  name="$1"
  url="$2"
  marker="$3"
  expected_total="$4"
  forbidden_pattern="$5"
  response_file="$WORK_DIR/$name.json"
  deep_one_file="$WORK_DIR/$name-deep-one.json"
  deep_two_file="$WORK_DIR/$name-deep-two.json"
  deep_url="${url/offset=0/offset=$PAGE_SIZE}"

  measure_page "$name" "$url" "$marker" "$expected_total" "$BUSINESS_QUERY_CEILING"
  response_bytes=$(wc -c < "$response_file" | tr -d ' ')
  write_queries=$(docker exec -e MYSQL_PWD="$ZBOARD_MYSQL_ROOT_PASSWORD" -i "$PROJECT-mysql-1" mysql -N -uroot mysql -e "
    SELECT COUNT(*)
    FROM general_log
    WHERE user_host LIKE 'zboard%'
      AND command_type IN ('Query','Execute')
      AND argument REGEXP '^[[:space:]]*(INSERT|UPDATE|DELETE|REPLACE)[[:space:]]';")
  if [ "$response_bytes" -gt 262144 ]; then
    printf '%s response exceeded 256 KiB: bytes=%s\n' "$name" "$response_bytes" >&2
    return 1
  fi
  if [ "$write_queries" != 0 ]; then
    printf '%s GET executed write queries: writes=%s\n' "$name" "$write_queries" >&2
    return 1
  fi
  if [ -n "$forbidden_pattern" ] && grep -Eq "$forbidden_pattern" "$response_file"; then
    printf '%s response leaked a detail-only field matching %s\n' "$name" "$forbidden_pattern" >&2
    return 1
  fi

  curl -fsS -o "$deep_one_file" -H "Authorization: Bearer $TOKEN" "$deep_url"
  curl -fsS -o "$deep_two_file" -H "Authorization: Bearer $TOKEN" "$deep_url"
  sed -E 's/"timestamp":"[^"]*"/"timestamp":"normalized"/' "$deep_one_file" > "$deep_one_file.stable"
  sed -E 's/"timestamp":"[^"]*"/"timestamp":"normalized"/' "$deep_two_file" > "$deep_two_file.stable"
  if ! cmp -s "$deep_one_file.stable" "$deep_two_file.stable"; then
    printf '%s repeated deep page was not stable\n' "$name" >&2
    return 1
  fi
  deep_items=$(grep -o "\"$marker\"" "$deep_one_file" | wc -l | tr -d ' ')
  if [ "$deep_items" != "$PAGE_SIZE" ]; then
    printf '%s deep page-size mismatch: expected=%s actual=%s\n' "$name" "$PAGE_SIZE" "$deep_items" >&2
    return 1
  fi
  printf '%s contract response_bytes=%s writes=0 detail_fields=0 deep_page_stable=1\n' "$name" "$response_bytes"
}

measure_business_page 'users-business' "$BASE/api/v1/admin/users?paged=true&q=scale-user&sort=created_at&direction=desc&limit=$PAGE_SIZE&offset=0" 'active_subscription_count' "$USER_COUNT" '"password"|"email_verified_at"|"last_login_at"'
measure_business_page 'plans-business' "$BASE/api/v1/plans?paged=true&include_inactive=true&q=scale-business-plan&limit=$PAGE_SIZE&offset=0" 'sku_count' "$BUSINESS_PLAN_COUNT" '"description"|"skus"|scale-business-sku'
measure_business_page 'node-groups-business' "$BASE/api/v1/admin/node-groups?paged=true&q=scale-business-group&limit=$PAGE_SIZE&offset=0" 'protocol_endpoint_count' "$BUSINESS_PLAN_COUNT" 'protocol_endpoint_ids'
measure_business_page 'subscriptions-business' "$BASE/api/v1/admin/subscriptions?paged=true&q=scale-user&limit=$PAGE_SIZE&offset=0" 'user_email' "$BUSINESS_SUBSCRIPTION_COUNT" '"config"|fixture_sequence'
measure_business_page 'orders-business' "$BASE/api/v1/admin/orders?paged=true&q=scale-business-order&limit=$PAGE_SIZE&offset=0" 'trade_no' "$ORDER_COUNT" 'raw_callback|failure_reason|provider_trade_no|scale-order-secret|scale processor diagnostic'
TOTAL_SUBSCRIPTION_FIXTURES=$((BUSINESS_SUBSCRIPTION_COUNT + TASK_TARGET_COUNT + 6))
measure_business_page 'reconciliation-business' "$BASE/api/v1/admin/traffic/reconciliation?paged=true&issues_only=false&limit=$PAGE_SIZE&offset=0" 'result' "$TOTAL_SUBSCRIPTION_FIXTURES" '"config"|fixture_sequence'

EXPIRED_BUSINESS_SUBSCRIPTIONS=$(curl -fsS -H "Authorization: Bearer $TOKEN" "$BASE/api/v1/admin/subscriptions?paged=true&q=scale-user&status=expired&limit=1&offset=0")
EXPIRED_BUSINESS_TOTAL=$(printf '%s' "$EXPIRED_BUSINESS_SUBSCRIPTIONS" | grep -o '"total":[0-9][0-9]*' | head -n 1 | cut -d: -f2)
test "$EXPIRED_BUSINESS_TOTAL" -gt 0
case "$EXPIRED_BUSINESS_SUBSCRIPTIONS" in
  *'"status":"expired"'*) ;;
  *)
    printf 'subscription effective-status filter returned a non-expired item\n' >&2
    exit 1
    ;;
esac
printf 'subscriptions effective_status expired_total=%s read_only=1\n' "$EXPIRED_BUSINESS_TOTAL"

measure_page 'nodes' "$BASE/api/v1/nodes?paged=true&limit=$PAGE_SIZE&offset=0" 'ssh_configured' "$NODE_COUNT" "$NODE_QUERY_CEILING"
measure_page 'protocols' "$BASE/api/v1/admin/protocol-endpoints?paged=true&limit=$PAGE_SIZE&offset=0" 'node_name' "$ENDPOINT_COUNT" "$PROTOCOL_QUERY_CEILING"

if [ "$ENDPOINT_COUNT" -le 10000 ]; then
  endpoint_selection=$(curl -fsS -H "Authorization: Bearer $TOKEN" "$BASE/api/v1/admin/protocol-endpoints/selection?active=true")
  endpoint_selection_total=$(printf '%s' "$endpoint_selection" | sed -n 's/.*"total":\([0-9][0-9]*\).*/\1/p')
  endpoint_selection_ids=$(printf '%s' "$endpoint_selection" | sed -n 's/.*"ids":\[\([^]]*\)\].*/\1/p')
  if [ -z "$endpoint_selection_ids" ]; then
    endpoint_selection_count=0
  else
    endpoint_selection_count=$(printf '%s' "$endpoint_selection_ids" | awk -F, '{print NF}')
  fi
  printf 'protocol selection snapshot total=%s ids=%s expected=%s\n' "$endpoint_selection_total" "$endpoint_selection_count" "$ENDPOINT_COUNT"
  if [ "$endpoint_selection_total" != "$ENDPOINT_COUNT" ] || [ "$endpoint_selection_count" != "$ENDPOINT_COUNT" ]; then
    printf 'protocol selection snapshot mismatch: total=%s ids=%s expected=%s\n' "$endpoint_selection_total" "$endpoint_selection_count" "$ENDPOINT_COUNT" >&2
    exit 1
  fi
  case "$endpoint_selection" in
    *'"name":'*|*'"address":'*|*'"config":'*|*'"usage":'*)
      printf 'protocol selection snapshot leaked endpoint details\n' >&2
      exit 1
      ;;
  esac
fi

assert_first_name() {
  name="$1"
  url="$2"
  expected="$3"
  response=$(curl -fsS -H "Authorization: Bearer $TOKEN" "$url")
  actual=$(printf '%s' "$response" | sed -n 's/.*"items":\[{[^}]*"name":"\([^"]*\)".*/\1/p')
  printf 'sort %s first=%s expected=%s\n' "$name" "$actual" "$expected"
  if [ "$actual" != "$expected" ]; then
    printf 'sort %s mismatch: expected=%s actual=%s\n' "$name" "$expected" "$actual" >&2
    return 1
  fi
}

assert_first_name 'nodes-name-desc' "$BASE/api/v1/nodes?paged=true&limit=1&offset=0&sort=name&direction=desc" "scale-node-$(printf '%06d' "$NODE_COUNT")"
assert_first_name 'protocols-name-desc' "$BASE/api/v1/admin/protocol-endpoints?paged=true&limit=1&offset=0&sort=name&direction=desc" "scale-endpoint-$(printf '%07d' "$ENDPOINT_COUNT")"

extract_item_ids() {
  grep -o '"id":[0-9][0-9]*' "$1" | cut -d: -f2
}

extract_item_keys() {
  file="$1"
  mode="$2"
  if [ "$mode" = source_id ]; then
    grep -o '"id":[0-9][0-9]*,"source":"[^"]*"' "$file" | sed 's/"id":\([0-9][0-9]*\),"source":"\([^"]*\)"/\2:\1/'
  else
    extract_item_ids "$file"
  fi
}

measure_cursor_history() {
  name="$1"
  url="$2"
  marker="$3"
  expected_total="$4"
  key_mode="${5:-id}"
  concurrent_mode="${6:-none}"
  first_file="$WORK_DIR/$name-first.json"
  second_file="$WORK_DIR/$name-second.json"
  return_file="$WORK_DIR/$name-return.json"
  fresh_file="$WORK_DIR/$name-fresh.json"

  docker exec -e MYSQL_PWD="$ZBOARD_MYSQL_ROOT_PASSWORD" -i "$PROJECT-mysql-1" mysql -uroot mysql <<'SQL'
SET GLOBAL log_output='TABLE';
SET GLOBAL general_log='OFF';
TRUNCATE TABLE mysql.general_log;
SET GLOBAL general_log='ON';
SQL
  elapsed=$(curl -fsS -o "$first_file" -w '%{time_total}' -H "Authorization: Bearer $TOKEN" "$url")
  docker exec -e MYSQL_PWD="$ZBOARD_MYSQL_ROOT_PASSWORD" -i "$PROJECT-mysql-1" mysql -uroot mysql -e "SET GLOBAL general_log='OFF';" >/dev/null

  total=$(grep -o '"total":[0-9][0-9]*' "$first_file" | head -n 1 | cut -d: -f2)
  items=$(grep -o "$marker" "$first_file" | wc -l | tr -d ' ')
  queries=$(docker exec -e MYSQL_PWD="$ZBOARD_MYSQL_ROOT_PASSWORD" -i "$PROJECT-mysql-1" mysql -N -uroot mysql -e "SELECT COUNT(*) FROM general_log WHERE user_host LIKE 'zboard%' AND command_type IN ('Query','Execute');")
  next_cursor=$(sed -n 's/.*"next_cursor":"\([^"]*\)".*/\1/p' "$first_file")
  printf '%s total=%s items=%s logical_queries=%s ceiling=%s elapsed_seconds=%s next_cursor=%s\n' "$name" "$total" "$items" "$queries" "$HISTORY_QUERY_CEILING" "$elapsed" "$([ -n "$next_cursor" ] && printf present || printf missing)"
  test "$total" = "$expected_total"
  test "$items" = "$PAGE_SIZE"
  test "$queries" -le "$HISTORY_QUERY_CEILING"
  test -n "$next_cursor"

  concurrent_id=''
  if [ "$concurrent_mode" = audit ]; then
    concurrent_id=$(docker exec -e MYSQL_PWD="$ZBOARD_MYSQL_ROOT_PASSWORD" -i "$PROJECT-mysql-1" mysql -N -uroot zboard -e "INSERT INTO audit_logs (user_id, actor, action, target, detail) VALUES ((SELECT id FROM users WHERE email = 'scale-validation@example.invalid' LIMIT 1), 'scale-history', 'scale.concurrent', 'fixture:concurrent', 'inserted between cursor pages'); SELECT LAST_INSERT_ID();")
    test -n "$concurrent_id"
  fi

  curl -fsS -o "$second_file" -H "Authorization: Bearer $TOKEN" "$url&cursor=$next_cursor"
  second_items=$(grep -o "$marker" "$second_file" | wc -l | tr -d ' ')
  previous_cursor=$(sed -n 's/.*"previous_cursor":"\([^"]*\)".*/\1/p' "$second_file")
  test "$second_items" = "$PAGE_SIZE"
  test -n "$previous_cursor"
  test -z "$(LC_ALL=C comm -12 <(extract_item_keys "$first_file" "$key_mode" | LC_ALL=C sort -u) <(extract_item_keys "$second_file" "$key_mode" | LC_ALL=C sort -u))"
  if [ -n "$concurrent_id" ]; then
    test -z "$(extract_item_ids "$second_file" | grep -x "$concurrent_id" || true)"
    curl -fsS -o "$fresh_file" -H "Authorization: Bearer $TOKEN" "$url"
    fresh_total=$(grep -o '"total":[0-9][0-9]*' "$fresh_file" | head -n 1 | cut -d: -f2)
    fresh_first_id=$(extract_item_ids "$fresh_file" | head -n 1)
    test "$fresh_total" = "$((expected_total + 1))"
    test "$fresh_first_id" = "$concurrent_id"
    printf '%s concurrent_write=passed inserted_id=%s old_cursor_excluded=1 fresh_page_visible=1\n' "$name" "$concurrent_id"
  fi

  curl -fsS -o "$return_file" -H "Authorization: Bearer $TOKEN" "$url&cursor=$previous_cursor"
  first_keys=$(extract_item_keys "$first_file" "$key_mode" | tr '\n' ',')
  return_keys=$(extract_item_keys "$return_file" "$key_mode" | tr '\n' ',')
  test "$first_keys" = "$return_keys"
  printf '%s cursor_roundtrip=passed first_second_overlap=0\n' "$name"
}

measure_cursor_history 'audit-history' "$BASE/api/v1/admin/audit-logs?limit=$PAGE_SIZE&actor=scale-history" '"actor":"scale-history"' "$AUDIT_COUNT" id audit
measure_cursor_history 'traffic-history' "$BASE/api/v1/admin/traffic/records?paged=true&limit=$PAGE_SIZE" '"record_at":"' "$((TRAFFIC_COUNT + 3))"
OPERATION_TOTAL=$((OPERATION_PER_SOURCE_COUNT * 3 + 5))
measure_cursor_history 'operation-history' "$BASE/api/v1/admin/operation-logs?limit=$PAGE_SIZE" '"created_at"' "$OPERATION_TOTAL" source_id

if [ "$KEEP_ENVIRONMENT" = true ]; then
  trap - EXIT INT TERM
  printf 'retained project=%s port=%s credential_file=%s\n' "$PROJECT" "$PORT" "$WORK_DIR/browser-credentials.txt"
else
  docker compose -p "$PROJECT" -f "$COMPOSE_FILE" down -v --remove-orphans >/dev/null
  if [ "$BUILD_IMAGE" = true ]; then docker image rm "$PROJECT-zboard" >/dev/null; fi
  rm -rf -- "$WORK_DIR"
  trap - EXIT INT TERM

  test -z "$(docker ps -a --filter "label=com.docker.compose.project=$PROJECT" -q)"
  test -z "$(docker volume ls --filter "label=com.docker.compose.project=$PROJECT" -q)"
  test -z "$(docker network ls --filter "label=com.docker.compose.project=$PROJECT" -q)"
  if [ "$BUILD_IMAGE" = true ]; then test -z "$(docker image ls "$PROJECT-zboard" -q)"; fi
  printf 'cleanup project=%s containers=0 volumes=0 networks=0 image=%s\n' "$PROJECT" "$([ "$BUILD_IMAGE" = true ] && printf removed || printf retained)"
fi
'@

$replacements = [ordered]@{
    "__REMOTE_ROOT__" = $RemoteRoot
    "__PROJECT__" = $Project
    "__PORT__" = [string]$Port
    "__NODE_COUNT__" = [string]$NodeCount
    "__ENDPOINT_COUNT__" = [string]$EndpointCount
    "__USER_COUNT__" = [string]$UserCount
    "__BUSINESS_PLAN_COUNT__" = [string]$BusinessPlanCount
    "__BUSINESS_SUBSCRIPTION_COUNT__" = [string]$BusinessSubscriptionCount
    "__ORDER_COUNT__" = [string]$OrderCount
    "__PLAN_SKU_COUNT__" = [string]$PlanSKUCount
    "__TEMPLATE_COUNT__" = [string]$TemplateCount
    "__PAGE_SIZE__" = [string]$PageSize
    "__NODE_QUERY_CEILING__" = [string]$NodeQueryCeiling
    "__PROTOCOL_QUERY_CEILING__" = [string]$ProtocolQueryCeiling
    "__BUSINESS_QUERY_CEILING__" = [string]$BusinessQueryCeiling
    "__AUDIT_COUNT__" = [string]$AuditCount
    "__TRAFFIC_COUNT__" = [string]$TrafficCount
    "__OPERATION_PER_SOURCE_COUNT__" = [string]$OperationPerSourceCount
    "__TASK_TARGET_COUNT__" = [string]$TaskTargetCount
    "__HISTORY_QUERY_CEILING__" = [string]$HistoryQueryCeiling
    "__BUILD_IMAGE__" = $buildFlag
    "__KEEP_ENVIRONMENT__" = $keepEnvironmentFlag
    "__CLEANUP_ONLY__" = $cleanupOnlyFlag
}
foreach ($replacement in $replacements.GetEnumerator()) {
    $remoteScript = $remoteScript.Replace($replacement.Key, $replacement.Value)
}

$temporaryRoot = Join-Path ([System.IO.Path]::GetTempPath()) "$Project-$stamp"
$localScript = Join-Path $temporaryRoot "verify-scale.sh"
$remoteScriptPath = "/tmp/$Project-verify-scale.sh"
$remoteExitCode = 1
New-Item -ItemType Directory -Path $temporaryRoot | Out-Null
try {
    $utf8WithoutBom = [System.Text.UTF8Encoding]::new($false)
    [System.IO.File]::WriteAllText($localScript, $remoteScript.Replace("`r`n", "`n"), $utf8WithoutBom)
    & scp $localScript "${Target}:$remoteScriptPath"
    if ($LASTEXITCODE -ne 0) {
        throw "Failed to upload the scale validation script."
    }
    & ssh $Target "bash '$remoteScriptPath'"
    $remoteExitCode = $LASTEXITCODE
    if ($remoteExitCode -eq 0 -and $KeepEnvironment) {
        & scp "${Target}:/tmp/$Project/browser-credentials.txt" $credentialLocalPath
        if ($LASTEXITCODE -ne 0) {
            throw "Validation was retained, but its temporary browser credentials could not be copied."
        }
        Write-Output "browser_base_url=http://127.0.0.1:$Port"
        Write-Output "browser_credential_file=$credentialLocalPath"
    }
} finally {
    try {
        & ssh $Target "rm -f -- '$remoteScriptPath'" 2>$null
    } catch {
        # The primary scp/ssh failure remains authoritative when the target is
        # unreachable; best-effort remote-script cleanup must not replace it.
    }
    if (Test-Path -LiteralPath $temporaryRoot) {
        $resolvedTemporary = (Resolve-Path -LiteralPath $temporaryRoot).Path
        $resolvedTempBase = (Resolve-Path -LiteralPath ([System.IO.Path]::GetTempPath())).Path
        if ($resolvedTemporary.StartsWith($resolvedTempBase, [System.StringComparison]::OrdinalIgnoreCase)) {
            [System.IO.Directory]::Delete($resolvedTemporary, $true)
        }
    }
}
if ($remoteExitCode -ne 0) {
    throw "Isolated scale validation failed. The remote cleanup trap was invoked."
}
if ($CleanupOnly -and (Test-Path -LiteralPath $credentialLocalPath)) {
    $resolvedCredentialPath = (Resolve-Path -LiteralPath $credentialLocalPath).Path
    $resolvedTempBase = (Resolve-Path -LiteralPath ([System.IO.Path]::GetTempPath())).Path
    if (-not $resolvedCredentialPath.StartsWith($resolvedTempBase, [System.StringComparison]::OrdinalIgnoreCase)) {
        throw "Refusing to delete a credential file outside the system temporary directory."
    }
    Remove-Item -LiteralPath $resolvedCredentialPath -Force
}
