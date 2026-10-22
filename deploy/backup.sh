#!/usr/bin/env bash
# ============================================================
# 备份：MySQL 全量 + 上传文件目录
#
#   用法：./deploy/backup.sh [备份目录，默认 backups]
#
# 产物：
#   backups/blog-YYYYmmdd-HHMMSS.sql.gz    数据库全量（含建表语句）
#   backups/storage-YYYYmmdd-HHMMSS.tar.gz 上传文件（本地存储驱动）
#
# 恢复见 restore.sh；保留策略默认 7 份，可用 KEEP 环境变量覆盖。
# ============================================================
set -euo pipefail

cd "$(dirname "$0")/.."
ENV_FILE=deploy/.env
BACKUP_DIR="${1:-backups}"
KEEP="${KEEP:-7}"

log() { printf '\033[36m[backup]\033[0m %s\n' "$*"; }
die() { printf '\033[31m[backup] %s\033[0m\n' "$*" >&2; exit 1; }

[[ -f "$ENV_FILE" ]] || die "缺少 $ENV_FILE"
# shellcheck source=deploy/_env.sh
. "$(dirname "$0")/_env.sh"

MYSQL_ROOT_PASSWORD=$(env_get MYSQL_ROOT_PASSWORD)
MYSQL_DATABASE=$(env_get MYSQL_DATABASE blog)
[[ -n "$MYSQL_ROOT_PASSWORD" ]] || die "deploy/.env 缺少 MYSQL_ROOT_PASSWORD"

mkdir -p "$BACKUP_DIR"
TS=$(date +%Y%m%d-%H%M%S)
SQL_FILE="$BACKUP_DIR/blog-$TS.sql.gz"
STORAGE_FILE="$BACKUP_DIR/storage-$TS.tar.gz"

log "备份数据库 $MYSQL_DATABASE → $SQL_FILE"
# --single-transaction：InnoDB 一致性快照，不锁表
docker compose -f deploy/docker-compose.yml exec -T mysql \
  mysqldump -uroot -p"$MYSQL_ROOT_PASSWORD" \
  --single-transaction --quick --routines --events --triggers \
  --default-character-set=utf8mb4 \
  "$MYSQL_DATABASE" | gzip -9 > "$SQL_FILE"

if [[ -d var/storage ]]; then
  log "备份上传文件 var/storage → $STORAGE_FILE"
  tar -czf "$STORAGE_FILE" -C var storage
else
  log "未发现 var/storage，跳过文件备份"
fi

# 校验：gzip 完整性 + 文件非空
gzip -t "$SQL_FILE" || die "备份文件损坏：$SQL_FILE"
[[ -s "$SQL_FILE" ]] || die "备份文件为空：$SQL_FILE"
log "数据库备份 $(du -h "$SQL_FILE" | cut -f1)，校验通过"

# 保留最近 KEEP 份
ls -1t "$BACKUP_DIR"/blog-*.sql.gz 2>/dev/null | tail -n +$((KEEP + 1)) | while read -r old; do
  log "清理旧备份 $old"; rm -f "$old"
done
ls -1t "$BACKUP_DIR"/storage-*.tar.gz 2>/dev/null | tail -n +$((KEEP + 1)) | while read -r old; do
  rm -f "$old"
done

log "完成"
