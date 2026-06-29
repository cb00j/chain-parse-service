package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"time"

	"unified-tx-parser/internal/types"

	"github.com/redis/go-redis/v9"
)

// RedisProgressTracker Redis版本的进度跟踪器
type RedisProgressTracker struct {
	client            *redis.Client
	keyPrefix         string
	maxErrorHistory   int
	maxMetricsHistory int
}

// NewRedisProgressTracker 创建Redis进度跟踪器
func NewRedisProgressTracker(client *redis.Client, keyPrefix string) *RedisProgressTracker {
	if keyPrefix == "" {
		keyPrefix = "unified_tx_parser"
	}

	return &RedisProgressTracker{
		client:            client,
		keyPrefix:         keyPrefix,
		maxErrorHistory:   1000,
		maxMetricsHistory: 10000,
	}
}

// 生成Redis键名
func (r *RedisProgressTracker) getProgressKey(chainType types.ChainType) string {
	return fmt.Sprintf("%s:progress:%s", r.keyPrefix, chainType)
}

func (r *RedisProgressTracker) getStatsKey(chainType types.ChainType) string {
	return fmt.Sprintf("%s:stats:%s", r.keyPrefix, chainType)
}

func (r *RedisProgressTracker) getErrorsKey(chainType types.ChainType) string {
	return fmt.Sprintf("%s:errors:%s", r.keyPrefix, chainType)
}

func (r *RedisProgressTracker) getMetricsKey(chainType types.ChainType) string {
	return fmt.Sprintf("%s:metrics:%s", r.keyPrefix, chainType)
}

func (r *RedisProgressTracker) getGlobalStatsKey() string {
	return fmt.Sprintf("%s:global_stats", r.keyPrefix)
}

// GetProgress 获取处理进度
func (r *RedisProgressTracker) GetProgress(chainType types.ChainType) (*types.ProcessProgress, error) {
	ctx := context.Background()
	key := r.getProgressKey(chainType)

	data, err := r.client.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			// 返回默认进度
			return &types.ProcessProgress{
				ChainType:          chainType,
				LastProcessedBlock: big.NewInt(0),
				LastUpdateTime:     time.Now(),
				ProcessingStatus:   types.ProcessingStatusIdle,
				StartTime:          time.Now(),
				SuccessRate:        100.0,
			}, nil
		}
		return nil, fmt.Errorf("获取进度失败: %w", err)
	}

	var progress types.ProcessProgress
	if err := json.Unmarshal([]byte(data), &progress); err != nil {
		return nil, fmt.Errorf("反序列化进度失败: %w", err)
	}

	return &progress, nil
}

// UpdateProgress 更新进度
func (r *RedisProgressTracker) UpdateProgress(chainType types.ChainType, progress *types.ProcessProgress) error {
	if progress == nil {
		return fmt.Errorf("进度信息不能为空")
	}

	ctx := context.Background()
	key := r.getProgressKey(chainType)

	// 更新进度信息
	progress.LastUpdateTime = time.Now()
	progress.ChainType = chainType

	// 计算成功率
	if progress.TotalTransactions > 0 {
		successfulTx := progress.TotalTransactions - progress.ErrorCount
		progress.SuccessRate = float64(successfulTx) / float64(progress.TotalTransactions) * 100
	}

	// 序列化并存储
	data, err := json.Marshal(progress)
	if err != nil {
		return fmt.Errorf("序列化进度失败: %w", err)
	}

	if err := r.client.Set(ctx, key, data, 0).Err(); err != nil {
		return fmt.Errorf("存储进度失败: %w", err)
	}

	return nil
}

// ResetProgress 重置进度
func (r *RedisProgressTracker) ResetProgress(chainType types.ChainType) error {
	ctx := context.Background()

	// 删除相关的所有键
	keys := []string{
		r.getProgressKey(chainType),
		r.getStatsKey(chainType),
		r.getErrorsKey(chainType),
		r.getMetricsKey(chainType),
	}

	for _, key := range keys {
		if err := r.client.Del(ctx, key).Err(); err != nil {
			return fmt.Errorf("删除键 %s 失败: %w", key, err)
		}
	}

	// 创建新的默认进度
	defaultProgress := &types.ProcessProgress{
		ChainType:          chainType,
		LastProcessedBlock: big.NewInt(0),
		LastUpdateTime:     time.Now(),
		ProcessingStatus:   types.ProcessingStatusIdle,
		StartTime:          time.Now(),
		TotalTransactions:  0,
		TotalEvents:        0,
		ErrorCount:         0,
		SuccessRate:        100.0,
	}

	return r.UpdateProgress(chainType, defaultProgress)
}

// GetAllProgress 获取所有进度
func (r *RedisProgressTracker) GetAllProgress() (map[types.ChainType]*types.ProcessProgress, error) {
	ctx := context.Background()
	pattern := fmt.Sprintf("%s:progress:*", r.keyPrefix)

	keys, err := r.client.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("获取进度键失败: %w", err)
	}

	result := make(map[types.ChainType]*types.ProcessProgress)

	for _, key := range keys {
		data, err := r.client.Get(ctx, key).Result()
		if err != nil {
			continue // 跳过错误的键
		}

		var progress types.ProcessProgress
		if err := json.Unmarshal([]byte(data), &progress); err != nil {
			continue // 跳过反序列化失败的数据
		}

		result[progress.ChainType] = &progress
	}

	return result, nil
}

// UpdateMultipleProgress 批量更新进度
func (r *RedisProgressTracker) UpdateMultipleProgress(progresses map[types.ChainType]*types.ProcessProgress) error {
	ctx := context.Background()
	pipe := r.client.Pipeline()

	for chainType, progress := range progresses {
		if progress == nil {
			continue
		}

		progress.LastUpdateTime = time.Now()
		progress.ChainType = chainType

		// 计算成功率
		if progress.TotalTransactions > 0 {
			successfulTx := progress.TotalTransactions - progress.ErrorCount
			progress.SuccessRate = float64(successfulTx) / float64(progress.TotalTransactions) * 100
		}

		// 序列化
		data, err := json.Marshal(progress)
		if err != nil {
			return fmt.Errorf("序列化进度失败 (链: %s): %w", chainType, err)
		}

		key := r.getProgressKey(chainType)
		pipe.Set(ctx, key, data, 0)
	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("批量更新进度失败: %w", err)
	}

	return nil
}

// GetProcessingStats 获取处理统计
func (r *RedisProgressTracker) GetProcessingStats(chainType types.ChainType) (*types.ProcessingStats, error) {
	ctx := context.Background()
	key := r.getStatsKey(chainType)

	data, err := r.client.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return &types.ProcessingStats{
				ChainType: chainType,
			}, nil
		}
		return nil, fmt.Errorf("获取统计失败: %w", err)
	}

	var stats types.ProcessingStats
	if err := json.Unmarshal([]byte(data), &stats); err != nil {
		return nil, fmt.Errorf("反序列化统计失败: %w", err)
	}

	return &stats, nil
}

// GetGlobalStats 获取全局统计
func (r *RedisProgressTracker) GetGlobalStats() (*types.GlobalProcessingStats, error) {
	// 获取所有进度
	allProgress, err := r.GetAllProgress()
	if err != nil {
		return nil, fmt.Errorf("获取所有进度失败: %w", err)
	}

	// 计算全局统计
	globalStats := &types.GlobalProcessingStats{
		TotalChains:    len(allProgress),
		ChainStats:     make(map[types.ChainType]*types.ProcessingStats),
		LastUpdateTime: time.Now(),
	}

	var totalSuccessRate float64
	activeChains := 0

	for chainType, progress := range allProgress {
		globalStats.TotalTransactions += progress.TotalTransactions
		globalStats.TotalEvents += progress.TotalEvents

		if progress.ProcessingStatus == types.ProcessingStatusRunning ||
			progress.ProcessingStatus == types.ProcessingStatusSyncing ||
			progress.ProcessingStatus == types.ProcessingStatusCatchingUp {
			globalStats.ActiveChains++
		}

		if progress.TotalTransactions > 0 {
			totalSuccessRate += progress.SuccessRate
			activeChains++
		}

		// 获取链统计
		if stats, err := r.GetProcessingStats(chainType); err == nil {
			globalStats.ChainStats[chainType] = stats
		}
	}

	// 计算整体成功率
	if activeChains > 0 {
		globalStats.OverallSuccessRate = totalSuccessRate / float64(activeChains)
	}

	return globalStats, nil
}

// SetProcessingStatus 设置处理状态
func (r *RedisProgressTracker) SetProcessingStatus(chainType types.ChainType, status types.ProcessingStatus) error {
	// 获取当前进度
	progress, err := r.GetProgress(chainType)
	if err != nil {
		return fmt.Errorf("获取当前进度失败: %w", err)
	}

	// 更新状态
	progress.ProcessingStatus = status
	progress.LastUpdateTime = time.Now()

	// 保存更新后的进度
	return r.UpdateProgress(chainType, progress)
}

// GetProcessingStatus 获取处理状态
func (r *RedisProgressTracker) GetProcessingStatus(chainType types.ChainType) (types.ProcessingStatus, error) {
	progress, err := r.GetProgress(chainType)
	if err != nil {
		return types.ProcessingStatusIdle, fmt.Errorf("获取进度失败: %w", err)
	}

	return progress.ProcessingStatus, nil
}

// RecordError 记录错误
func (r *RedisProgressTracker) RecordError(chainType types.ChainType, err error) error {
	if err == nil {
		return nil
	}

	ctx := context.Background()
	key := r.getErrorsKey(chainType)

	// 创建错误记录
	processingError := types.ProcessingError{
		ChainType: chainType,
		ErrorTime: time.Now(),
		ErrorType: fmt.Sprintf("%T", err),
		ErrorMsg:  err.Error(),
		Resolved:  false,
	}

	// 序列化错误
	data, marshalErr := json.Marshal(processingError)
	if marshalErr != nil {
		return fmt.Errorf("序列化错误失败: %w", marshalErr)
	}

	// 添加到错误列表（使用列表结构）
	if err := r.client.LPush(ctx, key, data).Err(); err != nil {
		return fmt.Errorf("记录错误失败: %w", err)
	}

	// 限制错误历史长度
	if err := r.client.LTrim(ctx, key, 0, int64(r.maxErrorHistory-1)).Err(); err != nil {
		return fmt.Errorf("修剪错误历史失败: %w", err)
	}

	// 更新进度中的错误计数
	progress, err := r.GetProgress(chainType)
	if err == nil {
		progress.ErrorCount++
		progress.LastErrorTime = time.Now()

		// 重新计算成功率
		if progress.TotalTransactions > 0 {
			successfulTx := progress.TotalTransactions - progress.ErrorCount
			progress.SuccessRate = float64(successfulTx) / float64(progress.TotalTransactions) * 100
		}

		r.UpdateProgress(chainType, progress)
	}

	return nil
}

// GetErrorHistory 获取错误历史
func (r *RedisProgressTracker) GetErrorHistory(chainType types.ChainType, limit int) ([]types.ProcessingError, error) {
	ctx := context.Background()
	key := r.getErrorsKey(chainType)

	// 获取错误列表
	var data []string
	var err error

	if limit > 0 {
		data, err = r.client.LRange(ctx, key, 0, int64(limit-1)).Result()
	} else {
		data, err = r.client.LRange(ctx, key, 0, -1).Result()
	}

	if err != nil {
		if err == redis.Nil {
			return []types.ProcessingError{}, nil
		}
		return nil, fmt.Errorf("获取错误历史失败: %w", err)
	}

	// 反序列化错误
	var errors []types.ProcessingError
	for _, item := range data {
		var processingError types.ProcessingError
		if err := json.Unmarshal([]byte(item), &processingError); err != nil {
			continue // 跳过反序列化失败的项
		}
		errors = append(errors, processingError)
	}

	return errors, nil
}

// ClearErrorHistory 清除错误历史
func (r *RedisProgressTracker) ClearErrorHistory(chainType types.ChainType) error {
	ctx := context.Background()
	key := r.getErrorsKey(chainType)

	// 删除错误历史
	if err := r.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("清除错误历史失败: %w", err)
	}

	// 重置进度中的错误计数
	progress, err := r.GetProgress(chainType)
	if err == nil {
		progress.ErrorCount = 0
		progress.LastErrorTime = time.Time{}

		// 重新计算成功率
		if progress.TotalTransactions > 0 {
			progress.SuccessRate = 100.0
		}

		r.UpdateProgress(chainType, progress)
	}

	return nil
}

// RecordProcessingMetrics 记录处理指标
func (r *RedisProgressTracker) RecordProcessingMetrics(chainType types.ChainType, metrics *types.ProcessingMetrics) error {
	if metrics == nil {
		return fmt.Errorf("指标信息不能为空")
	}

	ctx := context.Background()
	key := r.getMetricsKey(chainType)

	// 设置时间戳和链类型
	metrics.Timestamp = time.Now()
	metrics.ChainType = chainType

	// 序列化指标
	data, err := json.Marshal(metrics)
	if err != nil {
		return fmt.Errorf("序列化指标失败: %w", err)
	}

	// 添加到指标列表
	if err := r.client.LPush(ctx, key, data).Err(); err != nil {
		return fmt.Errorf("记录指标失败: %w", err)
	}

	// 限制指标历史长度
	if err := r.client.LTrim(ctx, key, 0, int64(r.maxMetricsHistory-1)).Err(); err != nil {
		return fmt.Errorf("修剪指标历史失败: %w", err)
	}

	return nil
}

// GetPerformanceMetrics 获取性能指标
func (r *RedisProgressTracker) GetPerformanceMetrics(chainType types.ChainType, duration time.Duration) (*types.PerformanceReport, error) {
	ctx := context.Background()
	key := r.getMetricsKey(chainType)

	// 获取所有指标
	data, err := r.client.LRange(ctx, key, 0, -1).Result()
	if err != nil {
		if err == redis.Nil {
			return &types.PerformanceReport{
				ChainType:    chainType,
				ReportPeriod: duration,
			}, nil
		}
		return nil, fmt.Errorf("获取指标失败: %w", err)
	}

	// 过滤指定时间范围内的指标
	cutoffTime := time.Now().Add(-duration)
	var filteredMetrics []types.ProcessingMetrics

	for _, item := range data {
		var metric types.ProcessingMetrics
		if err := json.Unmarshal([]byte(item), &metric); err != nil {
			continue // 跳过反序列化失败的项
		}

		if metric.Timestamp.After(cutoffTime) {
			filteredMetrics = append(filteredMetrics, metric)
		}
	}

	if len(filteredMetrics) == 0 {
		return &types.PerformanceReport{
			ChainType:    chainType,
			ReportPeriod: duration,
		}, nil
	}

	// 计算性能报告
	report := &types.PerformanceReport{
		ChainType:    chainType,
		ReportPeriod: duration,
	}

	var totalProcessTime time.Duration
	var totalTransactions, totalEvents int64
	var totalMemory int64
	var totalCPU float64
	var maxProcessTime, minProcessTime time.Duration

	for i, metric := range filteredMetrics {
		totalProcessTime += metric.ProcessingTime
		totalTransactions += int64(metric.TransactionCount)
		totalEvents += int64(metric.EventCount)
		totalMemory += metric.MemoryUsage
		totalCPU += metric.CPUUsage

		if i == 0 {
			maxProcessTime = metric.ProcessingTime
			minProcessTime = metric.ProcessingTime
		} else {
			if metric.ProcessingTime > maxProcessTime {
				maxProcessTime = metric.ProcessingTime
			}
			if metric.ProcessingTime < minProcessTime {
				minProcessTime = metric.ProcessingTime
			}
		}
	}

	metricsCount := int64(len(filteredMetrics))
	if metricsCount > 0 {
		report.AverageProcessTime = totalProcessTime / time.Duration(metricsCount)
		report.MaxProcessTime = maxProcessTime
		report.MinProcessTime = minProcessTime
		report.TotalTransactions = totalTransactions
		report.TotalEvents = totalEvents
		report.AverageMemoryUsage = totalMemory / metricsCount
		report.AverageCPUUsage = totalCPU / float64(metricsCount)

		// 计算吞吐量
		if duration > 0 {
			report.ThroughputTPS = float64(totalTransactions) / duration.Seconds()
		}
	}

	// 获取错误数量
	errors, err := r.GetErrorHistory(chainType, 0)
	if err == nil {
		for _, processingError := range errors {
			if processingError.ErrorTime.After(cutoffTime) {
				report.ErrorCount++
			}
		}
	}

	return report, nil
}

// HealthCheck 健康检查
func (r *RedisProgressTracker) HealthCheck() error {
	ctx := context.Background()

	// 检查Redis连接
	if err := r.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("Redis连接失败: %w", err)
	}

	// 检查基本操作
	testKey := fmt.Sprintf("%s:health_check", r.keyPrefix)
	testValue := strconv.FormatInt(time.Now().Unix(), 10)

	if err := r.client.Set(ctx, testKey, testValue, time.Minute).Err(); err != nil {
		return fmt.Errorf("Redis写入测试失败: %w", err)
	}

	if err := r.client.Del(ctx, testKey).Err(); err != nil {
		return fmt.Errorf("Redis删除测试失败: %w", err)
	}

	return nil
}

// Cleanup 清理旧数据
func (r *RedisProgressTracker) Cleanup(olderThan time.Duration) error {
	ctx := context.Background()
	cutoffTime := time.Now().Add(-olderThan)

	// 获取所有链类型
	allProgress, err := r.GetAllProgress()
	if err != nil {
		return fmt.Errorf("获取所有进度失败: %w", err)
	}

	// 清理每个链的旧数据
	for chainType := range allProgress {
		// 清理旧的指标数据
		metricsKey := r.getMetricsKey(chainType)
		if err := r.cleanupListData(ctx, metricsKey, cutoffTime, "metrics"); err != nil {
			return fmt.Errorf("清理指标数据失败 (链: %s): %w", chainType, err)
		}

		// 清理旧的错误数据
		errorsKey := r.getErrorsKey(chainType)
		if err := r.cleanupListData(ctx, errorsKey, cutoffTime, "errors"); err != nil {
			return fmt.Errorf("清理错误数据失败 (链: %s): %w", chainType, err)
		}
	}

	return nil
}

// cleanupListData 清理列表数据的辅助方法
func (r *RedisProgressTracker) cleanupListData(ctx context.Context, key string, cutoffTime time.Time, dataType string) error {
	// 获取所有数据
	data, err := r.client.LRange(ctx, key, 0, -1).Result()
	if err != nil {
		if err == redis.Nil {
			return nil // 没有数据，无需清理
		}
		return fmt.Errorf("获取%s数据失败: %w", dataType, err)
	}

	// 过滤需要保留的数据
	var validData []string
	for _, item := range data {
		var timestamp time.Time

		if dataType == "metrics" {
			var metric types.ProcessingMetrics
			if err := json.Unmarshal([]byte(item), &metric); err != nil {
				continue // 跳过无效数据
			}
			timestamp = metric.Timestamp
		} else if dataType == "errors" {
			var processingError types.ProcessingError
			if err := json.Unmarshal([]byte(item), &processingError); err != nil {
				continue // 跳过无效数据
			}
			timestamp = processingError.ErrorTime
		}

		// 保留新数据
		if timestamp.After(cutoffTime) {
			validData = append(validData, item)
		}
	}

	// 如果没有变化，无需更新
	if len(validData) == len(data) {
		return nil
	}

	// 删除旧列表
	if err := r.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("删除旧%s列表失败: %w", dataType, err)
	}

	// 重新创建列表（如果有有效数据）
	if len(validData) > 0 {
		// 反转数据以保持正确的顺序（因为LPUSH是从左边插入）
		for i := len(validData) - 1; i >= 0; i-- {
			if err := r.client.LPush(ctx, key, validData[i]).Err(); err != nil {
				return fmt.Errorf("重建%s列表失败: %w", dataType, err)
			}
		}
	}

	return nil
}
