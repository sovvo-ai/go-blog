-- ============================================================
-- 000001 init schema (rollback)
-- 逆序删除全部表
-- ============================================================

SET NAMES utf8mb4;
SET FOREIGN_KEY_CHECKS = 0;

DROP TABLE IF EXISTS `messages`;
DROP TABLE IF EXISTS `conversation_members`;
DROP TABLE IF EXISTS `conversations`;
DROP TABLE IF EXISTS `settings`;
DROP TABLE IF EXISTS `operation_logs`;
DROP TABLE IF EXISTS `reports`;
DROP TABLE IF EXISTS `notifications`;
DROP TABLE IF EXISTS `article_favorites`;
DROP TABLE IF EXISTS `article_likes`;
DROP TABLE IF EXISTS `media`;
DROP TABLE IF EXISTS `comments`;
DROP TABLE IF EXISTS `article_tags`;
DROP TABLE IF EXISTS `articles`;
DROP TABLE IF EXISTS `tags`;
DROP TABLE IF EXISTS `categories`;
DROP TABLE IF EXISTS `users`;

SET FOREIGN_KEY_CHECKS = 1;
