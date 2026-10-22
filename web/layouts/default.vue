<script setup lang="ts">
// 顶栏登录态来自 auth store，setup 阶段就取（SSR 已透传 Cookie），首屏不再闪"未登录"
const auth = useAuthStore()
await auth.fetchMe()
const me = computed(() => auth.user)
// 登出：清 store → 顶栏立即回到未登录态（同一 store，无需重挂载）
const signOut = async () => {
  await auth.logout()
  await navigateTo('/')
}

const navs = [
  { to: '/', label: '首页', match: '/' },
  { to: '/category/backend', label: '分类', match: '/category' },
  { to: '/tag/go', label: '标签', match: '/tag' },
  { to: '/messages', label: '消息', match: '/messages' }
]
const route = useRoute()
const isActive = (n: any) => n.match === '/' ? route.path === '/' : route.path.startsWith(n.match)
</script>

<template>
  <div>
    <header class="topbar">
      <NuxtLink to="/" class="logo">墨痕</NuxtLink>
      <nav>
        <NuxtLink v-for="n in navs" :key="n.to" :to="n.to" :class="{ active: isActive(n) }">{{ n.label }}</NuxtLink>
      </nav>
      <div class="right">
        <NuxtLink to="/search" style="border-bottom:0.5px solid #D3D1C7;padding-bottom:2px;">搜索</NuxtLink>
        <template v-if="me">
          <NuxtLink to="/me">{{ me.nickname || me.username }}</NuxtLink>
          <span class="link-quiet" @click="signOut">退出</span>
          <div class="avatar-dot">{{ (me.nickname || me.username || '?').slice(0, 1) }}</div>
        </template>
        <template v-else>
          <NuxtLink to="/login">登录</NuxtLink>
          <NuxtLink to="/register" class="btn-dark" style="text-decoration:none;">注册</NuxtLink>
        </template>
      </div>
    </header>
    <slot />
    <footer class="site-footer">© 2026 墨痕 · 备案号占位</footer>
  </div>
</template>
