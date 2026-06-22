-- MySQL 初始化脚本 (用户授权)
-- 表结构由 database/mysql/schema.sql 提供

CREATE USER IF NOT EXISTS 'parser_user'@'%' IDENTIFIED BY 'parser_pass';
GRANT ALL PRIVILEGES ON unified_tx_parser.* TO 'parser_user'@'%';
FLUSH PRIVILEGES;
