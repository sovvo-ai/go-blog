#!/usr/bin/env bash
# ============================================================
# 恢复：从 backup.sh 的产物还原数据库与上传文件
#
#   用法：./deploy/restore.sh backups/blog-20261022-160000.sql.gz [storage-....tar.gz]
#
# 危险操作：会 DROP 并重建目标库。脚本要求显式输入 YES 确认。
# 演练建议先恢复到临时库校验，见 docs/BACKUP_RESTORE.md。
# ============================================================
set -euo pipefail

cd "$(dirname "$0")/.."
ENV_FILE=deploy/.env

log() { printf '\033[36m[restore]\033[0m %s\n' "$*"; }
die() { printf '\033[31m[restore] %s\033[0m\n' "$*" >&2; exit 1; }

[[ $# -ge 1 ]] || die "用法：$0 <sql.gz> [storage.tar.gz]"
SQL_FILE="$1"
STORAGE_FILE="${2:-}"
[[ -f "$SQL_FILE" ]] || die "找不到 $SQL_FILE"
[[ -f "$ENV_FILE" ]] || die "缺少 $ENV_FILE"

# shellcheck source=deploy/_env.sh
. "$(dirname "$0")/_env.sh"

MYSQL_ROOT_PASSWORD=$(env_get MYSQL_ROOT_PASSWORD)
MYSQL_DATABASE=$(env_get MYSQL_DATABASE blog)
[[ -n "$MYSQL_ROOT_PASSWORD" ]] || die "deploy/.env 缺少 MYSQL_ROOT_PASSWORD"

gzip -t "$SQL_FILE" || die "备份文件损坏：$SQL_FILE"

echo "即将把 $SQL_FILE 恢复到数据库 ${MYSQL_DATABASE}（现有数据会被删除）。"
read -r -p "确认请输入 YES：" answer
[[ "$answer" == "YES" ]] || die "已取消"

COMPOSE=(docker compose -f deploy/docker-compose.yml)

log "重建数据库 $MYSQL_DATABASE"
"${COMPOSE[@]}" exec -T mysql mysql -uroot -p"$MYSQL_ROOT_PASSWORD" -e \
  "DROP DATABASE IF EXISTS \`$MYSQL_DATABASE\`; CREATE DATABASE \`$MYSQL_DATABASE\` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;"

log "导入数据"
gunzip -c "$SQL_FILE" | "${COMPOSE[@]}" exec -T mysql \
  mysql -uroot -p"$MYSQL_ROOT_PASSWORD" --default-character-set=utf8mb4 "$MYSQL_DATABASE"

if [[ -n "$STORAGE_FILE" ]]; then
  [[ -f "$STORAGE_FILE" ]] || die "找不到 $STORAGE_FILE"
  log "还原上传文件"
  rm -rf var/storage
  tar -xzf "$STORAGE_FILE" -C var
fi

log "恢复完成，建议重启应用：docker compose -f deploy/docker-compose.yml --profile full restart app"
