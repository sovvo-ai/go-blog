<script setup lang="ts">
definePageMeta({ layout: 'auth' })
const route = useRoute()
const p1 = ref(''); const p2 = ref(''); const err = ref(''); const ok = ref(false)
const submit = async () => {
  err.value = ''
  if (p1.value !== p2.value) { err.value = '两次输入的密码不一致'; return }
  const r = await apiRaw('/api/v1/auth/reset-password', { method: 'POST', body: { token: route.query.token, password: p1.value } })
  if (r.code === 0) ok.value = true
  else err.value = r.message || '重置失败，链接可能已失效'
}
</script>

<template>
  <div class="auth-card">
    <p class="logo">墨痕</p>
    <p class="sub">写作是对思考的最后一次编译。</p>
    <h1>设置新密码</h1>
    <p v-if="err" style="color:var(--danger);font-size:13px;margin-bottom:14px;">{{ err }}</p>
    <div v-if="ok" style="text-align:center;padding:14px;border:0.5px solid #BBF7D0;background:#F0FDF4;color:var(--green);font-size:13px;border-radius:2px;margin-bottom:18px;">
      密码已更新，请用新密码登录。
    </div>
    <template v-if="!ok">
      <div class="field"><label>新密码</label><input v-model="p1" type="password" placeholder="至少 8 位，含字母与数字"></div>
      <div class="field"><label>确认新密码</label><input v-model="p2" type="password"></div>
      <button class="btn-wide" @click="submit">重置密码</button>
    </template>
    <p class="alt"><NuxtLink to="/login">去登录 →</NuxtLink></p>
  </div>
</template>
