package router

import (
	"unified-tx-parser/internal/api/controller"
	"unified-tx-parser/internal/api/middleware"
	"unified-tx-parser/internal/api/service"
	"unified-tx-parser/internal/config"
	"unified-tx-parser/internal/types"

	"github.com/gin-gonic/gin"
)

// New creates a configured Gin engine with all routes and middleware.
func New(cfg *config.Config, storage types.StorageEngine, tracker types.ProgressTracker) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()

	r.Use(
		middleware.RequestID(),
		middleware.Logger(),
		middleware.Recovery(),
		middleware.CORS(cfg.API.AllowOrigins),
	)

	registerRoutes(r, storage, tracker)
	return r
}

func registerRoutes(r *gin.Engine, storage types.StorageEngine, tracker types.ProgressTracker) {
	healthSvc := service.NewHealthService(storage, tracker)
	txSvc := service.NewTransactionService(storage)
	statsSvc := service.NewStatsService(storage, tracker)

	healthCtrl := controller.NewHealthController(healthSvc)
	txCtrl := controller.NewTransactionController(txSvc)
	statsCtrl := controller.NewStatsController(statsSvc)

	r.GET("/health", healthCtrl.Health)

	v1 := r.Group("/api/v1")
	{
		v1.GET("/transactions/:hash", txCtrl.GetByHash)
		v1.GET("/storage/stats", statsCtrl.StorageStats)
		v1.GET("/progress", statsCtrl.Progress)
		v1.GET("/progress/stats", statsCtrl.GlobalStats)
	}
}
