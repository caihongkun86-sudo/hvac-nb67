package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

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

	// 启动预警超时清扫器：兜底关闭 last_seen_time 长期未刷新的活跃预警，
	// 避免 device 断流时预警无限期挂起（DB close + 内存 active 同步清理）。
	if cfg.WarnSweepTimeoutSec > 0 && cfg.WarnSweepIntervalSec > 0 {
		go func() {
			ticker := time.NewTicker(time.Duration(cfg.WarnSweepIntervalSec) * time.Second)
			defer ticker.Stop()
			log.Printf("[INFO] lifecycle sweep ticker started: timeout=%ds interval=%ds",
				cfg.WarnSweepTimeoutSec, cfg.WarnSweepIntervalSec)
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					sweepCtx, sweepCancel := context.WithTimeout(ctx, 10*time.Second)
					n, err := lifecycle.SweepTimeouts(sweepCtx, cfg.WarnSweepTimeoutSec)
					sweepCancel()
					if err != nil {
						log.Printf("[WARN] lifecycle sweep failed: %v", err)
					} else if n > 0 {
						log.Printf("[INFO] lifecycle sweep closed %d stale warnings", n)
					}
				}
			}
		}()
	}

	service := &adapter{
		cfg:       cfg,
		pool:      pool,
		lifecycle: lifecycle,
	}
	if err := service.run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("storage-adapter stopped with error: %v", err)
	}
}
