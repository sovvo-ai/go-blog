# 墨痕（go-blog）

一个**全栈自研**的技术博客：Go 后端 + Nuxt 3 SSR 前端，覆盖从账号体系、文章主链路、搜索、互动、站内通知、单聊到管理端的完整闭环。

> 契约先行（OpenAPI 3.0）· 双 Cookie + CSRF 鉴权 · WebSocket 实时推送 · Redis 限流 · Prometheus 指标 · Docker 一键部署

## 功能总览

**用户端**

- **账号**：注册 → 邮箱验证 → 登录；双 Cookie（HttpOnly access + refresh）+ CSRF 双提交 + 会话轮换 + 刷新令牌族失效；找回/重置密码
- **文章**：Markdown 写作（服务端渲染 HTML + 目录提取）、状态机（草稿 → 待审核 → 发布 → 下架 → 归档 → 删除）、乐观锁防并发覆盖
- **检索**：分类 / 标签 / 关键词搜索（Meilisearch，写库 → 同步 → 对账 → 失败队列 → 全量重建的完整闭环）
- **互动**：点赞、收藏（收藏夹）、评论（两级回复、先审后显可配）、举报
- **通知**：站内信 + WebSocket 实时推送，未读数、全部已读
- **单聊**：会话内 `seq` 行锁原子分配（并发无空洞）、`client_msg_id` 幂等去重、断线 `after_seq` 补齐、已读回执、免打扰、每日建会话上限、聊天图片走私有桶 + 1 小时签名 URL
- **SEO**：全站 SSR、`sitemap.xml` / `robots.txt`、文章页 canonical 与 OG/Twitter 卡片

**管理端**（13 页，与用户端同域同栈）

概览、文章审核（列表 + 详情）、用户管理（禁用即吊销令牌 + 踢下线）、评论管理、举报处理、分类/标签（含合并）、媒体库、索引状态（失败队列 + 全量重建）、系统设置（审核开关实时生效）、操作日志

**平台能力**

- Prometheus 文本格式的 `/metrics`（请求量 / 延迟直方图 / 在途 / 运行时），仅内网可读
- Redis 固定窗口限流：登录 / 注册 / 发信 / 发消息 / 评论 / 上传分别限速，其余写请求兜底限速；Redis 故障时放行（可用性优先）
- 备份 / 恢复脚本与演练记录（`docs/BACKUP_RESTORE.md`）
- 压测工具与报告（`tools/loadtest`、`docs/LOAD_TEST.md`）

## 技术栈

| 层 | 选型 |
|---|---|
| 后端 | Go 1.26+、Gin、GORM、Google Wire（编译期依赖注入）、coder/websocket |
| 契约 | OpenAPI 3.0 → oapi-codegen 生成 DTO 与接口（spec-first） |
| 存储 | MySQL 8.0（16 表，迁移脚本管理）、Redis 7.2、Meilisearch 1.6 |
| 前端 | Nuxt 3（SSR）+ Vue 3 + Pinia，无重型 UI 库（自有"墨痕"设计体系） |
| 部署 | Docker 多阶段构建 × 7 容器（app / web / nginx / mysql / redis / meilisearch / minio*），Nginx 同域反代 |

\* MinIO 为对象存储预留位，V1.0 用本地磁盘驱动，默认不启动。

## 快速开始（本地开发）

前置：Go 1.26+、Node 22+、Docker（compose v2）、make。

```bash
# 1. 生成环境配置并填入密码
cp deploy/.env.example deploy/.env

# 2. 起基础设施（MySQL 3307 / Redis 6380 / Meilisearch 7700）
make up-infra

# 3. 建表 + 种子数据（容器内执行迁移，宿主机无需装 migrate）
make migrate-up

# 4. 起后端（:8080）
make run

# 5. 起前端（:3300，SSR + 热更新）
cd web && npm install && npm run dev
```

创建管理员：

```bash
make admin-create USERNAME=admin EMAIL=admin@example.com PASSWORD='你的密码'
```

验证：`curl -s http://127.0.0.1:8080/readyz` 应返回 `status: ok`；打开 `http://localhost:3300`。

## 生产部署

```bash
cp deploy/.env.example deploy/.env   # 填好全部必填项
./deploy/deploy.sh                   # 迁移 → 构建镜像 → 起全栈 → 健康检查
```

- Nginx 入口（:80）：`/` → Nuxt SSR，`/api` → Go，`/ws` → WebSocket（Upgrade 透传），`/metrics` 仅内网
- 备份 / 恢复：`deploy/backup.sh`、`deploy/restore.sh`（演练记录见 `docs/BACKUP_RESTORE.md`）
- 性能基线：`docs/LOAD_TEST.md`（1 万文章 / 10 万评论量级，全部指标达标）

## 测试

```bash
go test ./... -count=1     # 19 个包；集成测试无数据库时自动跳过
make test-race             # 竞态检测（需 64 位 cgo 工具链）
cd web && npm run build    # 前端类型与模板检查
```

必测场景（PRD 5.1）：并发 seq 分配、消息幂等、断线补齐、已读单调、刷新令牌轮换、
限流触发、备份恢复演练——对应测试分布在 `internal/ws`、`internal/repository`、`internal/service`。

## 目录结构

```
├── api/                 # openapi.yaml（接口契约）+ 代码生成配置
├── cmd/
│   ├── server/          # 后端入口（HTTP + WebSocket + Worker）
│   └── admincreate/     # 管理员创建工具
├── internal/
│   ├── bootstrap/       # wire 依赖装配
│   ├── config/          # 配置加载（env 覆盖 > 文件 > 默认值）
│   ├── handler/         # HTTP 处理器（81 个接口）
│   ├── service/         # 业务逻辑（状态机、鉴权规则、限流策略）
│   ├── repository/      # 数据访问（GORM）
│   ├── middleware/      # 请求 ID / 日志 / CSRF / 限流 / 指标 / 恢复
│   ├── ws/              # WebSocket Hub（多端连接、踢下线、慢消费者保护）
│   ├── pkg/             # authcookie / jwtutil / markdown / metrics / ratelimit …
│   ├── errs/            # 统一错误码（1xxxx 分段）
│   └── model/           # GORM 模型
├── migrations/          # SQL 迁移（migrate 工具格式）
├── web/                 # Nuxt 3 前端（pages/components/layouts/server 路由）
├── deploy/              # Dockerfile / compose / nginx / 部署与备份脚本
├── docs/                # 备份演练记录、压测报告
├── tools/               # 演示数据播种、压测工具
└── prototypes/          # 视觉稿（静态原型，可独立运行）
```

## 文档索引

| 文档 | 内容 |
|---|---|
| [PRD.md](PRD.md) | 产品需求：功能清单、验收标准（5.1 功能 / 5.2 性能 / 5.3 安全 / 5.4 SEO） |
| [TECH_DESIGN.md](TECH_DESIGN.md) | 技术方案：架构、选型决策、部署、测试策略、风险登记 |
| [API_SPEC.md](API_SPEC.md) | 81 个接口的语义约定（认证 / 分页 / 错误码 / 幂等） |
| [DATABASE.md](DATABASE.md) | 16 张表 DDL、索引设计与变更记录 |
| [FRONTEND_DESIGN.md](FRONTEND_DESIGN.md) | 前端设计：页面清单、路由树、设计 token |
| [docs/BACKUP_RESTORE.md](docs/BACKUP_RESTORE.md) | 备份策略与恢复演练记录 |
| [docs/LOAD_TEST.md](docs/LOAD_TEST.md) | 压测报告（含压测中发现并修复的 3 个问题） |

## 设计取舍（节选）

- **无外键约束**：一致性由应用层保证，换取写入性能与迁移灵活性；级联删除在事务内显式处理。
- **消息序号用 MySQL 行锁而非 Redis INCR**：分配与落库必须同事务，Redis 回滚不了空洞。
- **GIF 不重编码、透明图转 PNG 而非 WebP**：Go 生态无纯 Go WebP 编码器，与"单一静态二进制"冲突，属既定取舍。
- **全量重建索引异步化**：1 万篇实测 3 分 21 秒，同步接口必然撞上 Nginx 60s 网关超时。
- **明确不做**：Grafana 看板、告警体系、链路追踪、K8s、多实例部署（V1.0 范围外）。

## License

仅供学习与个人使用。
