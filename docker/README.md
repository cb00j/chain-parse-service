# Docker 基础设施

本目录包含项目依赖的所有基础设施服务的 Docker Compose 配置。

## 服务列表

| 服务 | 镜像 | 端口 | 用途 |
|------|------|------|------|
| PostgreSQL | postgres:16-alpine | 5432 | 默认存储引擎，存储交易和事件数据 |
| MySQL | mysql:8.0 | 3306 | 可选存储引擎 |
| Redis | redis:7-alpine | 6379 | 进度跟踪、缓存 |
| InfluxDB | influxdb:2.7 | 8086 | 时序存储引擎，存储区块和 DEX 数据 |

## 快速启动

```bash
cd docker

# 启动推荐组合 (pgsql + redis)
docker-compose up -d postgres redis

# 启动全部服务
docker-compose up -d
```

## 常用命令

```bash
# 查看运行状态
docker-compose ps

# 查看日志 (-f 跟踪实时输出)
docker-compose logs -f postgres
docker-compose logs -f redis

# 停止服务
docker-compose down

# 停止并删除所有数据卷 (清空数据重来)
docker-compose down -v

# 重建单个服务
docker-compose up -d --force-recreate postgres
```

## 连接信息

### PostgreSQL

```
Host:     localhost:5432
User:     postgres
Password: password
Database: unified_tx_parser
DSN:      postgres://postgres:password@localhost:5432/unified_tx_parser?sslmode=disable
```

### MySQL

```
Host:     localhost:3306
User:     root / parser_user
Password: password / parser_pass
Database: unified_tx_parser
```

### Redis

```
Host:     localhost:6379
Password: (无)
```

### InfluxDB

```
URL:   http://localhost:8086
User:  admin
Pass:  admin123456
Org:   unified-tx-parser
Token: unified-tx-parser-token-2024
```

## 目录结构

```
docker/
├── docker-compose.yml     # 服务编排
├── Dockerfile             # 应用镜像构建
├── pgsql/
│   └── init.sql           # PostgreSQL 建表脚本 (首次启动自动执行)
├── mysql/
│   └── init.sql           # MySQL 建表脚本 (首次启动自动执行)
├── redis/
│   └── redis.conf         # Redis 配置文件
└── influxdb/
    └── init-buckets.sh    # InfluxDB bucket 初始化脚本
```

## 环境变量

可通过环境变量或 `.env` 文件覆盖默认密码：

```bash
POSTGRES_USER=postgres
POSTGRES_PASSWORD=password
MYSQL_ROOT_PASSWORD=password
MYSQL_PASSWORD=parser_pass
```

## 注意事项

- 初始化脚本 (`init.sql` / `init-buckets.sh`) 仅在**首次启动**（数据卷为空）时执行。若需重新初始化，先执行 `docker-compose down -v` 清除数据卷。
- 应用代码中的 `initTables()` 会自动执行 `CREATE TABLE IF NOT EXISTS`，即使不依赖 Docker 初始化脚本也能正常建表。
- 生产环境请务必修改默认密码。
