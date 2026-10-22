# 接口规范（API Spec）

| 项 | 内容 |
|---|---|
| 文档版本 | v1.2（分页拆为游标/偏移两套；聊天已读字段改为 `last_read_seq`；上游 PRD v0.6 / TECH_DESIGN v1.5） |
| 上游依据 | PRD v0.6、TECH_DESIGN v1.5、DATABASE.md v1.2 |
| 文档状态 | 待评审 |
| 更新时间 | 2026-10-21 |
| 规范版本 | OpenAPI 3.0.3 |
| 契约文件 | `api/openapi.yaml`（本文档是其人类可读版本） |

**职责边界**

- 本文档定义**接口契约**：路径、方法、权限、请求、响应、错误码
- 业务规则以 PRD 为准，实现方式以 TECH_DESIGN 为准
- 契约变更顺序：**先改本文档 → 再改 `openapi.yaml` → 重新生成类型 → 编译报错驱动 handler 修改**

---

## 1. 通用约定

### 1.1 Base URL 与版本

| 环境 | URL |
|---|---|
| 本地 | `http://localhost:8080/api/v1` |
| 生产 | `https://www.example.com/api/v1` |

版本放在路径中（`/v1`），不做 Header 版本协商。V1.0 期间不引入破坏性变更；若必须变更，升 `/v2` 并保留 `/v1` 一段时间。

### 1.2 认证方式

**不使用 `Authorization: Bearer` 头**。认证完全依赖 Cookie（TECH_DESIGN 6.1）：

| Cookie | Path | 用途 |
|---|---|---|
| `access_token` | `/` | 每次请求自动携带 |
| `refresh_token` | `/api/v1/auth` | 仅刷新与退出接口携带 |
| `csrf_token` | `/` | 前端读取后放入 `X-CSRF-Token` 头 |

**写请求必须带 CSRF 头**：

```
X-CSRF-Token: <csrf_token 的值>
```

前端请求需设置 `credentials: 'include'`（同域下为默认行为，但显式声明更安全）。

### 1.3 请求格式

- 编码：`application/json; charset=utf-8`，文件上传用 `multipart/form-data`
- 字段命名：`snake_case`（与数据库一致，避免二次映射）
- 未知字段：忽略，不报错（保证向前兼容）
- 字符串：首尾空白由服务端 `TrimSpace`

### 1.4 响应格式

**所有响应统一包裹**，HTTP 状态码与业务码分离：

```json
{
  "code": 0,
  "message": "ok",
  "data": {},
  "request_id": "01HQZ8X2K9J4M"
}
```

| 字段 | 说明 |
|---|---|
| `code` | 业务码，`0` 为成功 |
| `message` | 可直接展示的中文提示；成功时为 `ok` |
| `data` | 业务数据；失败时为 `null` |
| `request_id` | 请求追踪 ID，排查问题时提供此值 |

**HTTP 状态码使用规则**

| 场景 | HTTP | 说明 |
|---|---|---|
| 成功（查询/更新） | 200 | — |
| 成功（创建） | 201 | 响应体含新资源 ID |
| 成功（删除/无内容） | 204 | 响应体为空，**不包裹** |
| 参数错误 | 400 | — |
| 未登录 / Token 失效 | 401 | — |
| 已登录但无权 | 403 | — |
| 资源不存在或不可见 | 404 | 草稿等非公开内容对无权者返回 404 |
| 资源已永久删除 | 410 | 仅文章 |
| 冲突 | 409 | 重复注册、slug 冲突、乐观锁 |
| 限流 | 429 | 带 `Retry-After` 头 |
| 服务端错误 | 500 | 不返回堆栈 |
| 依赖不可用 | 503 | 检索/存储/缓存 |

### 1.5 ID 与时间

| 项 | 约定 |
|---|---|
| **ID** | **一律以字符串返回**，如 `"id": "1001"` |
| 时间 | RFC3339 UTC，如 `"2026-10-21T08:20:11Z"` |
| 时长/计数 | 整数，单位为秒或个 |

**ID 为什么用字符串**：数据库是 `BIGINT UNSIGNED`，超过 `2^53` 后 JavaScript 会丢失精度。虽然 V1.0 达不到这个量级，但改成字符串的成本是零，等到出问题时再改要动所有前端代码。

### 1.6 分页

**V1.0 存在两种分页方式，按数据来源区分，不得混用。**

| 场景 | 分页方式 | 原因 |
|---|---|---|
| 数据库查询类列表（文章、评论、通知、消息、收藏等） | **游标** | 数据可增长，游标不受深度限制，深翻页性能恒定 |
| 全文搜索 `GET /search` | **偏移** | Meilisearch 无原生游标，且结果集受 `maxTotalHits` 硬限制，无法突破 |

> **v1.0 文档的表述错误**：原文写"统一游标分页，不使用 offset"。这对搜索接口无法成立——Meilisearch 不提供游标，应用层伪造的 cursor 本质仍是 offset。此处已按实际能力拆分。

#### 1.6.1 游标分页（默认）

**请求参数**

| 参数 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `limit` | int | 20 | 每页条数，1–100 |
| `cursor` | string | 空 | 上一页返回的 `next_cursor`，首页不传 |

**响应结构**

```json
{
  "items": [],
  "next_cursor": "eyJpZCI6IjEwMDEifQ",
  "has_more": true
}
```

`next_cursor` 为不透明字符串，前端不得解析。`has_more=false` 时 `next_cursor` 为空串。

#### 1.6.2 偏移分页（仅搜索接口）

**请求参数**

| 参数 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `page` | int | 1 | 页码，从 1 开始 |
| `page_size` | int | 20 | 每页条数，1–50 |

**响应结构**

```json
{
  "items": [],
  "page": 1,
  "page_size": 20,
  "estimated_total": 976,
  "has_more": true,
  "degraded": false
}
```

**硬限制**：`page × page_size` 不得超过 **1000**。这是 Meilisearch 的 `maxTotalHits` 默认值，超出直接返回 400 + 10001，提示"请使用更精确的关键词"。

`estimated_total` 对应 Meilisearch 的 `estimatedTotalHits`，**是估算值不是精确总数**。前端不得用它计算总页数，只能用于判断"是否还有下一页"。

**为什么搜索不能做游标**

1. Meilisearch 1.6 仅支持 `offset/limit` 与 `page/hitsPerPage` 两种分页，没有 ES `search_after` 那样的游标
2. 应用层包装的 cursor 底层仍是 offset，无法突破 `maxTotalHits`
3. 调高 `maxTotalHits` 会让每次查询的排序管线成本线性上升，官方明确警告超过 20000 可能耗时数秒

**不提供页码选择器**：官方推荐 `offset/limit` + 上一页/下一页，因为计算精确总数代价很高。前端只提供"加载更多"，用 `has_more` 判断是否还能继续。

### 1.7 幂等

| 场景 | 机制 |
|---|---|
| 点赞/收藏 | 数据库联合主键，重复请求返回成功 |
| 发送消息 | `client_msg_id` 唯一索引，重复提交返回首次结果 |
| 创建文章 | 不幂等。重复提交会产生多篇草稿 |
| 举报 | `(reporter_id, target_type, target_id)` 唯一，重复举报返回成功但不新增 |

### 1.8 限流

| 接口 | 限制 |
|---|---|
| `POST /auth/login` | 单账号 5 次失败锁 15 分钟；单 IP 10 次/分钟 |
| `POST /auth/register` | 单 IP 5 次/小时 |
| `POST /auth/resend-verify` | 单邮箱 60 秒 1 次、24 小时 10 次 |
| `POST /auth/forgot-password` | 单邮箱 60 秒 1 次、24 小时 5 次 |
| `POST /articles/{id}/comments` | 单用户 10 次/分钟 |
| `POST /conversations/{id}/messages` | 单用户 30 次/分钟 |
| `POST /conversations` | 单用户 20 次/天 |
| `POST /uploads/images` | 单用户 50 次/小时 |

超限返回 429 + `Retry-After`（秒）。

### 1.9 权限标记

| 标记 | 含义 |
|---|---|
| 公开 | 无需登录 |
| 登录 | 任意 `active` 用户 |
| 作者 | `role >= author` |
| 本人 | 资源所有者，且 `role >= author` |
| 管理员 | `role = admin` |
| 成员 | 会话成员 |

**前端隐藏按钮不是权限控制**。所有接口必须独立鉴权，验收时以直接构造 HTTP 请求为准（PRD 2.5）。

### 1.10 路径参数与路由约定

| 资源 | 参数形式 | 说明 |
|---|---|---|
| 文章详情 | `/articles/{slug}` | 公开读，面向 SEO 与可读性 |
| 文章子资源（编辑取文、评论、点赞、收藏） | `/articles/{id}/...` | 数字 ID，避免 slug 解析开销 |
| 用户资源 | `/users/{id}/...` 或 `/users/me/...` | 自己的资源用 `me` |
| 管理端 | `/admin/...` | 一律用数字 ID |

**保留字冲突（必须先解决）**

`/articles/mine` 这类静态段会与 `/articles/{slug}` 冲突：若某篇文章的 slug 恰好等于保留字，详情页将永远命中另一个资源。因此：

- 用户维度资源不放 `/articles/*`，统一挂 `/users/me/*`
- slug 生成时禁用保留字：`mine`、`new`、`edit`、`create`、`search`、`admin`、`me`
- 用户手工填写 slug 时命中保留字，返回 30003 并提示更换

**Gin 路由约束**

`/articles/:key`（详情）与 `/articles/:key/edit`（子资源）共用同一路径位置，Gin 要求同一位置的参数名一致。实现时统一命名为 `:key`，由 handler 判断：**全数字按 ID 查询，否则按 slug 查询**。

这样同一套路由既支持 SEO 友好的 slug，也支持后台按 ID 直取，不必为两种标识各开一条路径。

---

## 2. 错误码

### 2.1 通用

| code | HTTP | message | 说明 |
|---|---|---|---|
| 0 | 200/201 | — | 成功 |
| 10001 | 400 | 参数错误 | 字段校验失败，`data.errors` 给出明细 |
| 10002 | 401 | 请先登录 | 无有效 Token |
| 10003 | 403 | 无权限执行此操作 | 角色不足或非资源所有者 |
| 10004 | 404 | 资源不存在 | 含"存在但无权查看" |
| 10005 | 429 | 操作过于频繁，请稍后再试 | 限流 |
| 10006 | 403 | 请求校验失败 | CSRF Token 缺失或不匹配 |
| 11001 | 500 | 服务异常，请稍后重试 | 未捕获异常 |
| 11002 | 500 | 数据库异常 | — |
| 11003 | 503 | 缓存服务不可用 | — |
| 11004 | 503 | 搜索服务不可用 | 检索降级 |
| 11005 | 503 | 存储服务不可用 | 上传失败 |

### 2.2 认证与用户

| code | HTTP | message |
|---|---|---|
| 20001 | 409 | 该邮箱已注册 |
| 20002 | 409 | 该用户名已被占用 |
| 20003 | 401 | 用户名或密码错误 |
| 20004 | 403 | 请先验证邮箱 |
| 20005 | 403 | 账号已被禁用 |
| 20006 | 403 | 账号已锁定，请 15 分钟后重试 |
| 20007 | 400 | 验证链接已失效 |
| 20008 | 400 | 重置链接已失效 |
| 20009 | 400 | 原密码错误 |

### 2.3 文章

| code | HTTP | message |
|---|---|---|
| 30001 | 404 | 文章不存在 |
| 30002 | 400 | 当前状态不允许此操作 |
| 30003 | 409 | 该 URL 标识已被占用 |
| 30004 | 409 | 文章已被修改，请刷新后重试 |
| 30005 | 400 | 标签数量不能超过 5 个 |
| 30006 | 410 | 文章已删除 |

### 2.4 评论与互动

| code | HTTP | message |
|---|---|---|
| 40001 | 404 | 评论不存在 |
| 40002 | 400 | 该文章已关闭评论 |
| 40003 | 400 | 请勿重复操作 |

### 2.5 聊天

| code | HTTP | message |
|---|---|---|
| 50001 | 404 | 会话不存在 |
| 50002 | 403 | 你不在该会话中 |
| 50003 | 403 | 对方账号当前不可用 |
| 50004 | 429 | 发起会话过于频繁 |

### 2.6 管理端

| code | HTTP | message |
|---|---|---|
| 60001 | 400 | 不能对自己执行此操作 |
| 60002 | 400 | 系统必须保留至少一个管理员 |
| 60003 | 400 | 请填写理由 |

---

## 3. 接口总览

共 **81 个接口**（按 `method + path` 组合计），按模块分组（2026-10-22 与 openapi.yaml 契约对齐后修正）。

| 模块 | 数量 | 说明 |
|---|---|---|
| 认证 | 8 | 注册、验证、登录、刷新、退出、找回密码 |
| 用户 | 5 | 当前用户、资料、密码、头像、公开主页（我的文章/收藏计入文章与互动） |
| 文章 | 14 | CRUD、4 个状态流转、编辑取文、我的文章、分类、标签、搜索 |
| 互动 | 9 | 点赞 2、收藏 2、收藏夹、评论 3、举报 |
| 通知 | 4 | 列表、未读数、单条已读、全部已读 |
| 媒体 | 2 | 上传、取访问地址 |
| 聊天 | 8 | 会话 2、消息 2、已读、免打扰、WS 票据、WS 连接 |
| 管理端 | 31 | 用户 4、文章 7、评论 4、举报 2、分类 3、标签 4、媒体 1、系统运维 6 |

> 计数口径：以 `method + path` 为准，与 `api/openapi.yaml` 的 81 个 operation 一一对应。

---

## 4. 接口详细定义

### 4.1 认证

#### POST /auth/register 注册

**权限**：公开 ｜ **CSRF**：白名单

**请求**

```json
{
  "username": "zhangsan",
  "email": "zhang@example.com",
  "password": "Passw0rd!"
}
```

| 字段 | 类型 | 必填 | 校验 |
|---|---|---|---|
| `username` | string | 是 | 4–20 位，`^[a-zA-Z0-9_]+$` |
| `email` | string | 是 | 合法邮箱，≤128 |
| `password` | string | 是 | 8–32 位，须同时含字母与数字 |

**响应 201**

```json
{
  "code": 0,
  "message": "注册成功，请查收验证邮件",
  "data": { "user_id": "1001", "status": "pending_verify" },
  "request_id": "01HQZ8X2K9J4M"
}
```

**业务规则**

- 创建后 `status = pending_verify`，**不签发任何 Token**
- 立即发送验证邮件，链接 24 小时有效
- 用户名或邮箱重复返回 409（分别对应 20002 / 20001），并在 `data` 中带 `resend_verify: true` 提示可重发验证

**错误**：10001、20001、20002、10005

---

#### GET /auth/verify-email 验证邮箱

**权限**：公开 ｜ **CSRF**：白名单

**请求参数**

| 参数 | 位置 | 说明 |
|---|---|---|
| `token` | query | 邮件中的令牌 |

**响应 200**

```json
{ "code": 0, "message": "验证成功，请登录", "data": null }
```

**业务规则**

- 一次性消费，`GETDEL` 取出后立即失效
- 成功后 `status → active`，写入 `email_verified_at`
- 链接过期返回 20007，前端应展示"重新发送"入口

**错误**：20007

---

#### POST /auth/resend-verify 重发验证邮件

**权限**：公开 ｜ **CSRF**：白名单

**请求**：`{ "email": "zhang@example.com" }`

**响应 200**：`{ "code": 0, "message": "若该邮箱未验证，验证邮件已发送" }`

**业务规则**

- 无论邮箱是否存在、是否已验证，**统一返回相同文案**，避免邮箱枚举
- 未验证用户走此接口；已激活用户调用不发送任何邮件

**错误**：10005

---

#### POST /auth/login 登录

**权限**：公开 ｜ **CSRF**：白名单

**请求**

```json
{ "account": "zhang@example.com", "password": "Passw0rd!" }
```

`account` 支持邮箱或用户名。

**响应 200**

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "user": {
      "id": "1001",
      "username": "zhangsan",
      "nickname": "张三",
      "avatar_url": "https://.../a256.webp",
      "role": "author"
    },
    "csrf_token": "8f3a...c9"
  }
}
```

**响应头**

```
Set-Cookie: access_token=...; Path=/; HttpOnly; Secure; SameSite=Lax
Set-Cookie: refresh_token=...; Path=/api/v1/auth; HttpOnly; Secure; SameSite=Lax
Set-Cookie: csrf_token=...; Path=/; Secure; SameSite=Lax
```

**业务规则**

- 密码错误与账号不存在返回**同一错误**（20003），不区分
- `pending_verify` 返回 20004，且**不设置任何 Cookie**
- `disabled` 返回 20005
- 连续失败 5 次锁定 15 分钟（20006）
- 登录成功写入 `last_login_at` / `last_login_ip`

**错误**：20003、20004、20005、20006、10005

---

#### POST /auth/refresh 刷新 Token

**权限**：凭 `refresh_token` Cookie ｜ **CSRF**：白名单

**请求**：无 body，凭证全在 Cookie

**响应 200**

```json
{ "code": 0, "data": { "csrf_token": "新值" } }
```

同时通过 `Set-Cookie` 覆盖三个 Cookie。

**业务规则**

- Refresh Token 一次性使用，轮换后旧值失效（TECH_DESIGN 6.3.2）
- 交接窗口默认关闭；关闭时并发刷新的第二个请求返回 401，客户端应重试一次
- 检测到重放时吊销该 family 全部令牌

**错误**：10002（Token 无效/过期/已使用）

---

#### POST /auth/logout 退出登录

**权限**：登录 ｜ **CSRF**：必需

**响应 204**，并清除三个 Cookie。

**业务规则**：仅吊销当前设备所属 family，不影响其他设备。

---

#### POST /auth/forgot-password 申请重置密码

**权限**：公开 ｜ **CSRF**：白名单

**请求**：`{ "email": "zhang@example.com" }`

**响应 200**：统一文案，不泄露邮箱是否存在。

**业务规则**

- `active` 用户：发送重置链接，15 分钟有效
- `pending_verify` 用户：**改发验证邮件**，不生成重置令牌（PRD 3.5.1）
- 单邮箱 60 秒 1 次、24 小时 5 次

**错误**：10005

---

#### POST /auth/reset-password 重置密码

**权限**：公开 ｜ **CSRF**：白名单

**请求**

```json
{ "token": "abc123", "new_password": "NewPassw0rd!" }
```

**响应 200**

**业务规则**

- 令牌一次性消费
- 成功后**吊销该用户全部 Refresh Token**，所有设备需重新登录
- 删除该用户所有未使用的重置令牌

**错误**：10001、20008

---

### 4.2 用户

#### GET /users/me 当前用户

**权限**：登录

**响应 200**

```json
{
  "code": 0,
  "data": {
    "id": "1001",
    "username": "zhangsan",
    "email": "zhang@example.com",
    "nickname": "张三",
    "avatar_url": "https://...",
    "bio": "写代码的",
    "role": "author",
    "status": "active",
    "created_at": "2026-01-01T00:00:00Z"
  }
}
```

---

#### PATCH /users/me 更新资料

**权限**：登录 ｜ **CSRF**：必需

**请求**（字段可选，只传要改的）

```json
{ "nickname": "新昵称", "bio": "新简介" }
```

| 字段 | 校验 |
|---|---|
| `nickname` | 1–32 字符 |
| `bio` | ≤255 字符 |

用户名与邮箱**不可通过此接口修改**。

---

#### PUT /users/me/password 修改密码

**权限**：登录 ｜ **CSRF**：必需

**请求**

```json
{ "old_password": "...", "new_password": "..." }
```

**业务规则**：成功后吊销全部 Refresh Token，清空 Cookie，前端应跳登录页。

**错误**：20009、10001

---

#### PUT /users/me/avatar 更新头像

> **实现状态**：契约保留，**阶段 4 交付**。依赖对象存储（MinIO）与三尺寸图片处理，
> 属于"检索与媒体"阶段的交付物；阶段 2 未注册该路由，调用返回 404。


**权限**：登录 ｜ **CSRF**：必需

**请求**：`multipart/form-data`，字段 `file`

**响应 200**

```json
{ "data": { "avatar_url": "https://.../a256.webp", "media_id": "2001" } }
```

**业务规则**：服务端生成 64/128/256 三档，`avatar_url` 返回 256 档。

**错误**：10001（格式/大小不符）、11005

---

#### GET /users/{id} 公开主页

**权限**：公开

**响应 200**

```json
{
  "data": {
    "id": "1001",
    "username": "zhangsan",
    "nickname": "张三",
    "avatar_url": "...",
    "bio": "...",
    "article_count": 12,
    "created_at": "..."
  }
}
```

不返回 `email`、`status`、`last_login_*`。

**错误**：10004（用户不存在或已禁用）

---

#### GET /users/{id}/articles 某人的公开文章

**权限**：公开 ｜ 分页

**请求参数**：`limit`、`cursor`

**业务规则**：只返回 `status=published` 的文章，按 `published_at` 倒序。

---

#### GET /users/me/articles 我的文章

**权限**：作者 ｜ 分页

> 路径挂在 `/users/me/*` 而非 `/articles/mine`：`mine` 是静态段，会与 `/articles/{slug}` 形成保留字冲突（若某篇文章 slug 恰为 `mine`，将永远无法访问）。见 1.10。

**请求参数**：`status`（可选，多值逗号分隔）、`q`（标题关键词）、`limit`、`cursor`

**响应**：同公开列表结构，额外含 `status`、`updated_at`、`reject_reason`。

**业务规则**：返回当前用户全部状态的文章，含草稿、待审、已拒绝、已下架。

---

### 4.3 文章

#### GET /articles 公开列表

**权限**：公开 ｜ 分页

**请求参数**

| 参数 | 类型 | 说明 |
|---|---|---|
| `limit` | int | 默认 20 |
| `cursor` | string | 游标 |
| `sort` | string | `latest`（默认）/ `hot` |
| `category_id` | string | 可选，`"0"` 表示未分类 |
| `tag_id` | string | 可选 |

**响应 200**

```json
{
  "data": {
    "items": [
      {
        "id": "1001",
        "slug": "go-concurrency",
        "title": "Go 并发模型详解",
        "summary": "从 GMP 讲起...",
        "cover_url": "...",
        "author": { "id": "12", "nickname": "张三", "avatar_url": "..." },
        "category": { "id": "3", "name": "后端" },
        "tags": [{ "id": "5", "name": "Go" }],
        "view_count": 3200,
        "like_count": 45,
        "comment_count": 8,
        "reading_time": 12,
        "is_top": false,
        "published_at": "2026-10-01T10:00:00Z"
      }
    ],
    "next_cursor": "eyJ...",
    "has_more": true
  }
}
```

**排序规则**

| sort | 排序 |
|---|---|
| `latest` | `is_top DESC, published_at DESC, id DESC` |
| `hot` | `view_count DESC, published_at DESC`，仅统计近 30 天 |

**业务规则**

- 只返回 `status=published` 且 `deleted_at IS NULL`
- `hot` 走数据库直查，不走搜索引擎（TECH_DESIGN 9.1）
- 列表项**不包含正文**

---

#### GET /articles/{slug} 文章详情

**权限**：公开（非公开状态见下）

**响应 200**

```json
{
  "data": {
    "id": "1001",
    "slug": "go-concurrency",
    "title": "Go 并发模型详解",
    "content_html": "<p>...</p>",
    "status": "published",
    "author": {...},
    "category": {...},
    "tags": [...],
    "view_count": 3200,
    "like_count": 45,
    "favorite_count": 12,
    "comment_count": 8,
    "allow_comment": true,
    "is_liked": false,
    "is_favorited": false,
    "published_at": "...",
    "updated_at": "..."
  }
}
```

**关键规则（PRD 2.2.1）**

| 文章状态 | 访问者 | 结果 |
|---|---|---|
| published | 任何人 | 200 |
| draft/pending/rejected/offline/archived | 作者本人 | 200，`status` 字段标明状态 |
| 同上 | 其他人（含管理员） | **404** |
| deleted | **所有人** | **410** |

- 不返回 `content_md`，编辑请用 `/articles/{id}/edit`
- `is_liked` / `is_favorited` 在未登录时恒为 `false`

**错误**：10004、30006

---

#### GET /articles/{id}/edit 取正文供编辑

**权限**：本人（作者） ｜ **CSRF**：无（GET）

**响应 200**

```json
{
  "data": {
    "id": "1001",
    "title": "...",
    "slug": "...",
    "summary": "...",
    "content_md": "# 标题\n正文...",
    "cover_url": "...",
    "category_id": "3",
    "tag_ids": ["5", "8"],
    "status": "draft",
    "version": 7,
    "allow_comment": true,
    "reject_reason": "",
    "updated_at": "..."
  }
}
```

**业务规则**

- **只有作者本人可调用**，管理员返回 403（管理员只能下架/删除，不能改正文）
- 已删除文章返回 30006

**错误**：30001、10003、30006

---

#### POST /articles 创建草稿

**权限**：作者 ｜ **CSRF**：必需

**请求**

```json
{
  "title": "新文章",
  "slug": "new-post",
  "content_md": "# 正文",
  "summary": "",
  "category_id": "3",
  "tag_ids": ["5"]
}
```

| 字段 | 必填 | 说明 |
|---|---|---|
| `title` | 是 | 1–200 |
| `slug` | 否 | 为空则自动生成；格式 `^[a-z0-9-]{1,200}$` |
| `content_md` | 是 | Markdown 原文 |
| `summary` | 否 | 为空则截取正文前 200 字 |
| `category_id` | 否 | 默认 `"0"`；非 0 时校验分类存在 |
| `tag_ids` | 否 | 最多 5 个 |

**响应 201**

```json
{ "data": { "id": "1050", "slug": "new-post", "status": "draft", "version": 0 } }
```

**错误**：10001、30003（slug 冲突）、30005

---

#### PUT /articles/{id} 更新文章

**权限**：本人 ｜ **CSRF**：必需

**请求**（含乐观锁）

```json
{
  "title": "...",
  "content_md": "...",
  "category_id": "3",
  "tag_ids": ["5"],
  "version": 7
}
```

**响应 200**

```json
{ "data": { "id": "1050", "version": 8, "status": "published" } }
```

**业务规则**

- `version` 必须与库中一致，否则返回 30004
- 审核开启时，已发布文章**编辑后立即生效，不重新审核**（PRD 3.2.1）
- 成功后：`search_sync_status = 0`、失效缓存、异步更新索引

**错误**：30001、30004、10003、30005

---

#### DELETE /articles/{id} 删除文章

**权限**：本人 / 管理员 ｜ **CSRF**：必需

**响应 204**

**业务规则**

- 软删除，`status=6`、写 `deleted_at`
- 管理员删除必须提供理由：`?reason=xxx`
- 同步：移除搜索索引、失效缓存、写墓碑
- 此后公开 URL 返回 **410**

**错误**：30001、10003、60003

---

#### POST /articles/{id}/submit 提交（发布或送审）

**权限**：本人 ｜ **CSRF**：必需

**响应 200**

```json
{ "data": { "id": "1050", "status": "published" } }
```

**业务规则**

- 审核关闭：`draft/rejected → published`，写 `published_at`（仅首次）
- 审核开启：`draft/rejected → pending`
- 校验：标题非空、正文非空、分类有效

**错误**：30001、30002、10001

---

#### POST /articles/{id}/withdraw 撤回审核

**权限**：本人 ｜ **CSRF**：必需

**业务规则**：仅 `pending → draft`。其他状态返回 30002。

---

#### POST /articles/{id}/archive 归档

**权限**：本人 ｜ **CSRF**：必需

**业务规则**：`published → archived`，分类与标签计数各 -1，移除索引。

---

#### POST /articles/{id}/restore 恢复为草稿

**权限**：本人 ｜ **CSRF**：必需

**业务规则**：`archived → draft`。不改变计数（未发布状态不计入）。

---

### 4.4 分类、标签、搜索

#### GET /categories

**权限**：公开

**响应**

```json
{
  "data": [
    { "id": "0", "name": "未分类", "slug": "uncategorized", "article_count": 3 },
    { "id": "3", "name": "后端", "slug": "backend", "article_count": 42 }
  ]
}
```

**注意**：`id="0"` 是虚拟分类，不对应 `categories` 表记录，用于前端统一渲染筛选器。

---

#### GET /tags

**权限**：公开 ｜ 参数：`limit`（默认 50，按 `article_count` 倒序）

---

#### GET /search 全文检索

**权限**：公开 ｜ **偏移分页**（见 1.6.2，不走游标）

**请求参数**

| 参数 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `q` | string | 必填 | 关键词，1–100 字符 |
| `page` | int | 1 | 页码，从 1 开始 |
| `page_size` | int | 20 | 每页条数，1–50 |

**响应 200**

```json
{
  "data": {
    "items": [
      {
        "id": "1001",
        "slug": "go-concurrency",
        "title": "Go 并发模型详解",
        "highlight": "Go <em>并发</em>模型详解",
        "summary": "...",
        "published_at": "..."
      }
    ],
    "page": 1,
    "page_size": 20,
    "estimated_total": 37,
    "has_more": false,
    "degraded": false
  }
}
```

**分页与回表过滤的配合**

1. 向 Meilisearch 请求 `limit = page_size`、`offset = (page - 1) × page_size`
2. 拿到命中 ID 后回表查 `articles`，仅保留 `status=2 AND deleted_at IS NULL`
3. 过滤后条数可能少于 `page_size`——**不补齐、不重试**。offset 严格递增，补齐需额外请求且会打乱偏移
4. `has_more` 判定：`offset + 本次命中数 < min(estimated_total, 1000)`

**为什么允许"这一页不满"**：索引与数据库存在同步延迟，已删文章可能残留在索引中。宁可某页只显示 12 条，也不返回打不开的链接（TECH_DESIGN 9.4）。索引重建后自然恢复。

**其他规则**

- `page × page_size > 1000` 返回 400 + 10001，提示使用更精确的关键词
- 检索服务不可用：返回 200 + 空 `items` + `degraded: true`，**不返回 5xx**
- 排序：相关度优先，其次 `published_at` 倒序
- `estimated_total` 为估算值，**不得用于计算总页数**

**错误**：10001（含分页超出上限）

---

### 4.5 互动

#### POST /articles/{id}/like 点赞

**权限**：登录 ｜ **CSRF**：必需

**响应 200**：`{ "data": { "liked": true, "like_count": 46 } }`

**业务规则**

- 幂等：已点赞返回相同结果
- 同事务内 `article_likes` 插入 + `articles.like_count + 1`
- 只允许对 `published` 文章点赞

**错误**：30001、10004

---

#### DELETE /articles/{id}/like 取消点赞

**权限**：登录 ｜ **CSRF**：必需

**响应 200**：`{ "data": { "liked": false, "like_count": 45 } }`

---

#### POST /articles/{id}/favorite 收藏

**权限**：登录 ｜ **CSRF**：必需

**响应 200**：`{ "data": { "favorited": true, "favorite_count": 13 } }`

---

#### DELETE /articles/{id}/favorite 取消收藏

**权限**：登录 ｜ **CSRF**：必需

---

#### GET /users/me/favorites 我的收藏

**权限**：登录 ｜ 分页

**业务规则**：按收藏时间倒序。已被删除或下架的文章**不返回**（避免死链）。

---

#### GET /articles/{id}/comments 评论列表

**权限**：公开 ｜ 分页

**请求参数**：`root_id`（可选，只取某主楼下的回复）、`limit`、`cursor`

**响应**

```json
{
  "data": {
    "items": [
      {
        "id": "5001",
        "user": { "id": "12", "nickname": "李四", "avatar_url": "..." },
        "content": "写得好",
        "status": "approved",
        "reply_count": 2,
        "created_at": "...",
        "replies": [
          {
            "id": "5002",
            "user": {...},
            "reply_to_user": { "id": "12", "nickname": "李四" },
            "content": "同意",
            "created_at": "..."
          }
        ]
      }
    ],
    "next_cursor": "...",
    "has_more": false
  }
}
```

**业务规则**

- 只返回 `approved` 的评论
- 评论者本人能看到自己的 `pending` / `rejected` 评论（带状态标记），其他人不可见
- 已删除评论返回占位：`{ "id": "...", "deleted": true, "content": "该评论已删除" }`
- 主楼带前 3 条回复，更多通过 `root_id` 单独拉取

---

#### POST /articles/{id}/comments 发表评论

**权限**：登录 ｜ **CSRF**：必需

**请求**

```json
{
  "content": "写得好",
  "parent_id": "0",
  "reply_to_user_id": "0"
}
```

**响应 201**

```json
{ "data": { "id": "5001", "status": "pending" } }
```

**业务规则**

- 文章必须 `published` 且 `allow_comment=true`，否则 40002
- `parent_id != 0` 时，父评论必须属于同一文章且已 `approved`
- `root_id` 由服务端推导：父评论的 `root_id`，或父评论自身 ID
- 审核开启 → `status=pending`；关闭 → `approved` 并立即 `comment_count+1`
- 通知：回复他人 → 通知被回复者；主楼 → 通知文章作者（非本人时）

**错误**：30001、40002、10001、10005

---

#### DELETE /comments/{id} 删除评论

**权限**：本人 / 管理员 ｜ **CSRF**：必需

**响应 204**

**业务规则**：主楼删除时级联其下所有回复，`comment_count` 按实际删除的 approved 条数递减。

---

#### POST /reports 举报

**权限**：登录 ｜ **CSRF**：必需

**请求**

```json
{
  "target_type": "article",
  "target_id": "1001",
  "reason": "spam",
  "detail": "广告内容"
}
```

| 字段 | 校验 |
|---|---|
| `target_type` | `article` / `comment` / `message` / `conversation` |
| `reason` | `spam` / `abuse` / `porn` / `illegal` / `other` |
| `detail` | ≤512，`reason=other` 时必填 |

**业务规则**：同一用户重复举报同一对象返回成功但不新增（唯一键去重）。

---

### 4.6 通知

#### GET /notifications

**权限**：登录 ｜ 分页 ｜ 参数：`unread_only`（bool）

---

#### GET /notifications/unread-count

**权限**：登录

**响应**：`{ "data": { "count": 5 } }`

---

#### PUT /notifications/{id}/read

**权限**：登录 ｜ **CSRF**：必需

**业务规则**：只能标记自己的通知，否则 10004。

---

#### PUT /notifications/read-all

**权限**：登录 ｜ **CSRF**：必需 ｜ 响应：`{ "data": { "updated": 12 } }`

---

### 4.7 媒体

#### POST /uploads/images 上传图片

**权限**：登录 ｜ **CSRF**：必需

**请求**：`multipart/form-data`

| 字段 | 说明 |
|---|---|
| `file` | 图片文件 |
| `purpose` | `article`（默认）/ `avatar` / `chat` |

**校验**

| 项 | 规则 |
|---|---|
| 格式 | jpg / png / webp / gif |
| 大小 | article/avatar ≤10MB，chat ≤5MB |
| 真实类型 | 读文件魔数判定，不信任 `Content-Type` |
| 尺寸 | `image.DecodeConfig` 校验，解码失败即拒绝 |

**响应 201**

```json
{
  "data": {
    "media_id": "2001",
    "url": "https://www.example.com/media/public/2026/10/abc.webp",
    "width": 1920,
    "height": 1080,
    "variants": { "w128": "...", "w256": "...", "w800": "..." }
  }
}
```

**业务规则**：`sha256` 命中已有记录时直接复用，不重复上传。

**错误**：10001、11005

---

#### GET /media/{id}/url 获取访问地址

**权限**：登录

**响应**：`{ "data": { "url": "https://.../signed?exp=...", "expires_at": "..." } }`

**业务规则**

- public bucket：直接返回永久 URL
- private bucket（聊天图片）：校验调用者是否为该消息所属会话成员，通过后生成 **1 小时**有效的预签名 URL
- 非成员返回 403

**错误**：10004、10003

---

### 4.8 聊天

#### GET /conversations 会话列表

**权限**：登录 ｜ 分页

**响应**

```json
{
  "data": {
    "items": [
      {
        "id": "7001",
        "type": "single",
        "peer": { "id": "12", "nickname": "李四", "avatar_url": "...", "online": false },
        "last_message": {
          "id": "9001",
          "preview": "在吗",
          "sender_id": "12",
          "created_at": "..."
        },
        "unread_count": 2,
        "is_muted": false
      }
    ],
    "next_cursor": null,
    "has_more": false
  }
}
```

**业务规则**：按 `last_message_at` 倒序，无消息的会话排最后。

---

#### POST /conversations 发起单聊

**权限**：登录 ｜ **CSRF**：必需

**请求**：`{ "peer_id": "12" }`

**响应 200**（已存在）或 **201**（新建）

```json
{ "data": { "id": "7001", "type": "single", "created": false } }
```

**业务规则**

- `peer_id` 等于自己 → 400
- 对方非 `active` → 50003
- 已存在则直接返回（幂等），`created=false`
- 每日新建上限 20 个 → 50004
- `conversation_key = min(uid):max(uid)` 保证唯一

**错误**：10001、50003、50004

---

#### GET /conversations/{id}/messages 历史消息

**权限**：成员 ｜ 分页

**请求参数**

| 参数 | 说明 |
|---|---|
| `before_id` | 向上翻历史（游标） |
| `after_seq` | **断线补齐**，返回 `seq > after_seq` 的消息，升序 |
| `limit` | 默认 30 |

**响应**

```json
{
  "data": {
    "items": [
      {
        "id": "9001",
        "seq": 128,
        "sender_id": "12",
        "type": "text",
        "content": "在吗",
        "status": "normal",
        "created_at": "..."
      }
    ],
    "has_more": true,
    "peer_last_read_seq": 128
  }
}
```

**业务规则**

- `before_id` 与 `after_seq` 互斥，同时传返回 10001
- 图片消息的 `content` 是 media_id（十进制字符串），前端调 `/media/{id}/url` 换取签名地址。**决策记录（2026-10-22）**：原文为 object_key，但接收方无法从 object_key 反查 media_id，签名地址的成员校验与换取流程都断在半路；改存 media_id 后链路自洽
- 非成员返回 50002

**消息已读状态的计算**

消息本身**没有 `is_read` 字段**，服务端也不逐条下发。响应只给 `peer_last_read_seq`，前端按 seq 比较：

| 观察者 | 判定公式 |
|---|---|
| 我发出的消息 | `message.seq <= peer_last_read_seq` → 对方已读 |
| 对方发出的消息 | `message.seq <= 本人已上报的 last_read_seq` |
| 自己发给自己 | 恒为已读，不产生回执 |

**不逐条返回 `is_read` 的理由**：一次 30 条会让响应体积翻倍，且该状态随对方操作实时变化，客户端缓存必然过期。比较 `seq` 是零成本且永远准确的。

**错误**：50001、50002、10001

---

#### POST /conversations/{id}/messages 发送消息

**权限**：成员 ｜ **CSRF**：必需

**请求**

```json
{
  "type": "text",
  "content": "在吗",
  "client_msg_id": "c-8f3a-001"
}
```

| 字段 | 校验 |
|---|---|
| `type` | `text` / `image` |
| `content` | text ≤2000 字符；image 为 media object_key |
| `client_msg_id` | 1–64，客户端生成，用于幂等 |

**响应 201**

```json
{ "data": { "id": "9002", "seq": 129, "created_at": "..." } }
```

**业务规则**

- 重复 `client_msg_id` 返回首次结果（200 而非 201），不报错
- 对方 `disabled` → 50003
- 发送方被禁用 → 403（中间件拦截）
- 落库后通过 WebSocket 推送 `message.new` 给对方
- 拉黑功能 V1.0 未实现

**错误**：50001、50002、50003、10001、10005

---

#### PUT /conversations/{id}/read 标记已读

**权限**：成员 ｜ **CSRF**：必需

**请求**

```json
{ "last_read_seq": 129 }
```

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `last_read_seq` | int | 是 | 客户端已读到的最大消息 `seq`，不是消息 `id` |

**响应 204**

**业务规则**

- 只能更新自己的已读位：`user_id` 取自登录态，**不接受请求体传入**
- 单调递增：新值小于当前值时不生效，仍返回 204（幂等，不报错）
- 越界截断：新值大于会话当前 `seq_counter` 时按 `seq_counter` 处理
- `unread_count` 归零
- 落库成功后通过 WS 推送 `message.read` 给对方（先落库后推送）
- 调用者不是会话成员返回 50002

**错误**：50001、50002、10001

---

#### PUT /conversations/{id}/mute 免打扰

**权限**：成员 ｜ **CSRF**：必需

**请求**：`{ "muted": true }`

**响应 204**

---

#### POST /ws/ticket 获取 WebSocket 票据

**权限**：登录 ｜ **CSRF**：必需

**响应 200**

```json
{ "data": { "ticket": "tk_8f3a...", "expires_in": 60 } }
```

**业务规则**：一次性使用，60 秒有效，用完即删。WebSocket 握手禁止在 URL 直接带 Token（会进日志）。

---

#### GET /ws WebSocket 连接

**权限**：凭 `ticket` query 参数

```
wss://www.example.com/ws?ticket=tk_8f3a...
```

**关闭码**

| code | 含义 |
|---|---|
| 4001 | 票据无效或过期 |
| 4003 | 账号被禁用 |
| 1000 | 正常关闭 |

**下行消息类型**

| type | data |
|---|---|
| `message.new` | `{ conversation_id, message }` |
| `message.read` | `{ conversation_id, user_id, last_read_seq }` |
| `conversation.update` | `{ conversation }` |
| `notification.new` | `{ notification }` |
| `system.kick` | `{ reason }` |

**V1.0 不定义上行消息**，发消息走 HTTP。

---

### 4.9 管理端

所有接口要求 `role=admin`，写操作需 CSRF。

#### 概览与用户

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/admin/overview` | 文章数、用户数、待审数、今日新增 |
| GET | `/admin/users` | 用户列表，支持 `q`、`role`、`status` 筛选 |
| PUT | `/admin/users/{id}/status` | 禁用/启用，body `{ "status": "disabled", "reason": "..." }` |
| PUT | `/admin/users/{id}/role` | 调整角色，body `{ "role": "author" }` |

**用户管理规则**

- 不能禁用自己 → 60001
- 不能撤销最后一个 admin → 60002
- 禁用后立即生效：吊销全部令牌、踢 WS、失效权限缓存
- 角色调整不吊销令牌（角色实时读取）

#### 文章审核

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/admin/articles` | 全站文章，支持 `status`、`author_id`、`q` |
| GET | `/admin/articles/{id}` | 文章详情（任意状态含已删除，含 `deleted_at` 与理由字段） |
| POST | `/admin/articles/{id}/approve` | 通过：`pending → published` |
| POST | `/admin/articles/{id}/reject` | 拒绝：`pending → rejected`，`reason` 必填 |
| POST | `/admin/articles/{id}/offline` | 下架：`published → offline`，`reason` 必填 |
| POST | `/admin/articles/{id}/relist` | 上架：`offline → published` |
| DELETE | `/admin/articles/{id}` | 删除，支持 `?reason=` |

**关键规则**

- 管理员**不可编辑他人正文**，只能下架或删除
- 拒绝、下架必须填理由，否则 60003
- 每次操作写 `operation_logs` 并通知作者

#### 评论管理

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/admin/comments` | 评论列表，支持 `status`、`article_id` |
| POST | `/admin/comments/{id}/approve` | 通过 |
| POST | `/admin/comments/{id}/reject` | 拒绝 |
| DELETE | `/admin/comments/{id}` | 删除 |

#### 举报处理

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/admin/reports` | 队列，支持 `status`、`target_type` |
| PUT | `/admin/reports/{id}` | 处理，body `{ "result": "accepted", "note": "...", "action": "offline" }` |

`action` 可选值：`none` / `offline`（下架文章）/ `delete_comment` / `disable_user`。

#### 分类与标签

| 方法 | 路径 |
|---|---|
| POST | `/admin/categories` |
| PUT | `/admin/categories/{id}` |
| DELETE | `/admin/categories/{id}`（其下文章 `category_id` 置 0） |
| POST | `/admin/tags` |
| PUT | `/admin/tags/{id}` |
| DELETE | `/admin/tags/{id}` |
| POST | `/admin/tags/merge`（`{ "from_id": "8", "to_id": "5" }`） |

#### 媒体库

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/admin/media` | 媒体列表，支持 `purpose`、`uploader_id` 筛选，按 ID 倒序分页 |

**业务规则**

- 返回媒体记录与访问 URL（URL 由存储驱动的 `public_base_url` 拼接，管理端不感知存储实现）
- `variants` 原样返回（JSON 对象，各尺寸 Key）

#### 系统与运维

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/admin/settings` | 全部配置 |
| PUT | `/admin/settings` | 批量更新 `{ "items": [{ "key": "review.comment", "value": "false" }] }` |
| GET | `/admin/logs` | 操作日志，支持 `operator_id`、`action`、`target_type`、时间范围 |
| GET | `/admin/index-failures` | 索引同步失败的文章列表 |
| POST | `/admin/index-rebuild` | 重建索引，body `{ "article_id": "1001" }` 或 `{ "all": true }` |
| POST | `/admin/counters/recalc` | 重算分类/标签计数 |

---

## 5. 与 OpenAPI 契约的对应

### 5.1 文件结构

```
api/
  openapi.yaml          # 契约唯一来源，手工维护
  oapi-codegen.yaml     # 生成配置
internal/dto/
  api.gen.go            # 生成产物，禁止手改
```

### 5.2 分组与 tag

`openapi.yaml` 使用与本文章节一致的 tag：`auth`、`user`、`article`、`taxonomy`、`interaction`、`notification`、`media`、`chat`、`admin`。

### 5.3 公共组件

契约中必须定义并复用以下 `components`：

| 组件 | 用途 |
|---|---|
| `schemas.Envelope` | 统一响应包裹 |
| `schemas.Error` | 错误响应 |
| `schemas.PageMeta` | `next_cursor` / `has_more` |
| `schemas.UserBrief` | 作者/用户简要信息 |
| `securitySchemes.cookieAuth` | `apiKey` in `cookie`，name `access_token` |
| `parameters.Limit` / `Cursor` | 分页参数 |

### 5.4 生成配置

```yaml
package: dto
output: internal/dto/api.gen.go
generate:
  models: true
  chi-server: false
  gin-server: false
```

**不生成 server 骨架**：路由由 Gin 手工注册，避免与 oapi-codegen 的路由约定冲突。

---

## 6. 待补与未覆盖

| 项 | 说明 | 处理 |
|---|---|---|
| 群聊接口 | V1.1 | 不设计 |
| 关注/粉丝接口 | V1.1 | 不设计 |
| 阅读历史接口 | V1.1 | 不设计 |
| RSS | V1.1，非 JSON 接口 | 独立路由 `/rss.xml`，不进本契约 |
| sitemap | 阶段 7 | 独立路由 `/sitemap.xml` |
| `/healthz` `/readyz` `/metrics` | 运维接口，无需文档化 | 不进 `/api/v1` |
| 消息撤回 | V1.1 | 不设计 |

---

## 7. 与上游文档的对应

| 上游 | 本文档 | 状态 |
|---|---|---|
| PRD 4.1 用户端功能 | 4.1–4.8 节 | ✅ 覆盖 V1.0 全部 P0/P1 |
| PRD 4.1.7 管理端 | 4.9 节 | ✅ 23 个接口 |
| PRD 2.2.1 状态码规则 | 1.4、4.3 详情接口 | ✅ 404/410 分工明确 |
| PRD 2.3 操作边界 | 各接口权限列 + 4.3 edit 接口 | ✅ 管理员不可编辑他人正文 |
| PRD 5.1 验收条目 | 各接口的业务规则 | 部分覆盖，剩余条目属行为而非接口 |
| TECH_DESIGN 13 章接口清单 | 本文档 3、4 章 | ✅ 路径与权限一致，补了 `/articles/{id}/edit` |
| DATABASE.md 表结构 | 请求/响应字段 | ✅ 字段名与 `snake_case` 对齐 |
