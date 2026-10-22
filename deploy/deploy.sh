#!/usr/bin/env bash
# ============================================================
# 一键部署（阶段 7）
#
#   用法：./deploy/deploy.sh            # 全量：迁移 + 构建 + 起服务 + 健康检查
#         ./deploy/deploy.sh --no-build # 只迁移 + 起服务（镜像已构建）
#
# 前置：deploy/.env 已按 .env.example 填好；Docker 与 compose v2 可用。
#
# 为什么迁移不放到应用启动时：多实例并发启动会抢锁，失败也没法回滚；
# 显式一步执行，失败时脚本直接退出，不会留下"半新半旧"的库。
# ============================================================
set -euo pipefail

cd "$(dirname "$0")/.."
COMPOSE=(docker compose -f deploy/docker-compose.yml)
ENV_FILE=deploy/.env
MIGRATE_IMG=migrate/migrate:v4.17.0
NETWORK=go-blog_default

log() { printf '\033[36m[deploy]\033[0m %s\n' "$*"; }
die() { printf '\033[31m[deploy] %s\033[0m\n' "$*" >&2; exit 1; }

[[ -f "$ENV_FILE" ]] || die "缺少 $ENV_FILE，请先 cp deploy/.env.example deploy/.env 并填写"
# shellcheck source=deploy/_env.sh
. "$(dirname "$0")/_env.sh"

MYSQL_USER=$(env_get MYSQL_USER blog)
MYSQL_PASSWORD=$(env_get MYSQL_PASSWORD)
MYSQL_ROOT_PASSWORD=$(env_get MYSQL_ROOT_PASSWORD)
MYSQL_DATABASE=$(env_get MYSQL_DATABASE blog)
[[ -n "$MYSQL_PASSWORD" ]] || die "deploy/.env 缺少 MYSQL_PASSWORD"
[[ -n "$MYSQL_ROOT_PASSWORD" ]] || die "deploy/.env 缺少 MYSQL_ROOT_PASSWORD"

DO_BUILD=1
[[ "${1:-}" == "--no-build" ]] && DO_BUILD=0

# ---------- 1. 基础设施 ----------
log "启动基础设施（mysql / redis / meilisearch）"
"${COMPOSE[@]}" up -d mysql blog-redis meilisearch

log "等待 MySQL 就绪"
for i in $(seq 1 60); do
  if "${COMPOSE[@]}" exec -T mysql mysqladmin ping -h 127.0.0.1 -uroot -p"$MYSQL_ROOT_PASSWORD" --silent >/dev/null 2>&1; then
    log "MySQL 已就绪（${i}s）"; break
  fi
  [[ $i -eq 60 ]] && die "MySQL 60 秒内未就绪"
  sleep 1
done

# ---------- 2. 迁移 ----------
# 用 migrate 容器 + compose 网络内的服务名，避免依赖宿主机端口映射
log "执行数据库迁移"
docker run --rm \
  --network "$NETWORK" \
  -v "$PWD/migrations:/migrations" \
  "$MIGRATE_IMG" \
  -path=/migrations \
  -database "mysql://${MYSQL_USER}:${MYSQL_PASSWORD}@tcp(mysql:3306)/${MYSQL_DATABASE}" \
  up

# ---------- 3. 构建并启动应用 ----------
if [[ $DO_BUILD -eq 1 ]]; then
  log "构建镜像（app / web）"
  "${COMPOSE[@]}" --profile full build app web
fi

log "启动应用与网关"
"${COMPOSE[@]}" --profile full up -d app web nginx

# ---------- 4. 健康检查 ----------
log "等待应用就绪"
PORT=$(env_get APP_HOST_PORT 8080)
for i in $(seq 1 60); do
  if curl -fsS "http://127.0.0.1:${PORT}/readyz" >/dev/null 2>&1; then
    log "应用就绪（${i}s）"
    break
  fi
  [[ $i -eq 60 ]] && die "应用 60 秒内未就绪，请查看：${COMPOSE[*]} logs app"
  sleep 1
done

NGINX_PORT=$(env_get NGINX_HOST_PORT 80)
log "部署完成：http://127.0.0.1:${NGINX_PORT}"
log "健康检查：curl -s http://127.0.0.1:${PORT}/readyz"
log "指标端点：curl -s http://127.0.0.1:${PORT}/metrics（仅内网）"
