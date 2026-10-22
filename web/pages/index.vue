<script setup lang="ts">
const base = useUpstream()
const { data } = await useFetch<any>(() => `${base}/api/v1/articles?limit=3`)
const { data: hot } = await useFetch<any>(() => `${base}/api/v1/articles?limit=4`)
const items = computed(() => (data.value as any)?.data?.items || [])
const hotItems = computed(() => (hot.value as any)?.data?.items || [])
// 固定格式，避免服务端/浏览器时区不同导致水合不一致
const pad = (n: number) => String(n).padStart(2, '0')
const fmt = (s?: string | null) => {
  if (!s) return ''
  const d = new Date(s)
  return `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())}`
}
</script>

<template>
  <div>
    <section class="hero">
      <p class="slogan">写作是对思考的最后一次编译。</p>
      <p class="sub">记录 Go、数据库与系统设计的实践与复盘 · 每周三更新</p>
    </section>

    <div class="layout" style="display:grid;grid-template-columns:minmax(0,1fr) 260px;gap:44px;padding:28px 40px 40px;align-items:start;">
      <main>
        <div style="display:flex;gap:8px;margin-bottom:6px;">
          <span style="font-size:12px;color:var(--ink);font-weight:500;border-bottom:2px solid var(--accent);padding-bottom:4px;">最新</span>
        </div>
          <p v-if="!items.length" style="padding:40px 0;color:var(--ink-4);font-size:13px;">暂无文章</p>
          <article v-for="a in items" :key="a.id" class="feed-item">
            <p class="feed-meta">{{ a.category?.name || '未分类' }} · {{ fmt(a.published_at) }} · {{ a.author.nickname }}</p>
            <p class="feed-title"><NuxtLink :to="`/article/${a.slug}`">{{ a.title }}</NuxtLink></p>
            <p class="feed-summary">{{ a.summary }}</p>
            <p style="margin:8px 0 0;font-size:12px;color:var(--ink-4);">阅读 {{ a.view_count }} · 赞 {{ a.like_count }} · 评论 {{ a.comment_count }}</p>
          </article>
        <p style="margin-top:26px;display:flex;justify-content:space-between;align-items:center;">
          <NuxtLink to="/search" class="btn-ghost">浏览全部文章 →</NuxtLink>
          <span style="font-size:11px;color:#D1D5DB;">每页 3 篇 · 右栏吸附跟随</span>
        </p>
      </main>

      <aside class="rail" style="border-left:0.5px solid var(--hairline);padding-left:32px;position:sticky;top:16px;align-self:start;">
        <h4 style="font-size:12px;color:var(--ink);font-weight:500;letter-spacing:2px;margin:0 0 14px;">热门</h4>
          <p v-for="(h, i) in hotItems" :key="h.id" style="margin:0 0 14px;font-family:var(--serif);font-size:14px;color:#374151;line-height:1.6;">
            {{ ['一','二','三','四','五','六'][i] || i + 1 }}、<NuxtLink :to="`/article/${h.slug}`">{{ h.title }}</NuxtLink>
          </p>
          <p v-if="!hotItems.length" style="font-size:12px;color:var(--ink-4);margin:0;">加载中…</p>
        <h4 style="font-size:12px;color:var(--ink);font-weight:500;letter-spacing:2px;margin:26px 0 14px;">标签</h4>
        <p style="margin:0;font-size:12px;color:var(--ink-3);line-height:2.2;">Go · MySQL · WebSocket · 架构 · 部署</p>
        <h4 style="font-size:12px;color:var(--ink);font-weight:500;letter-spacing:2px;margin:26px 0 14px;">订阅</h4>
        <div style="display:flex;gap:6px;">
          <input placeholder="邮箱地址（V1.1）" disabled style="flex:1;border:0.5px solid #D3D1C7;border-radius:2px;height:34px;padding:0 10px;font-size:12px;color:var(--ink-4);">
          <button class="btn-dark" disabled style="opacity:.5;">订阅</button>
        </div>
      </aside>
    </div>
  </div>
</template>
