<script setup lang="ts">
definePageMeta({ layout: 'admin', middleware: 'admin', crumb: ['概览', '站点数据'] })
const o = ref<any>(null)
onMounted(async () => { o.value = await api<any>('/api/v1/admin/overview') })
const pending = ref<any[]>([])
onMounted(async () => {
  try { pending.value = (await api<any>('/api/v1/admin/articles?status=pending&limit=3')).items || [] } catch {}
})
</script>

<template>
  <div>
    <div class="stat-band">
      <div class="cell"><p class="label">文章总数</p><p class="num">{{ o?.article_count ?? '—' }}</p><p class="delta" style="color:var(--green);">今日 +{{ o?.today_new_articles ?? 0 }}</p></div>
      <div class="cell"><p class="label">用户总数</p><p class="num">{{ o?.user_count ?? '—' }}</p><p class="delta" style="color:var(--green);">今日 +{{ o?.today_new_users ?? 0 }}</p></div>
      <div class="cell warn"><p class="label">待审核</p><p class="num">{{ o?.pending_article_count ?? '—' }}</p><p class="delta">文章 {{ o?.pending_article_count ?? 0 }} · 评论 {{ o?.pending_comment_count ?? 0 }}</p></div>
      <div class="cell"><p class="label">待审核评论</p><p class="num">{{ o?.pending_comment_count ?? '—' }}</p><p class="delta">进入评论管理处理</p></div>
    </div>

    <div style="margin-top:20px;">
      <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:10px;">
        <p style="margin:0;font-family:var(--serif);font-size:14px;color:var(--ink);">待审核文章</p>
        <NuxtLink to="/admin/articles" class="link-u" style="font-size:12px;">查看全部 →</NuxtLink>
      </div>
      <div style="border-top:0.5px solid var(--hairline);">
        <p v-if="!pending.length" style="padding:16px 4px;font-size:13px;color:var(--ink-4);">没有待审核文章。</p>
        <div v-for="a in pending" :key="a.id" style="display:flex;align-items:center;gap:12px;padding:12px 4px;font-size:13px;border-bottom:0.5px solid var(--hairline);">
          <span style="color:var(--ink);flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-family:var(--serif);">{{ a.title }}</span>
          <span style="font-size:12px;color:var(--ink-4);">{{ a.author?.nickname }}</span>
          <NuxtLink :to="`/admin/articles/${a.id}`" class="link-u" style="font-size:12px;">审核</NuxtLink>
        </div>
      </div>
    </div>
  </div>
</template>
