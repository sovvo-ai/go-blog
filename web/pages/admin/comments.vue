<script setup lang="ts">
definePageMeta({ layout: 'admin', middleware: 'admin', crumb: ['评论管理', '全部评论'] })
const items = ref<any[]>([])
const status = ref('')
const load = async () => {
  const q = status.value ? `&status=${status.value}` : ''
  items.value = (await api<any>(`/api/v1/admin/comments?limit=30${q}`)).items || []
}
onMounted(load)
const stName = (s: string) => ({ approved: '已通过', pending: '待审核', rejected: '已拒绝', deleted: '已删除' } as any)[s] || s
const chip = (s: string) => ({ approved: 'chip-green', pending: 'chip-amber', rejected: 'chip-gray', deleted: 'chip-gray' } as any)[s] || 'chip-gray'
const act = async (c: any, action: string) => {
  await api(`/api/v1/admin/comments/${c.id}/${action}`, { method: 'POST' })
  load()
}
const del = async (c: any) => {
  if (!confirm('确认删除该评论？')) return
  await api(`/api/v1/admin/comments/${c.id}`, { method: 'DELETE' })
  load()
}
</script>

<template>
  <div>
    <div style="display:flex;gap:10px;margin-bottom:16px;">
      <div v-for="s in ['', 'pending', 'approved']" :key="s" class="admin-select" @click="status = s; load()"
        :style="status === s ? 'border-color:var(--ink);color:var(--ink);' : ''">
        {{ s === '' ? '状态：全部' : stName(s) }}
      </div>
    </div>
    <div class="hairline-table">
      <div class="thead" style="display:grid;grid-template-columns:minmax(0,1fr) 160px 90px 96px 140px;">
        <div style="padding:0 14px 0 4px;">评论内容</div><div>所属文章</div><div>作者</div><div>状态</div><div style="text-align:right;padding-right:4px;">操作</div>
      </div>
      <div v-for="c in items" :key="c.id" class="trow" style="display:grid;grid-template-columns:minmax(0,1fr) 160px 90px 96px 140px;">
        <div class="cell-title" style="white-space:normal;">{{ c.content }}</div>
        <div class="cell">{{ c.article?.title || '—' }}</div>
        <div class="cell">{{ c.user?.nickname || '—' }}</div>
        <div><span class="chip" :class="chip(c.status)">{{ stName(c.status) }}</span></div>
        <div class="ops">
          <template v-if="c.status === 'pending'"><span class="link-u" @click="act(c, 'approve')">通过</span><span class="link-u" @click="act(c, 'reject')">拒绝</span></template>
          <span class="link-u" @click="del(c)">删除</span>
        </div>
      </div>
      <p v-if="!items.length" style="padding:24px 4px;font-size:13px;color:var(--ink-4);">没有评论。</p>
    </div>
  </div>
</template>
