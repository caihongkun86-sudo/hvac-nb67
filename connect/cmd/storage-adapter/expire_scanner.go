package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// expireStaleAlarmsSQL 消除超时预警
	// 条件：end_time IS NULL 且 last_seen_time 超过指定分钟数
	expireStaleAlarmsSQL = `
		UPDATE hvac.warning_lifecycle
		   SET end_time = NOW(),
		       last_seen_time = NOW()
		 WHERE end_time IS NULL
		   AND last_seen_time < NOW() - make_interval(mins => $1)
		 RETURNING device_id, fault_code, start_time;
	`
)

// ExpireScanner 预警超时扫描器
// 定期检查活跃预警，如果 last_seen_time 超过阈值，则自动判定为消除
type ExpireScanner struct {
	pool    *pgxpool.Pool
	writer  *LifecycleWriter
	expire  time.Duration // 超时时间
	scanInt time.Duration // 扫描间隔
}

// NewExpireScanner 创建预警超时扫描器
func NewExpireScanner(pool *pgxpool.Pool, writer *LifecycleWriter, expireMinutes, scanIntervalMinutes int) *ExpireScanner {
	if expireMinutes <= 0 {
		log.Printf("[INFO] expire scanner disabled (ALARM_EXPIRE_MINUTES=0)")
		return nil
	}

	expire := time.Duration(expireMinutes) * time.Minute
	scanInt := time.Duration(scanIntervalMinutes) * time.Minute

	log.Printf("[INFO] expire scanner enabled: expire=%v, scan_interval=%v", expire, scanInt)

	return &ExpireScanner{
		pool:    pool,
		writer:  writer,
		expire:  expire,
		scanInt: scanInt,
	}
}

// Start 启动后台扫描协程
func (s *ExpireScanner) Start(ctx context.Context) {
	if s == nil {
		return
	}

	go s.run(ctx)
}

// run 主循环
func (s *ExpireScanner) run(ctx context.Context) {
	ticker := time.NewTicker(s.scanInt)
	defer ticker.Stop()

	log.Printf("[INFO] expire scanner started")

	for {
		select {
		case <-ctx.Done():
			log.Printf("[INFO] expire scanner stopped")
			return
		case <-ticker.C:
			s.scanExpiredAlarms(ctx)
		}
	}
}

// scanExpiredAlarms 扫描并消除超时预警
func (s *ExpireScanner) scanExpiredAlarms(ctx context.Context) {
	scanCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// 查询并消除超时预警
	rows, err := s.pool.Query(scanCtx, expireStaleAlarmsSQL, int(s.expire.Minutes()))
	if err != nil {
		log.Printf("[ERROR] expire scanner query failed: %v", err)
		return
	}
	defer rows.Close()

	var expiredCount int
	expiredDevices := make(map[string][]string)

	for rows.Next() {
		var deviceID, faultCode string
		var startTime time.Time
		if err := rows.Scan(&deviceID, &faultCode, &startTime); err != nil {
			log.Printf("[ERROR] expire scanner scan failed: %v", err)
			continue
		}
		expiredCount++
		expiredDevices[deviceID] = append(expiredDevices[deviceID], faultCode)
	}

	if err := rows.Err(); err != nil {
		log.Printf("[ERROR] expire scanner rows err: %v", err)
		return
	}

	if expiredCount > 0 {
		log.Printf("[EXPIRE] auto-closed %d stale alarms across %d devices:", expiredCount, len(expiredDevices))
		for deviceID, codes := range expiredDevices {
			log.Printf("  - %s: %v", deviceID, codes)
		}

		// 同步更新内存状态（如果 writer 可用）
		if s.writer != nil {
			s.syncMemoryState(expiredDevices)
		}
	}
}

// syncMemoryState 同步更新内存状态，避免内存与数据库不一致
func (s *ExpireScanner) syncMemoryState(expiredDevices map[string][]string) {
	s.writer.mu.Lock()
	defer s.writer.mu.Unlock()

	totalRemoved := 0
	for deviceID, codes := range expiredDevices {
		prev, ok := s.writer.active[deviceID]
		if !ok {
			continue
		}
		for _, code := range codes {
			if _, exists := prev[code]; exists {
				delete(prev, code)
				totalRemoved++
			}
		}
	}

	if totalRemoved > 0 {
		log.Printf("[EXPIRE-MEM] removed %d codes from memory state", totalRemoved)
	}
}

// ExpireAlarmsOnce 立即执行一次扫描（用于测试或手动触发）
func (s *ExpireScanner) ExpireAlarmsOnce(ctx context.Context) (int, error) {
	scanCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	rows, err := s.pool.Query(scanCtx, expireStaleAlarmsSQL, int(s.expire.Minutes()))
	if err != nil {
		return 0, fmt.Errorf("expire alarms query: %w", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		count++
	}

	if err := rows.Err(); err != nil {
		return count, fmt.Errorf("expire alarms rows: %w", err)
	}

	return count, nil
}
