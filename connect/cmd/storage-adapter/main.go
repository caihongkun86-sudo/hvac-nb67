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

	// 根据订阅的 topic 条件化创建 lifecycle writer，避免一个容器跑两个 writer + 两个清扫器。
	// consumer.go 已有 != nil 检查，nil 的 writer 不会被调用。
	subscribesPredict := containsTopic(cfg.KafkaTopics, "signal-predict")
	subscribesAlarm := containsTopic(cfg.KafkaTopics, "signal-alarm")

	var lifecycle *LifecycleWriter
	if subscribesPredict {
		lifecycle = newLifecycleWriter(pool)
		if err := lifecycle.Recover(ctx); err != nil {
			log.Printf("[WARN] lifecycle recover failed (starting with empty state): %v", err)
		}
	}

	var alarmLifecycle *AlarmLifecycleWriter
	if subscribesAlarm {
		alarmLifecycle = newAlarmLifecycleWriter(pool)
		if err := alarmLifecycle.Recover(ctx); err != nil {
			log.Printf("[WARN] alarm lifecycle recover failed (starting with empty state): %v", err)
		}
	}

	// 启动预警超时清扫器：兜底关闭 last_seen_time 长期未刷新的活跃预警，
	// 避免 device 断流时预警无限期挂起（DB close + 内存 active 同步清理）。
	if lifecycle != nil && cfg.WarnSweepTimeoutSec > 0 && cfg.WarnSweepIntervalSec > 0 {
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

	// 启动告警超时清扫器：兜底关闭 last_seen_time 长期未刷新的活跃告警，
	// 避免 device 断流时告警无限期挂起（DB close + 内存 active 同步清理）。
	if alarmLifecycle != nil && cfg.AlarmSweepTimeoutSec > 0 && cfg.AlarmSweepIntervalSec > 0 {
		go func() {
			ticker := time.NewTicker(time.Duration(cfg.AlarmSweepIntervalSec) * time.Second)
			defer ticker.Stop()
			log.Printf("[INFO] alarm lifecycle sweep ticker started: timeout=%ds interval=%ds",
				cfg.AlarmSweepTimeoutSec, cfg.AlarmSweepIntervalSec)
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					sweepCtx, sweepCancel := context.WithTimeout(ctx, 10*time.Second)
					n, err := alarmLifecycle.SweepTimeouts(sweepCtx, cfg.AlarmSweepTimeoutSec)
					sweepCancel()
					if err != nil {
						log.Printf("[WARN] alarm lifecycle sweep failed: %v", err)
					} else if n > 0 {
						log.Printf("[INFO] alarm lifecycle sweep closed %d stale alarms", n)
					}
				}
			}
		}()
	}

	service := &adapter{
		cfg:            cfg,
		pool:           pool,
		lifecycle:      lifecycle,
		alarmLifecycle: alarmLifecycle,
	}
	if err := service.run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("storage-adapter stopped with error: %v", err)
	}
}

func containsTopic(topics []string, target string) bool {
	for _, t := range topics {
		if t == target {
			return true
		}
	}
	return false
}
