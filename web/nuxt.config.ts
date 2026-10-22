export default defineNuxtConfig({
  compatibilityDate: '2026-10-22',
  devtools: false,
  ssr: true,
  modules: ['@pinia/nuxt'],
  css: ['~/assets/css/site.css', '~/assets/css/components.css', '~/assets/css/admin.css'],
  runtimeConfig: {
    // 服务端 SSR 直连后端（Docker 内是 http://app:8080）；客户端走同域 /api
    // 注意 public 段可被环境变量覆盖：NUXT_PUBLIC_API_UPSTREAM / NUXT_PUBLIC_SITE_URL
    public: {
      apiUpstream: 'http://127.0.0.1:8080',
      wsUpstream: 'ws://127.0.0.1:8080',
      // 站点对外地址，用于 canonical / OG / sitemap 生成
      siteUrl: 'http://localhost:3300'
    }
  },
  nitro: {
    // devProxy 的 key 是"被替换的前缀"：/api/x → target + /x，
    // 因此 target 必须带 /api，后端才能收到 /api/v1/... 原路径
    devProxy: { '/api': { target: 'http://127.0.0.1:8080/api', changeOrigin: true } },
    // 缓存策略：
    //   · 构建产物（带内容哈希）可长期缓存
    //   · HTML 一律不缓存：顶栏登录态是服务端按 Cookie 渲染的，缓存会串用户
    routeRules: {
      '/_nuxt/**': { headers: { 'cache-control': 'public, max-age=31536000, immutable' } },
      '/**': { headers: { 'cache-control': 'no-cache, must-revalidate' } }
    }
  },
  app: {
    head: {
      title: '墨痕',
      htmlAttrs: { lang: 'zh-CN' },
      meta: [
        { charset: 'utf-8' },
        { name: 'viewport', content: 'width=device-width, initial-scale=1' },
        { name: 'description', content: '墨痕 · 记录 Go、数据库与系统设计的实践与复盘' },
        { name: 'theme-color', content: '#111827' },
        { property: 'og:site_name', content: '墨痕' },
        { property: 'og:type', content: 'website' }
      ]
    }
  }
})
