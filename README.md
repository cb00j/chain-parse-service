# Chain Parse Service

多链 DEX 数据解析服务，支持实时解析链上交易、流动性、池子等 DEX 事件。

## 支持链和协议

| 链 | 协议 | 状态 |
|---|------|------|
| BSC | PancakeSwap V2/V3, FourMeme | ✅ |
| Ethereum | Uniswap V2/V3 | ✅ |
| Solana | PumpFun, PumpSwap | ✅ |
| Sui | Bluefin | ✅ |

## 项目结构

```
chain-parse-service/
├── cmd/
│   ├── parser/          # 链解析服务入口
│   └── api/             # HTTP API 服务入口
├── configs/
│   ├── base.yaml        # 共享基础配置
│   ├── bsc.yaml         # BSC 链配置
│   ├── ethereum.yaml    # Ethereum 链配置
│   ├── solana.yaml      # Solana 链配置
│   ├── sui.yaml         # Sui 链配置
│   └── api.yaml         # API 服务配置
├── database/
│   ├── mysql/schema.sql
│   └── pgsql/schema.sql
├── docker/              # Docker 部署相关
├── internal/
│   ├── parser/
│   │   ├── chains/      # 各链 RPC 处理器
│   │   ├── dexs/        # DEX 事件提取器
│   │   │   ├── bsc/     # PancakeSwap, FourMeme
│   │   │   ├── eth/     # Uniswap
│   │   │   ├── solanadex/ # PumpFun, PumpSwap
│   │   │   └── suidex/  # Bluefin
│   │   └── engine/      # 解析引擎
│   ├── storage/         # 存储层 (MySQL, PostgreSQL, InfluxDB)
│   ├── api/             # HTTP API
│   └── config/          # 配置加载
└── Makefile
```

## 快速开始

### 1. 环境准备

- Go 1.21+
- Docker & Docker Compose
- Make

### 2. 启动基础设施

```bash
cd docker
cp .env.example .env    # 按需修改密码和端口
docker compose up -d postgres redis
```

### 3. 启动 Parser（选择一条链）

```bash
make run-parser CHAIN=bsc        # BSC 链
make run-parser CHAIN=ethereum   # Ethereum 链
make run-parser CHAIN=solana     # Solana 链
make run-parser CHAIN=sui        # Sui 链
```

### 4. 启动 API 服务

```bash
make run-api
# 默认监听 :8081，配置文件 configs/api.yaml

# 指定配置文件
./bin/api -config configs/api.yaml
```

### 5. Docker 方式启动

```bash
# 构建镜像
make docker-build

# 启动全部（基础设施 + 应用）
CHAIN_TYPE=bsc docker compose -f docker/docker-compose.yml --profile app up -d
```

## 常用命令

```bash
make build-all       # 编译 parser + api
make test            # 运行测试
make test-race       # 带竞态检测
make test-cover      # 测试覆盖率
make vet             # 静态检查
make fmt             # 格式化代码
make docker-up       # 启动 Docker 基础设施
make docker-down     # 停止 Docker
make docker-logs     # 查看日志
make clean           # 清理构建产物
```

## 配置说明

配置采用分层合并策略：`base.yaml` → 链配置（如 `bsc.yaml`）→ 环境变量覆盖。

Parser 启动时根据 `-chain` 或 `CHAIN_TYPE` 自动加载对应链配置并与 base.yaml 合并。

### 存储引擎

在 `base.yaml` 中通过 `storage.type` 切换：

| 引擎 | 值 | 说明 |
|------|-----|------|
| PostgreSQL | `pgsql` | 默认，推荐 |
| MySQL | `mysql` | 可选 |
| InfluxDB | `influxdb` | 时序存储 |

### 环境变量

关键环境变量见 `docker/.env.example`。

## 多链同时运行

每条链需要独立的 parser 进程：

```bash
make run-parser CHAIN=bsc &
make run-parser CHAIN=ethereum &
make run-parser CHAIN=solana &
```

API 服务独立于 parser，一个 API 实例可查询所有链的数据。
