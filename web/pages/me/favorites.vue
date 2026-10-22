<script setup lang="ts">
definePageMeta({ layout: 'workspace', middleware: 'auth' })
const { data } = await useAsyncData('me-favorites', () => api<any>('/api/v1/users/me/favorites?limit=20'))
const items = computed(() => (data.value as any)?.items || [])
const unfav = async (a: any) => {
  await api(`/api/v1/articles/${a.id}/favorite`, { method: 'DELETE' })
  location.reload()
}
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
    <h1>我的收藏（{{ items.length }}）</h1>
    <p v-if="!items.length" style="padding:24px 0;color:var(--ink-4);font-size:13px;">还没有收藏任何文章。</p>
    <div v-for="a in items" :key="a.id" style="display:flex;align-items:center;gap:14px;padding:14px 0;border-bottom:0.5px solid var(--hairline);">
      <div style="min-width:0;flex:1;">
        <p class="feed-title" style="margin:0 0 2px;font-size:15px;"><NuxtLink :to="`/article/${a.slug}`">{{ a.title }}</NuxtLink></p>
        <p style="margin:0;font-size:12px;color:var(--ink-4);">{{ a.author?.nickname }} · 收藏于 {{ fmt(a.published_at) }}</p>
      </div>
      <span class="btn-ghost-sm" style="border:none;color:var(--ink-4);cursor:pointer;" @click="unfav(a)">取消收藏</span>
    </div>
  </div>
</template>
