// 首屏水合后的登录态收口（客户端插件）
//
// 为什么需要：refresh_token 的 Cookie Path 限于 /api/v1/auth，页面请求带不上；
// 服务端只能区分"access 有效"与"纯匿名"，中间态（access 过期但 refresh 还在）
// 必须在浏览器里续期。
//
// 为什么用 onNuxtReady：Pinia 的 SSR 状态在水合时才落到 store，
// 插件若在水合前 fetchMe，赋值会被 payload 覆盖（表现为"刷新了但界面还是未登录"）。
export default defineNuxtPlugin(() => {
  onNuxtReady(async () => {
    const auth = useAuthStore()
    const route = useRoute()

    const wasLoggedIn = auth.isLoggedIn
    await auth.fetchMe(true) // 401 由 api 层单例刷新兜住

    if (auth.isLoggedIn) {
      if (!wasLoggedIn) await refreshNuxtData() // 补拉 SSR 因 401 取空的数据
      if (route.path === '/login' || route.path === '/register') {
        const back = String(route.query.redirect || '/')
        return navigateTo(back.startsWith('/') ? back : '/')
      }
      return
    }

    // 仍未登录：只处理"服务端放过、客户端才发现没身份"的情况
    const mw = route.meta.middleware
    const middlewares: string[] = Array.isArray(mw) ? (mw as string[]) : mw ? [mw as string] : []
    if (middlewares.includes('admin')) return navigateTo('/admin/login')
    if (middlewares.includes('auth')) {
      return navigateTo({ path: '/login', query: { redirect: route.fullPath } })
    }
  })
})
