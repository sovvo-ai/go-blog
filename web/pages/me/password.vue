<script setup lang="ts">
definePageMeta({ layout: 'workspace', middleware: 'auth' })
const oldp = ref(''); const p1 = ref(''); const p2 = ref('')
const msg = ref(''); const err = ref('')
const save = async () => {
  err.value = msg.value = ''
  if (p1.value !== p2.value) { err.value = '两次输入的密码不一致'; return }
  const r = await apiRaw('/api/v1/users/me/password', { method: 'PUT', body: { old_password: oldp.value, new_password: p1.value } })
  if (r.code === 0) { msg.value = '密码已更新'; oldp.value = p1.value = p2.value = '' }
  else err.value = r.message
}
</script>

<template>
  <div>
    <h1>修改密码</h1>
    <div style="max-width:420px;">
      <p v-if="msg" style="color:var(--green);font-size:13px;margin-bottom:14px;">{{ msg }}</p>
      <p v-if="err" style="color:var(--danger);font-size:13px;margin-bottom:14px;">{{ err }}</p>
      <div class="field"><label>当前密码</label><input v-model="oldp" type="password"></div>
      <div class="field"><label>新密码</label><input v-model="p1" type="password" placeholder="至少 8 位，含字母与数字"></div>
      <div class="field"><label>确认新密码</label><input v-model="p2" type="password"></div>
      <button class="btn-dark" style="height:40px;padding:0 28px;" @click="save">更新密码</button>
      <p style="margin-top:14px;font-size:11px;color:var(--ink-4);">更新后其他设备的会话将被要求重新登录。</p>
    </div>
  </div>
</template>
