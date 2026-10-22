-- ============================================================
-- 000002 seed data
-- 依据：DATABASE.md 4.13 / 7.4
--
-- 注意：不在此处创建初始管理员。
-- 原因：管理员密码不应硬编码在迁移脚本中（会进版本库）。
-- 由应用侧命令在首次部署时创建：
--     server admin:create --password-from-env
-- 该命令读取 BLOG_ADMIN_PASSWORD，用 argon2id 哈希后写入。
-- 详见 TECH_DESIGN 15.3 部署流程。
-- ============================================================

SET NAMES utf8mb4;

-- ---------- 默认分类 ----------
INSERT INTO `categories` (`name`, `slug`, `description`, `sort`) VALUES
  ('技术', 'tech',    '工程实践与技术笔记', 10),
  ('生活', 'life',    '日常记录',           20),
  ('随笔', 'essay',   '想法与杂谈',         30)
ON DUPLICATE KEY UPDATE `name` = VALUES(`name`);

-- ---------- 系统设置 ----------
-- 运行时以本表为准，配置文件的 feature.* 只作初始种子
INSERT INTO `settings` (`config_key`, `config_value`, `value_type`, `config_group`, `description`) VALUES
  ('site.name',        '我的博客',  'string', 'site',    '站点名称'),
  ('site.logo_url',    '',          'string', 'site',    'Logo 地址'),
  ('site.icp',         '',          'string', 'site',    '备案号'),
  ('site.description', '',          'string', 'site',    '站点描述，用于 SEO'),
  ('review.article',   'false',     'bool',   'review',  '文章审核开关（PRD 3.4 默认关）'),
  ('review.comment',   'true',      'bool',   'review',  '评论审核开关（默认开）'),
  ('review.register',  'false',     'bool',   'review',  '注册审核开关'),
  ('comment.enabled',  'true',      'bool',   'comment', '全站评论总开关'),
  ('register.enabled', 'true',      'bool',   'comment', '是否开放注册')
ON DUPLICATE KEY UPDATE `config_value` = `config_value`;
