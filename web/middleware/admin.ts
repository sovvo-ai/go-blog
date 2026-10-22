// 管理端守卫（FRONTEND_DESIGN 2.3）
// 与 auth 同样的约束：服务端只能拦纯匿名，其余交给客户端续期后判定。
export default defineNuxtRouteMiddleware(async () => {
  const auth = useAuthStore()

  if (import.meta.server) {
    const cookie = useRequestHeaders(['cookie']).cookie || ''
    if (!cookie.includes('csrf_token=')) return navigateTo('/admin/login')
    await auth.fetchMe()
    if (auth.isLoggedIn && !auth.isAdmin) {
      auth.clear()
      return navigateTo('/admin/login')
    }
    return
  }

  await auth.fetchMe()
  if (!auth.isAdmin) {
    auth.clear()
    return navigateTo('/admin/login')
  }
})
