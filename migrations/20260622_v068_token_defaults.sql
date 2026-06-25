CREATE TABLE IF NOT EXISTS panel_token_defaults (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  plan_id VARCHAR(64) NOT NULL,
  name VARCHAR(64) NOT NULL,
  max_connections INT UNSIGNED NOT NULL,
  rate_limit_mbps INT UNSIGNED NOT NULL,
  allow_tcp TINYINT(1) NOT NULL DEFAULT 1,
  allow_udp TINYINT(1) NOT NULL DEFAULT 1,
  description VARCHAR(255) NOT NULL DEFAULT '',
  sort_order INT NOT NULL DEFAULT 100,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_plan_id (plan_id),
  KEY idx_sort_order (sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

INSERT IGNORE INTO panel_token_defaults
  (plan_id, name, max_connections, rate_limit_mbps, allow_tcp, allow_udp, description, sort_order)
VALUES
  ('trial', '免费/测试', 32, 50, 1, 1, '短时测试、体验用户和低并发调试。', 10),
  ('standard', '普通', 64, 100, 1, 1, '默认游戏加速档位，适合 Steam 商店、社区和常规在线游戏。', 20),
  ('advanced', '高级', 128, 200, 1, 1, '推荐给 Steam 客户端联调和多连接游戏场景。', 30),
  ('premium', '旗舰', 256, 500, 1, 1, '高并发、多游戏下载和重度游戏加速档位。', 40);
