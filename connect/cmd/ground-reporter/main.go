package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	cfg := loadConfig()
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
	log.Printf("[INFO] ground-reporter starting: faultRecordURL=%s sysStatusURL=%s lifeRecordURL=%s subsystem=%s trainType=%s",
		cfg.FaultRecordURL, cfg.SysStatusURL, cfg.LifeRecordURL, cfg.SubsystemCode, cfg.TrainType)

	client := newPlatformClient(cfg)
	tracker := newAlarmTracker()
	lifeCache := newLifeCache()
	stationCache := newStationCache()

	// 初始化批量发送器
	batchSender := newBatchSender(client, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	// Phase 6: 启动时从 warning_lifecycle 恢复活跃集合（PG_DSN 非空时启用）。
	// 避免 ground-reporter 重启后整批重发 open 事件给平台。
	// pool 长持：SyncClearedFromDB 定时同步也依赖它。
	var longLivedPool *pgxpool.Pool
	if cfg.PGDSN != "" {
		pool, perr := pgxpool.New(ctx, cfg.PGDSN)
		if perr != nil {
			log.Printf("[WARN] PG pool init failed, skip lifecycle recover: %v", perr)
		} else {
			if rerr := tracker.RecoverFromLifecycle(ctx, pool); rerr != nil {
				log.Printf("[WARN] alarm tracker recover failed: %v", rerr)
			}
			longLivedPool = pool
		}
	}

	// 启动内存 active 同步协程：定期从 DB 活跃集合清理内存中已被 storage-adapter
	// SweepTimeouts close 的 code，使下次 hits=[X] 到达时能重新触发 Added 上报平台
	// （"消除后若仍存在能及时重报"）。
	if longLivedPool != nil && cfg.AlarmSyncIntervalSec > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ticker := time.NewTicker(time.Duration(cfg.AlarmSyncIntervalSec) * time.Second)
			defer ticker.Stop()
			log.Printf("[INFO] alarm sync ticker started: interval=%ds", cfg.AlarmSyncIntervalSec)
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					syncCtx, syncCancel := context.WithTimeout(ctx, 10*time.Second)
					if _, err := tracker.SyncClearedFromDB(syncCtx, longLivedPool); err != nil {
						log.Printf("[WARN] alarm sync failed: %v", err)
					}
					syncCancel()
				}
			}
		}()
	}

	// 启动批量发送器
	if batchSender != nil {
		batchSender.Start()
	}

	// --- 6.1: signal-predict ---
	wg.Add(1)
	go consumeTopic(ctx, &wg, cfg.KafkaBrokers,
		"signal-predict", "ground-reporter-predict",
		func(data []byte) {
			Handle61Predict(ctx, batchSender, tracker, stationCache, cfg, data)
		},
	)

	// --- 6.7 per-action: signal-life ---
	wg.Add(1)
	go consumeTopic(ctx, &wg, cfg.KafkaBrokers,
		"signal-life", "ground-reporter-life",
		func(data []byte) {
			Handle67LifeAction(ctx, client, cfg, data)
		},
	)

	// --- 6.7 daily cache + station cache: signal-parsed ---
	wg.Add(1)
	go consumeTopic(ctx, &wg, cfg.KafkaBrokers,
		"signal-parsed", "ground-reporter-life-cache",
		func(data []byte) {
			var msg ParsedMsg
			if err := json.Unmarshal(data, &msg); err != nil {
				if cfg.LogLevel == "DEBUG" {
					log.Printf("[DEBUG] life-cache: bad json: %v", err)
				}
				return
			}
			lifeCache.Update(msg, cfg.TrainType)
			stationCache.Update(msg)
		},
	)

	// --- 6.6: heartbeat timer ---
	wg.Add(1)
	go func() {
		defer wg.Done()
		Run66Heartbeat(ctx, client, cfg)
	}()

	// --- 6.7: daily batch timer ---
	wg.Add(1)
	go func() {
		defer wg.Done()
		Run67DailyBatch(ctx, client, lifeCache, cfg)
	}()

	// Graceful shutdown on SIGTERM / SIGINT
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	<-quit

	log.Printf("[INFO] ground-reporter shutting down...")

	// 停止批量发送器，等待剩余记录发送完成
	if batchSender != nil {
		batchSender.Stop()
	}

	// 关闭长持 PG pool（启动恢复 + SyncClearedFromDB 共用）
	if longLivedPool != nil {
		longLivedPool.Close()
	}

	cancel()
	wg.Wait()
	log.Printf("[INFO] ground-reporter stopped")
}
