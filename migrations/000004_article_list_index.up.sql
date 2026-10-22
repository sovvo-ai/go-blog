-- ============================================================
-- 000004 公开列表的排序索引
--
-- 背景：公开列表固定 status=2 且 deleted_at IS NULL，按 published_at DESC, id DESC 排序。
-- 原索引 idx_status_top_published(status,is_top,published_at) 中间夹了 is_top，
-- 只固定 status 时无法利用 published_at 的有序性，EXPLAIN 显示扫描约 4800 行 + filesort。
-- 1 万篇文章下列表接口 P95 达到 233ms，超出 PRD 6.3 的 200ms 目标。
--
-- 新索引把 deleted_at 放进等值前缀，published_at 紧随其后，使排序可直接走索引。
-- ============================================================

ALTER TABLE `articles`
  ADD KEY `idx_status_deleted_published` (`status`, `deleted_at`, `published_at`, `id`);
