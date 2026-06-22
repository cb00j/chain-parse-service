package service

import (
	"context"

	"unified-tx-parser/internal/types"
)

type HealthService struct {
	storage types.StorageEngine
	tracker types.ProgressTracker
}

func NewHealthService(storage types.StorageEngine, tracker types.ProgressTracker) *HealthService {
	return &HealthService{storage: storage, tracker: tracker}
}

// HealthStatus contains the results of dependency health checks.
type HealthStatus struct {
	StorageOK       bool   `json:"storage_ok"`
	StorageError    string `json:"storage_error,omitempty"`
	TrackerOK       bool   `json:"tracker_ok"`
	TrackerError    string `json:"tracker_error,omitempty"`
	TrackerEnabled  bool   `json:"tracker_enabled"`
}

func (s *HealthService) Check(ctx context.Context) *HealthStatus {
	status := &HealthStatus{}

	if err := s.storage.HealthCheck(ctx); err != nil {
		status.StorageError = err.Error()
	} else {
		status.StorageOK = true
	}

	if s.tracker != nil {
		status.TrackerEnabled = true
		if err := s.tracker.HealthCheck(); err != nil {
			status.TrackerError = err.Error()
		} else {
			status.TrackerOK = true
		}
	}

	return status
}
