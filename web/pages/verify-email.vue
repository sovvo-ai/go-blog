<script setup lang="ts">
definePageMeta({ layout: 'auth' })
const route = useRoute()
const state = ref<'loading' | 'ok' | 'fail'>('loading')
const msg = ref('')
onMounted(async () => {
  const r = await apiRaw<any>(`/api/v1/auth/verify-email?token=${encodeURIComponent(String(route.query.token || ''))}`)
  state.value = r.code === 0 ? 'ok' : 'fail'
  msg.value = r.message || (state.value === 'ok' ? '验证成功，账号已激活' : '验证链接无效或已过期，请重新注册或申请重发。')
})
</script>

<template>
  <div class="auth-card">
    <p class="logo">墨痕</p>
    <p class="sub">写作是对思考的最后一次编译。</p>
    <h1>邮箱验证</h1>
    <p v-if="state === 'loading'" style="color:var(--ink-3);font-size:13px;">正在验证…</p>
    <div v-else-if="state === 'ok'" style="text-align:center;padding:14px;border:0.5px solid #BBF7D0;background:#F0FDF4;color:var(--green);font-size:13px;border-radius:2px;margin-bottom:20px;">{{ msg }}</div>
    <div v-else style="text-align:center;padding:14px;border:0.5px solid #FECACA;background:#FEF2F2;color:var(--danger);font-size:13px;border-radius:2px;margin-bottom:20px;">{{ msg }}</div>
    <button class="btn-wide" @click="navigateTo('/login')">去登录 →</button>
  </div>
</template>
