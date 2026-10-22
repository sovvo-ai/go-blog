ALTER TABLE `conversations`
  DROP KEY `idx_creator_created`,
  DROP COLUMN `creator_id`;
