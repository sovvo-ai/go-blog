<script setup lang="ts">
definePageMeta({ layout: 'admin', middleware: 'admin', crumb: ['索引状态', 'Meilisearch'] })
const items = ref<any[]>([])
const stats = ref({ indexed: '-', failed: 0, lastRebuild: '-' })
const rebuilding = ref(false)
const notice = ref('')

const load = async () => {
  const d = await api<any>('/api/v1/admin/index-failures?limit=50')
  items.value = d.items || []
  stats.value.failed = items.value.length
}
onMounted(load)

const rebuild = async () => {
  if (!confirm('全量重建会清空索引并重新同步全部已发布文章，视文章量可能持续数分钟。继续？')) return
  rebuilding.value = true
  notice.value = ''
  try {
    const r = await api<any>('/api/v1/admin/index-rebuild', { method: 'POST', body: { all: true } })
    // 全量重建是异步任务：后端返回 202 + accepted，这里不要等它跑完
    notice.value = r?.accepted
      ? '已开始全量重建（后台执行），稍后刷新本页查看失败队列'
      : `重建完成，共 ${r?.count || 0} 篇`
    await load()
  } catch (e: any) {
    notice.value = e?.data?.message || e?.message || '重建失败'
  } finally {
    rebuilding.value = false
  }
}
</script>

<template>
  <div>
    <div class="stat-band">
      <div class="cell"><p class="label">同步失败文章</p><p class="num" :style="stats.failed ? 'color:var(--danger)' : ''">{{ stats.failed }}</p><p class="delta">见下方失败队列</p></div>
      <div class="cell"><p class="label">索引引擎</p><p class="num serif" style="font-size:15px;">Meilisearch</p><p class="delta">增量同步于发布/更新/删除</p></div>
      <div class="cell"><p class="label">重建操作</p><p class="delta" style="margin-top:10px;">清空后按 ID 正序全量推送</p></div>
      <div class="cell" style="display:flex;align-items:center;justify-content:center;">
        <button class="btn-ghost-sm" :disabled="rebuilding" @click="rebuild">{{ rebuilding ? '重建中…' : '全量重建' }}</button>
      </div>
    </div>

    <p v-if="notice" style="margin:12px 0 0;font-size:12px;color:var(--ink-3);">{{ notice }}</p>

    <p class="serif" style="margin:22px 0 10px;font-size:14px;color:var(--ink);">同步失败队列</p>
    <div class="hairline-table">
      <div class="thead" style="display:grid;grid-template-columns:minmax(0,1fr) 110px 130px 90px;">
        <div style="padding:0 14px 0 4px;">文章</div><div>当前状态</div><div>最近同步时间</div><div style="text-align:right;padding-right:4px;">操作</div>
      </div>
      <div v-for="a in items" :key="a.id" class="trow" style="display:grid;grid-template-columns:minmax(0,1fr) 110px 130px 90px;">
        <div class="cell-title">{{ a.title }}</div>
        <div class="cell"><span class="chip" :class="a.article_status === 'published' ? 'chip-green' : a.article_status === 'offline' ? 'chip-gray' : 'chip-amber'">{{ a.article_status || '-' }}</span></div>
        <div class="cell mono" style="font-size:11px;">{{ a.synced_at?.slice(0, 19).replace('T', ' ') || '从未' }}</div>
        <div class="ops"><NuxtLink :to="`/article/x`" class="link-u">详情</NuxtLink></div>
      </div>
      <p v-if="!items.length" style="padding:24px 4px;font-size:13px;color:var(--ink-3);">队列干净，无同步失败记录 ✓</p>
    </div>
    <p style="margin-top:12px;font-size:11px;color:var(--ink-4);">失败记录由同步对账任务自动重试；重建期间搜索接口短暂降级为数据库 LIKE 查询，前台无感。</p>
  </div>
</template>
