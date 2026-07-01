-- PostgreSQL Schema
-- Usage: psql -U <user> -d <database> -f schema.sql

-- ============================================================
-- 区块表
-- ============================================================

CREATE TABLE IF NOT EXISTS blocks (
                                      id BIGSERIAL PRIMARY KEY,
                                      block_number BIGINT NOT NULL,
                                      block_hash VARCHAR(128) NOT NULL,
    chain_type VARCHAR(32) NOT NULL,
    chain_id VARCHAR(64) NOT NULL,
    parent_hash VARCHAR(128),
    timestamp TIMESTAMP,
    tx_count INT DEFAULT 0,
    size BIGINT DEFAULT 0,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW(),
    UNIQUE (block_hash, chain_type)
    );

CREATE INDEX IF NOT EXISTS idx_blocks_chain_number ON blocks (chain_type, block_number);
CREATE INDEX IF NOT EXISTS idx_blocks_timestamp ON blocks (timestamp);

-- ============================================================
-- 基础交易表
-- ============================================================

CREATE TABLE IF NOT EXISTS transactions (
                                            id BIGSERIAL PRIMARY KEY,
                                            tx_hash VARCHAR(128) NOT NULL,
    chain_type VARCHAR(32) NOT NULL,
    chain_id VARCHAR(64) NOT NULL,
    block_number BIGINT,
    block_hash VARCHAR(128),
    tx_index INT,
    from_address VARCHAR(128),
    to_address VARCHAR(128),
    value NUMERIC(78, 0),
    gas_limit BIGINT,
    gas_used BIGINT,
    gas_price BIGINT,
    status VARCHAR(32),
    timestamp TIMESTAMP,
    raw_data JSONB,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW(),
    UNIQUE (tx_hash, chain_type)
    );

CREATE INDEX IF NOT EXISTS idx_ut_chain_block ON transactions (chain_type, block_number);
CREATE INDEX IF NOT EXISTS idx_ut_timestamp ON transactions (timestamp);
CREATE INDEX IF NOT EXISTS idx_ut_from_address ON transactions (from_address);
CREATE INDEX IF NOT EXISTS idx_ut_to_address ON transactions (to_address);

-- ============================================================
-- 处理进度表
-- ============================================================

CREATE TABLE IF NOT EXISTS processing_progress (
                                                   chain_type           VARCHAR(32)  PRIMARY KEY,
    last_processed_block BIGINT       NOT NULL DEFAULT 0,
    last_update_time     TIMESTAMPTZ,
    total_transactions   BIGINT       DEFAULT 0,
    total_events         BIGINT       DEFAULT 0,
    status               VARCHAR(20)  NOT NULL DEFAULT 'idle',
    error_count          BIGINT       NOT NULL DEFAULT 0,
    success_rate         FLOAT        NOT NULL DEFAULT 100.0,
    start_time           TIMESTAMPTZ  NULL,
    extra                JSONB        NULL,
    created_at           TIMESTAMPTZ  DEFAULT NOW(),
    updated_at           TIMESTAMPTZ  DEFAULT NOW()
    );

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
    source VARCHAR(32),  -- 数据来源: 'onchain' / 'thegraph'
    extra JSONB,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
    );

CREATE INDEX IF NOT EXISTS idx_dp_protocol ON dex_pools (protocol);
CREATE INDEX IF NOT EXISTS idx_dp_factory ON dex_pools (factory);

-- ============================================================
-- DEX 代币表
-- ============================================================

CREATE TABLE IF NOT EXISTS dex_tokens (
                                          addr VARCHAR(256) PRIMARY KEY,
    name VARCHAR(255),
    symbol VARCHAR(128),
    decimals INT DEFAULT 0,
    is_stable BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
    );

-- ============================================================
-- DEX 交易表
-- ============================================================

CREATE TABLE IF NOT EXISTS dex_transactions (
                                                id BIGSERIAL PRIMARY KEY,
                                                addr VARCHAR(256) NOT NULL,
    protocol VARCHAR(64),
    router VARCHAR(256),
    factory VARCHAR(256),
    pool VARCHAR(256) NOT NULL,
    hash VARCHAR(128) NOT NULL,
    from_addr VARCHAR(256),
    side VARCHAR(16) NOT NULL,
    amount NUMERIC(78, 0),
    price DOUBLE PRECISION DEFAULT 0,
    value DOUBLE PRECISION DEFAULT 0,
    time BIGINT NOT NULL,
    event_index BIGINT DEFAULT 0,
    tx_index BIGINT DEFAULT 0,
    swap_index BIGINT DEFAULT 0,
    block_number BIGINT DEFAULT 0,
    extra JSONB,
    created_at TIMESTAMP DEFAULT NOW(),
    UNIQUE (hash, event_index, side)
    );

CREATE INDEX IF NOT EXISTS idx_dt_pool ON dex_transactions (pool);
CREATE INDEX IF NOT EXISTS idx_dt_time ON dex_transactions (time);
CREATE INDEX IF NOT EXISTS idx_dt_block ON dex_transactions (block_number);

-- ============================================================
-- DEX 流动性表
-- ============================================================

CREATE TABLE IF NOT EXISTS dex_liquidities (
                                               id BIGSERIAL PRIMARY KEY,
                                               addr VARCHAR(256) NOT NULL,
    router VARCHAR(256),
    factory VARCHAR(256),
    pool VARCHAR(256) NOT NULL,
    hash VARCHAR(128) NOT NULL,
    from_addr VARCHAR(256),
    pos VARCHAR(256),
    side VARCHAR(16) NOT NULL,
    amount NUMERIC(78, 0),
    value DOUBLE PRECISION DEFAULT 0,
    time BIGINT NOT NULL,
    key VARCHAR(512),
    extra JSONB,
    created_at TIMESTAMP DEFAULT NOW(),
    UNIQUE (key, addr)
    );

CREATE INDEX IF NOT EXISTS idx_dl_pool ON dex_liquidities (pool);
CREATE INDEX IF NOT EXISTS idx_dl_time ON dex_liquidities (time);

-- ============================================================
-- DEX 储备表
-- ============================================================

CREATE TABLE IF NOT EXISTS dex_reserves (
                                            id BIGSERIAL PRIMARY KEY,
                                            addr VARCHAR(256) NOT NULL,
    protocol VARCHAR(64),
    amount0 NUMERIC(78, 0),
    amount1 NUMERIC(78, 0),
    time BIGINT NOT NULL,
    created_at TIMESTAMP DEFAULT NOW(),
    UNIQUE (addr, time)
    );

CREATE INDEX IF NOT EXISTS idx_dr_time ON dex_reserves (time);
-- ============================================================
-- 处理错误历史表 (DBProgressTracker 使用)
-- ============================================================
CREATE TABLE IF NOT EXISTS processing_errors (
                                                 id         BIGSERIAL PRIMARY KEY,
                                                 chain_type VARCHAR(32) NOT NULL,
    error_time TIMESTAMPTZ NOT NULL,
    error_type VARCHAR(256),
    error_msg  TEXT
    );
CREATE INDEX IF NOT EXISTS idx_pe_chain_time ON processing_errors (chain_type, error_time);

-- ============================================================
-- 处理性能指标表 (DBProgressTracker 使用)
-- ============================================================
CREATE TABLE IF NOT EXISTS processing_metrics (
                                                  id                  BIGSERIAL PRIMARY KEY,
                                                  chain_type          VARCHAR(32) NOT NULL,
    timestamp           TIMESTAMPTZ NOT NULL,
    block_number        BIGINT,
    processing_time_ns  BIGINT,
    transaction_count   INT DEFAULT 0,
    event_count         INT DEFAULT 0,
    memory_usage        BIGINT DEFAULT 0,
    cpu_usage           FLOAT DEFAULT 0
    );
CREATE INDEX IF NOT EXISTS idx_pm_chain_time ON processing_metrics (chain_type, timestamp);


-- ============================================================
-- 通用增量同步游标表 (CursorStore 使用)
-- 通用性设计:不写死 thegraph/uniswap/ethereum,同一张表可以给
-- 任意外部数据源 x 任意链 x 任意协议 复用(比如以后接 Covalent/
-- Bitquery,或给 BSC 的 PancakeSwap subgraph 用),只需换
-- source/chain_type/protocol/cursor_key 的取值,不需要新建表或加列。
-- cursor_value 统一存成字符串,可以装时间戳、区块号或不透明的分页token。
-- ============================================================
CREATE TABLE IF NOT EXISTS sync_cursors (
                                            id           BIGSERIAL PRIMARY KEY,
                                            source       VARCHAR(64)  NOT NULL,
    chain_type   VARCHAR(32)  NOT NULL,
    protocol     VARCHAR(64)  NOT NULL,
    cursor_key   VARCHAR(64)  NOT NULL,
    cursor_value VARCHAR(256) NOT NULL,
    updated_at   TIMESTAMPTZ  DEFAULT NOW(),
    created_at   TIMESTAMPTZ  DEFAULT NOW(),
    UNIQUE (source, chain_type, protocol, cursor_key)
    );