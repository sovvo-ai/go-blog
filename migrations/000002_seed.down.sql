-- ============================================================
-- 000002 seed data (rollback)
-- 只删除本迁移插入的种子数据，不影响业务数据
-- ============================================================

SET NAMES utf8mb4;

DELETE FROM `settings` WHERE `config_key` IN (
  'site.name', 'site.logo_url', 'site.icp', 'site.description',
  'review.article', 'review.comment', 'review.register',
  'comment.enabled', 'register.enabled'
);

DELETE FROM `categories` WHERE `slug` IN ('tech', 'life', 'essay');
