# -*- coding: utf-8 -*-
"""管理端补充页生成：概览（进生成器体系）、审核详情（F6）、索引状态（10.6）、管理登录。
复用 gen_protos 的模板函数；独立运行：python tools/gen_admin_extra.py"""
import gen_protos as g


def gen_admin_index():
    """管理端概览（发丝线统计带 + 黑色图表 + 待审核列表）"""
    content = '''<div class="stat-band">
      <div class="cell"><p class="label">文章总数</p><p class="num">128</p><p class="delta" style="color:var(--green);">今日 +3</p></div>
      <div class="cell"><p class="label">用户总数</p><p class="num">56</p><p class="delta" style="color:var(--green);">本周 +5</p></div>
      <div class="cell warn"><p class="label">待审核</p><p class="num">7</p><p class="delta">文章 5 · 评论 2</p></div>
      <div class="cell"><p class="label">今日访问</p><p class="num">1,842</p><p class="delta">较昨日 +12%</p></div>
    </div>
    <div style="margin-top:20px;border:0.5px solid var(--hairline);padding:16px;">
      <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:12px;">
        <p style="margin:0;font-family:var(--serif);font-size:14px;color:var(--ink);">近 14 天发文与访问</p>
        <div style="display:flex;gap:12px;font-size:11px;color:var(--ink-3);">
          <span style="display:flex;align-items:center;gap:4px;"><span style="width:10px;height:10px;background:var(--ink);display:inline-block;"></span>发文</span>
          <span style="display:flex;align-items:center;gap:4px;"><span style="width:10px;height:10px;background:#D3D1C7;display:inline-block;"></span>访问</span>
        </div>
      </div>
      <svg width="100%" height="100" viewBox="0 0 640 100" preserveAspectRatio="none">
        <path d="M0 80 C60 78 90 62 140 64 S240 34 300 46 S420 24 480 32 S580 16 640 20" fill="none" stroke="#D3D1C7" stroke-width="2"/>
        <rect x="20" y="74" width="10" height="20" fill="#111827"/><rect x="90" y="70" width="10" height="24" fill="#111827"/>
        <rect x="160" y="66" width="10" height="28" fill="#111827"/><rect x="230" y="60" width="10" height="34" fill="#111827"/>
        <rect x="300" y="64" width="10" height="30" fill="#111827"/><rect x="370" y="56" width="10" height="38" fill="#111827"/>
        <rect x="440" y="62" width="10" height="32" fill="#111827"/><rect x="510" y="50" width="10" height="44" fill="#111827"/>
        <rect x="580" y="54" width="10" height="40" fill="#111827"/>
      </svg>
    </div>
    <div style="margin-top:20px;">
      <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:10px;">
        <p style="margin:0;font-family:var(--serif);font-size:14px;color:var(--ink);">待审核文章</p>
        <span style="font-size:12px;color:var(--ink);border-bottom:0.5px solid var(--ink);"><a href="/admin/articles.html">查看全部 →</a></span>
      </div>
      <div style="border-top:0.5px solid var(--hairline);">
        <div style="display:flex;align-items:center;gap:12px;padding:12px 4px;font-size:13px;border-bottom:0.5px solid var(--hairline);">
          <span style="color:var(--ink);flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-family:var(--serif);">单聊消息序号：为什么不用 Redis INCR</span>
          <span style="font-size:12px;color:var(--ink-4);">阿张 · 2 小时前</span>
          <a class="link-u" href="/admin/article-detail.html">审核</a>
        </div>
        <div style="display:flex;align-items:center;gap:12px;padding:12px 4px;font-size:13px;border-bottom:0.5px solid var(--hairline);">
          <span style="color:var(--ink);flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-family:var(--serif);">Meilisearch 中文分词踩坑记录</span>
          <span style="font-size:12px;color:var(--ink-4);">阿李 · 3 小时前</span>
          <a class="link-u" href="/admin/article-detail.html">审核</a>
        </div>
        <div style="display:flex;align-items:center;gap:12px;padding:12px 4px;font-size:13px;border-bottom:0.5px solid var(--hairline);">
          <span style="color:var(--ink);flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-family:var(--serif);">Docker Compose 部署完整清单</span>
          <span style="font-size:12px;color:var(--ink-4);">阿王 · 昨天</span>
          <a class="link-u" href="/admin/article-detail.html">审核</a>
        </div>
      </div>
    </div>'''
    g.admin_page("admin/index.html", "概览", "概览",
                 '<span class="page serif">概览</span><span class="crumb-sep">/</span> 站点数据', content)


def gen_admin_detail():
    """管理端审核详情（F6 接口对应的页面）"""
    content = '''<div style="display:flex;align-items:center;margin-bottom:18px;">
  <span style="font-size:12px;color:var(--ink-3);">← <a class="link-u" href="/admin/articles.html">返回列表</a></span>
  <div style="margin-left:auto;display:flex;gap:10px;">
    <button class="btn-dark" style="height:36px;padding:0 20px;">通过并发布</button>
    <span class="btn-ghost-sm" style="color:#DC2626;border-color:#DC2626;">拒绝…</span>
    <span class="btn-ghost-sm">删除</span>
  </div>
</div>
<div style="display:grid;grid-template-columns:minmax(0,1fr) 280px;gap:24px;align-items:start;">
  <div style="border:0.5px solid var(--hairline);padding:24px;">
    <p style="margin:0 0 8px;font-size:12px;color:var(--ink-4);">架构 · 提交于 2026-10-22 08:40 · 版本 v3</p>
    <h1 style="font-family:var(--serif);font-size:24px;color:var(--ink);margin:0 0 16px;">单聊消息序号：为什么不用 Redis INCR</h1>
    <div style="font-size:15px;color:var(--ink-2);line-height:1.9;">
      <p style="margin:0 0 14px;">多实例并发写入时，全局自增 id 与事务提交顺序可能不一致。会话内需要严格单调的 seq……</p>
      <div style="background:var(--paper-2);border:0.5px solid var(--hairline);border-radius:4px;padding:12px 16px;font-family:var(--mono);font-size:13px;line-height:1.8;">UPDATE conversations<br>&nbsp;&nbsp;SET seq_counter = LAST_INSERT_ID(seq_counter + 1)<br>&nbsp;WHERE id = ?;</div>
    </div>
  </div>
  <div style="display:flex;flex-direction:column;gap:16px;">
    <div style="border:0.5px solid var(--hairline);padding:16px;">
      <p style="margin:0 0 10px;font-size:12px;letter-spacing:2px;color:var(--ink);font-weight:500;">元信息</p>
      <p style="margin:0;font-size:13px;color:var(--ink-2);line-height:2.1;">作者：阿张（<a class="link-u" href="/user.html">主页</a>）<br>分类：架构<br>标签：MySQL · Go<br>Slug：chat-seq-not-redis</p>
    </div>
    <div style="border:0.5px solid var(--hairline);padding:16px;">
      <p style="margin:0 0 10px;font-size:12px;letter-spacing:2px;color:var(--ink);font-weight:500;">状态与数据</p>
      <p style="margin:0;font-size:13px;color:var(--ink-2);line-height:2.1;">状态：<span class="chip chip-amber">待审核</span><br>阅读 0 · 赞 0 · 评论 0<br>乐观锁版本：v3</p>
    </div>
    <div style="border:0.5px solid var(--hairline);padding:16px;">
      <p style="margin:0 0 10px;font-size:12px;letter-spacing:2px;color:var(--ink);font-weight:500;">拒绝理由（拒绝时必填）</p>
      <textarea placeholder="例如：内容与分类不符 / 存在未引用的断言…" style="width:100%;min-height:70px;border:0.5px solid #D3D1C7;border-radius:2px;padding:8px 10px;font-size:12px;font-family:var(--sans);"></textarea>
    </div>
  </div>
</div>'''
    g.admin_page("admin/article-detail.html", "审核详情", "文章审核",
                 '<span class="page serif">文章审核</span><span class="crumb-sep">/</span> 审核详情', content)


def gen_admin_index_status():
    """索引状态与重建（TECH_DESIGN 10.6 / GET /admin/index-failures + rebuild）"""
    rows = ""
    for slug, err, retry in [
        ("meilisearch-sync", "网络超时（ETIMEDOUT）×3", "自动重试中 · 下次 2 分钟后"),
        ("spec-first-practice", "文档体积超限（>2MB）", "需人工处理"),
    ]:
        rows += f'''<div class="trow" style="display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr) 200px 110px;">
  <div class="cell-title">{slug}</div><div class="cell">{err}</div>
  <div class="cell">{retry}</div>
  <div class="ops"><span class="link-u">立即重试</span></div>
</div>'''
    content = f'''
<div class="stat-band" style="grid-template-columns:repeat(3,1fr);margin-bottom:20px;">
  <div class="cell"><p class="label">索引文档数</p><p class="num">126</p><p class="delta">库内文章 128 · 差 2</p></div>
  <div class="cell warn"><p class="label">同步失败</p><p class="num">2</p><p class="delta">1 自动重试 · 1 待人工</p></div>
  <div class="cell"><p class="label">最近全量重建</p><p class="num" style="font-size:16px;font-family:var(--serif);">10-21 23:00</p><p class="delta">耗时 41 秒</p></div>
</div>
<div style="display:flex;align-items:center;margin-bottom:14px;">
  <span style="font-family:var(--serif);font-size:14px;color:var(--ink);">同步失败队列</span>
  <span class="btn-dark" style="margin-left:auto;height:34px;padding:0 18px;">全部重试</span>
</div>
<div class="hairline-table">
  <div class="thead" style="display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr) 200px 110px;">
    <div style="padding:0 14px 0 4px;">文章 Slug</div><div>最近错误</div><div>状态</div><div style="text-align:right;padding-right:4px;">操作</div>
  </div>{rows}
</div>
<div style="display:flex;align-items:center;gap:12px;margin-top:22px;border:0.5px solid var(--hairline);padding:16px;">
  <div style="flex:1;">
    <p style="margin:0;font-family:var(--serif);font-size:14px;color:var(--ink);">全量重建</p>
    <p style="margin:4px 0 0;font-size:12px;color:var(--ink-4);">清空索引后从数据库重新推送全部已发布文章，期间搜索结果短暂不完整。</p>
  </div>
  <span class="btn-ghost-sm" style="color:#DC2626;border-color:#DC2626;">执行重建…</span>
</div>'''
    g.admin_page("admin/index-status.html", "索引状态", "索引状态",
                 '<span class="page serif">索引状态</span><span class="crumb-sep">/</span> Meilisearch 同步', content)


if __name__ == "__main__":
    g.auth_page("admin/login.html", "管理员登录", "管理员登录",
                [("账号", "text", "", ""), ("密码", "password", "", "")],
                '<a href="/login.html">返回用户端 →</a>', btn="登 录")
    gen_admin_index()
    gen_admin_detail()
    gen_admin_index_status()
    print("admin extra pages ok")
