<script setup lang="ts">
definePageMeta({ layout: 'workspace', middleware: 'auth', minRole: 'author' })
const title = ref(''); const contentMd = ref(''); const summary = ref('')
const categories = ref<any[]>([]); const categoryId = ref(''); const tagsInput = ref('')
const allowComment = ref(true); const draftId = ref(''); const slug = ref('')
const msg = ref(''); const err = ref('')
onMounted(async () => {
  try { categories.value = (await api<any>('/api/v1/categories')).items || [] } catch {}
})
const pickCat = (e: Event) => { categoryId.value = (e.target as HTMLSelectElement).value }
const saveDraft = async () => {
  err.value = msg.value = ''
  if (!title.value.trim() || !contentMd.value.trim()) { err.value = '标题和正文不能为空'; return null }
  const body = {
    title: title.value, content_md: contentMd.value, summary: summary.value || undefined,
    category_id: categoryId.value || undefined, allow_comment: allowComment.value
  }
  if (draftId.value) {
    await api(`/api/v1/articles/${draftId.value}`, { method: 'PUT', body: { ...body, version: 1 } })
    return draftId.value
  }
  const d = await api<any>('/api/v1/articles', { method: 'POST', body })
  draftId.value = d.id; slug.value = d.slug || ''
  msg.value = '草稿已保存'
  return d.id
}
const submit = async () => {
  const id = await saveDraft()
  if (!id) return
  err.value = msg.value = ''
  const r = await apiRaw(`/api/v1/articles/${id}/submit`, { method: 'POST' })
  if (r.code === 0) { msg.value = '已提交审核'; navigateTo('/me/articles') }
  else err.value = r.message
}
</script>

<template>
  <div>
    <div style="display:flex;align-items:center;margin-bottom:18px;">
      <span class="serif" style="font-size:18px;color:var(--ink);">写文章</span>
      <span style="font-size:12px;color:var(--ink-4);margin-left:12px;">正文支持 Markdown</span>
      <div style="margin-left:auto;display:flex;gap:10px;">
        <button class="btn-ghost-sm" @click="saveDraft">存草稿</button>
        <button class="btn-dark" style="height:36px;padding:0 24px;" @click="submit">提交审核</button>
      </div>
    </div>
    <p v-if="msg" style="color:var(--green);font-size:13px;margin-bottom:12px;">{{ msg }}</p>
    <p v-if="err" style="color:var(--danger);font-size:13px;margin-bottom:12px;">{{ err }}</p>
    <div class="editor-layout">
      <div class="editor-area">
        <div class="editor-title"><input v-model="title" placeholder="标题：一句话说清这篇文章解决什么问题"></div>
        <textarea v-model="contentMd" placeholder="## 二级标题、**加粗**、`代码`、- 列表…"></textarea>
      </div>
      <div class="settings-panel">
        <h4>发布设置</h4>
        <div class="field"><label>分类</label>
          <select @change="pickCat">
            <option value="">选择分类</option>
            <option v-for="c in categories" :key="c.id" :value="c.id">{{ c.name }}</option>
          </select>
        </div>
        <div class="field"><label>标签（逗号分隔，最多 5 个）</label><input v-model="tagsInput" type="text" placeholder="Go, 性能"></div>
        <div class="field"><label>摘要（留空自动截取）</label><textarea v-model="summary" style="height:72px;" /></div>
        <div class="field"><label>允许评论</label>
          <span class="toggle" :class="{ on: allowComment }" style="cursor:pointer;" @click="allowComment = !allowComment">
            <span class="track" /><span style="font-size:12px;color:var(--ink-3);">{{ allowComment ? '开启' : '关闭' }}</span>
          </span>
        </div>
      </div>
    </div>
  </div>
</template>
