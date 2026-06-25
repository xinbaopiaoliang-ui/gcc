SET @schema_name = DATABASE();

SET @sql = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS
   WHERE TABLE_SCHEMA = @schema_name AND TABLE_NAME = 'panel_nodes' AND COLUMN_NAME = 'hmac_secret_encrypted') = 0,
  'ALTER TABLE panel_nodes ADD COLUMN hmac_secret_encrypted TEXT NULL AFTER allow_udp',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @sql = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS
   WHERE TABLE_SCHEMA = @schema_name AND TABLE_NAME = 'panel_nodes' AND COLUMN_NAME = 'hmac_secret_source') = 0,
  'ALTER TABLE panel_nodes ADD COLUMN hmac_secret_source VARCHAR(32) NOT NULL DEFAULT '''' AFTER hmac_secret_encrypted',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @sql = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS
   WHERE TABLE_SCHEMA = @schema_name AND TABLE_NAME = 'panel_nodes' AND COLUMN_NAME = 'hmac_secret_updated_at') = 0,
  'ALTER TABLE panel_nodes ADD COLUMN hmac_secret_updated_at DATETIME NULL AFTER hmac_secret_source',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
