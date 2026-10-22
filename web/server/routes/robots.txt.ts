// robots.txt：放行公开页，屏蔽个人中心/消息/管理端/搜索参数页
export default defineEventHandler((event) => {
  const cfg = useRuntimeConfig(event)
  const base = String(cfg.public.siteUrl).replace(/\/$/, '')

  const body = `User-agent: *
Allow: /
Disallow: /admin/
Disallow: /me/
Disallow: /messages
Disallow: /write
Disallow: /login
Disallow: /register
Disallow: /api/

Sitemap: ${base}/sitemap.xml
`
  setHeader(event, 'content-type', 'text/plain; charset=utf-8')
  setHeader(event, 'cache-control', 'public, max-age=3600')
  return body
})
