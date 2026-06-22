# Chain Parse Service - API 接口文档

## 1. API 概述

### 1.1 基本信息

| 项目 | 说明 |
|------|------|
| 基础 URL | `http://<host>:8081` |
| 协议 | HTTP/1.1 |
| 数据格式 | JSON |
| 字符编码 | UTF-8 |
| API 版本 | v1 |
| 框架 | Gin (Go) |

### 1.2 通用请求头

| 请求头 | 必填 | 说明 |
|--------|------|------|
| `Content-Type` | 否 | 当前所有接口均为 GET 请求，无需设置 |
| `X-Request-ID` | 否 | 请求追踪 ID。若客户端未提供，服务端自动生成 32 位十六进制字符串并在响应头中返回 |

### 1.3 通用响应格式

**成功响应：**

```json
{
  "key": "value"
}
```

成功响应直接返回业务数据，HTTP 状态码为 `200 OK`。

**错误响应：**

```json
{
  "error": {
    "code": "ERROR_CODE",
    "message": "错误描述信息",
    "request_id": "请求追踪ID"
  }
}
```

### 1.4 中间件

所有 API 请求经过以下中间件处理（按顺序）：

1. **RequestID** - 为每个请求分配唯一追踪 ID，通过 `X-Request-ID` 响应头返回
2. **Logger** - 记录请求日志（方法、路径、状态码、耗时、客户端 IP 等）
3. **Recovery** - panic 恢复，确保服务不会因未捕获异常而崩溃
4. **CORS** - 跨域资源共享控制，默认允许所有来源（可通过配置修改）

CORS 支持的请求方法：`GET, POST, PUT, DELETE, OPTIONS`
CORS 支持的请求头：`Content-Type, Authorization, X-Request-ID`
CORS 预检缓存时间：86400 秒（24 小时）

---

## 2. 接口详情

### 2.1 健康检查

检查服务及其依赖组件的运行状态。

**请求：**

```
GET /health
```

**请求参数：** 无

**响应示例（所有组件正常）：**

```json
{
  "status": "ok",
  "timestamp": "2026-03-08T10:30:00.000Z",
  "storage": {
    "status": "ok"
  },
  "progress_tracker": {
    "status": "ok"
  }
}
```

**响应示例（存储异常）：**

```json
{
  "status": "ok",
  "timestamp": "2026-03-08T10:30:00.000Z",
  "storage": {
    "status": "error",
    "error": "connection refused"
  }
}
```

**响应字段说明：**

| 字段 | 类型 | 说明 |
|------|------|------|
| `status` | string | 服务整体状态，始终为 `"ok"` 表示 API 服务本身可用 |
| `timestamp` | string | 当前服务器时间（ISO 8601） |
| `storage.status` | string | 存储引擎状态：`"ok"` 或 `"error"` |
| `storage.error` | string | 存储引擎错误信息（仅在异常时出现） |
| `progress_tracker` | object | 进度跟踪器状态（仅在配置了 Redis 进度跟踪器时出现） |
| `progress_tracker.status` | string | 跟踪器状态：`"ok"` 或 `"error"` |
| `progress_tracker.error` | string | 跟踪器错误信息（仅在异常时出现） |

---

### 2.2 根据哈希查询交易

根据交易哈希查询链上交易详情。

**请求：**

```
GET /api/v1/transactions/:hash
```

**路径参数：**

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `hash` | string | 是 | 交易哈希。EVM 链支持带或不带 `0x` 前缀的 64 位十六进制字符串；Solana 等非 EVM 链支持长度 >= 20 的 base58 格式哈希 |

**响应示例（成功）：**

```json
{
  "transaction": {
    "tx_hash": "0xabc123def456789...",
    "chain_type": "bsc",
    "chain_id": "bsc-mainnet",
    "block_number": 12345678,
    "block_hash": "0x789abc...",
    "tx_index": 42,
    "from_address": "0x1234567890abcdef...",
    "to_address": "0x5678abcdef012345...",
    "value": "1000000000000000000",
    "gas_limit": 21000,
    "gas_used": 21000,
    "gas_price": 5000000000,
    "status": "success",
    "timestamp": "2026-03-08T10:30:00Z",
    "raw_data": {}
  }
}
```

**响应字段说明：**

| 字段 | 类型 | 说明 |
|------|------|------|
| `tx_hash` | string | 交易哈希 |
| `chain_type` | string | 链类型（`bsc`、`ethereum`、`solana`、`sui`） |
| `chain_id` | string | 链 ID（如 `bsc-mainnet`、`ethereum-mainnet`） |
| `block_number` | integer | 区块号 |
| `block_hash` | string | 区块哈希 |
| `tx_index` | integer | 交易在区块中的索引 |
| `from_address` | string | 发送方地址 |
| `to_address` | string | 接收方地址 |
| `value` | string | 交易金额（原始精度，字符串格式） |
| `gas_limit` | integer | Gas 上限 |
| `gas_used` | integer | 实际使用 Gas |
| `gas_price` | integer | Gas 价格（wei） |
| `status` | string | 交易状态 |
| `timestamp` | string | 交易时间（ISO 8601） |
| `raw_data` | object | 原始交易数据（JSON 格式） |

**错误响应示例：**

哈希格式不合法：

```json
{
  "error": {
    "code": "INVALID_PARAMETER",
    "message": "invalid transaction hash format",
    "request_id": "a1b2c3d4e5f67890..."
  }
}
```

交易不存在：

```json
{
  "error": {
    "code": "NOT_FOUND",
    "message": "transaction not found",
    "request_id": "a1b2c3d4e5f67890..."
  }
}
```

---

### 2.3 存储统计

获取存储引擎的统计数据，包括各链交易数量等。

**请求：**

```
GET /api/v1/storage/stats
```

**请求参数：** 无

**响应示例（PostgreSQL / MySQL 存储引擎）：**

```json
{
  "total_transactions": 1500000,
  "total_dex_transactions": 850000,
  "chain_stats": {
    "bsc": {
      "transactions": 600000
    },
    "ethereum": {
      "transactions": 500000
    },
    "solana": {
      "transactions": 300000
    },
    "sui": {
      "transactions": 100000
    }
  }
}
```

**响应示例（InfluxDB 存储引擎）：**

```json
{
  "storage_type": "influxdb",
  "bucket": "bsc",
  "url": "http://localhost:8086",
  "status": "connected"
}
```

**响应字段说明（关系型数据库）：**

| 字段 | 类型 | 说明 |
|------|------|------|
| `total_transactions` | integer | 基础交易总数（`transactions` 表） |
| `total_dex_transactions` | integer | DEX 交易总数（`dex_transactions` 表） |
| `chain_stats` | object | 按链类型分组的统计 |
| `chain_stats.<chain>.transactions` | integer | 该链的交易数量 |

---

### 2.4 解析进度

获取各链的解析处理进度。需要配置 Redis 进度跟踪器。

**请求：**

```
GET /api/v1/progress
```

**请求参数：** 无

**响应示例：**

```json
{
  "progress": {
    "bsc": {
      "chain_type": "bsc",
      "last_processed_block": 45678901,
      "last_update_time": "2026-03-08T10:30:00Z",
      "processing_status": "running",
      "total_transactions": 600000,
      "total_events": 1200000,
      "error_count": 5,
      "success_rate": 99.99,
      "start_time": "2026-03-01T00:00:00Z"
    },
    "ethereum": {
      "chain_type": "ethereum",
      "last_processed_block": 19876543,
      "last_update_time": "2026-03-08T10:29:55Z",
      "processing_status": "running",
      "total_transactions": 500000,
      "total_events": 980000,
      "error_count": 2,
      "success_rate": 99.99,
      "start_time": "2026-03-01T00:00:00Z"
    }
  }
}
```

**响应字段说明：**

| 字段 | 类型 | 说明 |
|------|------|------|
| `progress` | object | 各链进度的映射，键为链类型 |
| `chain_type` | string | 链类型 |
| `last_processed_block` | integer | 最后处理的区块号 |
| `last_update_time` | string | 最后更新时间（ISO 8601） |
| `processing_status` | string | 处理状态：`idle` / `running` / `syncing` / `catching_up` |
| `total_transactions` | integer | 已处理交易总数 |
| `total_events` | integer | 已处理事件总数 |
| `error_count` | integer | 累计错误数 |
| `success_rate` | float | 成功率（百分比） |
| `start_time` | string | 处理开始时间 |

**错误响应（进度跟踪器未配置）：**

```json
{
  "error": {
    "code": "SERVICE_UNAVAILABLE",
    "message": "progress tracker not configured",
    "request_id": "a1b2c3d4e5f67890..."
  }
}
```

---

### 2.5 全局统计

获取所有链的全局处理统计数据。需要配置 Redis 进度跟踪器。

**请求：**

```
GET /api/v1/progress/stats
```

**请求参数：** 无

**响应示例：**

```json
{
  "total_chains": 4,
  "active_chains": 3,
  "total_transactions": 1500000,
  "total_events": 3180000,
  "overall_success_rate": 99.98,
  "last_update_time": "2026-03-08T10:30:00Z",
  "chain_stats": {
    "bsc": {
      "chain_type": "bsc"
    },
    "ethereum": {
      "chain_type": "ethereum"
    }
  }
}
```

**响应字段说明：**

| 字段 | 类型 | 说明 |
|------|------|------|
| `total_chains` | integer | 已注册的链总数 |
| `active_chains` | integer | 当前活跃（正在处理）的链数 |
| `total_transactions` | integer | 所有链的交易总数 |
| `total_events` | integer | 所有链的事件总数 |
| `overall_success_rate` | float | 整体成功率（百分比） |
| `last_update_time` | string | 统计更新时间 |
| `chain_stats` | object | 各链的详细统计 |

**错误响应（进度跟踪器未配置）：**

```json
{
  "error": {
    "code": "SERVICE_UNAVAILABLE",
    "message": "progress tracker not configured",
    "request_id": "a1b2c3d4e5f67890..."
  }
}
```

---

## 3. 错误码参考

| HTTP 状态码 | 错误码 | 说明 | 触发场景 |
|-------------|--------|------|----------|
| 400 | `INVALID_PARAMETER` | 请求参数无效 | 交易哈希为空或格式不合法 |
| 404 | `NOT_FOUND` | 资源未找到 | 查询的交易在数据库中不存在 |
| 500 | `INTERNAL_ERROR` | 服务器内部错误 | 数据库查询失败、序列化异常、未捕获的 panic |
| 503 | `SERVICE_UNAVAILABLE` | 服务不可用 | 进度跟踪器（Redis）未配置或连接断开 |

---

## 4. 配置说明

API 服务配置位于 `configs/api.yaml`，继承 `configs/base.yaml` 中的共享配置：

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `api.port` | int | `8081` | HTTP 监听端口 |
| `api.read_timeout` | int | `30` | 读超时（秒） |
| `api.write_timeout` | int | `30` | 写超时（秒） |
| `api.allow_origins` | []string | `["*"]` | CORS 允许的来源域名列表 |
| `api.rate_limit` | int | `100` | 速率限制（请求/秒） |
| `api.rate_burst` | int | `200` | 突发请求上限 |

**环境差异：**

| 环境 | `allow_origins` | `rate_limit` | `rate_burst` |
|------|-----------------|--------------|--------------|
| 开发/默认 | `["*"]` | 100 | 200 |
| Staging | `["*"]` | 200 | 400 |
| 生产 | `[]`（需手动配置具体域名） | 50 | 100 |

---

## 5. 接口路由总览

| 方法 | 路径 | 说明 | 认证 |
|------|------|------|------|
| GET | `/health` | 健康检查 | 无 |
| GET | `/api/v1/transactions/:hash` | 根据哈希查询交易 | 无 |
| GET | `/api/v1/storage/stats` | 存储统计 | 无 |
| GET | `/api/v1/progress` | 解析进度 | 无 |
| GET | `/api/v1/progress/stats` | 全局统计 | 无 |

---

## 6. 使用示例

### cURL

```bash
# 健康检查
curl http://localhost:8081/health

# 查询交易（EVM 链）
curl http://localhost:8081/api/v1/transactions/0xabc123def456789...

# 查询交易（Solana base58 格式）
curl http://localhost:8081/api/v1/transactions/5VERv8NMhJr4fE9K...

# 存储统计
curl http://localhost:8081/api/v1/storage/stats

# 解析进度
curl http://localhost:8081/api/v1/progress

# 全局统计
curl http://localhost:8081/api/v1/progress/stats
```

### 自定义 Request ID

```bash
curl -H "X-Request-ID: my-trace-id-001" \
     http://localhost:8081/api/v1/transactions/0xabc123...
```
