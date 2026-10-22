<script setup lang="ts">
const route = useRoute()
const base = useUpstream()
const { data } = await useFetch<any>(() => `${base}/api/v1/users/${route.params.id}`)
const { data: arts } = await useFetch<any>(() => `${base}/api/v1/articles?author_id=${route.params.id}&limit=10`)
const u = computed(() => (data.value as any)?.data || {})
const items = computed(() => (arts.value as any)?.data?.items || [])
// 固定格式，避免时区差异导致水合不一致
const pad = (n: number) => String(n).padStart(2, '0')
const fmt = (s?: string | null) => {
  if (!s) return ''
  const d = new Date(s)
  return `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())}`
}
</script>

<template>
  <div class="container">
    <div class="user-head">
      <div class="ava-big">{{ (u.nickname || u.username || '?').slice(0, 1) }}</div>
      <div style="min-width:0;">
        <h1>{{ u.nickname || u.username }}</h1>
        <p class="bio">{{ u.bio || '这个人很懒，什么都没有留下。' }}</p>
        <p class="meta">注册于 {{ u.created_at ? fmt(u.created_at) : '—' }}</p>
      </div>
    </div>
    <div style="max-width:760px;padding:12px 0 40px;">
        <p v-if="!items.length" style="padding:40px 0;color:var(--ink-4);font-size:13px;">还没有公开文章。</p>
        <article v-for="a in items" :key="a.id" class="feed-item">
          <p class="feed-meta">{{ a.category?.name || '未分类' }} · {{ fmt(a.published_at) }}</p>
          <p class="feed-title"><NuxtLink :to="`/article/${a.slug}`">{{ a.title }}</NuxtLink></p>
          <p class="feed-summary">{{ a.summary }}</p>
          <p style="margin:8px 0 0;font-size:12px;color:var(--ink-4);">阅读 {{ a.view_count }} · 赞 {{ a.like_count }}</p>
        </article>
    </div>
  </div>
</template>
