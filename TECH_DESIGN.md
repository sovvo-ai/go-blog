# 技术设计文档

| 项 | 内容 |
|---|---|
| 文档版本 | v1.9 |
| 对应 PRD | v0.6（已同步） |
| 文档状态 | **V1.0 定稿**（D-A / D-B / D-C 已确认，D-D 为条件启用） |
| 上次更新 | 2026-10-21 |
| 适用范围 | V1.0 全量功能 |

### 修订记录

| 版本 | 日期 | 主要变更 |
|---|---|---|
| v1.0 | 2026-10-21 | 初稿 |
| v1.1 | 2026-10-21 | 按评审意见修订 23 项问题。核心修正：Refresh Token 存储模型自相矛盾、seq 分配器非原子、评论默认状态为公开、Cookie Path 缺失、CSRF 与 CORS 未闭环、分类标签计数规则缺失、异步任务补偿不完整、镜像版本不可复现。新增附录 A 问题清单与待决策项 |
| v1.2 | 2026-10-21 | **V1.0 技术方案定稿**。D-A 定案 slog 并新增 logger 包规范；D-B 定案 OpenAPI 3.0.3 spec-first（oapi-codegen），附工具兼容性说明；D-C 定案同域反代，补全路径规划与 Nginx 配置，解除阶段 7 阻塞；D-D 改为条件启用，交接窗口默认关闭并给出启用门槛；部署补上前端 `web` 服务（原方案遗漏），容器数更正为 7 |
| v1.3 | 2026-10-21 | 交叉审查修正。撤回"PRD 写明 zap"的错误表述（PRD 从未指定日志库），PRD 待同步项由 2 处收敛为 1 处；补充 `GET /articles/{id}/edit`（作者取正文原文）与列表排序参数，修复 API 清单缺口；新增 DATABASE.md、API_SPEC.md 两份下游文档的对应关系说明 |
| v1.4 | 2026-10-21 | 两项一致性收口：① PRD 7.1 已同步修订为同域 `/admin`，冲突项归零；② 删除 `comments.like_count`（V1.0 无评论点赞），文章点赞字段与逻辑不受影响。对应 PRD v0.5 |
| v1.5 | 2026-10-21 | 三项设计补齐：① 搜索分页改为 offset/limit 并明确 maxTotalHits=1000 上限（新增 9.6），撤回全站游标的错误表述；② 聊天已读字段改为 last_read_seq，补齐单调递增/并发/越权规则（新增 11.8）；③ 新增 10.9 数据清理任务。对应 PRD v0.6 |
| v1.6 | 2026-10-21 | **Go 版本口径更正并落地**：原写 1.22（已于 2025-02 停止支持），v1.5 阶段实际依赖要求 1.24（已于 2026-02-10 停止支持）。定案 `go` 指令 1.26.0、构建镜像 `golang:1.27.2-alpine` 并固定摘要，新增 2.3 版本策略与升级节奏；依赖锁定说明同步到 15.1；5.2 登记 11006（依赖未就绪）；14.3 补充未启用依赖不注册检查项；15.1 将 Redis 服务改名为 `blog-redis`（项目独立实例，宿主机映射 6380），并补充密码注入与派生配置 |

| v1.7 | 2026-10-21 | 阶段 2 评审修正：① `POST /auth/logout` 恢复 CSRF 校验（API_SPEC 4.1 要求必需，原白名单缺省），契约同步补参数；② 服务层数据依赖改为 `service.UserStore` 接口，wire 绑定实现，使 HTTP 级集成测试可脱离 MySQL 运行；③ 补充端到端集成测试，覆盖注册→验证→登录→CSRF→刷新→退出全链路；④ 标注头像接口延后到阶段 4、注销匿名化留待阶段 7 |

| v1.8 | 2026-10-21 | 存储抽象落地：新增 7.5 存储驱动边界（`storage.Store` + Local Driver，S3 未实现且配置即失败）；`media` 表只存对象 Key；本地存储目录纳入持久化要求；MinIO 从开发必需清单移入 `storage` profile，设计结论保留 |

**阅读前提**：本文档所有业务规则均以 PRD v0.6 为准。凡涉及"谁能做什么""状态怎么变""返回什么状态码"，以 PRD 为唯一依据，本文档只负责说明**如何实现**。两者冲突时以 PRD 为准，并回写修正本文档。

---

## 1. 架构总览

### 1.1 部署拓扑

```
                    ┌──────────────┐
        Internet ──▶│    Nginx     │  TLS 终止 / 静态资源 / 反向代理
                    └──────┬───────┘
                           │
                    ┌──────▼───────┐
                    │  Go 应用进程  │  单二进制，含 HTTP + WebSocket + Worker
                    └──┬───┬───┬───┘
                       │   │   │
          ┌────────────┘   │   └────────────┐
          │                │                │
    ┌─────▼─────┐   ┌──────▼──────┐   ┌─────▼────────┐
    │  MySQL 8  │   │    Redis    │   │ Meilisearch  │
    │  唯一事实源 │   │ 缓存/队列/WS │   │   检索索引    │
    └───────────┘   └─────────────┘   └──────────────┘
                           │
                    ┌──────▼───────┐
                    │    MinIO     │  图片与附件
                    └──────────────┘
```

V1.0 为**单机单进程**部署。Worker（asynq 消费者）与 HTTP 服务在同一个二进制内，通过命令行参数或配置决定是否启用，不强制拆进程。

### 1.2 应用内部分层

```
HTTP 请求
   ↓
middleware    RequestID → 日志 → Recovery → CORS → 限流 → 认证 → 鉴权 → CSRF
   ↓
handler       参数绑定、DTO 转换、调用 service、组装响应。不含业务逻辑
   ↓
service       业务规则、状态机校验、事务边界、领域事件投递
   ↓
repository    数据访问，唯一出现 GORM 的地方
```

依赖方向严格单向。`handler` 不得引用 `gorm`，`service` 不得引用 `gin`。

### 1.3 单进程内的并发模型

| 组件 | 运行方式 |
|---|---|
| HTTP 服务 | 独立 goroutine，监听 8080 |
| WebSocket Hub | 独立 goroutine，持有连接注册表 |
| asynq Server | 独立 goroutine，消费 Redis 队列 |
| asynq Scheduler | 独立 goroutine，触发定时任务 |
| 优雅关闭 | 收到 SIGTERM 后停止接收新请求，等待在途请求与任务，最长 30 秒 |

---

## 2. 技术栈与版本约束

| 组件 | 选型 | 版本要求 | 说明 |
|---|---|---|---|
| Go | — | **go.mod 最低 1.26.0；构建使用 1.27.2** | 见 2.3 版本策略，Dockerfile 固定 `golang:1.27.2-alpine` |
| Web 框架 | gin-gonic/gin | v1.10+ | — |
| ORM | gorm.io/gorm | v1.25+ | 配合 `gorm.io/driver/mysql` |
| 迁移 | golang-migrate/migrate | v4 | 禁用 `AutoMigrate` |
| 缓存 | redis/go-redis | v9 | — |
| 队列 | hibiken/asynq | v0.24+ | 基于 Redis |
| 检索 | meilisearch-go | 最新稳定版 | 官方 SDK，项目启动时在 go.mod 锁定版本 |
| 对象存储 | minio-go | v7 | 兼容 S3 |
| 配置 | spf13/viper | v1.18+ | — |
| 参数校验 | go-playground/validator | v10 | 由 Gin 内置绑定 |
| 日志 | log/slog | 标准库 | 统一封装于 `internal/pkg/logger`，[已确认 D-A] |
| JWT | golang-jwt/jwt | v5 | — |
| WebSocket | coder/websocket | v1.8+ | 原生 context 支持 |
| 密码哈希 | golang.org/x/crypto/argon2 | — | argon2id |
| 依赖注入 | google/wire | v0.6+ | 仅用于装配，不引入运行时容器 |
| 接口文档 | oapi-codegen | v2 | spec-first，从 `api/openapi.yaml` 生成类型，[已确认 D-B] |

**不引入的组件**（与 PRD 6.5 一致）：OpenTelemetry、Jaeger、Kafka、Casbin、Prometheus 服务端、Grafana。

### 2.1 接口文档方案（D-B 定案）

**结论：采用 OpenAPI 3.0.3，spec-first 模式，工具为 `oapi-codegen`。Gin 保持不变。**

#### 2.1.1 工具核实结果

| 方案 | 工具 | OpenAPI 3 支持情况 | 结论 |
|---|---|---|---|
| 注解式 code-first | swaggo/swag | 默认输出 **Swagger 2.0**；3.x 需显式开关，且不同版本参数名不一致（`--oas` / `--openapi`），社区有 Swagger UI 版本校验失败的报告 | **不能作为稳定方案** |
| 框架绑定式 | danielgtaylor/huma | 原生 OpenAPI 3.1，生成质量好 | 要求 handler 基于 huma + net/http 重写，等于更换 Web 框架，与"不换技术栈"冲突 |
| spec-first | oapi-codegen | 完整支持 3.0.x | **采用**。只生成类型，不接管路由，可与 Gin 自由组合 |

上一版写的"swag 生成 OpenAPI 3"是不准确的说法：它能产生 3.x 输出，但参数形态随版本变化、生成结果不稳定，不能作为工程依赖。

#### 2.1.2 为什么选 3.0.3 而不是 3.1

- `oapi-codegen` 对 3.0.x 支持最完整
- 3.1 的 `nullable` 写法、type 数组等特性的生成支持不完整，存在字段丢失风险
- 周边消费工具（文档渲染、客户端生成、契约校验）对 3.0 的支持远比 3.1 成熟

选"能稳定生成、能被工具链完整消费"的版本，而不是版本号最新的那个。

#### 2.1.3 落地方式

```
api/
  openapi.yaml            # 手工维护，契约唯一来源
  oapi-codegen.yaml       # 生成配置
internal/dto/
  api.gen.go              # 自动生成，禁止手改
```

```yaml
# api/oapi-codegen.yaml
package: dto
output: internal/dto/api.gen.go
generate:
  models: true
  chi-server: false
  gin-server: false
```

**不生成 server 骨架**：避免 oapi-codegen 的路由约定与现有 Gin 路由体系冲突。handler 直接使用生成的请求/响应类型，路由仍由 Gin 注册。

工作流：改接口 → 先改 `openapi.yaml` → 重新生成类型 → 编译会强制 handler 适配。

#### 2.1.4 与 PRD 的关系

PRD 5.5 / 8.5 要求"接口文档与实际返回一致"。spec-first 比注解式更能保证这一点：**spec 是编译期依赖，不会与代码漂移**；注解式则是"代码写完补注释"，天然滞后。

这不是对 PRD 的修改，是满足其要求的具体手段。

#### 2.1.5 兜底方案（记录但不起用）

若后续认为手写 yaml 维护成本过高，可退回 swag 生成 Swagger 2.0，再用 `swagger2openapi` 转 3.0。代价是构建链多一个 Node 工具依赖，且转换有损（部分扩展字段丢失）。**V1.0 不采用此路径。**

### 2.2 日志规范（D-A 定案）

**结论：采用 Go 标准库 `log/slog`，全项目统一通过 `internal/pkg/logger` 使用。**

| 规则 | 要求 |
|---|---|
| 唯一入口 | 业务代码只允许调用 `logger.Info / Warn / Error / Debug` 与 `logger.With(...)` |
| 禁止 | 禁止在业务包内 `slog.New(...)`、`log.Printf`、`fmt.Println`，禁止引入 zap / zerolog / logrus |
| 初始化 | 只在 `cmd/server/main.go` 初始化一次，注入 `logger` 包 |
| 字段 | 通过 `logger.With("request_id", ...)` 携带请求级字段，不手工拼接字符串 |
| 级别 | 由配置 `log.level` 控制，生产默认 `info`，开发默认 `debug` |
| 输出 | 生产 JSON 到 stdout（由容器日志采集），开发文本格式到 stderr |

**为什么禁止业务代码自行创建 logger**：多个实例会导致级别控制失效、字段格式不一致、输出目标分散。日志是故障排查的最后手段，格式不统一等于没有。

**为什么不用 zap**：Go 1.21 起 `slog` 已能满足结构化 JSON 日志的全部需求，少一个第三方依赖。

**PRD 无需修订**：PRD 7.1 技术选型表**未列日志实现**。slog 属于本文档的实现层选择，不构成与 PRD 的冲突。

> **更正（v1.3 审查）**：v1.2 声称"PRD 7.1 写的是 zap"，该表述错误——PRD 从未指定日志库。此处已纠正。

### 2.3 Go 版本策略（v1.6 定案）

**原文档写的 1.22 与 1.24 两个数字都是错的，原因是同一个：凭版本号直觉选，没对照官方支持窗口。**

Go 的支持策略是「每个大版本支持到出现两个更新的大版本为止」，约 12 个月：

| 版本 | 发布 | 最后补丁 | 支持状态（2026-10） |
|---|---|---|---|
| 1.24 | 2025-02-11 | 1.24.13（2026-02-04） | **已于 2026-02-10 停止支持** |
| 1.25 | 2025-08-12 | 1.25.14 | 已于 2026-08-19 停止支持 |
| **1.26** | 2026-02-10 | 1.26.9（2026-10-08） | **受支持** |
| **1.27** | 2026-08-19 | 1.27.2（2026-10-08） | **受支持** |

无支持的版本不再收到安全补丁（stdlib 也包含在内），而 Go 程序把 stdlib 静态链进二进制——**升级工具链并重新构建，补丁才会生效**。

**定案：两层版本，各有分工。**

| 层 | 版本 | 作用 |
|---|---|---|
| `go.mod` 的 `go` 指令 | `1.26.0` | 语言与标准库的**最低要求**。开发者用 1.26.x 或 1.27.x 都能构建 |
| Dockerfile / CI 构建镜像 | `golang:1.27.2-alpine` | 生产产物的**实际工具链**，取当前最新补丁 |

这样做的理由：`go` 指令定得太高会把协作者挡在门外，定得太低又用不上新特性；而构建工具链必须取最新补丁，否则已知漏洞会被打进产物。

**依赖兼容性实测**：`oapi-codegen/runtime v1.7.0` 声明 `go 1.24`，在 1.26/1.27 下构建无告警；`gin`、`gorm`、`go-redis`、`viper` 均在受支持版本上正常工作，见 2.3.1。

#### 2.3.1 升级节奏（必须执行，不是建议）

1. **每半年跟进一次大版本**（Go 每年 2 月、8 月发布）。跳过一个大版本，安全窗口就缩短一半。
2. **每月跟补丁**：`go` 指令不动，只改 Dockerfile 与 CI 的补丁版本，重新构建并部署。
3. **CI 只跑一个版本**：跟随 Dockerfile 的补丁版本，避免多版本矩阵拖慢流水线；本地开发允许 1.26+。
4. **升级大版本前先跑 `go test ./...` 与 `go vet ./...`**，重点看 `encoding/json`、`net/http` 的行为变化。

**当前待办**：`go` 指令为 `1.26.0`。2027-02 前后 Go 1.28 发布时，1.26 会退出支持，届时升到 1.27（语言层）与最新的 1.28/1.29（构建层）。已登记在 17 章风险表。

---

## 3. 目录结构

```
go-blog/
├── cmd/
│   └── server/
│       └── main.go              # 唯一入口
├── internal/
│   ├── config/
│   │   └── config.go            # 配置结构体与加载
│   ├── bootstrap/
│   │   ├── app.go               # 依赖装配、生命周期
│   │   ├── db.go                # MySQL 连接池
│   │   ├── redis.go
│   │   └── wire.go              # wire 注入定义
│   ├── router/
│   │   ├── router.go            # 路由注册
│   │   └── routes/
│   │       ├── public.go
│   │       ├── user.go
│   │       ├── admin.go
│   │       └── ws.go
│   ├── middleware/
│   │   ├── request_id.go
│   │   ├── logger.go
│   │   ├── recovery.go
│   │   ├── cors.go
│   │   ├── rate_limit.go
│   │   ├── auth.go              # 解析 Cookie、加载用户
│   │   ├── authz.go             # 角色校验
│   │   └── csrf.go
│   ├── handler/
│   │   ├── auth_handler.go
│   │   ├── user_handler.go
│   │   ├── article_handler.go
│   │   ├── comment_handler.go
│   │   ├── interaction_handler.go
│   │   ├── search_handler.go
│   │   ├── upload_handler.go
│   │   ├── notification_handler.go
│   │   ├── chat_handler.go
│   │   └── admin_handler.go
│   ├── service/
│   │   ├── auth_service.go
│   │   ├── user_service.go
│   │   ├── article_service.go   # 状态机在此
│   │   ├── comment_service.go
│   │   ├── interaction_service.go
│   │   ├── search_service.go
│   │   ├── upload_service.go
│   │   ├── notification_service.go
│   │   ├── chat_service.go
│   │   └── admin_service.go
│   ├── repository/
│   │   ├── user_repo.go
│   │   ├── article_repo.go
│   │   ├── comment_repo.go
│   │   ├── media_repo.go
│   │   ├── conversation_repo.go
│   │   ├── message_repo.go
│   │   └── misc_repo.go         # 分类/标签/通知/日志/设置/举报
│   ├── model/
│   │   └── *.go                 # 与表结构一一对应
│   ├── dto/
│   │   └── *.go                 # 请求与响应结构体
│   ├── authz/
│   │   ├── role.go              # 角色常量与层级比较
│   │   └── permission.go        # 权限点常量
│   ├── errs/
│   │   ├── code.go              # 错误码定义
│   │   └── errors.go            # 业务错误构造
│   ├── search/
│   │   ├── client.go            # Meilisearch 封装
│   │   └── document.go          # 文档结构与转换
│   ├── storage/
│   │   ├── minio.go
│   │   └── image.go             # 压缩与多尺寸
│   ├── ws/
│   │   ├── hub.go               # 连接注册表
│   │   ├── client.go            # 单连接读写循环
│   │   ├── protocol.go          # 消息协议
│   │   └── pubsub.go            # Redis Pub/Sub 桥接
│   ├── worker/
│   │   ├── server.go            # asynq server 与 scheduler
│   │   ├── tasks.go             # 任务定义与 payload
│   │   └── handlers/
│   │       ├── index.go
│   │       ├── image.go
│   │       ├── mail.go
│   │       └── reconcile.go
│   └── pkg/
│       ├── logger/
│       ├── jwtutil/
│       ├── password/
│       ├── idgen/
│       ├── markdown/
│       └── pagination/
├── migrations/
│   ├── 000001_init_users.up.sql
│   ├── 000001_init_users.down.sql
│   └── ...
├── configs/
│   ├── config.yaml              # 默认值
│   ├── config.dev.yaml
│   └── config.prod.yaml
├── deploy/
│   ├── docker-compose.yml
│   ├── Dockerfile
│   └── nginx.conf
├── docs/
│   ├── PRD.md               # 当前位于仓库根目录，后续可迁入此处
│   └── TECH_DESIGN.md       # 同上
├── Makefile
└── go.mod
```

**分层强制规则**（CI 中用 `go vet` 自定义检查或人工 review 保证）：

1. `handler` 包不得 import `gorm.io/*`
2. `service` 包不得 import `github.com/gin-gonic/*`
3. `repository` 包不得 import `gin` 或 `handler`
4. `model` 包不得 import 除 GORM 与标准库外的任何包

---

## 4. 配置管理

### 4.1 加载优先级

`环境变量 > config.{env}.yaml > config.yaml 默认值`

环境变量统一前缀 `BLOG_`，嵌套用下划线，例如 `BLOG_DB_PASSWORD`、`BLOG_JWT_SECRET`。

**密钥类配置只允许通过环境变量注入**，不得写入 yaml 文件，不得提交到仓库。

### 4.2 配置项清单

| 分组 | 键 | 默认值 | 说明 |
|---|---|---|---|
| app | `app.env` | development | development / test / production，非法值启动即失败 |
| app | `app.addr` | :8080 | 监听地址 |
| app | `app.version` | dev | 版本号，部署时注入，用于探针响应 |
| app | `app.shutdown_timeout_sec` | 15 | 优雅退出超时 |
| app | `app.base_url` | http://localhost:8080 | 用于生成邮件链接 |
| app | `app.worker_enabled` | true | 是否在本进程启动 worker |
| log | `log.level` | info | debug / info / warn / error |
| log | `log.format` | text | text（开发）/ json（生产，强制） |
| log | `log.source` | false | 是否输出调用位置，生产关闭 |
| db | `db.dsn` | — **必填** | MySQL DSN，缺失启动失败 |
| db | `db.max_open_conns` | 25 | — |
| db | `db.max_idle_conns` | 10 | 不得大于 max_open_conns |
| db | `db.conn_max_lifetime_min` | 30 | 单位分钟 |
| redis | `redis.addr` | — **必填** | 本地开发指向 `127.0.0.1:6380`（见 15.1） |
| redis | `redis.password` | — | 仅环境变量；生产环境为空则拒绝启动 |
| redis | `redis.db` | 0 | 逻辑库编号，可配 |
| jwt | `jwt.secret` | — **必填** | 仅环境变量，至少 32 字符，否则拒绝启动 |
| jwt | `jwt.issuer` | go-blog | 签发方标识，校验时强制比对 |
| jwt | `jwt.access_ttl_min` | 15 | 访问令牌有效期（分钟） |
| jwt | （refresh） | 7 天 | **代码内常量，不可配**：改它等于改安全边界，不适合做环境变量 |
| cookie | `cookie.secure` | false | 生产强制 true（config 会覆盖） |
| cookie | `cookie.same_site` | lax | lax / strict |
| cookie | （domain） | 不设置 | V1.0 不写 Domain，两端各自登录 |
| search | `search.host` | — | Meilisearch 地址 |
| search | `search.api_key` | — | 仅环境变量 |
| search | `search.index` | articles | 索引名 |
| storage | `storage.endpoint` | — | MinIO 地址（阶段 4 接入） |
| storage | `storage.bucket_public` | blog-public | 头像、文章封面 |
| storage | `storage.bucket_private` | blog-private | 聊天图片 |
| mail | `mail.driver` | console | console / smtp |
| mail | `mail.from` | no-reply@blog.local | 发件人 |
| mail | `mail.smtp_host` / `mail.smtp_port` | — / 587 | `driver=smtp` 时必填 |
| mail | `mail.verify_url_base` | http://localhost:3000/verify-email | 验证链接前缀 |
| mail | `mail.reset_url_base` | http://localhost:3000/reset-password | 重置链接前缀 |
| security | `security.login_fail_limit` | 5 | 连续失败几次锁定 |
| security | `security.login_lock_min` | 15 | 锁定时长（分钟） |
| security | `security.verify_resend_interval_sec` | 60 | 验证邮件最小重发间隔 |
| security | `security.verify_resend_daily_limit` | 10 | 验证邮件每日上限 |
| security | `security.reset_resend_interval_sec` | 60 | 重置邮件最小间隔 |
| security | `security.reset_resend_daily_limit` | 5 | 重置邮件每日上限 |
| upload | `upload.max_image_size` | 10485760 | 10MB |
| upload | `upload.max_chat_image_size` | 5242880 | 5MB |
| log | `log.level` | info | debug / info / warn / error |
| log | `log.format` | json | json（生产）/ text（开发） |
| security | `security.allowed_origins` | 见备注 | Origin 白名单，用于 CSRF 校验（6.7.2） |
| security | `security.refresh_handover` | **false** | Refresh Token 交接窗口开关（6.3.3） |
| security | `security.handover_ttl` | 10s | 交接窗口时长，仅在上一项为 true 时生效 |
| feature | `feature.article_review` | false | 文章审核开关 |
| feature | `feature.comment_review` | true | 评论审核开关 |
| feature | `feature.register_review` | false | 注册审核开关 |

**注意**：`feature.*` 三项虽然写在配置文件里作为初始值，但运行时以数据库 `settings` 表为准，管理端修改后立即生效，重启不丢。

---

## 5. 统一响应与错误码

### 5.1 响应格式

```json
{
  "code": 0,
  "message": "ok",
  "data": {},
  "request_id": "01HQZ8X2K9J4M"
}
```

- `code` 为业务码，`0` 表示成功
- 失败时 `data` 为 `null`，`message` 为可直接展示给用户的中文提示
- `request_id` 每个响应都返回，便于排查

### 5.2 错误码分段

| 区间 | 领域 | 示例 |
|---|---|---|
| 0 | 成功 | — |
| 10000–10999 | 通用 | 10001 参数错误 / 10002 未登录 / 10003 无权限 / 10004 资源不存在 / 10005 请求过于频繁 / 10006 CSRF 校验失败 |
| 11000–11999 | 系统 | 11001 内部错误 / 11002 数据库错误 / 11003 缓存不可用 / 11004 检索服务不可用 / 11005 存储服务不可用 / 11006 依赖未就绪（就绪探针） |
| 20000–20999 | 认证与用户 | 20001 邮箱已注册 / 20002 用户名已占用 / 20003 凭证错误 / 20004 账号未验证 / 20005 账号已禁用 / 20006 账号已锁定 / 20007 验证链接失效 / 20008 重置链接失效 / 20009 原密码错误 |
| 30000–30999 | 文章 | 30001 文章不存在 / 30002 状态不允许此操作 / 30003 slug 冲突 / 30004 编辑冲突（乐观锁） / 30005 标签数量超限 / 30006 文章已删除 |
| 40000–40999 | 评论与互动 | 40001 评论不存在 / 40002 文章已关闭评论 / 40003 重复操作 |
| 50000–50999 | 聊天 | 50001 会话不存在 / 50002 不是会话成员 / 50003 对方账号不可用 / 50004 发起过于频繁 |
| 60000–60999 | 管理端 | 60001 不可操作自身 / 60002 不可撤销最后一个管理员 / 60003 审核理由必填 |

### 5.3 HTTP 状态码映射

| 业务场景 | HTTP 状态码 | 业务码 |
|---|---|---|
| 成功 | 200 / 201 | 0 |
| 参数校验失败 | 400 | 10001 |
| 未登录 / Token 失效 | 401 | 10002 |
| 已登录但无权 | 403 | 10003 |
| 资源不存在（含无权查看的草稿） | 404 | 10004 |
| 资源已永久删除 | 410 | 30006 |
| 冲突（重复注册、slug 冲突、乐观锁） | 409 | 对应业务码 |
| 限流 | 429 | 10005 |
| 服务端错误 | 500 | 11001 |
| 上游依赖不可用 | 503 | 11004 / 11005 |

**关键规则**：PRD 2.2.1 规定草稿、待审核、已下架、已归档文章对无权访问者返回 404。实现时在 `article_service.GetForView` 中判断：非公开状态且访问者不是作者本人也不是管理员 → 返回 `errs.NotFound(10004)`，handler 统一转 404，**不泄露资源存在性**。

已删除文章单独判断，返回 410 + 业务码 30006。

### 5.4 业务错误结构

```go
type BusinessError struct {
    Code    int
    Message string
    HTTP    int
    Cause   error  // 内部原因，不返回给客户端
}
```

service 层返回 `*BusinessError`，handler 层用 `errors.As` 提取，统一由 `middleware.Recovery` 与 `handler.RespondError` 输出。**禁止在 handler 中拼接错误字符串。**

---

## 6. 认证与授权

### 6.1 凭证与 Cookie 属性

| 凭证 | 形式 | Cookie 名 | **Path** | HttpOnly | SameSite | 有效期 |
|---|---|---|---|---|---|---|
| Access Token | JWT (HS256) | `access_token` | **`/`** | ✅ | Lax | 15 分钟 |
| Refresh Token | 不透明随机串 | `refresh_token` | **`/api/v1/auth`** | ✅ | Lax | 7 天 |
| CSRF Token | 不透明随机串 | `csrf_token` | `/` | ❌（前端需读取） | Lax | 与 Refresh 同生命周期 |

统一属性：`Secure=true`（生产环境）、`SameSite=Lax`、**不设置 `Domain`**（仅当前 host 生效）。

**Path 取值依据**

- `access_token` 必须为 `/`。否则公开页渲染、业务接口、静态资源请求都拿不到凭证。
- `refresh_token` 限定为 `/api/v1/auth`，只有刷新与退出两个接口会携带。这样即使业务接口存在 XSS，攻击者也读不到刷新端点以外的凭证，把可被利用的窗口压到最小。
- `csrf_token` 为 `/`，所有写接口都要带上。

**为什么不设 Domain**：用户端与管理端各自独立登录，Cookie 不共享。代价是管理员需要登录两次；换来的是用户端一旦被 XSS，攻击者拿不到管理端凭证。该取舍已在 PRD 2.6 确认。

**代理场景**：Nginx 终止 TLS 时应用侧看到的是 HTTP，但 Cookie 的 `Secure` 由浏览器判定，不受代理影响，无需额外处理。必须确保 Nginx 传递 `X-Forwarded-Proto`，应用据此生成邮件中的绝对链接。

### 6.2 Access Token

Payload：

```json
{
  "uid": 1001,
  "iat": 1729490000,
  "exp": 1729490900,
  "jti": "01HQZ8X2K9J4M"
}
```

**只含 uid**。不含角色、权限、用户名——这是 PRD 2.4 要求的角色即时生效前提。

验签通过后仍需查用户状态（见 6.5），不能仅凭 JWT 有效就放行。

### 6.3 Refresh Token 存储模型

**v1.0 的矛盾**：6.1 定义 Refresh Token 是"不透明随机串"，6.3 却写"解析出 jti"。不透明串不携带任何字段，无法解析。本节给出唯一实现模型，替换原描述。

#### 6.3.1 概念定义

Refresh Token **不是 JWT**，是 32 字节密码学随机数的 base64url 编码（43 字符）。

| 概念 | 定义 |
|---|---|
| `token` | 返回给客户端的原始随机串，只存在于 Cookie |
| `token_hash` | `sha256(token)` 的十六进制串，**服务端唯一存储形式** |
| `family` | 登录时生成的 UUID，同一次登录派生的全部令牌共享，用于整体吊销 |

**服务端只存 hash，不存原文**。这样即使 Redis 被读取，攻击者也拿不到可直接使用的凭证。

**唯一的例外**：6.3.3 的交接窗口会把新 token 原文暂存于 `blog:auth:rt:handover:{hash}`。该机制**默认关闭**，仅在通过 6.3.3 的启用门槛后才开启；开启时的风险控制见该节。

存储结构（统一 `blog:` 前缀，与 8.1 规范一致）：

```
key:    blog:auth:rt:{token_hash}           type: hash
fields: uid, family, issued_at
TTL:    7 天

key:    blog:auth:family:{family}           type: set
members: token_hash ...（轮换时增删）
TTL:    7 天

key:    blog:auth:user_families:{uid}       type: set
members: family ...
TTL:    7 天

key:    blog:auth:rt:hash2family:{token_hash}   type: string
value:  family
TTL:    7 天（仅用于重放时反查 family）
```

#### 6.3.2 轮换流程

```
1. 从 Cookie 读 token，计算 hash = sha256(token)
2. HGETALL blog:auth:rt:{hash}
   ├─ 存在 → 取 uid、family，进入 3
   └─ 不存在 → 进入 6.3.4 的失效判定，结束
3. 生成 newToken、newHash
4. 用 Lua 脚本原子执行（单脚本，避免中间态）：
      DEL    blog:auth:rt:{hash}
      SREM   blog:auth:family:{family} {hash}
      HSET   blog:auth:rt:{newHash} uid {uid} family {family} issued_at {now}
      SADD   blog:auth:family:{family} {newHash}
      SET    blog:auth:rt:hash2family:{newHash} {family} EX 604800
      HSET   blog:auth:rt:meta:{hash} family {family} rotated_at {now} ip_hash {iph} ua_hash {uah}
      EXPIRE blog:auth:rt:meta:{hash} 60
      -- 下一行仅在 security.refresh_handover=true 时执行（开关作为 ARGV 传入）
      SET    blog:auth:rt:handover:{hash} {newToken} EX {handover_ttl}
      EXPIRE blog:auth:rt:{newHash} 604800
5. 签发新 Access Token
6. Set-Cookie 覆盖 access_token / refresh_token / csrf_token
```

**为什么必须用 Lua**：`DEL 旧 + HSET 新 + 改索引` 若分多条命令执行，网络中断或进程崩溃会留下"旧已删、新未建"或"新旧都存在"的状态，前者导致用户莫名掉线，后者导致一个 token 被用两次仍能轮换。

**脚本必须是单 key 族操作**：`blog:auth:*` 全部落在同一个 Redis 集群 slot（通过 hash tag），否则 Redis Cluster 下跨 slot 的 Lua 会失败。V1.0 单机部署不涉及，但 key 设计已预留（见 8.4）。

#### 6.3.3 并发刷新与交接窗口（D-D 条件启用）

前端必须单例化刷新，但后端不能依赖前端——多标签页、多设备下前端单例并不可靠。并发请求同时带旧 token 刷新时，若不处理，第二个请求会因"旧 token 已轮换"而失败，用户被迫跳登录页。

**交接窗口是为解决这个问题设计的，但 V1.0 默认关闭。**

| 配置 | 默认值 | 说明 |
|---|---|---|
| `security.refresh_handover` | **false** | 交接窗口开关 |
| `security.handover_ttl` | 10s | 窗口时长 |

##### 关闭时（V1.0 默认行为）

关闭交接后，出现一个必须正面处理的问题：**"并发的第二个请求"与"真正的重放攻击"在数据上完全相同**，都是"旧 token 再次出现"。若一律按重放吊销 family，弱网用户会被反复踢下线。

因此关闭交接时采用**宽容判定**，而不是直接吊销。判定依据是轮换时留下的短期元数据：

```
key:    blog:auth:rt:meta:{hash}     type: hash
fields: family, rotated_at, ip_hash, ua_hash
TTL:    60s
```

| 条件 | 判定 |
|---|---|
| 旧 token 在 60 秒内再次出现，且 IP / UA 指纹与轮换时一致 | 并发刷新：返回 401，客户端立即重试 |
| 旧 token 在 60 秒后出现，或 IP / UA 不符 | 重放：吊销 family |

**这个方案不完美**：60 秒内、同 IP 同 UA 的真实重放会被放过。但它优于"把正常用户反复踢下线"。交接窗口的作用正是消除这个模糊地带——这也是它值得在验证通过后启用的原因。

##### 启用时

```
刷新请求发现 blog:auth:rt:{hash} 不存在时：
  1. 查 blog:auth:rt:handover:{hash}
     ├─ 存在 → 判为并发刷新，直接返回该 token，不重新轮换
     └─ 不存在 → 进入 6.3.4
```

第二个并发请求拿到与第一个相同的新 token，判定确定，不再依赖 IP/UA 推测。

##### 交接数据的安全影响

| 风险 | 评估 |
|---|---|
| Redis 被读取 | 只能拿到 10 秒内刚签发的 token，且需先知道旧 hash 才能定位 |
| AOF 落盘 | 明文可能随 AOF 落盘；接受该风险或关闭交接 |
| 日志泄露 | 该值只写 Redis，不写日志 |

##### 启用门槛

**以下 6 项全部通过前，`security.refresh_handover` 保持 `false`。**

| # | 验证项 | 通过标准 |
|---|---|---|
| 1 | 轮换原子性 | 注入 Lua 中断，不存在"旧已删、新未建"或"新旧并存" |
| 2 | 并发刷新 | 同一旧 token 并发 5 次，均成功且返回同一新 token |
| 3 | **过期一致性** | handover 与 rt 生命周期同步；handover 过期后不得返回已失效 token |
| 4 | 重放识别 | 窗口后的旧 token 使用，family 全部吊销 |
| 5 | family 撤销 | 改密码 / 禁用后，该用户所有 family 与 handover 一并清除 |
| 6 | 降级回退 | 关闭开关后行为退回本节的宽容判定，无残留状态 |

第 3 项是 v1.1 遗漏的边界：交接数据必须与 token 本身同生命周期，否则会出现"返回一个已失效的 token"，客户端拿到后立刻再失败一次。

#### 6.3.4 失效与重放判定

判定顺序固定，不可调换：

| 顺序 | 条件 | 判定 | 动作 |
|---|---|---|---|
| 1 | `blog:auth:rt:{hash}` 存在 | 正常刷新 | 执行轮换 |
| 2 | 不存在，交接已启用，`handover:{hash}` 存在 | 并发刷新 | 返回交接 token |
| 3 | 不存在，`hash2family:{hash}` 不存在 | 已过期或从未签发 | 401，清空 Cookie |
| 4 | 不存在，`hash2family` 存在，`meta` 在 60 秒内且 IP/UA 一致 | 并发刷新（宽容判定） | 401，客户端重试 |
| 5 | 不存在，`hash2family` 存在，其余情况 | **重放** | 吊销 family，记录安全日志 |

`hash2family` 保留 7 天，意味着**已轮换的旧令牌在 7 天内再次出现都会被识别**。60 秒的 `meta` 仅用于区分"并发"与"重放"；超过 60 秒一律按重放处理。

#### 6.3.5 撤销矩阵

| 触发操作 | 撤销范围 | 附带动作 |
|---|---|---|
| 用户主动退出 | 当前 token_hash 所属 family | 清空 Cookie |
| 用户修改密码 | 该用户**全部** family | 清空 Cookie、踢 WS、`DEL blog:auth:user:{uid}` |
| 用户被禁用 | 该用户全部 family | 清空 Cookie、踢 WS、`DEL blog:auth:user:{uid}` |
| 用户被启用 | 不撤销（令牌已全失效） | `DEL blog:auth:user:{uid}` |
| 管理员调整角色 | **不撤销** | 仅 `DEL blog:auth:user:{uid}` |
| 检测到重放 | 涉事 family | 记录 IP、UA 到安全日志 |
| 重置密码成功 | 同"修改密码" | — |

**角色调整为什么不撤销令牌**：JWT 只含 uid，角色每次请求实时读取，缓存失效后立即生效。撤销令牌只会让用户无谓掉线。

**修改密码为什么必须撤销**：密码变更意味着旧凭证可能已泄露，必须全线重新认证。

**按用户批量吊销的完整流程**

```
1. SMEMBERS blog:auth:user_families:{uid}    → [f1, f2, ...]
2. 对每个 family：
     SMEMBERS blog:auth:family:{f}           → [h1, h2, ...]
     逐个 DEL blog:auth:rt:{h}
     DEL blog:auth:family:{f}
3. DEL blog:auth:user_families:{uid}
4. DEL blog:auth:user:{uid}                  （权限缓存，见 6.5.1）
```

遍历中若某个 key 已不存在，视为成功，不报错（幂等）。

#### 6.3.6 为什么不用 JWT 做 Refresh Token

JWT 无法撤销。刷新令牌的核心需求恰恰是"可立即失效"。自包含令牌要实现撤销就得维护黑名单，等于又回到服务端存储，反而不如直接用不透明串 + hash 简洁。

---

### 6.4 邮箱验证与密码重置令牌

这两类令牌与登录态无关，独立建模。**共同要求：一次性消费、短有效期、服务端只存 hash**。

| 项 | 邮箱验证 | 密码重置 |
|---|---|---|
| 生成时机 | 注册成功、用户申请重发 | 用户提交找回申请 |
| 长度 | 32 字节随机数 base64url | 同左 |
| Redis key | `blog:auth:email_verify:{sha256(token)}` | `blog:auth:pwd_reset:{sha256(token)}` |
| 存储值 | `uid` | `uid` |
| 有效期 | **24 小时** | **15 分钟** |
| 消费方式 | `GETDEL`（原子取出并删除） | `GETDEL` |
| 重发限制 | 60 秒 1 次、24 小时 10 次 | 60 秒 1 次、24 小时 5 次 |
| 失效条件 | 已使用、过期、用户状态已变 active | 已使用、过期、密码已被修改 |

**一次性消费的实现**：必须用 `GETDEL`（Redis 6.2+）或 Lua 脚本，不能 `GET` 后再 `DEL`——两步之间可能被并发请求重复消费。

**批量失效**：

- 修改密码成功后，删除该用户所有 `blog:auth:pwd_reset:*`（通过 `blog:auth:pwd_reset_user:{uid}` Set 索引）
- 邮箱验证成功后，删除对应的 `blog:auth:email_verify:*`

**未验证用户申请重置密码**：按 PRD 3.5.1，实际执行"重发验证邮件"，不生成重置令牌。实现上在 `ForgotPassword` 中先查用户状态，`pending_verify` 走重发分支。

**邮件发送失败的补偿**：邮件任务失败不影响令牌生成（令牌已落 Redis）。用户可通过"重发"接口自行恢复。这一点是"邮件任务不需要后台补偿"的依据——**它是用户可主动重试的操作**，与索引同步这类用户不可见的后台任务性质不同。

### 6.5 请求鉴权中间件

```
middleware.Auth（可选中间件，用于区分匿名与登录）
  1. 从 Cookie 读 access_token，无则标记匿名
  2. JWT 验签，失败则标记匿名
  3. 取 uid，读 Redis blog:auth:user:{uid}
     ├─ 命中 → 拿到 {status, role, version}
     └─ 未命中 → 查 MySQL，回写 Redis TTL 60s
  4. status != active → 返回 401/403 并清除 Cookie
  5. 写入 gin.Context: uid, role
```

#### 6.5.1 权限缓存不能只靠 TTL

**这是与内容缓存的根本区别，必须单独规定。**

- 内容缓存（文章、列表）：删除失败可接受，TTL 兜底
- 权限缓存（`blog:auth:user:{uid}`）：**删除失败必须视为操作失败**

原因很直接：缓存失效失败意味着被封禁的用户仍能继续发文章、发评论、访问管理接口。这是安全事故，不是体验问题。

| 步骤 | 要求 |
|---|---|
| TTL | **10 秒**（不是 60 秒），作为最后一道防线 |
| 删除时机 | 数据库事务提交后立即执行 |
| 删除失败 | 重试 3 次（间隔 50ms）；仍失败则返回 503 |
| 返回 503 时 | 操作日志标记"缓存失效失败，权限可能延迟生效"，提示管理员重试 |
| 禁用用户 | 除缓存外，还要执行 6.3.5 的全量令牌撤销 + WS 踢下线 |

**角色变更与禁用的即时生效**：四类操作（禁用、启用、改角色、改密码）都必须执行上述流程。正常路径下毫秒级生效，异常路径下最多 10 秒（TTL）且会明确报错，不会出现"看起来禁用了、实际没禁用"的静默失败。

**禁用用户的完整动作序列**（缺一即为漏洞）：

1. 事务内更新 `users.status = 2`
2. 事务提交
3. `DEL blog:auth:user:{uid}`（失败重试，仍失败返回 503）
4. 按 6.3.5 吊销该用户全部 family 的 Refresh Token
5. 向 `blog:ws:kick` 频道 publish `{uid, reason:"disabled"}`
6. 各节点收到后关闭该用户全部 WebSocket 连接（close code 4003）
7. 写 `operation_logs`

### 6.6 授权中间件

```go
// RequireRole 声明该路由所需的最低角色
router.POST("/articles", RequireRole(authz.Author), articleHandler.Create)
```

角色层级比较：

```go
var level = map[Role]int{
    RoleReader: 1,
    RoleAuthor: 2,
    RoleAdmin:  3,
}
// 通过条件：当前角色 level >= 要求 level
```

**资源归属校验不在中间件做**，必须在 service 层。因为归属信息需要查库，且不同资源的规则不同（文章仅作者本人，评论本人或管理员）。

```go
// article_service.Update
if article.AuthorID != operatorID {
    return errs.Forbidden("无权编辑该文章")
}
```

即使调用者是管理员，此处同样拒绝——PRD 2.3 明确管理员不可编辑他人正文。管理员的下架、删除走独立接口，不复用编辑接口。

### 6.7 CSRF、CORS 与 Origin 校验

#### 6.7.1 部署形态（D-C 定案：同域反代）

**结论：用户端、管理端、API 全部在同一域名下，由 Nginx 按路径分发。不开放 CORS。**

| 形态 | 前端域 | API 域 | Cookie 是否跨站 |
|---|---|---|---|
| 本地开发 | `localhost:3000`（web）/ `5173`（admin） | `localhost:8080` | 同站（跨端口不影响） |
| **生产（定案）** | `www.example.com` | 同域，`/api` 前缀 | **同站，无 CORS** |
| ~~生产（备选）~~ | ~~`www.example.com`~~ | ~~`api.example.com`~~ | 已弃用，不采用 |

##### 路径规划

| 路径 | 后端目标 | 说明 |
|---|---|---|
| `/` | `web` 服务（Nuxt3 Node） | 用户端，SSR 渲染 |
| `/_nuxt/*` | `web` 服务 | Nuxt 构建产物，长缓存 |
| `/admin/*` | Nginx 静态目录 | 管理端 SPA，`try_files` 回退 index.html |
| `/api/v1/*` | `app`（Gin） | 业务接口 |
| `/ws` | `app`（Gin） | WebSocket |
| `/media/public/*` | `minio` 或 app 代理 | 公开图片（头像、文章封面） |

**为什么管理端放在 `/admin` 子路径而不是子域名**

- 子域名（`admin.example.com`）与主站属于同一 site，`SameSite=Lax` 不提供任何额外保护，却会引入"要不要共享 Cookie"的额外决策
- 同路径下 Cookie 天然共享，管理员无需二次登录
- 安全边界改由**角色鉴权 + IP 白名单（可选）**保证，而不是靠域名隔离

这是对 PRD 7.1 中"独立应用、独立域名"备注的**收敛**：该备注为 `[建议]` 级，不是强制业务要求。用户端与管理端仍是两个独立构建单元（`web` / `admin`），只是共用同一域名，用路径区分。**PRD 7.1 已同步修订。**

##### Cookie 与 CSRF 配置

| 项 | 值 | 说明 |
|---|---|---|
| Cookie Domain | 不设置 | 仅当前 host |
| Cookie Path | `access_token=/`，`refresh_token=/api/v1/auth` | 见 6.1 |
| SameSite | Lax | 同站足够 |
| Secure | true（生产） | — |
| `security.allowed_origins` | `https://www.example.com` | Origin 校验白名单 |
| CSRF | 双提交 `X-CSRF-Token` | 见 6.7.2 |

**为什么仍然需要 CSRF 防护**：同站不等于安全。跨站表单提交时 `Origin` 会指向攻击者域名，必须由服务端拒绝；`SameSite=Lax` 只能挡住部分场景（如跨站 POST），对同站内的 XSS 注入无能为力。

**若将来必须改为独立 API 域名**，需要同时补：`Access-Control-Allow-Origin` 精确回写（禁止 `*`）、`Allow-Credentials: true`、Cookie 改 `SameSite=None; Secure`、前端 `credentials: 'include'`。这是 D-C 被推翻时的完整变更清单。

#### 6.7.2 三层校验

**第一层：SameSite=Lax** — 拦截绝大多数跨站 POST。Lax 仅在顶级导航 GET 时携带 Cookie。

**第二层：Origin / Referer 校验**（所有写请求）

```
读 Origin 头
  ├─ 存在  → 必须命中 security.allowed_origins 白名单，否则 403
  └─ 不存在 → 回退读 Referer 的 origin 部分
              ├─ 命中白名单 → 放行
              └─ 无 Referer → 拒绝（写请求不接受无来源）
```

白名单来自配置，开发环境为 `http://localhost:3000,http://localhost:5173`，生产为实际域名。

**第三层：CSRF 双提交**

1. 登录成功时生成 `csrf_token`，写入非 HttpOnly Cookie，同时写入响应体
2. 前端读 Cookie，写请求带 `X-CSRF-Token` 头
3. 中间件用 `subtle.ConstantTimeCompare` 比对 Cookie 与 Header，不一致返回 403 + 10006
4. 刷新 Token 时同步更新 `csrf_token`，避免两者生命周期脱节

白名单（此时可能尚无 CSRF Cookie）：`/auth/login`、`/auth/register`、`/auth/refresh`、`/auth/forgot-password`、`/auth/reset-password`、`/auth/verify-email`。

#### 6.7.3 为什么不能只靠 SameSite

SameSite 的判定基于"站点"（eTLD+1），不是"源"。`www.example.com` 与 `admin.example.com` 属于同一站点，Lax **不提供跨子域保护**。而管理端是攻击价值最高的目标。

因此 Origin 校验与 CSRF 双提交是必需项，不是可选项。

### 6.8 登录限流

| 维度 | 限制 | 实现 |
|---|---|---|
| 单账号失败 | 连续 5 次失败锁定 15 分钟 | Redis `blog:auth:fail:{uid}` 计数 |
| 单 IP | 每分钟 10 次登录尝试 | 滑动窗口 |
| 验证邮件重发 | 60 秒 1 次，24 小时 10 次 | Redis 计数 |

登录返回统一错误信息"用户名或密码错误"，不区分账号不存在与密码错误。但 PRD 3.5 要求"重复注册返回明确提示"，那是注册接口，不冲突。

---

## 7. 数据库设计

### 7.1 与 PRD 的对应关系

PRD 8.1 列出 15 类业务对象，实现为 **16 张表**。差异说明：

| PRD 业务对象 | 实现表 | 说明 |
|---|---|---|
| 会话 | `conversations` + `conversation_members` | 拆两张：会话本身（双方、最后消息）与成员维度状态（已读游标、未读数、免打扰）。单聊也需要成员维度状态，不是为群聊预建 |

所有 DDL 使用 InnoDB + `utf8mb4_0900_ai_ci`。所有表不含外键约束（由应用层保证，避免迁移与分库时的耦合）。

### 7.2 建表语句

```sql
-- ========== 1. 用户 ==========
CREATE TABLE `users` (
  `id`                BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `username`          VARCHAR(20)     NOT NULL COMMENT '4-20位字母数字下划线，不可修改',
  `email`             VARCHAR(128)    NOT NULL,
  `password_hash`     VARCHAR(255)    NOT NULL COMMENT 'argon2id',
  `nickname`          VARCHAR(32)     NOT NULL DEFAULT '',
  `avatar_url`        VARCHAR(255)    NOT NULL DEFAULT '',
  `bio`               VARCHAR(255)    NOT NULL DEFAULT '',
  `role`              TINYINT UNSIGNED NOT NULL DEFAULT 1 COMMENT '1=reader 2=author 3=admin',
  `status`            TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '0=pending_verify 1=active 2=disabled',
  `email_verified_at` DATETIME NULL,
  `last_login_at`     DATETIME NULL,
  `last_login_ip`     VARCHAR(45) NOT NULL DEFAULT '',
  `created_at`        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at`        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_username` (`username`),
  UNIQUE KEY `uk_email` (`email`),
  KEY `idx_role_status` (`role`, `status`),
  KEY `idx_created_at` (`created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='用户';

-- 用户表不做软删除，用 status=2 禁用代替（PRD 8.4）

-- ========== 2. 分类 ==========
CREATE TABLE `categories` (
  `id`            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `name`          VARCHAR(64)  NOT NULL,
  `slug`          VARCHAR(64)  NOT NULL,
  `description`   VARCHAR(255) NOT NULL DEFAULT '',
  `sort`          INT          NOT NULL DEFAULT 0,
  `article_count` INT UNSIGNED NOT NULL DEFAULT 0,
  `created_at`    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at`    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_slug` (`slug`),
  KEY `idx_sort` (`sort`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='分类';

-- ========== 3. 标签 ==========
CREATE TABLE `tags` (
  `id`            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `name`          VARCHAR(32)  NOT NULL,
  `slug`          VARCHAR(32)  NOT NULL,
  `article_count` INT UNSIGNED NOT NULL DEFAULT 0,
  `created_at`    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_slug` (`slug`),
  KEY `idx_name` (`name`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='标签';

-- ========== 4. 文章 ==========
CREATE TABLE `articles` (
  `id`                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `author_id`          BIGINT UNSIGNED NOT NULL,
  `category_id`        BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '0=未分类',
  `title`              VARCHAR(200)  NOT NULL,
  `slug`               VARCHAR(200)  NOT NULL,
  `summary`            VARCHAR(512)  NOT NULL DEFAULT '',
  `content_md`         MEDIUMTEXT    NOT NULL COMMENT '唯一真源',
  `content_html`       MEDIUMTEXT    NULL COMMENT '渲染产物，列表不查',
  `cover_url`          VARCHAR(255)  NOT NULL DEFAULT '',
  `status`             TINYINT UNSIGNED NOT NULL DEFAULT 0
                       COMMENT '0=draft 1=pending 2=published 3=rejected 4=offline 5=archived 6=deleted',
  `is_top`             TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `allow_comment`      TINYINT UNSIGNED NOT NULL DEFAULT 1,
  `view_count`         BIGINT UNSIGNED NOT NULL DEFAULT 0,
  `like_count`         INT UNSIGNED NOT NULL DEFAULT 0,
  `favorite_count`     INT UNSIGNED NOT NULL DEFAULT 0,
  `comment_count`      INT UNSIGNED NOT NULL DEFAULT 0,
  `word_count`         INT UNSIGNED NOT NULL DEFAULT 0,
  `reading_time`       INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '分钟',
  `version`            INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '乐观锁',
  `search_sync_status` TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '0=pending 1=synced 2=failed',
  `search_synced_at`   DATETIME NULL,
  `reject_reason`      VARCHAR(255) NOT NULL DEFAULT '',
  `offline_reason`     VARCHAR(255) NOT NULL DEFAULT '',
  `published_at`       DATETIME NULL,
  `created_at`         DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at`         DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `deleted_at`         DATETIME NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_slug` (`slug`),
  KEY `idx_status_top_published` (`status`, `is_top`, `published_at`),
  KEY `idx_author_status` (`author_id`, `status`, `updated_at`),
  KEY `idx_category_status` (`category_id`, `status`, `published_at`),
  KEY `idx_sync_scan` (`search_sync_status`, `updated_at`),
  KEY `idx_updated_at` (`updated_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='文章';

-- idx_sync_scan 是对账任务的扫描依据：WHERE search_sync_status != 1 OR search_synced_at < updated_at
-- 该索引让扫描走覆盖索引，避免全表扫

-- ========== 5. 文章标签关联 ==========
CREATE TABLE `article_tags` (
  `article_id` BIGINT UNSIGNED NOT NULL,
  `tag_id`     BIGINT UNSIGNED NOT NULL,
  PRIMARY KEY (`article_id`, `tag_id`),
  KEY `idx_tag` (`tag_id`, `article_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='文章标签关联';

-- ========== 6. 评论 ==========
CREATE TABLE `comments` (
  `id`               BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `article_id`       BIGINT UNSIGNED NOT NULL,
  `user_id`          BIGINT UNSIGNED NOT NULL,
  `parent_id`        BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '0=主楼',
  `root_id`          BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '所属主楼，0=自身即主楼',
  `reply_to_user_id` BIGINT UNSIGNED NOT NULL DEFAULT 0,
  `content`          VARCHAR(1000) NOT NULL,
  `status`           TINYINT UNSIGNED NOT NULL DEFAULT 0
                     COMMENT '0=pending 1=approved 2=rejected 3=deleted',
  `ip`               VARCHAR(45)  NOT NULL DEFAULT '',
  `user_agent`       VARCHAR(255) NOT NULL DEFAULT '',
  `created_at`       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `deleted_at`       DATETIME NULL,
  PRIMARY KEY (`id`),
  KEY `idx_article_status_root` (`article_id`, `status`, `root_id`, `created_at`),
  KEY `idx_user_created` (`user_id`, `created_at`),
  KEY `idx_status_created` (`status`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='评论';

-- ========== 7. 媒体资源 ==========
CREATE TABLE `media` (
  `id`            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `uploader_id`   BIGINT UNSIGNED NOT NULL,
  `bucket`        VARCHAR(64)  NOT NULL,
  `object_key`    VARCHAR(512) NOT NULL,
  `original_name` VARCHAR(255) NOT NULL DEFAULT '',
  `mime_type`     VARCHAR(128) NOT NULL DEFAULT '',
  `size`          BIGINT UNSIGNED NOT NULL DEFAULT 0,
  `width`         INT UNSIGNED NOT NULL DEFAULT 0,
  `height`        INT UNSIGNED NOT NULL DEFAULT 0,
  `sha256`        CHAR(64)     NOT NULL DEFAULT '',
  `variants`      JSON NULL COMMENT '{"w128":"...","w256":"..."}',
  `purpose`       VARCHAR(32)  NOT NULL DEFAULT 'article' COMMENT 'avatar/article/chat',
  `status`        TINYINT UNSIGNED NOT NULL DEFAULT 1 COMMENT '1=normal 0=deleted',
  `created_at`    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `idx_uploader` (`uploader_id`, `created_at`),
  KEY `idx_sha256` (`sha256`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='媒体资源';

-- ========== 8. 点赞 ==========
CREATE TABLE `article_likes` (
  `user_id`    BIGINT UNSIGNED NOT NULL,
  `article_id` BIGINT UNSIGNED NOT NULL,
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`user_id`, `article_id`),
  KEY `idx_article` (`article_id`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='文章点赞';

-- ========== 9. 收藏 ==========
CREATE TABLE `article_favorites` (
  `user_id`    BIGINT UNSIGNED NOT NULL,
  `article_id` BIGINT UNSIGNED NOT NULL,
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`user_id`, `article_id`),
  KEY `idx_article` (`article_id`, `created_at`),
  KEY `idx_user_created` (`user_id`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='文章收藏';

-- ========== 10. 站内通知 ==========
CREATE TABLE `notifications` (
  `id`         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `user_id`    BIGINT UNSIGNED NOT NULL COMMENT '接收者',
  `type`       VARCHAR(32) NOT NULL COMMENT 'comment_reply/comment_approved/article_approved/article_rejected/article_offline/system',
  `title`      VARCHAR(128) NOT NULL DEFAULT '',
  `content`    VARCHAR(512) NOT NULL DEFAULT '',
  `link`       VARCHAR(255) NOT NULL DEFAULT '',
  `ref_id`     BIGINT UNSIGNED NULL DEFAULT NULL COMMENT '关联对象ID；system 类为 NULL',
  `is_read`    TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_user_type_ref` (`user_id`, `type`, `ref_id`),
  KEY `idx_user_read_created` (`user_id`, `is_read`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='站内通知';

-- ========== 11. 操作日志 ==========
CREATE TABLE `operation_logs` (
  `id`          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `operator_id` BIGINT UNSIGNED NOT NULL,
  `action`      VARCHAR(64) NOT NULL COMMENT 'article.delete/article.offline/user.disable/user.role_change/comment.delete',
  `target_type` VARCHAR(32) NOT NULL,
  `target_id`   BIGINT UNSIGNED NOT NULL DEFAULT 0,
  `detail`      JSON NULL COMMENT '变更前后快照与原因',
  `ip`          VARCHAR(45) NOT NULL DEFAULT '',
  `user_agent`  VARCHAR(255) NOT NULL DEFAULT '',
  `created_at`  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `idx_operator_created` (`operator_id`, `created_at`),
  KEY `idx_target` (`target_type`, `target_id`),
  KEY `idx_action_created` (`action`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='操作审计日志';

-- ========== 12. 系统设置 ==========
CREATE TABLE `settings` (
  `config_key`   VARCHAR(64) NOT NULL COMMENT '避免使用保留字 key',
  `config_value` TEXT NOT NULL,
  `value_type`   VARCHAR(16) NOT NULL DEFAULT 'string' COMMENT 'string/int/bool/json',
  `config_group` VARCHAR(32) NOT NULL DEFAULT '' COMMENT 'site/comment/review',
  `description`  VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at`   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`config_key`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='系统设置';

-- ========== 13. 举报 ==========
CREATE TABLE `reports` (
  `id`           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `reporter_id`  BIGINT UNSIGNED NOT NULL,
  `target_type`  VARCHAR(32) NOT NULL COMMENT 'article/comment/message/conversation',
  `target_id`    BIGINT UNSIGNED NOT NULL,
  `reason`       VARCHAR(64) NOT NULL,
  `detail`       VARCHAR(512) NOT NULL DEFAULT '',
  `status`       TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '0=pending 1=accepted 2=rejected',
  `handler_id`   BIGINT UNSIGNED NOT NULL DEFAULT 0,
  `handled_at`   DATETIME NULL,
  `handle_note`  VARCHAR(255) NOT NULL DEFAULT '',
  `created_at`   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_reporter_target` (`reporter_id`, `target_type`, `target_id`),
  KEY `idx_status_created` (`status`, `created_at`),
  KEY `idx_target` (`target_type`, `target_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='举报';

-- ========== 14. 会话 ==========
CREATE TABLE `conversations` (
  `id`                   BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `type`                 TINYINT UNSIGNED NOT NULL DEFAULT 1 COMMENT '1=single（V1.0 仅此值）',
  `conversation_key`     VARCHAR(64) NOT NULL COMMENT 'min(uid):max(uid)',
  `creator_id`           BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '发起人：每日新建会话上限按此计数（PRD 4.1.6.1）',
  `last_message_id`      BIGINT UNSIGNED NOT NULL DEFAULT 0,
  `last_message_at`      DATETIME NULL,
  `last_message_preview` VARCHAR(128) NOT NULL DEFAULT '',
  `message_count`        INT UNSIGNED NOT NULL DEFAULT 0,
  `seq_counter`          BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '会话内消息序号分配器',
  `created_at`           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at`           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_conversation_key` (`conversation_key`),
  KEY `idx_last_message_at` (`last_message_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='会话';

-- type 字段为单值，保留它的成本是 1 字节，收益是 V1.1 加群聊时不必 ALTER TABLE。
-- 这不属于"为未验证功能预建表"，因为会话与群聊共用同一套消息表结构。

-- ========== 15. 会话成员 ==========
CREATE TABLE `conversation_members` (
  `conversation_id`       BIGINT UNSIGNED NOT NULL,
  `user_id`               BIGINT UNSIGNED NOT NULL,
  `last_read_seq`         BIGINT UNSIGNED NOT NULL DEFAULT 0,
  `unread_count`          INT UNSIGNED NOT NULL DEFAULT 0,
  `is_muted`              TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `joined_at`             DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `left_at`               DATETIME NULL,
  PRIMARY KEY (`conversation_id`, `user_id`),
  KEY `idx_user_conv` (`user_id`, `conversation_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='会话成员';

-- ========== 16. 消息 ==========
CREATE TABLE `messages` (
  `id`              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `conversation_id` BIGINT UNSIGNED NOT NULL,
  `sender_id`       BIGINT UNSIGNED NOT NULL,
  `seq`             BIGINT UNSIGNED NOT NULL COMMENT '会话内单调递增',
  `type`            TINYINT UNSIGNED NOT NULL DEFAULT 1 COMMENT '1=text 2=image 3=system',
  `content`         TEXT NOT NULL COMMENT '文本内容或图片 object_key',
  `client_msg_id`   VARCHAR(64) NULL COMMENT '幂等键，系统消息为 NULL',
  `status`          TINYINT UNSIGNED NOT NULL DEFAULT 1 COMMENT '1=normal 2=recalled 3=deleted',
  `created_at`      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_sender_client` (`sender_id`, `client_msg_id`),
  UNIQUE KEY `uk_conv_seq` (`conversation_id`, `seq`),
  KEY `idx_conv_id` (`conversation_id`, `id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='聊天消息';
```

### 7.3 索引设计说明

| 表 | 索引 | 服务的查询 |
|---|---|---|
| `articles` | `idx_status_top_published` | 首页列表：`WHERE status=2 ORDER BY is_top DESC, published_at DESC` |
| `articles` | `idx_author_status` | 我的文章：`WHERE author_id=? AND status=? ORDER BY updated_at DESC` |
| `articles` | `idx_sync_scan` | 对账任务：`WHERE search_sync_status != 1` |
| `comments` | `idx_article_status_root` | 评论区一次拉取整篇文章的两级评论 |
| `messages` | `uk_conv_seq` | 断线补齐：`WHERE conversation_id=? AND seq > ?` |
| `messages` | `idx_conv_id` | 历史消息游标分页 |
| `article_likes` | 主键 `(user_id, article_id)` | 点赞幂等与"我是否点过" |

**不建的索引**：`articles.content_md` 上的全文索引（MySQL ngram 检索质量差，检索走 Meilisearch）；`users.nickname` 模糊搜索（V1.0 不做用户搜索）。

### 7.4 计数规则与事务边界

#### 7.4.1 互动计数（点赞 / 收藏 / 评论）

三个计数字段采用**数据库直写**，不走 Redis 缓冲。

理由：V1.0 并发量不需要缓冲；直写配合唯一索引与事务即可满足 PRD 5.1 第 34 条"并发 100 次点赞后计数与记录数一致"，缓冲方案反而需要额外对账才能达标。

**事务边界（必须严格遵守）**

```sql
BEGIN;
  INSERT IGNORE INTO article_likes (user_id, article_id) VALUES (?, ?);
  -- 仅当 affectedRows = 1 才执行下一条
  UPDATE articles SET like_count = like_count + 1 WHERE id = ?;
COMMIT;
```

| 情况 | 处理 |
|---|---|
| `affectedRows = 1` | 同事务内 `+1` |
| `affectedRows = 0`（已点过） | 不更新计数，直接返回成功（幂等） |
| `UPDATE` 影响 0 行（文章不存在） | 回滚整个事务，返回 30001 |
| 事务中途失败 | 记录与计数一起回滚，不会单边写入 |

取消点赞对称处理：`DELETE` 的 `affectedRows = 1` 才 `-1`，并在 SQL 中加 `AND like_count > 0` 防止负数。收藏同理。

**评论计数只在 `approved` 状态下计入**

| 状态变化 | `comment_count` |
|---|---|
| 新建 → approved（审核关闭） | +1（同事务） |
| 新建 → pending | 不变 |
| pending → approved（审核通过） | +1 |
| approved → rejected | -1 |
| approved → deleted | -1 |
| 主楼删除级联回复 | 按实际删除的 approved 条数递减（同事务） |

**并发安全**：`UPDATE ... SET x = x + 1` 是行内原子操作，不需要读改写。禁止写成 `SELECT count` 再 `SET count = ?`，那会在并发下丢失更新。

#### 7.4.2 分类与标签计数

规则用"状态是否计入"统一表达，不逐状态枚举：

```
countedBefore = (旧状态 == published)
countedAfter  = (新状态 == published)

false → true : +1
true  → false: -1
相同         : 不因状态变化调整
```

| 事件 | 分类计数 | 标签计数 |
|---|---|---|
| 发布（draft/pending/rejected → published） | `category_id != 0` 时 +1 | 每个关联标签 +1 |
| 下架（published → offline） | -1 | -1 |
| 删除（published → deleted） | -1 | -1 |
| 重新上架（offline → published） | +1 | +1 |
| 归档（published → archived） | -1 | -1 |
| 已发布文章改分类 | 旧分类 -1，新分类 +1 | 不变 |
| 已发布文章改标签 | 不变 | 差集：移除的 -1，新增的 +1 |
| 草稿编辑（从未发布） | 不变 | 不变 |
| 归档恢复为草稿 | 不变 | 不变 |

**实现必须收口到单一函数**：

```go
applyCountDelta(tx, oldStatus, newStatus, oldCategoryID, newCategoryID, oldTagIDs, newTagIDs)
```

所有状态转换与关联修改都调用它，禁止在各业务方法里零散 `UPDATE`。

**为什么必须收口**：分类与标签计数是最容易漏的地方。只要有一个入口忘了调整，计数就长期漂移，而且很难发现——页面显示 12 篇，点进去只有 11 篇。收口后只需保证这一个函数被调用。

**计数修复**：管理端提供"重算分类/标签计数"按钮，按 `status=published` 全量重算，用于脏数据修复。该功能不依赖增量正确性。

#### 7.4.3 未分类语义（category_id = 0）

| 项 | 约定 |
|---|---|
| 含义 | `0` 表示"未分类"，**不是 NULL** |
| 分类表 | 不存在 `id = 0` 的记录；分类 ID 从 1 开始 |
| 文章计数 | `category_id = 0` 的文章不计入任何分类的 `article_count` |
| 列表查询 | 公开列表默认包含未分类文章；按分类筛选时 `category_id=0` 作为独立分组 |
| 关联查询 | 查询分类下文章时不 JOIN 分类表，避免 id=0 丢失 |
| 删除分类 | 同事务内先把该分类下所有文章 `category_id` 置 0，再删分类 |
| 校验 | 创建/编辑文章时，若 `category_id != 0` 必须校验分类存在，否则返回 10001 |

**删除分类为什么必须置 0 而不是级联删文章**：文章是用户资产，不能因为管理员删一个分类就消失。置 0 是可逆的，级联删除不可逆。

#### 7.4.4 浏览量

`view_count` 走 Redis 缓冲：`INCR blog:view:{article_id}`，每 60 秒由定时任务批量回写。

**允许丢失**：Redis 重启且未持久化时会丢失未回写的增量。浏览量属于展示数据，不参与权限判定，也**不作为搜索排序字段**（见 9.1），因此可以接受。

**防刷**：同一用户 60 秒内重复访问只计 1 次，用 `SETNX blog:view:uv:{article_id}:{uid}` TTL 60s 实现；未登录用户按 IP 去重。

---

### 7.5 存储驱动边界（v1.8）

**V1.0 只实现本地文件系统驱动。** 这不是权宜之计，而是把"存储"从业务里摘出来的结果：
业务层只认对象 Key，换驱动不改业务代码。

| 层 | 职责 | 不允许做的事 |
|---|---|---|
| 业务服务 | 生成对象 Key、调用 Put/Get/Delete、把 URL 写进 `users.avatar_url` | 直接用 `os.ReadFile` / `os.WriteFile` 操作业务文件；拼磁盘路径 |
| `storage.Store` | 定义 Put / Get / Delete / URL 四个动作，校验 Key 合法性 | 承载业务语义（如"头像要三档"） |
| Local Driver | 路径解析、目录创建、原子写入（临时文件 + rename）、权限位、Content-Type 推断 | 暴露磁盘绝对路径给调用方 |
| S3 Driver | **未实现**。`storage.driver=s3` 会在配置校验阶段直接报错 | 静默回退到 local |

**为什么禁止静默回退**：配置写 `s3` 却悄悄写本地盘，是那种"上线后图片只在某一台机器可见"的故障根源。
宁可启动失败，也不要带着错误配置跑起来。

**对象 Key 规则**（安全边界，不只是格式检查）：

- 只允许 `[a-zA-Z0-9_-]` 片段，最多 8 段，单段可带一个扩展名
- 拒绝 `..`、`.`、空段、前导 `/`、反斜杠；解析后再次用 `filepath.Rel` 确认仍在根目录内
- Key 会拼进文件路径，放过穿越就等于给了任意文件读写能力

**覆写与命名**：`Put` 允许同 Key 覆写（幂等），但头像 Key 带随机后缀
（`avatars/{uid}/{时间戳}-{随机}_{尺寸}.png`），因此**不会误覆盖**，
旧对象由阶段 4 之后的清理任务按"失引用"回收。

**访问地址**：`storage.public_base_url` 决定前缀。本地开发指向应用自身的
`/api/v1/media` 路由（由 MediaHandler 直出），MinIO 部署时改为桶域名或 CDN，
业务代码与数据库都不变——数据库里永远只有 Key，没有 URL 前缀。

**权限与部署**：

| 环境 | 目录 | 权限 | 说明 |
|---|---|---|---|
| 开发 | `var/storage`（相对进程工作目录） | 0750 目录 / 0640 文件 | 已加入 `.gitignore` 与 `.dockerignore` |
| 容器 | `/app/var/storage` | 同上 | 必须挂载持久化卷，否则容器重建即丢图 |
| 多实例 | **不适用** | — | 多实例下各实例本地目录互相不可见，必须换 S3/共享卷 |

## 8. 缓存设计

### 8.1 Key 命名规范

**唯一前缀 `blog:`，冒号分隔，全小写，禁止中文与空格。** 第 6 章与本章的 Key 均以本表为准；若其他章节出现命名差异，以本表为准并回写修正。

| Key | 类型 | TTL | 用途 |
|---|---|---|---|
| `blog:article:detail:{id}` | string(JSON) | 5m | 文章详情 |
| `blog:article:slug:{slug}` | string | 5m | slug → id 映射 |
| `blog:article:list:{hash}` | string(JSON) | 1m | 列表页 |
| `blog:article:deleted:{id}` | string | 24h | 删除墓碑，防缓存回填 |
| `blog:user:profile:{uid}` | string(JSON) | 5m | 公开主页 |
| `blog:auth:user:{uid}` | hash | **10s** | 用户状态与角色（权限缓存，见 6.5.1） |
| `blog:auth:rt:{token_hash}` | hash | 7d | Refresh Token 主体 |
| `blog:auth:family:{family}` | set | 7d | family 下全部 token_hash |
| `blog:auth:user_families:{uid}` | set | 7d | 用户全部 family |
| `blog:auth:rt:hash2family:{token_hash}` | string | 7d | 重放检测反查 |
| `blog:auth:rt:meta:{hash}` | hash | 60s | 轮换元数据（时间/IP/UA 指纹），区分并发与重放（见 6.3.4） |
| `blog:auth:rt:handover:{hash}` | string | 10s | 交接 token（明文，默认不写入，见 6.3.3） |
| `blog:auth:email_verify:{hash}` | string | 24h | 邮箱验证令牌 |
| `blog:auth:pwd_reset:{hash}` | string | 15m | 密码重置令牌 |
| `blog:auth:pwd_reset_user:{uid}` | set | 15m | 用户全部重置令牌索引 |
| `blog:auth:fail:{uid}` | string | 15m | 登录失败计数 |
| `blog:rate:{scope}:{key}` | string | 动态 | 限流计数 |
| `blog:view:{article_id}` | string | 1h | 浏览量缓冲 |
| `blog:view:uv:{article_id}:{uid}` | string | 60s | 浏览去重 |
| `blog:unread:{uid}` | string | — | 总未读数，可重建 |
| `blog:ws:ticket:{ticket}` | string | 60s | WS 握手票据 |
| `blog:ws:online:{uid}` | set | 90s | 用户连接所在节点 |

**已移除的 Key**

| Key | 原因 |
|---|---|
| `blog:conv:seq:{conv_id}` | seq 改由 `conversations.seq_counter` 原子分配（11.6.2），不再依赖 Redis |
| `blog:csrf:*` | CSRF 采用双提交模式，服务端不存储 |

**命名修正**：v1.0 同时存在 `blog:user:auth:{id}` 与 `auth:user:{uid}` 两种写法，现统一为 `blog:auth:user:{uid}`。

### 8.2 失效策略

| 场景 | 动作 |
|---|---|
| 文章更新 | `DEL blog:article:detail:{id}`、`DEL blog:article:slug:{slug}`；`SCAN blog:article:list:*` 批量删除 |
| 文章删除 | 同上，并 `SET blog:article:deleted:{id} 1 EX 86400` 写墓碑，防并发回填 |
| 用户改资料 | `DEL blog:user:profile:{uid}` |
| 用户改角色 / 禁用 / 改密码 | `DEL blog:auth:user:{uid}`，失败必须重试并报错（见 6.5.1） |
| 评论增删 | `DEL blog:article:detail:{id}`（详情页含评论数） |

**内容缓存与权限缓存的失败处理不同，不能一律"TTL 兜底"**：

| 缓存类别 | 删除失败时 |
|---|---|
| 内容缓存（文章、列表、主页） | 记录 warn 日志，靠 TTL 兜底（最长 5 分钟） |
| 权限缓存（`blog:auth:user:*`） | 重试 3 次，仍失败返回 503，不允许静默降级 |

`SCAN` 而非 `KEYS`：`KEYS` 会阻塞 Redis，任何情况下禁止使用。

### 8.3 缓存穿透与雪崩

| 问题 | 措施 |
|---|---|
| 穿透（查不存在的数据） | 查到空结果时缓存空值，TTL 30 秒 |
| 击穿（热点 key 过期） | 单飞模式（singleflight），同一 key 只允许一个请求回源 |
| 雪崩（大量同时过期） | TTL 加随机抖动 ±20% |

### 8.4 前缀隔离与集群扩展

所有 key 统一以 `blog:` 开头，目的是**将来多个应用共用同一 Redis 实例时保持命名空间隔离**。

若迁移到 Redis Cluster，同一段 Lua 脚本操作的所有 key 必须落在同一 slot，届时需为 key 增加 hash tag（如 `blog:auth:rt:{uid}:{hash}`）。V1.0 为单机部署，不做此改造，但有一条硬性约束：

> **禁止在同一个 Lua 脚本中操作不同用途的 key 族**（例如同时操作 `blog:auth:*` 与 `blog:ws:*`）。否则将来迁移集群时无法通过加 tag 解决。

当前唯一使用 Lua 的地方是 Refresh Token 轮换（6.3.2），只操作 `blog:auth:rt:*` 与 `blog:auth:family:*`，后缀都是 token_hash 或 family，可加统一 tag 迁移。

---

## 9. 搜索索引设计

### 9.1 索引文档结构

```json
{
  "id": 1001,
  "title": "Go 并发模型详解",
  "summary": "从 GMP 模型讲起...",
  "content": "正文纯文本，去除 Markdown 标记",
  "author_id": 12,
  "author_name": "张三",
  "category_id": 3,
  "category_name": "后端",
  "tag_ids": [5, 8],
  "tag_names": ["Go", "并发"],
  "published_at": 1729490000
}
```

可搜索字段：`title`（权重最高）、`summary`、`content`、`tag_names`。
可过滤字段：`author_id`、`category_id`、`tag_ids`、`published_at`。
**可排序字段：仅 `published_at`。**

**V1.0 不保留 `view_count` 排序（相对 v1.0 的简化）**

原因是一致性，不是性能：

- `articles.view_count` 由 Redis 缓冲，最多滞后 60 秒回写
- 索引文档里的 `view_count` 是同步那一刻的快照，可能更旧
- 两者天然不同步，会出现"详情页显示 3200 次阅读，搜索结果却按 2800 次排序"的可见矛盾

博客场景"最新优先"已足够。热门排序放在首页，走数据库直查（见 7.4.4），不经过搜索引擎。

因此索引文档中**不写入** `view_count` 字段。

中文分词依赖 Meilisearch 内建的 charabia，不额外配置词典。

### 9.2 同步流程

```
文章状态变更（发布/编辑/下架/删除）
  ↓ 同一事务
UPDATE articles SET search_sync_status = 0
  ↓ 事务提交后
asynq.Enqueue("article:index", {article_id})
  ↓ 队列投递失败
仅记录错误日志，不回滚（PRD 7.4）
  ↓ worker 消费
从 DB 重新读取文章（不信任 payload 中的内容）
  ↓
写入 / 删除 Meilisearch 文档
  ↓ 成功
UPDATE articles SET search_sync_status = 1, search_synced_at = NOW()
  ↓ 失败
重试 5 次（退避 1s/5s/30s/2m/10m）
  ↓ 仍失败
UPDATE articles SET search_sync_status = 2，写入错误日志
```

**worker 必须从数据库重新读取内容**，不能相信队列 payload。payload 只放 `article_id`，避免"队列积压期间文章已被修改，索引写成旧版本"。

### 9.3 对账任务

| 任务 | 频率 | 逻辑 |
|---|---|---|
| 增量补偿 | 每 10 分钟 | `SELECT id FROM articles WHERE search_sync_status != 1 OR (search_synced_at IS NOT NULL AND search_synced_at < updated_at) LIMIT 200`，逐条重新入队 |
| 抽样校验 | 每日 03:00 | 随机取 100 篇 `search_sync_status=1` 的文章，比对索引文档是否存在、title 是否一致，不一致则重置为 0 |
| 全量重建 | 手动触发 | 分页读取全部已发布文章，批量重建索引 |

### 9.4 搜索结果的二次校验

**问题**：索引同步是异步的。当删除或下架文章时，只要出现"重试耗尽 + 对账尚未修复"的窗口，该文章就可能长期残留在索引里，被用户搜到并点开，得到 404/410。

仅靠同步机制的最终一致性，无法保证"用户不会看到不该看到的内容"。

**方案：搜索结果回表过滤**

```
1. Meilisearch 返回 top N（N = limit × 2，冗余取）
2. 提取全部 id
3. SELECT id FROM articles
    WHERE id IN (...)
      AND status = 2            -- published
      AND deleted_at IS NULL
4. 用第 3 步结果过滤，截取 limit 条
5. 过滤后不足 limit 条时，扩大一次 N 重试
```

成本：一次主键 IN 查询，走聚簇索引，几十个 id 的开销可忽略。

**为什么必须做**：这是唯一能确定性保证"公开搜索结果不含已删除/已下架文章"的手段。索引同步无论重试多完善都存在时间窗口，回表校验是同步的。

**代价**：索引与数据库严重不一致时，搜索结果可能不足一页。这是可接受的降级——宁可少给结果，不可给出打不开的链接。

### 9.5 已删除文章的索引处理链路

| 环节 | 动作 |
|---|---|
| 删除事务内 | `search_sync_status = 0` |
| 事务提交后 | `enqueue article:unindex` |
| unindex worker | 删除索引文档，文档不存在视为成功 |
| 失败重试 | 5 次；仍失败则 `search_sync_status = 2` |
| 兜底一 | 9.4 回表过滤，用户搜不到 |
| 兜底二 | 9.3 增量对账重新投递 |
| 极端情况 | 管理端"重建全部索引" |

即使 unindex 长期失败，用户也不会搜到该文章，索引中只是多了一条无用文档，不影响正确性。

### 9.6 搜索分页与结果上限

**核实结论（Meilisearch 1.6）**

| 项 | 事实 |
|---|---|
| 分页方式 | 仅 `offset/limit`（返回 `estimatedTotalHits`）与 `page/hitsPerPage`（返回 `totalHits`） |
| 原生游标 | **不支持**，没有 `search_after` 类参数 |
| 结果上限 | `pagination.maxTotalHits`，默认 **1000** |
| 调高上限的代价 | 排序管线成本线性上升，官方警告超过 20000 可能耗时数秒 |

**V1.0 方案：offset/limit，上限保持默认 1000**

```
offset = (page - 1) × page_size
limit  = page_size
```

不改 `maxTotalHits`。博客量级下单个关键词最多出 1000 条已足够；超过说明关键词太宽泛，应引导用户细化而不是继续翻页。

**为什么不在应用层伪造游标**

即使自建 `(published_at, id)` 游标，Meilisearch 仍只接受 offset。把游标换算成 offset 后，一旦 `offset + limit` 超过 1000 就报错——**应用层游标无法突破引擎的结果访问限制**，包装得再像也没用。

**与回表过滤的配合**

过滤发生在拿到命中 ID 之后，因此某一页可能不足 `page_size` 条。这是设计内行为，不补齐、不重试：

- 补齐需要额外发起查询，且会打乱 offset 推进
- 少给几条不影响正确性，给打不开的链接才是事故

**与全站分页约定的差异**

`GET /search` 是全站唯一的偏移分页接口，其余列表一律游标。这个差异在 API_SPEC 1.6 中已显式标注，前端不得用统一的 `cursor` 参数调用搜索接口。

---

### 9.7 实现落地说明（v1.9）

第 9 章设计已在阶段 4B 落地，与设计的差异及实现要点：

| 项 | 实现 |
|---|---|
| 客户端 | `meilisearch-go v0.36.3`，`internal/search` 封装为 `Searcher` 接口（EnsureIndex/Upsert/Delete/Query/Fetch/Healthy） |
| 未启用时 | `search.Unavailable{}` 占位：写入静默成功、查询返回 `ErrUnavailable`，由搜索服务降级为空结果 |
| 索引初始化 | 进程启动时 `EnsureIndex`（幂等：已存在跳过创建，设置重复下发）；不初始化则"写入报错、查询默默为空"，最难受的故障形态 |
| 异步等待 | 所有写操作 `WaitForTask`（上限 15s）；不等就返回成功会让"刚发布搜不到"成为常态 |
| 队列 | `asynq v0.26.0`（复用项目 Redis 客户端），队列 `critical`（索引）/`default`（对账），重试退避 1s/5s/30s/2m/10m |
| 载荷 | 只含 `article_id`，正文一律由 worker 从数据库重读 |
| 对账 | 增量 `@every 10m`（扫 `search_sync_status≠1` 或 `synced_at<updated_at`，每轮 ≤200）；抽样每日 03:00（100 篇比对标题） |
| 主键过滤 | 抽样对账用 `id IN [...]` 过滤器取回索引文档；结果回表过滤由 `PublishedBriefs` 实现 |

**一个真实踩坑**：master key 在 compose 与应用侧各写了一个"默认值"，两者不一致只会以运行期 403 暴露。
已改为 `${MEILI_MASTER_KEY:?}` 强制必填——密钥不允许任何编造的默认值。

## 10. 异步任务设计

### 10.1 设计原则

V1.0 不引入 Outbox 表，但**不接受"记录日志后永久丢弃任务"**。做法是一条统一规则：

> **每一个异步任务，都必须能从数据库状态重新推导出来。任务本身只是加速器，不是唯一触发路径。**

这条规则替代了 Outbox 的作用。凡是无法从数据库状态推导的任务，必须在 10.5 中显式说明为什么不需要补偿。

### 10.2 任务清单与恢复依据

| 任务名 | Payload | 队列 | 重试 | **可重新发现的依据** |
|---|---|---|---|---|
| `article:index` | `{article_id}` | critical | 5 | `articles.search_sync_status = 0` |
| `article:unindex` | `{article_id}` | critical | 5 | 同上（删除时同样置 0） |
| `image:process` | `{media_id}` | low | 3 | `media.variants IS NULL AND status = 1` |
| `notification:create` | `{user_id, type, ref_id}` | default | 3 | 存在 approved 评论但无对应通知记录 |
| `mail:send` | `{template, to, vars}` | low | 5 | **无后台依据**，靠用户主动重发 |
| `counter:flush_view` | `{}` | default | 1 | 定时任务，下一轮自然重跑 |

### 10.3 投递失败的三个断点

任务从"业务变更"到"被执行"，中间有三个可能断掉的环节：

| 断点 | 现象 | 恢复方式 |
|---|---|---|
| ① 事务提交后 Enqueue 失败 | Redis 不可用，任务没进队列 | 数据库状态已落库（如 `search_sync_status=0`），对账任务重新发现 |
| ② 任务在队列中丢失 | Redis 重启且未持久化 | 同上 |
| ③ 任务执行失败 | 重试耗尽 | 状态置为失败标记，对账重新投递 + 人工介入 |

**为什么三个断点都能覆盖**：状态标记与业务变更在**同一事务**内写入，等价于把"待办事项"存进了业务表本身。Outbox 表解决的"事务与投递原子性"问题，在这里被"业务状态字段"承担了。

**为什么不直接加 Outbox**：多一张表、一个轮询 worker、一套消费幂等，而 V1.0 的下游只有一个（索引/通知/媒体），状态字段已经足够表达。等出现"多个下游消费同一事件"时再引入。

### 10.4 对账任务

| 任务 | 频率 | 扫描条件 | 动作 |
|---|---|---|---|
| `reconcile:index_incremental` | 每 10 分钟 | `search_sync_status != 1 OR search_synced_at < updated_at` | 重新入队，单批 200 条 |
| `reconcile:index_sample` | 每日 03:00 | 抽样 100 篇已同步文章 | 比对索引，不一致重置为 0 |
| `reconcile:media` | 每 10 分钟 | `variants IS NULL AND status=1 AND created_at > NOW()-INTERVAL 1 DAY` | 重新入队图片处理 |
| `reconcile:notification` | 每小时 | 近 24 小时 approved 的评论 | 检查通知是否存在，缺失则补建 |
| `counter:flush_view` | 每 60 秒 | — | 回写 Redis 浏览量 |

### 10.5 各类任务的补偿边界

| 任务 | 后台补偿 | 理由 |
|---|---|---|
| 索引同步 | ✅ 需要 | 用户不可见，无法自助修复 |
| 图片处理 | ✅ 需要 | 用户看到的是破图，同样无法自助 |
| 站内通知 | ✅ 需要（24 小时窗口） | 用户不知道通知丢了 |
| 邮件 | ❌ 不需要 | 验证邮件与重置邮件都有"重发"入口 |
| 聊天推送 | ❌ 不需要 | 消息已落库，客户端重连按 seq 补齐 |
| 浏览量回写 | ❌ 不需要 | 展示数据，丢失不影响正确性 |

**邮件不补偿的两个前提**：`mail:send` 失败必须写 error 日志；"重发"接口本身可用。若将来出现用户无法自助重试的邮件类型（如批量通知），需要重新评估。

**聊天推送不补偿的前提**：前端必须实现重连补齐。若前端不做补齐，推送丢失等于消息丢失——这是前端必须遵守的契约，不是可选项。

### 10.6 人工介入

管理端必须提供以下入口，作为自动补偿失效后的最后手段：

| 入口 | 作用 |
|---|---|
| 索引异常列表 | 展示 `search_sync_status = 2` 的文章，支持单篇重试 |
| 重建全部索引 | 索引服务故障恢复后使用 |
| 重算分类/标签计数 | 计数漂移时修复（见 7.4.2） |
| 媒体处理失败列表 | `variants IS NULL` 且超过 1 天的记录 |

### 10.7 幂等要求

asynq 在 worker 崩溃时会重复投递，所有任务必须幂等。

| 任务 | 幂等实现 |
|---|---|
| `article:index` | 全量覆盖写入 |
| `article:unindex` | 删除不存在的文档视为成功 |
| `image:process` | 检查 `variants` 已生成则跳过 |
| `notification:create` | `uk_user_type_ref` 唯一索引 + `INSERT IGNORE` |
| `mail:send` | 无法幂等。用 `blog:mail:dedupe:{key}` TTL 5 分钟去重，接受极小概率重复发送 |
| `counter:flush_view` | `GETDEL` 取走计数，避免重复回写 |

### 10.8 队列划分

| 队列 | 并发 | 用途 |
|---|---|---|
| `critical` | 10 | 索引同步 |
| `default` | 10 | 通知、计数回写、对账 |
| `low` | 3 | 图片处理、邮件 |

索引同步单独队列并提高优先级，避免被图片处理这类 CPU 密集任务阻塞。

### 10.9 数据清理任务

对应 PRD 6.7 的保留策略。**具体天数待决策（R1–R6），本节只定义机制，不写死期限。**

**任务清单**

| 任务名 | 队列 | 频率 | 单批上限 | 清理对象 |
|---|---|---|---|---|
| `cleanup:article` | low | 每日 04:00 | 500 | 软删超过保留期的文章 |
| `cleanup:comment` | low | 每日 04:10 | 500 | 软删超过保留期的评论 |
| `cleanup:message` | low | 每日 04:20 | 500 | 已删除超过保留期的消息 |
| `cleanup:notification` | low | 每日 04:30 | 1000 | 超过保留期的通知 |
| `cleanup:media` | low | 每日 04:40 | 200 | 无引用且超过保留期的媒体 |
| `cleanup:account` | critical | 每日 05:00 | 100 | 冷静期到期的注销账号 |

**五条共同规则**

1. **分批**：每批最多 N 条，处理完再投递下一批。禁止一次性 `DELETE WHERE deleted_at < ...`，那会长时间锁表
2. **幂等**：以时间戳条件删除，重复执行无副作用，中断后可从数据状态自然恢复断点
3. **顺序**：先删数据库记录，再清 MinIO 对象与搜索索引。反了会留下孤儿文件与幽灵文档
4. **有依赖则跳过**：删除前检查引用，有引用就跳过并记录，不级联误删
5. **不做物理删除的表不参与**：`operation_logs` 的清理需要单独决策，不与业务数据混在一起

**依赖关系**

```
cleanup:media  → 依赖 articles / comments / messages 清理完成
                 只有确认无引用后才删 MinIO 对象
cleanup:account → 依赖举报处理完毕
                 有未处理举报的账号跳过，下轮重试
cleanup:article → 清理后需同步移除搜索索引
cleanup:comment → 清理后需重算 articles.comment_count
```

**失败与监控**

| 项 | 处理 |
|---|---|
| 单批失败 | 记录日志，指数退避重试 3 次 |
| 连续失败 | 写告警日志，管理端"清理异常"可见 |
| 监控指标 | `cleanup_deleted_total{type}`、`cleanup_skipped_total{reason}`、`cleanup_duration_seconds` |
| 异常告警 | **单次删除量超过历史均值 3 倍时立即告警** |

最后一条是硬性要求。清理任务最大的风险不是删不掉，而是**误配置导致一次删太多**。宁可漏删，不可多删。

**用户注销执行流程**

```
cleanup:account 每日扫描：
  1. users.status = 待注销 且 冷静期已过
  2. 检查是否存在未处理举报 → 有则跳过
  3. 事务内：
       匿名化 email / password_hash / nickname / avatar_url / bio
       删除该用户 article_favorites、article_likes、notifications
       写 operation_logs
  4. 事务外：删除头像对象
```

**只处理 `users` 表关联的个人数据，不动 `articles` 与 `comments`** —— 这两类要保留（PRD 6.7.2）。

**与备份的关系**

清理只影响在线库。备份按 PRD 6.2 保留 7 天，意味着已清理数据在备份中最多再存活 7 天。若将来保留期要求长于备份周期，需要单独的归档机制，**不在 V1.0 范围**。

---

## 11. WebSocket 与消息通道

### 11.1 连接建立

浏览器无法在 WebSocket 握手中携带自定义 Header，因此**不使用 URL 直接传 Token**（会进入 Nginx 日志与浏览器历史）。

流程：

```
1. 前端调用 POST /api/v1/ws/ticket（需登录）
   → 服务端生成随机 ticket，SET blog:ws:ticket:{ticket} = uid，TTL 60s
   → 返回 {ticket}

2. 前端建立连接 ws://host/ws?ticket=xxx

3. 服务端：
   → GETDEL blog:ws:ticket:{ticket}，取 uid
   → 无效或过期 → 关闭连接（close code 4001）
   → 校验用户状态，非 active → 关闭（4003）
   → 注册到 Hub
```

ticket 一次性使用，60 秒有效，用完即删。

### 11.2 消息协议

统一 JSON 帧：

```json
{
  "type": "message.new",
  "data": { },
  "ts": 1729490000123
}
```

下行类型：

| type | data | 说明 |
|---|---|---|
| `message.new` | `{conversation_id, message}` | 新消息 |
| `message.read` | `{conversation_id, user_id, last_read_seq}` | 对方已读 |
| `message.recalled` | `{conversation_id, message_id}` | 撤回（V1.1） |
| `conversation.update` | `{conversation}` | 会话摘要变化 |
| `notification.new` | `{notification}` | 站内通知 |
| `system.kick` | `{reason}` | 被踢下线（禁用、异地登录） |
| `pong` | `{}` | 心跳响应 |

**V1.0 不设计上行消息类型**。发消息、标记已读全部走 HTTP，WebSocket 只做下行推送（PRD 7.2 第 3 条）。

### 11.3 Hub 与多端

```go
type Hub struct {
    mu    sync.RWMutex
    conns map[int64]map[*Client]struct{}  // uid → 该用户的全部连接
}
```

一个用户可以有多个连接（手机 + 电脑），推送时遍历全部。连接关闭时从对应集合移除，集合空则删除该 uid 的 key。

### 11.4 跨节点推送

单机部署时 Hub 直接推送即可。为将来多实例预留：所有推送先 publish 到 Redis 频道 `blog:ws:push`，各节点订阅后投递给本地连接。

单机情况下这条链路仍然成立（自己发自己收），开销是一次 Redis 往返。选择保留是因为它让"多实例"变成纯部署问题，不需要改代码。

### 11.5 心跳

- 服务端每 30 秒发送 Ping（WebSocket 协议层）
- 客户端 60 秒内无任何帧则判定断线
- 服务端读循环**不设超时**（v1.1 修正，2026-10-22）：V1.0 无上行消息，Pong 又被库内部消费，
  纯接收的连接永远等不到数据帧，按数据帧超时等于每 90 秒强制断线一次。
  死连接的清理由 Ping 写失败驱动（写超时 10 秒 + 30 秒间隔，最坏约 40 秒识别并关闭）
- 每次收到 Pong 刷新 `blog:ws:online:{uid}`（Set 类型，成员为节点 ID，TTL 90 秒）

### 11.6 消息序号与断线补齐

#### 11.6.1 为什么需要会话内序号

消息表 `id` 是全局自增，多实例并发写入时事务提交顺序与 id 顺序可能不一致，客户端会看到乱序。因此需要会话内严格单调的 `seq`。

#### 11.6.2 分配方案：数据库原子自增

**v1.0 的 Redis INCR 方案存在并发缺陷。** 原逻辑是：`INCR` 返回 1 时说明 key 刚创建，此时再去查 DB `MaxSeq` 并 `IncrBy`。这两步之间不是原子的——并发请求已经拿到 2、3，随后被 `IncrBy(maxSeq)` 整体推高，导致序号重复或大幅跳变。加锁或 Lua 脚本能缓解，但仍多一个故障点（Redis 重启、key 提前过期、时钟无关的基数漂移）。

**V1.0 改为在 `conversations.seq_counter` 上用单条 SQL 原子分配，不依赖 Redis。**

```sql
BEGIN;
  -- 单语句原子自增，并把新值挂到连接级 last_insert_id
  UPDATE conversations
     SET seq_counter = LAST_INSERT_ID(seq_counter + 1)
   WHERE id = ?;

  SELECT LAST_INSERT_ID();   -- 取回新 seq

  INSERT INTO messages (conversation_id, sender_id, seq, type, content, client_msg_id)
  VALUES (?, ?, ?, ?, ?, ?);

  UPDATE conversations
     SET last_message_id = ?, last_message_at = NOW(),
         last_message_preview = ?, message_count = message_count + 1
   WHERE id = ?;
COMMIT;
```

`LAST_INSERT_ID(expr)` 是 MySQL 内置能力：执行后把 `expr` 记录为当前连接的 `last_insert_id`，随后 `SELECT LAST_INSERT_ID()` 取回。整个过程在一条 UPDATE 的行锁内完成，应用层不需要分布式锁。

#### 11.6.3 边界情况

| 场景 | 结果 | 原因 |
|---|---|---|
| 会话首次发消息 | `seq = 1` | `seq_counter` 默认 0 |
| 同一会话并发发送 | 严格递增，无重复、无跳号 | UPDATE 行锁串行化 |
| Redis 重启或数据清空 | 完全不受影响 | 不读取 Redis |
| Redis key 过期 | 完全不受影响 | 无此 key |
| 消息事务失败 | `seq_counter` 随之回滚 | 同一事务，不产生空洞 |
| 进程在事务中途崩溃 | 事务整体回滚 | InnoDB 保证 |
| 客户端重复提交 | `client_msg_id` 唯一索引拦截 | 见 11.7 |

**性能说明**：`seq_counter` 的行锁只影响同一会话的并发写，不同会话互不阻塞。单聊单会话的写入频率以秒计，远未触及瓶颈。若将来群聊出现单会话高频写入，可改为分段预取（每 N 个号取一段），属实现阶段优化，不影响本设计的正确性。

#### 11.6.4 断线补齐

```
GET /api/v1/conversations?with_unread=1
  → 返回每个会话的 last_message_id 与 unread_count

GET /api/v1/conversations/{id}/messages?after_seq={本地最大seq}&limit=50
  → 按 seq 升序返回
```

`uk_conv_seq` 唯一索引保留为最后一道防线：任何原因导致的 seq 重复都会被数据库拒绝，service 捕获后重试一次。

### 11.7 发送流程

```
POST /api/v1/conversations/{id}/messages
  → 校验成员身份
  → 校验对方状态（disabled → 403）
  → nextSeq
  → 事务：INSERT messages + UPDATE conversations.last_message_*
  → 事务提交后：
      UPDATE conversation_members SET unread_count = unread_count + 1 WHERE conversation_id=? AND user_id=对方
      PUBLISH blog:ws:push {type:"message.new", target_uids:[对方], data:...}
  → 返回 {message_id, seq, created_at}
```

幂等：`client_msg_id` 撞唯一索引时，查出已存在的记录直接返回，不报错。

### 11.8 已读状态

**存储位置**：`conversation_members.last_read_seq`，不单独建已读表。理由见 DATABASE.md 4.15.1——联合主键 `(conversation_id, user_id)` 本身就是"某人在某会话的已读位置"，另建一张表会产生两个真相来源。

**标记已读**

```sql
UPDATE conversation_members
   SET last_read_seq = LEAST(?, (SELECT seq_counter FROM conversations WHERE id = ?)),
       unread_count  = 0
 WHERE conversation_id = ? AND user_id = ? AND last_read_seq < ?;
```

| 项 | 处理 |
|---|---|
| 单调递增 | `WHERE last_read_seq < ?` 同时挡住回退与重复；`affectedRows = 0` 视为成功，不报错 |
| 并发 | 单条原子 UPDATE，不需要事务与锁。乱序到达的旧请求被 WHERE 条件天然过滤 |
| 越界保护 | `LEAST(?, seq_counter)` 防止客户端传入未来序号 |
| 权限 | `user_id` 取自当前登录态，**不接受请求体传入**，杜绝改他人已读位 |
| 成员校验 | 更新前确认调用者在该会话中，否则 50002 |

**未读数维护**

| 时机 | 动作 |
|---|---|
| 发送消息 | 接收方 `unread_count = unread_count + 1` |
| 标记已读 | 归零 |
| 一致性 | 允许 `unread_count` 与 `seq_counter - last_read_seq` 短暂不等。对账仅在偏差超阈值时告警，**不做自动修正**（自动改会覆盖并发写入） |

**`is_read` 的对外表达**

服务端不返回逐条 `is_read`，只返回 `peer_last_read_seq`，前端按 `message.seq <= peer_last_read_seq` 比较。

理由：逐条下发会让响应体积翻倍，且该状态随对方操作实时变化，客户端缓存必然过期。

**推送时序**：先更新数据库 → 再推送 `message.read`。顺序反了会出现"对方看到已读、刷新后又变回未读"。

---

## 12. 关键业务流程

### 12.1 文章状态机实现

在 `service/article_state.go` 中集中定义，禁止分散在各 handler：

```go
type transition struct {
    From   Status
    Action Action
    To     Status
    Roles  []authz.Role
}

var transitions = []transition{
    {StatusDraft,    ActionSubmit,     StatusPublished, []authz.Role{authz.Author, authz.Admin}},
    {StatusDraft,    ActionSubmitReview, StatusPending, []authz.Role{authz.Author}},
    {StatusDraft,    ActionDelete,     StatusDeleted,   []authz.Role{authz.Author, authz.Admin}},
    {StatusPending,  ActionApprove,    StatusPublished, []authz.Role{authz.Admin}},
    {StatusPending,  ActionReject,     StatusRejected,  []authz.Role{authz.Admin}},
    {StatusPending,  ActionWithdraw,   StatusDraft,     []authz.Role{authz.Author}},
    {StatusRejected, ActionResubmit,   StatusPending,   []authz.Role{authz.Author}},
    {StatusPublished, ActionOffline,   StatusOffline,   []authz.Role{authz.Admin}},
    {StatusPublished, ActionArchive,   StatusArchived,  []authz.Role{authz.Author}},
    {StatusPublished, ActionDelete,    StatusDeleted,   []authz.Role{authz.Author, authz.Admin}},
    {StatusOffline,  ActionReedit,     StatusDraft,     []authz.Role{authz.Author}},
    {StatusOffline,  ActionRelist,     StatusPublished, []authz.Role{authz.Admin}},
    {StatusArchived, ActionRestore,    StatusDraft,     []authz.Role{authz.Author}},
}

// ActionDelete 对所有非 deleted 状态均允许（PRD 3.2「任意状态 → 软删除」），
// 在 CanTransition 中先做短路判断，不逐条枚举，避免状态增加时漏加转换
```

`CanTransition(from, action, role)` 遍历该表，找不到即返回 `30002 状态不允许此操作`。

`ActionSubmit` 与 `ActionSubmitReview` 的选择由 `settings.article_review` 决定：审核关闭走前者直接发布，开启走后者进入 pending。

**新增状态时必须同时在此表补充转换，CI 中的状态机测试会校验"所有状态都有出口"**。

### 12.2 文章发布

```
POST /api/v1/articles/{id}/publish
  1. 加载文章，校验 author_id == 当前用户
  2. 校验当前状态可执行 Submit
  3. 校验必填：标题非空、正文非空、分类有效
  4. 决定目标状态（审核开关）
  5. 事务：
       UPDATE articles SET status=?, published_at=IFNULL(published_at,NOW()),
                           version=version+1, search_sync_status=0
       WHERE id=? AND version=?
       （version 不匹配 → 30004 编辑冲突）
       更新 tags 关联、分类计数
  6. 事务提交后：
       asynq.Enqueue("article:index", {id})
       DEL 缓存
```

### 12.3 文章删除

```
DELETE /api/v1/articles/{id}
  1. 校验权限（作者本人或管理员）
  2. 事务：
       UPDATE articles SET status=6, deleted_at=NOW(), search_sync_status=0 WHERE id=?
       若管理员操作，写 operation_logs（含删除人、原因）
  3. 事务提交后：
       asynq.Enqueue("article:unindex", {id})
       DEL article:detail:{id}、article:slug:{slug}
       写入墓碑 article:deleted:{id}（TTL 24h），防止缓存回填
  4. 此后公开 URL 返回 410（PRD 2.2.1）
```

### 12.4 点赞

```
POST /api/v1/articles/{id}/like
  1. 校验文章 published 且未删除
  2. INSERT IGNORE INTO article_likes
  3. affectedRows == 1 → UPDATE articles SET like_count = like_count + 1
  4. affectedRows == 0 → 已点过，直接返回成功（幂等）
  5. DEL 文章详情缓存

DELETE 同理，用 DELETE 的 affectedRows 判断是否递减
```

### 12.5 评论与通知

```
POST /api/v1/articles/{id}/comments
  1. 校验文章 published 且 allow_comment=1
  2. 校验 parent_id：若存在，其文章必须是同一篇，且 parent 本身是 approved
  3. 决定初始状态（审核开关）
  4. 事务：INSERT comments；若 approved 则 UPDATE articles.comment_count+1
  5. 事务提交后：
       若 approved 且 parent_id != 0 → 通知被回复者
       若 approved 且 parent_id == 0 → 通知文章作者（非本人时）
       asynq.Enqueue("notification:create")
```

### 12.6 图片上传

```
POST /api/v1/uploads/images (multipart)
  1. 校验大小（文章图 10MB / 聊天图 5MB）
  2. 读取前 512 字节校验魔数，不信任 Content-Type
  3. 用 image.DecodeConfig 确认真实尺寸，拒绝解码失败的
  4. 计算 sha256，查 media 表命中则直接复用（秒传）
  5. 原始文件写入 MinIO
  6. INSERT media (purpose 决定 bucket)
  7. asynq.Enqueue("image:process", {media_id})
  8. 返回 media_id 与临时 URL

image:process worker：
  → 生成 w128 / w256 / w800 三种尺寸
  → 非 gif 转 webp
  → 写回 media.variants
```

聊天图片的访问：Bucket 为私有，接口返回 `GET /api/v1/media/{id}/url` 时校验调用者是否为对应会话成员，通过后生成 1 小时有效的预签名 URL。

---

## 13. API 清单

统一前缀 `/api/v1`。除标注"公开"外均需登录。

> **完整接口契约见 `API_SPEC.md`**（共 81 个接口，按 `method + path` 计）。本章只列清单与权限，请求体、响应体、错误码、业务规则以该文档为准。

### 13.1 认证

| 方法 | 路径 | 说明 | 权限 |
|---|---|---|---|
| POST | `/auth/register` | 注册 | 公开 |
| GET | `/auth/verify-email?token=` | 邮箱验证 | 公开 |
| POST | `/auth/resend-verify` | 重发验证邮件 | 公开 |
| POST | `/auth/login` | 登录 | 公开 |
| POST | `/auth/refresh` | 刷新 Token | Cookie |
| POST | `/auth/logout` | 退出 | 登录 |
| POST | `/auth/forgot-password` | 申请重置 | 公开 |
| POST | `/auth/reset-password` | 重置密码 | 公开 |

### 13.2 用户

| 方法 | 路径 | 说明 | 权限 |
|---|---|---|---|
| GET | `/users/me` | 当前用户信息 | 登录 |
| PATCH | `/users/me` | 更新昵称、简介 | 登录 |
| PUT | `/users/me/password` | 修改密码 | 登录 |
| PUT | `/users/me/avatar` | 更新头像 | 登录 |
| GET | `/users/{id}` | 公开主页 | 公开 |
| GET | `/users/{id}/articles` | 某人的已发布文章 | 公开 |
| GET | `/users/me/articles` | 我的文章（含草稿、待审、已拒等全部状态） | 作者 |

### 13.3 文章

| 方法 | 路径 | 说明 | 权限 |
|---|---|---|---|
| GET | `/articles` | 公开列表（分页、分类、标签筛选，`sort=latest\|hot`） | 公开 |
| GET | `/articles/{slug}` | 详情（按 slug，返回渲染后 HTML） | 公开 |
| **GET** | **`/articles/{id}/edit`** | **取正文原文供编辑（返回 `content_md`、`version`、`status`）** | 作者本人 |
| POST | `/articles` | 创建草稿 | author |
| PUT | `/articles/{id}` | 更新（含 version 乐观锁） | 作者本人 |
| DELETE | `/articles/{id}` | 删除 | 作者本人 / admin |
| POST | `/articles/{id}/submit` | 提交（依开关决定发布或送审） | 作者本人 |
| POST | `/articles/{id}/withdraw` | 撤回审核 | 作者本人 |
| POST | `/articles/{id}/archive` | 归档 | 作者本人 |
| POST | `/articles/{id}/restore` | 恢复为草稿 | 作者本人 |

> **路径参数约定**：公开读接口用 `{slug}`，写操作与后台操作统一用 `{id}`。slug 面向用户与 SEO，id 便于前端与后台精确定位。两者不混用，否则同一资源会出现两套权限判断与两套缓存键。

> **为什么需要 `/articles/{id}/edit`**：公开详情接口返回渲染后的 `content_html`，不返回 `content_md`，作者编辑时无法回填编辑器。编辑接口按 id 取原文，同时返回 `version` 用于乐观锁。该接口只对作者本人开放，管理员也不可调用（见 6.6）。

> **为什么"我的文章"不用 `/articles/mine`**：`mine` 是静态路径段，会与 `/articles/{slug}` 形成保留字冲突——若某篇文章的 slug 恰好是 `mine`，该文章将永远无法通过详情页访问。因此用户维度的资源统一挂在 `/users/me/*` 下，与 `/users/{id}/articles` 形成对称。

### 13.4 分类、标签、搜索

| 方法 | 路径 | 说明 | 权限 |
|---|---|---|---|
| GET | `/categories` | 分类列表 | 公开 |
| GET | `/tags` | 标签列表 | 公开 |
| GET | `/search?q=&type=&page=` | 全文检索 | 公开 |

### 13.5 互动

| 方法 | 路径 | 说明 | 权限 |
|---|---|---|---|
| POST | `/articles/{id}/like` | 点赞 | 登录 |
| DELETE | `/articles/{id}/like` | 取消点赞 | 登录 |
| POST | `/articles/{id}/favorite` | 收藏 | 登录 |
| DELETE | `/articles/{id}/favorite` | 取消收藏 | 登录 |
| GET | `/users/me/favorites` | 我的收藏 | 登录 |
| GET | `/articles/{id}/comments` | 评论列表（两级） | 公开 |
| POST | `/articles/{id}/comments` | 发表评论 | 登录 |
| DELETE | `/comments/{id}` | 删除评论 | 本人 / admin |
| POST | `/reports` | 举报 | 登录 |

### 13.6 通知

| 方法 | 路径 | 说明 | 权限 |
|---|---|---|---|
| GET | `/notifications` | 通知列表 | 登录 |
| GET | `/notifications/unread-count` | 未读数 | 登录 |
| PUT | `/notifications/{id}/read` | 标记已读 | 登录 |
| PUT | `/notifications/read-all` | 全部已读 | 登录 |

### 13.7 媒体

| 方法 | 路径 | 说明 | 权限 |
|---|---|---|---|
| POST | `/uploads/images` | 上传图片 | 登录 |
| GET | `/media/{id}/url` | 获取访问地址（私有桶校验成员身份） | 登录 |

### 13.8 聊天

| 方法 | 路径 | 说明 | 权限 |
|---|---|---|---|
| GET | `/conversations` | 会话列表（含未读数） | 登录 |
| POST | `/conversations` | 发起单聊 `{peer_id}` | 登录 |
| GET | `/conversations/{id}/messages` | 历史消息（`before_id` 或 `after_seq`） | 成员 |
| POST | `/conversations/{id}/messages` | 发送消息 | 成员 |
| PUT | `/conversations/{id}/read` | 标记已读 | 成员 |
| PUT | `/conversations/{id}/mute` | 免打扰 | 成员 |
| POST | `/ws/ticket` | 获取 WS 握手票据 | 登录 |
| GET | `/ws?ticket=` | WebSocket 连接 | 票据 |

### 13.9 管理端

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/admin/overview` | 关键数字 |
| GET | `/admin/users` | 用户列表 |
| PUT | `/admin/users/{id}/status` | 禁用 / 启用 |
| PUT | `/admin/users/{id}/role` | 调整角色 |
| GET | `/admin/articles` | 全站文章列表 |
| POST | `/admin/articles/{id}/approve` | 审核通过 |
| POST | `/admin/articles/{id}/reject` | 审核拒绝（原因必填） |
| POST | `/admin/articles/{id}/offline` | 下架（原因必填） |
| POST | `/admin/articles/{id}/relist` | 重新上架 |
| DELETE | `/admin/articles/{id}` | 删除 |
| GET | `/admin/comments` | 评论列表 |
| POST | `/admin/comments/{id}/approve` | 评论通过 |
| POST | `/admin/comments/{id}/reject` | 评论拒绝 |
| DELETE | `/admin/comments/{id}` | 删除评论 |
| GET | `/admin/reports` | 举报队列 |
| PUT | `/admin/reports/{id}` | 处理举报 |
| CRUD | `/admin/categories` | 分类管理 |
| CRUD | `/admin/tags` | 标签管理 |
| GET | `/admin/index-failures` | 索引同步失败列表 |
| POST | `/admin/index-rebuild` | 重建索引 |
| GET | `/admin/settings` | 系统设置 |
| PUT | `/admin/settings` | 更新设置 |
| GET | `/admin/logs` | 操作日志 |

---

## 14. 可观测性

### 14.1 日志

JSON 结构化，字段固定：

```json
{
  "ts": "2026-10-21T08:20:11.123+08:00",
  "level": "info",
  "msg": "http request",
  "request_id": "01HQZ8X2K9J4M",
  "uid": 1001,
  "method": "POST",
  "path": "/api/v1/articles",
  "status": 201,
  "latency_ms": 42,
  "ip": "1.2.3.4"
}
```

| 级别 | 用途 |
|---|---|
| debug | 开发期细节，生产关闭 |
| info | 请求、任务开始结束、状态变更 |
| warn | 降级（索引失败、缓存失败）、限流触发 |
| error | 未捕获异常、外部依赖不可用 |

**脱敏规则**：密码、Token、Cookie 一律不落日志。`password`、`token`、`authorization` 字段统一替换为 `***`。

### 14.2 指标

暴露 `GET /metrics`（Prometheus 文本格式，仅内网可访问）：

| 指标 | 类型 | 说明 |
|---|---|---|
| `http_requests_total` | counter | 按 method/path/status |
| `http_request_duration_seconds` | histogram | 按 path |
| `db_query_duration_seconds` | histogram | 按操作类型 |
| `cache_hit_total` / `cache_miss_total` | counter | 按 key 前缀 |
| `ws_active_connections` | gauge | 当前连接数 |
| `asynq_task_total` | counter | 按任务名与结果 |
| `index_sync_failed_total` | counter | 索引同步失败次数 |

### 14.3 健康检查

| 端点 | 用途 | 检查内容 |
|---|---|---|
| `/healthz` | 存活探针 | 进程可响应，不查依赖 |
| `/readyz` | 就绪探针 | MySQL Ping、Redis Ping、Meilisearch Health，任一失败返回 503 |

`/readyz` 的响应体列出各依赖状态、耗时与失败原因，便于部署时快速定位。

**未接入的依赖不注册检查项**：阶段 1 只有 MySQL 与 Redis，Meilisearch 在阶段 4 接入后才加入。注册一个永远 `skipped` 的项会让运维误以为"已检查通过"，比不检查更危险。

---

## 15. 部署方案

### 15.1 容器编排

`deploy/docker-compose.yml` 包含 **7 个服务**：

| 服务 | 镜像（固定版本） | 说明 |
|---|---|---|
| `app` | 本地构建 | Go 二进制：HTTP + WebSocket + Worker |
| `web` | 本地构建 | Nuxt3 Node 运行时（SSR） |
| `mysql` | `mysql:8.0.36` | 挂载数据卷；初始化字符集与账号 |
| `blog-redis` | `redis:7.2.4-alpine` | 本项目独立实例，开启 AOF 与 `requirepass`；宿主机映射 `6380:6379`，容器内仍用 `blog-redis:6379` |
| `meilisearch` | `getmeili/meilisearch:v1.6` | 挂载数据卷；配置 master key |
| `minio` | `minio/minio:RELEASE.2024-01-16T16-07-38Z` | **V1.0 不启动**（`profile: storage`）；V1.0 用本地存储驱动，该镜像 tag 在 2026-10 已无法匿名拉取，启用前必须重新核实 |
| `nginx` | `nginx:1.25.4-alpine` | 入口：TLS、静态资源、反向代理 |

**管理端不单独占容器**：`admin` 构建为纯静态文件（Vite 产物），挂载进 `nginx` 容器的 `/usr/share/nginx/html/admin`，由 Nginx 直接服务。为它单独跑一个 Node/Nginx 容器没有收益。

**v1.1 的遗漏**：上一版只列了 6 个服务，**完全没有前端运行时**。PRD 6.4 要求文章页服务端渲染，Nuxt3 SSR 必须有一个 Node 进程——这是既有产品决策的必然结果，不是新增基础设施。若改为纯静态生成（`nuxi generate`），可以去掉 `web` 服务，但文章详情页会退化为客户端取数，SEO 效果打折。V1.0 选择保留 SSR。

**版本约束**

- **禁止使用 `latest` 或浮动 tag**。所有镜像锁定具体版本，保证任意机器、任意时间拉起的环境一致。
- MinIO 不使用语义化版本，tag 为发布日期格式（`RELEASE.YYYY-MM-DDTHH-MM-SSZ`）。上表为设计基线；若该 tag 已从仓库下线，取官方 releases 中同 major 的最新 release 并登记变更。
- 升级任一镜像版本，必须在变更记录中登记原因与回归范围。
- **构建镜像固定到摘要**：`deploy/Dockerfile` 的 `golang` 与 `alpine` 基础镜像同时写补丁版本与 `@sha256:` 摘要，避免上游 tag 被重推导致构建产物不可复现。换版本时必须重新 `docker pull` 并用 `docker inspect .RepoDigests` 核对摘要，不允许手工编写。

**服务数量口径**：7 个容器服务 = 2 个自建应用（`app`、`web`）+ 4 个依赖（mysql / blog-redis / meilisearch / minio）+ 1 个入口（nginx）。若将来把 `app` 拆成 `api` 与 `worker`，数量变为 8，届时更新本节。

### 15.2 多阶段构建

实际文件为 `deploy/Dockerfile`，以下为等价摘要（完整内容以文件为准）：

```dockerfile
# 基础镜像固定补丁版本 + 摘要，摘要必须由 docker pull / inspect 取得
FROM golang:1.27.2-alpine@sha256:f92b6ef8... AS builder
ARG GOPROXY=https://proxy.golang.org,direct   # 受限网络可 --build-arg 覆盖
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM alpine:3.20@sha256:d9e853e8...
# 不执行 apk add：
#   CA 证书从 builder 复制，时区数据由 time/tzdata 内嵌进二进制
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
RUN addgroup -S app && adduser -S -G app app
COPY --from=builder /out/server /usr/local/bin/server
COPY --from=builder /src/migrations /app/migrations
USER app
ENTRYPOINT ["/usr/local/bin/server"]
```

**三个不显然的决定**：

| 决定 | 原因 |
|---|---|
| 运行阶段不装 tzdata，改用 `import _ "time/tzdata"` | 无网络也能构建；且避免 `TZ=Asia/Shanghai` 在缺时区库时静默退化为 UTC 导致时间偏 8 小时 |
| 不执行任何 `apk add` | 减少构建期外网依赖与镜像层攻击面；CA 证书直接复用构建阶段 |
| 不以 root 运行 | `adduser -S app` 后 `USER app`，容器逃逸的影响面更小 |

`.dockerignore` 排除 `deploy/.env` 等凭据文件——否则 `COPY . .` 会把密码写进镜像层，事后无法真正删除。

### 15.3 迁移执行

**迁移不放在应用启动时自动执行**。理由：多实例同时启动会并发抢锁，且失败时无法回滚。

由部署脚本显式执行：

```bash
migrate -path /migrations -database "$DSN" up
```

应用启动时只校验版本：

```go
// 启动时检查 schema 版本与代码期望版本是否一致，不一致则拒绝启动
```

### 15.4 Nginx 关键配置

```nginx
# ---------- 用户端：Nuxt3 SSR ----------
location / {
    proxy_pass http://web:3000;
    proxy_set_header Host              $host;
    proxy_set_header X-Real-IP         $remote_addr;
    proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;   # 应用生成邮件绝对链接依赖此头
}

location /_nuxt/ {
    proxy_pass http://web:3000;
    expires 1y;
    add_header Cache-Control "public, immutable";
}

# ---------- 管理端：SPA 静态文件 ----------
location /admin/ {
    alias /usr/share/nginx/html/admin/;
    try_files $uri $uri/ /admin/index.html;   # 刷新子路由不 404
}

# ---------- 业务接口 ----------
location /api/ {
    proxy_pass http://app:8080;
    proxy_set_header Host              $host;
    proxy_set_header X-Real-IP         $remote_addr;
    proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    client_max_body_size 12m;         # 图片上传上限略大于应用限制
}

# ---------- WebSocket ----------
location /ws {
    proxy_pass http://app:8080;
    proxy_http_version 1.1;
    proxy_set_header Upgrade    $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_set_header Host       $host;
    proxy_read_timeout 3600s;         # 必须大于心跳间隔（90s）
    proxy_send_timeout 3600s;
}
```

**关键点**

| 项 | 原因 |
|---|---|
| 全站统一 `X-Forwarded-Proto` | 应用在 TLS 终止后收到的是 HTTP，生成邮件链接时需要用原始 scheme |
| `/admin/` 的 `try_files` | SPA 刷新子路由时文件不存在，必须回退 index.html，否则 404 |
| `client_max_body_size 12m` | 略大于 `upload.max_image_size`（10MB），避免 Nginx 先于应用拒绝 |
| WebSocket 超时 3600s | 应用心跳 30s + 读超时 90s，代理超时不能接近这个值 |

**不配置 CORS**：同域反代不需要任何 `Access-Control-*` 头。若发现代码里出现 CORS 中间件，说明部署形态被改坏了。

---

## 16. 测试策略

| 类型 | 范围 | 工具 | 运行时机 |
|---|---|---|---|
| 单元测试 | service 层业务规则、状态机、权限判定 | testing + testify | 每次提交 |
| 集成测试 | 真实 MySQL + Redis 的主链路 | testcontainers-go | PR 合并前 |
| 接口测试 | 全部 API 的状态码与响应结构 | httptest | PR 合并前 |
| 端到端 | 手工走验收清单 | — | 每阶段结束 |

### 16.1 关键场景测试矩阵

不设全局覆盖率目标，但以下场景必须有测试，缺一不算通过本阶段。

| 场景 | 类型 | 验证点 |
|---|---|---|
| 并发刷新 | 集成 | 同一 RT 并发 5 次刷新全部成功且返回同一新 token；超出 10 秒窗口的第 6 次返回 401 |
| 刷新重放 | 集成 | 已轮换 RT 在窗口后再使用，返回 401，且该 family 全部令牌失效 |
| 修改密码 | 集成 | 改密后原 RT 立即失效，其他设备被踢下线 |
| 权限缓存失效 | 集成 | 注入 Redis 删除失败，禁用用户操作返回 503，不得静默成功 |
| 角色即时生效 | 集成 | 改角色后无需重新登录，下一次请求即生效 |
| 并发点赞 | 集成 | 50 并发点赞同一文章，记录数 = 1，`like_count` = 1 |
| 计数事务回滚 | 集成 | 计数更新失败时，点赞记录一并回滚，不留单边数据 |
| 乐观锁 | 单元 | 两个并发编辑，后提交者收到 30004 冲突而非静默覆盖 |
| 分类与标签计数 | 单元 | 发布/下架/删除/恢复/改分类/改标签六条路径后，计数与 `COUNT(*)` 一致 |
| 消息 seq | 集成 | 并发发送 50 条，seq 连续无空洞无重复；事务失败后 seq 回滚 |
| 任务入队失败 | 集成 | 停 Redis 后发布文章，发布成功且 `search_sync_status=0`；恢复后对账修复 |
| 搜索服务不可用 | 集成 | 停 Meilisearch 后发布文章成功；搜索接口降级返回空结果而非 500 |
| 搜索结果过滤 | 集成 | 手工向索引插入已删除文章，搜索不返回它（9.4 回表过滤生效） |
| 评论审核 | 单元 | 审核开/关两种模式下，状态流转与 `comment_count` 变化均正确 |
| 文章删除 | 集成 | 删除后公开 URL 410、搜索不含、缓存失效、索引移除四件事同时成立 |
| 缓存失效 | 集成 | 文章更新后立即读到新内容，而非等 TTL 到期 |
| 图片处理 | 集成 | 上传后 variants 生成；处理失败后对账重试成功 |
| **交接窗口关闭** | 集成 | 旧 token 在 60 秒内、同 IP/UA 再次出现 → 返回 401，**不得吊销 family** |
| **交接窗口开启** | 集成 | 同一旧 token 并发 5 次刷新，返回同一新 token；超出窗口后触发 family 吊销 |
| **交接过期一致性** | 集成 | 手工将 handover TTL 置 0，刷新不得返回已失效 token（6.3.3 门槛第 3 项） |
| 越权访问 | 接口 | 直接构造 HTTP 请求：reader 调 admin 接口返回 403，非作者调编辑接口返回 403 |
| 未验证登录 | 接口 | `pending_verify` 用户登录返回 403，且响应无任何 Token Cookie |

### 16.2 集成测试环境

用 `testcontainers-go` 启动 MySQL 与 Redis，每个测试包独立数据库，测试结束销毁。**不使用 mock 数据库**——被 mock 掉的 SQL 行为恰恰是最容易出问题的地方。

失败注入用两种手段：

- **依赖不可用**：临时停止对应容器（模拟 Redis / Meilisearch / MinIO 宕机）
- **缓存删除失败**：用自定义 Redis 客户端包装，按 key 模式返回错误

### 16.3 手工验收

每阶段结束按 PRD 5.1 的编号条目逐条验收，结果记录在验收清单中。第 16.1 节覆盖的是自动化测试，二者不互相替代。

---

## 17. 技术风险与遗留问题

| 项 | 说明 | 影响 | 处理 |
|---|---|---|---|
| PRD 已同步 | PRD 7.1 管理端备注原为"独立应用、独立域名"，已于 v1.4 修订为同域 `/admin` | — | 已闭环，无待办 |
| OpenAPI 契约维护 | spec-first 需手写 `openapi.yaml` | 中 | 接口变更须同步改契约；用生成类型保证不漂移 |
| 交接窗口默认关闭 | 并发刷新依赖 60 秒宽容判定，存在理论盲区 | 低 | 通过 6.3.3 六项验证后可开启 |
| Meilisearch 中文分词 | charabia 分词质量未经实测 | 中 | 阶段 4 用真实文章集验证召回率，不达标则引入自定义词典 |
| 消息 seq 行锁 | `seq_counter` 使同一会话写入串行 | 低 | 单聊频率低可接受；群聊高频写入时改分段预取 |
| 搜索回表过滤 | 索引与库严重不一致时结果不足一页 | 低 | 可接受降级：宁可少给结果，不返回坏链接 |
| Refresh Token 交接窗口 | 默认关闭；并发刷新靠 60 秒 IP/UA 宽容判定 | 低 | 通过 6.3.3 六项验证后可开启（D-D 条件启用） |
| 单机部署上限 | 单进程承载 HTTP + WS + Worker | 中 | PRD 6.3 已把 500 并发降为增长目标；瓶颈时先拆 Worker 进程 |
| Cookie 方案与 SSR 的配合 | Nuxt3 服务端需转发 Cookie | 中 | 阶段 3 验证 SSR 首屏登录态；必要时用中间层转发 |
| 图片处理 CPU 占用 | 大图压缩在应用进程内执行 | 中 | 限制并发（`low` 队列 3 并发），超限排队 |
| 全文检索索引膨胀 | 每次编辑都全量重建文档 | 低 | 文档较小，可接受；将来改为增量字段更新 |
| 头像接口延后 | `PUT /users/me/avatar` 依赖 MinIO 与图片处理，阶段 4 才交付 | 低 | 契约已保留并标注实现阶段；阶段 4 一并接入三尺寸生成 |
| 注销后匿名化未实现 | 禁用用户的公开主页仍显示原昵称 | 中 | 阶段 7 实现注销流程时统一处理，`user_service.PublicProfile` 已留 TODO 标记 |
| Go 版本支持窗口 | `go` 指令为 1.26.0，1.26 将在 Go 1.28 发布时退出支持（预计 2027-02） | 中 | 每半年跟进一次；届时升到 1.27（语言层）与最新补丁（构建层），见 2.3.1 |
| 构建期需要访问模块代理 | 受限网络下 `docker build` 无法下载依赖 | 低 | Dockerfile 已开放 `--build-arg GOPROXY=` 覆盖；镜像固定了摘要，可用内网镜像源 |

### 17.1 尚未设计、留待实现阶段确定的部分

以下几项本文档未展开，属于实现细节而非架构决策：

1. Markdown 渲染器的具体配置（是否允许原始 HTML、代码高亮主题）
2. 邮件模板的具体文案与样式
3. 前端路由表与组件划分
4. 管理端各页面的交互细节
5. 压测脚本的具体场景参数

---

## 18. 与 PRD 的对应检查

### 18.1 覆盖情况

| PRD 章节 | 本文档对应 | 状态 | 说明 |
|---|---|---|---|
| 2.1.1 权限模型 | 6.6 授权中间件 | ✅ 已覆盖 | 单一 role 字段，未引入 Casbin |
| 2.2.1 状态码规则 | 5.3 | ✅ 已覆盖 | 404 / 410 分工已落地 |
| 2.6 认证凭证 | 6.1–6.4 | ✅ 已覆盖 | 含 CSRF / CORS，同域反代（D-C 已定） |
| 3.1 用户状态 | 7.2 users 表、6.5 中间件 | ✅ 已覆盖 | `pending_verify` 已纳入 |
| 3.2 文章状态机 | 12.1 | ✅ 已覆盖 | 状态值与 DDL 一致 |
| 3.5.1 邮箱验证 | 4.2、6.4、10.2 | ✅ 已覆盖 | 令牌生命周期已补全 |
| 7.4 索引缓存一致性 | 第 8、9、10 章 | ✅ 已覆盖 | 状态字段 + 对账 + 回表过滤 |
| 4.1.6.1 单聊规则 | 11.6、11.7、13.8 | ✅ 已覆盖 | seq 改为数据库分配 |
| 5.1 验收条目 | 16.1 测试矩阵 | ⚠️ 部分覆盖 | 测试矩阵覆盖主要场景，逐条验收仍需独立清单 |
| 5.3 安全验收 | 6.x、16.1 | ⚠️ 部分覆盖 | 设计已保证，实际验证在阶段 7 |
| 5.2 性能目标 | — | ❌ 不在范围 | 属验证类要求，技术设计不承诺数字 |
| 5.4 SEO | — | ❌ 不在范围 | 前端实现，本文档不涉及 |

### 18.2 与 PRD 的已知冲突

| # | PRD 位置 | 冲突内容 | 本设计处理 | 建议 |
|---|---|---|---|---|
| 1 | 7.1 技术选型 | PRD 未列日志实现 | 定案 slog（D-A） | **不构成冲突**，属实现层选择 |
| 2 | 5.5 / 8.5 | PRD 要求 OpenAPI 文档 | 定案 OpenAPI 3.0.3 spec-first（D-B） | 已满足，PRD 无需改动 |
| 3 | 7.1 管理端备注 | 原为"独立应用、独立域名" | 定案同域 `/admin` 路径（D-C） | ✅ PRD 已同步修订 |
| 4 | 8.1 | PRD 列 15 类业务对象，未含会话成员 | 实现为 16 张表 | **不构成冲突**，属实现细化，已在 7.1 说明 |
| 5 | 2.6 | PRD 未规定 Refresh Token 交接机制 | 默认关闭，条件启用（D-D） | 不构成冲突 |

### 18.3 结论

- 设计层面**无阻塞问题**
- **4 项决策已全部定案**：D-A slog、D-B OpenAPI 3.0.3 spec-first、D-C 同域反代、D-D 条件启用（默认关闭）
- **阶段 7 部署阻塞已解除**：路径规划、Cookie、CSRF、Nginx 配置均已闭环
- PRD 需同步修订 **0 处**：7.1 管理端备注已改为同域 `/admin`，与本文档一致
- 日志库不构成冲突：PRD 未指定日志实现（v1.2 的相关表述已于 v1.3 更正）
- 本文档 v1.0 声称"全部通过"的表述已撤回，改为按项标注真实状态

---

## 附录 A 问题清单与决策结论

### A.1 v1.1 修订清单

> 本节记录 v1.1 的一致性问题修复。v1.2 的 4 项决策落地见 A.2。

| # | 级别 | 章节 | 问题 | 修改 |
|---|---|---|---|---|
| 1 | 致命 | 6.3 | Refresh Token 声明为不透明串，流程却要求"解析出 jti"，自相矛盾 | 重写为 token_hash 模型，服务端只存 sha256 |
| 2 | 致命 | 11.6 | Redis `INCR` + `IncrBy` 非原子，并发下 seq 重复或跳变 | 改为 `conversations.seq_counter` 单语句原子分配 |
| 3 | 严重 | 7.2 | `comments.status` 默认 1=approved，漏传业务参数即默认公开 | 默认改为 0=pending |
| 4 | 严重 | 6.1 | `access_token` 未指定 Cookie Path | 明确 `/`；refresh 限定 `/api/v1/auth` |
| 5 | 严重 | 6.6 | CSRF 只依赖双提交，未定义 CORS 与 Origin 校验，未覆盖跨子域场景 | 新增三层校验 + 部署形态分析 |
| 6 | 严重 | 6.4 | 邮箱验证、密码重置令牌无存储结构与生命周期定义 | 新增独立小节，`GETDEL` 一次性消费 |
| 7 | 严重 | 6.5.1 | 权限缓存"60 秒 TTL 兜底"，禁用可能延迟生效且无提示 | TTL 改 10s；删除失败重试并返回 503 |
| 8 | 严重 | 7.4.2 | 分类、标签计数在所有状态变更下的规则完全缺失 | 新增"状态是否计入"统一规则 + 收口函数 |
| 9 | 严重 | 7.4.1 | 点赞、收藏、评论计数未定义事务边界 | 明确同事务 + `affectedRows` 判定 |
| 10 | 严重 | 10 | 通知、图片任务失败仅记日志，任务永久丢失 | 新增"可重新发现依据"表 + 三个对账任务 |
| 11 | 中 | 8.1 | `auth:*` 与 `blog:auth:*` 混用；`user:auth` 与 `auth:user` 两种写法 | 统一前缀，重列全表 |
| 12 | 中 | 8.1 | 保留 `blog:conv:seq`，但 seq 已改数据库分配 | 删除该 key |
| 13 | 中 | 9.1 | 索引按 `view_count` 排序，与 Redis 缓冲天然不一致 | 移除该字段与排序能力 |
| 14 | 中 | 9.4 | 已删除/已下架文章可能长期留在索引中被搜到 | 新增搜索结果回表过滤 |
| 15 | 中 | 7.4.3 | `category_id=0` 语义未定义 | 新增未分类语义表 |
| 16 | 中 | 15.1 | 写"5 个服务"实际列出 6 个；镜像使用 `latest` 不可复现 | 更正数量，锁定全部版本 |
| 17 | 中 | 2 | swag 稳定输出 Swagger 2.0，文档写 OpenAPI 3 | 更正表述并说明替代路径 |
| 18 | 中 | 16 | 测试仅列 6 条，缺并发与失败场景 | 扩为 19 项矩阵 + 失败注入手段 |
| 19 | 中 | 18 | 对应检查全标 ✅，与实际存在问题不符 | 改为真实状态并列出 4 项冲突 |
| 20 | 低 | 全文 | 多处引用"附录 A.3"，但附录不存在 | 补全本附录 |
| 21 | 低 | 6.3.2 | 未说明为何轮换必须用 Lua | 补充中间态风险说明 |
| 22 | 低 | 10.7 | `notification:create` 缺幂等方案 | 加 `uk_user_type_ref` 唯一索引 + `INSERT IGNORE` |
| 23 | 低 | 3 | 目录树写 `docs/PRD.md`，实际文件位于仓库根目录 | 注明当前布局 |

### A.2 决策结论（v1.2 全部关闭）

| 编号 | 决策点 | 结论 | 落地章节 | 状态 |
|---|---|---|---|---|
| **D-A** | 日志库 | 标准库 `log/slog`，统一经 `internal/pkg/logger`；禁止业务代码自建 logger | 2.2 | ✅ 已定 |
| **D-B** | 接口文档 | **OpenAPI 3.0.3，spec-first**，`oapi-codegen` 只生成类型，Gin 保留 | 2.1 | ✅ 已定 |
| **D-C** | 部署形态 | 同域 Nginx 反代，`/` `/_nuxt` `/admin` `/api` `/ws` 五类路径 | 6.7.1 / 15.1 / 15.4 | ✅ 已定 |
| **D-D** | 交接窗口 | **默认关闭**；通过 6.3.3 六项验证后方可开启 | 6.3.3 / 6.3.4 | ⏸ 条件启用 |

**D-B 的核实结论（重要）**

原以为可以让 swag 直接产出 OpenAPI 3，核实后不成立：

- swaggo 默认输出 **Swagger 2.0**
- 其 3.x 输出需要显式开关，且不同版本参数名不一致（`--oas` / `--openapi`），社区有 Swagger UI 版本校验失败的报告
- 能稳定产出 OpenAPI 3 的 Go 方案是 huma，但它要求 handler 基于 net/http 重写，等于换框架

因此收敛为 **spec-first**：手写 `api/openapi.yaml`，用 `oapi-codegen` 生成 DTO 类型，路由仍由 Gin 管理。这是"保留 Gin + 拿到 OpenAPI 3 契约"的唯一稳定组合，也是 Go 生态里 design-first 的主流做法。

**D-D 的启用门槛**

默认 `security.refresh_handover=false`。关闭时并发刷新靠 60 秒宽容判定（IP/UA 指纹），存在理论盲区但是可预期行为。开启需先通过 6.3.3 的六项验证，其中第 3 项（交接过期一致性）是 v1.1 遗漏的边界。

**与 PRD 的一致性**

| 项 | PRD 现状 | 本文档 | 状态 |
|---|---|---|---|
| 管理端部署 | 7.1 已修订为"同域 `/admin`，V1.0 不要求独立域名" | 同域 `/admin` 子路径 | ✅ 已收敛 |

日志库**不在此列**：PRD 7.1 未指定日志实现，slog 是实现层选择。v1.2 曾误称"PRD 写 zap"，已于 v1.3 更正。

### A.3 仍未解决的问题

| 问题 | 阻塞开发 | 处理方式 |
|---|---|---|
| Meilisearch 中文分词质量未验证 | 否 | 阶段 4 用真实文章集实测召回率 |
| Nuxt3 SSR 与 Cookie 转发链路 | 否 | 阶段 3 验证首屏登录态 |
| 压测参数与性能基线 | 否 | 阶段 7 |
| PRD 与本文档 2 处表述不一致 | 否 | 随 D-A / D-B 的结论同步修订 PRD |
| 搜索结果回表过滤的额外查询开销 | 否 | 阶段 4 压测确认，必要时对过滤结果做短缓存 |

### A.4 一致性检查结果

| 检查项 | 结果 |
|---|---|
| 表结构字段与 DDL 一致 | ✅ 已核对（comments 默认值、conversations.seq_counter、notifications 唯一键） |
| Redis Key 全文统一 `blog:` 前缀 | ✅ 已核对，无 `auth:*` 裸前缀残留 |
| API 路径与接口清单一致 | ✅ 已核对，`{slug}` / `{id}` 使用规则已在 13.3 说明 |
| 状态码与 PRD 2.2.1 一致 | ✅ 已核对 |
| 状态机与 DDL 枚举一致 | ✅ 已核对（0–6 共 7 态） |
| 事务边界描述完整 | ✅ 点赞、收藏、评论、seq、文章状态变更均已标注 |
| 异步任务均有恢复路径 | ✅ 已核对，邮件与聊天推送的不补偿理由已说明 |
| 章节交叉引用有效 | ✅ 已核对，6.x 重编号后引用已同步 |
| 部署清单与实际服务数一致 | ✅ 已核对（7 个服务，含 web） |
| 决策项全部关闭 | ✅ D-A / D-B / D-C 已定案，D-D 条件启用并写明门槛 |
| 旧决策无残留 | ✅ 全文无"待决策 D-*"表述，无 `rt:used` 残留 |
