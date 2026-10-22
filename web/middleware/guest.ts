// 反向守卫（FRONTEND_DESIGN 2.3）：已登录访问 /login /register 直接回首页。
export default defineNuxtRouteMiddleware(async () => {
  const auth = useAuthStore()
  await auth.fetchMe()
  if (auth.isLoggedIn) return navigateTo('/')
})
