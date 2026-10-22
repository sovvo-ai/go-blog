// 登录守卫（FRONTEND_DESIGN 2.3）
//
// 关键约束：refresh_token 的 Cookie Path 限于 /api/v1/auth，
// 页面请求带不上它，服务端无法静默续期。因此：
//   · 请求里连 csrf_token 都没有 → 纯匿名，服务端直接跳登录（HTTP 302）
//   · 有 cookie 但在服务端解析失败（access 过期）→ 不判"未登录"，
//     渲染骨架交给 plugins/auth-resolve.client.ts 续期后重判
export default defineNuxtRouteMiddleware(async (to) => {
  const auth = useAuthStore()
  const loginTo = { path: '/login', query: { redirect: to.fullPath } }

  if (import.meta.server) {
    const cookie = useRequestHeaders(['cookie']).cookie || ''
    if (!cookie.includes('csrf_token=')) return navigateTo(loginTo)
    await auth.fetchMe()
    if (!auth.isLoggedIn) return // access 过期：交给客户端续期
  } else {
    await auth.fetchMe()
    if (!auth.isLoggedIn) return navigateTo(loginTo)
  }

  const minRole = to.meta.minRole as string | undefined
  if (minRole) {
    const rank: Record<string, number> = { reader: 1, author: 2, admin: 3 }
    if ((rank[auth.user?.role || ''] || 0) < (rank[minRole] || 99)) {
      return navigateTo('/403')
    }
  }
})
