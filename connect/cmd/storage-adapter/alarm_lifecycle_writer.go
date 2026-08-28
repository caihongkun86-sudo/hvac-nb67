// alarm_lifecycle_writer.go
//
// 告警生命周期写入器（对齐 LifecycleWriter 的设计，服务于 alarm 路径）：
// 在 storage-adapter 进程内维护一份 per-device active fault_code 集合，
// 每收到一帧 alarm 事件做 set-diff，只在状态变化时写入 hvac.alarm_lifecycle：
//   - 新出现的 code → INSERT (open)
//   - 仍出现的 code → UPDATE last_seen_time（轻量心跳）
//   - 不再出现的 code → UPDATE end_time = last_seen_time (close)
//
// 时间字段约定：
//   start_time      = 首次命中 event_time（应用层传入）
//   last_seen_time  = 最近命中 event_time（应用层传入）
//   end_time        = last_seen_time（close 或清扫器兜底时填）
//   created_at      = DB now()（DDL DEFAULT，INSERT 时不显式传）
//   updated_at      = DB now()（由 tr_alarm_lifecycle_updated_at 触发器维护）
//
// 双轨过渡：本 writer 写 alarm_lifecycle，consumer 仍同时写 fact_event，
// 待 BFF 切换查询 + 回填验证后再下线 fact_event 的 alarm 写入。
//
// 启动恢复：从 alarm_lifecycle WHERE end_time IS NULL 拉活跃集合重建内存，
// 进程重启不会重复 open。

package main

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
	// alarmInsertSQL：open。start_time=last_seen_time=event_time。
	// created_at/updated_at 由 DB DEFAULT now() 填充（不显式传）。
	// ON CONFLICT 时（部分唯一索引命中活跃行）退化为 touch：刷 last_seen_time（updated_at 由触发器维护）。
	alarmInsertSQL = `
INSERT INTO hvac.alarm_lifecycle (
    device_id, line_id, train_id, carriage_id, unit_id,
    fault_code, fault_name, severity,
    start_time, last_seen_time, trigger_snapshot, source
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
ON CONFLICT (device_id, fault_code) WHERE end_time IS NULL DO UPDATE
SET last_seen_time = EXCLUDED.last_seen_time;
`

	// alarmTouchSQL：仍命中的 code 刷 last_seen_time(event_time)。updated_at 由触发器维护。
	alarmTouchSQL = `
UPDATE hvac.alarm_lifecycle
   SET last_seen_time = $1
 WHERE device_id  = $2
   AND fault_code  = $3
   AND end_time IS NULL;
`

	// alarmCloseSQL：消失的 code，end_time = last_seen_time。updated_at 由触发器维护。
	alarmCloseSQL = `
UPDATE hvac.alarm_lifecycle
   SET end_time = last_seen_time
 WHERE device_id  = $1
   AND fault_code  = $2
   AND end_time IS NULL;
`

	alarmRecoverSQL = `
SELECT device_id, fault_code
  FROM hvac.alarm_lifecycle
 WHERE end_time IS NULL
`

	// alarmSweepSelectSQL：查超时未刷新的活跃行（last_seen_time 距今 > timeoutSec）。
	alarmSweepSelectSQL = `
SELECT device_id, fault_code
  FROM hvac.alarm_lifecycle
 WHERE end_time IS NULL
   AND last_seen_time < now() - ($1 * interval '1 second');
`

	// alarmSweepUpdateSQL：兜底关闭，end_time = last_seen_time。updated_at 由触发器维护。
	alarmSweepUpdateSQL = `
UPDATE hvac.alarm_lifecycle
   SET end_time = last_seen_time
 WHERE end_time IS NULL
   AND last_seen_time < now() - ($1 * interval '1 second');
`
)

// AlarmHitMeta 携带单条告警的最小信息（code + 名称 + 严重度 + 原始 payload）。
type AlarmHitMeta struct {
	FaultCode string
	FaultName string
	Severity  int16
	Payload   json.RawMessage
}

// AlarmFrame 一帧 alarm 事件（一个 device 在一个 event_time 的全部 hits）。
// 空帧（Hits 为空）是 close 信号：device 仍在线但本帧无告警 → 之前 active 的 code 全部 close。
type AlarmFrame struct {
	DeviceID      string
	LineID        int32
	TrainID       int32
	CarriageID    int32
	EventTimeText string // 原始 event_time 文本，applyFrame 内解析
	Hits          []AlarmHitMeta
}

// AlarmLifecycleWriter 维护 per-device 活跃 fault_code 集合，把状态变化落 hvac.alarm_lifecycle。
type AlarmLifecycleWriter struct {
	pool   *pgxpool.Pool
	mu     sync.Mutex
	active map[string]map[string]struct{} // deviceID → set(fault_code)
}

func newAlarmLifecycleWriter(pool *pgxpool.Pool) *AlarmLifecycleWriter {
	return &AlarmLifecycleWriter{
		pool:   pool,
		active: make(map[string]map[string]struct{}),
	}
}

// Recover 从 DB 拉取所有 end_time IS NULL 的活跃行，重建内存集合。
// 应在 Kafka 消费开始前调用一次。
func (w *AlarmLifecycleWriter) Recover(ctx context.Context) error {
	rows, err := w.pool.Query(ctx, alarmRecoverSQL)
	if err != nil {
		return fmt.Errorf("alarm lifecycle recover query: %w", err)
	}
	defer rows.Close()

	w.mu.Lock()
	defer w.mu.Unlock()
	w.active = make(map[string]map[string]struct{})

	total := 0
	for rows.Next() {
		var deviceID, faultCode string
		if err := rows.Scan(&deviceID, &faultCode); err != nil {
			return fmt.Errorf("alarm lifecycle recover scan: %w", err)
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
		return fmt.Errorf("alarm lifecycle recover iter: %w", err)
	}
	log.Printf("[INFO] alarm lifecycle recover: %d active codes across %d devices", total, len(w.active))
	return nil
}

// ProcessFrames 对一批 AlarmFrame 做 per-device diff 并落库。
// 同一 device 在同一 batch 内可能多帧，按 EventTimeText 升序逐帧推进状态。
func (w *AlarmLifecycleWriter) ProcessFrames(ctx context.Context, frames []AlarmFrame) error {
	if len(frames) == 0 {
		return nil
	}
	byDevice := make(map[string][]AlarmFrame)
	for _, f := range frames {
		if f.DeviceID == "" {
			continue
		}
		byDevice[f.DeviceID] = append(byDevice[f.DeviceID], f)
	}
	for device, list := range byDevice {
		sortAlarmFramesByTime(list)
		for _, frame := range list {
			eventTime, _ := parseEventTime(frame.EventTimeText, true, time.Now().UTC())
			if err := w.applyFrame(ctx, device, frame, eventTime); err != nil {
				return err
			}
		}
	}
	return nil
}

// applyFrame 对单个 (device, event_time) 帧做 diff 与写库。
func (w *AlarmLifecycleWriter) applyFrame(ctx context.Context, deviceID string, frame AlarmFrame, eventTime time.Time) error {
	w.mu.Lock()
	prev, ok := w.active[deviceID]
	if !ok {
		prev = make(map[string]struct{})
		w.active[deviceID] = prev
	}

	currCodes := make(map[string]AlarmHitMeta, len(frame.Hits))
	for _, h := range frame.Hits {
		if h.FaultCode == "" {
			continue
		}
		currCodes[h.FaultCode] = h
	}

	var toOpen []AlarmHitMeta
	var toTouch []AlarmHitMeta
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
		return fmt.Errorf("alarm lifecycle tx begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	for _, h := range toOpen {
		unitID := inferAlarmUnitID(h.FaultCode)
		if _, err := tx.Exec(ctx, alarmInsertSQL,
			deviceID, frame.LineID, frame.TrainID, frame.CarriageID, unitID,
			h.FaultCode, h.FaultName, h.Severity,
			eventTime, eventTime, h.Payload, "connect-rule-v2",
		); err != nil {
			return fmt.Errorf("alarm lifecycle insert %s/%s: %w", deviceID, h.FaultCode, err)
		}
	}
	for _, h := range toTouch {
		if _, err := tx.Exec(ctx, alarmTouchSQL, eventTime, deviceID, h.FaultCode); err != nil {
			return fmt.Errorf("alarm lifecycle touch %s/%s: %w", deviceID, h.FaultCode, err)
		}
	}
	for _, code := range toClose {
		if _, err := tx.Exec(ctx, alarmCloseSQL, deviceID, code); err != nil {
			return fmt.Errorf("alarm lifecycle close %s/%s: %w", deviceID, code, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("alarm lifecycle tx commit: %w", err)
	}
	if len(toOpen) > 0 || len(toClose) > 0 {
		log.Printf("[INFO] alarm lifecycle %s: +%d -%d ~%d", deviceID, len(toOpen), len(toClose), len(toTouch))
	}
	return nil
}

// SweepTimeouts 关闭超时未刷新的活跃告警，并同步清理内存 active 集合。
//
// 触发条件：end_time IS NULL 且 last_seen_time 距今超过 timeoutSec 秒。
// alarm 路径若上游不发空帧（无法靠 diff close 清除），设备断流时告警会无限期挂起，
// 由本清扫器兜底关闭（DB close + 内存 active 同步清理），避免告警卡死。
//
// 竞态：UPDATE 的 WHERE 同样判断 last_seen_time，查询后若有新帧刷新，UPDATE 不会误 close；
// 清内存时多删的 code 会在下次 applyFrame 重新 INSERT（部分唯一索引兜底防重）。
func (w *AlarmLifecycleWriter) SweepTimeouts(ctx context.Context, timeoutSec int) (int, error) {
	if timeoutSec <= 0 {
		return 0, nil
	}

	rows, err := w.pool.Query(ctx, alarmSweepSelectSQL, timeoutSec)
	if err != nil {
		return 0, fmt.Errorf("alarm sweep query: %w", err)
	}
	type dc struct{ device, code string }
	var cleared []dc
	for rows.Next() {
		var d, c string
		if err := rows.Scan(&d, &c); err != nil {
			rows.Close()
			return 0, fmt.Errorf("alarm sweep scan: %w", err)
		}
		cleared = append(cleared, dc{d, c})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("alarm sweep iter: %w", err)
	}
	if len(cleared) == 0 {
		return 0, nil
	}

	if _, err := w.pool.Exec(ctx, alarmSweepUpdateSQL, timeoutSec); err != nil {
		return 0, fmt.Errorf("alarm sweep update: %w", err)
	}

	// 内存同步：DB 已 close 的 code 从 active 删除，否则设备恢复后 applyFrame 误走 toTouch
	// （alarmTouchSQL 的 WHERE end_time IS NULL 匹配 0 行，DB 永不重新 INSERT）
	w.mu.Lock()
	for _, d := range cleared {
		if set, ok := w.active[d.device]; ok {
			delete(set, d.code)
			if len(set) == 0 {
				delete(w.active, d.device)
			}
		}
	}
	w.mu.Unlock()

	log.Printf("[INFO] alarm lifecycle sweep: closed %d stale alarms (timeout=%ds)", len(cleared), timeoutSec)
	return len(cleared), nil
}

// sortAlarmFramesByTime 按 EventTimeText 字符串升序排序（RFC3339 字典序 == 时间序）。
func sortAlarmFramesByTime(frames []AlarmFrame) {
	for i := 1; i < len(frames); i++ {
		for j := i; j > 0 && frames[j-1].EventTimeText > frames[j].EventTimeText; j-- {
			frames[j-1], frames[j] = frames[j], frames[j-1]
		}
	}
}

// inferAlarmUnitID 用 alarm fault_code 序号推 unit_id（机组号 1/2）。
// alarm 码格式 HVAC{carriageID*100+seq}（与 predict 同格式，但 seq 范围与机组划分不同）：
//   - seq 27-46：机组1告警 → unit_id=1
//   - seq 47-66：机组2告警 → unit_id=2
//   - seq 67-75：公共告警 → nil（不分机组）
//   - 其他（含 predict 的 1-26、125-126）：nil（alarm_lifecycle 不应出现，保守返回 nil）
//
// 注意：与 inferUnitID（predict 用的单双数划分）逻辑不同，不可混用。
// 来源：nb67_event_processor.go L668/L700/L727/L751 + alertcode_v2.xlsx。
func inferAlarmUnitID(faultCode string) *int16 {
	if !strings.HasPrefix(faultCode, "HVAC") {
		return nil
	}
	numStr := strings.TrimPrefix(faultCode, "HVAC")
	if idx := strings.Index(numStr, "_"); idx >= 0 {
		numStr = numStr[:idx]
	}
	var n int
	if _, err := fmt.Sscanf(numStr, "%d", &n); err != nil {
		return nil
	}
	seq := n % 100
	switch {
	case seq >= 27 && seq <= 46:
		u := int16(1)
		return &u
	case seq >= 47 && seq <= 66:
		u := int16(2)
		return &u
	default:
		return nil
	}
}
