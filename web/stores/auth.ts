// auth store（FRONTEND_DESIGN 6.2）：登录态唯一来源，SSR 与 CSR 共用。
// 不持久化到 localStorage——用户信息一律以 /users/me 为准，避免与后端权限不同步。
import { defineStore } from 'pinia'
import type { Me, Envelope } from '~/composables/useApi'

export const useAuthStore = defineStore('auth', () => {
  const user = ref<Me | null>(null)
  const loaded = ref(false)

  const isLoggedIn = computed(() => user.value !== null)
  const isAuthor = computed(() => user.value?.role === 'author' || user.value?.role === 'admin')
  const isAdmin = computed(() => user.value?.role === 'admin')

  // 首次拉取后缓存；force=true 用于登录成功或刷新后重取
  async function fetchMe(force = false): Promise<Me | null> {
    if (loaded.value && !force) return user.value
    try {
      // 登录态探测失败不自动跳转——是否跳由路由守卫决定
      user.value = await api<Me>('/api/v1/users/me', { skipAuthRedirect: true })
    } catch {
      user.value = null
    }
    loaded.value = true
    return user.value
  }

  async function login(account: string, password: string): Promise<Envelope<unknown>> {
    const r = await apiRaw('/api/v1/auth/login', { method: 'POST', body: { account, password } })
    if (r.code === 0) await fetchMe(true)
    return r
  }

  async function logout(): Promise<void> {
    try { await api('/api/v1/auth/logout', { method: 'POST' }) } catch { /* 已失效也照常清空 */ }
    clear()
  }

  function setUser(u: Me | null) {
    user.value = u
    loaded.value = true
  }

  function clear() {
    user.value = null
    loaded.value = true
  }

  return { user, loaded, isLoggedIn, isAuthor, isAdmin, fetchMe, login, logout, setUser, clear }
})
