<script setup lang="ts">
definePageMeta({ layout: 'admin', middleware: 'admin', crumb: ['用户管理', '全部用户'] })
const items = ref<any[]>([])
const q = ref('')
const load = async () => {
  const query = q.value ? `&q=${encodeURIComponent(q.value)}` : ''
  items.value = (await api<any>(`/api/v1/admin/users?limit=20${query}`)).items || []
}
onMounted(load)
const setStatus = async (u: any, status: string) => {
  const reason = status === 'disabled' ? (prompt('禁用原因（必填）') || '') : ''
  if (status === 'disabled' && !reason) return
  await api(`/api/v1/admin/users/${u.id}/status`, { method: 'PUT', body: { status, reason } })
  load()
}
const setRole = async (u: any, role: string) => {
  await api(`/api/v1/admin/users/${u.id}/role`, { method: 'PUT', body: { role } })
  load()
}
</script>

<template>
  <div>
    <div style="display:flex;gap:10px;margin-bottom:16px;">
      <input v-model="q" @keyup.enter="load" placeholder="搜索用户名 / 邮箱…"
        style="width:220px;height:36px;border:0.5px solid #D3D1C7;border-radius:2px;padding:0 12px;font-size:12px;">
      <button class="btn-ghost-sm" @click="load">搜索</button>
      <span style="margin-left:auto;font-size:12px;color:var(--ink-3);align-self:center;">共 {{ items.length }} 人</span>
    </div>
    <div class="hairline-table">
      <div class="thead" style="display:grid;grid-template-columns:minmax(0,1fr) 150px 90px 90px 120px 130px;">
        <div style="padding:0 14px 0 4px;">用户</div><div>邮箱</div><div>角色</div><div>状态</div><div>注册时间</div><div style="text-align:right;padding-right:4px;">操作</div>
      </div>
      <div v-for="u in items" :key="u.id" class="trow" style="display:grid;grid-template-columns:minmax(0,1fr) 150px 90px 90px 120px 130px;">
        <div class="cell-title">
          <NuxtLink :to="`/user/${u.id}`">{{ u.nickname || u.username }}</NuxtLink>
          <span style="color:var(--ink-4);font-size:12px;">（{{ u.username }}）</span>
        </div>
        <div class="cell mono" style="font-size:12px;">{{ u.email?.slice(0, 3) }}***</div>
        <div class="cell">{{ u.role === 'admin' ? '管理员' : u.role === 'author' ? '作者' : '读者' }}</div>
        <div><span class="chip" :class="u.status === 'active' ? 'chip-green' : 'chip-gray'">{{ u.status === 'active' ? '正常' : '已禁用' }}</span></div>
        <div class="cell">{{ u.created_at?.slice(0, 10) }}</div>
        <div class="ops">
          <span v-if="u.status === 'active'" class="link-u" @click="setStatus(u, 'disabled')">禁用</span>
          <span v-else class="link-u" @click="setStatus(u, 'active')">启用</span>
          <span v-if="u.role !== 'admin'" class="link-u" @click="setRole(u, u.role === 'author' ? 'reader' : 'author')">
            {{ u.role === 'author' ? '降为读者' : '升为作者' }}
          </span>
        </div>
      </div>
    </div>
    <p style="margin-top:12px;font-size:11px;color:var(--ink-4);">不能禁用自己；不能撤销最后一个管理员；禁用后令牌吊销、WS 踢下线、权限缓存失效。</p>
  </div>
</template>
