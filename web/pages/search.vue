<script setup lang="ts">
const q = ref('')
const results = ref<any[] | null>(null)
const searched = ref(false)
const doSearch = async () => {
  if (!q.value.trim()) return
  const d = await api<any>(`/api/v1/search?q=${encodeURIComponent(q.value.trim())}&limit=20`)
  results.value = d.items || d.articles || []
  searched.value = true
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
    <div style="padding:32px 40px 8px;border-bottom:0.5px solid var(--hairline);">
      <h1 class="serif" style="font-size:26px;font-weight:500;color:var(--ink);margin:0 0 16px;">搜索</h1>
      <div style="display:flex;gap:8px;max-width:560px;margin:0 auto 20px;">
        <input v-model="q" @keyup.enter="doSearch" placeholder="输入关键词，Enter 搜索"
          style="flex:1;height:42px;border:0.5px solid #D3D1C7;border-radius:2px;padding:0 14px;font-size:14px;">
        <button class="btn-dark" style="height:42px;padding:0 26px;" @click="doSearch">搜索</button>
      </div>
    </div>
    <div style="max-width:760px;margin:0 auto;padding:12px 40px 40px;">
      <p v-if="searched && results && !results.length" style="padding:40px 0;color:var(--ink-4);font-size:13px;">没有找到相关文章，换个关键词试试。</p>
        <article v-for="a in (results || [])" :key="a.id" class="feed-item">
          <p class="feed-meta">{{ a.category?.name || '未分类' }} · {{ fmt(a.published_at) }} · {{ a.author.nickname }}</p>
          <p class="feed-title"><NuxtLink :to="`/article/${a.slug}`">{{ a.title }}</NuxtLink></p>
          <p class="feed-summary">{{ a.summary }}</p>
        </article>
      <p v-if="!searched" style="padding:20px 0;color:var(--ink-4);font-size:12px;">搜索由 Meilisearch 提供，支持标题与正文关键词。</p>
    </div>
  </div>
</template>
