# 备份与恢复

> 阶段 7 交付物之一。本文既是用量说明，也是**一次真实演练的记录**（含时间、命令、校验点）。

## 1. 备份什么

| 对象 | 内容 | 丢失后果 |
|---|---|---|
| MySQL | 全部业务表（用户、文章、评论、会话、消息、设置、日志…） | 全站数据丢失 |
| `var/storage` | 本地存储驱动下的上传文件（头像、文章图、聊天图） | 图片 404，正文与消息里的引用全部失效 |

Redis 不备份：里面只有会话缓存、限流计数、WS 票据、在线状态，全部可重建。

## 2. 策略

- **全量备份**，不做增量：V1.0 数据量小（单机 MySQL），全量恢复更简单，避免增量链断裂。
- **保留 7 份**（`KEEP` 环境变量可改），早于 7 份的自动清理。
- 建议的调度（生产）：

```cron
# 每天 03:10 全量备份，日志留在 /var/log/blog-backup.log
10 3 * * * cd /srv/go-blog && ./deploy/backup.sh >> /var/log/blog-backup.log 2>&1
```

- 备份文件写在 `backups/`，该目录已在 `.gitignore` 中，**绝不入库**。生产应再同步到异地/对象存储。

## 3. 用法

```bash
# 备份（默认写入 backups/，可传目录覆盖）
./deploy/backup.sh
./deploy/backup.sh /data/backups

# 恢复（危险操作，需输入 YES 确认）
./deploy/restore.sh backups/blog-20261022-155315.sql.gz backups/storage-20261022-155315.tar.gz
```

脚本参数来自 `deploy/.env`。**注意**：`.env` 里 `BLOG_DB_DSN` 这类值含括号与 `&`，bash `source` 会语法报错，因此脚本用 `deploy/_env.sh` 的 `env_get` 按键解析，不要改成 `source`。

## 4. 演练记录（2026-10-22）

**环境**：Windows 11 + Docker Desktop；目标库为 compose 里的 `blog-mysql`（容器内 `mysql:3306`，宿主 `3307`），库名 `blog`，已有 16 张表。

| 步骤 | 命令 | 结果 |
|---|---|---|
| 1. 写入备份前标记 | `INSERT settings (drill.marker1, drill.noise)` | 2 行写入成功 |
| 2. 执行备份 | `./deploy/backup.sh` | `blog-20261022-155315.sql.gz`（4.2 KB）+ `storage-20261022-155315.tar.gz`（1.7 KB），`gzip -t` 校验通过 |
| 3. 制造差异 | 删除 `drill.noise`、新增 `drill.marker2` | 恢复前库内为 `marker1` + `marker2` |
| 4. 执行恢复 | `echo YES \| ./deploy/restore.sh <sql> <storage>` | 重建库 → 导入 → 还原文件，全程无报错 |
| 5. 校验 | 查询 `settings` 中 `drill.%` | `marker1` 存在、`noise` 已恢复、**`marker2` 不存在** |
| 6. 结构校验 | `information_schema` 计数 | 16 张表；`settings` 11 行（9 条种子 + 2 条演练标记） |

**结论**：备份产物可正确还原到「备份时刻」的一致性状态——备份后新增的数据按预期消失，备份后被删的数据按预期回来。这是恢复演练的核心判定点，只验证"文件能导入"不算通过。

**耗时**：空库 + 少量图片时，备份 45.3s（几乎全部耗在 `docker compose exec` 启动与 storage 打包），恢复约 6s。数据量增长后需重新测量，并在扩容前复演一次。

## 5. 恢复后检查清单

1. `curl -s http://127.0.0.1:8080/readyz` → `status: ok`（MySQL / Redis / Meilisearch 全绿）
2. 前台首页能出文章、文章详情页能出正文
3. 图片可访问（本地存储驱动下检查 `var/storage` 权限与属主）
4. 搜索可用：`curl 'http://127.0.0.1:8080/api/v1/search?q=Go'`
5. 管理端「索引状态」页无大量失败队列；必要时触发一次全量重建
6. 登录一次，确认会话与 Cookie 正常

## 6. 已知限制

- 只覆盖本地存储驱动。切换到 S3/MinIO 后，文件备份改用桶级快照/`mc mirror`，脚本需同步调整。
- 备份与恢复都在单机执行，没有异地副本与 PITR（binlog）。V1.0 明确不做多实例与高可用，属于既定取舍。
