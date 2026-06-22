package service

import (
	"context"
	"errors"

	"unified-tx-parser/internal/types"
)

var ErrTrackerUnavailable = errors.New("progress tracker not configured")

type StatsService struct {
	storage types.StorageEngine
	tracker types.ProgressTracker
}

func NewStatsService(storage types.StorageEngine, tracker types.ProgressTracker) *StatsService {
	return &StatsService{storage: storage, tracker: tracker}
}

func (s *StatsService) StorageStats(ctx context.Context) (map[string]interface{}, error) {
	return s.storage.GetStorageStats(ctx)
}

func (s *StatsService) Progress() (interface{}, error) {
	if s.tracker == nil {
		return nil, ErrTrackerUnavailable
	}
	return s.tracker.GetAllProgress()
}

func (s *StatsService) GlobalStats() (interface{}, error) {
	if s.tracker == nil {
		return nil, ErrTrackerUnavailable
	}
	return s.tracker.GetGlobalStats()
}
