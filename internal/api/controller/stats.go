package controller

import (
	"errors"

	"unified-tx-parser/internal/api/service"

	"github.com/gin-gonic/gin"
)

type StatsController struct {
	svc *service.StatsService
}

func NewStatsController(svc *service.StatsService) *StatsController {
	return &StatsController{svc: svc}
}

func (ctrl *StatsController) StorageStats(c *gin.Context) {
	stats, err := ctrl.svc.StorageStats(c.Request.Context())
	if err != nil {
		internalError(c, err.Error())
		return
	}
	success(c, stats)
}

func (ctrl *StatsController) Progress(c *gin.Context) {
	data, err := ctrl.svc.Progress()
	if err != nil {
		if errors.Is(err, service.ErrTrackerUnavailable) {
			serviceUnavailable(c, err.Error())
			return
		}
		internalError(c, err.Error())
		return
	}
	success(c, gin.H{"progress": data})
}

func (ctrl *StatsController) GlobalStats(c *gin.Context) {
	data, err := ctrl.svc.GlobalStats()
	if err != nil {
		if errors.Is(err, service.ErrTrackerUnavailable) {
			serviceUnavailable(c, err.Error())
			return
		}
		internalError(c, err.Error())
		return
	}
	success(c, data)
}
