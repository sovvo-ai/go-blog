<script setup lang="ts">
const route = useRoute()
const key = computed(() => route.params.slug as string || '')
const base = useUpstream()
const { data } = await useFetch<any>(() => `${base}/api/v1/articles?category=${key.value}&limit=10`)
const items = computed(() => (data.value as any)?.data?.items || [])
// 固定格式，避免服务端/浏览器时区不同导致水合不一致
const pad = (n: number) => String(n).padStart(2, '0')
const fmt = (s?: string | null) => {
  if (!s) return ''
  const d = new Date(s)
  return `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())}`
}
const title = computed(() => `分类：${items.value[0]?.category?.name || key.value}`)
useHead({ title: () => `${title.value} · 墨痕` })
</script>

<template>
  <div>
    <div style="padding:32px 40px 8px;border-bottom:0.5px solid var(--hairline);">
      <h1 class="serif" style="font-size:26px;font-weight:500;color:var(--ink);margin:0 0 6px;">{{ title }}</h1>
      <p style="margin:0 0 20px;font-size:13px;color:var(--ink-4);">共 {{ items.length }} 篇 · 按发布时间排序</p>
    </div>
    <div style="max-width:760px;margin:0 auto;padding:12px 40px 40px;">
        <p v-if="!items.length" style="padding:40px 0;color:var(--ink-4);font-size:13px;">该分类下暂无文章</p>
        <article v-for="a in items" :key="a.id" class="feed-item">
          <p class="feed-meta">{{ fmt(a.published_at) }} · {{ a.author.nickname }}</p>
          <p class="feed-title"><NuxtLink :to="`/article/${a.slug}`">{{ a.title }}</NuxtLink></p>
          <p class="feed-summary">{{ a.summary }}</p>
          <p style="margin:8px 0 0;font-size:12px;color:var(--ink-4);">阅读 {{ a.view_count }} · 赞 {{ a.like_count }}</p>
        </article>
    </div>
  </div>
</template>
