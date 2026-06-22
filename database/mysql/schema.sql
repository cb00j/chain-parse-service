-- MySQL Schema
-- Usage: mysql -u <user> -p <database> < schema.sql

-- ============================================================
-- 区块表
-- ============================================================

CREATE TABLE IF NOT EXISTS blocks (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    block_number BIGINT NOT NULL,
    block_hash VARCHAR(128) NOT NULL,
    chain_type VARCHAR(32) NOT NULL,
    chain_id VARCHAR(64) NOT NULL,
    parent_hash VARCHAR(128),
    timestamp TIMESTAMP,
    tx_count INT DEFAULT 0,
    size BIGINT DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_block_hash_chain (block_hash, chain_type),
    INDEX idx_blocks_chain_number (chain_type, block_number),
    INDEX idx_blocks_timestamp (timestamp)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ============================================================
-- 基础交易表
-- ============================================================

CREATE TABLE IF NOT EXISTS transactions (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    tx_hash VARCHAR(128) NOT NULL,
    chain_type VARCHAR(32) NOT NULL,
    chain_id VARCHAR(64) NOT NULL,
    block_number BIGINT,
    block_hash VARCHAR(128),
    tx_index INT,
    from_address VARCHAR(128),
    to_address VARCHAR(128),
    value DECIMAL(65, 0),
    gas_limit BIGINT,
    gas_used BIGINT,
    gas_price BIGINT,
    status VARCHAR(32),
    timestamp TIMESTAMP,
    raw_data JSON,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_tx_hash_chain (tx_hash, chain_type),
    INDEX idx_chain_block (chain_type, block_number),
    INDEX idx_timestamp (timestamp),
    INDEX idx_from_address (from_address),
    INDEX idx_to_address (to_address)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ============================================================
-- 处理进度表
-- ============================================================

CREATE TABLE IF NOT EXISTS processing_progress (
    chain_type VARCHAR(32) PRIMARY KEY,
    last_processed_block BIGINT NOT NULL DEFAULT 0,
    last_update_time TIMESTAMP,
    total_transactions BIGINT DEFAULT 0,
    total_events BIGINT DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ============================================================
-- DEX 池子表
-- ============================================================

CREATE TABLE IF NOT EXISTS dex_pools (
    addr VARCHAR(256) PRIMARY KEY,
    factory VARCHAR(256) NOT NULL,
    protocol VARCHAR(64) NOT NULL,
    token0 VARCHAR(256),
    token1 VARCHAR(256),
    fee INT DEFAULT 0,
    extra JSON,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_dp_protocol (protocol),
    INDEX idx_dp_factory (factory)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ============================================================
-- DEX 代币表
-- ============================================================

CREATE TABLE IF NOT EXISTS dex_tokens (
    addr VARCHAR(256) PRIMARY KEY,
    name VARCHAR(128),
    symbol VARCHAR(64),
    decimals INT DEFAULT 0,
    is_stable BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ============================================================
-- DEX 交易表
-- ============================================================

CREATE TABLE IF NOT EXISTS dex_transactions (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    addr VARCHAR(256) NOT NULL,
    router VARCHAR(256),
    factory VARCHAR(256),
    pool VARCHAR(256) NOT NULL,
    hash VARCHAR(128) NOT NULL,
    from_addr VARCHAR(256),
    side VARCHAR(16) NOT NULL,
    amount DECIMAL(65, 0),
    price DOUBLE DEFAULT 0,
    value DOUBLE DEFAULT 0,
    time BIGINT NOT NULL,
    event_index BIGINT DEFAULT 0,
    tx_index BIGINT DEFAULT 0,
    swap_index BIGINT DEFAULT 0,
    block_number BIGINT DEFAULT 0,
    extra JSON,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_dt_hash_event_side (hash, event_index, side),
    INDEX idx_dt_pool (pool),
    INDEX idx_dt_time (time),
    INDEX idx_dt_block (block_number)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ============================================================
-- DEX 流动性表
-- ============================================================

CREATE TABLE IF NOT EXISTS dex_liquidities (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    addr VARCHAR(256) NOT NULL,
    router VARCHAR(256),
    factory VARCHAR(256),
    pool VARCHAR(256) NOT NULL,
    hash VARCHAR(128) NOT NULL,
    from_addr VARCHAR(256),
    pos VARCHAR(256),
    side VARCHAR(16) NOT NULL,
    amount DECIMAL(65, 0),
    value DOUBLE DEFAULT 0,
    time BIGINT NOT NULL,
    `key` VARCHAR(512),
    extra JSON,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_dl_key_addr (`key`, addr),
    INDEX idx_dl_pool (pool),
    INDEX idx_dl_time (time)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ============================================================
-- DEX 储备表
-- ============================================================

CREATE TABLE IF NOT EXISTS dex_reserves (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    addr VARCHAR(256) NOT NULL,
    amount0 DECIMAL(65, 0),
    amount1 DECIMAL(65, 0),
    time BIGINT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_dr_addr_time (addr, time),
    INDEX idx_dr_time (time)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
