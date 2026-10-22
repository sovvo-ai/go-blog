<script setup lang="ts">
const route = useRoute()
const nav = [
  ['/me', '个人中心'], ['/me/profile', '编辑资料'], ['/me/password', '修改密码'],
  ['/me/articles', '我的文章'], ['/me/favorites', '我的收藏'], ['/me/notifications', '站内通知'],
  ['/write', '写文章']
]
// 守卫已保证已登录；这里 setup 取一次即可（SSR 首屏带 Cookie）
const auth = useAuthStore()
await auth.fetchMe()
const me = computed(() => auth.user)
const signOut = async () => {
  await auth.logout()
  await navigateTo('/login')
}
</script>

<template>
  <div>
    <header class="topbar" style="align-items:center;">
      <NuxtLink to="/" class="logo">墨痕</NuxtLink>
      <nav>
        <NuxtLink to="/" :class="{ active: route.path === '/' }">首页</NuxtLink>
        <NuxtLink to="/messages" :class="{ active: route.path.startsWith('/messages') }">消息</NuxtLink>
      </nav>
      <div class="right" v-if="me">
        <span>未读 <NuxtLink to="/me/notifications">通知</NuxtLink></span>
        <span class="link-quiet" @click="signOut">退出</span>
        <div class="avatar-dot">{{ (me.nickname || me.username || '?').slice(0, 1) }}</div>
      </div>
    </header>
    <div class="ws-layout">
      <aside class="ws-menu">
        <div class="who" v-if="me">
          <div class="avatar-dot">{{ (me.nickname || me.username || '?').slice(0, 1) }}</div>
          <p class="n">{{ me.nickname || me.username }}</p>
          <p class="r">{{ me.role === 'author' ? '作者' : me.role === 'admin' ? '管理员' : '读者' }} · 已登录</p>
        </div>
        <nav>
          <NuxtLink v-for="[to, label] in nav" :key="to" :to="to" :class="{ on: route.path === to }">{{ label }}</NuxtLink>
        </nav>
      </aside>
      <div class="ws-content"><slot /></div>
    </div>
  </div>
</template>
