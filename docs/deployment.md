# Chain Parse Service - 部署文档

## 1. 环境要求

### 1.1 开发环境

| 依赖 | 最低版本 | 说明 |
|------|----------|------|
| Go | 1.21+ | 编译语言 |
| Docker | 20.10+ | 容器化运行基础设施 |
| Docker Compose | v2+ | 多容器编排（使用 `docker compose` 命令） |
| Make | 3.81+ | 构建工具 |
| Git | 2.0+ | 版本管理 |

### 1.2 生产环境基础设施

| 组件 | 推荐版本 | 用途 | 是否必须 |
|------|----------|------|----------|
| PostgreSQL | 16-alpine | 主存储引擎（默认） | 是（三选一） |
| MySQL | 8.0 | 备选存储引擎 | 否 |
| InfluxDB | 2.7 | 时序数据存储 | 否 |
| Redis | 7-alpine | 解析进度跟踪 | 是 |
| Grafana | 10-alpine | 监控看板 | 否 |

### 1.3 系统资源建议

| 场景 | CPU | 内存 | 磁盘 |
|------|-----|------|------|
| 单链解析（开发） | 2 核 | 2 GB | 20 GB |
| 单链解析（生产） | 4 核 | 4 GB | 100 GB |
| 四链并行（生产） | 8 核 | 8 GB | 500 GB |

---

## 2. 项目克隆和初始化

### 2.1 获取代码

```bash
git clone <repository-url> chain-parse-service
cd chain-parse-service
```

### 2.2 安装 Go 依赖

```bash
go mod download
```

### 2.3 验证编译

```bash
make build-all
```

编译成功后会在 `bin/` 目录下生成两个二进制文件：
- `bin/parser` - 链解析服务
- `bin/api` - HTTP API 服务

### 2.4 运行测试

```bash
make test          # 基础测试
make test-race     # 带竞态检测
make test-cover    # 测试覆盖率报告
```

---

## 3. 基础设施启动

### 3.1 Docker Compose 启动

```bash
cd docker
cp .env.example .env    # 复制环境变量模板
```

编辑 `.env` 文件，修改密码和端口（按需）：

```bash
# 链类型（sui/ethereum/bsc/solana）
CHAIN_TYPE=bsc

# PostgreSQL
POSTGRES_USER=postgres
POSTGRES_PASSWORD=your_secure_password
POSTGRES_PORT=5432

# MySQL（如使用 MySQL 存储引擎）
MYSQL_ROOT_PASSWORD=your_secure_password
MYSQL_PASSWORD=parser_pass
MYSQL_PORT=3306

# Redis
REDIS_PORT=6379

# InfluxDB（如使用时序存储）
INFLUXDB_USERNAME=admin
INFLUXDB_PASSWORD=your_secure_password
INFLUXDB_ORG=unified-tx-parser
INFLUXDB_BUCKET=blockchain-data
INFLUXDB_RETENTION=90d
INFLUXDB_TOKEN=your_secure_token
INFLUXDB_PORT=8086

# Grafana
GRAFANA_PORT=3000

# API
API_PORT=8081
```

#### 最小化启动（推荐）

仅启动必需的 PostgreSQL 和 Redis：

```bash
docker compose up -d postgres redis
```

#### 完整启动

启动所有基础设施（PostgreSQL + MySQL + Redis + InfluxDB + Grafana）：

```bash
docker compose up -d
```

### 3.2 验证基础设施状态

```bash
docker compose ps
```

等待所有服务 health check 通过后再启动应用。可通过以下命令查看日志：

```bash
docker compose logs -f postgres
docker compose logs -f redis
```

### 3.3 数据库初始化

Docker Compose 会自动执行 schema 初始化：
- PostgreSQL：挂载 `database/pgsql/schema.sql` 到 `/docker-entrypoint-initdb.d/`
- MySQL：挂载 `database/mysql/schema.sql` 到 `/docker-entrypoint-initdb.d/`

如需手动初始化：

```bash
# PostgreSQL
psql -U postgres -d unified_tx_parser -f database/pgsql/schema.sql

# MySQL
mysql -u root -p unified_tx_parser < database/mysql/schema.sql
```

---

## 4. 配置文件说明

配置采用**分层合并策略**：`base.yaml` -> 链配置（如 `bsc.yaml`） -> 环境配置（如 `env/prod.yaml`） -> 环境变量覆盖。

### 4.1 base.yaml - 基础共享配置

所有服务和链共享的默认配置，位于 `configs/base.yaml`。

```yaml
# API 服务
api:
  port: 8081              # HTTP 监听端口
  read_timeout: 30        # 读超时（秒）
  write_timeout: 30       # 写超时（秒）
  allow_origins: ["*"]    # CORS 允许来源
  rate_limit: 100         # 速率限制（请求/秒）
  rate_burst: 200         # 突发请求上限

# Redis 缓存/进度跟踪
redis:
  host: "localhost"
  port: 6379
  password: ""
  db: 0
  maxRetries: 3
  poolSize: 10

# 解析处理器
processor:
  batch_size: 10          # 每批处理区块数
  max_concurrent: 10      # 最大并发数
  retry_delay: 5          # 重试延迟（秒）
  max_retries: 3          # 最大重试次数

# 日志
logging:
  level: "info"           # 日志级别：debug/info/warn/error
  format: "text"          # 日志格式：text/json
  output: "stdout"        # 输出目标

# 存储引擎
storage:
  type: "pgsql"           # 存储类型：pgsql/mysql/influxdb

  pgsql:
    host: "localhost"
    port: 5432
    username: "postgres"
    password: "password"
    database: "unified_tx_parser"
    sslmode: "disable"
    max_open_conns: 100
    max_idle_conns: 10
    conn_max_lifetime: 3600   # 连接最大生存时间（秒）

  mysql:
    host: "localhost"
    port: 3306
    username: "parser_user"
    password: "parser_pass"
    database: "unified_tx_parser"
    charset: "utf8mb4"
    max_open_conns: 100
    max_idle_conns: 10
    conn_max_lifetime: 3600

  influxdb:
    url: "http://localhost:8086"
    token: "unified-tx-parser-token-2024"
    org: "unified-tx-parser"
    bucket: "blockchain-data"
    batch_size: 1000
    flush_time: 10            # 缓存刷新间隔（秒）
    precision: "ms"
```

### 4.2 链配置文件

每条链有独立的配置文件，继承并覆盖 `base.yaml`。

#### bsc.yaml - BSC 链

```yaml
chains:
  bsc:
    enabled: true
    rpc_endpoint: "https://bsc.publicnode.com"
    chain_id: "bsc-mainnet"
    batch_size: 10
    timeout: 90               # RPC 超时（秒）
    retry_count: 3
    start_block: 0            # 起始区块号（0 表示从最新开始）

protocols:
  pancakeswap:
    enabled: true
    chain: "bsc"
    contract_addresses:       # 工厂合约地址
      - "0xcA143Ce32Fe78f1f7019d7d551a6402fC5350c73"   # V2
      - "0x0BFbCF9fa4f9C56B0F40a671Ad40E0805A091865"   # V3
  fourmeme:
    enabled: true
    chain: "bsc"
    contract_addresses:
      - "0xEC4549caDcE5DA21Df6E6422d448034B5233bFbC"   # V2
      - "0x5c952063c7fc8610FFDB798152D69F0B9550762b"   # V1

quoteAssets:                  # 报价资产（按 rank 排序确定交易方向）
  - name: USDT
    addr: "0x55d398326f99059fF775485246999027B3197955"
    rank: 100
  - name: USDC
    addr: "0x8AC76a51cc950d9822D68b83fE1Ad97B32Cd580d"
    rank: 99
  - name: BUSD
    addr: "0xe9e7CEA3DedcA5984780Bafc599bD69ADd087D56"
    rank: 98
  - name: WBNB
    addr: "0xbb4CdB9CBd36B01bD1cBaEBF2De08d9173bc095c"
    rank: 95

processor:                    # 覆盖 base 的处理器参数
  batch_size: 8
  max_concurrent: 5
  retry_delay: 8
  max_retries: 4

storage:
  influxdb:
    bucket: "bsc"             # 链专用 InfluxDB bucket
```

#### ethereum.yaml - Ethereum 链

```yaml
chains:
  ethereum:
    enabled: true
    rpc_endpoint: "https://eth.rpc.blxrbdn.com"
    chain_id: "ethereum-mainnet"
    timeout: 120              # Ethereum RPC 较慢，超时更长
    retry_count: 3
    start_block: 0

protocols:
  uniswap:
    enabled: true
    chain: "ethereum"

processor:
  batch_size: 5               # Ethereum 区块较大，批量较小
  max_concurrent: 3
  retry_delay: 10
  max_retries: 5
```

#### solana.yaml - Solana 链

```yaml
chains:
  solana:
    enabled: true
    rpc_endpoint: "https://api.mainnet-beta.solana.com"
    chain_id: "solana-mainnet"
    timeout: 30

protocols:
  pumpfun:
    enabled: true
    chain: "solana"
    program_id: "6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P"
  pumpswap:
    enabled: true
    chain: "solana"
    program_id: "pAMMBay6oceH9fJKBRHGP5D4bD4sWpmSwMn52FMfXEA"
```

#### sui.yaml - Sui 链

```yaml
chains:
  sui:
    enabled: true
    rpc_endpoint: "https://fullnode.mainnet.sui.io:443"
    chain_id: "sui-mainnet"
    timeout: 30

protocols:
  bluefin:
    enabled: true
    chain: "sui"
    contract_addresses:
      - "0x3492c874c1e3b3e2984e8c41b589e642d4d0a5d6459e5a9cfc2d52fd7c89c267"
```

### 4.3 环境覆盖配置

#### env/prod.yaml - 生产环境

```yaml
logging:
  level: "warn"               # 生产环境降低日志级别
  format: "json"              # JSON 格式便于日志收集

processor:
  batch_size: 20              # 更大批量提高吞吐
  max_concurrent: 20
  max_retries: 5

storage:
  pgsql:
    max_open_conns: 200       # 更大连接池
    max_idle_conns: 20
    conn_max_lifetime: 1800

redis:
  poolSize: 20
  maxRetries: 5

api:
  allow_origins: []           # 生产环境需手动配置允许的域名
  rate_limit: 50
  rate_burst: 100
```

#### env/staging.yaml - 预发布环境

```yaml
logging:
  level: "debug"
  format: "json"

processor:
  batch_size: 15
  max_concurrent: 10

api:
  rate_limit: 200
  rate_burst: 400
```

### 4.4 api.yaml - API 服务专用配置

```yaml
api:
  port: 8081
  read_timeout: 30
  write_timeout: 30
  allow_origins: ["*"]
  rate_limit: 100
  rate_burst: 200
```

API 服务启动时加载此文件，与 `base.yaml` 合并。

---

## 5. 服务启动顺序和命令

### 5.1 启动顺序

```
1. 基础设施（PostgreSQL + Redis）
       |
       v
2. Parser 服务（按链启动，可并行）
       |
       v
3. API 服务
```

Parser 和 API 服务可独立启动，但都依赖 PostgreSQL 和 Redis。

### 5.2 启动 Parser 服务

Parser 服务需要指定要解析的链类型：

```bash
# 方式一：Make 命令（自动编译 + 运行）
make run-parser CHAIN=bsc
make run-parser CHAIN=ethereum
make run-parser CHAIN=solana
make run-parser CHAIN=sui

# 方式二：直接运行二进制
make build-parser
./bin/parser -chain bsc
```

### 5.3 启动 API 服务

```bash
# 方式一：Make 命令
make run-api

# 方式二：直接运行
make build-api
./bin/api

# 方式三：指定配置文件
./bin/api -config configs/api.yaml
```

API 默认监听 `0.0.0.0:8081`。

---

## 6. Docker 方式部署

### 6.1 构建 Docker 镜像

```bash
# 构建所有镜像
make docker-build

# 单独构建
make docker-build-parser    # 构建 Parser 镜像
make docker-build-api       # 构建 API 镜像
```

镜像命名规则：
- `chain-parse-parser:latest` / `chain-parse-parser:<version>`
- `chain-parse-api:latest` / `chain-parse-api:<version>`

如需推送到私有仓库，设置 `DOCKER_REGISTRY` 环境变量：

```bash
DOCKER_REGISTRY=registry.example.com make docker-build
```

### 6.2 Dockerfile 说明

项目使用多阶段构建（`docker/Dockerfile`）：

- **构建阶段**：基于 `golang:1.21-alpine`，通过 `SERVICE` 构建参数选择编译 parser 或 api
- **运行阶段**：基于 `alpine:3.19`，仅包含二进制文件和配置文件，以非 root 用户 (`appuser`) 运行
- **版本注入**：通过 `VERSION` 和 `COMMIT` 构建参数注入版本信息

### 6.3 完整 Docker Compose 启动

```bash
cd docker

# 仅启动基础设施
docker compose up -d

# 启动基础设施 + 应用服务
CHAIN_TYPE=bsc docker compose --profile app up -d
```

> 注意：应用服务在 `app` profile 下，不指定 `--profile app` 时不会启动 parser 和 api 容器。

### 6.4 Docker 管理命令

```bash
make docker-up       # 启动基础设施
make docker-down     # 停止所有服务
make docker-ps       # 查看运行状态
make docker-logs     # 查看日志（tail -f）
```

---

## 7. 多链同时运行

每条链需要独立的 Parser 进程。API 服务只需一个实例即可查询所有链的数据。

### 7.1 本地多链运行

```bash
# 启动基础设施
make docker-up

# 后台启动多条链的 Parser
make run-parser CHAIN=bsc &
make run-parser CHAIN=ethereum &
make run-parser CHAIN=solana &
make run-parser CHAIN=sui &

# 启动 API 服务
make run-api
```

### 7.2 Docker 多链运行

需要为每条链创建独立的 Parser 容器，修改 `docker-compose.yml` 或使用 override：

```yaml
# docker-compose.override.yml
services:
  parser-bsc:
    extends:
      service: parser
    container_name: chain_parse_parser_bsc
    environment:
      CHAIN_TYPE: bsc

  parser-ethereum:
    extends:
      service: parser
    container_name: chain_parse_parser_ethereum
    environment:
      CHAIN_TYPE: ethereum

  parser-solana:
    extends:
      service: parser
    container_name: chain_parse_parser_solana
    environment:
      CHAIN_TYPE: solana

  parser-sui:
    extends:
      service: parser
    container_name: chain_parse_parser_sui
    environment:
      CHAIN_TYPE: sui
```

启动：

```bash
docker compose --profile app up -d
```

---

## 8. 健康检查

### 8.1 API 健康检查

```bash
curl http://localhost:8081/health
```

期望响应：

```json
{
  "status": "ok",
  "timestamp": "2026-03-08T10:30:00Z",
  "storage": { "status": "ok" },
  "progress_tracker": { "status": "ok" }
}
```

### 8.2 Docker 容器健康检查

Docker Compose 已为每个基础设施服务配置了健康检查：

| 服务 | 健康检查命令 | 间隔 | 超时 | 重试 |
|------|-------------|------|------|------|
| PostgreSQL | `pg_isready -U postgres` | 10s | 5s | 5 次 |
| MySQL | `mysqladmin ping -h localhost` | - | 20s | 10 次 |
| Redis | `redis-cli ping` | 10s | 5s | 5 次 |
| InfluxDB | `influx ping` | 10s | 5s | 5 次 |

检查容器健康状态：

```bash
docker compose ps
# 或
docker inspect --format='{{.State.Health.Status}}' chain_parse_postgres
```

### 8.3 数据库连通性验证

```bash
# PostgreSQL
docker exec chain_parse_postgres pg_isready -U postgres

# MySQL
docker exec chain_parse_mysql mysqladmin ping -h localhost

# Redis
docker exec chain_parse_redis redis-cli ping

# InfluxDB
docker exec chain_parse_influxdb influx ping
```

---

## 9. 常见问题排查

### 9.1 Parser 启动失败

**问题：** `connecting to PostgreSQL: connection refused`

**原因：** 数据库尚未启动或未通过健康检查

**解决：**
```bash
# 检查 PostgreSQL 是否在运行
docker compose ps postgres
# 查看日志
docker compose logs postgres
# 等待健康检查通过后重试
```

---

**问题：** `make run-parser` 报错 `Usage: make run-parser CHAIN=bsc`

**原因：** 未指定 `CHAIN` 参数

**解决：**
```bash
make run-parser CHAIN=bsc    # 必须指定链类型
```

---

### 9.2 API 无法连接数据库

**问题：** 存储统计接口返回 `INTERNAL_ERROR`

**排查步骤：**
1. 检查 `/health` 接口中 `storage.status` 是否为 `error`
2. 确认 `configs/base.yaml` 中的数据库连接信息与 `docker/.env` 一致
3. 确认 `storage.type` 配置正确（`pgsql` / `mysql` / `influxdb`）
4. 检查数据库容器日志

---

### 9.3 进度接口返回 503

**问题：** `GET /api/v1/progress` 返回 `SERVICE_UNAVAILABLE`

**原因：** Redis 进度跟踪器未配置或连接失败

**解决：**
```bash
# 检查 Redis 是否运行
docker exec chain_parse_redis redis-cli ping
# 检查 configs/base.yaml 中 redis 配置
```

---

### 9.4 Docker 构建失败

**问题：** `go mod download` 超时

**解决：** 设置 Go 代理：
```bash
docker build --build-arg GOPROXY=https://goproxy.cn,direct ...
```

或在 Dockerfile 中添加环境变量。

---

### 9.5 端口冲突

**问题：** `bind: address already in use`

**解决：** 修改 `docker/.env` 中的端口映射：
```bash
POSTGRES_PORT=15432
REDIS_PORT=16379
API_PORT=18081
```

---

### 9.6 InfluxDB 写入失败

**问题：** 日志显示 `连接InfluxDB失败` 或 `InfluxDB健康检查失败`

**排查步骤：**
1. 确认 InfluxDB 容器健康：`docker compose ps influxdb`
2. 检查 Token 是否与 `.env` 中 `INFLUXDB_TOKEN` 一致
3. 检查 Organization 和 Bucket 名称
4. 访问 Grafana（`http://localhost:3000`）确认数据源连接

---

### 9.7 跨平台编译

如需在 macOS 上编译 Linux 二进制：

```bash
make build-linux-amd64     # Linux x86_64
make build-linux-arm64     # Linux ARM64
make build-cross           # 所有平台（linux/darwin x amd64/arm64）
```

编译产物在 `bin/` 目录下，文件名包含平台信息（如 `parser-linux-amd64`）。

---

## 10. 生产部署检查清单

- [ ] 修改所有默认密码（PostgreSQL、MySQL、Redis、InfluxDB、Grafana）
- [ ] 配置 `configs/env/prod.yaml` 或通过环境变量覆盖敏感配置
- [ ] 将日志级别设为 `warn`，格式设为 `json`
- [ ] 配置 CORS `allow_origins` 为具体域名（不使用 `*`）
- [ ] 调整连接池大小（`max_open_conns`、`poolSize`）匹配预期负载
- [ ] 配置 Redis 持久化（默认已启用 AOF，`appendfsync everysec`）
- [ ] 设置 Redis 内存限制（默认 256MB，按需调整）
- [ ] 配置进程管理（systemd / supervisor）确保服务自动重启
- [ ] 配置日志收集（ELK / Loki）
- [ ] 配置 Grafana 告警规则
- [ ] 设置数据库定期备份
- [ ] 对外部 RPC 端点配置合理的超时和重试参数
