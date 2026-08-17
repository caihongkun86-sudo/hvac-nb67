package main

import (
	"context"
	"encoding/json"
	"log"
	"strconv"
	"strings"
)

// carriageNames maps carriage ID (1-6) to the physical coach designation.
// Order matches the spec: "Tc1,Mp1,M1,M2,Mp2,Tc2 分别对应 1-6 车厢".
var carriageNames = map[int]string{
	1: "Tc1",
	2: "Mp1",
	3: "M1",
	4: "M2",
	5: "Mp2",
	6: "Tc2",
}

func coachName(carriageID int) string {
	if name, ok := carriageNames[carriageID]; ok {
		return name
	}
	return strconv.Itoa(carriageID)
}

// Handle61Alarm processes a signal-alarm message.
// 按当前业务要求，报警/故障不再上报平台，仅记录状态变化日志。
func Handle61Alarm(ctx context.Context, client *PlatformClient, tracker *AlarmTracker, sc *StationCache, cfg Config, data []byte) {
	_ = ctx
	_ = client
	_ = sc
	_ = cfg

	var msg SubEventMsg
	if err := json.Unmarshal(data, &msg); err != nil {
		log.Printf("[WARN] 6.1 alarm: bad json: %v", err)
		return
	}

	var hits []AlarmHit
	if err := json.Unmarshal(msg.Hits, &hits); err != nil || len(hits) == 0 {
		return
	}

	codes := make([]HitCode, len(hits))
	for i, h := range hits {
		codes[i] = HitCode{Code: h.Code, Name: h.Name}
	}

	ts := nowMs()
	diff := tracker.Diff(msg.EventMeta.DeviceID, codes, ts)

	if len(diff.Added) > 0 {
		log.Printf("[ALARM-ADDED] device=%s: %d new alarms", msg.EventMeta.DeviceID, len(diff.Added))
		for _, hit := range diff.Added {
			log.Printf("  - %s (%s)", hit.Code, hit.Name)
		}
	}
	if len(diff.Removed) > 0 {
		log.Printf("[ALARM-REMOVED] device=%s: %d ended alarms", msg.EventMeta.DeviceID, len(diff.Removed))
		for _, hit := range diff.Removed {
			log.Printf("  - %s (ended at %d)", hit.Code, hit.EndTime)
		}
	}
}

// Handle61Predict processes a signal-predict message the same way as alarm.
func Handle61Predict(ctx context.Context, batchSender *BatchSender, tracker *AlarmTracker, sc *StationCache, cfg Config, data []byte) {
	var msg SubEventMsg
	if err := json.Unmarshal(data, &msg); err != nil {
		log.Printf("[WARN] 6.1 predict: bad json: %v", err)
		return
	}

	var hits []PredictHit
	if err := json.Unmarshal(msg.Hits, &hits); err != nil {
		log.Printf("[WARN] 6.1 predict: bad hits json: %v", err)
		return
	}
	// Note: hits may be empty — this is intentional when all alarms have cleared.
	// We must still call tracker.Diff so active codes are removed and "end" events fire.

	// Prefix device key with "predict:" to keep alarm and predict state tables separate.
	deviceKey := "predict:" + msg.EventMeta.DeviceID
	codes := make([]HitCode, len(hits))
	for i, h := range hits {
		codes[i] = HitCode{Code: h.Code, Name: h.Name}
	}

	ts := nowMs()
	diff := tracker.Diff(deviceKey, codes, ts)
	si := sc.Get(msg.EventMeta.DeviceID)

	if len(diff.Added) > 0 {
		log.Printf("[PREDICT-ADDED] device=%s: %d new predictions", msg.EventMeta.DeviceID, len(diff.Added))
		for _, hit := range diff.Added {
			log.Printf("  - %s (%s)", hit.Code, hit.Name)
		}
	}
	if len(diff.Removed) > 0 {
		log.Printf("[PREDICT-REMOVED] device=%s: %d ended predictions", msg.EventMeta.DeviceID, len(diff.Removed))
		for _, hit := range diff.Removed {
			log.Printf("  - %s (ended at %d)", hit.Code, hit.EndTime)
		}
	}

	// GitHub #23 / RET-46 修复（2026-06-12）：
	// 预警消除（diff.Removed）不再向平台上报报文。原因：
	//   - 平台只关心"新出现的预警"，消除事件由本地 hvac.warning_lifecycle.end_time 承载
	//   - 之前消除一并上报，导致平台收到 EndTime>0 的多余 6.1 报文
	// Removed 仍在 [PREDICT-REMOVED] 日志中体现，状态机不受影响。
	var records []Record61
	for _, hit := range diff.Added {
		records = append(records, buildRecord61(cfg, msg.EventMeta, si, "1", hit.Code, hit.StartTime, 0))
	}

	if len(records) == 0 {
		return
	}
	
	// 发送前打印完整请求信息
	log.Printf("[PREDICT-SEND] device=%s, carriage=%d, train=%d, records=%d, url=%s", 
		msg.EventMeta.DeviceID, msg.EventMeta.CarriageID, msg.EventMeta.TrainID, len(records), cfg.FaultRecordURL)
	// 打印完整的 JSON 请求体（仅 DEBUG 模式）
	if cfg.LogLevel == "DEBUG" {
		if jsonData, err := json.MarshalIndent(records, "", "  "); err == nil {
			log.Printf("[PREDICT-BODY] %s", string(jsonData))
		} else {
			log.Printf("[WARN] 6.1 predict: failed to marshal records: %v", err)
		}
	}

	// 使用批量发送器发送
	if batchSender != nil {
		batchSender.Add(records)
	} else {
		// 兼容不启用批量的情况，直接发送
		client := newPlatformClient(cfg)
		if err := client.PostJSON(ctx, cfg.FaultRecordURL, records); err != nil {
			log.Printf("[ERROR] 6.1 predict POST failed: device=%s, carriage=%d, error=%v", 
				msg.EventMeta.DeviceID, msg.EventMeta.CarriageID, err)
		} else {
			log.Printf("[SUCCESS] 6.1 predict POST success: device=%s, carriage=%d, records=%d", 
				msg.EventMeta.DeviceID, msg.EventMeta.CarriageID, len(records))
		}
	}
}

// normalizeCode strips event-processor mode suffixes (_c = cooling, _v = ventilation)
// so the base HVAC code can be matched against alertcodeLocationMap and sent to the platform.
func normalizeCode(code string) string {
	if strings.HasSuffix(code, "_c") || strings.HasSuffix(code, "_v") {
		return code[:len(code)-2]
	}
	return code
}

// buildRecord61 constructs a single 6.1 record.
// endTimeMs == 0 means alarm is still open (endtime = "").
// code is normalized (mode suffixes stripped), then:
//   - Location is looked up using the original internal code (preserves machine info)
//   - Code sent to platform is remapped to alertcode_v2.xlsx scheme (HVAC101-HVAC115)
func buildRecord61(cfg Config, meta EventMeta, si StationInfo, msgType, code string, startMs, endMs int64) Record61 {
	endTime := ""
	if endMs > 0 {
		endTime = strconv.FormatInt(endMs, 10)
	}

	baseCode := normalizeCode(code)
	return Record61{
		MessageType: msgType,
		TrainType:   cfg.TrainType,
		TrainNo:     padTrainNo(meta.TrainID),
		Coach:       coachName(meta.CarriageID),
		Location:    locationByCode(baseCode),
		Code:        platformHvacCode(baseCode),
		Station1:    strconv.Itoa(int(si.CurStation)),
		Station2:    strconv.Itoa(int(si.NextStation)),
		StartTime:   strconv.FormatInt(startMs, 10),
		EndTime:     endTime,
		Subsystem:   cfg.SubsystemCode,
		LineName:    meta.LineID,
	}
}
