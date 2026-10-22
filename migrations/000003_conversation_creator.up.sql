-- ============================================================
-- 000003 会话发起人与每日上限索引
--
-- 背景：PRD 4.1.6.1 要求「同一用户每日新建会话不超过 20 个」，
-- 该规则必须按发起人计数，而 000001 的 conversations 表没有 creator_id。
-- 阶段 6 先在开发库直接 ALTER，这里补成正式迁移，保证新机器可用。
-- ============================================================

ALTER TABLE `conversations`
  ADD COLUMN `creator_id` BIGINT UNSIGNED NOT NULL DEFAULT 0
    COMMENT '发起人：每日新建会话上限按此计数（PRD 4.1.6.1）' AFTER `conversation_key`,
  ADD KEY `idx_creator_created` (`creator_id`, `created_at`);
