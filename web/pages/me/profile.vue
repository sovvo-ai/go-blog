<script setup lang="ts">
definePageMeta({ layout: 'workspace', middleware: 'auth' })
const auth = useAuthStore()
await auth.fetchMe()
const me = computed(() => auth.user)
const nickname = ref(auth.user?.nickname || ''); const bio = ref(auth.user?.bio || '')
const msg = ref(''); const err = ref('')
const save = async () => {
  msg.value = err.value = ''
  const r = await apiRaw('/api/v1/users/me', { method: 'PATCH', body: { nickname: nickname.value, bio: bio.value } })
  if (r.code !== 0) { err.value = r.message; return }
  msg.value = '已保存'
  await auth.fetchMe(true) // 顶栏昵称立刻跟着更新
}
</script>

<template>
  <div>
    <h1>编辑资料</h1>
    <div style="display:flex;gap:40px;align-items:flex-start;">
      <div style="text-align:center;">
        <div class="avatar-dot" style="width:96px;height:96px;font-size:34px;margin:0 auto 12px;">{{ (nickname || '?').slice(0, 1) }}</div>
        <span class="btn-ghost-sm">更换头像（需图片上传）</span>
        <p style="font-size:11px;color:var(--ink-4);margin:8px 0 0;">jpg/png/webp/gif ≤10MB</p>
      </div>
      <div style="flex:1;min-width:0;max-width:420px;">
        <p v-if="msg" style="color:var(--green);font-size:13px;margin-bottom:14px;">{{ msg }}</p>
        <p v-if="err" style="color:var(--danger);font-size:13px;margin-bottom:14px;">{{ err }}</p>
        <div class="field"><label>用户名（不可改）</label><input :value="me?.username" disabled style="color:var(--ink-4);"></div>
        <div class="field"><label>昵称</label><input v-model="nickname" type="text"></div>
        <div class="field"><label>个人简介</label><textarea v-model="bio" placeholder="一句话介绍自己" /></div>
        <button class="btn-dark" style="height:40px;padding:0 28px;" @click="save">保存修改</button>
      </div>
    </div>
  </div>
</template>
