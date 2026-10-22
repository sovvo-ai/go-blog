<script setup lang="ts">
definePageMeta({ layout: 'auth' })
const account = ref(''); const password = ref(''); const err = ref(''); const busy = ref(false)
const auth = useAuthStore()
const submit = async () => {
  err.value = ''; busy.value = true
  const r = await auth.login(account.value, password.value)
  busy.value = false
  if (r.code !== 0) { err.value = r.message || '登录失败'; return }
  if (!auth.isAdmin) { err.value = '该账号不是管理员'; auth.clear(); return }
  navigateTo('/admin/')
}
</script>

<template>
  <div class="auth-card">
    <p class="logo">墨痕 · 管理</p>
    <p class="sub">管理端仅限管理员账号登录</p>
    <h1>管理员登录</h1>
    <p v-if="err" style="color:var(--danger);font-size:13px;margin-bottom:14px;">{{ err }}</p>
    <div class="field"><label>账号</label><input v-model="account" type="text" @keyup.enter="submit"></div>
    <div class="field"><label>密码</label><input v-model="password" type="password" @keyup.enter="submit"></div>
    <button class="btn-wide" :disabled="busy" @click="submit">{{ busy ? '登录中…' : '登 录' }}</button>
    <p class="alt"><NuxtLink to="/login.html">返回用户端 →</NuxtLink></p>
  </div>
</template>
