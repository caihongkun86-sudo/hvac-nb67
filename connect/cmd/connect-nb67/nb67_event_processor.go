package main

// nb67_event_processor.go
//
// NB67 空调事件构建处理器（Go 原生实现）
//
// 将原先写在 YAML mapping: | 块中的 Bloblang 脚本全部迁移到 Go 代码，
// 解决 Bloblang 解析器对注释中多字节（中文）字符的误判问题，
// 同时获得完整的类型检查、单元测试和 IDE 支持。
//
// 注册处理器名称：nb67_event_builder
// 输入消息：nb67_parser 输出的 signal-parsed JSON（含 raw 字段）
// 输出消息：三个子事件聚合体，YAML 通过 fan_out + mapping 分拣到三个 topic
//
// 事件码规范：
//   HVAC 预警码 = "HVAC" + string(carriage_id*100 + seq)  （来源：NB67 空调预警码表 20240802）
//   部件寿命码  = string(carriage_id*1000 + 50000 + offset)（来源：NB67 空调部件码表 20240802）
//
// Author: Macda Connect Team
// Google Go Style Guide: https://google.github.io/styleguide/go/

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/benthosdev/benthos/v4/public/service"
)

// ============================================================
// 寿命阈值常量（只需在此处修改）
// ============================================================

const (
	// 风机类（通风机 / 冷凝风机 / 废排风机）额定寿命 25000h = 90000000s
	fanLifeS = 90_000_000 // 额定寿命（秒）
	fanWarnS = 67_500_000 // 75% 预警
	fanCritS = 81_000_000 // 90% 严重

	// 压缩机额定寿命 50000h = 180000000s
	cpLifeS = 180_000_000
	cpWarnS = 135_000_000
	cpCritS = 162_000_000

	// 阀门类额定寿命 1000000 次
	valveLifeN = 1_000_000
	valveWarnN = 750_000
	valveCritN = 900_000
)

// ============================================================
// 数据结构定义
// ============================================================

// EventMeta 事件元数据，对应 signal-parsed 消息头。
type EventMeta struct {
	SchemaVersion string `json:"schema_version"`
	LineID        string `json:"line_id"`
	TrainID       string `json:"train_id"`
	CarriageID    int    `json:"carriage_id"`
	DeviceID      string `json:"device_id"`
	EventTimeText string `json:"event_time_text"`
	// EventTimeValid 由 nb67-parser.yaml 判定（Bug C 修复后含 hour/minute/second 全校验）。
	// 下游写入器（event-writer/storage-writer/lifecycle-writer）据此决定是否回退 ingest_time。
	EventTimeValid bool   `json:"event_time_valid"`
	IngestTime     string `json:"ingest_time"`
	ProcessTime    string `json:"process_time"`
}

// PredictHit 预警命中条目（基于算法规则）。
type PredictHit struct {
	Code                     string `json:"code"`                                 // e.g. "HVAC301"
	Name                     string `json:"name"`                                 // 中文名称
	Severity                 int    `json:"severity"`                             // 3=高 2=中 1=低
	TriggerConditionSnapshot string `json:"trigger_condition_snapshot,omitempty"` // 触发时刻配置快照，防止历史记录随配置变更而变
}

// AlarmHit 原生故障位命中条目（直接映射 binary 故障位）。
type AlarmHit struct {
	Code  string `json:"code"`  // e.g. "blpflt_comp_u11"
	Name  string `json:"name"`  // 中文名称
	Level int    `json:"level"` // 1=严重 2=一般
}

// LifeHit 部件寿命预警条目。
type LifeHit struct {
	Code     string `json:"code"`     // 部件码
	Name     string `json:"name"`     // 中文名称
	Severity int    `json:"severity"` // 3=高 2=中
	Value    int64  `json:"value"`    // 当前累计值（秒或次）
	Limit    int64  `json:"limit"`    // 额定寿命（秒或次）
}

// SubEvent 单个子事件，用于输出到对应 topic。
type SubEvent struct {
	EventMeta EventMeta   `json:"event_meta"`
	Hits      interface{} `json:"hits"`   // []PredictHit | []AlarmHit | []LifeHit
	Source    string      `json:"source"` // 来源标识
}

// EventOutput 处理器输出的聚合事件包，YAML fan_out 分拣用。
type EventOutput struct {
	PredictEvent SubEvent `json:"predict_event"`
	AlarmEvent   SubEvent `json:"alarm_event"`
	LifeEvent    SubEvent `json:"life_event"`
}

// parsedInput 是从上游 signal-parsed 消息解析的输入结构。
// 仅保留事件构建所需字段，Raw 字段来自 nb67_parser 输出的 raw 对象。
type parsedInput struct {
	LineID         json.Number    `json:"line_id"`
	TrainID        json.Number    `json:"train_id"`
	CarriageID     json.Number    `json:"carriage_id"`
	DeviceID       string         `json:"device_id"`
	EventTimeText  string         `json:"event_time_text"`
	EventTimeValid bool           `json:"event_time_valid"`
	IngestTime     string         `json:"ingest_time"`
	Raw            map[string]any `json:"raw"`
}

// ============================================================
// 处理器注册与实现
// ============================================================

// ============================================================
// 状态管理：规则计时器
// ============================================================

type ruleState struct {
	firstSeen time.Time
	triggered bool // true 表示已达到持续时间要求，进入"已激活"状态
}

// NB67EventProcessor 增加了状态表，用于判断规则持续时间。
type NB67EventProcessor struct {
	// key: DeviceID + RuleCode
	// value: *ruleState
	states  sync.Map
	logger  *service.Logger
	runtime string // ENV "RUNTIME": "DEV" | "PRD"
	// prevPredictHadHits tracks whether the previous predict message for a device
	// contained any hits. When hits transition from non-empty to empty, we must
	// still emit one message with an empty list so the ground-reporter can clear
	// any active alarms via AlarmTracker.Diff.
	// key: deviceID (string) → bool
	prevPredictHadHits sync.Map
}

// checkRule 判定规则是否满足持续时间要求，使用消息中的 currentTime。
func (p *NB67EventProcessor) checkRule(condition bool, duration time.Duration, deviceID string, ruleCode string, currentTime time.Time) bool {
	key := deviceID + ":" + ruleCode
	if !condition {
		p.states.Delete(key)
		return false
	}

	val, loaded := p.states.LoadOrStore(key, &ruleState{firstSeen: currentTime})
	state := val.(*ruleState)

	if !loaded {
		return duration <= 0
	}

	// 计算消息间的时间差，而不是系统运行时间差
	return currentTime.Sub(state.firstSeen) >= duration
}

// checkRuleWithClear 支持滞回（hysteresis）的规则判定：
//   - triggerCondition=true 且持续 duration 后激活（与 checkRule 相同）
//   - 激活后，只要 keepCondition=true（通常 value > clear_value）就保持激活
//   - keepCondition=false 时立即清除（value ≤ clear_value）
//
// 当 clearThreshold == triggerThreshold 时，keepCondition = triggerCondition，退化为 checkRule 的原有行为。
func (p *NB67EventProcessor) checkRuleWithClear(triggerCondition bool, keepCondition bool, duration time.Duration, deviceID string, ruleCode string, currentTime time.Time) bool {
	key := deviceID + ":" + ruleCode

	valRaw, exists := p.states.Load(key)
	if exists {
		state := valRaw.(*ruleState)
		if state.triggered {
			// 已激活：keepCondition 决定是否保持
			if !keepCondition {
				p.states.Delete(key)
				p.logger.Debugf("[ALARM-CLEAR] %s: cleared (keepCondition=false)", ruleCode)
				return false
			}
			return true
		}
		// 计时中但未激活
		if !triggerCondition {
			p.states.Delete(key)
			return false
		}
		fired := currentTime.Sub(state.firstSeen) >= duration
		if fired {
			state.triggered = true
			p.logger.Debugf("[ALARM-TRIGGER] %s: triggered after %v (threshold %v)", ruleCode, duration, currentTime.Sub(state.firstSeen))
		}
		return fired
	}

	// 无既有状态
	if !triggerCondition {
		return false
	}
	newState := &ruleState{firstSeen: currentTime, triggered: false}
	if duration <= 0 {
		newState.triggered = true
		p.logger.Debugf("[ALARM-IMMEDIATE] %s: triggered immediately (duration<=0)", ruleCode)
	} else {
		p.logger.Debugf("[ALARM-START] %s: monitoring (duration=%v)", ruleCode, duration)
	}
	p.states.Store(key, newState)
	return duration <= 0
}

// init 在程序启动时自动注册处理器。
func init() {
	err := service.RegisterProcessor(
		"nb67_event_builder",
		service.NewConfigSpec().
			Summary("NB67 空调事件构建处理器").
			Description("支持状态化持续时间判定的事件构建器"),
		func(conf *service.ParsedConfig, mgr *service.Resources) (service.Processor, error) {
			rt := os.Getenv("RUNTIME")
			if rt == "" {
				rt = "PRD"
			}
			ensureConfigStore(mgr.Logger())
			return &NB67EventProcessor{
				logger:  mgr.Logger(),
				runtime: rt,
			}, nil
		},
	)
	if err != nil {
		panic(fmt.Sprintf("注册 nb67_event_builder 处理器失败: %v", err))
	}
}

// Process 实现 service.Processor 接口，处理每条输入消息。
func (p *NB67EventProcessor) Process(ctx context.Context, msg *service.Message) (service.MessageBatch, error) {
	// 解析输入 JSON
	rawBytes, err := msg.AsBytes()
	if err != nil {
		// 读取字节失败，通常是内核或内存极端情况，直接丢弃
		return service.MessageBatch{}, nil
	}

	var input parsedInput
	if err := json.Unmarshal(rawBytes, &input); err != nil {
		// 【关键修复】：如果此 Processor 报错返回 error，Benthos 会透传原始巨大消息。
		// 为了保护下游 Topic，我们此处拦截错误并返回空 Batch 丢弃它。
		p.logger.Errorf("NB67处理器解析 JSON 失败（可能是非标准数据），已拦截丢弃，防止污染 Topic: %v", err)
		return service.MessageBatch{}, nil
	}

	// 极其重要：如果 raw 为空，说明不是合法的 signal-parsed 数据，丢弃
	if len(input.Raw) == 0 {
		return service.MessageBatch{}, nil
	}

	// 构建事件元数据
	meta := EventMeta{
		SchemaVersion:  "nb67.event",
		LineID:         input.LineID.String(),
		TrainID:        input.TrainID.String(),
		CarriageID:     func() int { n, _ := input.CarriageID.Int64(); return int(n) }(),
		DeviceID:       input.DeviceID,
		EventTimeText:  input.EventTimeText,
		EventTimeValid: input.EventTimeValid,
		IngestTime:     input.IngestTime,
		ProcessTime:    time.Now().In(beijingLoc).Format(time.RFC3339Nano),
	}

	// 【核心修复】：根据 RUNTIME 环境选择时间源
	var currentTime time.Time
	var parseErr error
	if p.runtime == "DEV" {
		currentTime, parseErr = time.Parse(time.RFC3339, input.IngestTime)
	} else {
		eventTimeFormats := []string{
			"2006-01-02 15:04:05",
			"2006-1-2 15:4:5",
			"2006-01-2 15:04:5",
			"2006-1-02 15:4:05",
		}
		for _, format := range eventTimeFormats {
			currentTime, parseErr = time.Parse(format, input.EventTimeText)
			if parseErr == nil {
				break
			}
		}
	}

	if parseErr != nil {
		p.logger.Warnf("解析时间源失败 [Mode:%s, Ingest:%s, Event:%s]: %v", p.runtime, input.IngestTime, input.EventTimeText, parseErr)
		currentTime = time.Now()
	}

	// 构建三类事件命中列表
	cidInt := func() int { n, _ := input.CarriageID.Int64(); return int(n) }()
	predictHits := p.buildPredictHits(input.Raw, cidInt, input.DeviceID, currentTime)
	alarmHits := buildAlarmHits(input.Raw, cidInt)
	lifeHits := buildLifeHits(input.Raw, cidInt)

	// 判断 predict 是否需要透传：若上一帧有 predict 命中而本帧清空，需要保留一帧空列表
	// 以便 ground-reporter 的 AlarmTracker.Diff 能够发出"预警结束"事件。
	prevHad, _ := p.prevPredictHadHits.Load(input.DeviceID)
	prevHadHits := prevHad != nil && prevHad.(bool)
	if len(predictHits) > 0 {
		p.prevPredictHadHits.Store(input.DeviceID, true)
		p.logger.Debugf("[PREDICT-STATE] %s: %d active hits", input.DeviceID, len(predictHits))
	} else if prevHadHits {
		// 本帧清空且上帧有命中：发送一次空列表后重置标志
		p.prevPredictHadHits.Store(input.DeviceID, false)
		p.logger.Debugf("[PREDICT-STATE] %s: transitioning from previous hits to 0 (sending empty list to trigger clear)", input.DeviceID)
	}
	needPredict := len(predictHits) > 0 || prevHadHits

	// 如果三类命中均无需输出，直接拦截
	if !needPredict && len(alarmHits) == 0 && len(lifeHits) == 0 {
		return service.MessageBatch{}, nil
	}

	// 聚合输出
	output := EventOutput{
		PredictEvent: SubEvent{EventMeta: meta, Hits: predictHits, Source: "connect-rule-v2"},
		AlarmEvent:   SubEvent{EventMeta: meta, Hits: alarmHits, Source: "raw-fault-bit"},
		LifeEvent:    SubEvent{EventMeta: meta, Hits: lifeHits, Source: "part-life-v2"},
	}

	outBytes, err := json.Marshal(output)
	if err != nil {
		p.logger.Errorf("NB67处理器序列化 JSON 失败: %v", err)
		return service.MessageBatch{}, nil
	}

	// 替换原始消息内容
	outMsg := msg.Copy()
	outMsg.SetBytes(outBytes)
	return service.MessageBatch{outMsg}, nil
}

// Close 实现 service.Processor 接口。
func (p *NB67EventProcessor) Close(ctx context.Context) error {
	return nil
}

// ============================================================
// 辅助函数：从 raw map 安全读取数值
// ============================================================

// rawInt 从 raw map 读取整数字段，找不到或类型不匹配时返回 0。
func rawInt(raw map[string]any, key string) int64 {
	v, ok := raw[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	}
	return 0
}

// rawBool 从 raw map 读取布尔字段，找不到或类型不匹配时返回 false。
func rawBool(raw map[string]any, key string) bool {
	v, ok := raw[key]
	if !ok {
		return false
	}
	b, _ := v.(bool)
	return b
}

// hvacCode 生成 HVAC 预警码字符串，格式：HVAC{carriageID*100+seq}
func hvacCode(hvacBase int, seq int) string {
	return fmt.Sprintf("HVAC%d", hvacBase+seq)
}

// ============================================================
// buildPredictHits：HVAC 算法预警（HVAC_01 ~ HVAC_26）
// 来源：NB67 空调预警码表 20240802
// 官方码格式：HVAC{carriage_id * 100 + seq}
//   carriage_id 1~6 对应 Tc1/Mp1/M1/M2/Mp2/Tc2
//   seq 01~26 对应 26 种预警类型
// ============================================================

// buildPredictHits 全量实现 HVAC101 ~ HVAC126 业务逻辑
func (p *NB67EventProcessor) buildPredictHits(raw map[string]any, carriageID int, deviceID string, currentTime time.Time) []PredictHit {
	hits := make([]PredictHit, 0)
	if len(raw) == 0 {
		return hits
	}
	base := carriageID * 100

	// 辅助变量
	wModeU1 := rawInt(raw, "WmodeU1")
	wModeU2 := rawInt(raw, "WmodeU2")

	// ================================================================
	// 1. 冷媒泄漏预警 (HVAC_01 ~ HVAC_04)
	// 阈值和持续时间从数据库热加载（WARN_REFRIGERANT_LEAK_COOLING / _VENT）
	// ================================================================
	// 预先读取可配置阈值（两个条件分别独立配置）
	suckpThresh := csRawThreshold("WARN_REFRIGERANT_LEAK_COOLING", 20) // 2.0bar×10
	coolingDur := csDuration("WARN_REFRIGERANT_LEAK_COOLING", 5*time.Minute)
	highpThresh := csRawThreshold("WARN_REFRIGERANT_LEAK_VENT", 50) // 5.0bar×10
	ventDur := csDuration("WARN_REFRIGERANT_LEAK_VENT", 15*time.Minute)

	checkRefLeak := func(mode int64, hvacSeq int, name string) {
		code := hvacCode(base, hvacSeq)
		uIdx := (hvacSeq + 1) / 2
		sIdx := (hvacSeq+1)%2 + 1
		fcp := rawInt(raw, fmt.Sprintf("FCpU%d%d", uIdx, sIdx))
		suckp := rawInt(raw, fmt.Sprintf("SuckpU%d%d", uIdx, sIdx))
		highp := rawInt(raw, fmt.Sprintf("HighpressU%d%d", uIdx, sIdx))

		// 条件1：制冷模式 + 频率>30Hz + 吸气<suckpThresh -> 持续coolingDur
		isCoolingLeak := (mode == 2 || mode == 3) && fcp > 300 && suckp < suckpThresh
		if p.checkRule(isCoolingLeak, coolingDur, deviceID, code+"_c", currentTime) {
			hits = append(hits, PredictHit{Code: code, Name: name, Severity: 3,
				TriggerConditionSnapshot: csTriggerConditionText("WARN_REFRIGERANT_LEAK_COOLING")})
			return
		}
		// 条件2：通风模式 + 高压<highpThresh -> 持续ventDur
		isVentLeak := mode == 1 && highp < highpThresh
		if p.checkRule(isVentLeak, ventDur, deviceID, code+"_v", currentTime) {
			hits = append(hits, PredictHit{Code: code, Name: name, Severity: 3,
				TriggerConditionSnapshot: csTriggerConditionText("WARN_REFRIGERANT_LEAK_VENT")})
		}
	}
	checkRefLeak(wModeU1, 1, "机组1系统1冷媒泄漏预警")
	checkRefLeak(wModeU1, 2, "机组1系统2冷媒泄漏预警")
	checkRefLeak(wModeU2, 3, "机组2系统1冷媒泄漏预警")
	checkRefLeak(wModeU2, 4, "机组2系统2冷媒泄漏预警")

	// ================================================================
	// 2. 制冷系统预警 (HVAC_05 ~ HVAC_06)
	// 电流差阈值和持续时间从数据库热加载（WARN_COOLING_SYSTEM）
	// ================================================================
	cpSysCurrentDiffThresh := csRawThreshold("WARN_COOLING_SYSTEM", 20) // 2.0A×10
	cpSysDur := csDuration("WARN_COOLING_SYSTEM", 3*time.Minute)

	checkCpSys := func(uIdx int, name string) {
		code := hvacCode(base, uIdx+4)
		f1 := rawInt(raw, fmt.Sprintf("FCpU%d1", uIdx))
		f2 := rawInt(raw, fmt.Sprintf("FCpU%d2", uIdx))
		i1 := rawInt(raw, fmt.Sprintf("ICpU%d1", uIdx))
		i2 := rawInt(raw, fmt.Sprintf("ICpU%d2", uIdx))
		sp1 := rawInt(raw, fmt.Sprintf("SpU%d1", uIdx))
		sp2 := rawInt(raw, fmt.Sprintf("SpU%d2", uIdx))

		// 条件1：同频电流差 > cpSysCurrentDiffThresh -> 持续cpSysDur
		isCurrentDiff := f1 == f2 && f1 > 0 && (i1-i2 > cpSysCurrentDiffThresh || i1-i2 < -cpSysCurrentDiffThresh)
		if p.checkRule(isCurrentDiff, cpSysDur, deviceID, code+"_i", currentTime) {
			fmt.Printf("[制冷系统预警] %s 触发条件1（电流差异常）\n", name)
			fmt.Printf("  - 判断值: %s\n", isCurrentDiff)
			fmt.Printf("  - 参数值: f1=%d, f2=%d, i1=%d, i2=%d, 电流差=%d, 阈值=%d\n", f1, f2, i1, i2, i1-i2, cpSysCurrentDiffThresh)
			fmt.Printf("  - 触发条件: f1==f2(%v) && f1>0(%v) && |i1-i2|>阈值(%v)\n", f1 == f2, f1 > 0, i1-i2 > cpSysCurrentDiffThresh || i1-i2 < -cpSysCurrentDiffThresh)
			hits = append(hits, PredictHit{Code: code, Name: name, Severity: 3,
				TriggerConditionSnapshot: csTriggerConditionText("WARN_COOLING_SYSTEM")})
			return
		}
		// 条件2：运行 > 5min 后，过热度异常 -> 持续10分钟（阈值维持硬编码，PHM文档 ±20℃/±8℃）
		isRunning := f1 > 0
		hasBeenRunning := p.checkRule(isRunning, 5*time.Minute, deviceID, code+"_run", currentTime)
		isSpErr := hasBeenRunning && (sp1 > 200 || sp1 < -80)
		if p.checkRule(isSpErr, 10*time.Minute, deviceID, code+"_sp", currentTime) {
			fmt.Printf("[制冷系统预警] %s 触发条件2（过热度异常）\n", name)
			fmt.Printf("  - 设备ID: %s\n", deviceID)
			fmt.Printf("  - 参数值: f1=%d, sp1=%d\n", f1, sp1)
			fmt.Printf("  - 触发条件: 已运行5分钟(%v) && 过热度异常(%v)\n", hasBeenRunning, isSpErr)
			fmt.Printf("  - 过热度范围检查: sp1>200(%v), sp1 < -80(%v)\n", sp1 > 200, sp1 < -80)
			hits = append(hits, PredictHit{Code: code, Name: name, Severity: 3})
			return
		}
		// 条件2扩展：机组2过热度异常检查
		isRunning2 := f2 > 0
		hasBeenRunning2 := p.checkRule(isRunning2, 5*time.Minute, deviceID, code+"_run2", currentTime)
		isSpErr2 := hasBeenRunning2 && (sp2 > 200 || sp2 < -80)
		if p.checkRule(isSpErr2, 10*time.Minute, deviceID, code+"_sp2", currentTime) {
			fmt.Printf("[制冷系统预警] %s 触发条件2（过热度异常-机组2）\n", name)
			fmt.Printf("  - 设备ID: %s\n", deviceID)
			fmt.Printf("  - 参数值: f2=%d, sp2=%d\n", f2, sp2)
			fmt.Printf("  - 触发条件: 已运行5分钟(%v) && 过热度异常(%v)\n", hasBeenRunning2, isSpErr2)
			fmt.Printf("  - 过热度范围检查: sp2>200(%v), sp2 < -80(%v)\n", sp2 > 200, sp2 < -80)
			hits = append(hits, PredictHit{Code: code, Name: name, Severity: 3})
		}
	}
	checkCpSys(1, "机组1制冷系统预警")
	checkCpSys(2, "机组2制冷系统预警")

	// ================================================================
	// 3. 传感器预警 (HVAC_07 ~ HVAC_11)
	// ================================================================
	// HVAC_07/08: 温差 > 8℃ -> 持续 8 分钟 (WARN_TEMP_SENSOR)
	ventModel := wModeU1 == 1 && wModeU2 == 1
	tempThresh := csRawThreshold("WARN_TEMP_SENSOR", 80)
	tempDur := csDuration("WARN_TEMP_SENSOR", 8*time.Minute)
	ventilationLong := p.checkRule(ventModel, tempDur, deviceID, "ventilation_long", currentTime)
	fasU1, fasU2 := rawInt(raw, "FasU1"), rawInt(raw, "FasU2")
	fasValid := fasU1 != 32767 && fasU2 != 32767
	fasCondition := ventilationLong && fasValid && (fasU1-fasU2 > tempThresh || fasU1-fasU2 < -tempThresh)
	if p.checkRule(fasCondition, tempDur, deviceID, hvacCode(base, 7), currentTime) {
		hits = append(hits, PredictHit{Code: hvacCode(base, 7), Name: "新风温度传感器预警", Severity: 3,
			TriggerConditionSnapshot: csTriggerConditionText("WARN_TEMP_SENSOR")})
	}
	rasU1, rasU2 := rawInt(raw, "RasU1"), rawInt(raw, "RasU2")
	rasValid := rasU1 != 32767 && rasU2 != 32767
	rasCondition := ventilationLong && rasValid && (rasU1-rasU2 > tempThresh || rasU1-rasU2 < -tempThresh)
	if p.checkRule(rasCondition, tempDur, deviceID, hvacCode(base, 8), currentTime) {
		hits = append(hits, PredictHit{Code: hvacCode(base, 8), Name: "回风温度传感器预警", Severity: 3,
			TriggerConditionSnapshot: csTriggerConditionText("WARN_TEMP_SENSOR")})
	}
	tveh1, tveh2 := rawInt(raw, "Tveh1"), rawInt(raw, "Tveh2")
	tvehValid := tveh1 != 32767 && tveh2 != 32767
	tvehCondition := ventilationLong && tvehValid && (tveh1-tveh2 > tempThresh || tveh1-tveh2 < -tempThresh)
	if p.checkRule(tvehCondition, tempDur, deviceID, hvacCode(base, 76), currentTime) {
		hits = append(hits, PredictHit{Code: hvacCode(base, 76), Name: "车厢温度传感器预警", Severity: 3,
			TriggerConditionSnapshot: csTriggerConditionText("WARN_TEMP_SENSOR")})
	}

	// HVAC_09: 车厢超温预警（PHM 文档条件）
	// 条件1：制冷系统核心部件无故障（PHM "制冷系统无故障"）
	//   注意：仅检查压缩机/变频器/高低压等制冷核心故障，传感器故障（如 BfltDiffpresU）不纳入，
	//   避免因 mock 帧 presdiff=32767 误触发传感器故障位而永久屏蔽超温预警。
	// 条件2：运行于强冷(2)/弱冷(3)模式，持续 > min_cooling_runtime_s
	// 条件3：回风温度(RasU) > 制冷目标温度 + delta，持续 > duration_seconds
	coolingSystemFaulty :=
		rawBool(raw, "BfltPowersupplyU1") || rawBool(raw, "BfltPowersupplyU2") ||
			rawBool(raw, "BlpfltCompU11") || rawBool(raw, "BlpfltCompU12") ||
			rawBool(raw, "BlpfltCompU21") || rawBool(raw, "BlpfltCompU22") ||
			rawBool(raw, "BscfltCompU11") || rawBool(raw, "BscfltCompU12") ||
			rawBool(raw, "BscfltCompU21") || rawBool(raw, "BscfltCompU22") ||
			rawBool(raw, "BfltHighpresU11") || rawBool(raw, "BfltHighpresU12") ||
			rawBool(raw, "BfltHighpresU21") || rawBool(raw, "BfltHighpresU22") ||
			rawBool(raw, "BfltLowpresU11") || rawBool(raw, "BfltLowpresU12") ||
			rawBool(raw, "BfltLowpresU21") || rawBool(raw, "BfltLowpresU22") ||
			rawBool(raw, "BfltVfdU11") || rawBool(raw, "BfltVfdU12") ||
			rawBool(raw, "BfltVfdU21") || rawBool(raw, "BfltVfdU22")
	overtempDelta := csRawThreshold("WARN_CABIN_OVERHEAT", 40) // 允许回风超出客室的阈值 (原始单位)
	overtempDur := csDuration("WARN_CABIN_OVERHEAT", 2*time.Minute)
	coolingPrecondDur := csCoolingPreconditionDur("WARN_CABIN_OVERHEAT", 20*time.Minute)
	coolingNormal := !coolingSystemFaulty && (wModeU1 == 2 || wModeU1 == 3 || wModeU2 == 2 || wModeU2 == 3)
	sysRunningLong := p.checkRule(coolingNormal, coolingPrecondDur, deviceID, "cooling_normal_20", currentTime)
	ticTemp := rawInt(raw, "Tic")             // 客室温度 (实时)
	overtempThresh := ticTemp + overtempDelta // 动态阈值 = 客室温度 + 允许超出量
	isOvertemp := sysRunningLong && (rawInt(raw, "RasSys") > overtempThresh)
	if p.checkRule(isOvertemp, overtempDur, deviceID, hvacCode(base, 9), currentTime) {
		hits = append(hits, PredictHit{Code: hvacCode(base, 9), Name: "车厢温度超温预警", Severity: 3,
			TriggerConditionSnapshot: csTriggerConditionText("WARN_CABIN_OVERHEAT")})
	}

	// HVAC_10/11: 压差超阈值 -> 持续 30 分钟 (WARN_FILTER_CLOG)
	filterThresh := csRawThreshold("WARN_FILTER_CLOG", 3000)
	filterDur := csDuration("WARN_FILTER_CLOG", 30*time.Minute)
	if p.checkRule(rawBool(raw, "CfbkEfU11") && rawInt(raw, "PresdiffU1") > filterThresh && rawInt(raw, "PresdiffU1") < 32767, filterDur, deviceID, hvacCode(base, 10), currentTime) {
		hits = append(hits, PredictHit{Code: hvacCode(base, 10), Name: "机组1滤网脏堵预警", Severity: 2,
			TriggerConditionSnapshot: csTriggerConditionText("WARN_FILTER_CLOG")})
	}
	if p.checkRule(rawBool(raw, "CfbkEfU21") && rawInt(raw, "PresdiffU2") > filterThresh && rawInt(raw, "PresdiffU2") < 32767, filterDur, deviceID, hvacCode(base, 11), currentTime) {
		hits = append(hits, PredictHit{Code: hvacCode(base, 11), Name: "机组2滤网脏堵预警", Severity: 2,
			TriggerConditionSnapshot: csTriggerConditionText("WARN_FILTER_CLOG")})
	}

	// ================================================================
	// 4. 风机电流预警 (HVAC_12 ~ HVAC_20) -> 持续时间由 DB 配置
	// ================================================================
	efThresh := csRawThreshold("WARN_EF_CURRENT", 18)     // 通风机 PHM 3.6
	cfThresh := csRawThreshold("WARN_CF_CURRENT", 23)     // 冷凝风机 PHM 3.7
	exufThresh := csRawThreshold("WARN_EXUF_CURRENT", 23) // 废排风机 PHM 3.8
	efClearThresh := csClearThreshold("WARN_EF_CURRENT", efThresh)
	cfClearThresh := csClearThreshold("WARN_CF_CURRENT", cfThresh)
	exufClearThresh := csClearThreshold("WARN_EXUF_CURRENT", exufThresh)

	checkFanI := func(cfbkField, iField string, threshold, clearThreshold int64, seq int, name, warnCode string) {
		code := hvacCode(base, seq)
		isOverI := rawBool(raw, cfbkField) && rawInt(raw, iField) > threshold
		// 保持激活：电流仍高于消除阈值（滞回区间内不反复触发/消除）
		keepI := rawBool(raw, cfbkField) && rawInt(raw, iField) > clearThreshold
		warnDur := csDuration(warnCode, 10*time.Minute)
		if p.checkRuleWithClear(isOverI, keepI, warnDur, deviceID, code, currentTime) {
			hits = append(hits, PredictHit{Code: code, Name: name, Severity: 3,
				TriggerConditionSnapshot: csTriggerConditionText(warnCode)})
		}
	}
	checkFanI("CfbkEfU11", "IEfU11", efThresh, efClearThresh, 12, "机组1通风机1电流预警", "WARN_EF_CURRENT")
	checkFanI("CfbkEfU11", "IEfU12", efThresh, efClearThresh, 13, "机组1通风机2电流预警", "WARN_EF_CURRENT")
	checkFanI("CfbkEfU21", "IEfU21", efThresh, efClearThresh, 14, "机组2通风机1电流预警", "WARN_EF_CURRENT")
	checkFanI("CfbkEfU21", "IEfU22", efThresh, efClearThresh, 15, "机组2通风机2电流预警", "WARN_EF_CURRENT")
	checkFanI("CfbkCfU11", "ICfU11", cfThresh, cfClearThresh, 16, "机组1冷凝风机1电流预警", "WARN_CF_CURRENT")
	checkFanI("CfbkCfU11", "ICfU12", cfThresh, cfClearThresh, 17, "机组1冷凝风机2电流预警", "WARN_CF_CURRENT")
	checkFanI("CfbkCfU21", "ICfU21", cfThresh, cfClearThresh, 18, "机组2冷凝风机1电流预警", "WARN_CF_CURRENT")
	checkFanI("CfbkCfU21", "ICfU22", cfThresh, cfClearThresh, 19, "机组2冷凝风机2电流预警", "WARN_CF_CURRENT")
	checkFanI("CfbkExufan", "IExufan", exufThresh, exufClearThresh, 20, "废排风机电流预警", "WARN_EXUF_CURRENT")

	// ================================================================
	// 5. 压缩机电流预警 (HVAC_21 ~ HVAC_24) -> 新风 < 35℃ 且 I 超阈值 -> 持续时间由 DB 配置
	// ================================================================
	cpThresh := csRawThreshold("WARN_CP_CURRENT", 180) // 18A × 10
	cpDur := csDuration("WARN_CP_CURRENT", 10*time.Minute)
	cpClearThresh := csClearThreshold("WARN_CP_CURRENT", cpThresh)

	checkCpI := func(fasField, iField string, seq int, name string) {
		code := hvacCode(base, seq)
		isOverI := rawInt(raw, fasField) < 350 && rawInt(raw, iField) > cpThresh
		keepI := rawInt(raw, fasField) < 350 && rawInt(raw, iField) > cpClearThresh
		if p.checkRuleWithClear(isOverI, keepI, cpDur, deviceID, code, currentTime) {
			hits = append(hits, PredictHit{Code: code, Name: name, Severity: 3,
				TriggerConditionSnapshot: csTriggerConditionText("WARN_CP_CURRENT")})
		}
	}
	checkCpI("FasU1", "ICpU11", 21, "机组1压缩机1电流预警")
	checkCpI("FasU1", "ICpU12", 22, "机组1压缩机2电流预警")
	checkCpI("FasU2", "ICpU21", 23, "机组2压缩机1电流预警")
	checkCpI("FasU2", "ICpU22", 24, "机组2压缩机2电流预警")

	// ================================================================
	// 6. 空气质量预警 (HVAC_125 ~ HVAC_126) — 阈值与持续时间由 DB 配置
	// ================================================================
	co2Thresh := csRawThreshold("WARN_AQ_CO2", 4500)
	co2Dur := csDuration("WARN_AQ_CO2", 15*time.Minute)
	pm25Thresh := csRawThreshold("WARN_AQ_PM25", 75)
	pm10Thresh := csRawThreshold("WARN_AQ_PM10", 150)
	tvocThresh := csRawThreshold("WARN_AQ_TVOC", 600)
	pmDur := csDuration("WARN_AQ_PM25", 20*time.Minute)

	checkAQ := func(uIdx int, name string) {
		code := hvacCode(base, uIdx+24) // uIdx=1→125, uIdx=2→126
		fanRunning := rawBool(raw, fmt.Sprintf("CfbkEfU%d1", uIdx))
		hasBeenRunning := p.checkRule(fanRunning, 20*time.Minute, deviceID, code+"_fanrun", currentTime)

		co2Err := hasBeenRunning && rawInt(raw, fmt.Sprintf("AqCo2U%d", uIdx)) > co2Thresh
		co2Hit := p.checkRule(co2Err, co2Dur, deviceID, code+"_co2", currentTime)

		pmTvocErr := hasBeenRunning && (rawInt(raw, fmt.Sprintf("AqPm25U%d", uIdx)) > pm25Thresh ||
			rawInt(raw, fmt.Sprintf("AqPm10U%d", uIdx)) > pm10Thresh ||
			rawInt(raw, fmt.Sprintf("AqTvocU%d", uIdx)) > tvocThresh)
		pmTvocHit := p.checkRule(pmTvocErr, pmDur, deviceID, code+"_pmtvoc", currentTime)

		if co2Hit || pmTvocHit {
			snapshot := csTriggerConditionText("WARN_AQ_CO2")
			if !co2Hit {
				snapshot = csTriggerConditionText("WARN_AQ_PM25")
			}
			hits = append(hits, PredictHit{Code: code, Name: name, Severity: 3,
				TriggerConditionSnapshot: snapshot})
		}
	}
	checkAQ(1, "机组1空气质量预警")
	checkAQ(2, "机组2空气质量预警")

	return hits
}

// ============================================================
// buildAlarmHits：原生故障位告警
// 按 alertcode_v2.xlsx（PHM v2）规范上报 HVAC127-HVAC175 平台码。
// 内部码格式：HVAC{carriageID*100+seq}，seq 27-75。
// ============================================================

func buildAlarmHits(raw map[string]any, carriageID int) []AlarmHit {
	hits := make([]AlarmHit, 0)
	if len(raw) == 0 {
		return hits
	}
	base := carriageID * 100

	// 辅助：单信号 → HVAC 码
	check := func(field string, seq int, name string, level int) {
		if rawBool(raw, field) {
			hits = append(hits, AlarmHit{
				Code:  fmt.Sprintf("HVAC%d", base+seq),
				Name:  name,
				Level: level,
			})
		}
	}
	// 辅助：OR 双信号 → 同一 HVAC 码（PHM 合并为单码）
	checkOr := func(f1, f2 string, seq int, name string, level int) {
		if rawBool(raw, f1) || rawBool(raw, f2) {
			hits = append(hits, AlarmHit{
				Code:  fmt.Sprintf("HVAC%d", base+seq),
				Name:  name,
				Level: level,
			})
		}
	}

	// ================================================================
	// 机组1（U1）告警 — HVAC{base+27} ~ HVAC{base+46}
	// ================================================================
	check("BocfltEfU11", 27, "通风机1-1过流故障", 1)
	check("BocfltEfU12", 28, "通风机1-2过流故障", 1)
	check("BocfltCfU11", 29, "冷凝风机1-1过流故障", 2)
	check("BocfltCfU12", 30, "冷凝风机1-2过流故障", 2)
	check("BfltVfdU11", 31, "变频器1-1故障", 2)
	check("BlpfltCompU11", 32, "压缩机1-1低压故障", 2)
	check("BscfltCompU11", 33, "压缩机1-1高压连锁故障", 2)
	check("BfltVfdU12", 34, "变频器1-2故障", 2)
	check("BlpfltCompU12", 35, "压缩机1-2低压故障", 2)
	check("BscfltCompU12", 36, "压缩机1-2高压连锁故障", 2)
	// PHM HVAC137: bFlt_FAD_U1（U11或U12任一故障均触发）
	checkOr("BfltFadU11", "BfltFadU12", 37, "新风阀U1故障", 3)
	// PHM HVAC138: bFlt_RAD_U1
	checkOr("BfltRadU11", "BfltRadU12", 38, "回风阀U1故障", 3)
	check("BfltApU11", 39, "空气净化U1故障", 3)
	check("BfltExpboardU1", 40, "扩展模块U1故障", 2)
	check("BfltFrstempU1", 41, "新风温度传感器U1故障", 3)
	check("BfltSplytempU11", 42, "送风温度传感器1-1故障", 3)
	check("BfltSplytempU12", 43, "送风温度传感器1-2故障", 3)
	check("BfltRnttempU1", 44, "回风温度传感器U1故障", 3)
	// PHM HVAC145/146: bFlt_DFSTemp（融霜传感器 = 盘管温度传感器 coiltemp）
	check("BfltCoiltempU11", 45, "融霜温度传感器1-1故障", 3)
	check("BfltCoiltempU12", 46, "融霜温度传感器1-2故障", 3)

	// ================================================================
	// 机组2（U2）告警 — HVAC{base+47} ~ HVAC{base+66}
	// ================================================================
	check("BocfltEfU21", 47, "通风机2-1过流故障", 1)
	check("BocfltEfU22", 48, "通风机2-2过流故障", 1)
	check("BocfltCfU21", 49, "冷凝风机2-1过流故障", 2)
	check("BocfltCfU22", 50, "冷凝风机2-2过流故障", 2)
	check("BfltVfdU21", 51, "变频器2-1故障", 2)
	check("BlpfltCompU21", 52, "压缩机2-1低压故障", 2)
	check("BscfltCompU21", 53, "压缩机2-1高压连锁故障", 2)
	check("BfltVfdU22", 54, "变频器2-2故障", 2)
	check("BlpfltCompU22", 55, "压缩机2-2低压故障", 2)
	check("BscfltCompU22", 56, "压缩机2-2高压连锁故障", 2)
	checkOr("BfltFadU21", "BfltFadU22", 57, "新风阀U2故障", 3)
	checkOr("BfltRadU21", "BfltRadU22", 58, "回风阀U2故障", 3)
	check("BfltApU21", 59, "空气净化U2故障", 3)
	check("BfltExpboardU2", 60, "扩展模块U2故障", 2)
	check("BfltFrstempU2", 61, "新风温度传感器U2故障", 3)
	check("BfltSplytempU21", 62, "送风温度传感器2-1故障", 3)
	check("BfltSplytempU22", 63, "送风温度传感器2-2故障", 3)
	check("BfltRnttempU2", 64, "回风温度传感器U2故障", 3)
	check("BfltCoiltempU21", 65, "融霜温度传感器2-1故障", 3)
	check("BfltCoiltempU22", 66, "融霜温度传感器2-2故障", 3)

	// ================================================================
	// 公共告警 — HVAC{base+67} ~ HVAC{base+75}
	// ================================================================
	// PHM HVAC167: bFlt_VehTemp → 车厢温度传感器1（KSY: bflt_vehtemp_u1）
	check("BfltVehtempU1", 67, "车厢温度传感器1故障", 3)
	// PHM HVAC168: bFlt_SeatTemp → 车厢温度传感器2（KSY: bflt_vehtemp_u2）
	check("BfltVehtempU2", 68, "车厢温度传感器2故障", 3)
	// PHM HVAC169: bFlt_EmergIVT → 紧急逆变器
	check("BfltEmergivt", 69, "紧急逆变器故障", 1)
	check("BfltVfdComU11", 70, "变频器1-1通讯故障", 2)
	check("BfltVfdComU12", 71, "变频器1-2通讯故障", 2)
	check("BfltVfdComU21", 72, "变频器2-1通讯故障", 2)
	check("BfltVfdComU22", 73, "变频器2-2通讯故障", 2)
	// PHM HVAC174/175: bMCBFlt_Pwr_U1/U2（KSY: BfltPowersupplyU1/U2）
	check("BfltPowersupplyU1", 74, "机组1供电故障", 1)
	check("BfltPowersupplyU2", 75, "机组2供电故障", 1)

	return hits
}

// ============================================================
// buildLifeHits：部件寿命预警
// part_code = carriage_id * 1000 + 50000 + offset
// 风机时间单位：秒（raw）  阀门单位：次（raw）
// ============================================================

func buildLifeHits(raw map[string]any, carriageID int) []LifeHit {
	// 初始化为空 slice（非 nil），序列化时输出 [] 而非 null
	hits := make([]LifeHit, 0)
	// raw 为空时直接返回，避免误判断
	if len(raw) == 0 {
		return hits
	}
	lifeBase := int64(carriageID*1000 + 50_000)

	// checkFan 检查风机类寿命（单位：秒）
	checkFan := func(field string, offset int, name string) {
		val := rawInt(raw, field)
		code := fmt.Sprintf("%d", lifeBase+int64(offset))
		if val >= fanCritS {
			hits = append(hits, LifeHit{Code: code, Name: name, Severity: 3, Value: val, Limit: fanLifeS})
		} else if val >= fanWarnS {
			hits = append(hits, LifeHit{Code: code, Name: name, Severity: 2, Value: val, Limit: fanLifeS})
		}
	}

	// checkComp 检查压缩机类寿命（单位：秒）
	checkComp := func(field string, offset int, name string) {
		val := rawInt(raw, field)
		code := fmt.Sprintf("%d", lifeBase+int64(offset))
		if val >= cpCritS {
			hits = append(hits, LifeHit{Code: code, Name: name, Severity: 3, Value: val, Limit: cpLifeS})
		} else if val >= cpWarnS {
			hits = append(hits, LifeHit{Code: code, Name: name, Severity: 2, Value: val, Limit: cpLifeS})
		}
	}

	// checkValve 检查阀门类寿命（单位：次）
	checkValve := func(field string, offset int, name string) {
		val := rawInt(raw, field)
		code := fmt.Sprintf("%d", lifeBase+int64(offset))
		if val >= valveCritN {
			hits = append(hits, LifeHit{Code: code, Name: name, Severity: 3, Value: val, Limit: valveLifeN})
		} else if val >= valveWarnN {
			hits = append(hits, LifeHit{Code: code, Name: name, Severity: 2, Value: val, Limit: valveLifeN})
		}
	}

	// 机组1（offset +001~+006）
	checkFan("DwefOpTmU11", 1, "机组1通风机累计运行时间")
	checkFan("DwcfOpTmU11", 2, "机组1冷凝风机累计运行时间")
	checkComp("DwcpOpTmU11", 3, "机组1压缩机1累计运行时间")
	checkComp("DwcpOpTmU12", 4, "机组1压缩机2累计运行时间")
	checkValve("DwfadOpCntU1", 5, "机组1新风阀开关总次数")
	checkValve("DwradOpCntU1", 6, "机组1回风阀开关总次数")

	// 机组2（offset +011~+016）
	checkFan("DwefOpTmU21", 11, "机组2通风机累计运行时间")
	checkFan("DwcfOpTmU21", 12, "机组2冷凝风机累计运行时间")
	checkComp("DwcpOpTmU21", 13, "机组2压缩机1累计运行时间")
	checkComp("DwcpOpTmU22", 14, "机组2压缩机2累计运行时间")
	checkValve("DwfadOpCntU2", 15, "机组2新风阀开关总次数")
	checkValve("DwradOpCntU2", 16, "机组2回风阀开关总次数")

	// 废排（offset +021~+022）
	checkFan("DwexufanOpTm", 21, "废排风机累计运行时间")
	checkValve("DwdmpexuOpCnt", 22, "废排风阀开关总次数")

	return hits
}
