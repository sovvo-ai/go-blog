<script setup lang="ts">
definePageMeta({ layout: 'admin', middleware: 'admin', crumb: ['举报处理', '举报队列'] })
const items = ref<any[]>([])
const st = ref('pending')
const load = async () => {
  const q = st.value ? `&status=${st.value}` : ''
  items.value = (await api<any>(`/api/v1/admin/reports?limit=30${q}`)).items || []
}
onMounted(load)
const stName = (s: string) => ({ pending: '待处理', accepted: '已受理', rejected: '已驳回' } as any)[s] || s
const chip = (s: string) => ({ pending: 'chip-amber', accepted: 'chip-green', rejected: 'chip-gray' } as any)[s] || 'chip-gray'
const handle = async (r: any, result: string) => {
  const note = prompt('处理备注（可留空）') || ''
  await api(`/api/v1/admin/reports/${r.id}`, { method: 'PUT', body: { result, note, action: result === 'accepted' ? 'offline' : undefined } })
  load()
}
</script>

<template>
  <div>
    <div style="display:flex;gap:10px;margin-bottom:16px;">
      <div v-for="s in ['pending', 'accepted', 'rejected']" :key="s" class="admin-select" @click="st = s; load()"
        :style="st === s ? 'border-color:var(--ink);color:var(--ink);' : ''">{{ stName(s) }}</div>
    </div>
    <div class="hairline-table">
      <div class="thead" style="display:grid;grid-template-columns:90px 90px minmax(0,1fr) 110px 96px 150px;">
        <div style="padding:0 14px 0 4px;">举报人</div><div>类型</div><div>对象</div><div>理由</div><div>状态</div><div style="text-align:right;padding-right:4px;">操作</div>
      </div>
      <div v-for="r in items" :key="r.id" class="trow" style="display:grid;grid-template-columns:90px 90px minmax(0,1fr) 110px 96px 150px;">
        <div class="cell">{{ r.reporter?.nickname || r.reporter_id }}</div>
        <div class="cell">{{ r.target_type }}</div>
        <div class="cell-title">{{ r.target_type }} #{{ r.target_id }}<span v-if="r.detail" style="color:var(--ink-4);"> · {{ r.detail }}</span></div>
        <div class="cell">{{ r.reason }}</div>
        <div><span class="chip" :class="chip(r.status)">{{ stName(r.status) }}</span></div>
        <div class="ops">
          <template v-if="r.status === 'pending'">
            <span class="link-u" @click="handle(r, 'accepted')">受理</span>
            <span class="link-u" @click="handle(r, 'rejected')">驳回</span>
          </template>
          <span v-else class="link-dim">已处理</span>
        </div>
      </div>
      <p v-if="!items.length" style="padding:24px 4px;font-size:13px;color:var(--ink-4);">队列为空。</p>
    </div>
  </div>
</template>
