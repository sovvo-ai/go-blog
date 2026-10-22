<script setup lang="ts">
definePageMeta({ layout: 'admin', middleware: 'admin', crumb: ['分类', '排序与增删'] })
const items = ref<any[]>([])
const name = ref(''); const slug = ref('')
const load = async () => { items.value = (await api<any>('/api/v1/admin/categories')).items || [] }
onMounted(load)
const create = async () => {
  if (!name.value.trim() || !slug.value.trim()) { alert('名称与 slug 必填'); return }
  const r = await apiRaw('/api/v1/admin/categories', { method: 'POST', body: { name: name.value, slug: slug.value } })
  if (r.code !== 0) { alert(r.message); return }
  name.value = slug.value = ''; load()
}
const rename = async (c: any) => {
  const n = prompt('新分类名', c.name)
  if (!n) return
  await api(`/api/v1/admin/categories/${c.id}`, { method: 'PUT', body: { name: n } })
  load()
}
const del = async (c: any) => {
  if (!confirm(`删除分类「${c.name}」？其下文章会变为未分类。`)) return
  await api(`/api/v1/admin/categories/${c.id}`, { method: 'DELETE' })
  load()
}
</script>

<template>
  <div>
    <div style="display:flex;gap:8px;align-items:center;margin-bottom:16px;">
      <input v-model="name" placeholder="分类名" style="width:160px;height:36px;border:0.5px solid #D3D1C7;border-radius:2px;padding:0 12px;font-size:12px;">
      <input v-model="slug" placeholder="slug（如 backend）" style="width:180px;height:36px;border:0.5px solid #D3D1C7;border-radius:2px;padding:0 12px;font-size:12px;">
      <button class="btn-dark" style="height:36px;padding:0 18px;" @click="create">+ 新建分类</button>
    </div>
    <div class="hairline-table">
      <div class="thead" style="display:grid;grid-template-columns:minmax(0,1fr) 160px 90px 150px;">
        <div style="padding:0 14px 0 4px;">名称</div><div>Slug</div><div>文章数</div><div style="text-align:right;padding-right:4px;">操作</div>
      </div>
      <div v-for="c in items" :key="c.id" class="trow" style="display:grid;grid-template-columns:minmax(0,1fr) 160px 90px 150px;">
        <div class="cell-title">{{ c.name }}</div>
        <div class="cell mono" style="font-size:12px;">{{ c.slug }}</div>
        <div class="cell">{{ c.article_count }} 篇</div>
        <div class="ops"><span class="link-u" @click="rename(c)">编辑</span><span class="link-u" @click="del(c)">删除</span></div>
      </div>
    </div>
  </div>
</template>
