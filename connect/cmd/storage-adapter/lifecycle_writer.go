package main

// lifecycle_writer.go
//
// 预警生命周期写入器（Phase 2+3 合并实现）：
// 在 storage-adapter 进程内维护一份 per-device active fault_code 集合，
// 每收到一帧 predict 事件做 set-diff，只在状态变化时写入 hvac.warning_lifecycle：
//   - 新出现的 code → INSERT (open)
//   - 不再出现的 code → UPDATE end_time (close)
//   - 仍出现的 code → UPDATE last_seen_time（轻量心跳）
//
// 数据库层有部分唯一索引 UNIQUE(device_id, fault_code) WHERE end_time IS NULL 兜底，
// 即使应用层判定失误，也无法插入重复活跃行（migration 08-20260603）。
//
// 启动恢复：从 warning_lifecycle WHERE end_time IS NULL 拉取已有活跃集合，
// 进程重启不会导致整批重发或丢状态。

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	lifecycleInsertSQL = `
INSERT INTO hvac.warning_lifecycle (
    device_id, line_id, train_id, carriage_id, unit_id,
    fault_code, fault_name, warn_code, severity,
    start_time, last_seen_time, trigger_snapshot, source
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
ON CONFLICT (device_id, fault_code) WHERE end_time IS NULL DO UPDATE
SET last_seen_time = EXCLUDED.last_seen_time;
`

	lifecycleCloseSQL = `
UPDATE hvac.warning_lifecycle
   SET end_time = $1,
       last_seen_time = $1
 WHERE device_id = $2
   AND fault_code = $3
   AND end_time IS NULL;
`

	lifecycleTouchSQL = `
UPDATE hvac.warning_lifecycle
   SET last_seen_time = $1
 WHERE device_id = $2
   AND fault_code = $3
   AND end_time IS NULL;
`

	lifecycleRecoverSQL = `
SELECT device_id, fault_code
  FROM hvac.warning_lifecycle
 WHERE end_time IS NULL;
`

	lifecycleSweepSelectSQL = `
SELECT device_id, fault_code
  FROM hvac.warning_lifecycle
 WHERE end_time IS NULL
   AND start_time < now() - ($1 * interval '1 second');
`

	lifecycleSweepUpdateSQL = `
UPDATE hvac.warning_lifecycle
   SET end_time = last_seen_time,
       updated_at = now()
 WHERE end_time IS NULL
   AND start_time < now() - ($1 * interval '1 second');
`
)

// PredictHitMeta 是 lifecycle 关心的最小 hit 信息。
type PredictHitMeta struct {
	FaultCode string
	FaultName string
	Severity  int16
	Payload   json.RawMessage // 触发时刻 hit 原文（含 trigger_condition_snapshot）
}

// PredictFrame 是 nb67 单条 predict 消息的设备级摘要。
// 关键：空帧（Hits=nil/empty）也合法 —— 表示 "该 device 在 EventTimeText 时刻没有任何活跃预警"，
// 是 lifecycle 状态机判定 close 的唯一信号源。
type PredictFrame struct {
	DeviceID      string
	LineID        int32
	TrainID       int32
	CarriageID    int32
	EventTimeText string
	Hits          []PredictHitMeta
}

// LifecycleWriter 维护 per-device 的活跃 fault_code 集合，
// 并把状态变化落 hvac.warning_lifecycle。
type LifecycleWriter struct {
	pool *pgxpool.Pool
	mu   sync.Mutex
	// active[deviceID] = set(fault_code)
	active map[string]map[string]struct{}
}

func newLifecycleWriter(pool *pgxpool.Pool) *LifecycleWriter {
	return &LifecycleWriter{
		pool:   pool,
		active: make(map[string]map[string]struct{}),
	}
}

// Recover 从 DB 拉取所有 end_time IS NULL 的活跃行，重建内存集合。
// 应在 Kafka 消费开始前调用一次。
func (w *LifecycleWriter) Recover(ctx context.Context) error {
	rows, err := w.pool.Query(ctx, lifecycleRecoverSQL)
	if err != nil {
		return fmt.Errorf("lifecycle recover query: %w", err)
	}
	defer rows.Close()

	w.mu.Lock()
	defer w.mu.Unlock()
	w.active = make(map[string]map[string]struct{})

	total := 0
	for rows.Next() {
		var deviceID, faultCode string
		if err := rows.Scan(&deviceID, &faultCode); err != nil {
			return fmt.Errorf("lifecycle recover scan: %w", err)
		}
		set, ok := w.active[deviceID]
		if !ok {
			set = make(map[string]struct{})
			w.active[deviceID] = set
		}
		set[faultCode] = struct{}{}
		total++
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("lifecycle recover iter: %w", err)
	}
	log.Printf("[INFO] lifecycle recover: %d active warnings across %d devices", total, len(w.active))
	return nil
}

// ProcessFrames 对一批 PredictFrame 做 per-device, per-frame 的 diff，并落库。
// 同一 device 在同一 batch 内可能多帧，按 EventTimeText 升序逐帧推进状态。
func (w *LifecycleWriter) ProcessFrames(ctx context.Context, frames []PredictFrame) error {
	if len(frames) == 0 {
		return nil
	}
	// 分组：device → []frame
	byDevice := make(map[string][]PredictFrame)
	for _, f := range frames {
		if f.DeviceID == "" {
			continue
		}
		byDevice[f.DeviceID] = append(byDevice[f.DeviceID], f)
	}
	for device, list := range byDevice {
		// 按 EventTimeText 升序（RFC3339-like 字典序 == 时间序）
		sortFramesByTime(list)
		for _, frame := range list {
			eventTime, ok := parseEventTime(frame.EventTimeText, true, time.Now().UTC())
			_ = ok
			if err := w.applyFrame(ctx, device, frame, eventTime); err != nil {
				return err
			}
		}
	}
	return nil
}

// applyFrame 对单个 (device, event_time) 帧做 diff 与写库。
func (w *LifecycleWriter) applyFrame(ctx context.Context, deviceID string, frame PredictFrame, eventTime time.Time) error {
	w.mu.Lock()
	prev, ok := w.active[deviceID]
	if !ok {
		prev = make(map[string]struct{})
		w.active[deviceID] = prev
	}

	currCodes := make(map[string]PredictHitMeta, len(frame.Hits))
	for _, h := range frame.Hits {
		if h.FaultCode == "" {
			continue
		}
		currCodes[h.FaultCode] = h
	}

	var toOpen []PredictHitMeta
	var toTouch []PredictHitMeta
	for _, h := range frame.Hits {
		if h.FaultCode == "" {
			continue
		}
		if _, exists := prev[h.FaultCode]; exists {
			toTouch = append(toTouch, h)
		} else {
			toOpen = append(toOpen, h)
		}
	}

	var toClose []string
	for code := range prev {
		if _, stillActive := currCodes[code]; !stillActive {
			toClose = append(toClose, code)
		}
	}

	// 推进内存状态（即便 DB 写失败也保持单调推进，避免重启风暴）
	for _, h := range toOpen {
		prev[h.FaultCode] = struct{}{}
	}
	for _, c := range toClose {
		delete(prev, c)
	}
	w.mu.Unlock()

	if len(toOpen) == 0 && len(toClose) == 0 && len(toTouch) == 0 {
		return nil
	}

	tx, err := w.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("lifecycle tx begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	for _, h := range toOpen {
		warnCode := inferWarnCode(h.FaultCode)
		unitID := inferUnitID(h.FaultCode)
		if _, err := tx.Exec(ctx, lifecycleInsertSQL,
			deviceID, frame.LineID, frame.TrainID, frame.CarriageID, unitID,
			h.FaultCode, h.FaultName, warnCode, h.Severity,
			eventTime, eventTime, h.Payload, "connect-rule-v2",
		); err != nil {
			return fmt.Errorf("lifecycle insert %s/%s: %w", deviceID, h.FaultCode, err)
		}
	}
	for _, h := range toTouch {
		if _, err := tx.Exec(ctx, lifecycleTouchSQL, eventTime, deviceID, h.FaultCode); err != nil {
			return fmt.Errorf("lifecycle touch %s/%s: %w", deviceID, h.FaultCode, err)
		}
	}
	for _, code := range toClose {
		if _, err := tx.Exec(ctx, lifecycleCloseSQL, eventTime, deviceID, code); err != nil {
			return fmt.Errorf("lifecycle close %s/%s: %w", deviceID, code, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("lifecycle tx commit: %w", err)
	}
	if len(toOpen) > 0 || len(toClose) > 0 {
		log.Printf("[INFO] lifecycle %s: +%d -%d ~%d", deviceID, len(toOpen), len(toClose), len(toTouch))
	}
	return nil
}

// inferWarnCode 用 HVAC 编码序号推 warning_config.warn_code，对齐 BFF 的 hvacSeqToWarnCode。
// 输入：HVACxyz，xyz % 100 = seq；带 _v/_c 后缀的特殊处理冷媒泄漏。
func inferWarnCode(faultCode string) string {
	if !strings.HasPrefix(faultCode, "HVAC") {
		return ""
	}
	numStr := strings.TrimPrefix(faultCode, "HVAC")
	if idx := strings.Index(numStr, "_"); idx >= 0 {
		numStr = numStr[:idx]
	}
	var n int
	fmt.Sscanf(numStr, "%d", &n)
	seq := n % 100
	switch {
	case seq >= 1 && seq <= 4:
		if strings.HasSuffix(faultCode, "_v") {
			return "WARN_REFRIGERANT_LEAK_VENT"
		}
		return "WARN_REFRIGERANT_LEAK_COOLING"
	case seq >= 5 && seq <= 6:
		return "WARN_COOLING_SYSTEM"
	case seq >= 7 && seq <= 8:
		return "WARN_TEMP_SENSOR"
	case seq == 9:
		return "WARN_CABIN_OVERHEAT"
	case seq >= 10 && seq <= 11:
		return "WARN_FILTER_CLOG"
	case seq >= 12 && seq <= 15:
		return "WARN_EF_CURRENT"
	case seq >= 16 && seq <= 19:
		return "WARN_CF_CURRENT"
	case seq == 20:
		return "WARN_EXUF_CURRENT"
	case seq >= 21 && seq <= 24:
		return "WARN_CP_CURRENT"
	case seq == 25, seq == 26:
		return "WARN_AQ_CO2"
	}
	return ""
}

// inferUnitID 用 fault_code 序号推 unit_id（机组号 1/2），单数侧→机组1 / 双数侧→机组2。
func inferUnitID(faultCode string) *int16 {
	if !strings.HasPrefix(faultCode, "HVAC") {
		return nil
	}
	numStr := strings.TrimPrefix(faultCode, "HVAC")
	if idx := strings.Index(numStr, "_"); idx >= 0 {
		numStr = numStr[:idx]
	}
	var n int
	fmt.Sscanf(numStr, "%d", &n)
	seq := n % 100
	var u int16
	switch seq {
	case 1, 2, 5, 10, 12, 13, 16, 17, 21, 22, 25:
		u = 1
	case 3, 4, 6, 11, 14, 15, 18, 19, 23, 24, 26:
		u = 2
	default:
		return nil
	}
	return &u
}

// sortFramesByTime 按 EventTimeText 字符串升序排序（RFC3339 字典序 == 时间序）。
func sortFramesByTime(frames []PredictFrame) {
	for i := 1; i < len(frames); i++ {
		for j := i; j > 0 && frames[j-1].EventTimeText > frames[j].EventTimeText; j-- {
			frames[j-1], frames[j] = frames[j], frames[j-1]
		}
	}
}

// SweepTimeouts 关闭超时未刷新的活跃预警，并同步清理内存 active 集合。
//
// 触发条件：end_time IS NULL 且 last_seen_time 距今超过 timeoutSec 秒。
// storage-adapter 每帧都刷新 last_seen_time，若连续 timeoutSec 秒未刷新，
// 说明设备断流，应由清扫器兜底关闭，避免预警无限期挂起。
//
// 本方法在同一进程内原子完成"DB close + 内存清理"，避免"DB 已 close、内存仍 active"
// 的不一致（否则设备恢复后 applyFrame 误走 toTouch，lifecycleTouchSQL 的 WHERE
// end_time IS NULL 匹配 0 行，DB 永远不会重新 INSERT）。
//
// 竞态：UPDATE 的 WHERE 同样判断 last_seen_time，查询后若有新帧刷新，UPDATE 不会误 close；
// 清内存时多删的 code 会在下次 applyFrame 重新 INSERT（部分唯一索引兜底防重）。
func (w *LifecycleWriter) SweepTimeouts(ctx context.Context, timeoutSec int) (int, error) {
	if timeoutSec <= 0 {
		return 0, nil
	}

	rows, err := w.pool.Query(ctx, lifecycleSweepSelectSQL, timeoutSec)
	if err != nil {
		return 0, fmt.Errorf("sweep query: %w", err)
	}
	type dc struct{ device, code string }
	var cleared []dc
	for rows.Next() {
		var d, c string
		if err := rows.Scan(&d, &c); err != nil {
			rows.Close()
			return 0, fmt.Errorf("sweep scan: %w", err)
		}
		cleared = append(cleared, dc{d, c})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("sweep iter: %w", err)
	}
	if len(cleared) == 0 {
		return 0, nil
	}

	if _, err := w.pool.Exec(ctx, lifecycleSweepUpdateSQL, timeoutSec); err != nil {
		return 0, fmt.Errorf("sweep update: %w", err)
	}

	w.mu.Lock()
	for _, c := range cleared {
		if codes, ok := w.active[c.device]; ok {
			delete(codes, c.code)
			if len(codes) == 0 {
				delete(w.active, c.device)
			}
		}
	}
	w.mu.Unlock()

	log.Printf("[INFO] lifecycle sweep: closed %d stale warnings (timeout=%ds)", len(cleared), timeoutSec)
	return len(cleared), nil
}
