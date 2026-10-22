# -*- coding: utf-8 -*-
"""视觉稿页面生成器：从共享模板批量产出 prototypes/ 下的静态页。
只属于开发预览流程，正式实现为 Nuxt 3 工程。重复运行安全（覆盖式生成）。"""
import os

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
OUT = os.path.join(ROOT, "prototypes")

# ---- 样例数据（与后端真实接口形状对齐，正式版由 API 渲染）----
ARTICLES = [
    ("go-1-26-generics", "Go 1.26 泛型性能实测：切片排序快了 40%？", "后端", "阿张", "2 小时前",
     "从编译器内联策略与逃逸分析两个角度，拆解新版泛型特化带来的性能变化，附完整 benchmark 与复现命令。", "1.2k", 46),
    ("chat-seq-not-redis", "单聊消息序号：为什么不用 Redis INCR", "架构", "阿张", "昨天",
     "用 LAST_INSERT_ID(expr) 在行锁内原子分配会话内 seq，一条 SQL 解决并发乱序，附四种方案的推演过程。", "3.4k", 182),
    ("mysql-index-review", "MySQL 索引设计：从 EXPLAIN 到上线复盘", "数据库", "阿李", "3 天前",
     "一张 500 万行的消息表怎么选索引，哪些看起来有用的索引实际是负担，上线后 QPS 曲线说明了什么。", "2.1k", 97),
    ("spec-first-practice", "先写契约再写代码：一次 spec-first 的完整实践", "工程", "阿王", "上周",
     "openapi.yaml 先行让前后端可以真正并行，也让每个接口在实现前就被审过一遍。", "986", 61),
    ("docker-compose-checklist", "Docker Compose 部署完整清单", "部署", "阿王", "两周前",
     "从裸 docker run 到 compose 编排：MySQL、Redis、Meilisearch 与应用的健康检查全配齐。", "1.5k", 73),
    ("e2e-not-brittle", "E2E 测试怎么做才不脆：从 curl 脚本到固定装置", "测试", "阿李", "三周前",
     "真实环境验证比 mock 更可信，但要让测试数据可清理、可重复，这些固定装置模式值得抄。", "823", 39),
    ("double-cookie-auth", "双 Cookie 认证的取舍记录", "后端", "阿张", "上个月",
     "访问令牌放 JS 可读 Cookie、刷新令牌放 HttpOnly， CSRF 与 XSS 的攻防推演全记录。", "4.2k", 233),
    ("meilisearch-sync", "Meilisearch 同步对账实践", "检索", "阿李", "上个月",
     "异步同步丢了怎么办：对账任务 + 索引失败队列，让检索永远追得上数据库。", "1.1k", 52),
]
HOT = ["WebSocket 心跳与断线补齐的正确姿势", "双 Cookie 认证的取舍记录", "Meilisearch 同步对账实践", "会话唯一键与幂等设计"]

NAV = [("index.html", "首页"), ("category.html", "分类"), ("tag.html", "标签"), ("messages.html", "消息"), ("#", "关于")]


def w(path, html):
    full = os.path.join(OUT, path)
    os.makedirs(os.path.dirname(full) or OUT, exist_ok=True)
    with open(full, "w", encoding="utf-8", newline="\n") as f:
        f.write(html)
    print("gen", path)


def topbar(active):
    links = "".join(
        f'<a{" class=\"active\"" if label == active else ""} href="{href}">{label}</a>'
        for href, label in NAV
    )
    return f'''<header class="topbar">
  <span class="logo">墨痕</span>
  <nav>{links}</nav>
  <div class="right">
    <span style="border-bottom:0.5px solid #D3D1C7;padding-bottom:2px;">搜索</span>
    <span>阿李</span><div class="avatar-dot">李</div>
  </div>
</header>'''


def footer():
    return '<footer class="site-footer">© 2026 墨痕 · 备案号占位</footer>'


def feed_item(a):
    slug, title, cat, author, time, summary, views, likes = a
    return f'''<article class="feed-item">
  <p class="feed-meta">{cat} · {time} · {author}</p>
  <p class="feed-title"><a href="/article.html">{title}</a></p>
  <p class="feed-summary">{summary}</p>
  <p style="margin:8px 0 0;font-size:12px;color:var(--ink-4);">阅读 {views} · 赞 {likes} · 评论 8</p>
</article>'''


def rail():
    hot = "".join(f'<p style="margin:0 0 14px;font-family:var(--serif);font-size:14px;color:#374151;line-height:1.6;">{i}、{t}</p>'
                  for i, t in enumerate(HOT, 1))
    return f'''<aside class="rail" style="border-left:0.5px solid var(--hairline);padding-left:32px;position:sticky;top:16px;align-self:start;">
  <h4 style="font-size:12px;color:var(--ink);font-weight:500;letter-spacing:2px;margin:0 0 14px;">热门</h4>
  {hot}
  <h4 style="font-size:12px;color:var(--ink);font-weight:500;letter-spacing:2px;margin:26px 0 14px;">标签</h4>
  <p style="margin:0;font-size:12px;color:var(--ink-3);line-height:2.2;">Go · MySQL · WebSocket · 架构 · 部署 · 测试 · Redis</p>
  <h4 style="font-size:12px;color:var(--ink);font-weight:500;letter-spacing:2px;margin:26px 0 14px;">订阅</h4>
  <div style="display:flex;gap:6px;">
    <input placeholder="邮箱地址" style="flex:1;border:0.5px solid #D3D1C7;border-radius:2px;height:34px;padding:0 10px;font-size:12px;color:var(--ink-4);">
    <button class="btn-dark">订阅</button>
  </div>
  <p style="margin:20px 0 0;font-size:12px;color:var(--ink-4);line-height:1.9;">文章 128 篇 · 读者 56 人<br>今日新增 3 篇</p>
</aside>'''


def hero():
    return '''<section style="padding:42px 40px 34px;text-align:center;border-bottom:0.5px solid var(--hairline);">
  <p style="margin:0 0 10px;font-family:var(--serif);font-size:32px;color:var(--ink);line-height:1.5;">写作是对思考的最后一次编译。</p>
  <p style="margin:0;font-size:13px;color:var(--ink-4);">记录 Go、数据库与系统设计的实践与复盘 · 每周三更新</p>
</section>'''


# ---- 1. 首页（方案三：每页 3 篇 + 右栏吸附）----
def gen_index():
    items = "".join(feed_item(a) for a in ARTICLES[:3])
    body = f'''{hero()}
<div style="display:grid;grid-template-columns:minmax(0,1fr) 260px;gap:44px;padding:28px 40px 40px;align-items:start;">
  <main>
    <div style="display:flex;gap:8px;margin-bottom:6px;">
      <span style="font-size:12px;color:var(--ink);font-weight:500;border-bottom:2px solid var(--accent);padding-bottom:4px;">最新</span>
      <span style="font-size:12px;color:var(--ink-4);padding-bottom:4px;">热门</span>
    </div>
    {items}
    <div style="margin-top:26px;display:flex;justify-content:space-between;align-items:center;">
      <a class="btn-ghost" href="#">加载更多 ↓</a>
      <span style="font-size:11px;color:#D1D5DB;">每页 3 篇 · 右栏吸附跟随</span>
    </div>
  </main>
  {rail()}
</div>'''
    w("index.html", page(title="墨痕 · 首页", active="首页", body=body))


# ---- 2. 列表页（分类 / 标签 / 搜索共用版式）----
def list_page(fname, title, crumb, sub, active):
    items = "".join(feed_item(a) for a in ARTICLES[:5])
    body = f'''<div style="padding:32px 40px 8px;border-bottom:0.5px solid var(--hairline);">
  <h1 style="font-family:var(--serif);font-size:26px;font-weight:500;color:var(--ink);margin:0 0 6px;">{crumb}</h1>
  <p style="margin:0 0 20px;font-size:13px;color:var(--ink-4);">{sub}</p>
</div>
<div style="max-width:760px;margin:0 auto;padding:12px 40px 40px;">
  {items}
  <p style="margin:26px 0 0;text-align:center;"><a class="btn-ghost" href="#">加载更多 ↓</a></p>
</div>'''
    w(fname, page(title=title, active=active, body=body))


# ---- 3. 作者主页 ----
def gen_user():
    items = "".join(feed_item(a) for a in ARTICLES[:4])
    body = f'''<div class="container">
  <div class="user-head">
    <div class="ava-big">张</div>
    <div style="min-width:0;">
      <h1>阿张</h1>
      <p class="bio">后端工程师，写 Go 与系统设计。相信复杂问题都有简单的边界划分。</p>
      <p class="meta">32 篇文章 · 获赞 1,082 · 注册于 2026-08</p>
    </div>
    <div class="ops"><span class="btn-ghost-sm" style="color:var(--ink);border-color:var(--ink);">+ 关注（V1.1）</span></div>
  </div>
  <div style="max-width:760px;padding:12px 0 40px;">
    {items}
    <p style="margin:26px 0 0;text-align:center;"><a class="btn-ghost" href="#">加载更多 ↓</a></p>
  </div>
</div>'''
    w("user.html", page(title="阿张 · 墨痕", active="", body=body))


# ---- 4. 认证页 ----
def auth_page(fname, title, h1, fields, alt, extra="", btn=None):
    rows = "".join(f'<div class="field"><label>{lbl}</label><input type="{typ}" placeholder="{ph}">{hint}</div>'
                   for lbl, typ, ph, hint in fields)
    body = f'''<div class="auth-page">
  <div class="auth-card">
    <p class="logo">墨痕</p>
    <p class="sub">写作是对思考的最后一次编译。</p>
    <h1>{h1}</h1>
    {rows}
    {extra}
    <button class="btn-wide">{btn or h1}</button>
    <p class="alt">{alt}</p>
  </div>
</div>'''
    w(fname, f'''<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{title} · 墨痕</title>
<link rel="stylesheet" href="/assets/site.css">
<link rel="stylesheet" href="/assets/components.css">
</head>
<body>
{body}
</body>
</html>''')


# ---- 5. 工作台页 ----
WS_NAV = [("me.html", "个人中心"), ("me-profile.html", "编辑资料"), ("me-password.html", "修改密码"),
          ("me-articles.html", "我的文章"), ("me-favorites.html", "我的收藏"), ("me-notifications.html", "站内通知"),
          ("write.html", "写文章")]


def ws_page(fname, title, active, content):
    nav = "".join(f'<a{" class=\"on\"" if n == active else ""} href="{h}">{n}</a>' for h, n in WS_NAV)
    body = f'''{topbar(active)}
<div class="ws-layout">
  <aside class="ws-menu">
    <div class="who"><div class="avatar-dot">李</div><p class="n">阿李</p><p class="r">作者 · 注册于 2026-08</p></div>
    <nav>{nav}</nav>
  </aside>
  <div class="ws-content">{content}</div>
</div>
{footer()}'''
    w(fname, page(title=f"{title} · 墨痕", active="消息" if "notif" in fname else "", body=body))


def gen_me_pages():
    # 个人中心首页
    ws_page("me.html", "个人中心", "个人中心", '''
<h1>下午好，阿李</h1>
<div style="display:grid;grid-template-columns:repeat(3,1fr);border:0.5px solid var(--hairline);margin-bottom:24px;">
  <div style="padding:16px;border-right:0.5px solid var(--hairline);"><p style="margin:0 0 6px;font-size:12px;color:var(--ink-4);">我的文章</p><p class="mono" style="margin:0;font-size:24px;font-weight:500;color:var(--ink);">8</p></div>
  <div style="padding:16px;border-right:0.5px solid var(--hairline);"><p style="margin:0 0 6px;font-size:12px;color:var(--ink-4);">收到赞</p><p class="mono" style="margin:0;font-size:24px;font-weight:500;color:var(--ink);">156</p></div>
  <div style="padding:16px;"><p style="margin:0 0 6px;font-size:12px;color:var(--ink-4);">未读通知</p><p class="mono" style="margin:0;font-size:24px;font-weight:500;color:var(--accent);">3</p></div>
</div>
<h1 style="font-size:16px;">快捷入口</h1>
<div style="display:flex;gap:10px;flex-wrap:wrap;">
  <a class="btn-dark" style="text-decoration:none;height:38px;padding:0 20px;" href="/write.html">写新文章</a>
  <a class="btn-ghost-sm" href="/me-articles.html">管理我的文章</a>
  <a class="btn-ghost-sm" href="/me-notifications.html">查看通知</a>
  <a class="btn-ghost-sm" href="/messages.html">打开消息</a>
</div>
<p style="margin-top:28px;font-size:12px;color:var(--ink-4);">提示：文章发布流程为 草稿 → 提交审核 → 发布，见 PRD 3.2 状态机。</p>''')

    # 编辑资料
    ws_page("me-profile.html", "编辑资料", "编辑资料", '''
<h1>编辑资料</h1>
<div style="display:flex;gap:40px;align-items:flex-start;">
  <div style="text-align:center;">
    <div class="avatar-dot" style="width:96px;height:96px;font-size:34px;margin:0 auto 12px;">李</div>
    <span class="btn-ghost-sm">更换头像</span>
    <p style="font-size:11px;color:var(--ink-4);margin:8px 0 0;">jpg/png ≤10MB</p>
  </div>
  <div style="flex:1;min-width:0;max-width:420px;">
    <div class="field"><label>用户名（不可改）</label><input type="text" value="li152412" disabled style="color:var(--ink-4);"></div>
    <div class="field"><label>昵称</label><input type="text" value="阿李"></div>
    <div class="field"><label>个人简介</label><textarea placeholder="一句话介绍自己">数据库方向，偶尔写写测试。</textarea></div>
    <div class="field"><label>邮箱（已验证）</label><input type="email" value="li***@example.com" disabled style="color:var(--ink-4);"></div>
    <button class="btn-dark" style="height:40px;padding:0 28px;">保存修改</button>
  </div>
</div>''')

    # 修改密码
    ws_page("me-password.html", "修改密码", "修改密码", '''
<h1>修改密码</h1>
<div style="max-width:420px;">
  <div class="field"><label>当前密码</label><input type="password" placeholder="请输入当前密码"></div>
  <div class="field"><label>新密码</label><input type="password" placeholder="至少 8 位，含字母与数字"></div>
  <div class="field"><label>确认新密码</label><input type="password" placeholder="再输入一次"></div>
  <button class="btn-dark" style="height:40px;padding:0 28px;">更新密码</button>
  <p class="hint" style="margin-top:14px;">更新后其他设备将被要求重新登录。</p>
</div>''')

    # 我的文章（全状态表格）
    rows = ""
    for slug, title, cat, author, time, summary, views, likes in ARTICLES[:5]:
        status = '<span class="chip chip-green">已发布</span>'
        ops = '<span class="link-u">编辑</span><span class="link-u">下架</span>'
        if slug == "meilisearch-sync":
            status, ops = '<span class="chip chip-amber">待审核</span>', '<span class="link-dim">审核中</span>'
        if slug == "spec-first-practice":
            status, ops = '<span class="chip chip-draft">草稿</span>', '<span class="link-u">编辑</span><span class="link-u">提交审核</span>'
        rows += f'''<div class="trow cols">
  <div class="cell-title">{title}</div>
  <div class="cell">{cat}</div><div>{status}</div>
  <div class="cell num-sub">{time}</div>
  <div class="ops">{ops}<span class="link-dim">删除</span></div>
</div>'''
    ws_page("me-articles.html", "我的文章", "我的文章", f'''
<div style="display:flex;align-items:center;margin-bottom:18px;">
  <h1 style="margin:0;">我的文章</h1>
  <a class="btn-dark" style="margin-left:auto;height:36px;padding:0 20px;text-decoration:none;" href="/write.html">+ 写新文章</a>
</div>
<div style="display:flex;gap:8px;margin-bottom:14px;">
  <span style="font-size:12px;padding:5px 14px;background:var(--ink);color:#fff;border-radius:2px;">全部</span>
  <span style="font-size:12px;padding:5px 14px;color:var(--ink-3);border:0.5px solid #D3D1C7;border-radius:2px;">草稿</span>
  <span style="font-size:12px;padding:5px 14px;color:var(--ink-3);border:0.5px solid #D3D1C7;border-radius:2px;">待审核</span>
  <span style="font-size:12px;padding:5px 14px;color:var(--ink-3);border:0.5px solid #D3D1C7;border-radius:2px;">已发布</span>
  <span style="font-size:12px;padding:5px 14px;color:var(--ink-3);border:0.5px solid #D3D1C7;border-radius:2px;">已下架</span>
</div>
<div class="hairline-table">
  <div class="thead cols" style="display:grid;grid-template-columns:minmax(0,1fr) 80px 96px 90px 140px;">
    <div style="padding:0 14px 0 4px;">标题</div><div>分类</div><div>状态</div><div>更新时间</div><div style="text-align:right;padding-right:4px;">操作</div>
  </div>
  {rows}
</div>''')

    # 收藏
    favs = "".join(f'''<div style="display:flex;align-items:center;gap:14px;padding:14px 0;border-bottom:0.5px solid var(--hairline);">
  <div style="min-width:0;flex:1;"><p class="feed-title" style="margin:0 0 2px;font-size:15px;"><a href="/article.html">{t}</a></p>
  <p style="margin:0;font-size:12px;color:var(--ink-4);">{au} · 收藏于 {tm}</p></div>
  <span class="btn-ghost-sm" style="border:none;color:var(--ink-4);">取消收藏</span>
</div>''' for t, au, tm in [(a[1], a[3], d) for a, d in zip(ARTICLES[:4], ["3 天前", "上周", "上周", "两周前"])])
    ws_page("me-favorites.html", "我的收藏", "我的收藏", f'<h1>我的收藏（4）</h1>{favs}')

    # 通知
    notifs = "".join(f'''<div class="notif"><span class="dot{" read" if i >= 2 else ""}"></span>
  <div style="min-width:0;flex:1;"><p class="n-title">{t}</p><p class="n-body">{b}</p><p class="n-time">{tm}</p></div>
</div>''' for i, (t, b, tm) in enumerate([
        ("评论回复", "阿王 回复了你在《单聊消息序号》下的评论", "2 小时前"),
        ("文章审核通过", "《Meilisearch 中文分词踩坑记录》已通过审核并发布", "昨天"),
        ("点赞", "阿陈 赞了你的文章《MySQL 索引设计》", "昨天"),
        ("系统通知", "你已连续写作 4 周，继续保持", "3 天前"),
    ]))
    ws_page("me-notifications.html", "站内通知", "站内通知",
            f'<h1>站内通知</h1><p style="font-size:12px;color:var(--ink-4);margin:-12px 0 14px;"><span class="link-u">全部标为已读</span></p>{notifs}')


# ---- 6. 写文章 ----
def gen_write():
    body = f'''{topbar("")}
<div class="container" style="padding-top:24px;padding-bottom:40px;">
  <div style="display:flex;align-items:center;margin-bottom:18px;">
    <span style="font-family:var(--serif);font-size:18px;color:var(--ink);">写文章</span>
    <span style="font-size:12px;color:var(--ink-4);margin-left:12px;">草稿保存于 10:44</span>
    <div style="margin-left:auto;display:flex;gap:10px;">
      <button class="btn-ghost-sm">存草稿</button>
      <button class="btn-dark" style="height:36px;padding:0 24px;">提交审核</button>
    </div>
  </div>
  <div class="editor-layout">
    <div class="editor-area">
      <div class="editor-title"><input placeholder="标题：一句话说清这篇文章解决什么问题"></div>
      <textarea placeholder="正文支持 Markdown：## 二级标题、**加粗**、`代码`、- 列表…"></textarea>
      <p style="margin:8px 0 0;font-size:12px;color:var(--ink-4);">正文以 Markdown 原文存储 · 支持粘贴图片自动上传</p>
    </div>
    <div class="settings-panel">
      <h4>发布设置</h4>
      <div class="field"><label>分类</label><select><option>后端</option><option>架构</option><option>数据库</option></select></div>
      <div class="field"><label>标签（最多 5 个）</label><input type="text" value="Go, 性能" placeholder="输入后回车"></div>
      <div class="field"><label>封面图</label>
        <div style="height:100px;border:0.5px dashed #D3D1C7;display:flex;align-items:center;justify-content:center;font-size:12px;color:var(--ink-4);">点击或拖拽上传封面</div>
      </div>
      <div class="field"><label>摘要（留空自动截取）</label><textarea style="height:72px;" placeholder="显示在列表页的一段话"></textarea></div>
      <div class="field"><label>允许评论</label>
        <span class="toggle on"><span class="track"></span><span style="font-size:12px;color:var(--ink-3);">开启</span></span>
      </div>
    </div>
  </div>
</div>
{footer()}'''
    w("write.html", page(title="写文章 · 墨痕", active="", body=body))


# ---- 7. 错误页 ----
def error_page(fname, code, msg, sub):
    body = f'''{topbar("")}
<div class="error-page">
  <p class="code">{code}</p>
  <p class="msg">{msg}</p>
  <p class="sub">{sub}</p>
  <a class="btn-dark" style="text-decoration:none;height:38px;padding:0 26px;" href="/">回到首页</a>
</div>
{footer()}'''
    w(fname, page(title=f"{code} · 墨痕", active="", body=body))


# ---- 8. 管理端 ----
ADMIN_NAV = [("/admin/index.html", "概览"), ("/admin/users.html", "用户管理"), ("/admin/articles.html", "文章审核"),
             ("/admin/comments.html", "评论管理"), ("/admin/reports.html", "举报处理"), ("/admin/categories.html", "分类"),
             ("/admin/tags.html", "标签"), ("/admin/media.html", "媒体库"), ("/admin/settings.html", "系统设置"),
             ("/admin/index-status.html", "索引状态"), ("/admin/logs.html", "操作日志")]


def admin_page(fname, title, active, crumb, content):
    nav = "".join(f'<a{" class=\"on\"" if n == active else ""} href="{h}">{n}</a>' for h, n in ADMIN_NAV)
    html = f'''<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{title} · 墨痕管理</title>
<link rel="stylesheet" href="/assets/site.css">
<link rel="stylesheet" href="/assets/components.css">
<link rel="stylesheet" href="/assets/admin.css">
</head>
<body>
<div class="admin-layout">
  <aside class="admin-menu">
    <span class="logo">墨痕 · 管理</span>
    <nav>{nav}</nav>
    <div class="menu-foot">
      <div class="avatar-dot">管</div>
      <div><p class="name">admin</p><p class="role">超级管理员</p></div>
    </div>
  </aside>
  <div class="admin-main">
    <div class="admin-crumb">{crumb}</div>
    <div class="admin-content">{content}</div>
  </div>
</div>
</body>
</html>'''
    w(fname, html)


COLS_USER = "minmax(0,1fr) 150px 90px 90px 120px 130px"


def gen_admin():
    # 用户管理
    rows = ""
    for name, mail, role, status, joined in [
        ("阿张 liu**", "zhang***@test.local", "作者", "正常", "2026-08-02"),
        ("阿李 li**", "li***@test.local", "作者", "正常", "2026-08-11"),
        ("阿王 wa**", "wang***@test.local", "读者", "正常", "2026-09-01"),
        ("阿赵 zh**", "zhao***@test.local", "读者", "已禁用", "2026-09-14"),
    ]:
        st = '<span class="chip chip-green">正常</span>' if status == "正常" else '<span class="chip chip-gray">已禁用</span>'
        op = "禁用" if status == "正常" else "启用"
        rows += f'''<div class="trow" style="display:grid;grid-template-columns:{COLS_USER};">
  <div class="cell-title">{name}</div><div class="cell" style="font-family:var(--mono);font-size:12px;">{mail}</div>
  <div class="cell">{role}</div><div>{st}</div><div class="cell">{joined}</div>
  <div class="ops"><span class="link-u">{op}</span><span class="link-u">角色</span></div>
</div>'''
    admin_page("admin/users.html", "用户管理", "用户管理", '<span class="page serif">用户管理</span><span class="crumb-sep">/</span> 全部用户', f'''
<div style="display:flex;gap:10px;margin-bottom:16px;">
  <div class="admin-input" style="width:220px;">搜索用户名 / 邮箱…</div>
  <div class="admin-select">角色：全部 ▾</div><div class="admin-select">状态：全部 ▾</div>
  <span style="margin-left:auto;font-size:12px;color:var(--ink-3);align-self:center;">共 56 人</span>
</div>
<div class="hairline-table">
  <div class="thead" style="display:grid;grid-template-columns:{COLS_USER};">
    <div style="padding:0 14px 0 4px;">用户</div><div>邮箱</div><div>角色</div><div>状态</div><div>注册时间</div><div style="text-align:right;padding-right:4px;">操作</div>
  </div>{rows}
</div>
<p style="margin-top:12px;font-size:11px;color:var(--ink-4);">不能禁用自己；不能撤销最后一个管理员；禁用后令牌吊销并踢下线。</p>''')

    # 评论管理
    rows = ""
    for content, article, author, st in [
        ("LAST_INSERT_ID 这个用法第一次见，长知识了。", "单聊消息序号", "阿王", "已通过"),
        ("行锁会不会成瓶颈？求压测数据。", "单聊消息序号", "阿陈", "待审核"),
        ("写得很清楚，收藏了。", "MySQL 索引设计", "阿周", "已通过"),
        ("垃圾广告内容（已删除示例）", "Go 1.26 泛型性能实测", "匿名", "已删除"),
    ]:
        chip = {"已通过": '<span class="chip chip-green">已通过</span>',
                "待审核": '<span class="chip chip-amber">待审核</span>',
                "已删除": '<span class="chip chip-gray">已删除</span>'}[st]
        ops = '<span class="link-u">通过</span><span class="link-u">拒绝</span>' if st == "待审核" else '<span class="link-dim">—</span>'
        rows += f'''<div class="trow" style="display:grid;grid-template-columns:minmax(0,1fr) 160px 90px 96px 140px;">
  <div class="cell-title" style="white-space:normal;">{content}</div>
  <div class="cell">{article}</div><div class="cell">{author}</div><div>{chip}</div>
  <div class="ops">{ops}<span class="link-u">删除</span></div>
</div>'''
    admin_page("admin/comments.html", "评论管理", "评论管理", '<span class="page serif">评论管理</span><span class="crumb-sep">/</span> 全部评论', f'''
<div style="display:flex;gap:10px;margin-bottom:16px;">
  <div class="admin-select">状态：全部 ▾</div><div class="admin-select">文章：全部 ▾</div>
</div>
<div class="hairline-table">
  <div class="thead" style="display:grid;grid-template-columns:minmax(0,1fr) 160px 90px 96px 140px;">
    <div style="padding:0 14px 0 4px;">评论内容</div><div>所属文章</div><div>作者</div><div>状态</div><div style="text-align:right;padding-right:4px;">操作</div>
  </div>{rows}
</div>''')

    # 举报处理
    rows = ""
    for reporter, ttype, target, reason, st in [
        ("阿王", "评论", "#102", "人身攻击", "待处理"),
        ("阿李", "文章", "《某广告文》", "垃圾广告", "待处理"),
        ("阿张", "消息", "会话 #1 · 消息 #44", "骚扰", "已驳回"),
    ]:
        chip = '<span class="chip chip-amber">待处理</span>' if st == "待处理" else '<span class="chip chip-gray">已驳回</span>'
        ops = '<span class="link-u">下架对象</span><span class="link-u">驳回</span>' if st == "待处理" else '<span class="link-dim">查看</span>'
        rows += f'''<div class="trow" style="display:grid;grid-template-columns:90px 90px minmax(0,1fr) 110px 96px 150px;">
  <div class="cell">{reporter}</div><div class="cell">{ttype}</div><div class="cell-title">{target}</div>
  <div class="cell">{reason}</div><div>{chip}</div><div class="ops">{ops}</div>
</div>'''
    admin_page("admin/reports.html", "举报处理", "举报处理", '<span class="page serif">举报处理</span><span class="crumb-sep">/</span> 举报队列', f'''
<div style="display:flex;gap:10px;margin-bottom:16px;">
  <div class="admin-select">状态：待处理 ▾</div><div class="admin-select">类型：全部 ▾</div>
</div>
<div class="hairline-table">
  <div class="thead" style="display:grid;grid-template-columns:90px 90px minmax(0,1fr) 110px 96px 150px;">
    <div style="padding:0 14px 0 4px;">举报人</div><div>类型</div><div>对象</div><div>理由</div><div>状态</div><div style="text-align:right;padding-right:4px;">操作</div>
  </div>{rows}
</div>''')

    # 分类
    rows = "".join(f'''<div class="trow" style="display:grid;grid-template-columns:minmax(0,1fr) 160px 90px 90px 150px;">
  <div class="cell-title">{n}</div><div class="cell mono" style="font-size:12px;">{s}</div>
  <div class="cell">{c} 篇</div><div class="cell">{o}</div>
  <div class="ops"><span class="link-u">编辑</span><span class="link-u">删除</span><span class="link-dim">上移 / 下移</span></div>
</div>''' for n, s, c, o in [("后端", "backend", 34, 1), ("架构", "architecture", 28, 2), ("数据库", "database", 31, 3), ("部署", "deploy", 19, 4), ("测试", "testing", 16, 5)])
    admin_page("admin/categories.html", "分类", "分类", '<span class="page serif">分类</span><span class="crumb-sep">/</span> 排序与增删', f'''
<div style="display:flex;align-items:center;margin-bottom:16px;">
  <span style="font-size:12px;color:var(--ink-3);">拖动或用上下移调整顺序，顺序即时生效于前台导航。</span>
  <span class="btn-dark" style="margin-left:auto;height:34px;padding:0 18px;">+ 新建分类</span>
</div>
<div class="hairline-table">
  <div class="thead" style="display:grid;grid-template-columns:minmax(0,1fr) 160px 90px 90px 150px;">
    <div style="padding:0 14px 0 4px;">名称</div><div>Slug</div><div>文章数</div><div>排序</div><div style="text-align:right;padding-right:4px;">操作</div>
  </div>{rows}
</div>''')

    # 标签
    rows = "".join(f'''<div class="trow" style="display:grid;grid-template-columns:minmax(0,1fr) 90px 90px 150px;">
  <div class="cell-title">{n}</div><div class="cell">{c} 篇</div><div class="cell">{t}</div>
  <div class="ops"><span class="link-u">编辑</span><span class="link-u">删除</span></div>
</div>''' for n, c, t in [("Go", 46, "2026-08-03"), ("MySQL", 31, "2026-08-09"), ("WebSocket", 12, "2026-09-20"), ("架构", 28, "2026-08-15"), ("部署", 19, "2026-09-01")])
    admin_page("admin/tags.html", "标签", "标签", '<span class="page serif">标签</span><span class="crumb-sep">/</span> 增删改与合并', f'''
<div style="display:flex;align-items:center;margin-bottom:16px;">
  <span style="font-size:12px;color:var(--ink-3);">同义标签用"合并"收编，文章关联自动迁移。</span>
  <span class="btn-dark" style="margin-left:auto;height:34px;padding:0 18px;">+ 新建标签</span>
  <span class="btn-ghost-sm" style="margin-left:8px;">合并标签…</span>
</div>
<div class="hairline-table">
  <div class="thead" style="display:grid;grid-template-columns:minmax(0,1fr) 90px 90px 150px;">
    <div style="padding:0 14px 0 4px;">名称</div><div>文章数</div><div>创建时间</div><div style="text-align:right;padding-right:4px;">操作</div>
  </div>{rows}
</div>''')

    # 媒体库
    tiles = "".join(f'''<div class="m">
  <div class="img" style="background:{bg};">{svg}</div>
  <div class="meta"><p class="t" style="margin:0 0 2px;">{name}</p>{size} · {d}</div>
</div>''' for name, bg, svg, size, d in [
        ("avatar_张_256.png", "#E6F1FB", '<svg width="44" height="30" viewBox="0 0 70 44"><path d="M8 36 L22 12 L34 28 L46 8 L62 36" fill="none" stroke="#185FA5" stroke-width="3"/></svg>', "24 KB", "2 小时前"),
        ("chat_img_004.png", "#FAEEDA", '<svg width="44" height="26" viewBox="0 0 70 40"><rect x="10" y="8" width="50" height="24" rx="6" fill="none" stroke="#854F0B" stroke-width="3"/></svg>', "86 KB", "昨天"),
        ("cover_mysql.png", "#EAF3DE", '<svg width="44" height="30" viewBox="0 0 64 44"><circle cx="22" cy="22" r="14" fill="none" stroke="#3B6D11" stroke-width="3"/></svg>', "132 KB", "3 天前"),
        ("cover_spec.png", "#EEEDFE", '<svg width="44" height="30" viewBox="0 0 64 44"><rect x="12" y="10" width="40" height="26" rx="4" fill="none" stroke="#534AB7" stroke-width="3"/></svg>', "98 KB", "上周"),
        ("chat_img_009.gif", "#FCEBEB", '<svg width="44" height="30" viewBox="0 0 70 44"><path d="M14 32 h42 M20 32 V16 l14-9 14 9 v16" fill="none" stroke="#A32D2D" stroke-width="3"/></svg>', "1.2 MB", "上周"),
        ("avatar_王_256.png", "#FBEAF0", '<svg width="44" height="30" viewBox="0 0 64 44"><circle cx="32" cy="22" r="14" fill="none" stroke="#993556" stroke-width="3"/></svg>', "22 KB", "两周前"),
    ])
    admin_page("admin/media.html", "媒体库", "媒体库", '<span class="page serif">媒体库</span><span class="crumb-sep">/</span> 全部图片', f'''
<div style="display:flex;gap:10px;margin-bottom:16px;">
  <div class="admin-input" style="width:220px;">搜索文件名…</div>
  <div class="admin-select">用途：全部 ▾</div>
  <span style="margin-left:auto;font-size:12px;color:var(--ink-3);align-self:center;">共 6 个文件</span>
</div>
<div class="media-grid">{tiles}</div>
<p style="margin-top:14px;font-size:11px;color:var(--ink-4);">聊天图片为私有桶：管理端可见，前台仅会话成员经签名 URL 访问。删除不影响已引用该图的历史消息展示（回退占位图）。</p>''')

    # 系统设置
    admin_page("admin/settings.html", "系统设置", "系统设置", '<span class="page serif">系统设置</span><span class="crumb-sep">/</span> 站点参数', '''
<div style="max-width:560px;">
  <h1 style="font-family:var(--serif);font-size:18px;color:var(--ink);margin:0 0 18px;">站点信息</h1>
  <div class="field"><label>站点名称</label><input type="text" value="墨痕"></div>
  <div class="field"><label>站点描述</label><input type="text" value="记录 Go、数据库与系统设计的实践与复盘"></div>
  <div class="field"><label>备案号</label><input type="text" value="沪ICP备2026xxxxx号"></div>
  <h1 style="font-family:var(--serif);font-size:18px;color:var(--ink);margin:26px 0 18px;">功能开关</h1>
  <div style="display:flex;flex-direction:column;gap:16px;margin-bottom:26px;">
    <div style="display:flex;align-items:center;justify-content:space-between;border-bottom:0.5px solid var(--hairline);padding-bottom:14px;">
      <div><p style="margin:0;font-size:13px;color:var(--ink);">文章审核</p><p style="margin:2px 0 0;font-size:12px;color:var(--ink-4);">开启后文章需管理员通过才发布</p></div>
      <span class="toggle on"><span class="track"></span></span>
    </div>
    <div style="display:flex;align-items:center;justify-content:space-between;border-bottom:0.5px solid var(--hairline);padding-bottom:14px;">
      <div><p style="margin:0;font-size:13px;color:var(--ink);">开放注册</p><p style="margin:2px 0 0;font-size:12px;color:var(--ink-4);">关闭后仅已有账号可登录</p></div>
      <span class="toggle on"><span class="track"></span></span>
    </div>
    <div style="display:flex;align-items:center;justify-content:space-between;border-bottom:0.5px solid var(--hairline);padding-bottom:14px;">
      <div><p style="margin:0;font-size:13px;color:var(--ink);">评论开关（全站）</p><p style="margin:2px 0 0;font-size:12px;color:var(--ink-4);">关闭后所有文章不可评论，实时生效</p></div>
      <span class="toggle"><span class="track"></span></span>
    </div>
  </div>
  <button class="btn-dark" style="height:40px;padding:0 28px;">保存设置</button>
  <p style="margin-top:12px;font-size:11px;color:var(--ink-4);">每次修改写入操作日志。</p>
</div>''')

    # 操作日志
    rows = "".join(f'''<div class="trow" style="display:grid;grid-template-columns:150px 90px 150px minmax(0,1fr) 120px;">
  <div class="cell mono" style="font-size:12px;">{tm}</div><div class="cell">{who}</div>
  <div class="cell">{act}</div><div class="cell">{obj}</div><div class="cell mono" style="font-size:12px;">{ip}</div>
</div>''' for tm, who, act, obj, ip in [
        ("2026-10-22 09:12", "admin", "article.approve", "《Meilisearch 中文分词踩坑记录》", "127.0.0.1"),
        ("2026-10-22 09:10", "admin", "user.set_status", "禁用用户 阿赵（原因：刷屏）", "127.0.0.1"),
        ("2026-10-21 18:40", "admin", "article.offline", "《某广告文》（原因：垃圾广告）", "127.0.0.1"),
        ("2026-10-21 18:38", "admin", "report.handle", "举报 #12 驳回", "127.0.0.1"),
        ("2026-10-21 17:02", "admin", "settings.update", "关闭全站评论开关（10 分钟后恢复）", "127.0.0.1"),
    ])
    admin_page("admin/logs.html", "操作日志", "操作日志", '<span class="page serif">操作日志</span><span class="crumb-sep">/</span> 审计记录', f'''
<div style="display:flex;gap:10px;margin-bottom:16px;">
  <div class="admin-input" style="width:220px;">搜索动作 / 对象…</div>
  <div class="admin-select">操作人：全部 ▾</div>
</div>
<div class="hairline-table">
  <div class="thead" style="display:grid;grid-template-columns:150px 90px 150px minmax(0,1fr) 120px;">
    <div style="padding:0 14px 0 4px;">时间</div><div>操作人</div><div>动作</div><div>对象</div><div>IP</div>
  </div>{rows}
</div>
<p style="margin-top:12px;font-size:11px;color:var(--ink-4);">日志只增不改，保留 180 天。</p>''')


def page(title, active, body):
    return f'''<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{title}</title>
<link rel="stylesheet" href="/assets/site.css">
<link rel="stylesheet" href="/assets/components.css">
</head>
<body>
{topbar(active)}
{body}
{footer()}
</body>
</html>'''


if __name__ == "__main__":
    gen_index()
    list_page("category.html", "分类 · 墨痕", "分类：架构", "共 28 篇 · 按发布时间排序", "分类")
    list_page("tag.html", "标签 · 墨痕", "标签：Go", "共 46 篇 · 按发布时间排序", "标签")
    list_page("search.html", "搜索 · 墨痕", "搜索：MySQL", "找到 6 条与“MySQL”相关的文章", "首页")
    gen_user()
    auth_page("login.html", "登录", "欢迎回来",
              [("邮箱 / 用户名", "text", "", ""), ("密码", "password", "", "")],
              '还没有账号？<a href="/register.html">注册</a> · <a href="/forgot-password.html">忘记密码</a>', btn="登 录")
    auth_page("register.html", "注册", "创建账号",
              [("用户名", "text", "3–20 位字母数字", ""),
               ("邮箱", "email", "用于接收验证邮件", ""),
               ("密码", "password", "至少 8 位，含字母与数字", "")],
              '已有账号？<a href="/login.html">直接登录</a>',
              '<p style="font-size:11px;color:var(--ink-4);margin:-6px 0 16px;">注册后需点击邮件中的验证链接才能登录。</p>', btn="注 册")
    auth_page("forgot-password.html", "找回密码", "找回密码",
              [("注册邮箱", "email", "用于接收重置链接", "")],
              '想起来密码了？<a href="/login.html">直接登录</a>',
              '<p style="font-size:11px;color:var(--ink-4);margin:-6px 0 16px;">重置链接 30 分钟内有效。</p>', btn="发送重置链接")
    auth_page("reset-password.html", "重置密码", "设置新密码",
              [("新密码", "password", "至少 8 位，含字母与数字", ""),
               ("确认新密码", "password", "", "")],
              '重置完成后请用新密码登录', btn="重置密码")
    auth_page("verify-email.html", "邮箱验证", "邮箱验证",
              [],
              '<a href="/login.html">验证成功，去登录 →</a>',
              '<div style="text-align:center;padding:12px;border:0.5px solid #BBF7D0;background:#F0FDF4;color:var(--green);font-size:13px;border-radius:2px;margin-bottom:20px;">验证成功，账号已激活</div>', btn="重新发送验证邮件")
    gen_me_pages()
    gen_write()
    error_page("404.html", "404", "页面不存在", "可能已被删除，或地址写错了。")
    error_page("410.html", "410", "内容已删除", "这篇文章已被作者删除，无法恢复。")
    error_page("403.html", "403", "没有权限", "登录或角色不满足访问条件。")
    gen_admin()
    print("全部页面生成完毕")



