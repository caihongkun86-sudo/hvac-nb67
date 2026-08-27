package main

// config_store.go
//
// 从 hvac.warning_config 表热加载预警阈值，每 30s 轮询一次。
// 使用 atomic.Value 整体替换配置 map，读取零锁争用。
//
// 设计约定：
//   - warn_code 对应 warning_config.warn_code，一行驱动同类型所有 check
//     （如 WARN_EF_CURRENT 控制 HVAC_12~15 四个通风机电流检查）
//   - trigger_value 存 UI 显示单位（A / ℃ / ppm / pa / bar）
//   - params.raw_scale 存与 Go 代码原始比较单位的换算因子（默认 1.0）
//     raw_threshold = trigger_value × raw_scale
//   - duration_seconds > 0 时覆盖硬编码持续时间，否则保持原默认值
//   - enabled = false 时跳过该预警，等同于硬编码默认值

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "github.com/lib/pq"

	"github.com/benthosdev/benthos/v4/public/service"
)

// warnEntry 单条预警配置的运行时表示。
type warnEntry struct {
	TriggerValue         float64 // UI 显示单位阈值（超温类为超出量，单位℃）
	ClearValue           float64 // 消除阈值（与 trigger_value 同单位）；0 表示与 trigger_value 相同（立即消除）
	DurationSeconds      int     // 持续时间门控（秒），0 表示立即触发
	Enabled              bool
	RawScale             float64 // params.raw_scale，默认 1.0
	TargetTemp           float64 // params.target_temp（℃），0=未配置；超温类专用
	MinCoolingRuntimeS   int     // params.min_cooling_runtime_s（秒），-1=未配置
	ThresholdBad         string  // 用于显示的触发阈值文本（与设置页一致），用于生成快照
}

type configMap map[string]warnEntry

// ConfigStore 持有从 DB 加载的预警配置，支持并发安全热更新。
type ConfigStore struct {
	val    atomic.Value // 存储 *configMap，整体替换保证原子性
	db     *sql.DB
	logger *service.Logger
}

var (
	globalConfigStore *ConfigStore
	configStoreOnce   sync.Once
)

// ensureConfigStore 保证 ConfigStore 只初始化一次（sync.Once）。
// 若 PG_DSN 未设置或连接失败，globalConfigStore 保持 nil，所有读取返回硬编码默认值。
func ensureConfigStore(logger *service.Logger) {
	configStoreOnce.Do(func() {
		dsn := os.Getenv("PG_DSN")
		if dsn == "" {
			logger.Infof("ConfigStore: PG_DSN 未设置，使用硬编码阈值（不影响运行）")
			return
		}
		db, err := sql.Open("postgres", dsn)
		if err != nil {
			logger.Warnf("ConfigStore: DB 连接失败，使用硬编码阈值: %v", err)
			return
		}
		cs := &ConfigStore{db: db, logger: logger}
		if err := cs.load(); err != nil {
			logger.Warnf("ConfigStore: 首次加载失败，使用硬编码阈值: %v", err)
		}
		globalConfigStore = cs
		cs.startPolling(context.Background(), 30*time.Second)
		logger.Infof("ConfigStore: 已启动，每 30s 从 hvac.warning_config 刷新预警阈值")
	})
}

func (cs *ConfigStore) startPolling(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := cs.load(); err != nil {
					cs.logger.Warnf("ConfigStore: 轮询加载失败，继续使用上次配置: %v", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (cs *ConfigStore) load() error {
	rows, err := cs.db.Query(
		`SELECT warn_code, trigger_value, COALESCE(clear_value, 0), duration_seconds, enabled, params, COALESCE(threshold_bad, '')
		 FROM hvac.warning_config`)
	if err != nil {
		return err
	}
	defer rows.Close()

	m := make(configMap)
	for rows.Next() {
		var code string
		var tv float64
		var cv float64
		var dur int
		var enabled bool
		var paramsJSON sql.NullString
		var threshBad string
		if err := rows.Scan(&code, &tv, &cv, &dur, &enabled, &paramsJSON, &threshBad); err != nil {
			cs.logger.Warnf("ConfigStore: 行扫描失败，跳过: %v", err)
			continue
		}
		rawScale := 1.0
		targetTemp := 0.0
		minCoolingRuntimeS := -1
		if paramsJSON.Valid {
			var params map[string]any
			if json.Unmarshal([]byte(paramsJSON.String), &params) == nil {
				if s, ok := params["raw_scale"].(float64); ok && s > 0 {
					rawScale = s
				}
				if t, ok := params["target_temp"].(float64); ok && t > 0 {
					targetTemp = t
				}
				if v, ok := params["min_cooling_runtime_s"].(float64); ok && v >= 0 {
					minCoolingRuntimeS = int(v)
				}
			}
		}
		m[code] = warnEntry{
			TriggerValue:       tv,
			ClearValue:         cv,
			DurationSeconds:    dur,
			Enabled:            enabled,
			RawScale:           rawScale,
			TargetTemp:         targetTemp,
			MinCoolingRuntimeS: minCoolingRuntimeS,
			ThresholdBad:       threshBad,
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	cs.val.Store(&m)
	cs.logger.Debugf("ConfigStore: 已加载 %d 条预警配置", len(m))
	
	// 输出关键预警的持续时间配置（便于调试）
	// if e, ok := m["WARN_EF_CURRENT"]; ok {
	// 	cs.logger.Infof("ConfigStore[WARN_EF_CURRENT]: enabled=%v, duration=%ds, threshold=%f, clear=%f", 
	// 		e.Enabled, e.DurationSeconds, e.TriggerValue, e.ClearValue)
	// }
	// if e, ok := m["WARN_CF_CURRENT"]; ok {
	// 	cs.logger.Infof("ConfigStore[WARN_CF_CURRENT]: enabled=%v, duration=%ds, threshold=%f, clear=%f", 
	// 		e.Enabled, e.DurationSeconds, e.TriggerValue, e.ClearValue)
	// }
	return nil
}

// csRawThreshold 返回原始传感器单位的触发阈值。
// 找不到、未启用或 PG_DSN 未配置时，返回 defaultVal（硬编码降级）。
func csRawThreshold(warnCode string, defaultVal int64) int64 {
	cs := globalConfigStore
	if cs == nil {
		return defaultVal
	}
	p := cs.val.Load()
	if p == nil {
		return defaultVal
	}
	m := *p.(*configMap)
	e, ok := m[warnCode]
	if !ok || !e.Enabled {
		return defaultVal
	}
	return int64(e.TriggerValue * e.RawScale)
}

// csDuration 返回持续时间门控。
// DB 中 duration_seconds > 0 时使用 DB 值，否则返回 defaultDur。
func csDuration(warnCode string, defaultDur time.Duration) time.Duration {
	cs := globalConfigStore
	if cs == nil {
		return defaultDur
	}
	p := cs.val.Load()
	if p == nil {
		return defaultDur
	}
	m := *p.(*configMap)
	e, ok := m[warnCode]
	if !ok {
		return defaultDur
	}
	// 若 enabled=false，返回默认值（等同于未配置）
	if !e.Enabled {
		return defaultDur
	}
	// 若 duration_seconds > 0，返回 DB 配置值；否则返回默认值
	if e.DurationSeconds > 0 {
		return time.Duration(e.DurationSeconds) * time.Second
	}
	return defaultDur
}

// csIsEnabled 返回预警项是否启用，找不到时默认启用。
func csIsEnabled(warnCode string) bool {
	cs := globalConfigStore
	if cs == nil {
		return true
	}
	p := cs.val.Load()
	if p == nil {
		return true
	}
	m := *p.(*configMap)
	e, ok := m[warnCode]
	if !ok {
		return true
	}
	return e.Enabled
}

// csCoolingPreconditionDur 返回 HVAC_09 的冷却模式前置持续时间。
// 优先读 params.min_cooling_runtime_s；未配置则返回 defaultDur（硬编码 20min）。
func csCoolingPreconditionDur(warnCode string, defaultDur time.Duration) time.Duration {
	cs := globalConfigStore
	if cs == nil {
		return defaultDur
	}
	p := cs.val.Load()
	if p == nil {
		return defaultDur
	}
	m := *p.(*configMap)
	e, ok := m[warnCode]
	if !ok || e.MinCoolingRuntimeS < 0 {
		return defaultDur
	}
	return time.Duration(e.MinCoolingRuntimeS) * time.Second
}

// csTriggerConditionText 返回触发时刻的配置快照文本（与设置页/BFF 展示逻辑一致）。
// 格式："{threshold_bad} 持续N分钟" 或 "{threshold_bad} 持续Ns"，空字符串表示无快照。
// 用于写入 PredictHit.TriggerConditionSnapshot，确保历史预警展示的触发条件不随配置变更而改变。
func csTriggerConditionText(warnCode string) string {
	cs := globalConfigStore
	if cs == nil {
		return ""
	}
	p := cs.val.Load()
	if p == nil {
		return ""
	}
	m := *p.(*configMap)
	e, ok := m[warnCode]
	if !ok || !e.Enabled {
		return ""
	}
	parts := []string{}
	if e.ThresholdBad != "" {
		parts = append(parts, e.ThresholdBad)
	}
	if e.DurationSeconds > 0 {
		if e.DurationSeconds >= 60 && e.DurationSeconds%60 == 0 {
			parts = append(parts, fmt.Sprintf("持续%d分钟", e.DurationSeconds/60))
		} else {
			parts = append(parts, fmt.Sprintf("持续%d秒", e.DurationSeconds))
		}
	}
	return strings.Join(parts, " ")
}

// csClearThreshold 返回原始传感器单位的消除阈值。
// clear_value = 0（或未设置）时，返回 defaultVal（即与触发阈值相同，立即消除，保持原有行为）。
// 消除阈值必须小于触发阈值，否则退回 defaultVal。
func csClearThreshold(warnCode string, defaultVal int64) int64 {
	cs := globalConfigStore
	if cs == nil {
		return defaultVal
	}
	p := cs.val.Load()
	if p == nil {
		return defaultVal
	}
	m := *p.(*configMap)
	e, ok := m[warnCode]
	if !ok || !e.Enabled || e.ClearValue <= 0 {
		return defaultVal
	}
	cv := int64(e.ClearValue * e.RawScale)
	// 消除阈值必须严格小于触发阈值，防止逻辑错误
	triggerRaw := int64(e.TriggerValue * e.RawScale)
	if cv >= triggerRaw {
		return defaultVal
	}
	return cv
}

// csOvertempAbsThreshold 返回车厢超温的绝对阈值（原始传感器单位）。
// 从 DB 读取 target_temp（目标温度℃）和 trigger_value（允许超出量℃），
// 计算 absolute_raw = (target_temp + trigger_value) × raw_scale。
// 未配置或未启用时返回 defaultRaw（硬编码降级，默认 300 = (26+4)×10）。
func csOvertempAbsThreshold(warnCode string, defaultRaw int64) int64 {
	cs := globalConfigStore
	if cs == nil {
		return defaultRaw
	}
	p := cs.val.Load()
	if p == nil {
		return defaultRaw
	}
	m := *p.(*configMap)
	e, ok := m[warnCode]
	if !ok || !e.Enabled || e.TargetTemp <= 0 {
		return defaultRaw
	}
	return int64((e.TargetTemp + e.TriggerValue) * e.RawScale)
}
