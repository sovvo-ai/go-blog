<script setup lang="ts">
const route = useRoute()
const nav = [
  ['/admin/', '概览'], ['/admin/users', '用户管理'], ['/admin/articles', '文章审核'],
  ['/admin/comments', '评论管理'], ['/admin/reports', '举报处理'], ['/admin/categories', '分类'],
  ['/admin/tags', '标签'], ['/admin/media', '媒体库'], ['/admin/index-status', '索引状态'],
  ['/admin/settings', '系统设置'], ['/admin/logs', '操作日志']
]
const isOn = (to: string) => to === '/admin/' ? route.path === '/admin' || route.path === '/admin/' : route.path.startsWith(to)
// 守卫（middleware/admin）已校验角色，这里只取展示信息
const auth = useAuthStore()
await auth.fetchMe()
const me = computed(() => auth.user)
</script>

<template>
  <div class="admin-layout">
    <aside class="admin-menu">
      <span class="logo">墨痕 · 管理</span>
      <nav>
        <NuxtLink v-for="[to, label] in nav" :key="to" :to="to" :class="{ on: isOn(to) }">{{ label }}</NuxtLink>
      </nav>
      <div class="menu-foot" v-if="me">
        <div class="avatar-dot">管</div>
        <div><p class="name">{{ me.username }}</p><p class="role">超级管理员</p></div>
      </div>
    </aside>
    <div class="admin-main">
      <div class="admin-crumb">
        <template v-for="(c, i) in (route.meta.crumb as string[]) || []" :key="i">
          <span class="crumb-sep" v-if="i">/</span>
          <span :class="i === 0 ? 'page serif' : ''" :style="i === 0 ? '' : 'color:var(--ink-2)'">{{ c }}</span>
        </template>
      </div>
      <div class="admin-content"><slot /></div>
    </div>
  </div>
</template>
