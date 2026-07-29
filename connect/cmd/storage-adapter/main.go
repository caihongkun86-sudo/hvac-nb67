package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	cfg := loadConfig()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	pool, err := pgxpool.New(ctx, cfg.PGDSN)
	if err != nil {
		log.Fatalf("failed to connect timescaledb: %v", err)
	}
	defer pool.Close()

	lifecycle := newLifecycleWriter(pool)
	if err := lifecycle.Recover(ctx); err != nil {
		log.Printf("[WARN] lifecycle recover failed (starting with empty state): %v", err)
	}

	// 启动预警超时扫描器
	expireScanner := NewExpireScanner(pool, lifecycle, cfg.AlarmExpireMinutes, cfg.ExpireScanIntervalMinutes)
	expireScanner.Start(ctx)

	service := &adapter{
		cfg:       cfg,
		pool:      pool,
		lifecycle: lifecycle,
	}
	if err := service.run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("storage-adapter stopped with error: %v", err)
	}
}
