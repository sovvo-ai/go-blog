<script setup lang="ts">
definePageMeta({ layout: 'workspace', middleware: 'auth' })
const { data } = await useAsyncData('me-articles', () => api<any>('/api/v1/users/me/articles?limit=20'))
const items = computed(() => (data.value as any)?.items || [])
const filter = ref('全部')
const statusName = (s: string) => ({ draft: '草稿', pending: '待审核', published: '已发布', rejected: '被拒绝', offline: '已下架' } as any)[s] || s
const chip = (s: string) => ({ draft: 'chip-draft', pending: 'chip-amber', published: 'chip-green', rejected: 'chip-gray', offline: 'chip-gray' } as any)[s] || 'chip-gray'
const filtered = computed(() => filter.value === '全部' ? items.value : items.value.filter((a: any) => statusName(a.status) === filter.value))
const withdraw = async (a: any) => { await api(`/api/v1/articles/${a.id}/withdraw`, { method: 'POST' }); location.reload() }
const del = async (a: any) => { if (!confirm('确认删除这篇文章？')) return; await api(`/api/v1/articles/${a.id}`, { method: 'DELETE' }); location.reload() }
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
    <div style="display:flex;align-items:center;margin-bottom:18px;">
      <h1 style="margin:0;">我的文章</h1>
      <NuxtLink to="/write" class="btn-dark" style="margin-left:auto;height:36px;padding:0 20px;text-decoration:none;">+ 写新文章</NuxtLink>
    </div>
    <div style="display:flex;gap:8px;margin-bottom:14px;">
      <span v-for="f in ['全部', '草稿', '待审核', '已发布', '已下架']" :key="f" @click="filter = f"
        :style="filter === f ? 'font-size:12px;padding:5px 14px;background:var(--ink);color:#fff;border-radius:2px;cursor:pointer;' : 'font-size:12px;padding:5px 14px;color:var(--ink-3);border:0.5px solid #D3D1C7;border-radius:2px;cursor:pointer;'">{{ f }}</span>
    </div>
    <div class="hairline-table">
      <div class="thead" style="display:grid;grid-template-columns:minmax(0,1fr) 90px 96px 100px 150px;">
        <div style="padding:0 14px 0 4px;">标题</div><div>分类</div><div>状态</div><div>更新时间</div><div style="text-align:right;padding-right:4px;">操作</div>
      </div>
      <div v-for="a in filtered" :key="a.id" class="trow" style="display:grid;grid-template-columns:minmax(0,1fr) 90px 96px 100px 150px;">
        <div class="cell-title">{{ a.title }}</div>
        <div class="cell">{{ a.category?.name || '—' }}</div>
        <div><span class="chip" :class="chip(a.status)">{{ statusName(a.status) }}</span></div>
        <div class="cell">{{ fmt(a.updated_at || a.published_at) }}</div>
        <div class="ops">
          <NuxtLink v-if="a.status === 'published'" :to="`/article/${a.slug}`" class="link-dim">查看</NuxtLink>
          <span v-if="['draft', 'rejected', 'offline'].includes(a.status)" class="link-u">编辑</span>
          <span v-if="a.status === 'draft'" class="link-u">提交审核</span>
          <span v-if="a.status === 'published'" class="link-u" @click="withdraw(a)">下架</span>
          <span class="link-u" @click="del(a)">删除</span>
        </div>
      </div>
      <p v-if="!filtered.length" style="padding:24px 4px;font-size:13px;color:var(--ink-4);">这个状态下没有文章。</p>
    </div>
  </div>
</template>
