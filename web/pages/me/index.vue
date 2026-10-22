<script setup lang="ts">
definePageMeta({ layout: 'workspace', middleware: 'auth' })
const auth = useAuthStore()
await auth.fetchMe()
const me = computed(() => auth.user)
const unread = ref(0)
onMounted(async () => {
  try { unread.value = (await api<any>('/api/v1/notifications/unread-count')).count } catch {}
})
const hour = new Date().getHours()
const greet = hour < 12 ? '上午好' : hour < 18 ? '下午好' : '晚上好'
</script>

<template>
  <div>
    <h1>{{ greet }}，{{ me?.nickname || me?.username || '' }}</h1>
    <div style="display:grid;grid-template-columns:repeat(3,1fr);border:0.5px solid var(--hairline);margin-bottom:24px;">
      <div style="padding:16px;border-right:0.5px solid var(--hairline);"><p style="margin:0 0 6px;font-size:12px;color:var(--ink-4);">我的角色</p><p class="mono" style="margin:0;font-size:22px;font-weight:500;color:var(--ink);">{{ me?.role === 'author' ? '作者' : me?.role === 'admin' ? '管理员' : '读者' }}</p></div>
      <div style="padding:16px;border-right:0.5px solid var(--hairline);"><p style="margin:0 0 6px;font-size:12px;color:var(--ink-4);">未读通知</p><p class="mono" style="margin:0;font-size:22px;font-weight:500;" :style="unread ? 'color:var(--accent);' : 'color:var(--ink);'">{{ unread }}</p></div>
      <div style="padding:16px;"><p style="margin:0 0 6px;font-size:12px;color:var(--ink-4);">快捷操作</p><NuxtLink to="/write" class="link-u" style="font-size:13px;">写新文章 →</NuxtLink></div>
    </div>
    <p style="font-size:12px;color:var(--ink-4);">提示：文章发布流程为 草稿 → 提交审核 → 发布（管理员可通过后关闭审核，直接发布）。</p>
  </div>
</template>
