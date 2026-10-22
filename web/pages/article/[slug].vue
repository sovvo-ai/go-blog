<script setup lang="ts">
const route = useRoute()
const base = useUpstream()
const { data, error } = await useFetch<any>(() => `${base}/api/v1/articles/${route.params.slug}`)
if (error.value || !data.value) {
  const code = (error.value as any)?.statusCode
  showError({ statusCode: code === 410 ? 410 : 404 })
}
const a = computed(() => (data.value as any)?.data || {})
// 固定格式，避免服务端/浏览器时区不同导致水合不一致
const pad = (n: number) => String(n).padStart(2, '0')
const fmt = (s?: string | null) => { if (!s) return ''
  const d = new Date(s)
  return `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())}` + ` ${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}`
}
const toc = computed(() => {
  const out: { id: string; text: string }[] = []
  const html = String(a.value.content_html || '')
  html.replace(/<h2[^>]*id="([^"]+)"[^>]*>(.*?)<\/h2>/g, (_m, id, text) => { out.push({ id, text: text.replace(/<[^>]+>/g, '') }); return _m })
  return out
})
// SEO：标题、描述、canonical、OG/Twitter 卡片（爬虫与分享链接都靠这一段）
const siteUrl = String(useRuntimeConfig().public.siteUrl).replace(/\/$/, '')
useHead(() => {
  const title = a.value.title ? `${a.value.title} · 墨痕` : '文章 · 墨痕'
  const desc = String(a.value.summary || '').slice(0, 120)
  const url = a.value.slug ? `${siteUrl}/article/${a.value.slug}` : siteUrl
  const meta: any[] = [
    { name: 'description', content: desc },
    { property: 'og:type', content: 'article' },
    { property: 'og:title', content: a.value.title || '墨痕' },
    { property: 'og:description', content: desc },
    { property: 'og:url', content: url },
    { name: 'twitter:card', content: 'summary_large_image' }
  ]
  if (a.value.cover_url) meta.push({ property: 'og:image', content: a.value.cover_url })
  if (a.value.published_at) meta.push({ property: 'article:published_time', content: a.value.published_at })
  return { title, meta, link: [{ rel: 'canonical', href: url }] }
})

// 写操作失败要给出提示：未登录/被限流时不能让点击毫无反馈
const actionErr = ref('')
const act = async (path: string, okMsg: string) => {
  actionErr.value = ''
  try {
    await api(path, { method: 'POST' })
    location.reload()
  } catch (e: any) {
    if (e?.statusCode === 401 || e?.data?.code === 10002) {
      actionErr.value = '请先登录后再操作'
      return
    }
    actionErr.value = e?.data?.message || e?.message || `${okMsg}失败，请稍后重试`
  }
}
const like = () => act(`/api/v1/articles/${a.value.id}/like`, '点赞')
const fav = () => act(`/api/v1/articles/${a.value.id}/favorite`, '收藏')
</script>

<template>
  <div class="container" style="display:grid;grid-template-columns:200px minmax(0,1fr);gap:40px;padding:32px 40px;">
    <aside class="toc" v-if="toc.length">
      <h5>目录</h5>
      <a v-for="t in toc" :key="t.id" :href="`#${t.id}`">{{ t.text }}</a>
      <h5 style="margin-top:24px;">本文标签</h5>
      <div><span v-for="t in (a.tags || [])" :key="t.id" class="tag-chip">{{ t.name }}</span></div>
    </aside>

    <main style="min-width:0;">
        <p class="feed-meta">{{ a.category?.name || '未分类' }} · {{ fmt(a.published_at) }} · 阅读 {{ a.view_count }}</p>
      <h1 class="serif" style="font-size:30px;font-weight:500;color:var(--ink);line-height:1.4;margin:0 0 14px;">{{ a.title }}</h1>
      <div class="author-bar">
        <div class="avatar-dot" style="width:32px;height:32px;font-size:12px;">{{ (a.author?.nickname || '?').slice(0, 1) }}</div>
        <div>
          <p class="name"><NuxtLink :to="`/user/${a.author?.id}`">{{ a.author?.nickname }}</NuxtLink></p>
        </div>
      </div>
      <div class="prose" v-html="a.content_html" />
      <div class="actions">
        <span @click="like">{{ a.is_liked ? '♥' : '♡' }} 赞 {{ a.like_count }}</span>
        <span @click="fav">{{ a.is_favorited ? '★' : '☆' }} 收藏 {{ a.favorite_count }}</span>
        <span v-if="actionErr" style="color:var(--danger);font-size:12px;">{{ actionErr }}</span>
        <span style="color:var(--ink-3);">评论 {{ a.comment_count }}</span>
      </div>
    </main>
  </div>
</template>
