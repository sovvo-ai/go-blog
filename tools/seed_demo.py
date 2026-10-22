"""演示数据播种脚本（可重复执行：已存在的账号/文章会跳过）。

用法：python tools/seed_demo.py
前提：后端 8080 在跑，MySQL 可用（用于把注册账号直接置为已验证）。
"""
import json
import subprocess
import urllib.error
import urllib.request
import http.cookiejar

API = 'http://127.0.0.1:8080'
DB = ['mysql', '-h', '127.0.0.1', '-u', 'root', '-p123456', 'blog', '-e']
PASSWORD = 'Demo12345'

ROLE_ID = {'reader': 1, 'author': 2, 'admin': 3}


def sql(query: str) -> None:
    subprocess.run(DB + [query], capture_output=True)


class Client:
    def __init__(self):
        self.jar = http.cookiejar.CookieJar()
        self.opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(self.jar))

    def _csrf(self) -> str:
        for c in self.jar:
            if c.name == 'csrf_token':
                return c.value
        return ''

    def req(self, method: str, path: str, body=None):
        data = json.dumps(body).encode() if body is not None else None
        req = urllib.request.Request(API + path, data=data, method=method)
        if data:
            req.add_header('Content-Type', 'application/json')
        token = self._csrf()
        if token and method not in ('GET', 'HEAD'):
            req.add_header('X-CSRF-Token', token)
        try:
            with self.opener.open(req, timeout=30) as resp:
                return resp.status, json.loads(resp.read().decode() or '{}')
        except urllib.error.HTTPError as e:
            try:
                return e.code, json.loads(e.read().decode() or '{}')
            except Exception:
                return e.code, {}

    def login(self, account: str, password: str = PASSWORD):
        return self.req('POST', '/api/v1/auth/login', {'account': account, 'password': password})


# ---------------------------------------------------------------- 账号
ACCOUNTS = [
    ('lin_zhiyuan', '林知远', 'author', 'lin@mohen.local'),
    ('su_mu', '苏木', 'author', 'su@mohen.local'),
    ('chen_yi', '陈一', 'author', 'chen@mohen.local'),
    ('wang_ke', '王珂', 'reader', 'wang@mohen.local'),
    ('zhou_ning', '周宁', 'reader', 'zhou@mohen.local'),
]

# ---------------------------------------------------------------- 标签
TAGS = [
    ('架构设计', 'arch-design'),
    ('性能优化', 'performance'),
    ('前端工程', 'frontend-eng'),
    ('数据库', 'database'),
    ('随笔', 'essay'),
    ('工具链', 'toolchain'),
]

# ---------------------------------------------------------------- 文章
ARTICLES = [
    {
        'author': 'lin_zhiyuan', 'category': 1, 'tags': ['架构设计', '数据库'],
        'title': '为什么单聊消息序号不用 Redis INCR',
        'summary': '会话内 seq 必须严格递增且不能有空洞。Redis 的 INCR 看似顺手，但在"消息落库失败要回滚"这件事上没有退路。',
        'content': '''## 问题长什么样

单聊的消息要按 `seq` 排序，客户端断线重连后用 `after_seq` 补齐。要求很硬：

- 同一会话内严格递增
- 不能有空洞（有空洞客户端会以为丢了消息）
- 并发发送不能重复

## Redis INCR 为什么不够

`INCR conv:{id}:seq` 一行就能拿到序号的思路很诱人，但它把"分配序号"和"写入消息"拆成了两个事务：

```go
seq := redis.Incr(key)              // 分配了序号
if err := db.Create(msg.WithSeq(seq)); err != nil {
    // 这里回滚不了 Redis 的自增
}
```

落库失败就产生永久空洞。用 Lua 把两者包一起也不行——Redis 回滚不了 MySQL。

## 用行锁 + LAST_INSERT_ID 反而最稳

把分配动作放进消息插入的同一个事务里，靠 InnoDB 行锁串行化：

```sql
UPDATE conversations SET seq_counter = LAST_INSERT_ID(seq_counter + 1) WHERE id = ?;
INSERT INTO messages (conversation_id, seq, ...) VALUES (?, LAST_INSERT_ID(), ...);
```

两条语句在同一事务内，`LAST_INSERT_ID()` 返回本次会话连接上刚生成的值，天然和插入对齐；事务回滚时 `seq_counter` 一起回滚，不会留空洞。

## 实测

10 路并发往同一会话发消息，落库后检查 `seq` 应当是 1..10 连续无重复。集成测试里已经把这条钉死了——这类"看着能跑、并发就漏"的逻辑，没有测试等于没有实现。

> 结论：能用数据库一处保证的事，不要拆到两个存储上。''',
    },
    {
        'author': 'lin_zhiyuan', 'category': 1, 'tags': ['性能优化', '架构设计'],
        'title': 'Meilisearch 中文分词踩坑记录',
        'summary': '默认分词器对中文按字切，搜"消息序号"会命中一堆无关文章。换成 jieba 词典后，召回率和精确率都变了。',
        'content': '''## 现象

上线搜索后，搜「消息序号」的前几条是讲「消息队列」和「序号生成」的两篇不同文章，混在一起。

默认的 `chinese` 分词器把词切得很碎，`消息序号` 被拆成单字，倒排索引里全是噪声。

## 换 jieba 词典

```bash
meilisearch --dictionary /dict/jieba.txt
```

建索引时指定：

```json
{ "tokenize": "jieba" }
```

重跑全量重建后，「消息序号」只召回目标文章，前 3 条全部相关。

## 代价

- 词典文件要跟着部署走，镜像变大
- 新词（比如框架名）需要手工补，否则被切碎
- 全量重建期间搜索要降级到数据库 LIKE，接口层要做开关

## 别忘同步对账

索引和数据库是两套状态，写库成功不代表索引成功。我们的做法是：

1. 文章发布/更新/删除时投递同步任务
2. 每 5 分钟对账一次，把 `index_failures` 里的文章重推
3. 管理端「索引状态」页暴露失败队列和全量重建入口

没有对账的搜索，迟早和数据库对不上。''',
    },
    {
        'author': 'su_mu', 'category': 1, 'tags': ['前端工程'],
        'title': 'Nuxt 3 里 401 刷新的正确姿势',
        'summary': '并发请求同时 401，如果每个都去刷新，会互相把对方的 refresh token 轮换掉。必须单例化。',
        'content': '''## 错误写法

```ts
try {
  return await $fetch(url, opts)
} catch (e) {
  if (e.status === 401) {
    await $fetch('/api/v1/auth/refresh', { method: 'POST' })
    return await $fetch(url, opts)   // 每个请求各自刷新
  }
}
```

页面上同时有 5 个请求时，5 个刷新并发打到后端。后端的 refresh token 是一次性轮换的，第 2 个之后的刷新全部失败，用户被踢下线。

## 单例化

模块级持有一个 Promise：

```ts
let refreshing: Promise<boolean> | null = null
function refreshOnce() {
  if (!refreshing) {
    refreshing = doRefresh().finally(() => { refreshing = null })
  }
  return refreshing
}
```

所有 401 都 `await refreshOnce()`，无论多少个并发，后端只收到一次刷新。

## 还有几个坑

- **重放只做一次**：重放后还是 401，说明会话真的失效了，直接清状态跳登录，别再递归
- **登录/刷新接口本身不重试**：这两个 401 就是失败本身
- **SSR 拿不到 refresh cookie**：如果后端把 refresh token 的 Cookie Path 限定在 `/api/v1/auth`，页面请求是带不上它的，服务端无法续期——只能交给浏览器解决

最后一条最容易被忽略：服务端渲染时看着"用户没登录"，其实是"没有续期的手段"。''',
    },
    {
        'author': 'su_mu', 'category': 1, 'tags': ['前端工程', '工具链'],
        'title': '把设计稿变成 token，而不是一堆魔法数字',
        'summary': '同一个间距在 6 个页面里写了 5 种值，改一次要全局搜。抽成 token 之后，改版只动一处。',
        'content': '''## 症状

```css
/* 页面 A */ margin-bottom: 13px;
/* 页面 B */ margin-bottom: 12px;
/* 页面 C */ margin-bottom: 14px;
```

没有人是故意的，但六个月后没人敢动。

## 抽成 token

```css
:root {
  --space-1: 4px;
  --space-2: 8px;
  --space-3: 12px;
  --space-4: 16px;
  --space-6: 24px;
  --ink: #111827;
  --ink-3: #6B7280;
  --hairline: #E5E7EB;
}
```

规则很简单：**只有 token 里有的值可以出现**。设计师改间距，改 `--space-3`，全站生效。

## 圆角要有档位

2px / 4px / 8px 三档足够。出现 6px 这种值，通常是有人手滑，而不是设计意图。

## 字体只留两个

- 标题：衬线（Noto Serif SC）
- 正文：系统无衬线

第三个字体加进来之前，先问问它解决什么问题。多数时候答案是"没有"。

## 收益

改版从"全局搜替换 + 祈祷"变成改一个文件。这不是美学问题，是维护成本问题。''',
    },
    {
        'author': 'chen_yi', 'category': 2, 'tags': ['随笔'],
        'title': '写了三年技术博客，我删掉了大半',
        'summary': '留下来的文章有个共同点：它们回答的是"为什么"，而不是"怎么做"。',
        'content': '''## 删掉的是什么

教程类的、抄文档的、翻译官方 blog 的。当时写得很爽，回头看没有任何自己的东西。

## 留下来的是什么

- 踩过的坑，以及坑背后的机制
- 做过的取舍，以及放弃的那条路为什么不行
- 想明白的一件事，哪怕只有一段话

## 一个判断标准

写之前问自己：这篇东西，三个月后的我还愿意读吗？

如果答案是否定的，那它大概率对别人也没有价值。

## 数量的错觉

一年 50 篇水文，不如 8 篇能被人收藏的文章。

读者不会因为你更新勤快而记住你，他们只会因为某一篇解决了他的问题而记住你。''',
    },
    {
        'author': 'chen_yi', 'category': 2, 'tags': ['随笔', '工具链'],
        'title': '我的写作工具链：从记想法到发布',
        'summary': '工具越少越好。现在的链路是：手机备忘录 → Markdown 文件 → 提交审核 → 发布。',
        'content': '''## 记录

想法随手丢进手机备忘录，一句话也行。**不要在记录阶段考虑结构**，那会让你一个字都写不出来。

## 起草

每周挑一个想法，扩成 300 字的骨架：三个小标题，每个下面两三句话。骨架不满意就丢掉，不硬写。

## 成文

Markdown 本地写，代码块直接从编辑器复制，避免二次转义。

写完放一天再读一遍，通常会删掉开头两段——那两段只是在热身。

## 发布

提交 → 审核 → 发布。审核这一步对我有用：它逼我在提交前再读一遍。

## 不做的事

- 不追热点
- 不为更新频率硬凑
- 不在没想清楚的时候动笔''',
    },
    {
        'author': 'lin_zhiyuan', 'category': 1, 'tags': ['数据库', '性能优化'],
        'title': '列表接口的 N+1，都是这样长出来的',
        'summary': '一个会话列表接口，20 条数据发了 61 次查询。没人故意写坏，它是"先跑通"的必然结果。',
        'content': '''## 长出来的过程

第一版为了快，先查会话，再在循环里补对方用户：

```go
for _, c := range convs {
    peer, _ := repo.UserByID(c.PeerID)
    last, _ := repo.LastMessage(c.ID)
    unread, _ := repo.UnreadCount(c.ID, uid)
}
```

功能完全正确，本地 3 条数据也很快。上线后 limit=20，一次列表 = 1 + 60 次查询。

## 怎么修

1. **JOIN 拿能拿的**：成员表的 `unread_count`、`is_muted` 本来就在 join 里，直接 select 出来
2. **批量 IN 拿剩下的**：最后一条消息按 id 集合一次取回，内存里配对
3. **别在循环里等 I/O**：任何"循环 + 查询"都值得警觉

修完是固定 3 次查询，与 limit 无关。

## 怎么早点发现

- 本地开发时打开 SQL 日志，看一次请求打了几条
- 列表接口 review 时，先看有没有 for 循环里调仓储
- 压测时关注 DB QPS 和接口 QPS 的比值，超过 5 就要查

N+1 不会报错，它只会让数据库先累死。''',
    },
]

ARTICLES += [
    {
        'author': 'demo_writer', 'category': 1, 'tags': ['工具链', '前端工程'],
        'title': '把博客当成自己的知识库来写',
        'summary': '写作的第一读者是自己。当成知识库写，收益是复利；当成作业写，写三篇就断了。',
        'content': '''## 两个心态，两种结果

**当成作业**：目标是"发出去"，于是凑字数、抄文档、追热点。发完就不想再看。

**当成知识库**：目标是"三个月后的我能用得上"，于是记录上下文、记录取舍、记录踩过的坑。

## 具体做法

1. 每篇文章至少留下一个**可复用的结论**，比如"能用数据库一处保证的事，不要拆到两个存储上"
2. 代码片段要能直接跑，贴半截代码等于没写
3. 失败的路也要写：为什么 A 不行，比 A 怎么做更有信息量

## 索引比分类重要

分类是给别人看的，索引是给自己用的。我的习惯是每篇文章至少挂两个标签：一个领域，一个手法（如"数据库 + 性能优化"），后来找东西时按手法搜得更多。

## 收益

半年后回看，能快速捡起当时的判断依据，而不是只记得"好像踩过这个坑"。
''',
    },
    {
        'author': 'demo_writer', 'category': 2, 'tags': ['随笔', '工具链'],
        'title': '每天写一百字，比每周写三千字容易',
        'summary': '写作的阻力不在能力，在启动成本。把门槛降到一百字，习惯才立得住。',
        'content': '''## 为什么"每周三千字"总是断

因为它需要一整块时间、一个完整的想法、一个不被打扰的下午。三个条件同时满足的次数，一个月可能只有两次。

## 一百字的版本

- 今天遇到的一个报错，以及原因
- 一个想明白了的小概念
- 一段值得留下的代码

门槛低到"打开就能写"，反而天天都在写。

## 攒起来之后

一百字攒一个月大约三千字，但它是**分散在三十个真实场景里**的三千字，比憋出来的一篇长文密度高得多。

需要成文时，挑三条相关的扩写，素材现成。

## 唯一的纪律

写完就发，不等"攒够"。等待是最容易让习惯死掉的动作。
''',
    },
]

# 未发布的两篇：一篇待审核、一篇草稿（用来看状态差异）
PENDING_ARTICLE = {
    'author': 'su_mu', 'category': 1, 'tags': ['前端工程'],
    'title': '聊聊表单校验的边界',
    'summary': '前端校验是为了体验，不是为了安全。这条线一旦模糊，两边都会出问题。',
    'content': '''## 前端校验管什么

- 即时反馈：格式错了立刻提示，不用等一次往返
- 减少无效请求：必填没填就别发
- 引导输入：手机号分段、密码强度提示

## 前端校验不管什么

**安全**。任何前端校验都能被绕过，权限与合法性必须后端再判一次。

## 常见越界

- 前端做了唯一性校验，后端就不查了 → 并发下必然重复
- 前端限制文件类型，后端直接信任 Content-Type → 传个改扩展名的脚本就进去了
- 前端算价格，后端照单全收 → 改个请求体就能一块钱买

## 结论

前端校验是体验层，后端校验是正确性层。两边都要有，职责不能混。''',
}

DRAFT_ARTICLE = {
    'author': 'chen_yi', 'category': 2, 'tags': ['随笔'],
    'title': '关于"够用就好"的一点想法（草稿）',
    'summary': '草稿，还没想清楚。',
    'content': '''## 待补

- 够用和将就的区别在哪
- 什么时候该重构，什么时候该忍
''',
}

COMMENTS = [
    {'article': '为什么单聊消息序号不用 Redis INCR', 'user': 'wang_ke', 'content': '行锁那段的解释很清楚，之前一直以为 INCR 够用。'},
    {'article': '为什么单聊消息序号不用 Redis INCR', 'user': 'zhou_ning', 'content': '想问下 seq_counter 放在 conversations 表里，会话多了会不会有热点？'},
    {'article': 'Meilisearch 中文分词踩坑记录', 'user': 'su_mu', 'content': '词典方案我们也在用，新词补充确实是个长期活儿。'},
    {'article': 'Nuxt 3 里 401 刷新的正确姿势', 'user': 'wang_ke', 'content': '单例那段救我狗命，之前就是并发刷新把 token 轮换没了。'},
    {'article': '把设计稿变成 token，而不是一堆魔法数字', 'user': 'lin_zhiyuan', 'content': '“只有 token 里有的值可以出现”，这句应该写进规范。'},
    {'article': '列表接口的 N+1，都是这样长出来的', 'user': 'chen_yi', 'content': '压测看 DB QPS 和接口 QPS 的比值，这个指标很实用。'},
    {'article': '把博客当成自己的知识库来写', 'user': 'wang_ke', 'content': '“失败的路也要写”这句说到点子上了，A 为什么不行往往更有用。'},
    {'article': '每天写一百字，比每周写三千字容易', 'user': 'su_mu', 'content': '一百字门槛确实低，我准备试试。'},

]

CONVERSATIONS = [
    ('demo_writer', 'lin_zhiyuan', ['看到你那篇 seq 的文章了，行锁那部分我转给同事了', '有个问题：如果会话跨分库，LAST_INSERT_ID 还成立吗', '我们还没分库，暂时没验证过，等后面拆库时再看']),
    ('wang_ke', 'su_mu', ['401 那篇我照着改了，确实不再互踢了', '单例 + 只重放一次，两个细节都很关键']),
    ('zhou_ning', 'lin_zhiyuan', ['索引状态页挺好用的，失败队列一目了然', '后续想加个失败原因展示']),
]


def ensure_accounts():
    for username, nickname, role, email in ACCOUNTS:
        c = Client()
        status, body = c.req('POST', '/api/v1/auth/register',
                             {'username': username, 'email': email, 'password': PASSWORD})
        if body.get('code') == 0:
            uid = body['data']['user_id']
            sql(f"UPDATE users SET nickname='{nickname}', role={ROLE_ID[role]}, status=1, "
                f"email_verified_at=NOW() WHERE id={uid};")
            print(f'  注册 {username} ({nickname}) uid={uid} role={role}')
        else:
            sql(f"UPDATE users SET nickname='{nickname}', role={ROLE_ID[role]}, status=1, "
                f"email_verified_at=NOW() WHERE username='{username}';")
            print(f'  已存在 {username}（已对齐角色/状态）')


def uid_of(username: str) -> int:
    out = subprocess.run(['mysql', '-h', '127.0.0.1', '-u', 'root', '-p123456', 'blog',
                          '-N', '-e', f"SELECT id FROM users WHERE username='{username}';"],
                         capture_output=True, text=True, encoding='utf-8', errors='ignore')
    return int(out.stdout.strip())


def ensure_tags(admin: Client):
    _, body = admin.req('GET', '/api/v1/tags?limit=100')
    existing = {t['name'] for t in body.get('data', {}).get('items', [])}
    for name, slug in TAGS:
        if name in existing:
            print(f'  标签已存在 {name}')
            continue
        st, res = admin.req('POST', '/api/v1/admin/tags', {'name': name, 'slug': slug})
        print(f'  标签 {name}: {st} {res.get("message")}')


def tag_map(admin: Client) -> dict:
    _, body = admin.req('GET', '/api/v1/tags?limit=100')
    return {t['name']: t['id'] for t in body.get('data', {}).get('items', [])}


def existing_titles(client) -> set:
    _, body = client.req('GET', '/api/v1/users/me/articles?limit=50')
    return {a['title'] for a in body.get('data', {}).get('items', [])}


def create_article(clients, tags, spec, publish=True):
    c = clients[spec['author']]
    if spec['title'] in existing_titles(c):
        print(f'  跳过（已存在）{spec["title"]}')
        return None
    payload = {
        'title': spec['title'],
        'content_md': spec['content'],
        'summary': spec['summary'],
        'category_id': str(spec['category']),
        'tag_ids': [str(tags[t]) for t in spec['tags']],
        'allow_comment': True,
    }
    st, body = c.req('POST', '/api/v1/articles', payload)
    if body.get('code') != 0:
        print(f'  创建失败 {spec["title"]}: {st} {body.get("message")}')
        return None
    aid, slug = body['data']['id'], body['data']['slug']
    if publish:
        c.req('POST', f'/api/v1/articles/{aid}/submit')
        admin = clients['__admin']
        st2, b2 = admin.req('POST', f'/api/v1/admin/articles/{aid}/approve')
        print(f'  发布 {spec["title"]} (id={aid}) approve={b2.get("code") == 0}')
    else:
        print(f'  草稿 {spec["title"]} (id={aid})')
    return {'id': aid, 'slug': slug, 'title': spec['title']}


def main():
    print('== 1. 账号 ==')
    ensure_accounts()
    sql("UPDATE users SET nickname='演示作者', role=2, status=1, email_verified_at=NOW() WHERE username='demo_writer';")

    print('== 2. 登录 ==')
    clients = {}
    for username, *_ in ACCOUNTS:
        c = Client()
        st, body = c.login(username)
        print(f'  {username} 登录 {st}')
        clients[username] = c
    demo = Client(); demo.login('demo_writer'); clients['demo_writer'] = demo
    admin = Client()
    st, _ = admin.login('admin', 'Admin12345')
    print(f'  admin 登录 {st}')
    clients['__admin'] = admin

    print('== 3. 标签 ==')
    ensure_tags(admin)
    tags = tag_map(admin)

    print('== 4. 文章 ==')
    published = []
    for spec in ARTICLES:
        r = create_article(clients, tags, spec, publish=True)
        if r:
            published.append(r)
    pending = create_article(clients, tags, PENDING_ARTICLE, publish=False)
    if pending:
        clients['su_mu'].req('POST', f'/api/v1/articles/{pending["id"]}/submit')
        print('  已提交待审核：', pending['title'])
    create_article(clients, tags, DRAFT_ARTICLE, publish=False)

    print('== 5. 评论 ==')
    # 从全量已发布列表建标题→ID 映射（脚本重跑时新文章会被跳过，不能只靠本次创建结果）
    _, allpub = clients['__admin'].req('GET', '/api/v1/admin/articles?status=published&limit=100')
    by_title = {a['title']: a['id'] for a in allpub.get('data', {}).get('items', [])}
    for cm in COMMENTS:
        aid = by_title.get(cm['article'])
        if not aid:
            continue
        _, exist = clients['__admin'].req('GET', f'/api/v1/articles/{aid}/comments?limit=50')
        if any(c['content'] == cm['content'] for c in exist.get('data', {}).get('items', [])):
            print(f'  跳过（已有评论）{cm["article"][:14]}…')
            continue
        st, body = clients[cm['user']].req('POST', f'/api/v1/articles/{aid}/comments', {'content': cm['content']})
        cid = body.get('data', {}).get('id')
        if cid:
            admin.req('POST', f'/api/v1/admin/comments/{cid}/approve')
        print(f'  {cm["user"]} → {cm["article"][:16]}… {st}')

    print('== 6. 点赞 / 收藏 ==')
    likers = ['wang_ke', 'zhou_ning', 'demo_writer', 'su_mu']
    for i, a in enumerate(published):
        for u in likers[: (i % 3) + 1]:
            clients[u].req('POST', f'/api/v1/articles/{a["id"]}/like')
        for u in likers[1: (i % 2) + 2]:
            clients[u].req('POST', f'/api/v1/articles/{a["id"]}/favorite')
    print(f'  已为 {len(published)} 篇加上点赞与收藏')

    print('== 7. 会话与消息 ==')
    for u1, u2, msgs in CONVERSATIONS:
        peer = uid_of(u2)
        st, body = clients[u1].req('POST', '/api/v1/conversations', {'peer_id': str(peer)})
        cid = body.get('data', {}).get('id')
        if not cid:
            print(f'  {u1} ↔ {u2} 建会话失败: {body.get("message")}')
            continue
        for k, text in enumerate(msgs):
            who = u1 if k % 2 == 0 else u2
            clients[who].req('POST', f'/api/v1/conversations/{cid}/messages',
                             {'type': 'text', 'content': text, 'client_msg_id': f'seed-{cid}-{k}'})
        print(f'  {u1} ↔ {u2} 会话 {cid}：{len(msgs)} 条消息')

    print('== 完成 ==')


if __name__ == '__main__':
    main()
