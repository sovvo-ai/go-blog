<script setup lang="ts">
definePageMeta({ layout: 'admin', middleware: 'admin', crumb: ['系统设置', '审核与开关'] })
const items = ref<{ key: string; value: string }[]>([])
const loaded = ref(false)

// 设置项白名单与说明（与后端 settings 白名单对齐）
const meta: Record<string, { label: string; desc: string; type: string; options?: [string, string][] }> = {
  'comment.review_enabled': { label: '评论先审后显', desc: '开启后新评论进入待审核队列，审核通过才对前台可见', type: 'toggle' },
  'article.review_enabled': { label: '文章先审后发', desc: '开启后作者提交的文章需管理员审核通过才发布', type: 'toggle' },
  'registration.enabled': { label: '开放注册', desc: '关闭后新用户注册接口返回 403，已有账号不受影响', type: 'toggle' },
  'comment.max_depth': { label: '评论最大嵌套层数', desc: '超过层数的回复按同级展示', type: 'number' },
}
const unknown: string[] = []

const load = async () => {
  const d = await api<any>('/api/v1/admin/settings')
  items.value = (d.items || []).filter((i: any) => {
    if (meta[i.key]) return true
    unknown.push(i.key)
    return false
  })
  loaded.value = true
}
onMounted(load)

const save = async () => {
  await api('/api/v1/admin/settings', {
    method: 'PUT',
    body: { items: items.value.map(i => ({ key: i.key, value: i.value })) },
  })
  alert('已保存，设置实时生效')
}
</script>

<template>
  <div>
    <div v-if="loaded" style="max-width:640px;">
      <div v-for="i in items" :key="i.key" style="display:flex;align-items:flex-start;justify-content:space-between;gap:24px;padding:16px 0;border-bottom:0.5px solid var(--hairline);">
        <div>
          <p style="margin:0 0 4px;font-size:13px;color:var(--ink);">{{ meta[i.key].label }}</p>
          <p style="margin:0;font-size:12px;color:var(--ink-3);">{{ meta[i.key].desc }}</p>
          <p class="mono" style="margin:4px 0 0;font-size:11px;color:var(--ink-4);">{{ i.key }}</p>
        </div>
        <template v-if="meta[i.key].type === 'toggle'">
          <label class="switch">
            <input type="checkbox" :checked="i.value === '1' || i.value === 'true'" @change="i.value = ($event.target as HTMLInputElement).checked ? '1' : '0'">
            <span class="slider"></span>
          </label>
        </template>
        <input v-else v-model="i.value" style="width:90px;height:34px;border:0.5px solid #D3D1C7;border-radius:2px;padding:0 10px;font-size:13px;text-align:center;">
      </div>
      <p v-if="unknown.length" style="margin-top:12px;font-size:11px;color:var(--ink-4);">白名单外设置项（前端未渲染）：{{ unknown.join('、') }}</p>
      <button class="btn-ghost-sm" style="margin-top:20px;" @click="save">保存全部</button>
    </div>
  </div>
</template>
