<script setup lang="ts">
definePageMeta({ layout: 'admin', middleware: 'admin', crumb: ['文章审核', '全站文章'] })
const items = ref<any[]>([])
const status = ref('')
const statusName = (s: string) => ({ draft: '草稿', pending: '待审核', published: '已发布', rejected: '被拒绝', offline: '已下架', deleted: '已删除' } as any)[s] || s
const chip = (s: string) => ({ published: 'chip-green', pending: 'chip-amber', rejected: 'chip-gray', offline: 'chip-gray', deleted: 'chip-gray', draft: 'chip-draft' } as any)[s] || 'chip-gray'
const load = async () => {
  const q = status.value ? `&status=${status.value}` : ''
  items.value = (await api<any>(`/api/v1/admin/articles?limit=20${q}`)).items || []
}
onMounted(load)
const act = async (a: any, action: string, reason = '') => {
  if (action === 'delete' && !confirm('确认删除该文章？')) return
  await api(`/api/v1/admin/articles/${a.id}/${action}`, { method: 'POST', body: reason ? { reason } : undefined })
  load()
}
const del = async (a: any) => {
  const reason = prompt('删除原因（可留空）') || ''
  if (!confirm('确认删除该文章？')) return
  await api(`/api/v1/admin/articles/${a.id}?reason=${encodeURIComponent(reason)}`, { method: 'DELETE' })
  load()
}
</script>

<template>
  <div>
    <div style="display:flex;gap:10px;margin-bottom:16px;">
      <div class="admin-select" v-for="s in ['', 'pending', 'published', 'offline', 'draft']" :key="s" @click="status = s; load()"
        :style="status === s ? 'border-color:var(--ink);color:var(--ink);' : ''">
        {{ s === '' ? '状态：全部' : statusName(s) }}
      </div>
      <span style="margin-left:auto;font-size:12px;color:var(--ink-3);align-self:center;">共 {{ items.length }} 条</span>
    </div>
    <div class="hairline-table">
      <div class="thead" style="display:grid;grid-template-columns:minmax(0,1fr) 90px 96px 100px 150px;">
        <div style="padding:0 14px 0 4px;">标题</div><div>作者</div><div>状态</div><div>数据</div><div style="text-align:right;padding-right:4px;">操作</div>
      </div>
      <div v-for="a in items" :key="a.id" class="trow" style="display:grid;grid-template-columns:minmax(0,1fr) 90px 96px 100px 150px;">
        <div class="cell-title">{{ a.title }}</div>
        <div class="cell">{{ a.author?.nickname }}</div>
        <div><span class="chip" :class="chip(a.status)">{{ statusName(a.status) }}</span></div>
        <div class="cell num-sub" style="line-height:1.5;">{{ a.view_count }} 阅读 · {{ a.like_count }} 赞</div>
        <div class="ops">
          <NuxtLink :to="`/admin/articles/${a.id}`" class="link-u">详情</NuxtLink>
          <span v-if="a.status === 'pending'" class="link-u">通过</span>
          <span v-if="a.status === 'published'" class="link-u" @click="act(a, 'offline', '管理端下架')">下架</span>
          <span v-if="a.status === 'offline'" class="link-u" @click="act(a, 'relist')">上架</span>
          <span class="link-dim" @click="del(a)">删除</span>
        </div>
      </div>
      <p v-if="!items.length" style="padding:24px 4px;font-size:13px;color:var(--ink-4);">没有文章。</p>
    </div>
  </div>
</template>
