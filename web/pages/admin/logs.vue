<script setup lang="ts">
definePageMeta({ layout: 'admin', middleware: 'admin', crumb: ['操作日志', '审计记录'] })
const items = ref<any[]>([])
const hasMore = ref(false)
let cursor = ''

const load = async (reset = true) => {
  const c = reset || !cursor ? '' : `&cursor=${encodeURIComponent(cursor)}`
  const d = await api<any>(`/api/v1/admin/logs?limit=30${c}`)
  items.value = reset ? (d.items || []) : items.value.concat(d.items || [])
  hasMore.value = !!d.has_more
  cursor = d.next_cursor || ''
}
onMounted(() => load())

const actionName = (a: string) => {
  const map: Record<string, string> = {
    user_disable: '禁用用户', user_enable: '启用用户', user_role: '调整角色',
    article_approve: '通过文章', article_reject: '拒绝文章', article_offline: '下架文章',
    article_relist: '恢复文章', article_delete: '删除文章',
    comment_approve: '通过评论', comment_reject: '拒绝评论', comment_delete: '删除评论',
    report_handle: '处理举报', settings_update: '修改设置', index_rebuild: '重建索引',
  }
  return map[a] || a
}
</script>

<template>
  <div>
    <div class="hairline-table">
      <div class="thead" style="display:grid;grid-template-columns:170px 130px minmax(0,1fr) 110px 130px;">
        <div style="padding:0 14px 0 4px;">时间</div><div>操作人</div><div>动作与对象</div><div>来源 IP</div><div style="text-align:right;padding-right:4px;">对象</div>
      </div>
      <div v-for="l in items" :key="l.id" class="trow" style="display:grid;grid-template-columns:170px 130px minmax(0,1fr) 110px 130px;">
        <div class="cell mono" style="font-size:11px;color:var(--ink-3);">{{ l.created_at?.slice(0, 19).replace('T', ' ') }}</div>
        <div class="cell mono" style="font-size:12px;">UID {{ l.operator_id }}</div>
        <div class="cell-title" style="min-width:0;">
          <span>{{ actionName(l.action) }}</span>
          <span v-if="l.detail" style="color:var(--ink-4);font-size:12px;">{{ l.detail }}</span>
        </div>
        <div class="cell mono" style="font-size:12px;">{{ l.ip || '-' }}</div>
        <div class="cell mono" style="font-size:11px;text-align:right;color:var(--ink-4);">{{ l.target_type }}#{{ l.target_id || '-' }}</div>
      </div>
      <p v-if="!items.length" style="padding:24px 4px;font-size:13px;color:var(--ink-3);">暂无操作记录</p>
    </div>
    <div v-if="hasMore" style="margin-top:14px;text-align:center;">
      <button class="btn-ghost-sm" @click="load(false)">加载更早记录</button>
    </div>
  </div>
</template>
