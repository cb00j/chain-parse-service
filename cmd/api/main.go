package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"unified-tx-parser/internal/api/router"
	"unified-tx-parser/internal/app"
	"unified-tx-parser/internal/config"
	"unified-tx-parser/internal/logger"
)

var (
	version = "dev"
	commit  = "unknown"
)

var log = logger.New("api", "main")

func main() {
	configPath := flag.String("config", "configs/api.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	logger.SetLevel(cfg.Logging.Level)
	log.Infof("api service starting (version=%s, commit=%s)", version, commit)

	storage, err := app.CreateStorageEngine(cfg)
	if err != nil {
		log.Fatalf("failed to init storage: %v", err)
	}
	log.Infof("storage engine: %s", cfg.Storage.Type)

	tracker, redisClient, err := app.CreateProgressTracker(cfg)
	if err != nil {
		log.Warnf("progress tracker unavailable: %v", err)
	} else {
		log.Info("progress tracker: redis")
	}
	defer func() {
		if redisClient != nil {
			redisClient.Close()
		}
	}()

	engine := router.New(cfg, storage, tracker)

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.API.Port),
		Handler:      engine,
		ReadTimeout:  time.Duration(cfg.API.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(cfg.API.WriteTimeout) * time.Second,
	}

	go func() {
		log.Infof("http server listening on :%d", cfg.API.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("shutdown signal received, stopping")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Errorf("server forced shutdown: %v", err)
	}

	log.Info("api service stopped")
}

