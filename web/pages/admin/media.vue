<script setup lang="ts">
definePageMeta({ layout: 'admin', middleware: 'admin', crumb: ['媒体库', '全站文件'] })
const items = ref<any[]>([])
const purpose = ref('')
const hasMore = ref(false)
let cursor = ''
const load = async (reset = true) => {
  const p = purpose.value ? `&purpose=${purpose.value}` : ''
  const c = reset ? '' : `&cursor=${encodeURIComponent(cursor)}`
  const d = await api<any>(`/api/v1/admin/media?limit=20${p}${c}`)
  items.value = reset ? (d.items || []) : items.value.concat(d.items || [])
  hasMore.value = !!d.has_more
  cursor = d.next_cursor || ''
}
onMounted(() => load())

const fmtSize = (s?: number | null) => {
  if (!s) return '-'
  if (s < 1024) return `${s} B`
  if (s < 1048576) return `${(s / 1024).toFixed(1)} KB`
  return `${(s / 1048576).toFixed(1)} MB`
}
const purposeName = (p: string) => ({ article: '文章图', avatar: '头像', chat: '聊天图（私有）' } as any)[p] || p
</script>

<template>
  <div>
    <div style="display:flex;gap:10px;margin-bottom:16px;">
      <select v-model="purpose" @change="load()"
        style="height:36px;border:0.5px solid #D3D1C7;border-radius:2px;padding:0 8px;font-size:12px;background:#fff;">
        <option value="">全部用途</option>
        <option value="article">文章图</option>
        <option value="avatar">头像</option>
        <option value="chat">聊天图（私有）</option>
      </select>
      <span style="margin-left:auto;font-size:12px;color:var(--ink-3);align-self:center;">
        {{ items.length }} 条<template v-if="hasMore">+（可加载更多）</template>
      </span>
    </div>
    <div class="hairline-table">
      <div class="thead" style="display:grid;grid-template-columns:minmax(0,1fr) 110px 120px 90px 70px 110px;">
        <div style="padding:0 14px 0 4px;">文件</div><div>用途</div><div>类型 / 尺寸</div><div>大小</div><div></div><div style="text-align:right;padding-right:4px;">上传时间</div>
      </div>
      <div v-for="m in items" :key="m.id" class="trow" style="display:grid;grid-template-columns:minmax(0,1fr) 110px 120px 90px 70px 110px;">
        <div class="cell-title" style="min-width:0;">
          <span style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap;">{{ m.original_name || m.id }}</span>
          <span class="mono" style="color:var(--ink-4);font-size:11px;">{{ m.mime_type }}</span>
        </div>
        <div class="cell">
          <span class="chip" :class="m.purpose === 'chat' ? 'chip-gray' : 'chip-green'">{{ purposeName(m.purpose) }}</span>
        </div>
        <div class="cell mono" style="font-size:11px;">{{ m.width || '?' }}×{{ m.height || '?' }}</div>
        <div class="cell mono" style="font-size:12px;">{{ fmtSize(m.size) }}</div>
        <div class="cell">
          <a v-if="m.url" :href="m.url" target="_blank" class="link-u">查看</a>
        </div>
        <div class="cell" style="text-align:right;font-size:12px;color:var(--ink-3);">{{ m.created_at?.slice(0, 10) }}</div>
      </div>
      <p v-if="!items.length" style="padding:24px 4px;font-size:13px;color:var(--ink-3);">暂无媒体记录</p>
    </div>
    <div v-if="hasMore" style="margin-top:14px;text-align:center;">
      <button class="btn-ghost-sm" @click="load(false)">加载更多</button>
    </div>
    <p style="margin-top:12px;font-size:11px;color:var(--ink-4);">聊天图（purpose=chat）走私有桶：仅会话成员经 /media/{id}/url 换取 1 小时签名地址，此处"查看"对 chat 图预期 403。</p>
  </div>
</template>
