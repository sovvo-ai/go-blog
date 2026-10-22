<script setup lang="ts">
definePageMeta({ layout: 'auth' })
const email = ref(''); const msg = ref(''); const err = ref(''); const done = ref(false)
const submit = async () => {
  err.value = ''
  const r = await apiRaw('/api/v1/auth/forgot-password', { method: 'POST', body: { email: email.value } })
  done.value = true
  msg.value = r.code === 0 ? '重置链接已发送，请查收邮件（30 分钟内有效）。' : r.message
  if (r.code !== 0) err.value = r.message
}
</script>

<template>
  <div class="auth-card">
    <p class="logo">墨痕</p>
    <p class="sub">写作是对思考的最后一次编译。</p>
    <h1>找回密码</h1>
    <p v-if="done" style="color:var(--green);font-size:13px;margin-bottom:14px;">{{ msg }}</p>
    <p v-if="err" style="color:var(--danger);font-size:13px;margin-bottom:14px;">{{ err }}</p>
    <div class="field"><label>注册邮箱</label><input v-model="email" type="email" placeholder="用于接收重置链接"></div>
    <button class="btn-wide" @click="submit">发送重置链接</button>
    <p class="alt">想起来密码了？<NuxtLink to="/login">直接登录</NuxtLink></p>
  </div>
</template>
