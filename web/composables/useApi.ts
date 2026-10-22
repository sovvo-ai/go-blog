// 统一 API 访问层：SSR 透传 Cookie、401 单例刷新重放、写请求自动带 CSRF。
// 规范依据：FRONTEND_DESIGN 5.5（401 与并发刷新）/ 6.2（auth store）/ 2.3（守卫）。

import { setCookie } from 'h3'

export function useUpstream(): string {
  const cfg = useRuntimeConfig()
  return import.meta.server ? cfg.public.apiUpstream : ''
}

export interface Envelope<T> { code: number; message: string; data: T }

// ---- 界面用类型（与后端 dto 对齐的最小集）----
export interface ArticleBrief {
  id: string; slug: string; title: string; summary: string; cover_url: string
  author: { id: string; nickname: string; avatar_url: string }
  category?: { id: string; name: string; slug: string } | null
  tags?: { id: string; name: string }[] | null
  published_at?: string | null; view_count: number; like_count: number; comment_count: number
}
export interface ArticleList { items: ArticleBrief[]; next_cursor?: string | null; has_more: boolean }
export interface ArticleDetail {
  id: string; slug: string; title: string; summary?: string | null; content_html: string
  author: { id: string; nickname: string; avatar_url: string }
  category?: { id: string; name: string; slug: string } | null
  tags?: { id: string; name: string }[] | null
  published_at?: string | null; view_count: number; like_count: number; favorite_count: number
  comment_count: number; is_liked: boolean; is_favorited: boolean; allow_comment: boolean
}
export interface Me { id: string; username: string; nickname: string; avatar_url: string; role: string; bio: string }

// 这些接口的 401 就是失败本身，刷新只会无限循环（规范 5.5 末段）
const NO_REFRESH_PATHS = [
  '/api/v1/auth/login',
  '/api/v1/auth/refresh',
  '/api/v1/auth/logout',
  '/api/v1/auth/register',
  '/api/v1/auth/password/forgot',
  '/api/v1/auth/password/reset'
]

function readCsrf(cookie?: string): string {
  // 注意用 || 而非 ??：调用方在浏览器端传的是空字符串 ''，
  // ?? 只在 null/undefined 时回退，空串会导致永远读不到 document.cookie 里的 token
  const src = cookie || (import.meta.client ? document.cookie : '')
  const m = src.match(/(?:^|;\s*)csrf_token=([^;]+)/)
  return m ? decodeURIComponent(m[1]) : ''
}

// SSR 阶段必须手动把浏览器的 Cookie 透传给后端，
// 否则服务端请求一律是匿名，首屏会渲染成"未登录"再水合跳变（FRONTEND_DESIGN 2.3）。
function ssrCookie(): string {
  if (import.meta.server) {
    try { return useRequestHeaders(['cookie']).cookie || '' } catch { return '' }
  }
  return ''
}

function buildHeaders(method: string, extra?: Record<string, string>): Record<string, string> {
  const h: Record<string, string> = { ...(extra || {}) }
  const cookie = ssrCookie()
  if (cookie) h.cookie = cookie
  if (method !== 'GET' && method !== 'HEAD') {
    const token = readCsrf(cookie)
    if (token) h['X-CSRF-Token'] = token
  }
  return h
}

function isUnauthorized(e: any): boolean {
  const status = e?.statusCode ?? e?.response?.status
  if (status === 401) return true
  const code = e?.data?.code ?? e?.response?._data?.code
  return code === 10002
}

// 模块级单例：并发 401 只发起一次刷新，其余等待同一个 Promise（规范 5.5）
let refreshPromise: Promise<boolean> | null = null

// SSR 阶段刷新拿到的 Set-Cookie 必须回写浏览器，否则刷新等于没发生
function forwardSetCookies(list: string[]) {
  try {
    const event = useRequestEvent()
    if (!event) return
    for (const raw of list) {
      const [pair, ...attrs] = raw.split(';')
      const eq = pair.indexOf('=')
      if (eq < 0) continue
      const name = pair.slice(0, eq).trim()
      const value = pair.slice(eq + 1).trim()
      const opts: Record<string, any> = { path: '/' }
      for (const a of attrs) {
        const [k, v] = a.split('=')
        const key = (k || '').trim().toLowerCase()
        if (key === 'path' && v) opts.path = v.trim()
        else if (key === 'max-age' && v) opts.maxAge = Number(v.trim())
        else if (key === 'expires' && v) opts.expires = new Date(v.trim())
        else if (key === 'httponly') opts.httpOnly = true
        else if (key === 'samesite' && v) opts.sameSite = v.trim().toLowerCase()
        else if (key === 'secure') opts.secure = true
      }
      setCookie(event, name, value, opts)
    }
  } catch { /* 无事件上下文时忽略 */ }
}

async function doRefresh(): Promise<boolean> {
  try {
    let cookies: string[] = []
    await $fetch('/api/v1/auth/refresh', {
      method: 'POST',
      credentials: 'include',
      baseURL: useUpstream(),
      headers: buildHeaders('POST'),
      onResponse({ response }) {
        const get = (response.headers as any).getSetCookie
        cookies = typeof get === 'function' ? get.call(response.headers) : []
      }
    })
    if (import.meta.server && cookies.length) forwardSetCookies(cookies)
    return true
  } catch {
    return false
  }
}

function refreshOnce(): Promise<boolean> {
  if (!refreshPromise) {
    refreshPromise = doRefresh().finally(() => { refreshPromise = null })
  }
  return refreshPromise
}

async function resetAndGoLogin(): Promise<void> {
  try {
    // 动态引入避免 useApi ↔ store 的循环依赖
    const { useAuthStore } = await import('~/stores/auth')
    useAuthStore().clear()
  } catch { /* store 未就绪时忽略 */ }
  if (import.meta.client) {
    const route = useRoute()
    // 已经在登录/注册页时不再跳，否则会把 redirect 套成 redirect=/login?redirect=...
    if (route.path === '/login' || route.path === '/register') return
    await navigateTo({ path: '/login', query: { redirect: route.fullPath } })
  }
}

async function request<T>(url: string, opts: any = {}): Promise<Envelope<T>> {
  const method = (opts.method || 'GET').toUpperCase()
  const call = () => $fetch<Envelope<T>>(url, {
    ...opts,
    method,
    headers: buildHeaders(method, opts.headers),
    credentials: 'include',
    baseURL: useUpstream()
  })

  try {
    return await call()
  } catch (e: any) {
    if (!isUnauthorized(e) || NO_REFRESH_PATHS.includes(url)) throw e
    const ok = await refreshOnce()
    if (!ok) {
      if (!opts.skipAuthRedirect) await resetAndGoLogin()
      throw e
    }
    try {
      return await call() // 只重放一次，不递归
    } catch (e2: any) {
      if (isUnauthorized(e2) && !opts.skipAuthRedirect) await resetAndGoLogin()
      throw e2
    }
  }
}

// 取 data：失败抛错（业务调用默认用这个）
export async function api<T = any>(url: string, opts: any = {}): Promise<T> {
  return (await request<T>(url, opts)).data
}

// 取整个信封：4xx 也不抛，便于表单展示 message
export async function apiRaw<T = any>(url: string, opts: any = {}): Promise<Envelope<T>> {
  try {
    return await request<T>(url, opts)
  } catch (e: any) {
    const body = e?.data ?? e?.response?._data
    if (body && typeof body.code === 'number') return body as Envelope<T>
    throw e
  }
}
