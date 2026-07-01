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
                                                   chain_type           VARCHAR(32)  PRIMARY KEY,
    last_processed_block BIGINT       NOT NULL DEFAULT 0,
    last_update_time     TIMESTAMP,
    total_transactions   BIGINT       DEFAULT 0,
    total_events         BIGINT       DEFAULT 0,
    status               VARCHAR(20)  NOT NULL DEFAULT 'idle',
    error_count          BIGINT       NOT NULL DEFAULT 0,
    success_rate         FLOAT        NOT NULL DEFAULT 100.0,
    start_time           TIMESTAMP    NULL,
    extra                JSON         NULL,
    created_at           TIMESTAMP    DEFAULT CURRENT_TIMESTAMP,
    updated_at           TIMESTAMP    DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
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
    source VARCHAR(32),  -- 数据来源: 'onchain'(扫链上事件) / 'thegraph'(subgraph 预取)
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
    protocol VARCHAR(64),
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
    protocol VARCHAR(64),
    amount0 DECIMAL(65, 0),
    amount1 DECIMAL(65, 0),
    time BIGINT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_dr_addr_time (addr, time),
    INDEX idx_dr_time (time)
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
-- ============================================================
-- 处理错误历史表 (DBProgressTracker 使用)
-- ============================================================
CREATE TABLE IF NOT EXISTS processing_errors (
                                                 id          BIGINT AUTO_INCREMENT PRIMARY KEY,
                                                 chain_type  VARCHAR(32) NOT NULL,
    error_time  TIMESTAMP NOT NULL,
    error_type  VARCHAR(256),
    error_msg   TEXT,
    INDEX idx_pe_chain_time (chain_type, error_time)
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ============================================================
-- 处理性能指标表 (DBProgressTracker 使用)
-- ============================================================
CREATE TABLE IF NOT EXISTS processing_metrics (
                                                  id                  BIGINT AUTO_INCREMENT PRIMARY KEY,
                                                  chain_type          VARCHAR(32) NOT NULL,
    timestamp           TIMESTAMP NOT NULL,
    block_number        BIGINT,
    processing_time_ns  BIGINT,
    transaction_count   INT DEFAULT 0,
    event_count         INT DEFAULT 0,
    memory_usage        BIGINT DEFAULT 0,
    cpu_usage           FLOAT DEFAULT 0,
    INDEX idx_pm_chain_time (chain_type, timestamp)
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;


-- ============================================================
-- 通用增量同步游标表 (CursorStore 使用)
-- 通用性设计:不写死 thegraph/uniswap/ethereum,同一张表可以给
-- 任意外部数据源 x 任意链 x 任意协议 复用(比如以后接 Covalent/
-- Bitquery,或给 BSC 的 PancakeSwap subgraph 用),只需换
-- source/chain_type/protocol/cursor_key 的取值,不需要新建表或加列。
-- cursor_value 统一存成字符串,可以装时间戳、区块号或不透明的分页token。
-- ============================================================
CREATE TABLE IF NOT EXISTS sync_cursors (
                                            id            BIGINT AUTO_INCREMENT PRIMARY KEY,
                                            source        VARCHAR(64)  NOT NULL,   -- 数据源,例如 'thegraph'
    chain_type    VARCHAR(32)  NOT NULL,   -- 链,例如 'ethereum'
    protocol      VARCHAR(64)  NOT NULL,   -- 协议,例如 'uniswap_v2' / 'uniswap_v3'
    cursor_key    VARCHAR(64)  NOT NULL,   -- 游标字段,例如 'created_at_timestamp'
    cursor_value  VARCHAR(256) NOT NULL,   -- 游标值(字符串存储,兼容时间戳/区块号/分页token)
    updated_at    TIMESTAMP    DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    created_at    TIMESTAMP    DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_sc_source_chain_protocol_key (source, chain_type, protocol, cursor_key)
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;