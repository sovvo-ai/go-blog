<script setup lang="ts">
definePageMeta({ layout: 'admin', middleware: 'admin', crumb: ['标签', '增删改与合并'] })
const items = ref<any[]>([])
const name = ref('')
const load = async () => { items.value = (await api<any>('/api/v1/admin/tags?limit=100')).items || [] }
onMounted(load)
const create = async () => {
  if (!name.value.trim()) return
  const r = await apiRaw('/api/v1/admin/tags', { method: 'POST', body: { name: name.value.trim() } })
  if (r.code !== 0) { alert(r.message); return }
  name.value = ''; load()
}
const del = async (t: any) => {
  if (!confirm(`删除标签「${t.name}」？`)) return
  await api(`/api/v1/admin/tags/${t.id}`, { method: 'DELETE' })
  load()
}
const merge = async () => {
  const from = prompt('被合并的标签名（将删除）')
  if (!from) return
  const to = prompt('合并到的目标标签名')
  if (!to) return
  const r = await apiRaw('/api/v1/admin/tags/merge', { method: 'POST', body: { from, to } })
  alert(r.code === 0 ? '合并完成' : r.message)
  load()
}
</script>

<template>
  <div>
    <div style="display:flex;gap:8px;align-items:center;margin-bottom:16px;">
      <input v-model="name" placeholder="标签名" @keyup.enter="create" style="width:160px;height:36px;border:0.5px solid #D3D1C7;border-radius:2px;padding:0 12px;font-size:12px;">
      <button class="btn-dark" style="height:36px;padding:0 18px;" @click="create">+ 新建标签</button>
      <button class="btn-ghost-sm" @click="merge">合并标签…</button>
    </div>
    <div class="hairline-table">
      <div class="thead" style="display:grid;grid-template-columns:minmax(0,1fr) 90px 150px;">
        <div style="padding:0 14px 0 4px;">名称</div><div>文章数</div><div style="text-align:right;padding-right:4px;">操作</div>
      </div>
      <div v-for="t in items" :key="t.id" class="trow" style="display:grid;grid-template-columns:minmax(0,1fr) 90px 150px;">
        <div class="cell-title">{{ t.name }}</div>
        <div class="cell">{{ t.article_count }} 篇</div>
        <div class="ops"><span class="link-u" @click="del(t)">删除</span></div>
      </div>
    </div>
  </div>
</template>
