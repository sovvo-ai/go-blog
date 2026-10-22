<script setup lang="ts">
definePageMeta({ layout: 'workspace', middleware: 'auth' })
const { data } = await useAsyncData('me-notifications', () => api<any>('/api/v1/notifications?limit=30'))
const items = computed(() => (data.value as any)?.items || [])
// 固定格式，避免服务端/浏览器时区不同导致水合不一致
const pad = (n: number) => String(n).padStart(2, '0')
const fmt = (s?: string | null) => { if (!s) return ''
  const d = new Date(s)
  return `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())}` + ` ${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}`
}
const readAll = async () => { await api('/api/v1/notifications/read-all', { method: 'PUT' }); location.reload() }
const readOne = async (n: any) => {
  if (n.is_read) return
  await api(`/api/v1/notifications/${n.id}/read`, { method: 'PUT' }); location.reload()
}
</script>

<template>
  <div>
    <div style="display:flex;align-items:center;margin-bottom:14px;">
      <h1 style="margin:0;">站内通知</h1>
      <span class="link-u" style="margin-left:auto;font-size:12px;" @click="readAll">全部标为已读</span>
    </div>
    <p v-if="!items.length" style="padding:24px 0;color:var(--ink-4);font-size:13px;">暂无通知。</p>
    <div v-for="n in items" :key="n.id" class="notif" style="cursor:pointer;" @click="readOne(n)">
      <span class="dot" :class="{ read: n.is_read }" />
      <div style="min-width:0;flex:1;">
        <p class="n-title">{{ n.title }}</p>
        <p class="n-body">{{ n.content }}</p>
        <p class="n-time">{{ fmt(n.created_at) }}</p>
      </div>
    </div>
  </div>
</template>
