<script setup lang="ts">
definePageMeta({ layout: 'auth', middleware: 'guest' })
const username = ref(''); const password = ref(''); const err = ref(''); const busy = ref(false)
const route = useRoute()
const auth = useAuthStore()
const submit = async () => {
  err.value = ''; busy.value = true
  const r = await auth.login(username.value, password.value)
  busy.value = false
  if (r.code !== 0) { err.value = r.message || '登录失败'; return }
  const back = String(route.query.redirect || '/')
  navigateTo(back.startsWith('/') ? back : '/')
}
</script>

<template>
  <div class="auth-card">
    <p class="logo">墨痕</p>
    <p class="sub">写作是对思考的最后一次编译。</p>
    <h1>欢迎回来</h1>
    <p v-if="err" style="color:var(--danger);font-size:13px;margin-bottom:14px;">{{ err }}</p>
    <div class="field"><label>邮箱 / 用户名</label><input v-model="username" type="text" @keyup.enter="submit"></div>
    <div class="field"><label>密码</label><input v-model="password" type="password" @keyup.enter="submit"></div>
    <button class="btn-wide" :disabled="busy" @click="submit">{{ busy ? '登录中…' : '登 录' }}</button>
    <p class="alt">还没有账号？<NuxtLink to="/register">注册</NuxtLink> · <NuxtLink to="/forgot-password">忘记密码</NuxtLink></p>
  </div>
</template>
