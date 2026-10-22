<script setup lang="ts">
definePageMeta({ layout: 'auth', middleware: 'guest' })
const f = ref({ username: '', email: '', password: '' })
const err = ref(''); const ok = ref(false); const busy = ref(false)
const submit = async () => {
  err.value = ''; busy.value = true
  const r = await apiRaw('/api/v1/auth/register', { method: 'POST', body: f.value })
  busy.value = false
  if (r.code === 0) ok.value = true
  else err.value = r.message || '注册失败'
}
</script>

<template>
  <div class="auth-card">
    <p class="logo">墨痕</p>
    <p class="sub">写作是对思考的最后一次编译。</p>
    <h1>创建账号</h1>
    <p v-if="err" style="color:var(--danger);font-size:13px;margin-bottom:14px;">{{ err }}</p>
    <template v-if="!ok">
      <div class="field"><label>用户名</label><input v-model="f.username" placeholder="3–20 位字母数字"></div>
      <div class="field"><label>邮箱</label><input v-model="f.email" type="email" placeholder="用于接收验证邮件"></div>
      <div class="field"><label>密码</label><input v-model="f.password" type="password" placeholder="至少 8 位，含字母与数字"></div>
      <button class="btn-wide" :disabled="busy" @click="submit">{{ busy ? '提交中…' : '注 册' }}</button>
    </template>
    <div v-else style="text-align:center;padding:14px;border:0.5px solid #BBF7D0;background:#F0FDF4;color:var(--green);font-size:13px;border-radius:2px;margin-bottom:18px;">
      注册成功！验证邮件已发送到 {{ f.email }}，请点击邮件中的链接完成验证。
    </div>
    <p class="alt">已有账号？<NuxtLink to="/login">直接登录</NuxtLink></p>
  </div>
</template>
