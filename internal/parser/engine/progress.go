package engine

import (
	"fmt"
	"math/big"
	"sync"
	"time"

	"unified-tx-parser/internal/types"
)

// MemoryProgressTracker 内存版本的进度跟踪器
type MemoryProgressTracker struct {
	mu                sync.RWMutex
	progresses        map[types.ChainType]*types.ProcessProgress
	stats             map[types.ChainType]*types.ProcessingStats
	errors            map[types.ChainType][]types.ProcessingError
	metrics           map[types.ChainType][]types.ProcessingMetrics
	globalStats       *types.GlobalProcessingStats
	systemStartTime   time.Time
	maxErrorHistory   int
	maxMetricsHistory int
}

// NewMemoryProgressTracker 创建内存进度跟踪器
func NewMemoryProgressTracker() *MemoryProgressTracker {
	return &MemoryProgressTracker{
		progresses:        make(map[types.ChainType]*types.ProcessProgress),
		stats:             make(map[types.ChainType]*types.ProcessingStats),
		errors:            make(map[types.ChainType][]types.ProcessingError),
		metrics:           make(map[types.ChainType][]types.ProcessingMetrics),
		systemStartTime:   time.Now(),
		maxErrorHistory:   1000,
		maxMetricsHistory: 10000,
		globalStats: &types.GlobalProcessingStats{
			ChainStats:     make(map[types.ChainType]*types.ProcessingStats),
			LastUpdateTime: time.Now(),
		},
	}
}

// GetProgress 获取处理进度
func (m *MemoryProgressTracker) GetProgress(chainType types.ChainType) (*types.ProcessProgress, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	progress, exists := m.progresses[chainType]
	if !exists {
		// 返回默认进度
		return &types.ProcessProgress{
			ChainType:          chainType,
			LastProcessedBlock: big.NewInt(0),
			LastUpdateTime:     time.Now(),
			ProcessingStatus:   types.ProcessingStatusIdle,
			StartTime:          time.Now(),
		}, nil
	}

	// 返回副本避免并发修改
	progressCopy := *progress
	return &progressCopy, nil
}

// UpdateProgress 更新进度
func (m *MemoryProgressTracker) UpdateProgress(chainType types.ChainType, progress *types.ProcessProgress) error {
	if progress == nil {
		return fmt.Errorf("进度信息不能为空")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// 更新进度
	progress.LastUpdateTime = time.Now()
	progress.ChainType = chainType

	// 计算成功率
	if progress.TotalTransactions > 0 {
		successfulTx := progress.TotalTransactions - progress.ErrorCount
		progress.SuccessRate = float64(successfulTx) / float64(progress.TotalTransactions) * 100
	}

	m.progresses[chainType] = progress

	// 更新全局统计
	m.updateGlobalStats()

	return nil
}

// ResetProgress 重置进度
func (m *MemoryProgressTracker) ResetProgress(chainType types.ChainType) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 重置进度
	m.progresses[chainType] = &types.ProcessProgress{
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

	// 清除相关数据
	delete(m.stats, chainType)
	delete(m.errors, chainType)
	delete(m.metrics, chainType)

	// 更新全局统计
	m.updateGlobalStats()

	return nil
}

// GetAllProgress 获取所有进度
func (m *MemoryProgressTracker) GetAllProgress() (map[types.ChainType]*types.ProcessProgress, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[types.ChainType]*types.ProcessProgress)
	for chainType, progress := range m.progresses {
		progressCopy := *progress
		result[chainType] = &progressCopy
	}

	return result, nil
}

// UpdateMultipleProgress 批量更新进度
func (m *MemoryProgressTracker) UpdateMultipleProgress(progresses map[types.ChainType]*types.ProcessProgress) error {
	m.mu.Lock()
	defer m.mu.Unlock()

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

		m.progresses[chainType] = progress
	}

	// 更新全局统计
	m.updateGlobalStats()

	return nil
}

// updateGlobalStats 更新全局统计（内部方法，需要持有锁）
func (m *MemoryProgressTracker) updateGlobalStats() {
	m.globalStats.TotalChains = len(m.progresses)
	m.globalStats.ActiveChains = 0
	m.globalStats.TotalTransactions = 0
	m.globalStats.TotalEvents = 0
	m.globalStats.LastUpdateTime = time.Now()
	m.globalStats.SystemUptime = time.Since(m.systemStartTime)

	var totalSuccessRate float64
	activeChains := 0

	for chainType, progress := range m.progresses {
		m.globalStats.TotalTransactions += progress.TotalTransactions
		m.globalStats.TotalEvents += progress.TotalEvents

		if progress.ProcessingStatus == types.ProcessingStatusRunning ||
			progress.ProcessingStatus == types.ProcessingStatusSyncing ||
			progress.ProcessingStatus == types.ProcessingStatusCatchingUp {
			m.globalStats.ActiveChains++
		}

		if progress.TotalTransactions > 0 {
			totalSuccessRate += progress.SuccessRate
			activeChains++
		}

		// 更新链统计
		if stats, exists := m.stats[chainType]; exists {
			m.globalStats.ChainStats[chainType] = stats
		}
	}

	// 计算整体成功率
	if activeChains > 0 {
		m.globalStats.OverallSuccessRate = totalSuccessRate / float64(activeChains)
	}
}

// GetProcessingStats 获取处理统计
func (m *MemoryProgressTracker) GetProcessingStats(chainType types.ChainType) (*types.ProcessingStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats, exists := m.stats[chainType]
	if !exists {
		return &types.ProcessingStats{
			ChainType: chainType,
		}, nil
	}

	// 返回副本
	statsCopy := *stats
	return &statsCopy, nil
}

// GetGlobalStats 获取全局统计
func (m *MemoryProgressTracker) GetGlobalStats() (*types.GlobalProcessingStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 返回副本
	globalStatsCopy := *m.globalStats
	globalStatsCopy.ChainStats = make(map[types.ChainType]*types.ProcessingStats)
	for chainType, stats := range m.globalStats.ChainStats {
		statsCopy := *stats
		globalStatsCopy.ChainStats[chainType] = &statsCopy
	}

	return &globalStatsCopy, nil
}

// SetProcessingStatus 设置处理状态
func (m *MemoryProgressTracker) SetProcessingStatus(chainType types.ChainType, status types.ProcessingStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	progress, exists := m.progresses[chainType]
	if !exists {
		// 创建新的进度记录
		progress = &types.ProcessProgress{
			ChainType:          chainType,
			LastProcessedBlock: big.NewInt(0),
			StartTime:          time.Now(),
		}
		m.progresses[chainType] = progress
	}

	progress.ProcessingStatus = status
	progress.LastUpdateTime = time.Now()

	// 更新全局统计
	m.updateGlobalStats()

	return nil
}

// GetProcessingStatus 获取处理状态
func (m *MemoryProgressTracker) GetProcessingStatus(chainType types.ChainType) (types.ProcessingStatus, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	progress, exists := m.progresses[chainType]
	if !exists {
		return types.ProcessingStatusIdle, nil
	}

	return progress.ProcessingStatus, nil
}

// RecordError 记录错误
func (m *MemoryProgressTracker) RecordError(chainType types.ChainType, err error) error {
	if err == nil {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// 创建错误记录
	processingError := types.ProcessingError{
		ChainType: chainType,
		ErrorTime: time.Now(),
		ErrorType: fmt.Sprintf("%T", err),
		ErrorMsg:  err.Error(),
		Resolved:  false,
	}

	// 添加到错误历史
	if _, exists := m.errors[chainType]; !exists {
		m.errors[chainType] = make([]types.ProcessingError, 0)
	}

	m.errors[chainType] = append(m.errors[chainType], processingError)

	// 限制错误历史长度
	if len(m.errors[chainType]) > m.maxErrorHistory {
		m.errors[chainType] = m.errors[chainType][len(m.errors[chainType])-m.maxErrorHistory:]
	}

	// 更新进度中的错误计数
	if progress, exists := m.progresses[chainType]; exists {
		progress.ErrorCount++
		progress.LastErrorTime = time.Now()

		// 重新计算成功率
		if progress.TotalTransactions > 0 {
			successfulTx := progress.TotalTransactions - progress.ErrorCount
			progress.SuccessRate = float64(successfulTx) / float64(progress.TotalTransactions) * 100
		}
	}

	return nil
}

// GetErrorHistory 获取错误历史
func (m *MemoryProgressTracker) GetErrorHistory(chainType types.ChainType, limit int) ([]types.ProcessingError, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	errors, exists := m.errors[chainType]
	if !exists {
		return []types.ProcessingError{}, nil
	}

	// 应用限制
	if limit > 0 && len(errors) > limit {
		errors = errors[len(errors)-limit:]
	}

	// 返回副本
	result := make([]types.ProcessingError, len(errors))
	copy(result, errors)

	return result, nil
}

// ClearErrorHistory 清除错误历史
func (m *MemoryProgressTracker) ClearErrorHistory(chainType types.ChainType) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.errors, chainType)

	// 重置进度中的错误计数
	if progress, exists := m.progresses[chainType]; exists {
		progress.ErrorCount = 0
		progress.LastErrorTime = time.Time{}

		// 重新计算成功率
		if progress.TotalTransactions > 0 {
			progress.SuccessRate = 100.0
		}
	}

	return nil
}

// RecordProcessingMetrics 记录处理指标
func (m *MemoryProgressTracker) RecordProcessingMetrics(chainType types.ChainType, metrics *types.ProcessingMetrics) error {
	if metrics == nil {
		return fmt.Errorf("指标信息不能为空")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// 设置时间戳和链类型
	metrics.Timestamp = time.Now()
	metrics.ChainType = chainType

	// 添加到指标历史
	if _, exists := m.metrics[chainType]; !exists {
		m.metrics[chainType] = make([]types.ProcessingMetrics, 0)
	}

	m.metrics[chainType] = append(m.metrics[chainType], *metrics)

	// 限制指标历史长度
	if len(m.metrics[chainType]) > m.maxMetricsHistory {
		m.metrics[chainType] = m.metrics[chainType][len(m.metrics[chainType])-m.maxMetricsHistory:]
	}

	// 更新统计信息
	m.updateProcessingStats(chainType)

	return nil
}

// updateProcessingStats 更新处理统计（内部方法）
func (m *MemoryProgressTracker) updateProcessingStats(chainType types.ChainType) {
	metrics, exists := m.metrics[chainType]
	if !exists || len(metrics) == 0 {
		return
	}

	progress, progressExists := m.progresses[chainType]
	if !progressExists {
		return
	}

	// 计算统计信息
	stats := &types.ProcessingStats{
		ChainType: chainType,
	}

	if len(metrics) > 0 {
		// 计算处理持续时间
		if progress.StartTime.IsZero() {
			stats.ProcessingDuration = 0
		} else {
			stats.ProcessingDuration = time.Since(progress.StartTime)
		}

		// 计算平均处理时间
		var totalProcessingTime time.Duration
		var totalTransactions, totalEvents int64
		var totalMemory int64
		var totalCPU float64

		for _, metric := range metrics {
			totalProcessingTime += metric.ProcessingTime
			totalTransactions += int64(metric.TransactionCount)
			totalEvents += int64(metric.EventCount)
			totalMemory += metric.MemoryUsage
			totalCPU += metric.CPUUsage
		}

		metricsCount := int64(len(metrics))
		if metricsCount > 0 {
			stats.LastProcessingTime = totalProcessingTime / time.Duration(metricsCount)
		}

		// 计算TPS和EPS
		if stats.ProcessingDuration > 0 {
			stats.TransactionsPerSec = float64(progress.TotalTransactions) / stats.ProcessingDuration.Seconds()
			stats.EventsPerSec = float64(progress.TotalEvents) / stats.ProcessingDuration.Seconds()
		}

		// 计算错误率
		if progress.TotalTransactions > 0 {
			stats.ErrorRate = float64(progress.ErrorCount) / float64(progress.TotalTransactions) * 100
		}

		// 计算已处理区块数
		if progress.LastProcessedBlock != nil {
			stats.BlocksProcessed = progress.LastProcessedBlock.Int64()
		}
	}

	m.stats[chainType] = stats
}

// GetPerformanceMetrics 获取性能指标
func (m *MemoryProgressTracker) GetPerformanceMetrics(chainType types.ChainType, duration time.Duration) (*types.PerformanceReport, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	metrics, exists := m.metrics[chainType]
	if !exists || len(metrics) == 0 {
		return &types.PerformanceReport{
			ChainType:    chainType,
			ReportPeriod: duration,
		}, nil
	}

	// 过滤指定时间范围内的指标
	cutoffTime := time.Now().Add(-duration)
	var filteredMetrics []types.ProcessingMetrics

	for _, metric := range metrics {
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
	if errors, exists := m.errors[chainType]; exists {
		for _, err := range errors {
			if err.ErrorTime.After(cutoffTime) {
				report.ErrorCount++
			}
		}
	}

	return report, nil
}

// HealthCheck 健康检查
func (m *MemoryProgressTracker) HealthCheck() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 检查基本状态
	if m.progresses == nil {
		return fmt.Errorf("进度跟踪器未初始化")
	}

	// 检查内存使用情况
	totalMetrics := 0
	totalErrors := 0

	for _, metrics := range m.metrics {
		totalMetrics += len(metrics)
	}

	for _, errors := range m.errors {
		totalErrors += len(errors)
	}

	// 如果内存使用过多，发出警告
	if totalMetrics > m.maxMetricsHistory*10 {
		return fmt.Errorf("指标历史过多，可能存在内存泄漏")
	}

	if totalErrors > m.maxErrorHistory*10 {
		return fmt.Errorf("错误历史过多，可能存在内存泄漏")
	}

	return nil
}

// Cleanup 清理旧数据
func (m *MemoryProgressTracker) Cleanup(olderThan time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cutoffTime := time.Now().Add(-olderThan)

	// 清理旧的指标数据
	for chainType, metrics := range m.metrics {
		var filteredMetrics []types.ProcessingMetrics
		for _, metric := range metrics {
			if metric.Timestamp.After(cutoffTime) {
				filteredMetrics = append(filteredMetrics, metric)
			}
		}
		m.metrics[chainType] = filteredMetrics
	}

	// 清理旧的错误数据
	for chainType, errors := range m.errors {
		var filteredErrors []types.ProcessingError
		for _, err := range errors {
			if err.ErrorTime.After(cutoffTime) {
				filteredErrors = append(filteredErrors, err)
			}
		}
		m.errors[chainType] = filteredErrors
	}

	return nil
}
