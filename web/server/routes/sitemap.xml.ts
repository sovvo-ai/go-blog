// sitemap.xml：首页 + 已发布文章 + 分类 + 标签
//
// 只在服务端生成，数据直连后端（容器内是 http://app:8080）。
// 不包含草稿/待审核/已删除文章——它们的 URL 对外是 404/410，放进来会拉低抓取质量。
export default defineEventHandler(async (event) => {
  const cfg = useRuntimeConfig(event)
  const base = String(cfg.public.siteUrl).replace(/\/$/, '')
  const api = String(cfg.public.apiUpstream).replace(/\/$/, '')

  const urls: { loc: string; lastmod?: string; changefreq: string; priority: string }[] = [
    { loc: `${base}/`, changefreq: 'daily', priority: '1.0' },
    { loc: `${base}/search`, changefreq: 'weekly', priority: '0.5' }
  ]

  try {
    const res = await $fetch<any>(`${api}/api/v1/articles?limit=200`, { timeout: 5000 })
    for (const a of res?.data?.items || []) {
      urls.push({
        loc: `${base}/article/${a.slug}`,
        lastmod: a.published_at ? new Date(a.published_at).toISOString().slice(0, 10) : undefined,
        changefreq: 'weekly',
        priority: '0.8'
      })
      if (a.category?.slug) {
        urls.push({ loc: `${base}/category/${a.category.slug}`, changefreq: 'weekly', priority: '0.6' })
      }
      for (const t of a.tags || []) {
        if (t.slug) urls.push({ loc: `${base}/tag/${t.slug}`, changefreq: 'weekly', priority: '0.4' })
      }
    }
  } catch {
    // 后端不可用时退化：至少给出静态页，不让爬虫拿到 500
  }

  // 分类与标签单独取一次：文章列表里不一定带 slug，聚合页却都需要收录
  try {
    const [cats, tgs] = await Promise.all([
      $fetch<any>(`${api}/api/v1/categories`, { timeout: 3000 }),
      $fetch<any>(`${api}/api/v1/tags?limit=100`, { timeout: 3000 })
    ])
    for (const c of cats?.data?.items || []) {
      if (c.slug) urls.push({ loc: `${base}/category/${c.slug}`, changefreq: 'weekly', priority: '0.6' })
    }
    for (const t of tgs?.data?.items || []) {
      if (t.slug) urls.push({ loc: `${base}/tag/${t.slug}`, changefreq: 'weekly', priority: '0.4' })
    }
  } catch {
    // 同上：拿不到就跳过，不影响文章 URL
  }

  // 去重（分类/标签会被多篇文章重复带入）
  const seen = new Set<string>()
  const unique = urls.filter(u => (seen.has(u.loc) ? false : (seen.add(u.loc), true)))

  const body = `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
${unique.map(u => `  <url>
    <loc>${u.loc}</loc>${u.lastmod ? `\n    <lastmod>${u.lastmod}</lastmod>` : ''}
    <changefreq>${u.changefreq}</changefreq>
    <priority>${u.priority}</priority>
  </url>`).join('\n')}
</urlset>
`

  setHeader(event, 'content-type', 'application/xml; charset=utf-8')
  setHeader(event, 'cache-control', 'public, max-age=600')
  return body
})
