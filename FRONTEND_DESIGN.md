# 前端设计规范

| 项 | 内容 |
|---|---|
| 文档版本 | v1.1 |
| 上游依据 | PRD v0.6、TECH_DESIGN v1.5、DATABASE v1.2、API_SPEC v1.2 |
| 文档状态 | 待评审 |
| 更新时间 | 2026-10-21 |
| 适用范围 | V1.0 用户端（web）+ 管理端（admin） |

**变更记录**

| 版本 | 日期 | 变更 |
|---|---|---|
| v1.0 | 2026-10-21 | 初稿，14 章 |
| v1.2 | 2026-10-22 | **视觉主题重定（重要）**：用户经 6 轮方案评审选定"墨痕"编辑风（原方案 B），3.1 token 全面修订——主色 #2563EB→#111827（墨黑），新增衬线标题字体与 --color-accent 朱红单点强调，卡片改为无卡片发丝线分隔（圆角 12px→2px），admin 视觉与 web 统一为墨痕（Element Plus 仅作行为组件，视觉由 token 覆盖）；管理端页面清单补 article-detail（审核详情，F6）与 index-status（索引状态，TECH_DESIGN 10.6），合计 13 页；首页定稿为方案三：无封面图、每页 3 篇、右栏 sticky 吸附 |
| v1.1 | 2026-10-21 | 补齐四项：① 新增 5.9 聊天消息去重（seq 为唯一依据）；② 新增 2.4 admin 首屏鉴权（SPA 无 SSR，用 loading 掩盖时间窗）；③ 新增 7.6 文件上传进度与取消；④ 新增第 10 章前端性能度量，原 10–14 章顺延为 11–15；新增待确认 F8（性能上报端点缺口） |

**本文档的定位**

- 只定义**前端怎么组织、怎么实现、怎么约束**，不重复 PRD 的业务规则，不复制 API_SPEC 的接口定义
- 涉及具体接口时只给路径，字段与语义一律以 API_SPEC 为准
- 涉及业务规则时只引用 PRD 章节号，不重新表述

**四份上游文档的引用约定**

| 需要知道什么 | 查哪里 |
|---|---|
| 功能有哪些、谁能用 | PRD 4.1 / 2.2 |
| 接口路径、请求响应、错误码 | API_SPEC |
| 技术选型、部署路径、认证机制 | TECH_DESIGN 2 / 6 / 15 |
| 字段含义 | DATABASE |

---

## 1. 前端总体架构

### 1.1 两个应用

| 应用 | 目录 | 技术栈 | 面向 | 渲染 |
|---|---|---|---|---|
| **web**（用户端） | `apps/web` | Vue 3 + Nuxt 3 | 访客、读者、作者 | SSR + CSR 混合 |
| **admin**（管理端） | `apps/admin` | Vue 3 + Vite + Element Plus | 管理员 | 纯 CSR（SPA） |

**为什么是两个应用而不是一个**

- 受众与发布节奏不同：用户端内容页需要 SEO 与 CDN 缓存，管理端不需要
- 攻击面不同：管理端是高风险入口，代码里不应包含任何管理接口的调用逻辑
- 构建产物形态不同：web 需要 Node 运行时，admin 是纯静态文件

**为什么用户端不用 Element Plus**

Element Plus 是后台组件库：表格、表单、弹窗占主导，视觉偏"系统工具"，且全量引入体积大。用户端以内容阅读为主，需要的是排版、留白、图片，不是表格。

用户端采用 **UnoCSS 原子化样式 + 自研基础组件**（约 6 个），理由见 3.1。

### 1.2 开发与部署关系

**开发环境**：两个应用独立启动，各自代理到后端。

| 应用 | 开发端口 | API 代理目标 |
|---|---|---|
| web | 3000 | `http://localhost:8080` |
| admin | 5173 | `http://localhost:8080` |

**生产环境**：同域反代，路径分发（TECH_DESIGN 6.7.1）。

| 路径 | 目标 |
|---|---|
| `/` `/_nuxt/*` | web（Nuxt Node） |
| `/admin/*` | nginx 静态目录（admin 构建产物） |
| `/api/v1/*` | app（Gin） |
| `/ws` | app |

**这意味着前端代码里不出现跨域配置，也不出现 API 域名的环境变量。** 所有请求用相对路径 `/api/v1/...`，由部署形态保证可达。

### 1.3 依赖选型

| 用途 | 选择 | 理由 |
|---|---|---|
| 框架 | Vue 3.4+ / Nuxt 3.10+ | 既定选型 |
| 语言 | TypeScript（strict） | 契约类型来自 oapi-codegen，无 TS 则无收益 |
| 样式 | **UnoCSS**（web）/ Element Plus（admin） | web 需控制体积与视觉自由度；admin 需表单表格效率 |
| 状态 | **Pinia** | Vue 官方推荐，无替代必要 |
| 请求 | **ofetch** | Nuxt 内置；admin 单独安装同一库，保证两端行为一致 |
| 编辑器 | **Vditor** | TECH_DESIGN 风险表已指定，不自研 |
| 图标 | `@iconify/vue`（web）/ Element Plus 内置（admin） | 按需加载 |
| 日期 | `dayjs` | 体积小，两端共用 |

**明确不引入**：VeeValidate（表单量小，手写校验足够）、Axios（与 ofetch 功能重叠）、Tailwind（与 UnoCSS 二选一）、任何 UI 组件库到 web。

> **新增依赖说明**：UnoCSS 与 Pinia 不在 TECH_DESIGN 的选型表中。前者是 Vue 生态的原子化样式方案，构建产物为零运行时 CSS；后者是 Vue 3 官方状态方案。二者均不改变技术栈，属实现层选择。若需评审，见第 12 节 D1/D2。

### 1.4 Monorepo 结构

```
go-blog/
├── api/openapi.yaml          # 契约，前后端共用
├── internal/dto/api.gen.go   # 后端生成类型
├── apps/
│   ├── web/                  # Nuxt 3
│   ├── admin/                # Vite + Vue 3
│   └── shared/               # 两端共用：类型、工具、常量
└── pnpm-workspace.yaml
```

`apps/shared` 只放三类东西：由 `openapi.yaml` 生成的 TS 类型（`openapi-typescript`）、错误码常量、日期/格式化工具。**不放组件**——两端组件体系不同，强行共用会更乱。

---

## 2. 页面与路由清单

### 2.1 用户端（web）

**布局**

| 布局 | 用途 | 结构 |
|---|---|---|
| `default` | 大部分页面 | 顶部导航 + 内容区 + 页脚 |
| `full` | 登录、注册、验证 | 居中卡片，无导航 |
| `workspace` | 个人中心、创作、聊天 | 左侧边栏 + 主区域 |

**页面清单**

| 路由 | 页面 | 布局 | 渲染 | 权限 | 对应 PRD |
|---|---|---|---|---|---|
| `/` | 首页（最新/热门） | default | SSR | 公开 | 4.1.2 |
| `/category/[slug]` | 分类文章列表 | default | SSR | 公开 | 4.1.2 |
| `/tag/[slug]` | 标签文章列表 | default | SSR | 公开 | 4.1.2 |
| `/article/[slug]` | 文章详情 | default | SSR | 公开 | 4.1.2 |
| `/search` | 搜索结果 | default | SSR 首屏 + CSR | 公开 | 4.1.2 |
| `/user/[id]` | 作者公开主页 | default | SSR | 公开 | 4.1.1 |
| `/login` | 登录 | full | CSR | 仅访客 | 4.1.1 |
| `/register` | 注册 | full | CSR | 仅访客 | 4.1.1 |
| `/verify-email` | 邮箱验证结果 | full | CSR | 公开 | 3.5.1 |
| `/forgot-password` | 申请重置 | full | CSR | 仅访客 | 4.1.1 |
| `/reset-password` | 重置密码 | full | CSR | 公开 | 4.1.1 |
| `/me` | 个人中心首页 | workspace | CSR | 登录 | 4.1.1 |
| `/me/profile` | 编辑资料与头像 | workspace | CSR | 登录 | 4.1.1 |
| `/me/password` | 修改密码 | workspace | CSR | 登录 | 4.1.1 |
| `/me/articles` | 我的文章（全状态） | workspace | CSR | 作者 | 4.1.3 |
| `/me/favorites` | 我的收藏 | workspace | CSR | 登录 | 4.1.3 |
| `/me/notifications` | 站内通知 | workspace | CSR | 登录 | 4.1.5 |
| `/write` | 新建文章 | workspace | CSR | 作者 | 4.1.3 |
| `/write/[id]` | 编辑文章 | workspace | CSR | 作者本人 | 4.1.3 |
| `/messages` | 会话列表 | workspace | CSR | 登录 | 4.1.6 |
| `/messages/[id]` | 会话详情 | workspace | CSR | 会话成员 | 4.1.6 |
| `/404` | 页面不存在 | full | CSR | 公开 | 4.1.2 |
| `/410` | 内容已删除 | full | CSR | 公开 | 3.2.2 |
| `/403` | 无权限 | full | CSR | 登录 | 2.2 |

**页面数量：24 个。** 覆盖 PRD 4.1.1–4.1.6 的全部 V1.0 功能。

**未包含（V1.1）**：归档页、关注列表、RSS 页面、深色模式切换入口。

### 2.2 管理端（admin）

全部页面基于 `admin` 布局：左侧菜单 + 顶部面包屑 + 内容区。最小宽度 1280px。

| 路由 | 页面 | 权限 | 对应 PRD |
|---|---|---|---|
| `/login` | 管理员登录 | 公开 | 4.1.7 |
| `/` | 概览（4 个数字卡片） | admin | 4.1.7 |
| `/users` | 用户列表与管理 | admin | 4.1.7 |
| `/articles` | 文章列表 + 审核操作 | admin | 4.1.7 |
| `/articles/[id]` | 文章详情（审核上下文） | admin | 4.1.7 |
| `/comments` | 评论审核 | admin | 4.1.7 |
| `/reports` | 举报队列 | admin | 4.1.7 |
| `/categories` | 分类管理 | admin | 4.1.7 |
| `/tags` | 标签管理 | admin | 4.1.7 |
| `/media` | 媒体库 | admin | 4.1.7（P1） |
| `/settings` | 系统设置 | admin | 4.1.7 |
| `/logs` | 操作日志 | admin | 4.1.7 |
| `/index-status` | 索引异常与重建 | admin | TECH_DESIGN 10.6 |

**页面数量：13 个。**

**两个页面存在接口缺口，见 F6 / F7**

| 页面 | 依赖的接口 | API_SPEC 现状 |
|---|---|---|
| `/articles/[id]` | `GET /admin/articles/{id}` | ✅ 已定义（F6 已补，API_SPEC 4.9） |
| `/media` | `GET /admin/media` | **未定义**。现有只有上传 `POST /uploads/images` 与 `GET /media/{id}/url` |

这两个页面在接口补齐前无法完成。补接口需要走 API_SPEC 的变更流程（12.1），本文档不擅自定义。

**admin 的路径前缀**：开发时路由为 `/`，构建后部署在 `/admin/`。Nuxt/Vite 的 `base` 与 `router` base 需配置为 `/admin/`，请勿在路由表里手写 `/admin` 前缀。

### 2.3 路由守卫规则

| 条件 | 行为 |
|---|---|
| 未登录访问需登录页 | 跳 `/login?redirect=<当前路径>` |
| 已登录访问 `/login` `/register` | 跳 `/` |
| 角色不足（reader 访问 `/write`） | 跳 `/403`（web）或提示无权限（admin） |
| 文章详情返回 404 | 渲染 404 页，不跳转 |
| 文章详情返回 410 | 渲染 410 页，不跳转 |
| admin 非 admin 角色 | 跳 `/login` 并清空本地态 |

**守卫只做体验，不做安全**（PRD 2.5）。真正的权限由后端每个接口独立校验。

### 2.4 admin 首屏鉴权（SPA 特有）

**admin 是纯 CSR 应用，不存在服务端渲染鉴权。** 但首次加载存在一个时间窗：路由已挂载、用户态尚未拉取。若直接渲染界面，会出现"管理界面闪一下，然后跳转登录"。

处理方式：

| 阶段 | 行为 |
|---|---|
| 应用启动 | `auth.fetchMe()` 未完成前渲染全屏 loading，**不渲染任何业务组件** |
| 拉取成功且 `role === admin` | 放行到目标路由 |
| 拉取失败（401）或角色不足 | 跳 `/login`，清空本地态 |
| 后续路由跳转 | 用内存中的用户态判断，不重复请求 |

**不采用"返回 403 状态码"或"服务端重定向"**：admin 没有 SSR，服务端只返回 `index.html` 一个静态文件，不存在按需输出错误页的能力。这个时间窗只能用 loading 掩盖。

> 若将来把 admin 改成 Nuxt SSR，才需要重新设计首屏鉴权失败的表现。V1.0 不涉及。

---

## 3. UI 设计规范

### 3.1 设计 token

web 与 admin 各自定义，但**色彩与字号取同一套基准**，保证两端观感一致。

```
/* 色彩（墨痕主题，2026-10-22 视觉评审定稿） */
--color-primary: #111827;        /* 主色＝墨黑：主按钮、选中态、激活菜单 */
--color-accent: #DC2626;         /* 朱红：仅用于 Tab 选中下划线等单点强调 */
--color-text-1: #111827;         /* 标题（衬线） */
--color-text-2: #374151;         /* 正文 */
--color-text-3: #6B7280;         /* 辅助、时间、计数 */
--color-text-4: #9CA3AF;         /* 占位、禁用 */
--color-border: #E5E7EB;         /* 发丝线 */
--color-bg: #FFFFFF;
--color-bg-secondary: #F9FAFB;   /* 页面底色、代码块底、选中行 */
--color-success: #16A34A;
--color-warning: #D97706;
--color-danger: #DC2626;

/* 字体 */
--font-serif: "Noto Serif SC", "Source Han Serif SC", serif;  /* 标题、文章流、会话名 */
--font-sans: -apple-system, "PingFang SC", "Microsoft YaHei", sans-serif;  /* 正文 */
--font-mono: ui-monospace, Consolas, monospace;  /* 数字、代码 */

**为什么正文最大 760px**：中文阅读超过 40 字/行会显著降低可读性。760px 在 16px 字号下约 38–42 字/行。

### 3.2 布局规范

**用户端页头**：高度 64px，固定吸顶。左侧 Logo，中间导航（首页/分类/标签），右侧搜索入口 + 通知铃铛 + 头像菜单。未登录时右侧显示"登录/注册"。

**用户端页脚**：高度 120px，浅灰底，含备案号与 RSS 入口（RSS 为 V1.1，先留位置不实现）。

**文章详情页**：三栏在 1280px 以上出现（左侧目录 200px + 正文 760px + 右侧留白），1024–1280px 隐藏右侧，<1024px 目录折叠为浮层按钮。

**列表页**：单列卡片流，卡片横向结构（封面 200×120 + 右侧标题摘要）。移动端封面置顶。

**管理端**：左侧菜单 220px 固定，顶部面包屑 56px，内容区左右 padding 24px。表格行高 48px。

### 3.3 响应式策略

| 断点 | 宽度 | 策略 |
|---|---|---|
| `sm` | < 640px | 单列，导航折叠为抽屉，隐藏次要信息 |
| `md` | 640–1024px | 两列，导航保留 |
| `lg` | 1024–1280px | 完整布局，隐藏右侧边栏 |
| `xl` | ≥ 1280px | 三栏完整布局 |

**用户端移动优先**，从 `sm` 开始写，向上覆盖。
**管理端桌面优先**，不做移动适配，最小宽度 1280px，低于此宽度出现横向滚动条并提示"请使用桌面端访问"。

理由：管理端是单人低频操作，投入移动适配的收益远低于成本。

### 3.4 组件风格

| 组件 | 规范 |
|---|---|
| 按钮 | 高度 36px（默认）/ 32px（小）/ 44px（大）；圆角 8px；主按钮实底，次按钮描边，文字按钮无边框 |
| 输入框 | 高度 40px，圆角 8px，focus 时主色描边 2px，错误时 danger 描边 |
| 卡片 | 白底 + `--color-border` 1px 描边 + 圆角 12px，**不使用阴影**（扁平风格） |
| 标签 | 高度 24px，圆角 4px，浅灰底，文本 12px |
| 头像 | 圆形，尺寸 24/32/40/64/128 五档，统一用后端生成的 256px 源图 |
| 空状态 | 居中插画（或图标）+ 一行说明 + 可选操作按钮，垂直居中于容器 |
| 加载 | 列表用骨架屏（3–5 行），按钮用行内 spinner，禁止整页遮罩 |
| 分页 | 用户端一律"加载更多"按钮；管理端用页码（数据量可控） |

**web 端自研组件清单（仅 6 个）**：`AppButton`、`AppInput`、`AppModal`、`AppEmpty`、`AppSkeleton`、`AppAvatar`。其余用原生 HTML + UnoCSS。

不做完整组件库：V1.0 的界面元素就这几种，抽象越多维护越重。

---

## 4. 目录结构与组件规范

### 4.1 web（Nuxt 3）

```
apps/web/
├── nuxt.config.ts
├── app.vue
├── layouts/
│   ├── default.vue
│   ├── full.vue
│   └── workspace.vue
├── pages/
│   ├── index.vue
│   ├── login.vue
│   ├── register.vue
│   ├── article/[slug].vue
│   ├── category/[slug].vue
│   ├── tag/[slug].vue
│   ├── search.vue
│   ├── user/[id].vue
│   ├── me/
│   │   ├── index.vue
│   │   ├── profile.vue
│   │   ├── password.vue
│   │   ├── articles.vue
│   │   ├── favorites.vue
│   │   └── notifications.vue
│   ├── write/
│   │   ├── index.vue
│   │   └── [id].vue
│   └── messages/
│       ├── index.vue
│       └── [id].vue
├── components/
│   ├── base/            # AppButton 等 6 个通用组件
│   ├── layout/          # AppHeader、AppFooter、AppSidebar
│   ├── article/         # ArticleCard、ArticleMeta、ArticleToc
│   ├── comment/         # CommentList、CommentItem、CommentEditor
│   ├── editor/          # MarkdownEditor、EditorToolbar
│   └── chat/            # ConversationList、MessageBubble、MessageInput
├── composables/
│   ├── useApi.ts        # 请求封装（见 5）
│   ├── useAuth.ts       # 登录态与用户信息
│   ├── usePagination.ts # 游标分页
│   └── useToast.ts      # 轻提示
├── stores/
│   ├── auth.ts
│   └── ui.ts
├── middleware/
│   ├── auth.ts          # 需登录
│   └── role.ts          # 角色要求
└── assets/css/
    ├── tokens.css
    └── main.css
```

**分层职责**

| 层 | 职责 | 禁止 |
|---|---|---|
| `pages/` | 路由入口、数据获取、页面编排 | 写业务计算、直接调 `$fetch` 裸用 |
| `components/` | 纯展示与局部交互 | 发起跨组件的数据请求 |
| `composables/` | 可复用逻辑（请求、分页、鉴权） | 持有页面级状态 |
| `stores/` | 跨页面共享状态 | 放只有单个页面用到的数据 |
| `middleware/` | 路由级拦截 | 做接口鉴权 |

### 4.2 admin（Vite + Vue 3）

```
apps/admin/
├── vite.config.ts
├── src/
│   ├── main.ts
│   ├── App.vue
│   ├── router/index.ts
│   ├── layouts/AdminLayout.vue
│   ├── views/
│   │   ├── LoginView.vue
│   │   ├── OverviewView.vue
│   │   ├── UserListView.vue
│   │   ├── ArticleListView.vue
│   │   ├── ArticleDetailView.vue
│   │   ├── CommentListView.vue
│   │   ├── ReportListView.vue
│   │   ├── CategoryView.vue
│   │   ├── TagView.vue
│   │   ├── MediaView.vue
│   │   ├── SettingsView.vue
│   │   ├── LogView.vue
│   │   └── IndexStatusView.vue
│   ├── components/          # 业务组件：审核弹窗、举报处理面板等
│   ├── api/                 # 按模块拆分的接口函数
│   ├── stores/auth.ts
│   └── styles/
```

**admin 与 web 的差异**：admin 用 Element Plus 的表格/表单/弹窗，不做自研基础组件；数据获取用 `api/` 目录下的函数，不在 view 里直接写请求。

### 4.3 命名约定

| 类型 | 规范 | 示例 |
|---|---|---|
| 组件文件 | PascalCase | `ArticleCard.vue` |
| composable | camelCase + `use` 前缀 | `useAuth.ts` |
| store | camelCase | `auth.ts` |
| 路由 | kebab-case | `/forgot-password` |
| CSS 变量 | kebab-case | `--color-text-1` |
| 事件 | kebab-case | `@submit-success` |

---

## 4.9 图片资源约定（v1.1）

**前端只消费 API 返回的图片 URL，不拼路径、不假设存储实现。**

- 上传成功后，接口返回 `avatar_url` / `url`，直接赋值给 `<img src>`
- 文章正文里的图片同样是完整 URL，编辑器插入时使用接口返回值
- 禁止在前端拼 `/media/...` 或假设 CDN 域名：存储驱动改了（local → MinIO），前端零改动是验收标准
- 本地开发时 URL 指向应用自身（`http://127.0.0.1:8080/api/v1/media/...`），生产指向桶域名，两者对前端透明
- `<img>` 建议带 `width`/`height` 或固定容器比例，避免三档尺寸切换导致布局跳动

## 5. API 请求规范

### 5.1 统一封装

所有请求经 `useApi`（web）/ `api/client.ts`（admin），**业务代码不直接调用 `$fetch` 或 `fetch`**。

```
useApi<T>(path, options) → Promise<T>
```

职责：

1. 拼 `/api/v1` 前缀（相对路径，见 1.2）
2. 自动带 `X-CSRF-Token`（写请求）
3. 解包统一响应 `{ code, message, data, request_id }`
4. `code !== 0` 抛业务错误
5. 401 触发刷新并重试一次
6. 统一超时与错误提示

### 5.2 响应解包

| 后端返回 | 前端行为 |
|---|---|
| `code === 0` | 返回 `data` |
| `code !== 0` | 抛出 `ApiError{code, message, request_id}` |
| HTTP 204 | 返回 `null`（响应体为空，不解析） |

**业务代码拿到的永远是 `data` 本身**，不需要每次判断 `code`。

### 5.3 错误处理分层

| 层级 | 处理 |
|---|---|
| 封装层 | 401 刷新重试、网络错误转 `ApiError`、超时转 `ApiError` |
| 页面层 | 捕获后决定展示方式：整页错误 / 局部重试 / 轻提示 |
| 全局 | 未捕获错误上报 console + 上报（V1.0 仅 console） |

**不做的**：不做全局错误弹窗。同一个错误在不同场景的呈现方式不同，全局拦截会打断用户当前操作。

### 5.4 超时

| 场景 | 超时 |
|---|---|
| 普通请求 | 15s |
| 文件上传 | 60s |
| 搜索 | 5s（超时按降级处理，展示"搜索服务暂时不可用"） |

### 5.5 401 与并发刷新

这是最容易写出 bug 的地方，规则写死：

```
请求返回 401：
  1. 若当前无刷新在途 → 发起刷新（单例）
  2. 若有刷新在途 → 等待同一个 Promise
  3. 刷新成功 → 重放原请求一次（仅一次，不递归）
  4. 刷新失败 → 清空登录态，跳 /login?redirect=当前路径
  5. 重放后仍 401 → 不再重试，直接跳登录
```

**单例实现要点**：模块级变量持有 `refreshPromise`，无论多少并发请求，同一时刻只有一个刷新请求。这是 TECH_DESIGN 6.3.3 中"前端必须单例化刷新"的落地。

**不重试的情况**：刷新接口本身返回 401、登录接口返回 401——这两处失败就是失败，重试只会无限循环。

### 5.6 403 与 404/410

| 状态 | 前端行为 |
|---|---|
| 403 | 轻提示"无权限执行此操作"，不跳转 |
| 404（页面首屏） | 渲染 404 页 |
| 410 | 渲染 410 页（仅文章详情） |
| 404（列表中的单项） | 该行标记"已失效"，不整页报错 |

### 5.7 分页

两类分页严格区分（API_SPEC 1.6）：

| 接口 | 方式 | 前端实现 |
|---|---|---|
| 其余全部列表 | 游标 | `usePagination` 组合式函数，维护 `cursor` 与 `hasMore` |
| `GET /search` | 偏移 | 独立的 `useSearchPagination`，维护 `page`，禁用"加载更多"改用"下一页" |

**不允许**：把搜索接口塞进通用分页逻辑。搜索的 `page × page_size > 1000` 会返回 400，通用逻辑无法处理。

### 5.8 CSRF

写请求（POST/PUT/PATCH/DELETE）必须带 `X-CSRF-Token`，值从 Cookie `csrf_token` 读取（该 Cookie 非 HttpOnly，专为前端读取设计）。

请求封装层统一注入，业务代码不感知。登录、注册、刷新三个接口不需要该头。

### 5.9 WebSocket 与消息去重

**连接**：先 `POST /ws/ticket` 拿一次性票据，再连 `/ws?ticket=...`（API_SPEC 4.8）。不在 URL 直接带 Token。

**重连**：断线后指数退避（1s / 2s / 4s / 8s，上限 30s），每次重连都重新取票据。

#### 5.9.1 去重规则

重连后拉增量，用**本地已渲染的最大 seq** 作游标：

```
GET /conversations/{id}/messages?after_seq={本地最大 seq}
```

| 情况 | 处理 |
|---|---|
| 返回消息 `seq > 本地最大 seq` | 追加渲染 |
| 返回消息 `seq <= 本地最大 seq` | **丢弃**，不渲染 |
| 本地有未确认的乐观消息（无 seq） | 按 `client_msg_id` 与服务端返回匹配：匹配上则替换，未匹配且已超时则标记失败 |

**为什么用 seq 而不是 message.id 去重**

- 本地乐观消息在服务端确认前没有 id，只有 `client_msg_id`
- 网络超时重试会让本地出现多个临时条目，它们的 id 都是空的，无法用 id 判定
- `seq` 由 `conversations.seq_counter` 在数据库事务内原子分配，同一会话严格单调无空洞（TECH_DESIGN 11.6.2），重复投递的消息 seq 必然 ≤ 本地最大值

**结论：seq 是唯一可靠的服务端去重依据，`client_msg_id` 是本地乐观消息的关联键。**

#### 5.9.2 乐观更新流程

```
1. 发送前：生成 client_msg_id，插入本地列表，state = pending
2. POST 成功：拿到 { id, seq }，按 client_msg_id 找到本地条目，替换为 confirmed
3. POST 失败：state = failed，提供重试按钮（复用同一 client_msg_id，服务端幂等）
4. WS 收到 message.new：
     若 seq <= 本地最大 seq  → 丢弃（这是自己刚发出的回声）
     若 seq >  本地最大 seq  → 追加
```

**第 4 条不能省**。自己发的消息会被 WS 推回一次，不做判断就会在界面上出现两遍。

#### 5.9.3 已读状态的合并

`message.read` 事件更新对方的 `last_read_seq`。合并规则：

- 只接受比当前值大的 `last_read_seq`（单调）
- 本地已上报的已读位不受对方事件影响

渲染时按 `message.seq <= peer_last_read_seq` 判定"对方已读"（DATABASE 4.15.1）。

---

## 6. 状态管理

### 6.1 什么放 Pinia

| 状态 | 位置 | 理由 |
|---|---|---|
| 当前用户信息 | `auth` store | 全站导航、按钮显隐、权限判断都要用 |
| 登录态 | `auth` store（由用户信息推导） | 同上 |
| 全局提示队列 | `ui` store | 多个组件触发 toast |
| 文章详情数据 | **页面内** | 只有该页面用 |
| 列表数据 | **页面内** | 同上 |
| 草稿内容 | **页面内 + localStorage** | 不需要跨页面 |
| 聊天消息 | **页面内** | 消息量大会拖垮 store |

**原则：只有"三个以上页面共享"或"生命周期长于页面"的状态才进 store。** 其余一律组件内维护。

### 6.2 auth store

```
auth store 持有：
  user: { id, username, nickname, avatar_url, role } | null
  csrfToken: string | null

派生：
  isLoggedIn = user !== null
  isAuthor   = role >= author
  isAdmin    = role === admin

动作：
  fetchMe()   首次进入时拉取，SSR 与 CSR 都调用
  setUser()   登录成功后写入
  clear()     退出或刷新失败时清空
```

**不持久化到 localStorage**：用户信息来自 `/users/me`，刷新页面重新拉取。localStorage 里的用户信息会过期，且与后端权限不同步，是隐患。

### 6.3 服务端状态

不引入 TanStack Query 等缓存库。V1.0 页面间数据依赖很弱，页面级 `useAsyncData` 已足够。引入客户端缓存层会增加"什么时候失效"的心智负担，收益不足。

---

## 7. 表单与交互规范

### 7.1 四种状态必须都有

每个异步操作的界面必须覆盖四种状态，缺一个就是功能不完整：

| 状态 | 表现 |
|---|---|
| 加载中 | 骨架屏（列表）/ 按钮 spinner（提交）/ 局部 loading（区块） |
| 空数据 | 插画 + 说明 + 可选操作（如"去写第一篇"） |
| 请求失败 | 错误说明 + 重试按钮（不要只显示"加载失败"） |
| 成功 | 轻提示 / 状态变化 / 跳转，三者选一，不要都做 |

### 7.2 表单校验

**双端校验，前端只做体验，后端是准绳。**

| 时机 | 规则 |
|---|---|
| 失焦 | 校验该字段，错误显示在字段下方 |
| 提交 | 全量校验，第一个错误字段滚动到视口并聚焦 |
| 输入中 | 不校验（避免输入密码时不断报错） |

校验规则**从 API_SPEC 的字段约束抄写**，不自行发明更严格或更宽松的规则。

### 7.3 重复提交

| 场景 | 处理 |
|---|---|
| 普通提交 | 提交中禁用按钮 + 显示 spinner |
| 网络超时后重试 | 幂等接口（点赞、收藏）直接重发；非幂等接口（发文章）提示"请确认是否已创建" |
| 消息发送 | 用 `client_msg_id` 保证幂等，重试安全 |

### 7.4 操作确认

| 操作 | 是否确认 | 确认形式 |
|---|---|---|
| 删除文章/评论 | 是 | 弹窗，含二次说明（"删除后链接将失效"） |
| 退出登录 | 否 | 直接执行 |
| 取消点赞/收藏 | 否 | 直接执行，可撤销 |
| 管理端下架/拒绝 | 是 | 弹窗 + **必填理由** |
| 禁用用户 | 是 | 弹窗 + 必填理由 |

**理由必填的接口，前端也必须必填**，否则用户提交后收到 60003 才知道，体验很差。

### 7.5 成功反馈

| 场景 | 反馈 |
|---|---|
| 保存草稿 | 轻提示"已保存"，不跳转 |
| 发布成功 | 跳转到文章详情 |
| 删除成功 | 列表移除该行 + 轻提示 | 
| 设置修改 | 轻提示"已保存" |
| 登录成功 | 跳转 redirect 参数或首页 |

### 7.6 文件上传交互

**先澄清一个前提**：本项目的上传是**后端中转**（`POST /uploads/images`，`multipart/form-data`），不是浏览器直传 MinIO。文件先到 Go 服务，由后端校验魔数与尺寸后再写入对象存储（TECH_DESIGN 12.6）。

因此前端拿不到"对象存储上传进度"，只能拿"到后端的传输进度"。

#### 7.6.1 三项必须实现的能力

| 能力 | 实现要点 |
|---|---|
| **进度** | `XMLHttpRequest.upload.onprogress`。**`ofetch` 不暴露上传进度**，上传必须单独用 XHR 封装，不能复用 `useApi` |
| **取消** | `xhr.abort()`。取消后立即移除占位，不提示错误、不自动重试 |
| **重试** | 保留已选文件的 `File` 引用，重试时无需用户重新选择 |

#### 7.6.2 界面状态

| 状态 | 表现 |
|---|---|
| 上传中 | 进度条（0–100%）+ 取消按钮；禁用重复选择 |
| 成功 | 替换为图片预览，插入编辑器光标位置，清除进度 UI |
| 失败 | 显示失败原因 + 重试按钮；**保留文件**，不要求重选 |
| 取消 | 立即移除占位，静默 |

#### 7.6.3 前置校验

在 `input[type=file]` 的 `change` 事件里就校验，不要等传完：

| 项 | 规则（来自 API_SPEC 4.7） |
|---|---|
| 格式 | jpg / png / webp / gif |
| 大小 | 文章图与头像 ≤10MB；聊天图 ≤5MB |
| 数量 | 单次单文件 |

前端校验只是省一次无效上传，**服务端仍会重新校验魔数与解码**，前端结果不可信。

#### 7.6.4 明确不做

- **不做分片上传**：≤10MB 单文件直传足够，分片是给 GB 级文件准备的
- **不做断点续传**：同理
- **不做前端压缩**：压缩交给服务端（保质量可控、规则统一）。若将来上传体验成为瓶颈，再评估客户端预压缩

#### 7.6.5 临时对象清理

取消或失败的上传可能已在服务端产生对象。清理责任在**后端**（`reconcile:media` 对账任务处理无引用文件，TECH_DESIGN 10.4），前端不承担、也无法可靠承担。

---

## 8. 安全规范

### 8.1 Markdown 与 XSS

**渲染链路**：作者写 Markdown → 后端渲染为 HTML 并净化 → 前端 `v-html` 插入。

| 环节 | 责任方 | 前端要求 |
|---|---|---|
| Markdown → HTML | 后端（`goldmark`） | 前端不做渲染 |
| HTML 净化 | 后端 | 前端**不得**自己再做一次渲染或替换 |
| 插入 DOM | 前端 | 只允许在文章正文、评论内容两处使用 `v-html` |

**红线**：

1. **禁止**在前端用 `marked`/`markdown-it` 渲染用户内容后再插入
2. **禁止**把 `v-html` 用在任何非"后端已净化"的内容上
3. 编辑器预览用的是后端返回的 HTML（调预览接口），不是前端本地渲染——保证预览与线上完全一致

第 3 条很重要：如果前端自己渲染预览，用户看到的和最终发布的不一样（后端净化规则可能不同），会产生"预览好好的，发出来变了"的投诉。

### 8.2 路由权限

**三层，缺一不可**：

| 层 | 作用 | 失效后果 |
|---|---|---|
| 路由守卫 | 未登录跳转、角色不足拦截 | 用户看到不该看到的页面骨架 |
| 组件级显隐 | 无权按钮不渲染 | 用户点了才报错，体验差 |
| **后端接口鉴权** | 真正的安全边界 | 数据泄露 |

前两层只影响体验，**不能替代第三层**。任何"前端隐藏了所以安全"的想法都是错的。

### 8.3 敏感信息

| 项 | 规则 |
|---|---|
| Token | 不存 localStorage，不可被 JS 读取（HttpOnly Cookie） |
| csrf_token | 可读，但只用于请求头，不落日志 |
| 用户密码 | 只在提交时存在于内存，不做任何持久化或日志 |
| `request_id` | 可展示给用户（用于报障），不可用于业务判断 |
| 错误堆栈 | 生产环境不展示给用户，只上报 console |

### 8.4 Cookie 与 CSRF 协作

前端不管理 Cookie 内容，只做两件事：

1. 请求带 `credentials: 'include'`（同域下为默认，但显式声明）
2. 写请求从 `csrf_token` Cookie 读出值，放入 `X-CSRF-Token` 头

**不做的**：不手工设置 Token Cookie，不从响应体里读 Token。凭证流转完全由后端 Set-Cookie 控制。

### 8.5 外链与嵌入

- 外链一律 `rel="noopener noreferrer"` + `target="_blank"`
- 禁止 `v-html` 内容里的 `javascript:` 链接（后端净化应已处理，前端仍需在渲染后扫描一次作为兜底）

---

## 9. Nuxt 特有约定

### 9.1 SSR / CSR 边界

| 页面类型 | 渲染方式 | 原因 |
|---|---|---|
| 首页、分类、标签、文章详情、作者主页、搜索 | **SSR** | SEO 与首屏速度 |
| 登录、注册、验证、找回密码 | CSR | 无 SEO 价值，且涉及表单交互 |
| 个人中心、创作、聊天 | CSR | 登录态页面，SSR 无收益且易出错 |

**实现方式**：使用 `routeRules` 统一定义，不逐页写 `ssr: false`。

```ts
// nuxt.config.ts
routeRules: {
  '/me/**': { ssr: false },
  '/write/**': { ssr: false },
  '/messages/**': { ssr: false },
  '/login': { ssr: false },
  '/register': { ssr: false },
}
```

### 9.2 数据获取

| 场景 | 方法 |
|---|---|
| 首屏需要 SEO 的数据 | `useAsyncData` / `useFetch`（服务端执行，结果水合） |
| 用户交互触发的请求 | `$fetch`（客户端执行） |
| 需要 Cookie 的接口 | `useFetch` 需手动转发 Cookie（见 9.3） |

**不要在 `onMounted` 里发首屏数据请求**——那样 SSR 拿不到数据，等于白做。

### 9.3 Cookie 转发（关键）

SSR 时，请求由 Node 服务端发起，浏览器 Cookie 不会自动带上。必须手动转发：

```ts
const headers = useRequestHeaders(['cookie'])
const { data } = await useFetch('/api/v1/users/me', { headers })
```

**这是最容易漏的一步**。漏了的表现是：首屏显示"未登录"，客户端水合后又变成登录状态，页面闪烁。

**验收要求**：登录后直接访问文章详情页（不走前端跳转），首屏 HTML 里应包含用户头像，而不是"登录"按钮。这条在阶段 3 必须验证。

### 9.4 水合一致性

服务端与客户端渲染结果不一致会导致水合报错。三个高发点：

| 风险点 | 处理 |
|---|---|
| 时间格式化 | 统一用 `dayjs` 且固定时区（`Asia/Shanghai`），不在服务端用本地时区 |
| 随机数 / `Math.random` | 禁止出现在渲染逻辑里 |
| `window` / `localStorage` | 只在 `onMounted` 或 `import.meta.client` 分支内使用 |

### 9.5 SEO

| 项 | 实现 |
|---|---|
| 标题 | `useHead({ title })`，格式 `页面名 - 站点名` |
| 描述 | 文章详情取 `summary`，列表页取分类/标签描述 |
| canonical | 每个内容页设置，避免参数污染 |
| Open Graph | 文章详情设 `og:title` `og:description` `og:image`（封面） |
| 结构化数据 | 文章详情输出 `Article` JSON-LD |

`robots.txt` 与 `sitemap.xml` 由后端生成（TECH_DESIGN 15.4 已含路由规划），前端不重复实现。

### 9.6 缓存

| 对象 | 策略 |
|---|---|
| `/_nuxt/*` 静态资源 | 一年，immutable |
| 公开内容页 | 前端不设缓存，由 Nginx 与后端控制 |

**前端不做 ISR / 页面级缓存**。V1.0 内容是低频更新，SSR 直出已足够；引入 ISR 会让"发布后多久可见"变得不可预期。

---

## 10. 前端性能度量

**问题**：PRD 6.3 的性能目标全部基于后端压测口径（接口 P95/P99）。后端 200ms 返回、前端渲染阻塞 2 秒，用户依然觉得卡。**接口达标不等于体验达标。**

### 10.1 采集指标

| 指标 | 含义 | 目标 | 来源 |
|---|---|---|---|
| LCP | 最大内容绘制 | < 2.5s | Web Vitals |
| INP | 交互到下次绘制 | < 200ms | Web Vitals |
| CLS | 累计布局偏移 | < 0.1 | Web Vitals |
| TTFB | 首字节时间 | < 800ms | Navigation Timing |
| 接口耗时 | 按 path 分组的 P50/P95 | 对齐 PRD 6.3 | 请求封装埋点 |

> **用 INP 而非 FID**：FID 已于 2024 年被 INP 取代为 Core Web Vital，新项目没有理由再采 FID。

### 10.2 实现方式

1. 引入 `web-vitals`（约 2KB gzip），在 Nuxt 插件中注册
2. **仅生产环境采集**，开发与预览环境不上报
3. 采样率 100%：V1.0 流量小，全量采集成本可忽略
4. 通过 `navigator.sendBeacon` 上报，不阻塞页面卸载

```ts
// 示意，非最终实现
onLCP(send, { reportAllChanges: false })
onINP(send)
onCLS(send)
```

### 10.3 接口耗时埋点

在 `useApi` 封装内记录 `performance.now()` 差值，按 `path` 模板（`/articles/{slug}` 而非真实 slug）聚合。

**批量上报，不逐次上报**：逐次上报会把监控流量放大到与业务流量同量级，反而污染指标。

| 项 | 约定 |
|---|---|
| 聚合窗口 | 页面卸载时统一上报 |
| 分桶 | 按 `path + method` 分组，取 P50/P95 |
| 排除 | 静态资源、预检请求、被取消的请求 |

### 10.4 与验收的关系

| 场景 | 判定 |
|---|---|
| 阶段 7 压测 | 后端 P95/P99 **与** 前端 LCP/INP **同时**达标才算通过 |
| 任一不达标 | 视为未通过，不带病上线 |

**这是硬性条件**，不是"参考指标"。只交后端压测报告就宣布通过，等于没验证用户侧。

### 10.5 V1.0 的边界

- **不做性能看板**：数据先落日志，人工抽查
- **不做 Prometheus 指标化**：前端指标是直方图分布，塞进 Prometheus 需要额外的 bucket 设计，V1.0 不值得
- **不做告警**：没有稳定值班需求

> **新增接口依赖**：上报端点 `POST /api/v1/telemetry/web-vitals` 在 API_SPEC 中**未定义**，见 F8。

---

## 11. 前端测试与验收

### 10.1 分层

| 类型 | 范围 | 工具 | 时机 |
|---|---|---|---|
| 构建 | 两端 `build` 无 TS 错误 | `vue-tsc` | 每次提交 |
| 组件测试 | 6 个基础组件 + 编辑器关键交互 | Vitest + Vue Test Utils | PR 前 |
| 接口联调 | 全部页面走真实后端 | 手工 | 每阶段末 |
| 权限测试 | 三种角色逐个页面验证 | 手工 | 阶段 2、5 必做 |
| E2E | 关键用户流程 | Playwright（可选） | 阶段 7 |

**不做**：不为每个页面写快照测试。UI 会频繁调整，快照维护成本高于收益。

### 10.2 必测的权限场景

| 场景 | 预期 |
|---|---|
| 未登录访问 `/me` | 跳登录 |
| reader 访问 `/write` | 跳 403 |
| 作者访问他人文章编辑页 | 跳 403 |
| 管理员在用户端访问 `/write` | 允许（角色层级） |
| 非 admin 登录 admin | 拒绝并清空态 |
| 被禁用用户刷新页面 | 401 → 跳登录 |

### 10.3 关键用户流程（手工走通）

1. 注册 → 收邮件 → 验证 → 登录 → 发文章 → 前台可见
2. 编辑已发布文章 → 前台 10 秒内更新
3. 评论 → 管理员审核 → 前台可见
4. 点赞 → 刷新页面状态保持
5. 双开两个账号 → 单聊收发 → 断网 30 秒恢复 → 消息补齐
6. 管理员禁用用户 → 该用户下一次操作即失效

### 10.4 联调检查项

| 项 | 判定 |
|---|---|
| 所有接口用相对路径 | 无硬编码域名 |
| 无 `console.log` 残留 | 生产构建检查 |
| 401 并发刷新只发一次 | Network 面板确认 |
| 首屏 HTML 含真实内容 | `curl` 抓取验证 |
| 移动端 375px 无横向滚动 | 手工 |

---

## 12. 前后端协作规范

### 12.1 契约的权威性

**API_SPEC 是唯一契约。** 前端不根据后端实际返回"猜"字段，后端不根据前端需要"临时加"字段。

变更顺序（TECH_DESIGN 2.1.3）：

```
API_SPEC.md → api/openapi.yaml → oapi-codegen 生成 Go 类型
                              → openapi-typescript 生成 TS 类型
           → 后端 handler 编译报错驱动修改
           → 前端类型报错驱动修改
```

**跳过任何一步都会导致文档与代码漂移**，这是本项目最核心的协作纪律。

### 11.2 字段命名

| 层 | 规范 |
|---|---|
| JSON 字段 | `snake_case`（与 DATABASE 一致） |
| TS 类型属性 | `snake_case`，**不做驼峰转换** |
| 组件内部变量 | `camelCase` |

**为什么前端也用 snake_case**：转换层会带来"类型定义一套、运行时另一套"的隐患，且生成工具默认不转换。多打几个下划线的成本，远低于排查转换 bug 的成本。

### 11.3 错误码处理

| 错误码 | 前端行为 |
|---|---|
| 10001 | 显示 `data.errors` 中的字段级错误（如有），否则轻提示 |
| 10002 | 走 401 流程 |
| 10003 | 轻提示 |
| 10004 | 404 页或行内失效标记 |
| 10005 | 轻提示 + 倒计时 | 
| 20004 20005 | 跳登录并提示原因 |
| 30004（乐观锁） | 提示"内容已被修改" + 提供刷新按钮，**不自动覆盖** |
| 50004 | 轻提示"发起过于频繁" |

**未在表中列出的错误码**：展示后端返回的 `message` 原文。前端不自行编造文案。

### 11.4 联调节奏

| 时机 | 动作 |
|---|---|
| 阶段开始 | 后端先提供 mock 响应或真实接口，前端按契约并行开发 |
| 阶段中期 | 前后端联调，只改实现不改契约 |
| 阶段末 | 契约冻结，本阶段不再改接口 |
| 需要改契约 | 走 12.1 的流程，并在 PRD/API_SPEC 变更记录中登记 |

---

## 13. 待确认决策

| 编号 | 决策点 | 选项 | 建议 | 影响 |
|---|---|---|---|---|
| **F1** | web 端是否引入 UnoCSS | A. 引入<br>B. 纯手写 CSS | **A** | 引入可显著提升开发速度，但新增一个构建依赖 |
| **F2** | 是否引入 Pinia | A. 引入<br>B. 用 provide/inject | **A** | 官方方案，B 方案在跨布局场景下很别扭 |
| **F3** | 编辑器选型 | Vditor / TipTap | **Vditor** | TECH_DESIGN 已建议 Vditor，此处确认 |
| **F4** | 管理端是否做移动适配 | A. 不做（最小 1280px）<br>B. 做基础适配 | **A** | 做适配会让 13 个页面的工作量翻倍 |
| **F5** | 是否引入 Playwright E2E | A. 阶段 7 再说<br>B. 现在就建 | **A** | V1.0 阶段末再评估，现在建维护成本高 |
| **F6** | `GET /admin/articles/{id}` 缺口 | A. 补该接口<br>B. 用列表数据 + 详情弹窗替代 | **A** | admin 审核详情页需要完整数据（含正文、审核历史）；PRD 2.2.1 还要求管理员可查已删除文章的删除人/时间，列表接口给不了 |
| **F7** | `GET /admin/media` 缺口 | A. 补该接口<br>B. V1.0 砍掉媒体库页面 | **A** | 媒体库是 PRD 4.1.7 的 P1 项；若砍掉，图片删除只能靠命令行，风险高 |
| **F8** | 前端性能数据上报端点 | A. 新增 `POST /telemetry/web-vitals`<br>B. 不采集，只靠后端压测 | **A** | 选 B 则 10.4 的"前后端同时达标"无法执行，退化为只看后端数字 |

**F6 / F7 / F8 都是 API_SPEC 的缺口，不是前端设计问题。** 补齐需要修改 API_SPEC 与 `openapi.yaml`，本文档不代为决定。

若 F8 选 B（不采集），需同步放宽 PRD 6.3 的验收表述——否则阶段 7 会出现"要求前端达标但没有任何采集手段"的死结。

---

## 14. 下一阶段最小任务清单

搭骨架时只做这些，不做多余抽象：

| # | 任务 | 产出 |
|---|---|---|
| 1 | 初始化 pnpm workspace | `pnpm-workspace.yaml`、根 `package.json` |
| 2 | 创建 `apps/shared`，接入 `openapi-typescript` | 从 `openapi.yaml` 生成 TS 类型 |
| 3 | 初始化 Nuxt 3 项目 | 可 `dev` 启动，含 TS strict |
| 4 | 初始化 Vite + Vue 3 + Element Plus | 可 `dev` 启动，Element Plus 按需引入 |
| 5 | 落地设计 token | `tokens.css`（web）、Element Plus 主题变量覆盖（admin） |
| 6 | 写 6 个基础组件 | `AppButton` `AppInput` `AppModal` `AppEmpty` `AppSkeleton` `AppAvatar` |
| 7 | 写 `useApi` 封装 | 含 CSRF 注入、401 单例刷新 |
| 8 | 写 `auth` store | 含 `fetchMe` |
| 9 | 写路由守卫 | 登录校验 + 角色校验 |
| 10 | 三个布局骨架 | `default` `full` `workspace` |
| 11 | 登录页 + 注册页 | 打通真实接口 |
| 12 | 404 / 410 页 | 静态页 |
| 13 | 配置代理与构建 base | 开发代理到 8080；admin 构建 base 为 `/admin/` |

做完这 13 项，阶段 2 就有地方落代码。

**不做的事**：不建组件库文档站、不配置 Storybook、不做 i18n、不做主题切换（深色模式 V1.1）。

---

## 15. 与上游文档的对应

| 上游要求 | 本文档 | 状态 |
|---|---|---|
| PRD 4.1.1–4.1.6 用户端功能 | 2.1（24 页） | ✅ 全覆盖 |
| PRD 4.1.7 管理端功能 | 2.2（13 页） | ✅ 全覆盖 |
| PRD 2.5 前端只做体验不做安全 | 8.2 | ✅ |
| PRD 6.4 SEO 要求 | 9.5 | ✅ |
| PRD 3.5.1 邮箱验证流程 | 2.1 `/verify-email` | ✅ |
| TECH_DESIGN 6.7.1 同域反代 | 1.2 | ✅ |
| TECH_DESIGN 6.3.3 并发刷新 | 5.5 | ✅ |
| TECH_DESIGN 2.1 spec-first | 12.1 | ✅ |
| API_SPEC 1.6 两类分页 | 5.7 | ✅ |
| API_SPEC 4.8 聊天已读 | 5.9.3 | ✅ |
| PRD 6.3 性能目标 | 第 10 章 | ✅ 补前端侧度量 |

**冲突检查结果**：四份上游文档之间、以及本文档与之，**未发现语义冲突**。但有四处需要注意：

| # | 情况 | 性质 | 处理 |
|---|---|---|---|
| 1 | TECH_DESIGN 只定了管理端用 Element Plus，未指定 web 的 UI 方案 | 补充 | 本文档选 UnoCSS + 6 个自研组件，见 F1 |
| 2 | TECH_DESIGN 选型表未列 Pinia | 补充 | 本文档引入，属实现层，见 F2 |
| 3 | **API_SPEC 缺 `GET /admin/articles/{id}`** | **缺口** | admin 文章详情页做不了，见 F6 |
| 4 | **API_SPEC 缺 `GET /admin/media`** | **缺口** | admin 媒体库页做不了，见 F7 |
| 5 | **API_SPEC 缺 `POST /telemetry/web-vitals`** | **缺口** | 性能数据无处上报，见 F8 |

**第 3、4、5 项是真实缺口**，不是设计冲突。前两项会让管理端两个页面无法交付，第三项会让第 10 章的验收无法执行。建议在对应阶段开工前补齐 API_SPEC——补接口不属于前端设计范围，本文档只标注不代决。
