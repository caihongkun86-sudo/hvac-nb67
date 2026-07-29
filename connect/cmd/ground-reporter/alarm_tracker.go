package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type activeAlarm struct {
	UUID      string
	StartTime int64 // unix ms at first detection
}

// AlarmTracker maintains the lifecycle state for 6.1 alarm/predict records.
// For each device, it tracks which hit codes are currently active.
// When a code appears for the first time → fire "start" (endtime empty).
// When a code disappears → fire "end" (endtime = now).
type AlarmTracker struct {
	mu     sync.Mutex
	active map[string]map[string]*activeAlarm // deviceID → code → alarm
}

func newAlarmTracker() *AlarmTracker {
	return &AlarmTracker{
		active: make(map[string]map[string]*activeAlarm),
	}
}

type AlarmDiff struct {
	Added   []ActiveHit // new alarms to report (endtime = "")
	Removed []ActiveHit // ended alarms to report (endtime = now)
}

type ActiveHit struct {
	UUID      string
	Code      string
	Name      string
	StartTime int64 // ms
	EndTime   int64 // ms; 0 means not yet ended
}

// Diff computes which hit codes are new vs. which have ended for the given device.
// currentCodes is the full set of hit codes present in the current message.
// The caller provides nowMs as the reference time so the tracker is deterministic.
func (t *AlarmTracker) Diff(deviceID string, currentCodes []HitCode, nowMs int64) AlarmDiff {
	t.mu.Lock()
	defer t.mu.Unlock()

	existing, ok := t.active[deviceID]
	if !ok {
		existing = make(map[string]*activeAlarm)
		t.active[deviceID] = existing
	}

	currSet := make(map[string]HitCode, len(currentCodes))
	for _, h := range currentCodes {
		currSet[h.Code] = h
	}

	var diff AlarmDiff

	// Codes present now but not before → new alarm
	for code, hit := range currSet {
		if _, exists := existing[code]; !exists {
			uuid := newUUID()
			existing[code] = &activeAlarm{UUID: uuid, StartTime: nowMs}
			diff.Added = append(diff.Added, ActiveHit{
				UUID:      uuid,
				Code:      code,
				Name:      hit.Name,
				StartTime: nowMs,
			})
		}
	}

	// Codes present before but not now → alarm ended
	for code, alarm := range existing {
		if _, stillActive := currSet[code]; !stillActive {
			diff.Removed = append(diff.Removed, ActiveHit{
				UUID:      alarm.UUID,
				Code:      code,
				Name:      "",
				StartTime: alarm.StartTime,
				EndTime:   nowMs,
			})
			delete(existing, code)
		}
	}

	return diff
}

// SyncWithDB 根据数据库中的活跃预警列表，同步更新内存状态
// 参数:
//   - deviceID: 设备 ID
//   - dbActiveCodes: 数据库中活跃的预警代码集合 (code -> true)
//   - nowMs: 当前时间戳
//
// 行为:
//   - 内存中有但数据库中没有的 code → 从内存中删除（预警已被 storage-adapter 消除）
//   - 数据库中有但内存中没有的 code → 添加到内存（预警在数据库中仍活跃）
//
// 返回: 被删除的 code 数量
func (t *AlarmTracker) SyncWithDB(deviceID string, dbActiveCodes map[string]bool, nowMs int64) int {
	t.mu.Lock()
	defer t.mu.Unlock()

	existing, ok := t.active[deviceID]
	if !ok {
		existing = make(map[string]*activeAlarm)
		t.active[deviceID] = existing
	}

	removedCount := 0
	addedCount := 0

	// 找出内存中有但数据库中没有的 code → 删除
	for code := range existing {
		if _, stillActive := dbActiveCodes[code]; !stillActive {
			delete(existing, code)
			removedCount++
		}
	}

	// 找出数据库中有但内存中没有的 code → 添加
	for code := range dbActiveCodes {
		if _, exists := existing[code]; !exists {
			existing[code] = &activeAlarm{
				UUID:      newUUID(),
				StartTime: nowMs,
			}
			addedCount++
		}
	}

	if removedCount > 0 || addedCount > 0 {
		log.Printf("[SYNC-STATE] device=%s: removed %d, added %d from DB", deviceID, removedCount, addedCount)
	}

	return removedCount
}

// HitCode carries the minimal info needed for diff (code + display name).
type HitCode struct {
	Code string
	Name string
}

// newUUID generates a 32-char hex UUID (no dashes) matching platform field max-length=32.
func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%08x%04x%04x%04x%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func nowMs() int64 {
	return time.Now().UnixMilli()
}

// RecoverFromLifecycle 在 ground-reporter 启动时从 hvac.warning_lifecycle 拉活跃集合
// 重建内存 active map，避免重启后把已 active 的预警当作新 open 重发给平台。
//
// 注：恢复的条目 UUID 仍是新生成的（DB 表没存 platform-side UUID），但 Diff 算法
// 只用 UUID 做"同一预警的关联标识"，平台侧用 (device, code) 作为业务键，新 UUID 不会引发重复 open。
// 关键效果：本次启动后再次出现已恢复的 code 时，Diff 看到它"已 active"，不会发 open。
func (t *AlarmTracker) RecoverFromLifecycle(ctx context.Context, pool *pgxpool.Pool) error {
	rows, err := pool.Query(ctx, `
		SELECT device_id, fault_code, start_time
		  FROM hvac.warning_lifecycle
		 WHERE end_time IS NULL
	`)
	if err != nil {
		return fmt.Errorf("alarm tracker recover query: %w", err)
	}
	defer rows.Close()

	t.mu.Lock()
	defer t.mu.Unlock()

	total := 0
	for rows.Next() {
		var deviceID, faultCode string
		var startTime time.Time
		if err := rows.Scan(&deviceID, &faultCode, &startTime); err != nil {
			return fmt.Errorf("alarm tracker recover scan: %w", err)
		}
		// GitHub #23 / RET-46 修复（2026-06-12）：
		// Handle61Predict 用 "predict:" + deviceID 作为 tracker key（见 api_6_1.go），
		// 此处恢复时必须用相同前缀，否则重启后已活跃 predict 不会命中 tracker → 重新
		// 触发 diff.Added → 给平台重发 open 报文（症状 D：重启风暴）。
		key := "predict:" + deviceID
		existing, ok := t.active[key]
		if !ok {
			existing = make(map[string]*activeAlarm)
			t.active[key] = existing
		}
		existing[faultCode] = &activeAlarm{
			UUID:      newUUID(),
			StartTime: startTime.UnixMilli(),
		}
		total++
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("alarm tracker recover iter: %w", err)
	}
	log.Printf("[INFO] alarm tracker recover: %d active codes across %d devices", total, len(t.active))
	return nil
}

// GetDevices 获取所有正在追踪的设备 ID 列表（去除前缀）
func (t *AlarmTracker) GetDevices() []string {
	t.mu.Lock()
	defer t.mu.Unlock()

	deviceSet := make(map[string]bool)
	for key := range t.active {
		// 去除前缀（predict: 或 alarm:）
		deviceID := key
		if len(key) > 8 && key[:8] == "predict:" {
			deviceID = key[8:]
		} else if len(key) > 6 && key[:6] == "alarm:" {
			deviceID = key[6:]
		}
		deviceSet[deviceID] = true
	}

	devices := make([]string, 0, len(deviceSet))
	for deviceID := range deviceSet {
		devices = append(devices, deviceID)
	}
	return devices
}

// HasDevice 检查指定设备键是否存在
func (t *AlarmTracker) HasDevice(deviceKey string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, ok := t.active[deviceKey]
	return ok
}
