-- ============================================================
-- 000001 init schema
-- 依据：DATABASE.md v1.2
-- 目标：MySQL 8.0.36 / InnoDB / utf8mb4_0900_ai_ci
--
-- 约定：
--   1. 主键统一 BIGINT UNSIGNED AUTO_INCREMENT
--   2. 时间统一 DATETIME（不用 TIMESTAMP）
--   3. 不使用外键约束，由应用层保证一致性
--   4. 关联表用联合主键
-- ============================================================

SET NAMES utf8mb4;
SET FOREIGN_KEY_CHECKS = 0;

-- ------------------------------------------------------------
-- 1. 用户
-- ------------------------------------------------------------
CREATE TABLE `users` (
  `id`                BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `username`          VARCHAR(20)     NOT NULL COMMENT '4-20 位字母数字下划线，注册后不可改',
  `email`             VARCHAR(128)    NOT NULL,
  `password_hash`     VARCHAR(255)    NOT NULL COMMENT 'argon2id 编码串',
  `nickname`          VARCHAR(32)     NOT NULL DEFAULT '',
  `avatar_url`        VARCHAR(255)    NOT NULL DEFAULT '',
  `bio`               VARCHAR(255)    NOT NULL DEFAULT '',
  `role`              TINYINT UNSIGNED NOT NULL DEFAULT 1 COMMENT '1=reader 2=author 3=admin',
  `status`            TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '0=pending_verify 1=active 2=disabled',
  `email_verified_at` DATETIME NULL,
  `last_login_at`     DATETIME NULL,
  `last_login_ip`     VARCHAR(45) NOT NULL DEFAULT '',
  `created_at`        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at`        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_username` (`username`),
  UNIQUE KEY `uk_email` (`email`),
  KEY `idx_role_status` (`role`, `status`),
  KEY `idx_created_at` (`created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='用户';

-- ------------------------------------------------------------
-- 2. 分类
-- ------------------------------------------------------------
CREATE TABLE `categories` (
  `id`            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `name`          VARCHAR(64)  NOT NULL,
  `slug`          VARCHAR(64)  NOT NULL,
  `description`   VARCHAR(255) NOT NULL DEFAULT '',
  `sort`          INT          NOT NULL DEFAULT 0,
  `article_count` INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '只统计 status=published 的文章',
  `created_at`    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at`    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_slug` (`slug`),
  KEY `idx_sort` (`sort`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='分类';

-- ------------------------------------------------------------
-- 3. 标签
-- ------------------------------------------------------------
CREATE TABLE `tags` (
  `id`            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `name`          VARCHAR(32)  NOT NULL,
  `slug`          VARCHAR(32)  NOT NULL,
  `article_count` INT UNSIGNED NOT NULL DEFAULT 0,
  `created_at`    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_slug` (`slug`),
  KEY `idx_name` (`name`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='标签';

-- ------------------------------------------------------------
-- 4. 文章
-- ------------------------------------------------------------
CREATE TABLE `articles` (
  `id`                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `author_id`          BIGINT UNSIGNED NOT NULL,
  `category_id`        BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '0=未分类，不指向任何分类记录',
  `title`              VARCHAR(200)  NOT NULL,
  `slug`               VARCHAR(200)  NOT NULL COMMENT '全局唯一，删除后永久占用',
  `summary`            VARCHAR(512)  NOT NULL DEFAULT '',
  `content_md`         MEDIUMTEXT    NOT NULL COMMENT 'Markdown 原文，唯一事实源',
  `content_html`       MEDIUMTEXT    NULL COMMENT '渲染产物，列表查询不读取',
  `cover_url`          VARCHAR(255)  NOT NULL DEFAULT '',
  `status`             TINYINT UNSIGNED NOT NULL DEFAULT 0
                       COMMENT '0=draft 1=pending 2=published 3=rejected 4=offline 5=archived 6=deleted',
  `is_top`             TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `allow_comment`      TINYINT UNSIGNED NOT NULL DEFAULT 1,
  `view_count`         BIGINT UNSIGNED NOT NULL DEFAULT 0,
  `like_count`         INT UNSIGNED NOT NULL DEFAULT 0,
  `favorite_count`     INT UNSIGNED NOT NULL DEFAULT 0,
  `comment_count`      INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '只统计 approved 的评论',
  `word_count`         INT UNSIGNED NOT NULL DEFAULT 0,
  `reading_time`       INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '预估阅读分钟数',
  `version`            INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '乐观锁',
  `search_sync_status` TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '0=pending 1=synced 2=failed',
  `search_synced_at`   DATETIME NULL,
  `reject_reason`      VARCHAR(255) NOT NULL DEFAULT '',
  `offline_reason`     VARCHAR(255) NOT NULL DEFAULT '',
  `published_at`       DATETIME NULL COMMENT '首次发布时间，编辑不更新',
  `created_at`         DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at`         DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `deleted_at`         DATETIME NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_slug` (`slug`),
  KEY `idx_status_top_published` (`status`, `is_top`, `published_at`),
  KEY `idx_author_status` (`author_id`, `status`, `updated_at`),
  KEY `idx_category_status` (`category_id`, `status`, `published_at`),
  KEY `idx_sync_scan` (`search_sync_status`, `updated_at`),
  KEY `idx_updated_at` (`updated_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='文章';

-- ------------------------------------------------------------
-- 5. 文章标签关联
-- ------------------------------------------------------------
CREATE TABLE `article_tags` (
  `article_id` BIGINT UNSIGNED NOT NULL,
  `tag_id`     BIGINT UNSIGNED NOT NULL,
  PRIMARY KEY (`article_id`, `tag_id`),
  KEY `idx_tag` (`tag_id`, `article_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='文章标签关联';

-- ------------------------------------------------------------
-- 6. 评论
-- 注意：status 默认 0(pending) 是安全默认值，审核关闭时应用层显式写 1
-- 注意：V1.0 不含评论点赞，故无 like_count
-- ------------------------------------------------------------
CREATE TABLE `comments` (
  `id`               BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `article_id`       BIGINT UNSIGNED NOT NULL,
  `user_id`          BIGINT UNSIGNED NOT NULL,
  `parent_id`        BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '0=主楼',
  `root_id`          BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '所属主楼 ID，0=自身即主楼',
  `reply_to_user_id` BIGINT UNSIGNED NOT NULL DEFAULT 0,
  `content`          VARCHAR(1000) NOT NULL COMMENT '纯文本',
  `status`           TINYINT UNSIGNED NOT NULL DEFAULT 0
                     COMMENT '0=pending 1=approved 2=rejected 3=deleted',
  `ip`               VARCHAR(45)  NOT NULL DEFAULT '',
  `user_agent`       VARCHAR(255) NOT NULL DEFAULT '',
  `created_at`       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `deleted_at`       DATETIME NULL,
  PRIMARY KEY (`id`),
  KEY `idx_article_status_root` (`article_id`, `status`, `root_id`, `created_at`),
  KEY `idx_user_created` (`user_id`, `created_at`),
  KEY `idx_status_created` (`status`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='评论';

-- ------------------------------------------------------------
-- 7. 媒体资源
-- ------------------------------------------------------------
CREATE TABLE `media` (
  `id`            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `uploader_id`   BIGINT UNSIGNED NOT NULL,
  `bucket`        VARCHAR(64)  NOT NULL,
  `object_key`    VARCHAR(512) NOT NULL,
  `original_name` VARCHAR(255) NOT NULL DEFAULT '',
  `mime_type`     VARCHAR(128) NOT NULL DEFAULT '',
  `size`          BIGINT UNSIGNED NOT NULL DEFAULT 0,
  `width`         INT UNSIGNED NOT NULL DEFAULT 0,
  `height`        INT UNSIGNED NOT NULL DEFAULT 0,
  `sha256`        CHAR(64)     NOT NULL DEFAULT '',
  `variants`      JSON NULL COMMENT '{"w128":"key1","w256":"key2","w800":"key3"}',
  `purpose`       VARCHAR(32)  NOT NULL DEFAULT 'article' COMMENT 'avatar/article/chat',
  `status`        TINYINT UNSIGNED NOT NULL DEFAULT 1 COMMENT '1=normal 0=deleted',
  `created_at`    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `idx_uploader` (`uploader_id`, `created_at`),
  KEY `idx_sha256` (`sha256`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='媒体资源';

-- ------------------------------------------------------------
-- 8. 点赞
-- ------------------------------------------------------------
CREATE TABLE `article_likes` (
  `user_id`    BIGINT UNSIGNED NOT NULL,
  `article_id` BIGINT UNSIGNED NOT NULL,
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`user_id`, `article_id`),
  KEY `idx_article` (`article_id`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='文章点赞';

-- ------------------------------------------------------------
-- 9. 收藏
-- ------------------------------------------------------------
CREATE TABLE `article_favorites` (
  `user_id`    BIGINT UNSIGNED NOT NULL,
  `article_id` BIGINT UNSIGNED NOT NULL,
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`user_id`, `article_id`),
  KEY `idx_article` (`article_id`, `created_at`),
  KEY `idx_user_created` (`user_id`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='文章收藏';

-- ------------------------------------------------------------
-- 10. 站内通知
-- ref_id 可为 NULL：唯一索引允许多个 NULL，但不允许多个空字符串
-- ------------------------------------------------------------
CREATE TABLE `notifications` (
  `id`         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `user_id`    BIGINT UNSIGNED NOT NULL COMMENT '接收者',
  `type`       VARCHAR(32) NOT NULL
               COMMENT 'comment_reply/comment_approved/article_approved/article_rejected/article_offline/system',
  `title`      VARCHAR(128) NOT NULL DEFAULT '',
  `content`    VARCHAR(512) NOT NULL DEFAULT '',
  `link`       VARCHAR(255) NOT NULL DEFAULT '',
  `ref_id`     BIGINT UNSIGNED NULL DEFAULT NULL COMMENT '关联对象 ID；system 类为 NULL',
  `is_read`    TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_user_type_ref` (`user_id`, `type`, `ref_id`),
  KEY `idx_user_read_created` (`user_id`, `is_read`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='站内通知';

-- ------------------------------------------------------------
-- 11. 举报
-- ------------------------------------------------------------
CREATE TABLE `reports` (
  `id`          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `reporter_id` BIGINT UNSIGNED NOT NULL,
  `target_type` VARCHAR(32) NOT NULL COMMENT 'article/comment/message/conversation',
  `target_id`   BIGINT UNSIGNED NOT NULL,
  `reason`      VARCHAR(64) NOT NULL COMMENT 'spam/abuse/porn/illegal/other',
  `detail`      VARCHAR(512) NOT NULL DEFAULT '',
  `status`      TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '0=pending 1=accepted 2=rejected',
  `handler_id`  BIGINT UNSIGNED NOT NULL DEFAULT 0,
  `handled_at`  DATETIME NULL,
  `handle_note` VARCHAR(255) NOT NULL DEFAULT '',
  `created_at`  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_reporter_target` (`reporter_id`, `target_type`, `target_id`),
  KEY `idx_status_created` (`status`, `created_at`),
  KEY `idx_target` (`target_type`, `target_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='举报';

-- ------------------------------------------------------------
-- 12. 操作审计日志
-- ------------------------------------------------------------
CREATE TABLE `operation_logs` (
  `id`          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `operator_id` BIGINT UNSIGNED NOT NULL,
  `action`      VARCHAR(64) NOT NULL
                COMMENT 'article.delete/article.offline/article.approve/article.reject/comment.delete/user.disable/user.enable/user.role_change/report.handle/settings.update',
  `target_type` VARCHAR(32) NOT NULL,
  `target_id`   BIGINT UNSIGNED NOT NULL DEFAULT 0,
  `detail`      JSON NULL,
  `ip`          VARCHAR(45) NOT NULL DEFAULT '',
  `user_agent`  VARCHAR(255) NOT NULL DEFAULT '',
  `created_at`  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `idx_operator_created` (`operator_id`, `created_at`),
  KEY `idx_target` (`target_type`, `target_id`),
  KEY `idx_action_created` (`action`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='操作审计日志';

-- ------------------------------------------------------------
-- 13. 系统设置
-- 列名用 config_* 前缀，避免 key / group 保留字
-- ------------------------------------------------------------
CREATE TABLE `settings` (
  `config_key`   VARCHAR(64) NOT NULL,
  `config_value` TEXT NOT NULL,
  `value_type`   VARCHAR(16) NOT NULL DEFAULT 'string' COMMENT 'string/int/bool/json',
  `config_group` VARCHAR(32) NOT NULL DEFAULT '' COMMENT 'site/comment/review',
  `description`  VARCHAR(255) NOT NULL DEFAULT '',
  `updated_at`   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`config_key`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='系统设置';

-- ------------------------------------------------------------
-- 14. 会话
-- conversation_key = min(uid):max(uid)，保证单聊唯一
-- seq_counter 是消息序号的唯一来源
-- ------------------------------------------------------------
CREATE TABLE `conversations` (
  `id`                   BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `type`                 TINYINT UNSIGNED NOT NULL DEFAULT 1 COMMENT '1=single（V1.0 仅此值）',
  `conversation_key`     VARCHAR(64) NOT NULL,
  `last_message_id`      BIGINT UNSIGNED NOT NULL DEFAULT 0,
  `last_message_at`      DATETIME NULL,
  `last_message_preview` VARCHAR(128) NOT NULL DEFAULT '',
  `message_count`        INT UNSIGNED NOT NULL DEFAULT 0,
  `seq_counter`          BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '会话内 seq 分配器',
  `created_at`           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at`           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_conversation_key` (`conversation_key`),
  KEY `idx_last_message_at` (`last_message_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='会话';

-- ------------------------------------------------------------
-- 15. 会话成员
-- last_read_seq 与 messages.seq 同量纲，避免 id/seq 换算
-- ------------------------------------------------------------
CREATE TABLE `conversation_members` (
  `conversation_id` BIGINT UNSIGNED NOT NULL,
  `user_id`         BIGINT UNSIGNED NOT NULL,
  `last_read_seq`   BIGINT UNSIGNED NOT NULL DEFAULT 0,
  `unread_count`    INT UNSIGNED NOT NULL DEFAULT 0,
  `is_muted`        TINYINT UNSIGNED NOT NULL DEFAULT 0,
  `joined_at`       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `left_at`         DATETIME NULL,
  PRIMARY KEY (`conversation_id`, `user_id`),
  KEY `idx_user_conv` (`user_id`, `conversation_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='会话成员';

-- ------------------------------------------------------------
-- 16. 消息
-- client_msg_id 可为 NULL：唯一索引允许多个 NULL
-- ------------------------------------------------------------
CREATE TABLE `messages` (
  `id`              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `conversation_id` BIGINT UNSIGNED NOT NULL,
  `sender_id`       BIGINT UNSIGNED NOT NULL,
  `seq`             BIGINT UNSIGNED NOT NULL COMMENT '会话内严格单调递增',
  `type`            TINYINT UNSIGNED NOT NULL DEFAULT 1 COMMENT '1=text 2=image 3=system',
  `content`         TEXT NOT NULL COMMENT '文本内容或 media object_key',
  `client_msg_id`   VARCHAR(64) NULL DEFAULT NULL COMMENT '客户端幂等键；系统消息为 NULL',
  `status`          TINYINT UNSIGNED NOT NULL DEFAULT 1 COMMENT '1=normal 2=recalled 3=deleted',
  `created_at`      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_sender_client` (`sender_id`, `client_msg_id`),
  UNIQUE KEY `uk_conv_seq` (`conversation_id`, `seq`),
  KEY `idx_conv_id` (`conversation_id`, `id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='聊天消息';

SET FOREIGN_KEY_CHECKS = 1;
