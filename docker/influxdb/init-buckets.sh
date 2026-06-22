#!/bin/bash

# InfluxDB多链bucket初始化脚本
# 该脚本在InfluxDB容器启动后创建各个链专用的bucket

set -e

echo "🚀 开始初始化InfluxDB多链bucket..."

# 等待InfluxDB服务完全启动
echo "⏳ 等待InfluxDB服务启动..."
sleep 10

# InfluxDB配置
INFLUX_URL="http://localhost:8086"
INFLUX_TOKEN="${DOCKER_INFLUXDB_INIT_ADMIN_TOKEN}"
INFLUX_ORG="${DOCKER_INFLUXDB_INIT_ORG}"

# 检查InfluxDB是否可用
echo "🔍 检查InfluxDB连接..."
until influx ping --host $INFLUX_URL; do
  echo "等待InfluxDB启动..."
  sleep 5
done

echo "✅ InfluxDB已启动，开始创建bucket..."

# 创建各个链的专用bucket
BUCKETS=("sui" "ethereum" "bsc" "solana")
RETENTION="90d"

for bucket in "${BUCKETS[@]}"; do
  echo "📦 创建bucket: $bucket"
  
  # 检查bucket是否已存在
  if influx bucket list --host $INFLUX_URL --token $INFLUX_TOKEN --org $INFLUX_ORG --name $bucket > /dev/null 2>&1; then
    echo "⚠️  Bucket '$bucket' 已存在，跳过创建"
  else
    # 创建bucket
    influx bucket create \
      --host $INFLUX_URL \
      --token $INFLUX_TOKEN \
      --org $INFLUX_ORG \
      --name $bucket \
      --retention $RETENTION
    
    if [ $? -eq 0 ]; then
      echo "✅ Bucket '$bucket' 创建成功"
    else
      echo "❌ Bucket '$bucket' 创建失败"
    fi
  fi
done

echo "🎉 InfluxDB多链bucket初始化完成！"
echo "📋 已创建的bucket列表："
influx bucket list --host $INFLUX_URL --token $INFLUX_TOKEN --org $INFLUX_ORG
