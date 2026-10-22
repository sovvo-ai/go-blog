<script setup lang="ts">
definePageMeta({ layout: 'admin', crumb: ['文章审核', '审核详情'] })
const route = useRoute()
const a = ref<any>(null)
onMounted(async () => { a.value = await api<any>(`/api/v1/admin/articles/${route.params.id}`) })
const act = async (action: string) => {
  let reason = ''
  if (action === 'reject' || action === 'offline') {
    reason = prompt('理由（必填）') || ''
    if (!reason) { alert('理由必填'); return }
  }
  const body = reason ? { reason } : undefined
  await api(`/api/v1/admin/articles/${a.value.id}/${action}`, { method: 'POST', body })
  a.value = await api<any>(`/api/v1/admin/articles/${a.value.id}`)
}
const del = async () => {
  const reason = prompt('删除原因（可留空）') || ''
  if (!confirm('确认删除？')) return
  await api(`/api/v1/admin/articles/${a.value.id}?reason=${encodeURIComponent(reason)}`, { method: 'DELETE' })
  navigateTo('/admin/articles')
}
// 固定格式，避免服务端/浏览器时区不同导致水合不一致
const pad = (n: number) => String(n).padStart(2, '0')
const fmt = (s?: string | null) => { if (!s) return '—'
  const d = new Date(s)
  return `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())}` + ` ${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}`
}
</script>

<template>
  <div>
    <p v-if="!a" style="color:var(--ink-4);font-size:13px;">加载中…</p>
    <div v-else>
      <div style="display:flex;align-items:center;margin-bottom:18px;">
        <NuxtLink to="/admin/articles" class="link-dim" style="font-size:12px;">← 返回列表</NuxtLink>
        <div style="margin-left:auto;display:flex;gap:10px;">
          <button v-if="a.status === 'pending'" class="btn-dark" style="height:36px;padding:0 20px;" @click="act('approve')">通过并发布</button>
          <button v-if="a.status === 'pending'" class="btn-ghost-sm" style="color:#DC2626;border-color:#DC2626;" @click="act('reject')">拒绝…</button>
          <button v-if="a.status === 'published'" class="btn-ghost-sm" @click="act('offline')">下架…</button>
          <button v-if="a.status === 'offline'" class="btn-ghost-sm" @click="act('relist')">上架</button>
          <button class="btn-ghost-sm" @click="del">删除</button>
        </div>
      </div>
      <div style="display:grid;grid-template-columns:minmax(0,1fr) 280px;gap:24px;align-items:start;">
        <div style="border:0.5px solid var(--hairline);padding:24px;">
          <p style="margin:0 0 8px;font-size:12px;color:var(--ink-4);">
            {{ a.category_name || '未分类' }} · 提交于 {{ fmt(a.created_at) }} · 版本 v{{ a.version }}
            <span v-if="a.deleted" class="chip chip-gray" style="margin-left:6px;">已删除</span>
          </p>
          <h1 class="serif" style="font-size:24px;color:var(--ink);margin:0 0 16px;">{{ a.title }}</h1>
          <div class="prose" style="font-size:15px;color:var(--ink-2);line-height:1.9;" v-html="a.content_html || `<pre style='white-space:pre-wrap;font-family:var(--sans);'>${a.content_md || ''}</pre>`" />
        </div>
        <div style="display:flex;flex-direction:column;gap:16px;">
          <div style="border:0.5px solid var(--hairline);padding:16px;">
            <p style="margin:0 0 10px;font-size:12px;letter-spacing:2px;color:var(--ink);font-weight:500;">元信息</p>
            <p style="margin:0;font-size:13px;color:var(--ink-2);line-height:2.1;">
              作者：{{ a.author_name || '—' }}（<NuxtLink :to="`/user/${a.author_id}`" class="link-u">主页</NuxtLink>）<br>
              Slug：{{ a.slug }}<br>
              摘要：{{ a.summary || '—' }}
            </p>
          </div>
          <div style="border:0.5px solid var(--hairline);padding:16px;">
            <p style="margin:0 0 10px;font-size:12px;letter-spacing:2px;color:var(--ink);font-weight:500;">状态与数据</p>
            <p style="margin:0;font-size:13px;color:var(--ink-2);line-height:2.1;">
              状态：<span class="chip" :class="a.status === 'published' ? 'chip-green' : a.status === 'pending' ? 'chip-amber' : 'chip-gray'">{{ a.status }}</span><br>
              阅读 {{ a.view_count ?? 0 }} · 赞 {{ a.like_count ?? 0 }} · 收藏 {{ a.favorite_count ?? 0 }}<br>
              发布于：{{ fmt(a.published_at) }}
            </p>
            <p v-if="a.reject_reason" style="margin:10px 0 0;font-size:12px;color:var(--danger);">拒绝理由：{{ a.reject_reason }}</p>
            <p v-if="a.offline_reason" style="margin:10px 0 0;font-size:12px;color:var(--amber);">下架理由：{{ a.offline_reason }}</p>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
