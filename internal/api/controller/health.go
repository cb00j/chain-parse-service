package controller

import (
	"net/http"
	"time"

	"unified-tx-parser/internal/api/service"

	"github.com/gin-gonic/gin"
)

type HealthController struct {
	svc *service.HealthService
}

func NewHealthController(svc *service.HealthService) *HealthController {
	return &HealthController{svc: svc}
}

func (ctrl *HealthController) Health(c *gin.Context) {
	status := ctrl.svc.Check(c.Request.Context())

	resp := gin.H{
		"status":    "ok",
		"timestamp": time.Now(),
		"storage":   gin.H{"status": boolStatus(status.StorageOK)},
	}
	if status.StorageError != "" {
		resp["storage"] = gin.H{"status": "error", "error": status.StorageError}
	}
	if status.TrackerEnabled {
		tracker := gin.H{"status": boolStatus(status.TrackerOK)}
		if status.TrackerError != "" {
			tracker["status"] = "error"
			tracker["error"] = status.TrackerError
		}
		resp["progress_tracker"] = tracker
	}

	c.JSON(http.StatusOK, resp)
}

func boolStatus(ok bool) string {
	if ok {
		return "ok"
	}
	return "error"
}
