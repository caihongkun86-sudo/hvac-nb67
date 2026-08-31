package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/IBM/sarama"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type adapter struct {
	cfg            Config
	pool           *pgxpool.Pool
	lifecycle      *LifecycleWriter
	alarmLifecycle *AlarmLifecycleWriter
}

type saramaMessage struct {
	inner *sarama.ConsumerMessage
}

func (m saramaMessage) GetTopic() string    { return m.inner.Topic }
func (m saramaMessage) GetPartition() int32 { return m.inner.Partition }
func (m saramaMessage) GetOffset() int64    { return m.inner.Offset }
func (m saramaMessage) GetValue() []byte    { return m.inner.Value }

const insertSQL = `
INSERT INTO hvac.fact_raw (
  event_time, ingest_time, process_time,
  line_id, train_id, carriage_id, device_id, frame_no,
  parser_version, quality_code, event_time_valid,
  wmode_u1, wmode_u2,
  f_cp_u11, f_cp_u12, f_cp_u21, f_cp_u22,
  i_cp_u11, i_cp_u12, i_cp_u21, i_cp_u22,
  suckp_u11, suckp_u12, suckp_u21, suckp_u22,
  highpress_u11, highpress_u12, highpress_u21, highpress_u22,
  fas_u1, fas_u2, ras_u1, ras_u2,
  presdiff_u1, presdiff_u2,
  aq_co2_u1, aq_co2_u2, aq_tvoc_u1, aq_tvoc_u2,
  aq_pm2_5_u1, aq_pm2_5_u2, aq_pm10_u1, aq_pm10_u2,
  bocflt_ef_u11, bocflt_ef_u12, bocflt_ef_u21, bocflt_ef_u22,
  bocflt_cf_u11, bocflt_cf_u12, bocflt_cf_u21, bocflt_cf_u22,
  blpflt_comp_u11, blpflt_comp_u12, blpflt_comp_u21, blpflt_comp_u22,
  bscflt_comp_u11, bscflt_comp_u12, bscflt_comp_u21, bscflt_comp_u22,
  bflt_tempover, bflt_diffpres_u1, bflt_diffpres_u2,
  payload_json
) VALUES (
  $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,
  $12,$13,$14,$15,$16,$17,$18,$19,$20,$21,
  $22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,$33,$34,$35,
  $36,$37,$38,$39,$40,$41,$42,$43,$44,$45,$46,$47,$48,$49,$50,
  $51,$52,$53,$54,$55,$56,$57,$58,$59,$60,$61,$62,$63
)
ON CONFLICT (device_id, event_time, ingest_time) DO NOTHING;
`

const insertEventSQL = `
INSERT INTO hvac.fact_event (
    event_time, ingest_time, line_id, train_id, carriage_id, device_id,
    event_type, fault_code, fault_name, severity, payload_json
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (event_time, device_id, fault_code) DO UPDATE 
SET ingest_time = EXCLUDED.ingest_time;
`

func (a *adapter) run(ctx context.Context) error {
	kcfg := sarama.NewConfig()
	kcfg.Version = sarama.V2_6_0_0
	kcfg.Consumer.Return.Errors = true
	kcfg.Consumer.Offsets.Initial = sarama.OffsetNewest
	kcfg.Consumer.Group.Rebalance.Strategy = sarama.BalanceStrategyRange

	group, err := sarama.NewConsumerGroup(a.cfg.KafkaBrokers, a.cfg.KafkaGroup, kcfg)
	if err != nil {
		return fmt.Errorf("create consumer group: %w", err)
	}
	defer group.Close()

	handler := &consumerGroupHandler{adapter: a}
	go func() {
		for err := range group.Errors() {
			log.Printf("[WARN] kafka consumer error: %v", err)
		}
	}()

	for {
		if err := group.Consume(ctx, a.cfg.KafkaTopics, handler); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			log.Printf("[ERROR] consume loop failed: %v", err)
			time.Sleep(2 * time.Second)
		}
		if ctx.Err() != nil {
			return nil
		}
	}
}

type consumerGroupHandler struct {
	adapter *adapter
}

func (h *consumerGroupHandler) Setup(sarama.ConsumerGroupSession) error   { return nil }
func (h *consumerGroupHandler) Cleanup(sarama.ConsumerGroupSession) error { return nil }

func (h *consumerGroupHandler) ConsumeClaim(sess sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	batch := make([]pendingMessage, 0, h.adapter.cfg.BatchSize)
	predictFrames := make([]PredictFrame, 0, h.adapter.cfg.BatchSize)
	alarmFrames := make([]AlarmFrame, 0, h.adapter.cfg.BatchSize)
	ticker := time.NewTicker(h.adapter.cfg.FlushInterval)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 && len(predictFrames) == 0 && len(alarmFrames) == 0 {
			return
		}
		if len(batch) > 0 {
			if err := h.adapter.flushBatch(sess.Context(), batch); err != nil {
				log.Printf("[ERROR] flush failed: %v", err)
				return
			}
		}
		// 状态机：predict frames（含空帧）独立推送给 lifecycle writer
		if len(predictFrames) > 0 && h.adapter.lifecycle != nil {
			lcCtx, lcCancel := context.WithTimeout(sess.Context(), 10*time.Second)
			if err := h.adapter.lifecycle.ProcessFrames(lcCtx, predictFrames); err != nil {
				log.Printf("[WARN] lifecycle write failed (continuing): %v", err)
			}
			lcCancel()
		}
		// 状态机：alarm frames（含空帧）推送给 alarm lifecycle writer（双轨：同时已写 fact_event）
		if len(alarmFrames) > 0 && h.adapter.alarmLifecycle != nil {
			lcCtx, lcCancel := context.WithTimeout(sess.Context(), 10*time.Second)
			if err := h.adapter.alarmLifecycle.ProcessFrames(lcCtx, alarmFrames); err != nil {
				log.Printf("[WARN] alarm lifecycle write failed (continuing): %v", err)
			}
			lcCancel()
		}
		for _, item := range batch {
			sess.MarkMessage(item.msg.inner, "")
		}
		batch = batch[:0]
		predictFrames = predictFrames[:0]
		alarmFrames = alarmFrames[:0]
	}

	for {
		select {
		case <-sess.Context().Done():
			flush()
			return nil
		case <-ticker.C:
			flush()
		case msg, ok := <-claim.Messages():
			if !ok {
				flush()
				return nil
			}

			// 日志：每收到一条消息
			if h.adapter.cfg.LogLevel == "DEBUG" {
				log.Printf("[DEBUG] received msg from topic=%s partition=%d offset=%d", msg.Topic, msg.Partition, msg.Offset)
			}

			isEventTopic := msg.Topic == "signal-alarm" || msg.Topic == "signal-predict" || msg.Topic == "signal-life"

			if isEventTopic {
				var ev EventRecord
				if err := json.Unmarshal(msg.Value, &ev); err != nil {
					log.Printf("[WARN] invalid event json topic=%s err=%v", msg.Topic, err)
					sess.MarkMessage(msg, "")
					continue
				}

				eventType := "alarm"
				if msg.Topic == "signal-predict" {
					eventType = "predict"
				} else if msg.Topic == "signal-life" {
					eventType = "life"
				}

				// 顶层 event_meta（适配 nb67 输出格式）；旧消息若 EventMeta 在 hit 里也兼容
				topMeta := ev.EventMeta

				// 双轨下线（2026-08-28）：alarm 已切换到 hvac.alarm_lifecycle 单一事实表，
				// 停止向 fact_event 写 alarm 行；predict/life 仍写 fact_event。
				// alarm 消息仍进入 batch（保证 offset 正常提交），flats 保持为空。
				flats := make([]EventFlatRecord, 0, len(ev.Hits))
				if eventType != "alarm" {
					for _, hit := range ev.Hits {
						sev := hit.Severity
						if hit.Level != nil {
							sev = *hit.Level
						}

						// hit.EventMeta 在旧 schema 中携带元数据；新 schema 走顶层
						meta := hit.EventMeta
						if meta.DeviceID == "" {
							meta = topMeta
						}

						// 为了方便以后分析，payload_json 包含原始 hit 信息
						payload, _ := json.Marshal(hit)

						flats = append(flats, EventFlatRecord{
							EventTime:  meta.EventTimeText,
							IngestTime: time.Now().Format(time.RFC3339),
							LineID:     meta.LineIDInt32(),
							TrainID:    meta.TrainIDInt32(),
							CarriageID: meta.CarriageIDInt32(),
							DeviceID:   meta.DeviceID,
							EventType:  eventType,
							FaultCode:  hit.Code,
							FaultName:  hit.Name,
							Severity:   sev,
							Payload:    payload,
						})
					}
				}

				// predict 帧（含空帧）走状态机；EventMeta 来自顶层
				if eventType == "predict" && topMeta.DeviceID != "" {
					frame := PredictFrame{
						DeviceID:      topMeta.DeviceID,
						LineID:        topMeta.LineIDInt32(),
						TrainID:       topMeta.TrainIDInt32(),
						CarriageID:    topMeta.CarriageIDInt32(),
						EventTimeText: topMeta.EventTimeText,
					}
					for _, hit := range ev.Hits {
						sev := hit.Severity
						if hit.Level != nil {
							sev = *hit.Level
						}
						payload, _ := json.Marshal(hit)
						frame.Hits = append(frame.Hits, PredictHitMeta{
							FaultCode: hit.Code,
							FaultName: hit.Name,
							Severity:  sev,
							Payload:   payload,
						})
					}
					predictFrames = append(predictFrames, frame)
				}
				// alarm 帧走 alarm lifecycle writer（fact_event 不再写 alarm 行）
				if eventType == "alarm" && topMeta.DeviceID != "" {
					alarmFrame := AlarmFrame{
						DeviceID:      topMeta.DeviceID,
						LineID:        topMeta.LineIDInt32(),
						TrainID:       topMeta.TrainIDInt32(),
						CarriageID:    topMeta.CarriageIDInt32(),
						EventTimeText: topMeta.EventTimeText,
					}
					for _, hit := range ev.Hits {
						sev := hit.Severity
						if hit.Level != nil {
							sev = *hit.Level
						}
						payload, _ := json.Marshal(hit)
						alarmFrame.Hits = append(alarmFrame.Hits, AlarmHitMeta{
							FaultCode: hit.Code,
							FaultName: hit.Name,
							Severity:  sev,
							Payload:   payload,
						})
					}
					alarmFrames = append(alarmFrames, alarmFrame)
				}
				batch = append(batch, pendingMessage{isEvent: true, record: flats, msg: saramaMessage{inner: msg}})
			} else {
				var record StorageRecord
				if err := json.Unmarshal(msg.Value, &record); err != nil {
					log.Printf("[WARN] invalid storage json topic=%s err=%v", msg.Topic, err)
					sess.MarkMessage(msg, "")
					continue
				}
				batch = append(batch, pendingMessage{isEvent: false, record: record, msg: saramaMessage{inner: msg}})
			}

			if len(batch) >= h.adapter.cfg.BatchSize {
				flush()
			}
		}
	}
}

func (a *adapter) flushBatch(ctx context.Context, batch []pendingMessage) error {
	txCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	pgBatch := &pgx.Batch{}
	totalRaw := 0
	totalEvents := 0

	for _, item := range batch {
		if item.isEvent {
			if !a.cfg.WriteFactEvent {
				continue
			}
			flats := item.record.([]EventFlatRecord)
			for _, f := range flats {
				args, _ := f.toEventInsertArgs()
				pgBatch.Queue(insertEventSQL, args...)
				totalEvents++
			}
		} else {
			if !a.cfg.WriteFactRaw {
				continue
			}
			record := item.record.(StorageRecord)
			args, err := record.toInsertArgs(item.msg.inner.Value)
			if err != nil {
				continue
			}
			pgBatch.Queue(insertSQL, args...)
			totalRaw++
		}
	}

	totalCount := totalRaw + totalEvents
	if totalCount == 0 {
		return nil
	}

	start := time.Now()
	batchResult := a.pool.SendBatch(txCtx, pgBatch)
	defer batchResult.Close()

	for i := 0; i < totalCount; i++ {
		if _, err := batchResult.Exec(); err != nil {
			return fmt.Errorf("exec batch insert: %w", err)
		}
	}

	log.Printf("[INFO] flushed batch: raw=%d events=%d took=%v", totalRaw, totalEvents, time.Since(start))
	return nil
}
